package runtime

import (
	"reflect"
	"sync"
	"unsafe"

	lua "github.com/yuin/gopher-lua"
)

// Desenrollado NO capturable por `pcall` (api.md §1.3, sesión S08, inventario
// 🔒). Esta es la pieza que hace que la **cancelación** (`Task:cancel`, S08) y el
// **watchdog** (S09) aborten una task atravesando cualquier `pcall`/`xpcall` del
// usuario sin que estos lo atrapen —"si fueran errores normales, cualquier pcall
// los capturaría y el programa seguiría como si nada" (§1.3)—.
//
// EL PROBLEMA. El aborto se realiza con un **pánico Go** (el centinela
// `abortSignal`, ver scheduler.go) que desenrolla la pila de la goroutine de la
// task. Pero gopher-lua implementa `pcall`/`xpcall` en Go con un `recover()`
// (`LState.PCall`): recupera **cualquier** pánico Go —es el mismo motivo de
// ADR-011— y lo entrega a Lua como `false, err`. Por tanto, un `pcall` de usuario
// que envuelva un punto de suspensión atraparía el aborto. Inaceptable: §1.3
// exige que NO sea capturable.
//
// LA SOLUCIÓN (la técnica conocida del wrapper). Reemplazamos los globales
// `pcall` y `xpcall` —que el baseline de S01 controla (sandbox.go)— por versiones
// Go propias que delegan en el `pcall` nativo (`LState.PCall`) y, **si la task en
// curso está abortando** (`task.aborting`, puesto por `scheduler.abort` justo
// antes de lanzar el centinela), **re-lanzan** el centinela en vez de devolver
// `false, err` a Lua. Así el aborto "se cuela" por cada frontera `pcall`/`xpcall`
// hasta el `CallByParam` de `runTask`, que es quien legítimamente lo recupera,
// corre los `cleanup` y descarta el desenlace.
//
// POR QUÉ `task.aborting` Y NO EL VALOR DEL PÁNICO. Al cruzar `LState.PCall`, un
// pánico que no sea `*lua.ApiError` se convierte en un `*ApiError` con su mensaje
// vía `fmt.Sprint` —se pierde el tipo Go `abortSignal`—, así que detectar el
// aborto por el valor recuperado sería frágil. En cambio `aborting` es un flag de
// la propia task, escrito y leído por su única goroutine bajo el token: detección
// robusta e independiente de cómo gopher-lua represente el pánico. Sale gratis el
// re-lanzado idéntico: reconstruimos `abortSignal{t: t}` a partir de la task.
//
// LOS ERRORES NORMALES SIGUEN CAPTURÁNDOSE. Si la task NO está abortando, estos
// envoltorios se comportan EXACTAMENTE como los nativos: devuelven `false, err`
// para cualquier error de §1.4 (un `EINVAL`, un `error("texto")`, etc.). Solo el
// aborto —y solo mientras se está desenrollando— es inmune. Es decir: no rompemos
// `pcall` (§1.4), solo lo blindamos contra el aborto (§1.3).
//
// DÓNDE SE ATERRIZA. El chunk principal y los handlers síncronos corren sobre
// `host` (sin task en `coToTask`): ahí `aborting` nunca aplica, así que `pcall`
// se comporta como el nativo —no hay aborto que filtrar fuera de una task—. Los
// `cleanup` corren con `aborting` ya bajado (`runCleanups`), de modo que un
// `pcall` dentro de un cleanup vuelve a capturar con normalidad.

// installCancelPcall reemplaza los globales `pcall` y `xpcall` por las versiones
// envueltas. Lo llama `registerNu` tras `applySandbox` (que abre el baselib con
// los `pcall`/`xpcall` nativos): aquí los sustituimos. La superficie pública no
// cambia —siguen siendo `pcall(fn, ...)` y `xpcall(fn, errfn)` con su semántica
// de §1.4—; lo único que añadimos es la inmunidad al aborto de §1.3 y el
// blindaje de upvalues de G41 (abajo).
func (s *scheduler) installCancelPcall() {
	L := s.host
	tramp := L.NewFunction(s.protectedTrampoline) // el frame interpuesto de G41
	L.SetGlobal("pcall", L.NewFunction(func(L *lua.LState) int {
		return s.protectedPCall(L, tramp)
	}))
	L.SetGlobal("xpcall", L.NewFunction(func(L *lua.LState) int {
		return s.protectedXPCall(L, tramp)
	}))
}

