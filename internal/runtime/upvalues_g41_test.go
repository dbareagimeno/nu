package runtime

// Tests de G41: un error CAPTURADO no debe cerrar upvalues de frames vivos.
//
// El bug (aguas arriba, gopher-lua v1.1.2): `raiseError` con
// `hasErrorFunc == false` ejecuta `closeAllUpvalues()` sobre TODA la pila del
// thread — también los frames por debajo del pcall que captura, que siguen
// vivos. Una closure que capturó un local vivo queda escribiendo/leyendo una
// celda desanclada mientras el dueño usa el registry: las escrituras "se
// pierden" en silencio. Lua estándar solo cierra los frames desenrollados.
//
// El blindaje (cancel.go, G41): `hasErrorFunc` se mantiene ARMADO mientras
// haya un pcall/xpcall envuelto activo (contador de profundidad por thread),
// de modo que el camino de raiseError sea el mismo que upstream usa con
// message handler (que no sobre-cierra). Estos tests blindan:
//   - la repro mínima (síncrona, sin tasks);
//   - pcalls ANIDADOS (el flag de upstream no es una pila; el contador sí);
//   - el caso real que la destapó: un handler de eventos escribiendo en el
//     upvalue de una task suspendida tras un error capturado previo;
//   - que los abortos (§1.3) siguen siendo NO capturables (la inmunidad de
//     S08 no se ve afectada por el re-armado del flag).

import (
	"testing"
)

// TestG41ReproMinima: la semántica estándar de Lua — el error capturado no
// desancla el upvalue de un frame vivo.
func TestG41ReproMinima(t *testing.T) {
	h := newHarness(t)
	h.expectEval(`
		local X = nil
		local set = function(v) X = v end   -- closure con upvalue X (abierto)
		pcall(function() error("boom") end) -- error capturado MÁS ADENTRO
		set(42)
		return tostring(X)`, "42")
}

// TestG41PcallsAnidados: el agujero del flag no-apilado de upstream — un pcall
// interno termina, y un error posterior capturado por el EXTERNO tampoco debe
// desanclar. También xpcall.
func TestG41PcallsAnidados(t *testing.T) {
	h := newHarness(t)
	h.expectEval(`
		local out
		pcall(function()
			local X = nil
			local set = function(v) X = v end
			pcall(function() end)               -- pcall interno termina (resetea el flag upstream)
			pcall(function() error("boom") end) -- error capturado después
			set(7)
			out = X
		end)
		return tostring(out)`, "7")
	h.expectEval(`
		local X = nil
		local set = function(v) X = v end
		xpcall(function() error("boom") end, function(e) return e end)
		set(9)
		return tostring(X)`, "9")
}

// TestG41HandlerYTaskSuspendida: el caso que destapó la grieta (tests de G40).
// Una task suspendida dentro de un pcall, con un error capturado previo en el
// mismo thread; un handler de eventos escribe en el upvalue LOCAL durante la
// suspensión — y al reanudar, la escritura SE VE.
func TestG41HandlerYTaskSuspendida(t *testing.T) {
	h := newHarness(t)
	h.eval(`
		out = nil
		nu.task.spawn(function()
			pcall(function()
				local EV = nil
				nu.events.on("g41:ping", function(p) EV = p end)
				pcall(function() error("ruido previo") end)  -- el detonante original
				local fut = nu.task.future()
				nu.task.spawn(function()
					nu.events.emit("g41:ping", { hola = 1 })
					fut:set(true)
				end)
				fut:await()                                   -- ⏸ suspendida bajo pcall
				out = EV and "capturado" or "perdido"
			end)
		end)
	`)
	h.expectEval(`return tostring(out)`, "capturado")
}

// TestG41AbortSigueNoCapturable: el re-armado del flag NO debilita §1.3 — un
// Task:cancel sigue atravesando el pcall del usuario sin ser capturado.
func TestG41AbortSigueNoCapturable(t *testing.T) {
	h := newHarness(t)
	h.eval(`
		out = "sin-tocar"
		local tsk
		tsk = nu.task.spawn(function()
			pcall(function()
				nu.task.sleep(60000)      -- ⏸ punto de suspensión: aquí muerde cancel
				out = "revivio-dentro-del-pcall" -- NO debe ejecutarse
			end)
			out = "revivio-tras-el-pcall"       -- NO debe ejecutarse (aborto no capturable)
		end)
		nu.task.spawn(function() tsk:cancel() end)
	`)
	h.expectEval(`return out`, "sin-tocar")
}

// TestG41FronteraDeCierre: el cierre en el vuelo del pánico (trampolín) respeta
// la frontera — lo VIVO por debajo del pcall sigue enlazado a su local; lo
// creado en el tramo DESENROLLADO queda cerrado con su valor correcto (no el
// nil del registry ya truncado), y la caché no lo realía con locals nuevos.
func TestG41FronteraDeCierre(t *testing.T) {
	h := newHarness(t)
	h.expectEval(`
		local X = "antes"
		local get = function() return X end
		pcall(function() error("boom") end)
		X = "despues"
		return get()`, "despues")
	h.expectEval(`
		local guardada
		pcall(function()
			local Y = "capturado-en-frame-muerto"
			guardada = function() return Y end
			error("boom")
		end)
		local basura = "local-nuevo-que-reusa-el-slot"
		local mas = "mas-basura"
		return guardada()`, "capturado-en-frame-muerto")
}

// TestG41AbortConCleanup: la interacción con el cierre de S16 — una task
// abortada DENTRO de un pcall (el flag de G41 armado) debe seguir cerrando
// TODOS sus upvalues antes de desenrollar, para que sus cleanups vean las
// capturas intactas (aquí, el future que resuelve). Es exactamente el patrón
// del turno del agente (TestSessionCancel lo cubre de punta a punta; este es
// el mínimo aislado).
func TestG41AbortConCleanup(t *testing.T) {
	h := newHarness(t)
	h.eval(`
		out = "nada"
		nu.task.spawn(function()
			local fut = nu.task.future()
			local worker = nu.task.spawn(function()
				pcall(function()
					nu.task.cleanup(function() fut:set("limpio") end)
					nu.task.sleep(60000)
				end)
				out = "sobrevivio-al-pcall" -- NO debe ejecutarse (aborto no capturable)
			end)
			nu.task.spawn(function() worker:cancel() end)
			out = fut:await()
		end)
	`)
	h.expectEval(`return tostring(out)`, "limpio")
}
