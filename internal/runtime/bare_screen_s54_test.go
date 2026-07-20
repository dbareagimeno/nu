package runtime

// Tests 🔒 de la ELECCIÓN POR TECLADO en la pantalla de runtime desnudo (S54;
// api.md §14, G21, ADR-010, ADR-015, ADR-017). Blindan la máquina de estados de la
// elección (menú ↔ selección de catálogo), que vive en Go —no como widget de
// `enu.ui`—: el reenviador `on_input` anota la tecla y el driver la despacha a
// `bareScreen.handleKey` desde `feed`. Los ejes exigidos por el inventario 🔒:
//
//   - RE-ENTRADA: una segunda pulsación de activar con la activación ya iniciada NO
//     dispara otra (doble `enu.toml`/doble `Boot` es el fallo silencioso).
//   - CURSOR ACOTADO a [0, len(catálogo)-1], catálogo VACÍO incluido (sin índice
//     fuera de rango).
//   - la acción 1 activa EXACTAMENTE `officialProductSet` (ADR-015: `example`/`mesh`
//     fuera) y la 2 (enter) activa SOLO la embebida elegida.
//   - FALLO de activación (`enu.toml` malformado → EINVAL sin pisar el fichero,
//     G21/S33): el error accionable se pinta y la SALIDA por teclado sigue viva
//     (ADR-017: la terminal jamás queda atrapada en raw mode).
//
// La máquina se prueba por unidad con un `activate` espía (determinista, sin montar
// el `Boot` real); el fallo end-to-end (toml malformado → error pintado + `q` sale)
// se conduce con el driver y tuberías en memoria, la misma vía que driver_test.go.

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// activateSpy sustituye a `rt.activateAndBoot` en los tests de la máquina: cuenta las
// activaciones y sus argumentos (para blindar la re-entrada y "activa exactamente
// X"), y puede simular un fallo (`err`) sin escribir `enu.toml` ni arrancar nada.
type activateSpy struct {
	calls [][]string
	err   error
}

func (s *activateSpy) fn(names []string) error {
	s.calls = append(s.calls, append([]string(nil), names...))
	return s.err
}

// bareWithSpy construye un `bareScreen` con un catálogo CONTROLADO y la activación
// espiada, sobre un Runtime con UI (para que `render` —side effect de handleKey—
// tenga compositor). Devuelve también el rt (handleKey lo necesita).
func bareWithSpy(t *testing.T, catalog []string) (*bareScreen, *activateSpy, *Runtime) {
	t.Helper()
	rt, _ := newBareRuntime(t, WithUISize(80, 24))
	spy := &activateSpy{}
	bs := &bareScreen{
		model:    rt.buildBareScreenModel(),
		mode:     bareMenu,
		catalog:  catalog,
		activate: spy.fn,
	}
	return bs, spy, rt
}

// TestBareKeyActivateOfficialS54: la acción `1` activa EXACTAMENTE el conjunto oficial
// de producto (ADR-015: `example`/`mesh` fuera) y marca la pantalla como hecha.
func TestBareKeyActivateOfficialS54(t *testing.T) {
	t.Run("dado_menu_cuando_pulsa_1_entonces_activa_exactamente_el_conjunto_oficial", func(t *testing.T) {
		bs, spy, rt := bareWithSpy(t, []string{"example", "repl", "agent", "mesh"})

		if act := bs.handleKey("1", rt); act != bareNone {
			t.Fatalf("acción tras '1' = %v, want bareNone", act)
		}
		if len(spy.calls) != 1 {
			t.Fatalf("activate llamado %d veces, want 1", len(spy.calls))
		}
		want, err := officialProductSet()
		if err != nil {
			t.Fatalf("officialProductSet: %v", err)
		}
		if !reflect.DeepEqual(spy.calls[0], want) {
			t.Fatalf("la acción 1 activó %v, want EXACTAMENTE el conjunto oficial %v", spy.calls[0], want)
		}
		for _, n := range spy.calls[0] {
			if n == "example" || n == "mesh" {
				t.Fatalf("el conjunto oficial no debe incluir %q (ADR-015); got %v", n, spy.calls[0])
			}
		}
		if !bs.done {
			t.Fatal("tras activar con éxito, bs.done debería ser true (deja de sondearse)")
		}
	})
}

