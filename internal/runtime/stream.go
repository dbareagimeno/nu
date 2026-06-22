package runtime

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	lua "github.com/yuin/gopher-lua"
)

// `nu.http.stream` — respuesta HTTP en streaming (api.md §8, sesión S20,
// inventario 🔒). A diferencia de `nu.http.request` (S19, buffereada), `stream`
// **no lee el body entero**: hace la petición, devuelve un `Stream` **al recibir
// las cabeceras** (`Stream.status`, `Stream.headers`) y entrega el cuerpo trozo a
// trozo según llega —`Stream:chunks()` (crudo) o `Stream:events()` (parser SSE
// incorporado, la lógica 🔒)—. Es lo que pide ADR-005: los adaptadores de
// providers viven en Lua y consumen SSE de un endpoint que va emitiendo tokens.
//
// REUSA TODO S19 (claude_decisions.md S19/S20): el parseo de `opts`
// (`parseReqOpts`), el modelo del cliente reutilizable vs por-petición
// (`clientFor`, con TLS/proxy de G12) y el mapeo de errores de transporte
// (`classifyTransportError`/`httpError`, que ya deciden el código del core fuera
// del token). Lo único que cambia es el consumo del body: no `io.ReadAll`, sino
// una goroutine de fondo que lo lee a trozos hacia un buffer **acotado**.
//
// EL PUENTE ⏸ (S04, ADR-011). Como el resto de IO, `stream` y sus iteradores son
// ⏸: sueltan el token y bloquean en la goroutine de fondo, que **JAMÁS toca Lua**.
// `nu.http.stream` suspende hasta las cabeceras; cada `next` de `chunks()`/
// `events()` suspende hasta el siguiente trozo/evento; los bytes (o el evento ya
// parseado en Go) cruzan a Lua solo en la `deliverFn`, con el token recuperado.
//
// EL MODELO DEL BUFFER ACOTADO Y EL BACKPRESSURE → EIO (§8). El body se lee en
// **una sola** goroutine de fondo que arranca al recibir las cabeceras y empuja
// los trozos crudos a una cola interna (`chunkQueue`) protegida por mutex+cond.
// La cola lleva la cuenta de los **bytes pendientes** (`buffered`); si un trozo
// nuevo haría superar el límite (`maxStreamBuffer`) porque Lua consume más lento
// de lo que el servidor empuja, el stream **falla con `EIO`** en vez de crecer sin
// límite —es la semántica de backpressure de §8: el buffer tiene tope, y
// desbordarlo es un error, no una espera infinita ni una fuga de memoria—. El
// consumidor (`nextChunk`) saca de la cola bajo el mismo candado; si está vacía y
// el body no ha terminado, espera en la `cond` (con el token soltado, vía el
// puente ⏸: otras tasks progresan).
//
// IDLE TIMEOUT → ETIMEOUT (§8). Un SSE puede quedarse **mudo para siempre** sin
// cerrar la conexión. `opts.timeout_ms` solo cubre hasta las cabeceras; pasadas
// éstas, el plazo que protege contra un body que no envía nada es
// `opts.idle_timeout_ms`: un `time.Timer` que se **re-arma** cada vez que llegan
// bytes y que, al disparar, **cancela el contexto** de la petición —el `Read`
// bloqueado del body retorna error, que se clasifica como `ETIMEOUT`—.
//
// CLOSE / CLEANUP. `Stream:close()` aborta la conexión (cancela el contexto, cierra
// el body) y es **idempotente** (`closeOnce`). El idioma de vida es el de §6: quien
// abre el stream registra `nu.task.cleanup(function() st:close() end)`, de modo que
// al cancelar/terminar la task el stream se cierra sin fuga de goroutines. Como red
// de seguridad, `Runtime.Close` cierra todos los streams vivos (`stopAllStreams`).
// El `Stream` NO es un `ownedHandle` por dueño como `Proc`: un stream es de la task
// que lo consume (su vida es la del turno de IO), no del plugin —se ata con
// `cleanup`, no con el registro de `reload`—; aun así se rastrea para `Close`.

