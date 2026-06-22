package runtime

import (
	"os"
	"path/filepath"
	"sync"
	"time"

	ignore "github.com/sabhiram/go-gitignore"

	"github.com/fsnotify/fsnotify"
	lua "github.com/yuin/gopher-lua"
)

// `nu.fs.watch` — observador del sistema de ficheros (api.md §5, §16, sesión S15,
// inventario 🔒). Vigila un `path` (fichero o directorio) y, cuando cambia, llama
// a un handler **síncrono** con un **lote** de eventos. A diferencia del resto de
// `nu.fs` (todo ⏸), `watch` **NO es ⏸ y es solo estado principal** (§16: no [W],
// no se registra en workers): no suspende la task que lo arranca —devuelve un
// `Watcher` y sigue—; los cambios llegan después, como los disparos de un `every`
// (S05), por el camino del handler síncrono sobre el token.
//
// LAS TRES PIEZAS DE LÓGICA NUESTRA (las que el inventario 🔒 manda blindar, G7):
//
//  1. ENTREGA EN LOTES + DEBOUNCE (G7). El SO entrega los cambios uno a uno
//     (fsnotify reenvía cada evento inotify/kqueue/ReadDirectoryChanges). Un `git
//     checkout` que toca miles de ficheros generaría miles de eventos; entregarlos
//     uno a uno ahogaría al handler. En su lugar, la goroutine de fondo **acumula**
//     los eventos en un buffer y arranca (o re-arma) un temporizador de
//     `debounce_ms`; cuando pasa ese tiempo **sin nuevos eventos**, vuelca TODO el
//     buffer como **un solo** `fn(events[])`. Así una ráfaga llega como un lote y
//     no como N llamadas —el batching/coalescing es lógica NUESTRA, no de fsnotify—.
//     El debounce es "trailing": el lote sale tras la calma, de modo que una ráfaga
//     continua se sigue agrupando (cada evento re-arma el reloj).
//
//  2. FILTRADO GITIGNORE (G7). Vigilar `node_modules/`, `.git/`, `target/`… es
//     ruido: lo que git ignora rara vez interesa a una herramienta de código. Con
//     `gitignore = true` (default), se parsea el `.gitignore` de la raíz observada
//     y **cada** evento cuyo path coincida con un patrón ignorado se descarta antes
//     de entrar al buffer —nunca llega al handler ni cuenta para el debounce—. El
//     `.git/` interno se ignora siempre (no aparece en `.gitignore` pero es ruido
//     universal de un repo).
//
//  3. RECURSIVO (alcance). fsnotify NO recursa por sí mismo (vigila directorios
//     concretos, no subárboles). Con `recursive = true` se **camina el árbol** al
//     arrancar y se añade cada subdirectorio (saltando los ignorados, para no
//     vigilar `node_modules/`); y un directorio **creado al vuelo** se añade al
//     watcher al verlo, de modo que los cambios bajo él también se reporten. El
//     alcance documentado: la recursión se reconstruye observando creaciones de
//     directorio; borrados de directorio los limpia el SO al desaparecer el watch.
//
// HILO Y CONCURRENCIA. La goroutine de fondo **jamás toca Lua**: recibe eventos
// del SO, filtra y acumula (datos Go puros), y para entregar el lote llama a
// `deliverBatch`, que **toma el token** (como `runSyncHandler` de los timers) y
// corre el handler en un thread efímero del estado principal bajo `pcall` por
// frontera (ADR-008). Es el mismo invariante que `every`: el trabajo de fondo va
// sin token; el código Lua, con token, en el estado principal. Cero data races
// (los paths cruzan como `string`, copiados; el handler se invoca bajo el token).
//
// QUIESCENCIA. Un `Watcher` activo **no** cuenta como trabajo de primer plano (no
// toca `pending`), igual que un `every`: un watcher nunca "termina", y haría que
// `nu -e` no volviera jamás. `Watcher:stop()` (o `Runtime.Close`) corta su
// goroutine y cierra el watcher del SO, sin fuga.

// watcherTypeName identifica la metatabla del handle `Watcher` (lo que devuelve
// `watch`), de la que cuelga `stop`.
const watcherTypeName = "nu.fs.Watcher"

// watchKindCreate/Modify/Remove son los `kind` de cada evento del lote (§5):
// `{path, kind}`. fsnotify reporta operaciones por máscara de bits (`Op`); las
// mapeamos a estos tres `kind` estables del contrato.
const (
	watchKindCreate = "create"
	watchKindModify = "modify"
	watchKindRemove = "remove"
)