// TestBareKeyReentradaS54 blinda la RE-ENTRADA (🔒): una segunda pulsación de activar
// nunca dispara una segunda activación —ni con la primera en éxito (gate `done`) ni en
// fallo (latch `activated`)—. La doble escritura de `enu.toml`/doble `Boot` es el
// fallo silencioso que esto cierra.
func TestBareKeyReentradaS54(t *testing.T) {
	t.Run("dado_activacion_ok_cuando_segunda_pulsacion_entonces_una_sola_activacion", func(t *testing.T) {
		bs, spy, rt := bareWithSpy(t, []string{"repl"})
		bs.handleKey("1", rt)
		bs.handleKey("1", rt)
		if len(spy.calls) != 1 {
			t.Fatalf("dos '1' (éxito) ⇒ activate llamado %d veces, want 1", len(spy.calls))
		}
	})

	t.Run("dado_activacion_falla_cuando_segunda_pulsacion_entonces_una_sola_activacion", func(t *testing.T) {
		bs, spy, rt := bareWithSpy(t, []string{"repl"})
		spy.err = &StructuredError{Code: CodeEINVAL, Message: "enu.toml inválido"}
		bs.handleKey("1", rt)
		bs.handleKey("1", rt)
		if len(spy.calls) != 1 {
			t.Fatalf("dos '1' (fallo) ⇒ activate llamado %d veces, want 1 (latch de re-entrada)", len(spy.calls))
		}
		if bs.errMsg == "" {
			t.Fatal("un fallo de activación debería registrar un error accionable")
		}
	})
}

// TestBareKeyCursorAcotadoS54 blinda el CURSOR ACOTADO (🔒): no baja de 0 ni pasa del
// último índice, y con catálogo VACÍO no indexa fuera de rango ni activa nada.
func TestBareKeyCursorAcotadoS54(t *testing.T) {
	t.Run("dado_seleccion_cuando_up_en_el_tope_entonces_no_baja_de_cero", func(t *testing.T) {
		bs, _, rt := bareWithSpy(t, []string{"a", "b", "c"})
		bs.handleKey("2", rt) // entra en selección (cursor 0)
		bs.handleKey("up", rt)
		bs.handleKey("k", rt)
		if bs.cursor != 0 {
			t.Fatalf("cursor = %d tras up/k en el tope, want 0", bs.cursor)
		}
	})

	t.Run("dado_seleccion_cuando_down_de_mas_entonces_se_queda_en_el_ultimo", func(t *testing.T) {
		bs, _, rt := bareWithSpy(t, []string{"a", "b", "c"})
		bs.handleKey("2", rt)
		for i := 0; i < 5; i++ {
			bs.handleKey("down", rt)
		}
		if bs.cursor != 2 {
			t.Fatalf("cursor = %d tras bajar de más, want 2 (último índice)", bs.cursor)
		}
	})

	t.Run("dado_seleccion_cuando_j_entonces_baja_el_cursor", func(t *testing.T) {
		bs, _, rt := bareWithSpy(t, []string{"a", "b", "c"})
		bs.handleKey("2", rt) // selección (cursor 0)
		bs.handleKey("j", rt) // alias de bajar
		if bs.cursor != 1 {
			t.Fatalf("'j' debería bajar el cursor a 1; got %d", bs.cursor)
		}
	})

	t.Run("dado_catalogo_vacio_cuando_navega_y_enter_entonces_no_activa_ni_panica", func(t *testing.T) {
		bs, spy, rt := bareWithSpy(t, nil)
		bs.handleKey("2", rt)    // selección con catálogo vacío
		bs.handleKey("down", rt) // no debe indexar fuera de rango
		bs.handleKey("up", rt)
		bs.handleKey("enter", rt) // nada que activar
		if bs.cursor != 0 {
			t.Fatalf("cursor = %d con catálogo vacío, want 0", bs.cursor)
		}
		if len(spy.calls) != 0 {
			t.Fatalf("enter con catálogo vacío NO debe activar; got %v", spy.calls)
		}
	})
}

// TestBareKeyActivaSoloElegidaS54: la acción 2 → enter activa SOLO la embebida bajo el
// cursor (ni el conjunto oficial ni otras).
func TestBareKeyActivaSoloElegidaS54(t *testing.T) {
	t.Run("dado_seleccion_cuando_mueve_y_enter_entonces_activa_solo_la_elegida", func(t *testing.T) {
		bs, spy, rt := bareWithSpy(t, []string{"example", "repl", "agent"})
		bs.handleKey("2", rt)    // selección
		bs.handleKey("down", rt) // cursor 1 → "repl"
		if act := bs.handleKey("enter", rt); act != bareNone {
			t.Fatalf("enter devolvió %v, want bareNone", act)
		}
		if want := []string{"repl"}; len(spy.calls) != 1 || !reflect.DeepEqual(spy.calls[0], want) {
			t.Fatalf("enter debería activar SOLO %v; got %v", want, spy.calls)
		}
	})
}

