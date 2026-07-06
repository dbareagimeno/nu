package runtime

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/coder/websocket"
	lua "github.com/yuin/gopher-lua"
)

// `nu.ws` — websockets (api.md §8, sesión S21). Una sola primitiva,
// `nu.ws.connect(url, opts?) -> Ws`, y tres métodos del handle: `Ws:send(data)`
// ⏸, `Ws:recv() -> string?` ⏸ (nil al cerrarse) y `Ws:close()`. Cierra la Fase 4
// (Red). Es el complemento full-duplex de `nu.http.stream` (S20): donde el stream
// es un body que el servidor va emitiendo (SSE), el websocket es un canal de ida
// y vuelta —el caso de un provider que empuja tokens y a la vez recibe control—.
//
// EL PUENTE ⏸ (S04, ADR-011). Como todo el IO de red, `connect`/`send`/`recv` son
// ⏸: sueltan el token y bloquean en la goroutine de fondo del puente `suspend`,
// que **JAMÁS toca Lua**. A diferencia de `nu.http.stream` (S20), aquí NO hace
// falta una goroutine permanente de lectura: el modelo de un websocket es
// *petición-respuesta dirigida por el consumidor* —Lua llama `recv()` cuando
// quiere el siguiente mensaje—, así que cada `send`/`recv` ejecuta su `Write`/
// `Read` bloqueante DENTRO de la goroutine de fondo de ESE `suspend` y los datos
// (o el error) cruzan a Lua solo en la `deliverFn`, con el token recuperado. Es el
// mismo patrón que `Proc:read_line`/`Proc:write` (S16), no el de `Stream` (que sí
// necesita un productor de fondo porque el body llega aunque nadie lo pida).
//
// LA LIBRERÍA (claude_decisions.md S21). Se usa `github.com/coder/websocket`
// (antes `nhooyr.io/websocket`): **puro-Go, sin dependencias transitivas**
// (`CGO_ENABLED=0` intacto, ADR-001), API limpia basada en `context.Context`
// (encaja con la cancelación de tasks) y mantenida. La alternativa
// `gorilla/websocket` exige un mutex propio para serializar escrituras y su API es
// más vieja; `coder/websocket` ya serializa internamente y su `Read`/`Write` por
// contexto es justo lo que el puente ⏸ necesita.
//
// EL MODELO DE recv → nil AL CERRAR (§8, criterio de hecho de S21). `recv()`
// devuelve `string?`: el mensaje recibido o **`nil` cuando la conexión se cierra**
// —ordenadamente (el servidor mandó un frame de cierre normal) o porque nosotros
// llamamos `Ws:close()`—. Distinguir "cierre normal" (→ nil, fin de stream, no es
// error) de un fallo de transporte real (→ `ENET`) lo hace `websocket.CloseStatus`:
// un cierre limpio (1000/1001) o un cierre local rinden `nil`; cualquier otro
// error de lectura es transporte (`ENET`). Así un bucle `while msg do ... end`
// termina solo cuando la otra punta cierra, igual que `Proc:read_line` con EOF.
//
// CLOSE / CLEANUP. `Ws:close()` cierra la conexión y es **idempotente**
// (`closeOnce`). El idioma de vida es el de §6 (igual que `Stream`): quien abre el
// websocket registra `nu.task.cleanup(function() w:close() end)`, de modo que al
// cancelar/terminar la task se cierra sin fuga de goroutines. Como red de
// seguridad, `Runtime.Close` cierra todos los websockets vivos (`stopAllWs`). El
// `Ws` se rastrea para `Close` (como `Stream`) pero **no** cuenta para la
// quiescencia: su vida es la del turno de IO, atada con `cleanup`, no con el
// registro de `reload`.

// wsTypeName identifica la metatabla del handle `Ws` (lo que devuelve
// `nu.ws.connect`), de la que cuelgan `send`/`recv`/`close`.
const wsTypeName = "nu.ws.Ws"

