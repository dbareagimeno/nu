package runtime

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"

	lua "github.com/yuin/gopher-lua"
)

// `nu.fs` — sistema de ficheros (api.md §5, sesión S14). Primitivas de IO de
// disco, **todas ⏸ salvo `cwd`** (que es síncrona y [W]): se construyen sobre el
// puente `suspend` del scheduler (S04, ADR-011) —sueltan el token, hacen el IO
// **bloqueante** en una goroutine de fondo que **jamás toca Lua**, y al volver
// recuperan el token y entregan el resultado vía `deliverFn`—. Es el primer
// submódulo de IO real; **el patrón que aquí se fija lo reusan S15/S16** (watch,
// proc) y toda la Fase 4 (red): el trabajo Go va dentro del `work func()`, los
// datos cruzan a Lua solo en la `deliverFn`, y los errores del SO se mapean a los
// códigos reservados (§1.4) antes de cruzar.
//
// "Lua decide, Go ejecuta" (ADR-004): el IO pesado (read entero, copy, walk del
// directorio) corre en Go, fuera del token, sin congelar el loop —mientras una
// task lee un fichero grande, otras progresan—. No se usa el `io`/`os` de Lua: el
// baseline del sandbox (§1.2) los dejó fuera; esto es Go puro bajo el token.
//
// Mapeo de errores del SO → códigos §1.4 (`mapFsError`, abajo): inexistente →
// `ENOENT` (salvo `stat`, que devuelve `nil` sin lanzar, §5); ya-existe →
// `EEXIST` (la pieza de `write{exclusive}`, G17, para lockfiles); permiso →
// `EACCES`; cualquier otro fallo de IO → `EIO`.

const (
	// fsDirPerm es el modo con que se crean directorios (`mkdir`): permisos
	// estándar de usuario, recortados por el umask del proceso como en cualquier
	// herramienta de terminal.
	fsDirPerm = 0o755
	// fsFilePerm es el modo con que se crean ficheros nuevos (`write`/`append`/
	// `copy`): legible/escribible por el dueño, legible por el grupo/otros, también
	// sujeto al umask. La escritura atómica preserva este modo en el rename.
	fsFilePerm = 0o644
)

// fsState es el estado de sesión del submódulo `fs`: hoy, solo el directorio
// temporal propio (`nu.fs.tmpdir`, §5). Se crea **perezosamente** la primera vez
// que `tmpdir` se invoca (no todas las sesiones lo necesitan) y se **reutiliza**
// en las siguientes; `Close` lo borra recursivamente. El candado protege la
// creación perezosa: las primitivas ⏸ corren su IO en goroutines de fondo, así
// que dos `tmpdir` concurrentes podrían carrera sobre el campo —el candado lo
// blinda sin depender del token.
type fsState struct {
	mu     sync.Mutex
	tmpdir string // directorio temporal de la sesión; "" hasta el primer tmpdir()
}

// registerFs cuelga `nu.fs` del global `nu` con sus firmas de §5. Lo llama
// `registerNu` (nu.go). Cada función ⏸ comprueba primero que corre dentro de una
// task (como el resto de ⏸: `cleanup`, `await`, `reload`): el chunk de `-e` y los
// handlers síncronos corren sobre `host` y no pueden suspender (§1.3) → `EINVAL`
// accionable.
func (rt *Runtime) registerFs(nu *lua.LTable) {
	L := rt.L
	fs := L.NewTable()
	fs.RawSetString("read", L.NewFunction(rt.fsRead))
	fs.RawSetString("write", L.NewFunction(rt.fsWrite))
	fs.RawSetString("append", L.NewFunction(rt.fsAppend))
	fs.RawSetString("stat", L.NewFunction(rt.fsStat))
	fs.RawSetString("list", L.NewFunction(rt.fsList))
	fs.RawSetString("mkdir", L.NewFunction(rt.fsMkdir))
	fs.RawSetString("remove", L.NewFunction(rt.fsRemove))
	fs.RawSetString("rename", L.NewFunction(rt.fsRename))
	fs.RawSetString("copy", L.NewFunction(rt.fsCopy))
	fs.RawSetString("tmpdir", L.NewFunction(rt.fsTmpdir))
	fs.RawSetString("cwd", L.NewFunction(rt.fsCwd))
	// `nu.fs.watch` (S15, §5): observador del FS. NO es ⏸ (no suspende) y es solo
	// estado principal (§16); entrega en lotes con debounce y filtrado gitignore
	// (G7). Su superficie y el tipo `Watcher` viven en watch.go.
	rt.registerWatch(fs)
	nu.RawSetString("fs", fs)
}