// watchEvent es un evento ya filtrado, listo para el lote: el path absoluto y su
// `kind`. Es un dato Go puro (sin Lua): cruza a la `deliverBatch` que lo convierte
// en la tabla `{path, kind}` bajo el token.
type watchEvent struct {
	path string
	kind string
}

// luaWatcher es el handle Go detrás del userdata `Watcher`. Su goroutine corre el
// bucle fsnotify + debounce; `stopCh` la corta (cerrado por `stop`, idempotente
// vía `stopped`).
type luaWatcher struct {
	s   *scheduler
	fn  *lua.LFunction
	fsw *fsnotify.Watcher

	recursive bool
	debounce  time.Duration
	// gi es el matcher de `.gitignore` de la raíz observada (nil si `gitignore =
	// false` o no hay `.gitignore`). `nil` se trata como "no ignora nada".
	gi *ignore.GitIgnore

	stopCh   chan struct{}
	stopOnce sync.Once

	// ownerName es el dueño con que se etiquetó el watcher al crearse
	// (`currentOwner()` vigente en `watch`, S11). Es lo que hace que
	// `nu.plugin.reload` (S13, G2) pare exactamente los watchers de ESE plugin —un
	// `watch` que un plugin arrancó en su `init.lua` no debe seguir vigilando tras
	// recargarlo, "reload no deja handlers huérfanos", inventario 🔒—.
	ownerName string
}

// luaWatcher implementa ownedHandle (S13): el registro de handles por dueño
// (handles.go) lo para al recargar su plugin sin conocer su tipo concreto, igual
// que a un `*luaTimer` o un `*subscriber`.

// release corta el watcher (su goroutine y el watcher del SO), igual que
// `Watcher:stop`. `stopWatcher` es idempotente, así que liberar uno ya parado es
// inocuo. NO toca el registro de handles (eso lo orquesta `releaseOwnerHandles`,
// que ya vació la lista del dueño).
func (w *luaWatcher) release() { w.s.stopWatcher(w) }

// owner devuelve el dueño con que se etiquetó el watcher al crearse.
func (w *luaWatcher) owner() string { return w.ownerName }

// registerWatch cuelga `nu.fs.watch` de la tabla `nu.fs` ya creada por
// `registerFs`, e instala la metatabla del tipo `Watcher`. Lo llama `registerFs`
// (fs.go) para mantener toda la superficie de `nu.fs` junta.
func (rt *Runtime) registerWatch(fs *lua.LTable) {
	L := rt.L

	mt := L.NewTypeMetatable(watcherTypeName)
	index := L.NewTable()
	index.RawSetString("stop", L.NewFunction(rt.watchStop))
	L.SetField(mt, "__index", index)

	fs.RawSetString("watch", L.NewFunction(rt.fsWatch))
}