// wsReadLimit es el tope de bytes de un único mensaje entrante. `coder/websocket`
// trae 32 KiB por defecto, que es poco para el caso de un provider que empuja un
// turno grande en un solo mensaje; se sube a un límite holgado pero acotado (acota
// la memoria de un mensaje gigante malicioso). No es backpressure entre mensajes
// (eso es del stream): es el tamaño máximo de UN mensaje.
const wsReadLimit = 32 << 20 // 32 MiB

// luaWs es el handle Go detrás del userdata `Ws`. Guarda la conexión y un contexto
// cancelable que ata la vida de todas las operaciones: `close()` lo cancela, lo que
// desbloquea cualquier `Read`/`Write` en curso en una goroutine de fondo. El acceso
// a `closed` va bajo `mu` porque lo tocan el consumidor (vía el puente ⏸, sin el
// token) y `close()` (síncrono, bajo el token): el candado, no el token, evita la
// carrera (el token solo serializa Lua, y la goroutine de fondo jamás lo toma).
type luaWs struct {
	s *scheduler

	conn *websocket.Conn

	// ctx/cancel atan la vida de la conexión. `connect` los crea (sin deadline: una
	// conexión websocket es de larga duración, el plazo es solo del handshake); `Read`/
	// `Write` los usan como contexto base; `close()` llama a `cancel` para abortar
	// cualquier IO colgado.
	ctx    context.Context
	cancel context.CancelFunc

	mu        sync.Mutex
	closeOnce sync.Once
	closed    bool
}

// registerWs cuelga `nu.ws` del global `nu` con su firma de §8 e instala la
// metatabla del tipo `Ws`. Lo llama `registerNu` (nu.go).
func (rt *Runtime) registerWs(nu *lua.LTable) {
	L := rt.L
	wsT := L.NewTable()
	wsT.RawSetString("connect", L.NewFunction(rt.wsConnect))
	nu.RawSetString("ws", wsT)

	rt.registerWsType()
}

// registerWsType instala la metatabla del tipo `Ws` con `send`/`recv`/`close`.
func (rt *Runtime) registerWsType() {
	L := rt.L
	mt := L.NewTypeMetatable(wsTypeName)
	methods := L.NewTable()
	methods.RawSetString("send", L.NewFunction(rt.wsSend))
	methods.RawSetString("recv", L.NewFunction(rt.wsRecv))
	methods.RawSetString("close", L.NewFunction(rt.wsClose))
	L.SetField(mt, "__index", methods)
}

// checkWs recupera el `*luaWs` del userdata `self` del primer argumento. Lanza
// `EINVAL` si no es un handle de `Ws`.
func checkWs(L *lua.LState) *luaWs {
	ud := L.CheckUserData(1)
	w, ok := ud.Value.(*luaWs)
	if !ok {
		raiseError(L, CodeEINVAL, "Ws: se esperaba un handle de Ws", lua.LNil)
		return nil
	}
	return w
}

// --- nu.ws.connect ------------------------------------------------------------

// wsConnect implementa `nu.ws.connect(url, opts?) -> Ws` ⏸ (§8). El handshake va
// **fuera del token** (en la goroutine de fondo del puente `suspend`), y la
// función devuelve **al establecerse** la conexión. Un fallo de conexión (puerto
// cerrado, DNS, handshake rechazado) → `ENET`; expirar `timeout_ms` → `ETIMEOUT`;
// `url`/`opts` malos → `EINVAL` (antes de suspender).
func (rt *Runtime) wsConnect(L *lua.LState) int {
	if !rt.requireTask(L, "nu.ws.connect") {
		return 0
	}
	url, opts, ok := parseWsOpts(L)
	if !ok {
		return 0 // parseWsOpts ya lanzó EINVAL
	}

	vals := rt.sched.suspend(L, func() deliverFn {
		w, rerr := rt.dialWs(url, opts)
		return func(L *lua.LState) []lua.LValue {
			if rerr != nil {
				raiseHTTPError(L, rerr)
				return nil
			}
			rt.sched.trackWs(w)
			ud := L.NewUserData()
			ud.Value = w
			L.SetMetatable(ud, L.GetTypeMetatable(wsTypeName))
			return []lua.LValue{ud}
		}
	})
	return pushAll(L, vals)
}

