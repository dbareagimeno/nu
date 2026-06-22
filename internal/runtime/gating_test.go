package runtime

// Tests del GATING HEADLESS de `nu.ui` (G20, §9, S32) y de la superficie de S32
// (clipboard OSC 52, eventos `ui:*`). Blindan el "Criterio de hecho" de la sesión:
//
//   - **Gating (G20)**: con la UI desactivada (simula headless: `WithForceUI(false)`),
//     el módulo `nu.ui` es **inexistente** desde Lua (`nu.ui == nil`) y
//     `nu.has("ui")` es **false**. Con la UI forzada (test), `nu.ui` existe y
//     `nu.has("ui")` es **true**. Es el criterio "bajo `nu -e` nu.ui inexistente".
//   - **clipboard_set OSC 52**: produce la secuencia OSC 52 correcta (bytes emitidos
//     al "terminal" inyectado).
//   - **clipboard_get**: sin driver de TTY (headless) devuelve `nil` (el parseo se
//     blinda en `osc52_test.go`).
//   - **eventos `ui:*`**: `ui:resize` se emite al cambiar el tamaño; `ui:focus`/
//     `ui:suspend`/`ui:resume` se emiten por sus vías inyectables.

import (
	"bytes"
	"testing"
)

// TestGatingHeadlessNoUI blinda G20: con la UI desactivada (headless), `nu.ui` no
// existe y `nu.has("ui")` es false. El bus (`ui:` es del core) sigue disponible.
func TestGatingHeadlessNoUI(t *testing.T) {
	rt := New(WithDataDir(t.TempDir()), WithForceUI(false))
	defer rt.Close()
	h := &harness{t: t, rt: rt}

	// `nu.ui` directamente no está en el global `nu` (no es probar-y-capturar: es nil).
	h.expectEval(`return tostring(nu.ui == nil)`, "true")
	// `nu.has("ui")` es false (deny-by-default, coherente con que el módulo no exista).
	h.expectEval(`return tostring(nu.has("ui"))`, "false")
	// El compositor tampoco se construyó (caja blanca): rt.ui es nil.
	if rt.ui != nil {
		t.Fatal("headless: rt.ui debería ser nil (sin compositor)")
	}
	// `nu.has` de una cap desconocida sigue siendo false.
	h.expectEval(`return tostring(nu.has("inexistente"))`, "false")
}

// TestGatingForcedUI blinda el otro lado de G20: con la UI forzada (lo que hacen los
// tests), `nu.ui` existe y `nu.has("ui")` es true.
func TestGatingForcedUI(t *testing.T) {
	h := newHarness(t) // newHarness fuerza la UI (WithForceUI(true))
	h.expectEval(`return tostring(nu.ui ~= nil)`, "true")
	h.expectEval(`return tostring(nu.has("ui"))`, "true")
	// ui.images sigue false: el protocolo de imágenes no se ha negociado (driver S33+).
	h.expectEval(`return tostring(nu.has("ui.images"))`, "false")
}

// TestClipboardSetOSC52 blinda que `nu.ui.clipboard_set` emite la secuencia OSC 52
// correcta al terminal. Inyecta un buffer como destino (`clipWriter`) para inspeccionar
// los bytes exactos —en producción ese destino es el TTY (os.Stdout)—.
func TestClipboardSetOSC52(t *testing.T) {
	h := newHarness(t)
	var buf bytes.Buffer
	h.rt.ui.clipWriter = &buf

	h.eval(`nu.ui.clipboard_set("hola mundo")`)

	want := encodeOSC52Set("hola mundo")
	if got := buf.String(); got != want {
		t.Fatalf("clipboard_set emitió %q, quería %q", got, want)
	}
}

