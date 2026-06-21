package runtime

import (
	"os"
	"path/filepath"
	"sync"
	"time"

	lua "github.com/yuin/gopher-lua"
)

// `nu.log` (api.md §15): las extensiones registran lo que pasa a un fichero en
// `data_dir`, **nunca a la pantalla** —la UI es competencia de las extensiones,
// y un kernel que escupe a stdout/stderr contaminaría la salida de `nu -e` y la
// TUI por igual. Cada línea anota el plugin de origen, para que en un log
// compartido se sepa quién habló.
//
// La superficie es deliberadamente mínima: cuatro niveles (`debug/info/warn/
// error`) con la misma firma `(fmt, ...)`, y `print` redefinido como alias de
// `info`. Sin niveles de filtrado, sin rotación (P20: un log de texto crece
// despacio; se reabre si aparecen logs de varios MB).

const logFileName = "nu.log"

// logLevel es la etiqueta textual que precede a cada línea. No hay umbral de
// filtrado en v1: los cuatro niveles escriben siempre; el nivel es solo una
// anotación para quien lea el fichero.
type logLevel string

const (
	levelDebug logLevel = "DEBUG"
	levelInfo  logLevel = "INFO"
	levelWarn  logLevel = "WARN"
	levelError logLevel = "ERROR"
)

// logger serializa las escrituras al fichero de log. El estado principal es
// single-threaded (ADR-004), pero `nu.log` es **[W]** (disponible en workers,
// §16): varios estados Lua de la misma proceso pueden compartir el fichero, así
// que el `mutex` no es decorativo. El fichero se abre **perezosamente** en la
// primera escritura: un `nu -e` que no loguea nada no crea ni el directorio ni
// el fichero.
type logger struct {
	mu   sync.Mutex
	path string           // <data_dir>/nu.log
	f    *os.File         // nil hasta la primera escritura
	now  func() time.Time // inyectable en tests para timestamps deterministas
}

// newLogger prepara un logger sobre `path` sin tocar el disco todavía.
func newLogger(path string) *logger {
	return &logger{path: path, now: time.Now}
}

// write añade una línea `<timestamp> <LEVEL> [<owner>] <message>` al fichero,
// abriéndolo (y creando su directorio) si es la primera vez. Es **best-effort**:
// si el disco falla, devuelve el error pero el llamante lo ignora —un fallo al
// loguear no debe tumbar el programa ni, menos aún, escupir a la pantalla (que
// es justo lo que `nu.log` promete no hacer).
func (lg *logger) write(level logLevel, owner, msg string) error {
	lg.mu.Lock()
	defer lg.mu.Unlock()

	if lg.f == nil {
		if err := os.MkdirAll(filepath.Dir(lg.path), 0o700); err != nil {
			return err
		}
		// 0600: el log puede contener fragmentos de prompts o rutas privadas;
		// es del usuario y de nadie más (coherente con los permisos de
		// data_dir/plugins en problemas.md G14).
		f, err := os.OpenFile(lg.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return err
		}
		lg.f = f
	}

	ts := lg.now().Format("2006-01-02T15:04:05.000Z07:00")
	line := ts + " " + padLevel(level) + " [" + owner + "] " + msg + "\n"
	_, err := lg.f.WriteString(line)
	return err
}

// close cierra el fichero si llegó a abrirse. Idempotente.
func (lg *logger) close() error {
	lg.mu.Lock()
	defer lg.mu.Unlock()
	if lg.f == nil {
		return nil
	}
	err := lg.f.Close()
	lg.f = nil
	return err
}

// padLevel alinea la etiqueta de nivel a 5 columnas (el ancho de "DEBUG"/
// "ERROR") para que las líneas queden tabuladas y el log sea legible a ojo.
func padLevel(level logLevel) string {
	s := string(level)
	for len(s) < 5 {
		s += " "
	}
	return s
}

// defaultDataDir calcula el `data_dir` por defecto donde vive el log (§14):
// `$XDG_DATA_HOME/nu` o `~/.local/share/nu`. S11 promoverá esta lógica a
// `nu.config.data_dir`; aquí solo se necesita el destino del log. Si no hay
// `HOME`, cae a un subdirectorio de los temporales del sistema antes que fallar:
// loguear nunca debe ser la razón de que el runtime no arranque.
func defaultDataDir() string {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "nu")
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".local", "share", "nu")
	}
	return filepath.Join(os.TempDir(), "nu")
}

// registerLog cuelga la tabla `nu.log` (con sus cuatro niveles) del global `nu`
// y redefine `print` como alias de `nu.log.info` (§15). Devuelve la función
// `info` para que el alias comparta exactamente la misma closure.
func registerLog(rt *Runtime, nu *lua.LTable) {
	L := rt.L

	logTbl := L.NewTable()
	info := L.NewFunction(rt.logFunc(levelInfo))
	logTbl.RawSetString("debug", L.NewFunction(rt.logFunc(levelDebug)))
	logTbl.RawSetString("info", info)
	logTbl.RawSetString("warn", L.NewFunction(rt.logFunc(levelWarn)))
	logTbl.RawSetString("error", L.NewFunction(rt.logFunc(levelError)))
	nu.RawSetString("log", logTbl)

	// `print` es alias de `info` (§15): la misma función, no una copia, para que
	// nadie pueda distinguirlas. Reemplaza al `print` que define OpenBase (que
	// escribiría a stdout) y al provisional de S01.
	L.SetGlobal("print", info)
}

// logFunc fabrica la closure Go que respalda un nivel de `nu.log`. Lee
// `rt.owner` en cada llamada (no al construirse): S11 hará que ese campo siga la
// pila de plugins, y el log reflejará el plugin activo en el momento de loguear.
func (rt *Runtime) logFunc(level logLevel) lua.LGFunction {
	return func(L *lua.LState) int {
		msg := logMessage(L)
		// Best-effort: un fallo de escritura no se propaga a Lua ni a la
		// pantalla (§15). Si el disco está roto, el programa sigue.
		_ = rt.log.write(level, rt.owner, msg)
		return 0
	}
}

// logMessage construye el texto de la línea a partir de los argumentos de la
// llamada `nu.log.<nivel>(fmt, ...)`:
//
//   - sin argumentos -> línea vacía;
//   - un solo argumento -> su `tostring` (respeta `__tostring`), sin tratarlo
//     como formato (así `print(t)` no rompe si `t` contiene un `%`);
//   - `fmt` + varargs -> `string.format(fmt, ...)`, delegando en Lua para tener
//     su semántica exacta de directivas.
func logMessage(L *lua.LState) string {
	n := L.GetTop()
	switch {
	case n == 0:
		return ""
	case n == 1:
		return L.ToStringMeta(L.Get(1)).String()
	default:
		return luaStringFormat(L, n)
	}
}

// luaStringFormat invoca `string.format` con los `n` argumentos que hay en la
// pila. Reutiliza la implementación de Lua en vez de reimplementar las
// directivas en Go: un formato mal escrito lanza el mismo error que en Lua, que
// se propaga al `pcall` envolvente —es un error del programador, no algo que el
// log deba tragarse en silencio.
func luaStringFormat(L *lua.LState, n int) string {
	strLib, ok := L.GetGlobal("string").(*lua.LTable)
	if !ok {
		return L.ToStringMeta(L.Get(1)).String()
	}
	format := strLib.RawGetString("format")
	L.Push(format)
	for i := 1; i <= n; i++ {
		L.Push(L.Get(i))
	}
	L.Call(n, 1)
	res := L.ToStringMeta(L.Get(-1)).String()
	L.Pop(1)
	return res
}