// requireTask es el guardia común de toda primitiva ⏸ de `fs`: si el código no
// corre dentro de una task (está sobre `host`), suspender es imposible (§1.3), así
// que se lanza `EINVAL` accionable y se devuelve false para que el llamante
// retorne sin tocar nada.
func (rt *Runtime) requireTask(L *lua.LState, fn string) bool {
	if L == rt.L {
		raiseError(L, CodeEINVAL, fn+" solo puede llamarse dentro de una task", lua.LNil)
		return false
	}
	return true
}

// mapFsError traduce un error de la stdlib de Go (un `*os.PathError`, etc.) a un
// error estructurado del core (§1.4) y lo **lanza** hacia Lua. Es el único punto
// donde un errno del SO cruza la frontera: centraliza el mapeo para que todas las
// primitivas de `fs` reporten códigos coherentes (`ENOENT`/`EEXIST`/`EACCES`/
// `EIO`). El mensaje conserva el texto del error de Go (la ruta incluida) como
// pista accionable; nunca se traga el error.
func mapFsError(L *lua.LState, err error) {
	switch {
	case errors.Is(err, os.ErrNotExist):
		raiseError(L, CodeENOENT, err.Error(), lua.LNil)
	case errors.Is(err, os.ErrExist):
		raiseError(L, CodeEEXIST, err.Error(), lua.LNil)
	case errors.Is(err, os.ErrPermission):
		raiseError(L, CodeEACCES, err.Error(), lua.LNil)
	default:
		raiseError(L, CodeEIO, err.Error(), lua.LNil)
	}
}

// fsRead implementa `nu.fs.read(path) -> string` ⏸ (§5): lee el fichero entero.
// El IO (la lectura completa) va en la goroutine de fondo; el contenido cruza a
// Lua como `LString` en la `deliverFn`. Inexistente → `ENOENT`; permiso →
// `EACCES`.
func (rt *Runtime) fsRead(L *lua.LState) int {
	if !rt.requireTask(L, "nu.fs.read") {
		return 0
	}
	path := L.CheckString(1)

	vals := rt.sched.suspend(L, func() deliverFn {
		data, err := os.ReadFile(path) // IO bloqueante, fuera del token
		return func(L *lua.LState) []lua.LValue {
			if err != nil {
				mapFsError(L, err)
				return nil
			}
			return []lua.LValue{lua.LString(data)}
		}
	})
	return pushAll(L, vals)
}

// fsWrite implementa `nu.fs.write(path, data, opts?)` ⏸ (§5): **escritura
// atómica**. El camino normal escribe a un fichero **temporal en el MISMO
// directorio destino** y luego hace `rename` —un rename dentro del mismo
// sistema de ficheros es atómico, así que un lector concurrente ve el contenido
// viejo o el nuevo entero, **jamás un fichero a medias**—. El temporal se limpia
// si algo falla antes del rename (no deja residuo).
//
// `opts.exclusive = true` (G17): crea **solo si no existe**, en una **única
// operación indivisible** con `O_EXCL`. Aquí NO se usa temporal+rename porque el
// rename sobreescribiría un fichero existente, rompiendo la exclusión; `O_EXCL`
// es la primitiva del SO que falla con `EEXIST` si el fichero ya existe. Es la
// pieza para lockfiles de sesiones (sesiones.md §6): la creación del lock debe
// ser atómica y fallar si otro ya lo tiene.
func (rt *Runtime) fsWrite(L *lua.LState) int {
	if !rt.requireTask(L, "nu.fs.write") {
		return 0
	}
	path := L.CheckString(1)
	data := []byte(L.CheckString(2))
	exclusive := false
	if opts, ok := L.Get(3).(*lua.LTable); ok {
		exclusive = lua.LVAsBool(opts.RawGetString("exclusive"))
	}

	vals := rt.sched.suspend(L, func() deliverFn {
		var err error
		if exclusive {
			err = writeExclusive(path, data)
		} else {
			err = writeAtomic(path, data)
		}
		return func(L *lua.LState) []lua.LValue {
			if err != nil {
				mapFsError(L, err)
			}
			return nil
		}
	})
	return pushAll(L, vals)
}