// ---------------------------------------------------------------------------
// G41: un error capturado NO debe cerrar upvalues de frames VIVOS.
//
// EL BUG (aguas arriba, gopher-lua v1.1.2). `raiseError` —el camino de todo
// `error()` de Lua y de los errores estructurados de las primitivas— ejecuta
// `closeAllUpvalues()` cuando `hasErrorFunc == false`, y ese cierre recorre
// TODA la pila del thread: cierra también los upvalues de frames por DEBAJO
// del `pcall` que va a capturar el error, frames que siguen vivos. Lua
// estándar solo cierra los frames desenrollados (luaF_close hasta el nivel
// del pcall). Consecuencia observable (la repro de G41): tras un
// `pcall(function() error("x") end)`, una closure previa que capturó un local
// aún vivo escribe en una celda DESANCLADA (`uv.value`) mientras el dueño lee
// su local del registry — la escritura "se pierde". Con el scheduler de
// ADR-011 esto muerde fuerte: un handler de eventos que escribe en un upvalue
// de una task suspendida deja de verse si CUALQUIER error fue capturado antes
// en ese thread (p. ej. el pcall(nu.fs.read) de un agent.toml ausente).
//
// LA SOLUCIÓN. gopher-lua ya tiene un modo que NO sobre-cierra: cuando hay un
// message handler (`hasErrorFunc == true`, el camino de xpcall). Como TODOS
// los `pcall`/`xpcall` del runtime pasan por nuestros envoltorios (arriba),
// mantenemos `hasErrorFunc` ARMADO exactamente mientras haya al menos un
// pcall nuestro activo en el thread (contador de profundidad por *LState*).
// El flag es un campo no exportado: se escribe vía reflect+unsafe con el
// offset calculado en init() — si un upgrade de gopher-lua renombra el campo,
// el init panica en el arranque (fallo ruidoso, jamás silencioso) y los tests
// de G41 lo delatan. El propio `LState.PCall` baja el flag a false en su
// defer de salida; por eso se re-arma tras CADA PCall interno y al salir del
// envoltorio se restaura según la profundidad restante (cierra el agujero de
// pcalls anidados: el flag de upstream no es una pila, el contador sí).
//
// LA SEGUNDA MITAD (imprescindible). Saltarse `closeAllUpvalues` protege los
// upvalues VIVOS, pero deja sin cerrar los de los frames DESENROLLADOS por el
// error — y eso corrompe: la caché de upvalues del thread (`uvcache`) conserva
// entradas con índices ya liberados, que se REALÍAN con locals nuevos de
// llamadas posteriores en los mismos índices (dos closures sin relación acaban
// compartiendo celda; en la práctica: futures del agente cruzados y deadlock —
// lo delató TestSessionCancel bajo -race). Lua estándar cierra EXACTAMENTE los
// frames desenrollados (`luaF_close` hasta el nivel del pcall): eso hace
// `closeUnwoundUpvalues`. Y el CUÁNDO es tan importante como el qué: hay que
// cerrar EN EL VUELO DEL PÁNICO, mientras los slots del registry aún tienen
// sus valores — la recuperación de `LState.PCall` los trunca (`reg.SetTop`),
// y un cierre posterior solo snapshotearía nils (se comprobó: corrompe con
// LValues nil-interface que revientan el VM). Por eso el envoltorio no llama
// a la función directamente: la llama a través de un TRAMPOLÍN Go cuyo defer
// recupera el pánico ANTES que el de PCall (está más adentro en la pila),
// cierra lo desenrollado con los slots aún vivos y re-lanza. Vale para los
// errores normales Y para los abortos (§1.3), que siguen su viaje no
// capturable igual que siempre — solo que dejando la caché limpia.
//
// LO QUE NO CAMBIA. Con profundidad 0 (error sin pcall: la task muere), el
// comportamiento es el de upstream — el cierre-de-todo en la agonía del thread
// es inofensivo.
// ---------------------------------------------------------------------------