// streamTypeName identifica la metatabla del handle `Stream` (lo que devuelve
// `nu.http.stream`), de la que cuelgan `chunks`/`events`/`close` y los campos
// `status`/`headers`.
const streamTypeName = "nu.http.Stream"

// maxStreamBuffer es el tope de bytes del body **pendientes de consumir** en la
// cola interna de un `Stream` (§8). Si la goroutine de fondo acumula más que esto
// porque Lua no consume, el stream falla con `EIO` (backpressure desbordado). 8 MiB
// es holgado para SSE (eventos de tokens son pequeños) pero acota la memoria de un
// consumidor que se quedó atrás frente a un servidor que vuelca rápido.
const maxStreamBuffer = 8 << 20 // 8 MiB

// streamReadChunk es el tamaño de cada lectura del body en la goroutine de fondo.
// El body se lee en trozos de hasta este tamaño; `chunks()` los entrega tal cual
// llegan (el contrato dice "trozos crudos según llegan", no líneas ni tamaños
// fijos), así que un `Read` que devuelve menos se entrega con lo que trajo.
const streamReadChunk = 32 << 10 // 32 KiB

// httpStream es el handle Go detrás del userdata `Stream`. Guarda las cabeceras ya
// recibidas (status/headers, datos inmutables tras la respuesta), la cola acotada
// de trozos del body que llena la goroutine de fondo, y los mecanismos de cierre
// (contexto cancelable + body) e idle-timeout. El estado de la cola lo tocan dos
// goroutines —la de fondo (productor) y la del consumidor (vía el puente ⏸)— así
// que va bajo `mu`/`cond`, NO bajo el token (el token solo serializa Lua, y el
// productor jamás lo toma).
type httpStream struct {
	s *scheduler

	status  int
	headers map[string]string

	// cancel cancela el `context.Context` de la petición: lo dispara `close()` y el
	// idle-timeout. Cancelar hace que el `Read` bloqueado del body retorne error.
	cancel context.CancelFunc
	body   io.ReadCloser

	// idleTimer es el temporizador del idle-timeout (§8): se re-arma con cada trozo
	// recibido y, al disparar, cancela el contexto (body mudo demasiado tiempo →
	// `ETIMEOUT`). nil si no se pidió `idle_timeout_ms`.
	idleTimer *time.Timer
	idle      time.Duration

	// --- Cola acotada del body (productor: goroutine de fondo; consumidor: ⏸) ---
	mu       sync.Mutex
	cond     *sync.Cond
	queue    [][]byte // trozos crudos pendientes de consumir, en orden
	buffered int      // bytes acumulados en `queue` (para el tope de backpressure)
	done     bool     // la goroutine de fondo terminó de leer el body (EOF o error)
	readErr  error    // error de lectura del body ya clasificado (httpError); nil = EOF limpio

	// closeOnce hace `close()` idempotente; `closed` lo leen el productor y el
	// consumidor para dejar de trabajar tras un cierre. `idleFired` distingue una
	// cancelación del contexto por idle-timeout (→ `ETIMEOUT`) de una por `close()`
	// del usuario (→ fin normal); lo escribe `onIdle` y lo lee `finishRead`, ambos
	// bajo `mu`.
	closeOnce sync.Once
	closed    bool
	idleFired bool

	// leftover es el residuo del parser SSE incremental entre llamadas a
	// `events()`: bytes de un evento aún sin cerrar (una línea a medias, un evento
	// sin su línea en blanco final). Solo lo toca el consumidor (en la goroutine de
	// la task, una llamada a `events()` cada vez), nunca el productor: no necesita
	// candado. Ver `sseParser`.
	sse sseParser
}