// TestBareKeyEscContextualS54: `esc` es contextual — en selección VUELVE al menú (no
// sale); en el menú raíz SÍ sale.
func TestBareKeyEscContextualS54(t *testing.T) {
	bs, _, rt := bareWithSpy(t, []string{"a"})
	bs.handleKey("2", rt)
	if bs.mode != bareSelect {
		t.Fatal("'2' debería entrar en modo selección")
	}
	if act := bs.handleKey("esc", rt); act != bareNone {
		t.Fatalf("esc en selección devolvió %v, want bareNone (vuelve al menú)", act)
	}
	if bs.mode != bareMenu {
		t.Fatalf("esc en selección debería volver al menú; mode=%v", bs.mode)
	}
	if act := bs.handleKey("esc", rt); act != bareQuit {
		t.Fatalf("esc en el menú raíz devolvió %v, want bareQuit", act)
	}
}

// TestBareKeySalidaMenuS54: las cuatro teclas de salida del menú (`3`, `q`, `ctrl+c`,
// `esc`) devuelven bareQuit.
func TestBareKeySalidaMenuS54(t *testing.T) {
	for _, k := range []string{"3", "q", "ctrl+c", "esc"} {
		t.Run("dado_menu_cuando_pulsa_"+k+"_entonces_sale", func(t *testing.T) {
			bs, _, rt := bareWithSpy(t, []string{"a"})
			if act := bs.handleKey(k, rt); act != bareQuit {
				t.Fatalf("en el menú, %q debería salir (bareQuit); got %v", k, act)
			}
		})
	}
}

// TestBareKeySalidaSeleccionS54: los atajos de salida DUROS (`q`, `ctrl+c`) salen
// también desde el modo SELECCIÓN, no solo desde el menú.
func TestBareKeySalidaSeleccionS54(t *testing.T) {
	for _, k := range []string{"q", "ctrl+c"} {
		t.Run("dado_seleccion_cuando_pulsa_"+k+"_entonces_sale", func(t *testing.T) {
			bs, _, rt := bareWithSpy(t, []string{"a"})
			bs.handleKey("2", rt) // entra en selección
			if act := bs.handleKey(k, rt); act != bareQuit {
				t.Fatalf("en modo selección, %q debería salir (bareQuit); got %v", k, act)
			}
		})
	}
}

// TestBareKeyFalloDejaSalidaVivaS54 blinda ADR-017: tras una activación fallida el
// error accionable queda registrado, la pantalla NO se marca hecha, y la salida por
// teclado sigue operativa (`q` sale) — la terminal jamás queda atrapada.
func TestBareKeyFalloDejaSalidaVivaS54(t *testing.T) {
	bs, spy, rt := bareWithSpy(t, []string{"repl"})
	spy.err = &StructuredError{Code: CodeEINVAL, Message: "enu.toml inválido en \"x\""}

	if act := bs.handleKey("1", rt); act != bareNone {
		t.Fatalf("'1' con activación fallida devolvió %v, want bareNone", act)
	}
	if !strings.Contains(bs.errMsg, "inválido") {
		t.Fatalf("el error accionable no se registró; errMsg=%q", bs.errMsg)
	}
	if bs.done {
		t.Fatal("una activación fallida NO debe marcar done (el producto no se montó)")
	}
	if act := bs.handleKey("q", rt); act != bareQuit {
		t.Fatalf("tras el fallo, 'q' debería salir (bareQuit); got %v", act)
	}
}

// TestBareSelectRenderS54 blinda que el modo SELECCIÓN se PINTA de verdad (el
// catálogo navegable con el marcador de cursor "> " en la elegida, y el mensaje de
// catálogo vacío). Sin esto, la máquina podía cambiar de modo sin que la pantalla lo
// reflejara (el estado del cursor es del driver, pero debe verse).
func TestBareSelectRenderS54(t *testing.T) {
	t.Run("dado_seleccion_cuando_render_entonces_pinta_catalogo_y_marca_el_cursor", func(t *testing.T) {
		bs, _, rt := bareWithSpy(t, []string{"alfa", "beta"})
		bs.handleKey("2", rt) // entra en selección + render (cursor en alfa)

		grid := gridText(rt.ui.comp.back)
		if !strings.Contains(grid, "> alfa") {
			t.Fatalf("la selección no marcó el cursor en la primera opción; pantalla:\n%s", grid)
		}
		if !strings.Contains(grid, "beta") {
			t.Fatalf("la selección no listó el catálogo; pantalla:\n%s", grid)
		}

		bs.handleKey("down", rt) // cursor a beta + render
		grid = gridText(rt.ui.comp.back)
		if !strings.Contains(grid, "> beta") {
			t.Fatalf("tras 'down' el marcador de cursor no está en beta; pantalla:\n%s", grid)
		}
		if strings.Contains(grid, "> alfa") {
			t.Fatalf("tras 'down' el marcador NO debería seguir en alfa; pantalla:\n%s", grid)
		}
	})

	t.Run("dado_catalogo_vacio_cuando_render_seleccion_entonces_mensaje_de_vacio", func(t *testing.T) {
		bs, _, rt := bareWithSpy(t, nil)
		bs.handleKey("2", rt)
		grid := gridText(rt.ui.comp.back)
		if !strings.Contains(grid, "no hay extensiones") {
			t.Fatalf("con catálogo vacío la selección debería avisar; pantalla:\n%s", grid)
		}
	})
}