// writeAtomic realiza la escritura atómica del camino normal de `write`: temporal
// en el MISMO directorio destino + `rename`. El temporal va al mismo dir (no a
// `/tmp`) para garantizar que el rename es **same-filesystem** y por tanto
// atómico —un rename entre sistemas de ficheros distintos no es atómico (y en Go
// ni siquiera funciona con `os.Rename`)—. Si algo falla tras crear el temporal,
// se borra para no dejar residuo (la prueba 🔒 verifica "no queda temporal").
func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".nu-fs-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	// Limpieza best-effort: si retornamos por error antes del rename, el temporal
	// no debe sobrevivir. Tras un rename con éxito, `tmpName` ya no existe con ese
	// nombre, así que el `os.Remove` diferido es un no-op inocuo.
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	// Iguala el modo del temporal (que `CreateTemp` crea 0600) al modo estándar de
	// ficheros nuevos, para que un `write` produzca un fichero con permisos
	// normales y no el restrictivo del temporal.
	if err := os.Chmod(tmpName, fsFilePerm); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	committed = true
	return nil
}

// writeExclusive realiza `write{exclusive=true}` (G17): `O_EXCL` crea el fichero
// **solo si no existe**, en una única llamada al SO. Si ya existe, `OpenFile`
// falla con un error que envuelve `os.ErrExist` → `mapFsError` lo rinde como
// `EEXIST`. No hay temporal+rename: la exclusión exige que la creación misma sea
// la operación indivisible.
func writeExclusive(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fsFilePerm)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// fsAppend implementa `nu.fs.append(path, data)` ⏸ (§5): añade `data` al final del
// fichero (lo crea si no existe). No es atómico como `write` —un append es por
// naturaleza incremental (logs, JSONL de sesiones)—: abre en modo `O_APPEND` y
// escribe. El `O_APPEND` del SO garantiza que cada escritura va al final aunque
// otro proceso escriba a la vez.
func (rt *Runtime) fsAppend(L *lua.LState) int {
	if !rt.requireTask(L, "nu.fs.append") {
		return 0
	}
	path := L.CheckString(1)
	data := []byte(L.CheckString(2))

	vals := rt.sched.suspend(L, func() deliverFn {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, fsFilePerm)
		if err == nil {
			_, err = f.Write(data)
			if cerr := f.Close(); err == nil {
				err = cerr
			}
		}
		return func(L *lua.LState) []lua.LValue {
			if err != nil {
				mapFsError(L, err)
			}
			return nil
		}
	})
	return pushAll(L, vals)
}

// fsStat implementa `nu.fs.stat(path) -> {size, mtime_ms, is_dir, mode}?` ⏸ (§5).
// **No lanza `ENOENT`**: un fichero inexistente devuelve `nil` (es la consulta
// "¿existe y qué es?", no una lectura que falla). Cualquier OTRO error (permiso
// sobre un componente del path, IO) sí se lanza. `mtime_ms` es el tiempo de
// modificación en **milisegundos** (§1.5: los tiempos del core son en ms); `mode`
// son los bits de permiso Unix.
func (rt *Runtime) fsStat(L *lua.LState) int {
	if !rt.requireTask(L, "nu.fs.stat") {
		return 0
	}
	path := L.CheckString(1)

	vals := rt.sched.suspend(L, func() deliverFn {
		info, err := os.Stat(path)
		return func(L *lua.LState) []lua.LValue {
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return []lua.LValue{lua.LNil} // inexistente → nil, NO lanza (§5)
				}
				mapFsError(L, err)
				return nil
			}
			t := L.NewTable()
			t.RawSetString("size", lua.LNumber(info.Size()))
			t.RawSetString("mtime_ms", lua.LNumber(info.ModTime().UnixMilli()))
			t.RawSetString("is_dir", lua.LBool(info.IsDir()))
			t.RawSetString("mode", lua.LNumber(info.Mode().Perm()))
			return []lua.LValue{t}
		}
	})
	return pushAll(L, vals)
}

// fsList implementa `nu.fs.list(dir) -> {name, is_dir}[]` ⏸ (§5): las entradas
// directas del directorio, **sin recursión** (para recursivo, `nu.search.files`,
// S27). Un directorio inexistente lanza `ENOENT` (a diferencia de `stat`: aquí
// listar exige que el directorio exista). Solo se captura `name`/`is_dir` por
// entrada —el `stat` por entrada (size/mtime) es trabajo aparte si se quiere, no
// se paga en un `list`.
func (rt *Runtime) fsList(L *lua.LState) int {
	if !rt.requireTask(L, "nu.fs.list") {
		return 0
	}
	dir := L.CheckString(1)

	type entry struct {
		name  string
		isDir bool
	}
	vals := rt.sched.suspend(L, func() deliverFn {
		des, err := os.ReadDir(dir)
		var entries []entry
		if err == nil {
			entries = make([]entry, len(des))
			for i, de := range des {
				entries[i] = entry{name: de.Name(), isDir: de.IsDir()}
			}
		}
		return func(L *lua.LState) []lua.LValue {
			if err != nil {
				mapFsError(L, err)
				return nil
			}
			arr := L.NewTable()
			for i, e := range entries {
				t := L.NewTable()
				t.RawSetString("name", lua.LString(e.name))
				t.RawSetString("is_dir", lua.LBool(e.isDir))
				arr.RawSetInt(i+1, t)
			}
			return []lua.LValue{arr}
		}
	})
	return pushAll(L, vals)
}