// newHTTPStream construye el handle y arranca la goroutine de fondo que lee el
// body. Se llama bajo el token (en la `deliverFn` de `nu.http.stream`), pero la
// goroutine que lanza corre fuera de él.
func newHTTPStream(s *scheduler, status int, headers map[string]string, body io.ReadCloser, cancel context.CancelFunc, idle time.Duration) *httpStream {
	st := &httpStream{
		s:       s,
		status:  status,
		headers: headers,
		cancel:  cancel,
		body:    body,
		idle:    idle,
	}
	st.cond = sync.NewCond(&st.mu)
	if idle > 0 {
		// Idle-timeout: al disparar, cancela el contexto. El `Read` bloqueado del
		// body retornará error, que `readLoop` clasifica como `ETIMEOUT` (el contexto
		// se canceló sin que `close()` lo pidiera). Se arma ya (cubre el hueco entre
		// las cabeceras y el primer byte del body) y `readLoop` lo re-arma con cada
		// trozo.
		st.idleTimer = time.AfterFunc(idle, st.onIdle)
	}
	go st.readLoop()
	return st
}

// onIdle lo dispara el `idleTimer` cuando pasan `idle` ms sin recibir bytes del
// body (§8). Marca el motivo (idle) y cancela el contexto: el `Read` colgado
// retorna y `readLoop` rinde `ETIMEOUT`. Distinguir un cierre por idle de uno por
// `close()` del usuario lo hace `idleFired`.
func (st *httpStream) onIdle() {
	st.mu.Lock()
	st.idleFired = true
	st.mu.Unlock()
	if st.cancel != nil {
		st.cancel()
	}
}

// readLoop es el **productor**: lee el body en trozos **fuera del token** (jamás
// toca Lua) y los empuja a la cola acotada. Aplica el tope de backpressure
// (`maxStreamBuffer` → `EIO`) y re-arma el idle-timeout con cada trozo. Al terminar
// (EOF, error o cierre) marca `done`, guarda `readErr` ya clasificado y despierta a
// cualquier consumidor que espere en la `cond`.
func (st *httpStream) readLoop() {
	buf := make([]byte, streamReadChunk)
	for {
		n, err := st.body.Read(buf)
		if n > 0 {
			// Llegaron bytes: re-arma el idle-timeout (el body no está mudo) y encola
			// una copia (buf se reutiliza en la próxima iteración).
			if st.idleTimer != nil {
				st.idleTimer.Reset(st.idle)
			}
			chunk := make([]byte, n)
			copy(chunk, buf[:n])

			st.mu.Lock()
			if st.closed {
				st.mu.Unlock()
				break // cerrado mientras leíamos: descarta y termina
			}
			if st.buffered+len(chunk) > maxStreamBuffer {
				// Backpressure desbordado (§8): el consumidor va demasiado lento y el
				// buffer superaría su tope. Falla con `EIO` en vez de crecer sin límite.
				st.readErr = &httpError{code: CodeEIO, msg: "nu.http.stream: buffer de backpressure desbordado (consumidor demasiado lento)"}
				st.done = true
				st.cond.Broadcast()
				st.mu.Unlock()
				break
			}
			st.queue = append(st.queue, chunk)
			st.buffered += len(chunk)
			st.cond.Broadcast()
			st.mu.Unlock()
		}
		if err != nil {
			st.finishRead(err)
			return
		}
	}
	// Salida por backpressure/cierre: drena el idle-timer.
	if st.idleTimer != nil {
		st.idleTimer.Stop()
	}
}