// TestBareRenderReusaRegionS54 blinda que la pantalla desnuda NO acumula regiones:
// por mucho que se re-renderice (cada pulsación repinta), el compositor conserva UNA
// sola región (recrear suelta la anterior). Un `addRegion` sin `removeRegion` sería
// una fuga que crece con cada tecla.
func TestBareRenderReusaRegionS54(t *testing.T) {
	bs, _, rt := bareWithSpy(t, []string{"a", "b", "c"})
	bs.render(rt)            // 1ª región (menú)
	bs.handleKey("2", rt)    // selección + render
	bs.handleKey("down", rt) // + render
	bs.handleKey("up", rt)   // + render
	bs.handleKey("esc", rt)  // vuelve al menú + render
	if n := len(rt.ui.comp.regions); n != 1 {
		t.Fatalf("la pantalla desnuda no debe acumular regiones tras varios renders; got %d, want 1", n)
	}
}

// TestBareTeardownSueltaRegionS54 blinda que, tras activar con éxito, la región del
// menú desnudo se SUELTA del compositor: el producto gobierna la pantalla y el menú
// (z=0, al fondo) no debe quedar debajo (fuga de región / bleed-through).
func TestBareTeardownSueltaRegionS54(t *testing.T) {
	bs, _, rt := bareWithSpy(t, []string{"a"})
	bs.render(rt) // crea la región del menú
	if n := len(rt.ui.comp.regions); n != 1 {
		t.Fatalf("precondición: debería haber 1 región; got %d", n)
	}
	bs.teardown(rt)
	if n := len(rt.ui.comp.regions); n != 0 {
		t.Fatalf("tras teardown la región del menú desnudo debe soltarse; quedan %d", n)
	}
	if bs.region != nil {
		t.Fatal("bs.region debería quedar nil tras teardown")
	}
}