// Offsets de los campos no exportados de gopher-lua que el blindaje necesita
// tocar. Se calculan con reflect UNA vez y fallan en el ARRANQUE (nunca en
// silencio en producción) si un upgrade de la dependencia los mueve o
// renombra; los tests de G41 (upvalues_g41_test.go) los delatan igualmente.
var (
	offHasErrorFunc uintptr // LState.hasErrorFunc (bool)
	offLReg         uintptr // LState.reg (*registry)
	offLUvcache     uintptr // LState.uvcache (*Upvalue)
	offRegTop       uintptr // registry.top (int)
	offUvNext       uintptr // Upvalue.next (*Upvalue)
	offUvIndex      uintptr // Upvalue.index (int)
)

func init() {
	must := func(t reflect.Type, name string, kind reflect.Kind) reflect.StructField {
		f, ok := t.FieldByName(name)
		if !ok || f.Type.Kind() != kind {
			panic("gopher-lua: el campo " + t.String() + "." + name +
				" cambió; revisa el blindaje de G41 (cancel.go)")
		}
		return f
	}
	lt := reflect.TypeOf(lua.LState{})
	offHasErrorFunc = must(lt, "hasErrorFunc", reflect.Bool).Offset
	fReg := must(lt, "reg", reflect.Ptr)
	offLReg = fReg.Offset
	offRegTop = must(fReg.Type.Elem(), "top", reflect.Int).Offset
	offLUvcache = must(lt, "uvcache", reflect.Ptr).Offset
	ut := reflect.TypeOf(lua.Upvalue{})
	offUvNext = must(ut, "next", reflect.Ptr).Offset
	offUvIndex = must(ut, "index", reflect.Int).Offset
}

func setHasErrorFunc(L *lua.LState, v bool) {
	*(*bool)(unsafe.Pointer(uintptr(unsafe.Pointer(L)) + offHasErrorFunc)) = v
}

// regTopOf lee el top ABSOLUTO del registry del thread (gopher-lua no lo
// expone; L.GetTop() es relativo al frame). Es la frontera que PCall usa para
// restaurar en la recuperación (`base = reg.Top() - nargs - 1`).
func regTopOf(L *lua.LState) int {
	reg := *(*unsafe.Pointer)(unsafe.Pointer(uintptr(unsafe.Pointer(L)) + offLReg))
	return *(*int)(unsafe.Pointer(uintptr(reg) + offRegTop))
}

// closeUnwoundUpvalues cierra los upvalues abiertos con índice >= base y los
// retira de la caché del thread — la semántica de `luaF_close(L, base)` de Lua
// estándar aplicada al tramo DESENROLLADO por un error recuperado. Sin esto,
// la caché conserva entradas de frames muertos que se realían con locals
// nuevos en los mismos índices (ver el bloque G41 arriba). A diferencia del
// `closeUpvalues` de upstream (que trunca la lista al primer índice que
// cumple, asumiéndola ordenada), aquí se filtra elemento a elemento: correcto
// con cualquier orden y estrictamente equivalente cuando está ordenada.
func closeUnwoundUpvalues(L *lua.LState, base int) {
	head := (**lua.Upvalue)(unsafe.Pointer(uintptr(unsafe.Pointer(L)) + offLUvcache))
	var prev *lua.Upvalue
	uv := *head
	for uv != nil {
		next := *(**lua.Upvalue)(unsafe.Pointer(uintptr(unsafe.Pointer(uv)) + offUvNext))
		idx := *(*int)(unsafe.Pointer(uintptr(unsafe.Pointer(uv)) + offUvIndex))
		if idx >= base {
			uv.Close()
			if prev == nil {
				*head = next
			} else {
				*(**lua.Upvalue)(unsafe.Pointer(uintptr(unsafe.Pointer(prev)) + offUvNext)) = next
			}
		} else {
			prev = uv
		}
		uv = next
	}
}