// TestClipboardGetHeadless blinda que `nu.ui.clipboard_get` (⏸) devuelve nil cuando no
// hay driver de TTY del que leer la respuesta (este entorno headless con `clipReader`
// nil). Además comprueba que SÍ escribió la consulta OSC 52 al terminal.
func TestClipboardGetHeadless(t *testing.T) {
	h := newHarness(t)
	if err := h.rt.Boot(); err != nil {
		t.Fatalf("Boot falló: %v", err)
	}
	var buf bytes.Buffer
	h.rt.ui.clipWriter = &buf
	// clipReader es nil (sin driver): la lectura resuelve a nil de inmediato.

	h.eval(`
		got = "SENTINEL"
		nu.task.spawn(function()
			got = nu.ui.clipboard_get()
		end)
	`)
	// La task corrió y resolvió a nil (got pasó de "SENTINEL" a nil).
	h.expectEval(`return tostring(got)`, "nil")
	// La consulta OSC 52 sí se envió al terminal.
	if got := buf.String(); got != encodeOSC52Query() {
		t.Fatalf("clipboard_get debió enviar la consulta %q, envió %q", encodeOSC52Query(), got)
	}
}

// TestUIResizeEvent blinda que cambiar el tamaño de la pantalla emite `ui:resize` con
// `{w, h}` (§9.1: "cambios → evento ui:resize") y actualiza `nu.ui.size()`. Un resize
// al MISMO tamaño no emite un evento espurio.
func TestUIResizeEvent(t *testing.T) {
	h := newHarnessUI(t, 80, 24)
	h.eval(`
		rw, rh, count = nil, nil, 0
		nu.events.on("ui:resize", function(ev) count = count + 1; rw, rh = ev.w, ev.h end)
	`)

	// Inyecta un cambio de tamaño (lo que el driver de TTY haría ante un SIGWINCH).
	h.rt.resizeUI(100, 40)
	h.expectEval(`return tostring(rw), tostring(rh), tostring(count)`, "100", "40", "1")
	// El tamaño visible por Lua se actualizó.
	h.expectEval(`local s = nu.ui.size(); return s.w, s.h`, "100", "40")

	// Un resize al mismo tamaño NO emite otro evento.
	h.rt.resizeUI(100, 40)
	h.expectEval(`return tostring(count)`, "1")
}

// TestUIFocusSuspendResumeEvents blinda las vías de emisión de `ui:focus`,
// `ui:suspend` y `ui:resume` (esqueleto de S32; el driver real los disparará en S33+).
func TestUIFocusSuspendResumeEvents(t *testing.T) {
	h := newHarnessUI(t, 20, 5)
	h.eval(`
		focused, suspended, resumed = nil, 0, 0
		nu.events.on("ui:focus", function(ev) focused = ev.focused end)
		nu.events.on("ui:suspend", function() suspended = suspended + 1 end)
		nu.events.on("ui:resume", function() resumed = resumed + 1 end)
	`)

	h.rt.emitUIFocus(true)
	h.expectEval(`return tostring(focused)`, "true")
	h.rt.emitUIFocus(false)
	h.expectEval(`return tostring(focused)`, "false")

	h.rt.emitUISuspend()
	h.rt.emitUIResume()
	h.rt.emitUISuspend()
	h.expectEval(`return tostring(suspended), tostring(resumed)`, "2", "1")
}

// TestUIClipboardLuaSurface es el snippet del lado del autor de extensiones (DoD §2):
// comprueba que `nu.has("ui")` es true y que las firmas del portapapeles existen y se
// invocan sin error desde Lua (set síncrono; get desde una task).
func TestUIClipboardLuaSurface(t *testing.T) {
	h := newHarness(t)
	if err := h.rt.Boot(); err != nil {
		t.Fatalf("Boot falló: %v", err)
	}
	var buf bytes.Buffer
	h.rt.ui.clipWriter = &buf

	h.expectEval(`
		assert(nu.has("ui"), "nu.has('ui') debe ser true con UI activa")
		assert(type(nu.ui.clipboard_set) == "function", "clipboard_set existe")
		assert(type(nu.ui.clipboard_get) == "function", "clipboard_get existe")
		nu.ui.clipboard_set("x")
		return "ok"
	`, "ok")
}