// wsOpts son las opciones de `connect` ya extraídas de Lua (bajo el token); el
// dial las consume fuera del token.
type wsOpts struct {
	headers map[string]string
	timeout time.Duration // plazo del handshake; 0 = el default
}

// parseWsOpts extrae `url` (arg 1, string no vacío) y la tabla `opts?` (arg 2):
// `headers?` (string→string) y `timeout_ms?` (positivo). Valida en el estado
// principal (bajo el token) y lanza `EINVAL` ante un uso malo —antes de suspender,
// como el resto de primitivas ⏸—.
func parseWsOpts(L *lua.LState) (string, wsOpts, bool) {
	o := wsOpts{timeout: httpDefaultTimeout}

	urlVal, ok := L.Get(1).(lua.LString)
	if !ok || string(urlVal) == "" {
		raiseError(L, CodeEINVAL, "nu.ws.connect: url es obligatoria (string no vacío)", lua.LNil)
		return "", o, false
	}

	switch tbl := L.Get(2).(type) {
	case *lua.LTable:
		switch ht := tbl.RawGetString("headers").(type) {
		case *lua.LTable:
			o.headers = make(map[string]string)
			bad := false
			ht.ForEach(func(k, v lua.LValue) {
				name, kok := k.(lua.LString)
				value, vok := v.(lua.LString)
				if !kok || !vok {
					bad = true
					return
				}
				o.headers[string(name)] = string(value)
			})
			if bad {
				raiseError(L, CodeEINVAL, "nu.ws.connect: opts.headers debe ser una tabla de string→string", lua.LNil)
				return "", o, false
			}
		case *lua.LNilType, nil:
			// sin headers
		default:
			raiseError(L, CodeEINVAL, "nu.ws.connect: opts.headers debe ser una tabla", lua.LNil)
			return "", o, false
		}

		switch tm := tbl.RawGetString("timeout_ms").(type) {
		case lua.LNumber:
			if tm <= 0 {
				raiseError(L, CodeEINVAL, "nu.ws.connect: opts.timeout_ms debe ser positivo", lua.LNil)
				return "", o, false
			}
			o.timeout = time.Duration(tm) * time.Millisecond
		case *lua.LNilType, nil:
			// default
		default:
			raiseError(L, CodeEINVAL, "nu.ws.connect: opts.timeout_ms debe ser un número", lua.LNil)
			return "", o, false
		}
	case *lua.LNilType, nil:
		// sin opts
	default:
		raiseError(L, CodeEINVAL, "nu.ws.connect: opts debe ser una tabla", lua.LNil)
		return "", o, false
	}

	return string(urlVal), o, true
}

// dialWs hace el handshake **fuera del token** (lo llama la goroutine de fondo de
// `connect`) y devuelve el handle con la conexión ya establecida. El `timeout_ms`
// cubre SOLO el handshake (vía un contexto con plazo que se desecha al conectar);
// la conexión en sí vive bajo un contexto cancelable sin plazo (un websocket es de
// larga duración). Un fallo del handshake → `ENET`, su timeout → `ETIMEOUT`. El
// mapeo reusa `classifyTransportError` de S19.
func (rt *Runtime) dialWs(url string, o wsOpts) (*luaWs, error) {
	// El contexto que vive con la conexión: cancelable, sin plazo. `close()` lo
	// cancela para abortar cualquier IO colgado.
	connCtx, connCancel := context.WithCancel(context.Background())

	// El contexto del handshake: el de la conexión + el plazo del `timeout_ms`. Se
	// usa solo para `Dial`; al volver, el plazo ya no aplica al `Read`/`Write`.
	dialCtx, dialCancel := context.WithTimeout(connCtx, o.timeout)
	defer dialCancel()

	var dopts *websocket.DialOptions
	if len(o.headers) > 0 {
		h := make(map[string][]string, len(o.headers))
		for name, value := range o.headers {
			h[name] = []string{value}
		}
		dopts = &websocket.DialOptions{HTTPHeader: h}
	}

	conn, _, err := websocket.Dial(dialCtx, url, dopts)
	if err != nil {
		connCancel()
		// El handshake falló: distingue timeout (`ETIMEOUT`) de transporte (`ENET`).
		// `dialCtx.Err()` es `DeadlineExceeded` si el plazo expiró.
		return nil, classifyTransportError(dialCtx, err)
	}
	conn.SetReadLimit(wsReadLimit)

	return &luaWs{
		s:      rt.sched,
		conn:   conn,
		ctx:    connCtx,
		cancel: connCancel,
	}, nil
}