// fsMkdir implementa `nu.fs.mkdir(path)` ⏸ (§5): crea el directorio. Crea también
// los **padres que falten** (`MkdirAll`), y es **idempotente** si ya existe como
// directorio —es el comportamiento esperado de una herramienta de terminal
// (`mkdir -p`): nadie quiere encadenar mkdirs para crear `a/b/c`, ni que falle
// porque el directorio ya estaba—. Si el path existe pero es un **fichero**,
// `MkdirAll` falla (no se sobreescribe un fichero por un directorio). Decisión y
// motivación en claude_decisions.md (S14).
func (rt *Runtime) fsMkdir(L *lua.LState) int {
	if !rt.requireTask(L, "nu.fs.mkdir") {
		return 0
	}
	path := L.CheckString(1)

	vals := rt.sched.suspend(L, func() deliverFn {
		err := os.MkdirAll(path, fsDirPerm)
		return func(L *lua.LState) []lua.LValue {
			if err != nil {
				mapFsError(L, err)
			}
			return nil
		}
	})
	return pushAll(L, vals)
}

// fsRemove implementa `nu.fs.remove(path, opts?)` ⏸ (§5). Borra un fichero o un
// directorio **vacío** sin más. Un directorio **no vacío** exige
// `opts.recursive = true` —sin él, `os.Remove` falla y el error se rinde como
// `EIO`—: es la salvaguarda contra un `rm -rf` accidental; borrar un árbol entero
// debe ser explícito.
//
// **Inexistente → no-op** (no lanza `ENOENT`): borrar lo que ya no está deja el
// sistema en el estado deseado (el fichero no existe), que es justo lo que pedía
// la llamada —semántica idempotente, como `mkdir`—. Decisión en
// claude_decisions.md (S14).
func (rt *Runtime) fsRemove(L *lua.LState) int {
	if !rt.requireTask(L, "nu.fs.remove") {
		return 0
	}
	path := L.CheckString(1)
	recursive := false
	if opts, ok := L.Get(2).(*lua.LTable); ok {
		recursive = lua.LVAsBool(opts.RawGetString("recursive"))
	}

	vals := rt.sched.suspend(L, func() deliverFn {
		var err error
		if recursive {
			err = os.RemoveAll(path) // borra el árbol; no-op si no existe
		} else {
			err = os.Remove(path) // falla si es dir no vacío (→ EIO)
			if errors.Is(err, os.ErrNotExist) {
				err = nil // inexistente → no-op idempotente
			}
		}
		return func(L *lua.LState) []lua.LValue {
			if err != nil {
				mapFsError(L, err)
			}
			return nil
		}
	})
	return pushAll(L, vals)
}

// fsRename implementa `nu.fs.rename(from, to)` ⏸ (§5): renombra/mueve `from` a
// `to`. Es `os.Rename` directo —atómico dentro del mismo sistema de ficheros—;
// entre sistemas de ficheros distintos falla (lo que el usuario quiere ahí es
// `copy` + `remove`). `from` inexistente → `ENOENT`.
func (rt *Runtime) fsRename(L *lua.LState) int {
	if !rt.requireTask(L, "nu.fs.rename") {
		return 0
	}
	from := L.CheckString(1)
	to := L.CheckString(2)

	vals := rt.sched.suspend(L, func() deliverFn {
		err := os.Rename(from, to)
		return func(L *lua.LState) []lua.LValue {
			if err != nil {
				mapFsError(L, err)
			}
			return nil
		}
	})
	return pushAll(L, vals)
}