// protectedTrampoline es el frame Go que el envoltorio interpone entre el
// PCall y la función protegida: su defer recupera el pánico ANTES que la
// recuperación de PCall (está más adentro), cierra los upvalues del tramo que
// se está desenrollando —con los slots del registry aún vivos— y re-lanza.
// Recibe [fn, a1..an] y devuelve los resultados de fn tal cual.
func (s *scheduler) protectedTrampoline(L *lua.LState) int {
	nargs := L.GetTop() - 1
	base := regTopOf(L) - nargs - 1 // el slot absoluto de fn: nada vivo por encima tras desenrollar
	defer func() {
		if r := recover(); r != nil {
			closeUnwoundUpvalues(L, base)
			panic(r)
		}
	}()
	L.Call(nargs, lua.MultRet)
	return L.GetTop()
}

// pcallDepth: profundidad de pcalls envueltos por thread (*lua.LState -> int).
// Cada clave la escribe solo la goroutine que ejecuta ese thread; sync.Map da
// la seguridad del mapa entre threads concurrentes (main + workers comparten
// wrapper por scheduler, y los threads efímeros de eventos tienen el suyo).
var pcallDepth sync.Map

// enterProtected/exitProtected arman y desarman el blindaje de G41 alrededor
// de cada pcall/xpcall envuelto. exitProtected va en defer: restaura también
// cuando el aborto (§1.3) atraviesa el envoltorio re-lanzado.
func enterProtected(L *lua.LState) {
	d := 0
	if v, ok := pcallDepth.Load(L); ok {
		d = v.(int)
	}
	pcallDepth.Store(L, d+1)
	setHasErrorFunc(L, true)
}

func exitProtected(L *lua.LState) {
	d := 1
	if v, ok := pcallDepth.Load(L); ok {
		d = v.(int)
	}
	if d <= 1 {
		pcallDepth.Delete(L)
		setHasErrorFunc(L, false)
	} else {
		pcallDepth.Store(L, d-1)
		setHasErrorFunc(L, true)
	}
}

// reraiseIfAborting re-lanza el pánico centinela si la task en curso (la que
// corre sobre `L`) está abortando. Lo invocan `pcall`/`xpcall` envueltos
// **después** de que el `PCall` nativo capturó un error: si ese error es en
// realidad un aborto en curso, no debe entregarse a Lua sino seguir
// desenrollando. Si no hay task, o no está abortando, no hace nada (el error es
// uno normal de §1.4 y se devuelve a Lua como siempre).
//
// Watchdog (S09): el corte por presupuesto entra por una vía distinta a la
// cancelación. La cancelación (S08) ya viene con `t.aborting` puesto (lo hizo
// `abort` en el punto de suspensión); el watchdog, en cambio, solo dejó el flag
// atómico `budgetExceeded` y rompió el bucle por contexto —el error que el `pcall`
// nativo acaba de capturar es el "context canceled" de gopher-lua, aún un error
// normal—. Por eso aquí, si la task no está ya abortando pero el watchdog
// disparó, se **reclama** el aborto (`claimBudgetAbort` pone `aborting`/`reason =
// abortBudget`/`canceled`) para que el re-lanzado del centinela también lo cuele
// no capturable por este `pcall`/`xpcall` y los de más afuera, hasta `runTask`.
func (s *scheduler) reraiseIfAborting(L *lua.LState) {
	t, ok := s.taskOf(L)
	if !ok {
		return
	}
	if !t.aborting {
		s.claimBudgetAbort(t) // watchdog: convierte el corte por contexto en aborto
	}
	if t.aborting {
		// La task entera muere: cierra TODOS los upvalues abiertos antes de seguir
		// desenrollando, para que los cleanups vean sus capturas intactas (el mismo
		// cierre que `abort` hace en los puntos de suspensión; aquí cubre el caso
		// del watchdog reclamado en el propio envoltorio). Idempotente.
		closeUnwoundUpvalues(L, 0)
		panic(abortSignal{t: t})
	}
}