// fsWatch implementa `nu.fs.watch(path, opts?, fn) -> Watcher` (§5). NO es ⏸ (no
// suspende: arma el observador y devuelve el handle en el acto) y es **solo estado
// principal** (§16): vive en el estado principal —no se registra en los workers
// (S34)—, pero, como `every`/`on`, es invocable indistintamente desde el chunk, un
// handler síncrono, el `init.lua` **o desde dentro de una task** (las tasks corren
// en el event loop del estado principal y comparten el `nu` global).
//
// Por qué "solo estado principal" y no ⏸: el handler de `watch` es síncrono (como
// `every`/`on`), corre en el loop del estado principal; el bus de entrega
// (token + thread efímero) vive ahí. Que `watch` no sea ⏸ es coherente con que su
// trabajo no es "esperar un resultado" sino "registrar un observador que dispara
// luego": `watch` registra el `Watcher` síncronamente y devuelve, sin suspender la
// task que la llama —idéntico a `every`, que tampoco distingue host de task—. "Solo
// estado principal" (§16) significa "no en workers" (donde `fs.watch` ni se
// registra, S34), no "no en tasks".
func (rt *Runtime) fsWatch(L *lua.LState) int {
	path := L.CheckString(1)

	// Opciones con sus defaults de §5: `recursive = false`, `gitignore = true`,
	// `debounce_ms = 50`. El handler es el 3.º argumento (tras `path` y `opts`),
	// pero por ergonomía se acepta también `watch(path, fn)` —si el 2.º es la
	// función, no hay tabla de opts—.
	recursive := false
	gitignore := true
	debounceMs := 50.0
	var fn *lua.LFunction

	if f, ok := L.Get(2).(*lua.LFunction); ok {
		fn = f // forma watch(path, fn): defaults para todas las opciones
	} else {
		if opts, ok := L.Get(2).(*lua.LTable); ok {
			recursive = lua.LVAsBool(opts.RawGetString("recursive"))
			// `gitignore` default true: solo se desactiva con un `false` explícito.
			if v := opts.RawGetString("gitignore"); v != lua.LNil {
				gitignore = lua.LVAsBool(v)
			}
			if v, ok := opts.RawGetString("debounce_ms").(lua.LNumber); ok {
				debounceMs = float64(v)
			}
		}
		fn = L.CheckFunction(3)
	}

	if debounceMs < 0 {
		raiseError(L, CodeEINVAL, "nu.fs.watch: debounce_ms no puede ser negativo", lua.LNil)
		return 0
	}

	// El path debe existir para vigilarlo: fsnotify falla al añadir un inexistente.
	// Se reporta como `ENOENT`/`EACCES`/`EIO` por el mapeo común (§1.4).
	root, err := filepath.Abs(path)
	if err != nil {
		mapFsError(L, err)
		return 0
	}
	info, err := os.Stat(root)
	if err != nil {
		mapFsError(L, err)
		return 0
	}

	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		mapFsError(L, err)
		return 0
	}

	// Carga el `.gitignore` de la raíz si procede. La raíz para resolver patrones es
	// el directorio observado (o el dir del fichero, si `path` es un fichero): los
	// patrones de `.gitignore` son relativos a él. Un `.gitignore` ausente no es
	// error —simplemente no se ignora nada por esa vía—.
	var gi *ignore.GitIgnore
	ignoreRoot := root
	if !info.IsDir() {
		ignoreRoot = filepath.Dir(root)
	}
	if gitignore {
		if g, gerr := ignore.CompileIgnoreFile(filepath.Join(ignoreRoot, ".gitignore")); gerr == nil {
			gi = g
		}
	}

	w := &luaWatcher{
		s:         rt.sched,
		fn:        fn,
		fsw:       fsw,
		recursive: recursive,
		debounce:  time.Duration(debounceMs) * time.Millisecond,
		gi:        gi,
		stopCh:    make(chan struct{}),
		ownerName: rt.currentOwner(),
	}

	// Añade el path (y, si es recursivo y un directorio, su subárbol) al watcher del
	// SO. Si algo falla aquí, cierra el watcher y reporta —no se devuelve un handle
	// a medias—.
	if err := w.addTree(root, info.IsDir()); err != nil {
		_ = fsw.Close()
		mapFsError(L, err)
		return 0
	}

	rt.sched.trackWatcher(w)
	rt.sched.track(w) // registro de handles por dueño (S13): que `reload` lo encuentre y pare
	go w.run(ignoreRoot)

	ud := L.NewUserData()
	ud.Value = w
	L.SetMetatable(ud, L.GetTypeMetatable(watcherTypeName))
	L.Push(ud)
	return 1
}

// addTree añade `root` al watcher del SO; si `isDir` y el watcher es recursivo,
// camina el subárbol añadiendo cada subdirectorio **no ignorado**. fsnotify vigila
// directorios (no subárboles): para un fichero suelto se añade su directorio
// padre y luego se filtran en `run` los eventos que no son ese fichero. Para un
// directorio se añade él mismo; con `recursive`, además sus descendientes.
//
// Saltar los directorios ignorados aquí (no solo al filtrar eventos) evita
// **vigilarlos**: añadir `node_modules/` al watcher gastaría descriptores y
// generaría ruido que luego habría que descartar —mejor no observarlo de entrada—.
func (w *luaWatcher) addTree(root string, isDir bool) error {
	if !isDir {
		// Un fichero suelto: fsnotify no vigila ficheros de forma portable; se vigila
		// su directorio y `run` filtra los eventos que no afectan a este path.
		return w.fsw.Add(filepath.Dir(root))
	}
	if err := w.fsw.Add(root); err != nil {
		return err
	}
	if !w.recursive {
		return nil
	}
	// Camina el subárbol. Un error al leer una entrada concreta no aborta el watch
	// entero (best-effort: el subárbol observable es el que se pudo leer); el fallo
	// de añadir la raíz, en cambio, ya se reportó arriba.
	return filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // entrada ilegible: la saltamos, no rompemos el watch
		}
		if !d.IsDir() || p == root {
			return nil
		}
		if w.isIgnoredDir(p) {
			return filepath.SkipDir // no vigiles el subárbol ignorado (node_modules/...)
		}
		_ = w.fsw.Add(p) // best-effort: un subdir que no se pueda añadir se omite
		return nil
	})
}