// fsCopy implementa `nu.fs.copy(from, to)` ⏸ (§5): copia el contenido de `from` a
// `to` (lo crea o lo sobreescribe). Copia en streaming (`io.Copy`) para no cargar
// un fichero grande entero en memoria. `from` inexistente → `ENOENT`. Solo
// ficheros: copiar un directorio recursivamente es trabajo de más alto nivel
// (Lua sobre `list`+`copy`), no una primitiva del core.
func (rt *Runtime) fsCopy(L *lua.LState) int {
	if !rt.requireTask(L, "nu.fs.copy") {
		return 0
	}
	from := L.CheckString(1)
	to := L.CheckString(2)

	vals := rt.sched.suspend(L, func() deliverFn {
		err := copyFile(from, to)
		return func(L *lua.LState) []lua.LValue {
			if err != nil {
				mapFsError(L, err)
			}
			return nil
		}
	})
	return pushAll(L, vals)
}

// copyFile copia el contenido de `from` a `to` en streaming. Abre el origen
// primero (su inexistencia/permiso es el error que el usuario espera ver) y solo
// entonces crea el destino. `io.Copy` mueve los bytes en bloques, sin materializar
// el fichero entero en RAM.
func copyFile(from, to string) error {
	src, err := os.Open(from)
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, fsFilePerm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		return err
	}
	return dst.Close()
}

// fsTmpdir implementa `nu.fs.tmpdir() -> string` ⏸ (§5): el directorio temporal
// **propio de la sesión**. Se crea una sola vez (perezosamente, la primera
// llamada) bajo el `os.TempDir()` del sistema y se **reutiliza** en las
// siguientes —así toda la sesión comparte un scratch único, que `Close` borra
// recursivamente—. La creación va en la goroutine de fondo (es IO); el candado de
// `fsState` serializa la decisión "crear o reutilizar" frente a llamadas
// concurrentes desde varias tasks.
func (rt *Runtime) fsTmpdir(L *lua.LState) int {
	if !rt.requireTask(L, "nu.fs.tmpdir") {
		return 0
	}
	vals := rt.sched.suspend(L, func() deliverFn {
		dir, err := rt.fs.ensureTmpdir()
		return func(L *lua.LState) []lua.LValue {
			if err != nil {
				mapFsError(L, err)
				return nil
			}
			return []lua.LValue{lua.LString(dir)}
		}
	})
	return pushAll(L, vals)
}

// ensureTmpdir crea el directorio temporal de la sesión la primera vez y lo
// devuelve cacheado después. Corre en la goroutine de fondo de `tmpdir` (fuera del
// token), de ahí el candado: dos `tmpdir` concurrentes no deben crear dos
// directorios ni correr una carrera sobre el campo.
func (s *fsState) ensureTmpdir() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tmpdir != "" {
		return s.tmpdir, nil
	}
	dir, err := os.MkdirTemp("", "nu-session-*")
	if err != nil {
		return "", err
	}
	s.tmpdir = dir
	return dir, nil
}

// closeTmpdir borra el directorio temporal de la sesión si llegó a crearse. Lo
// llama `Runtime.Close`: el scratch de la sesión no debe sobrevivir al proceso.
// Best-effort (un fallo al borrar no es accionable al cerrar).
func (s *fsState) closeTmpdir() {
	s.mu.Lock()
	dir := s.tmpdir
	s.tmpdir = ""
	s.mu.Unlock()
	if dir != "" {
		_ = os.RemoveAll(dir)
	}
}

// fsCwd implementa `nu.fs.cwd() -> string` [W] (§5): el directorio de trabajo,
// **inmutable durante la sesión**. Es la **única** función de `fs` que NO es ⏸:
// es una consulta pura y barata (`os.Getwd`), no suspende. Que sea inmutable es la
// semántica del contrato (no hay `chdir`): los subprocesos que quieran otro dir lo
// reciben por `opts.cwd` (§6), sin mover el cwd del proceso —cambiarlo sería un
// efecto global que rompería el aislamiento por tarea (ADR-008)—.
func (rt *Runtime) fsCwd(L *lua.LState) int {
	dir, err := os.Getwd()
	if err != nil {
		mapFsError(L, err)
		return 0
	}
	L.Push(lua.LString(dir))
	return 1
}

// pushAll empuja a la pila de `L` los valores que una primitiva ⏸ devolvió (los
// que la `deliverFn` construyó) y retorna su número, para el `return` de la
// función Go. Si la `deliverFn` lanzó un error (vía `mapFsError`/`raiseError`),
// nunca se llega aquí —el error desenrolla la pila Go antes—.
func pushAll(L *lua.LState, vals []lua.LValue) int {
	for _, v := range vals {
		L.Push(v)
	}
	return len(vals)
}
