package spike

// Pregunta 1 del spike (¿compila y arranca?) y pregunta 2 (¿cede a través del
// puente y de pcall?): el Lua OFICIAL corriendo sobre wazero con el trampolín.

import (
	"strings"
	"testing"
)

func boot(t *testing.T) *LuaWasm {
	t.Helper()
	lw, err := NewLuaWasm("../lua.wasm")
	if err != nil {
		t.Fatalf("boot: %v", err)
	}
	t.Cleanup(lw.Close)
	return lw
}

// P1a: el intérprete arranca y evalúa.
func TestLuaBoot(t *testing.T) {
	lw := boot(t)
	out, lerr, err := lw.Eval(`return _VERSION .. " ok " .. tostring(1+1)`)
	if err != nil || lerr != "" {
		t.Fatalf("eval: %q %q %v", out, lerr, err)
	}
	if out != "Lua 5.4 ok 2" {
		t.Fatalf("got %q", out)
	}
}

// P1b: un error() atraviesa el trampolín, se captura, y el estado SIGUE VIVO.
func TestLuaErrorYRecuperacion(t *testing.T) {
	lw := boot(t)
	_, lerr, err := lw.Eval(`error("boom controlado")`)
	if err != nil {
		t.Fatalf("error duro: %v", err)
	}
	if !strings.Contains(lerr, "boom controlado") {
		t.Fatalf("el mensaje no llegó: %q", lerr)
	}
	for i := 0; i < 200; i++ { // reusabilidad tras errores
		if _, lerr, err = lw.Eval(`error("x")`); err != nil || lerr == "" {
			t.Fatalf("iteración %d: %q %v", i, lerr, err)
		}
	}
	out, _, _ := lw.Eval(`return "vivo"`)
	if out != "vivo" {
		t.Fatalf("el estado quedó tocado: %q", out)
	}
}

// P1c: pcall de Lua (try ANIDADO del trampolín) captura y se sigue ejecutando.
func TestLuaPcallAnidado(t *testing.T) {
	lw := boot(t)
	out, lerr, err := lw.Eval(`
		local ok1, e1 = pcall(function() error({ code = "EINVAL" }) end)
		local ok2 = pcall(function()
			local ok3 = pcall(function() error("interno") end)
			if ok3 then error("el interno no capturó") end
			error("relanzado")
		end)
		return tostring(ok1) .. "," .. tostring(e1.code) .. "," .. tostring(ok2)`)
	if err != nil || lerr != "" {
		t.Fatalf("%q %v", lerr, err)
	}
	if out != "false,EINVAL,false" {
		t.Fatalf("got %q", out)
	}
}

// P2a — LA JOYA: la repro de G41 da la semántica ESTÁNDAR (42, no nil).
// En gopher-lua v1.1.2 esto devuelve nil sin el blindaje del kernel.
func TestLuaG41SemanticaEstandar(t *testing.T) {
	lw := boot(t)
	out, lerr, err := lw.Eval(`
		local X = nil
		local set = function(v) X = v end
		pcall(function() error("boom") end)
		set(42)
		return tostring(X)`)
	if err != nil || lerr != "" {
		t.Fatalf("%q %v", lerr, err)
	}
	if out != "42" {
		t.Fatalf("G41: got %q, want 42 — ¡la implementación de referencia no tiene el bug!", out)
	}
}

// P2b — LA OTRA JOYA: yield A TRAVÉS de pcall (G31: imposible en gopher-lua,
// lo que forzó el scheduler sin yields de ADR-011). Aquí es Lua estándar.
func TestLuaYieldATravesDePcall(t *testing.T) {
	lw := boot(t)
	ref, err := lw.CoSpawn(`
		local ok, res = pcall(function()
			local respuesta = nu_await("necesito-io")   -- ⏸ yield DENTRO de un pcall
			if respuesta ~= "io-listo" then error("respuesta inesperada: " .. tostring(respuesta)) end
			return "completado"
		end)
		return tostring(ok) .. ":" .. tostring(res)`)
	if err != nil {
		t.Fatal(err)
	}
	st, payload, err := lw.CoResume(ref, nil) // arranca; debe quedar suspendida
	if err != nil || st != CoYield || payload != "necesito-io" {
		t.Fatalf("primer resume: st=%v payload=%q err=%v", st, payload, err)
	}
	respuesta := "io-listo" // ...el lado Go "hace el IO"...
	st, out, err := lw.CoResume(ref, &respuesta)
	if err != nil || st != CoDone {
		t.Fatalf("segundo resume: st=%v out=%q err=%v", st, out, err)
	}
	if out != "true:completado" {
		t.Fatalf("got %q — el pcall no sobrevivió al yield", out)
	}
}

// P2c: un error DESPUÉS del yield, dentro del pcall que lo cruzó: se captura.
func TestLuaErrorTrasYieldDentroDePcall(t *testing.T) {
	lw := boot(t)
	ref, err := lw.CoSpawn(`
		local ok, e = pcall(function()
			nu_await("x")
			error("fallo tras reanudar")
		end)
		return tostring(ok) .. ":" .. tostring(e)`)
	if err != nil {
		t.Fatal(err)
	}
	if st, _, _ := lw.CoResume(ref, nil); st != CoYield {
		t.Fatalf("no suspendió")
	}
	v := "da igual"
	st, out, err := lw.CoResume(ref, &v)
	if err != nil || st != CoDone {
		t.Fatalf("st=%v err=%v", st, err)
	}
	if !strings.Contains(out, "false:") || !strings.Contains(out, "fallo tras reanudar") {
		t.Fatalf("got %q", out)
	}
}