// isIgnoredDir decide si un directorio debe excluirse de la vigilancia recursiva:
// el `.git/` interno (ruido universal de un repo) o lo que el `.gitignore` ignora.
// Es el filtro al **añadir** al watcher; el filtro de **eventos** (run) es análogo
// pero sobre cada path que llega.
func (w *luaWatcher) isIgnoredDir(p string) bool {
	if filepath.Base(p) == ".git" {
		return true
	}
	return w.matchesIgnore(p)
}

// matchesIgnore consulta el `.gitignore` cargado (si lo hay). Sin matcher, nada se
// ignora. Es el corazón del filtrado G7: un path que git ignora no genera evento.
func (w *luaWatcher) matchesIgnore(p string) bool {
	if w.gi == nil {
		return false
	}
	return w.gi.MatchesPath(p)
}

// run es el bucle de fondo del watcher: recibe eventos de fsnotify, los filtra
// (gitignore, `.git/`), los acumula en un buffer y los entrega **en lotes** tras
// `debounce_ms` de calma (G7). **Jamás toca Lua**: solo manipula datos Go; la
// entrega del lote (que sí toca Lua) la hace `deliverBatch` tomando el token.
//
// El debounce es trailing y coalescente: cada evento (re)arma `time.Timer`; el
// lote sale cuando pasa `debounce` **sin** nuevos eventos. Mientras hay ráfaga, el
// reloj se reinicia y el buffer crece —de ahí que un `git checkout` de miles de
// ficheros salga como UN lote, no como N—.
func (w *luaWatcher) run(ignoreRoot string) {
	var (
		buf   []watchEvent
		timer *time.Timer
		// fireCh es el canal del temporizador de debounce; nil cuando no hay reloj
		// armado (un `select` sobre un canal nil bloquea para siempre, que es justo
		// lo que queremos: no disparar si no hay nada acumulado).
		fireCh <-chan time.Time
	)
	// Garantiza que el temporizador no quede vivo al salir (sin fuga).
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()

	for {
		select {
		case <-w.stopCh:
			return

		case ev, ok := <-w.fsw.Events:
			if !ok {
				return // el watcher del SO se cerró
			}
			we, keep := w.classify(ev)
			if !keep {
				continue // filtrado (gitignore / .git / no concierne al fichero vigilado)
			}
			// Si es un directorio recién creado y el watch es recursivo, empieza a
			// vigilarlo: así los cambios bajo un dir nuevo también se reportan (alcance
			// documentado de `recursive`).
			if w.recursive && we.kind == watchKindCreate {
				if fi, serr := os.Stat(we.path); serr == nil && fi.IsDir() && !w.isIgnoredDir(we.path) {
					_ = w.fsw.Add(we.path)
				}
			}
			buf = append(buf, we)
			// (Re)arma el debounce: el lote saldrá tras `debounce` de calma.
			if timer == nil {
				timer = time.NewTimer(w.debounce)
			} else {
				if !timer.Stop() {
					// El timer ya disparó pero aún no lo hemos leído: drena su canal para
					// re-armarlo limpio (patrón estándar de reset de time.Timer).
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(w.debounce)
			}
			fireCh = timer.C

		case <-fireCh:
			// Pasó `debounce` sin nuevos eventos: vuelca el buffer como UN lote.
			batch := buf
			buf = nil
			fireCh = nil
			w.deliverBatch(batch)

		case err, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
			// Un error del backend (p. ej. overflow de la cola del SO) no debe tumbar
			// el watcher ni el proceso: queda en el log best-effort (ADR-008), como el
			// resto de fallos de fondo. La toma de token del log es segura desde aquí
			// porque `log.write` no toca Lua.
			_ = w.s.rt.log.write(levelError, w.ownerName, "nu.fs.watch: error del backend: "+err.Error())
		}
	}
}

// classify filtra y traduce un evento de fsnotify a un `watchEvent` del contrato.
// Devuelve `keep = false` si el evento debe descartarse: lo que `.gitignore`
// ignora, el `.git/` interno, o —cuando se vigila un fichero suelto— eventos de
// otros ficheros del mismo directorio. Mapea la máscara `Op` a `create`/`modify`/
// `remove` (§5).
//
// El filtrado gitignore (G7) ocurre AQUÍ, sobre cada evento, además de al añadir
// al watcher: un fichero ignorado dentro de un dir vigilado (p. ej. `dir/.env` con
// `.env` en `.gitignore`) no genera evento, aunque su directorio sí se observe.
func (w *luaWatcher) classify(ev fsnotify.Event) (watchEvent, bool) {
	// Filtro de ruido universal y gitignore. Se comprueba sobre el path del evento;
	// también se filtra cualquier cosa bajo un `.git/` (eventos internos del repo).
	if w.pathIgnored(ev.Name) {
		return watchEvent{}, false
	}

	var kind string
	switch {
	case ev.Op&fsnotify.Create != 0:
		kind = watchKindCreate
	case ev.Op&fsnotify.Remove != 0, ev.Op&fsnotify.Rename != 0:
		// Rename se reporta como remove del nombre viejo (el destino, si está dentro
		// del árbol vigilado, llega como Create aparte): para el observador, el path
		// viejo desapareció.
		kind = watchKindRemove
	case ev.Op&fsnotify.Write != 0, ev.Op&fsnotify.Chmod != 0:
		kind = watchKindModify
	default:
		return watchEvent{}, false // operación que no mapeamos: se ignora
	}
	return watchEvent{path: ev.Name, kind: kind}, true
}

// pathIgnored decide si el path de un evento debe descartarse: bajo un `.git/`, o
// ignorado por `.gitignore` (G7). Es el filtro de eventos, gemelo de
// `isIgnoredDir` (que filtra qué dirs vigilar).
func (w *luaWatcher) pathIgnored(p string) bool {
	// Cualquier componente `.git` en la ruta = ruido interno del repo.
	for cur := p; ; {
		if filepath.Base(cur) == ".git" {
			return true
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	return w.matchesIgnore(p)
}

// deliverBatch entrega un lote al handler **bajo el token**, en el estado
// principal, sobre un thread efímero y bajo `pcall` por frontera (ADR-008). Es el
// análogo de `runSyncHandler` (timers.go) para `watch`: la goroutine de fondo
// (sin token) llama aquí; aquí se toma el token y se toca Lua. Un lote vacío no se
// entrega (no debería ocurrir: solo se llama tras acumular al menos un evento).
//
// Atiende también a `stopCh` mientras espera el token: si se paró el watcher
// justo cuando un lote iba a entregarse, no se invoca el handler (contrato de
// `stop`: tras pararlo, no llegan más lotes).
func (w *luaWatcher) deliverBatch(batch []watchEvent) {
	if len(batch) == 0 {
		return
	}
	select {
	case <-w.s.gil:
		// Token tomado a mano (equivale a `acquire`); se suelta al salir.
		defer w.s.release()
		select {
		case <-w.stopCh:
			return // se pidió stop entre acumular y tomar el token: no entregues
		default:
		}
		w.callBatchLocked(batch)
	case <-w.stopCh:
		return
	}
}

// callBatchLocked construye la tabla `events[]` (array de `{path, kind}`) y llama
// al handler con ella, sobre un thread efímero, bajo `pcall`. **Presupone el token
// tomado.** El thread efímero (como en `every`/eventos) evita tocar la pila del
// estado principal, que mientras `EvalString` espera en `waitIdle` aún custodia
// los valores de retorno del chunk. Un error en el handler queda en el log
// best-effort (ADR-008), nunca tumba el proceso.
func (w *luaWatcher) callBatchLocked(batch []watchEvent) {
	co, _ := w.s.host.NewThread()
	arr := co.NewTable()
	for i, e := range batch {
		t := co.NewTable()
		t.RawSetString("path", lua.LString(e.path))
		t.RawSetString("kind", lua.LString(e.kind))
		arr.RawSetInt(i+1, t)
	}
	err := co.CallByParam(lua.P{Fn: w.fn, NRet: 0, Protect: true}, arr)
	if err != nil {
		_ = w.s.rt.log.write(levelError, w.s.rt.currentOwner(),
			"un handler de nu.fs.watch lanzó: "+errString(raisedValue(err)))
	}
}

// watchStop implementa `Watcher:stop()` (§5): corta el watcher (su goroutine de
// fondo y el watcher del SO) sin dejar nada colgado. Idempotente. Tras `stop` no
// llegan más lotes: la goroutine ve `stopCh` cerrado y retorna, y `deliverBatch`
// aborta si lo ve cerrado al ir a entregar.
func (rt *Runtime) watchStop(L *lua.LState) int {
	ud := L.CheckUserData(1)
	w, ok := ud.Value.(*luaWatcher)
	if !ok {
		raiseError(L, CodeEINVAL, "Watcher:stop espera un handle de Watcher", lua.LNil)
		return 0
	}
	rt.sched.stopWatcher(w)
	// Desregistra del registro de handles por dueño (S13): un `stop` a mano no debe
	// dejar el watcher parado colgando en `ownerHandles` (fuga; un `reload` posterior
	// intentaría re-pararlo). `untrack` es idempotente. Va aquí —en el camino
	// manual—, no en `stopWatcher`, porque `release()` (vía `reload`) ya vacía la
	// lista del dueño y no debe tocarla a media iteración.
	rt.sched.untrack(w)
	return 0
}