// --- métodos del tipo Ws ------------------------------------------------------

// wsSend implementa `Ws:send(data)` ⏸ (§8): envía `data` como un mensaje de
// **texto** (el caso por defecto del contrato; el provider habla JSON sobre texto).
// La escritura bloqueante (que puede esperar por backpressure de la red) va en la
// goroutine de fondo del puente ⏸. Tras `close`, enviar lanza `ECLOSED`.
func (rt *Runtime) wsSend(L *lua.LState) int {
	if !rt.requireTask(L, "Ws:send") {
		return 0
	}
	w := checkWs(L)
	if w == nil {
		return 0
	}
	data := []byte(L.CheckString(2))

	vals := rt.sched.suspend(L, func() deliverFn {
		err := w.send(data)
		return func(L *lua.LState) []lua.LValue {
			if err != nil {
				raiseWsError(L, err, "Ws:send")
			}
			return nil
		}
	})
	return pushAll(L, vals)
}

// send escribe un mensaje de texto **fuera del token** (lo llama la goroutine de
// fondo de `wsSend`). Si el handle ya se cerró, devuelve `errWsClosed` (→ `ECLOSED`)
// sin tocar la conexión. Un fallo del `Write` real (conexión rota) es transporte
// (`ENET`).
func (w *luaWs) send(data []byte) error {
	w.mu.Lock()
	closed := w.closed
	w.mu.Unlock()
	if closed {
		return errWsClosed
	}
	err := w.conn.Write(w.ctx, websocket.MessageText, data)
	if err != nil {
		// Si nosotros lo cerramos mientras escribíamos, es cierre, no error de red.
		w.mu.Lock()
		closed := w.closed
		w.mu.Unlock()
		if closed {
			return errWsClosed
		}
		return classifyTransportError(w.ctx, err)
	}
	return nil
}

// wsRecv implementa `Ws:recv() -> string?` ⏸ (§8): recibe el siguiente mensaje y lo
// devuelve como string; **`nil` cuando la conexión se cierra** (ordenadamente o por
// `Ws:close()`). Un fallo de transporte real (conexión rota a media) lanza `ENET`.
// La lectura bloqueante va en la goroutine de fondo del puente ⏸.
func (rt *Runtime) wsRecv(L *lua.LState) int {
	if !rt.requireTask(L, "Ws:recv") {
		return 0
	}
	w := checkWs(L)
	if w == nil {
		return 0
	}

	vals := rt.sched.suspend(L, func() deliverFn {
		data, closed, err := w.recv()
		return func(L *lua.LState) []lua.LValue {
			if err != nil {
				raiseWsError(L, err, "Ws:recv")
				return nil
			}
			if closed {
				return []lua.LValue{lua.LNil} // conexión cerrada: fin del stream
			}
			return []lua.LValue{lua.LString(data)}
		}
	})
	return pushAll(L, vals)
}