// finishRead cierra el lado productor cuando el body terminó (EOF o error). EOF
// limpio → `readErr = nil` (el consumidor verá fin de stream); cualquier otro
// error se clasifica: un cierre/cancelación por `close()` del usuario es fin
// normal (no error); una cancelación por idle es `ETIMEOUT`; el resto, transporte
// (`ENET`/`EIO`). Despierta a los consumidores.
func (st *httpStream) finishRead(err error) {
	if st.idleTimer != nil {
		st.idleTimer.Stop()
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.done {
		return // ya cerrado por backpressure o un finish previo
	}
	st.done = true
	if errors.Is(err, io.EOF) {
		st.readErr = nil // fin limpio del body
	} else if st.closed && !st.idleFired {
		// La canceló `close()` del usuario: no es un error que reportar —el stream se
		// cerró a propósito; un `next` posterior verá "fin" (readErr nil) o ECLOSED.
		st.readErr = nil
	} else if st.idleFired {
		st.readErr = &httpError{code: CodeETIMEOUT, msg: "nu.http.stream: el body no envió bytes en idle_timeout_ms"}
	} else {
		// Error de transporte leyendo el body (conexión cortada a media respuesta,
		// reset): mismo mapeo que S19. No hay `ctx` aquí, pero un timeout de red lo
		// detecta `classifyTransportError` por el tipo del error.
		st.readErr = classifyTransportError(context.Background(), err)
	}
	st.cond.Broadcast()
}

// nextChunk es el **consumidor**: saca el siguiente trozo crudo de la cola,
// bloqueando en la `cond` si está vacía y el body no ha terminado. Corre en la
// goroutine de fondo del puente ⏸ (sin el token), de ahí que use el candado de la
// cola y no el token. Devuelve `(chunk, false, nil)` con datos; `(nil, true, nil)`
// en fin de stream (EOF limpio); `(nil, false, err)` si el body falló (error ya
// clasificado: `EIO`/`ETIMEOUT`/`ENET`).
func (st *httpStream) nextChunk() ([]byte, bool, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	for {
		if len(st.queue) > 0 {
			chunk := st.queue[0]
			st.queue = st.queue[1:]
			st.buffered -= len(chunk)
			return chunk, false, nil
		}
		if st.closed {
			return nil, false, errStreamClosed
		}
		if st.done {
			if st.readErr != nil {
				return nil, false, st.readErr
			}
			return nil, true, nil // EOF limpio: fin del stream
		}
		st.cond.Wait()
	}
}

// errStreamClosed lo devuelve `nextChunk` cuando el stream se cerró (`close()`)
// mientras se consumía: los iteradores lo rinden como `ECLOSED`.
var errStreamClosed = errors.New("nu.http.stream: stream cerrado")

// close aborta la conexión y libera recursos (§8). **Idempotente** (`closeOnce`):
// llamarlo dos veces, o desde un `cleanup` tras un fin natural, es inocuo. Cancela
// el contexto (desbloquea el `Read` del body), cierra el body, para el idle-timer
// y despierta a cualquier consumidor bloqueado (que verá `ECLOSED`). Es síncrono
// (no ⏸): cerrar es inmediato. Lo llaman `Stream:close`, el `cleanup` de quien lo
// abrió y `Runtime.Close` (vía `stopAllStreams`).
func (st *httpStream) close() {
	st.closeOnce.Do(func() {
		st.mu.Lock()
		st.closed = true
		st.cond.Broadcast()
		st.mu.Unlock()
		if st.idleTimer != nil {
			st.idleTimer.Stop()
		}
		if st.cancel != nil {
			st.cancel()
		}
		if st.body != nil {
			_ = st.body.Close()
		}
		st.s.untrackStream(st)
	})
}

// --- nu.http.stream -----------------------------------------------------------

// httpStreamStart implementa `nu.http.stream(opts) -> Stream` ⏸ (§8). Reusa el
// parseo de `opts` y el modelo de cliente de S19; lo que cambia es que **no
// bufferiza el body**: lee solo hasta las cabeceras y devuelve el `Stream`. El
// `opts.timeout_ms` cubre hasta aquí; el `opts.idle_timeout_ms` protege el body.
func (rt *Runtime) httpStream(L *lua.LState) int {
	if !rt.requireTask(L, "nu.http.stream") {
		return 0
	}
	opts, ok := parseReqOpts(L)
	if !ok {
		return 0 // parseReqOpts ya lanzó EINVAL
	}
	idle, ok := parseIdleTimeout(L)
	if !ok {
		return 0
	}

	vals := rt.sched.suspend(L, func() deliverFn {
		// Fuera del token: arma y lanza la petición, lee SOLO hasta las cabeceras.
		st, rerr := rt.http.openStream(rt.sched, opts, idle)
		return func(L *lua.LState) []lua.LValue {
			if rerr != nil {
				raiseHTTPError(L, rerr)
				return nil
			}
			rt.sched.trackStream(st)
			ud := L.NewUserData()
			ud.Value = st
			L.SetMetatable(ud, L.GetTypeMetatable(streamTypeName))
			return []lua.LValue{ud}
		}
	})
	return pushAll(L, vals)
}

// parseIdleTimeout extrae `opts.idle_timeout_ms` (§8), validándolo igual que
// `timeout_ms`: ausente → 0 (sin idle-timeout); presente debe ser un número
// positivo, si no `EINVAL`. Corre bajo el token (toca Lua) tras `parseReqOpts`.
func parseIdleTimeout(L *lua.LState) (time.Duration, bool) {
	tbl, ok := L.Get(1).(*lua.LTable)
	if !ok {
		return 0, true // parseReqOpts ya validó que opts es tabla; defensivo
	}
	switch v := tbl.RawGetString("idle_timeout_ms").(type) {
	case lua.LNumber:
		if v <= 0 {
			raiseError(L, CodeEINVAL, "nu.http.stream: opts.idle_timeout_ms debe ser positivo", lua.LNil)
			return 0, false
		}
		return time.Duration(v) * time.Millisecond, true
	case *lua.LNilType, nil:
		return 0, true
	default:
		raiseError(L, CodeEINVAL, "nu.http.stream: opts.idle_timeout_ms debe ser un número", lua.LNil)
		return 0, false
	}
}

// openStream hace la petición **fuera del token** y devuelve un `httpStream` con
// las cabeceras ya recibidas y la goroutine de fondo leyendo el body. NO lee el
// body aquí (esa es la diferencia con `do` de S19). Un status ≥ 400 NO es error
// (igual que `request`): se devuelve el `Stream` con su status. Solo el transporte
// (conexión/DNS/reset → `ENET`, `timeout_ms` hasta cabeceras → `ETIMEOUT`, uso
// malo → `EINVAL`) produce error. El `context` vive más allá de esta función (lo
// cierra `Stream:close`), así que el `cancel` se entrega al `httpStream`, no se
// difiere aquí.
func (st *httpState) openStream(sched *scheduler, o reqOpts, idle time.Duration) (*httpStream, error) {
	client, err := st.clientFor(o)
	if err != nil {
		return nil, &httpError{code: CodeEINVAL, msg: err.Error()}
	}

	// El `timeout_ms` cubre HASTA las cabeceras (§8); pasadas éstas, el plazo del
	// body es el idle-timeout. Por eso NO se usa `context.WithTimeout` para toda la
	// vida del stream (cortaría un SSE largo legítimo): se usa un contexto
	// cancelable y un `time.AfterFunc(timeout)` que solo se cancela si las cabeceras
	// no llegan a tiempo; al recibirlas, se detiene.
	ctx, cancel := context.WithCancel(context.Background())

	var headerTimer *time.Timer
	headerTimedOut := false
	var htMu sync.Mutex
	if o.timeout > 0 {
		headerTimer = time.AfterFunc(o.timeout, func() {
			htMu.Lock()
			headerTimedOut = true
			htMu.Unlock()
			cancel()
		})
	}

	var bodyReader io.Reader
	if o.hasBody {
		bodyReader = strings.NewReader(o.body)
	}
	req, err := http.NewRequestWithContext(ctx, o.method, o.rawURL, bodyReader)
	if err != nil {
		if headerTimer != nil {
			headerTimer.Stop()
		}
		cancel()
		return nil, &httpError{code: CodeEINVAL, msg: "nu.http.stream: " + err.Error()}
	}
	for name, value := range o.headers {
		req.Header.Set(name, value)
	}

	resp, err := client.Do(req)
	if headerTimer != nil {
		headerTimer.Stop()
	}
	if err != nil {
		cancel()
		htMu.Lock()
		timedOut := headerTimedOut
		htMu.Unlock()
		if timedOut {
			return nil, &httpError{code: CodeETIMEOUT, msg: "nu.http.stream: la petición excedió timeout_ms (hasta cabeceras)"}
		}
		return nil, classifyTransportError(ctx, err)
	}

	// Cabeceras recibidas: el `Stream` toma posesión del body y del `cancel`. La
	// goroutine de fondo (en `newHTTPStream`) empieza a leer el body de inmediato.
	return newHTTPStream(sched, resp.StatusCode, flattenHeaders(resp.Header), resp.Body, cancel, idle), nil
}

// --- registro y métodos del tipo Stream ---------------------------------------

// registerStreamType instala la metatabla del tipo `Stream` con `chunks`/`events`/
// `close` y un `__index` que también resuelve los campos `status`/`headers` (§8).
// Lo llama `registerHTTP` (http.go). Como `status`/`headers` son **campos** (no
// métodos) en el contrato, el `__index` los devuelve directamente y delega el
// resto en la tabla de métodos.
func (rt *Runtime) registerStreamType() {
	L := rt.L
	mt := L.NewTypeMetatable(streamTypeName)

	methods := L.NewTable()
	methods.RawSetString("chunks", L.NewFunction(rt.streamChunks))
	methods.RawSetString("events", L.NewFunction(rt.streamEvents))
	methods.RawSetString("close", L.NewFunction(rt.streamClose))

	// `__index` función: resuelve `status`/`headers` como campos y los métodos por
	// nombre. Un acceso desconocido cae a nil (como una tabla normal).
	L.SetField(mt, "__index", L.NewFunction(func(L *lua.LState) int {
		st := checkStream(L)
		if st == nil {
			return 0
		}
		key := L.CheckString(2)
		switch key {
		case "status":
			L.Push(lua.LNumber(st.status))
			return 1
		case "headers":
			h := L.NewTable()
			for name, value := range st.headers {
				h.RawSetString(name, lua.LString(value))
			}
			L.Push(h)
			return 1
		default:
			L.Push(methods.RawGetString(key))
			return 1
		}
	}))
}

// checkStream recupera el `*httpStream` del userdata `self` del primer argumento.
// Lanza `EINVAL` si no es un handle de `Stream`.
func checkStream(L *lua.LState) *httpStream {
	ud := L.CheckUserData(1)
	st, ok := ud.Value.(*httpStream)
	if !ok {
		raiseError(L, CodeEINVAL, "Stream: se esperaba un handle de Stream", lua.LNil)
		return nil
	}
	return st
}

// streamChunks implementa `Stream:chunks() -> iterator` ⏸ (§8). Devuelve una
// **función iteradora** que, en cada llamada, suspende hasta el siguiente trozo
// crudo del body y lo devuelve; `nil` al terminar el body (fin del stream). Un
// error de transporte/backpressure/idle se **lanza** desde el `next` (capturable
// con `pcall`).
func (rt *Runtime) streamChunks(L *lua.LState) int {
	st := checkStream(L)
	if st == nil {
		return 0
	}
	L.Push(L.NewFunction(func(L *lua.LState) int {
		if !rt.requireTask(L, "Stream:chunks (next)") {
			return 0
		}
		vals := rt.sched.suspend(L, func() deliverFn {
			chunk, eof, rerr := st.nextChunk()
			return func(L *lua.LState) []lua.LValue {
				return deliverChunk(L, chunk, eof, rerr)
			}
		})
		return pushAll(L, vals)
	}))
	return 1
}

// deliverChunk traduce el resultado de `nextChunk` a los valores que el iterador
// devuelve a Lua: el string del trozo, `nil` en fin de stream, o el error mapeado.
// Corre bajo el token (es la `deliverFn`).
func deliverChunk(L *lua.LState, chunk []byte, eof bool, rerr error) []lua.LValue {
	if rerr != nil {
		raiseStreamError(L, rerr)
		return nil
	}
	if eof {
		return []lua.LValue{lua.LNil} // fin del body
	}
	return []lua.LValue{lua.LString(chunk)}
}

// raiseStreamError lanza el error de un `next` de stream hacia Lua: un cierre
// (`ECLOSED`) o un `httpError` ya clasificado (`EIO`/`ETIMEOUT`/`ENET`).
func raiseStreamError(L *lua.LState, err error) {
	if errors.Is(err, errStreamClosed) {
		raiseError(L, CodeECLOSED, "nu.http.stream: el stream fue cerrado", lua.LNil)
		return
	}
	raiseHTTPError(L, err)
}

// streamEvents implementa `Stream:events() -> iterator` ⏸ (§8), la lógica 🔒.
// Devuelve una función iteradora que, en cada llamada, suspende hasta tener un
// **evento SSE completo** y devuelve una tabla `{event?, data, id?}`; `nil` al
// terminar el body. El parser SSE es **incremental** (`sseParser`): un evento
// puede llegar partido entre varios trozos de red, así que cada `next` consume
// trozos crudos (vía `nextChunk`) hasta cerrar un evento o agotar el body.
func (rt *Runtime) streamEvents(L *lua.LState) int {
	st := checkStream(L)
	if st == nil {
		return 0
	}
	L.Push(L.NewFunction(func(L *lua.LState) int {
		if !rt.requireTask(L, "Stream:events (next)") {
			return 0
		}
		// El bucle puede suspender VARIAS veces (un evento partido en N trozos): cada
		// `suspend` saca un trozo, lo mete en el parser y, si aún no hay evento
		// completo, vuelve a suspender por el siguiente. El parser guarda su estado
		// (`leftover`) entre trozos y entre llamadas a `next`.
		for {
			// 1) ¿El parser ya tiene un evento completo de un trozo anterior?
			if ev, has := st.sse.next(); has {
				return pushEvent(L, ev)
			}
			// 2) No: pide el siguiente trozo crudo (suspende).
			var (
				chunk []byte
				eof   bool
				rerr  error
			)
			rt.sched.suspend(L, func() deliverFn {
				chunk, eof, rerr = st.nextChunk()
				return func(L *lua.LState) []lua.LValue { return nil }
			})
			if rerr != nil {
				raiseStreamError(L, rerr)
				return 0
			}
			if eof {
				// Fin del body: vacía el parser (un último evento sin línea en blanco
				// final se despacha en EOF, semántica SSE); si no queda nada, fin.
				if ev, has := st.sse.flush(); has {
					return pushEvent(L, ev)
				}
				L.Push(lua.LNil)
				return 1
			}
			st.sse.feed(chunk)
		}
	}))
	return 1
}

// pushEvent empuja a Lua la tabla `{event?, data, id?}` de un evento SSE (§8). Los
// campos opcionales (`event`, `id`) solo se ponen si el evento los traía —`data`
// siempre está (un evento sin `data` no se despacha, ver `sseParser`)—.
func pushEvent(L *lua.LState, ev sseEvent) int {
	t := L.NewTable()
	t.RawSetString("data", lua.LString(ev.data))
	if ev.hasEvent {
		t.RawSetString("event", lua.LString(ev.event))
	}
	if ev.hasID {
		t.RawSetString("id", lua.LString(ev.id))
	}
	L.Push(t)
	return 1
}

// streamClose implementa `Stream:close()` (§8): aborta la conexión y libera. **No
// es ⏸** (cerrar es inmediato) e idempotente.
func (rt *Runtime) streamClose(L *lua.LState) int {
	st := checkStream(L)
	if st == nil {
		return 0
	}
	st.close()
	return 0
}