// TestBareScreenDriverNavigaNoSaleS54 blinda que SOLO las teclas de salida apagan el
// bucle: una tecla de navegación ('2', que entra en selección) NO debe apagarlo, y
// 'q' sí. Cierra el hueco de que `pollBareAction` apagara ante cualquier tecla.
func TestBareScreenDriverNavigaNoSaleS54(t *testing.T) {
	rt, _ := newBareRuntime(t, WithUISize(80, 24))
	h := &harness{t: t, rt: rt}
	rt.PrepareBareScreen()

	inR, inW := io.Pipe()
	out := &syncBuf{}
	d := newDriver(rt, inR, out)
	d.installShutdownHandler()
	d.attachOutput()

	done := make(chan struct{})
	go func() { d.drive(); close(done) }()

	// '2' entra en modo selección (bareNone): el bucle NO debe apagarse.
	if _, err := inW.Write([]byte("2")); err != nil {
		t.Fatalf("write '2': %v", err)
	}
	if !waitScreen(h, "elige una extensión", 2*time.Second) {
		t.Fatalf("'2' no entró en modo selección; pantalla:\n%s", dumpScreen(h))
	}
	select {
	case <-done:
		t.Fatal("una tecla de navegación ('2') NO debe apagar el bucle del driver")
	case <-time.After(150 * time.Millisecond):
		// sigue vivo: correcto.
	}

	// 'q' sí apaga.
	if _, err := inW.Write([]byte("q")); err != nil {
		t.Fatalf("write 'q': %v", err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("'q' debería apagar el bucle; pantalla:\n%s", dumpScreen(h))
	}
	_ = inW.Close()
}

// TestBareScreenDriverActivateUnderPumpS54 blinda las dos grietas de concurrencia y
// salida que el juicio destapó, ambas en el camino de activación REAL bajo el driver
// (que los tests de spy directo a `handleKey` no ejercitan):
//
//   - C1 (crítico): `activateAndBoot`→`Boot`→`BootWasm` llama a `RunTasks`, REENTRANTE
//     con el pump continuo vivo (pumpActive). Sin parar el pump alrededor, activar
//     SIEMPRE fallaría. El spy reproduce EXACTAMENTE esa llamada (`RunTasks`) y afirma
//     que devuelve nil: solo ocurre si `drive` paró el pump antes de activar.
//   - H1 (ADR-017): tras activar una embebida SIN salida propia (aquí el spy no monta
//     nada), la terminal no debe quedar atrapada: `q` sigue apagando por la red de
//     salida de emergencia del fondo (el reenviador se vuelve transparente con `done`).
func TestBareScreenDriverActivateUnderPumpS54(t *testing.T) {
	rt, _ := newBareRuntime(t, WithUISize(80, 24))
	rt.PrepareBareScreen()

	// Sustituye la activación por la operación que C1 rompe: `RunTasks` (lo que
	// `BootWasm` invoca), reentrante si el pump sigue vivo. Se fija ANTES de `drive`,
	// que la envuelve con parar/rearrancar el pump.
	activated := make(chan error, 1)
	rt.bare.activate = func(names []string) error {
		err := rt.wasm.RunTasks(context.Background())
		activated <- err
		return err
	}

	inR, inW := io.Pipe()
	out := &syncBuf{}
	d := newDriver(rt, inR, out)
	d.installShutdownHandler()
	d.attachOutput()

	done := make(chan struct{})
	go func() { d.drive(); close(done) }()

	// '1' activa: `drive` debe PARAR el pump antes, así que `RunTasks` no es reentrante.
	if _, err := inW.Write([]byte("1")); err != nil {
		t.Fatalf("write '1': %v", err)
	}
	select {
	case err := <-activated:
		if err != nil {
			t.Fatalf("C1: la activación bajo el pump vivo falló (RunTasks reentrante): %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("la activación no ocurrió")
	}

	// H1 (ADR-017): sin salida propia montada, 'q' sigue apagando (red de emergencia).
	if _, err := inW.Write([]byte("q")); err != nil {
		t.Fatalf("write 'q': %v", err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("tras activar sin UI propia, 'q' no apagó el bucle — terminal atrapada (ADR-017)")
	}
	_ = inW.Close()
}

// TestBareScreenDriverMalformedTomlS54 es el test de INTEGRACIÓN (🔒, ADR-017):
// conduce el driver con tuberías en memoria contra un `enu.toml` MALFORMADO. `1`
// (activar oficial) falla con EINVAL sin pisar el fichero, el error se pinta en la
// pantalla, y `q` sigue apagando el bucle (la terminal no queda atrapada en raw mode).
func TestBareScreenDriverMalformedTomlS54(t *testing.T) {
	cfg := t.TempDir()
	bad := "[plugins\nenabled = roto"
	writeNuToml(t, cfg, bad)

	rt := New(WithDataDir(t.TempDir()), WithConfigDir(cfg), WithForceUI(true), WithUISize(80, 40))
	t.Cleanup(rt.Close)
	h := &harness{t: t, rt: rt}

	if !rt.BareScreenActive() {
		t.Fatal("con un enu.toml malformado (sin plugins activos) la pantalla desnuda debe estar activa")
	}
	rt.PrepareBareScreen()

	inR, inW := io.Pipe()
	out := &syncBuf{}
	d := newDriver(rt, inR, out)
	d.installShutdownHandler()
	d.attachOutput()

	done := make(chan struct{})
	go func() { d.drive(); close(done) }()

	// '1' → activar oficial → writeEnabledPlugins devuelve EINVAL (no pisa el fichero)
	// → el error accionable se pinta en la pantalla.
	if _, err := inW.Write([]byte("1")); err != nil {
		t.Fatalf("write '1': %v", err)
	}
	if !waitScreen(h, "no se pudo activar", 2*time.Second) {
		t.Fatalf("el error de activación no se pintó; pantalla:\n%s", dumpScreen(h))
	}

	// El enu.toml malformado quedó INTACTO (no se sobrescribió a ciegas).
	data, _ := os.ReadFile(filepath.Join(cfg, nuTomlName))
	if string(data) != bad {
		t.Fatalf("el enu.toml malformado no debía tocarse; got:\n%s", data)
	}

	// La salida por teclado sigue viva (ADR-017): 'q' apaga el bucle.
	if _, err := inW.Write([]byte("q")); err != nil {
		t.Fatalf("write 'q': %v", err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("tras el fallo de activación, 'q' no apagó el bucle; pantalla:\n%s", dumpScreen(h))
	}
	_ = inW.Close()
}