// recv lee el siguiente mensaje **fuera del token** (lo llama la goroutine de fondo
// de `wsRecv`). Devuelve:
//   - `(data, false, nil)` con un mensaje recibido,
//   - `(nil, true, nil)` cuando la conexión se cerró —ordenadamente (la otra punta
//     mandó un cierre normal) o porque nosotros llamamos `close()`— (→ `recv` da `nil`),
//   - `(nil, false, err)` ante un fallo de transporte real (→ `ENET`).
//
// El criterio que distingue "cierre → nil" de "error → lanza" es
// `websocket.CloseStatus(err)`: un cierre normal/going-away rinde fin de stream; un
// `Read` abortado por nuestro propio `close()` también (la conexión se cerró a
// propósito); cualquier otro error es transporte.
func (w *luaWs) recv() ([]byte, bool, error) {
	w.mu.Lock()
	closed := w.closed
	w.mu.Unlock()
	if closed {
		return nil, true, nil // ya cerrado: fin de stream
	}

	_, data, err := w.conn.Read(w.ctx)
	if err != nil {
		// ¿Lo cerramos nosotros mientras leíamos? Entonces es fin de stream, no error.
		w.mu.Lock()
		closed := w.closed
		w.mu.Unlock()
		if closed {
			return nil, true, nil
		}
		// Cierre ordenado de la otra punta (frame de cierre): fin de stream → nil.
		// Tras detectarlo se marca el handle como cerrado (vía `close`, que es
		// idempotente y desregistra del rastreo): la conexión ya no sirve, y un `recv`
		// posterior debe seguir dando `nil` (no reintentar un `Read` que ya falla con un
		// error distinto, no clasificable como cierre normal).
		if isWsNormalClose(err) {
			w.close()
			return nil, true, nil
		}
		// Cualquier otro fallo de lectura: transporte.
		return nil, false, classifyTransportError(w.ctx, err)
	}
	return data, false, nil
}

// isWsNormalClose informa de si `err` es un cierre de conexión que el contrato trata
// como **fin de stream** (→ `recv` da `nil`), no como error. Cubre los frames de
// cierre normales (1000 NormalClosure, 1001 GoingAway) y también un cierre sin
// frame (`StatusNoStatusRcvd`, 1005: la otra punta cortó sin código), que en la
// práctica es "se acabó". Un cierre con código de error (p. ej. 1011 InternalError)
// NO entra aquí: eso es un fallo y se rinde como `ENET`.
func isWsNormalClose(err error) bool {
	switch websocket.CloseStatus(err) {
	case websocket.StatusNormalClosure, websocket.StatusGoingAway, websocket.StatusNoStatusRcvd:
		return true
	default:
		return false
	}
}

// errWsClosed lo devuelve `send` cuando el handle ya se cerró: `wsSend` lo rinde
// como `ECLOSED`.
var errWsClosed = errors.New("nu.ws: conexión cerrada")

// raiseWsError lanza el error de un `send`/`recv` hacia Lua: un cierre (`ECLOSED`)
// o un `httpError` ya clasificado (`ENET`/`ETIMEOUT`).
func raiseWsError(L *lua.LState, err error, fn string) {
	if errors.Is(err, errWsClosed) {
		raiseError(L, CodeECLOSED, fn+": la conexión fue cerrada", lua.LNil)
		return
	}
	raiseHTTPError(L, err)
}

// wsClose implementa `Ws:close()` (§8): cierra la conexión. **No es ⏸** (cerrar es
// inmediato) e **idempotente** (`closeOnce`). Lo llaman `Ws:close`, el `cleanup` de
// quien lo abrió y `Runtime.Close` (vía `stopAllWs`).
func (rt *Runtime) wsClose(L *lua.LState) int {
	w := checkWs(L)
	if w == nil {
		return 0
	}
	w.close()
	return 0
}

// close cierra la conexión y libera recursos (§8). Idempotente. Marca `closed` (para
// que un `send`/`recv` concurrente sepa que el cierre es a propósito), manda un frame
// de cierre normal (best-effort) y cancela el contexto —lo que desbloquea cualquier
// `Read`/`Write` colgado en una goroutine de fondo, que verá `closed` y rendirá fin
// de stream/`ECLOSED`—. Se desregistra del rastreo del scheduler.
func (w *luaWs) close() {
	w.closeOnce.Do(func() {
		w.mu.Lock()
		w.closed = true
		w.mu.Unlock()
		// Cierre limpio best-effort (manda el frame de cierre). Si falla (la conexión ya
		// está rota), `cancel` igual libera los recursos.
		_ = w.conn.Close(websocket.StatusNormalClosure, "")
		if w.cancel != nil {
			w.cancel()
		}
		// El rastreo del scheduler (para `Runtime.Close` → `stopAllWs`) es del backend
		// gopher; el backend wasm (M13b) reusa este handle VM-agnóstico con `s == nil`
		// (su ciclo de vida a nivel de Runtime lo cablea M13d), así que se guarda el nil.
		if w.s != nil {
			w.s.untrackWs(w)
		}
	})
}