// protectedPCall es la versión envuelta de `pcall(f, ...)` (§1.4 + inmunidad al
// aborto de §1.3). Reproduce `basePCall` de gopher-lua —incluida la comprobación
// de "es llamable"— y, ante un error capturado, consulta si la task está
// abortando: si lo está, re-lanza el centinela (no capturable); si no, devuelve
// `false, err` como el nativo.
func (s *scheduler) protectedPCall(L *lua.LState, tramp *lua.LFunction) int {
	L.CheckAny(1)
	v := L.Get(1)
	if v.Type() != lua.LTFunction && L.GetMetaField(v, "__call").Type() != lua.LTFunction {
		L.Push(lua.LFalse)
		L.Push(lua.LString("attempt to call a " + v.Type().String() + " value"))
		return 2
	}
	enterProtected(L)      // G41: los errores capturados no cierran upvalues vivos
	defer exitProtected(L) // (defer: restaura también si un aborto atraviesa)
	nargs := L.GetTop() - 1
	L.Insert(tramp, 1) // [tramp, fn, a1..an]: fn corre A TRAVÉS del trampolín (G41)
	if err := L.PCall(nargs+1, lua.MultRet, nil); err != nil {
		setHasErrorFunc(L, true) // el defer de PCall lo bajó; seguimos protegidos
		s.reraiseIfAborting(L)   // aborto en curso → re-lanza; no captures
		L.Push(lua.LFalse)
		L.Push(errToLua(err))
		return 2
	}
	setHasErrorFunc(L, true) // ídem: re-armado hasta el exitProtected del defer
	L.Insert(lua.LTrue, 1)
	return L.GetTop()
}

// protectedXPCall es la versión envuelta de `xpcall(f, errfn)` (§1.4 + inmunidad
// al aborto de §1.3). Reproduce `baseXPCall` de gopher-lua. Subraya por qué hace
// falta envolver también `xpcall`: su `errfn` (message handler) correría sobre el
// aborto si no lo filtráramos —el aborto NO debe pasar por el manejador de errores
// del usuario—, así que el re-lanzado se hace ANTES de que `PCall` invoque a
// `errfn`... salvo que gopher-lua ya la invocó dentro de su `PCall`. Como `errfn`
// se ejecuta dentro del `LState.PCall` nativo, para no dejar que toque el aborto
// pasamos `nil` como manejador al `PCall` nativo y aplicamos `errfn` nosotros
// solo si el error NO es un aborto.
func (s *scheduler) protectedXPCall(L *lua.LState, tramp *lua.LFunction) int {
	fn := L.CheckFunction(1)
	errfn := L.CheckFunction(2)

	enterProtected(L) // G41 (ver protectedPCall)
	defer exitProtected(L)
	top := L.GetTop()
	L.Push(tramp) // fn corre a través del trampolín de G41
	L.Push(fn)
	// Manejador nil al `PCall` nativo: queremos decidir nosotros si `errfn` corre.
	// Si corriera dentro del `PCall` nativo, el aborto pasaría por el manejador del
	// usuario antes de que pudiéramos filtrarlo.
	if err := L.PCall(1, lua.MultRet, nil); err != nil {
		setHasErrorFunc(L, true) // el defer de PCall lo bajó; re-arma
		s.reraiseIfAborting(L)   // aborto en curso → re-lanza; ni `errfn` ni captura
		// Error normal de §1.4: aplica el manejador del usuario, como `xpcall`.
		L.Push(tramp) // el manejador también corre a través del trampolín (G41)
		L.Push(errfn)
		L.Push(errToLua(err))
		if hErr := L.PCall(2, 1, nil); hErr != nil {
			setHasErrorFunc(L, true) // re-arma también tras el PCall del manejador
			// El manejador mismo falló o fue abortado: respeta el aborto, y si no,
			// propaga el resultado del manejador como hace el nativo.
			s.reraiseIfAborting(L)
			L.Push(lua.LFalse)
			L.Push(errToLua(hErr))
			return 2
		}
		handlerRet := L.Get(-1)
		L.Pop(1)
		L.Push(lua.LFalse)
		L.Push(handlerRet)
		return 2
	}
	L.Insert(lua.LTrue, top+1)
	return L.GetTop() - top
}

// errToLua extrae el valor Lua que `pcall`/`xpcall` entregan como segundo retorno
// a partir del error de `LState.PCall`: el `Object` de la tabla estructurada
// (§1.4) si lo hay, o el texto del error en otro caso. Es exactamente lo que
// hacen `basePCall`/`baseXPCall` de gopher-lua.
func errToLua(err error) lua.LValue {
	if aerr, ok := err.(*lua.ApiError); ok {
		return aerr.Object
	}
	return lua.LString(err.Error())
}
