package runtime

import (
	"testing"

	lua "github.com/yuin/gopher-lua"
)

// Tests unitarios del puente de errores estructurados (S02, inventario 🔒).
// Blindan dos invariantes de api.md §1.4:
//   - la **forma** de la tabla es exactamente `{code, message, detail?}`;
//   - un código reservado **nunca se traga ni se reescribe** al cruzar el puente
//     Go→Lua→Go (ni se degrada a texto, ni se remapea el `code`).

// registerFail instala en el arnés una primitiva Go de andamiaje `fail` que
// lanza un error estructurado con el `code`/`message`/`detail` que reciba. Es la
// forma de "forzar un EINVAL" (criterio de hecho de S02) sin que el runtime de
// producción tenga aún ninguna primitiva que falle.
func registerFail(h *harness) {
	if h.isWasm() {
		// Equivalente wasm expresado en Lua: lanza la MISMA tabla estructurada
		// {code, message, detail?}. `detail` sólo aparece si se pasó (nil si no),
		// idéntico a raiseError con lua.LNil. No necesita una LGFunction: el error
		// estructurado es un valor Lua puro.
		h.defWasmGlobal(`function fail(code, msg, detail)
  error({ code = code, message = msg, detail = detail })
end`)
		return
	}
	h.register("fail", func(L *lua.LState) int {
		code := L.CheckString(1)
		msg := L.OptString(2, "")
		detail := L.Get(3) // lua.LNil si no se pasó
		raiseError(L, code, msg, detail)
		return 0 // inalcanzable: raiseError desenrolla
	})
}

// allReservedCodes es el orden estable de los códigos reservados para las tablas
// de casos. Si §1.4 crece, esta lista crece con él (y reservedCodes también).
var allReservedCodes = []string{
	CodeENOENT, CodeEEXIST, CodeEACCES, CodeEIO, CodeEHTTP, CodeENET,
	CodeETIMEOUT, CodeECANCELED, CodeEBUDGET, CodeEINVAL, CodeECLOSED,
}

// TestErrorTableShape: la tabla capturada por `pcall` tiene la forma del
// contrato. `detail` aparece solo si se aportó (distingue "sin detalle" de
// "detalle = nil").
func TestErrorTableShape(t *testing.T) {
	h := newHarness(t)
	registerFail(h)

	// Con detalle: code/message/detail presentes y del tipo correcto.
	h.eval(`
		local ok, err = pcall(function() fail("EINVAL", "ruta vacía", { arg = "path" }) end)
		assert(ok == false, "fail debió lanzar")
		assert(type(err) == "table", "el error debe ser una tabla, no " .. type(err))
		assert(err.code == "EINVAL", "code inesperado: " .. tostring(err.code))
		assert(err.message == "ruta vacía", "message inesperado: " .. tostring(err.message))
		assert(type(err.detail) == "table", "detail debe conservarse")
		assert(err.detail.arg == "path", "detail.arg inesperado")
		return true
	`)

	// Sin detalle: la clave `detail` no existe (es nil), no una tabla vacía.
	h.eval(`
		local ok, err = pcall(function() fail("EIO", "disco lleno") end)
		assert(ok == false)
		assert(err.code == "EIO")
		assert(err.message == "disco lleno")
		assert(err.detail == nil, "detail debe ser nil cuando no se aporta")
		return true
	`)
}

// TestReservedCodesNotSwallowedNorRewritten: el corazón del 🔒 de S02. Para cada
// código reservado, lanzado desde Go: (a) Lua lo captura como tabla con el code
// **literal** intacto; (b) si no se captura, el puente Eval+ EvalString lo
// devuelve como *StructuredError con el mismo code (no como texto, no remapeado).
func TestReservedCodesNotSwallowedNorRewritten(t *testing.T) {
	for _, code := range allReservedCodes {
		code := code
		t.Run(code, func(t *testing.T) {
			// (a) capturado en Lua: forma y code exactos.
			h := newHarness(t)
			registerFail(h)
			h.eval(`
				local code = "` + code + `"
				local ok, err = pcall(function() fail(code, "msg de " .. code) end)
				assert(ok == false, "debió lanzar " .. code)
				assert(type(err) == "table", code .. ": el error se degradó a " .. type(err))
				assert(err.code == code, "code reescrito: " .. tostring(err.code) .. " != " .. code)
				assert(err.message == "msg de " .. code, "message alterado")
				return true
			`)

			// (b) sin capturar: cruza el puente como *StructuredError intacto.
			se := h.evalErr(`fail("` + code + `", "no capturado")`)
			if se.Code != code {
				t.Fatalf("code reescrito al cruzar el puente: got %q, want %q", se.Code, code)
			}
			if se.Message != "no capturado" {
				t.Fatalf("message alterado al cruzar el puente: %q", se.Message)
			}
		})
	}
}

// TestStructuredErrorRoundTripDetail: un detalle estructurado sobrevive el viaje
// Go→Lua→Go: tras EvalString, el *StructuredError conserva la tabla `detail`.
func TestStructuredErrorRoundTripDetail(t *testing.T) {
	h := newHarness(t)
	// StructuredError.Detail es un lua.LValue (tipo GOPHER): el error de un chunk wasm
	// no nace en un LState gopher, así que su detail no puede materializarse como
	// *lua.LTable en la frontera de EvalString (el code/message SÍ cruzan fiel, que es
	// la garantía funcional; el detail sigue accesible como tabla DENTRO de Lua vía
	// pcall). Comprobar el tipo gopher del detail sólo tiene sentido en el backend gopher.
	h.skipIfWasm("StructuredError.Detail como *lua.LTable es un artefacto del LState gopher")
	registerFail(h)

	se := h.evalErr(`fail("ENOENT", "no está", { path = "/x", retries = 3 })`)
	if se.Code != CodeENOENT {
		t.Fatalf("code: got %q, want %q", se.Code, CodeENOENT)
	}
	detail, ok := se.Detail.(*lua.LTable)
	if !ok {
		t.Fatalf("detail: got %T, want *lua.LTable", se.Detail)
	}
	if got := detail.RawGetString("path"); got.String() != "/x" {
		t.Fatalf("detail.path: got %q, want %q", got.String(), "/x")
	}
	if got := detail.RawGetString("retries"); got != lua.LNumber(3) {
		t.Fatalf("detail.retries: got %v, want 3", got)
	}
}

// TestExtensionCodePassesThrough: el puente no es exclusivo de los reservados.
// Una extensión acuña su propio código (§1.4) y debe cruzar igual de intacto:
// la regla "no reescribir" vale para cualquier code, no solo los del core.
func TestExtensionCodePassesThrough(t *testing.T) {
	h := newHarness(t)
	registerFail(h)

	se := h.evalErr(`fail("EPROVIDER", "rate limit")`)
	if se.Code != "EPROVIDER" {
		t.Fatalf("code de extensión reescrito: got %q, want %q", se.Code, "EPROVIDER")
	}
	if IsReservedCode("EPROVIDER") {
		t.Fatalf("EPROVIDER no debe figurar como reservado del core")
	}
}

// TestLuaErrorStringIsNotStructured: un `error("texto")` de Lua —o cualquier
// error sin la forma del contrato— NO se hace pasar por estructurado. El puente
// solo reconoce tablas con `code` string; lo demás se devuelve tal cual, sin
// inventar un code.
func TestLuaErrorStringIsNotStructured(t *testing.T) {
	h := newHarness(t)

	// Un error de string: EvalString lo devuelve, pero no como *StructuredError.
	_, err := h.rt.EvalString(`error("explosión")`)
	if err == nil {
		t.Fatal("se esperaba un error")
	}
	if _, ok := err.(*StructuredError); ok {
		t.Fatalf("un error de string no debe verse como estructurado: %v", err)
	}

	// Una tabla sin `code` tampoco es estructurada (forma incompleta).
	_, err = h.rt.EvalString(`error({ message = "sin code" })`)
	if err == nil {
		t.Fatal("se esperaba un error")
	}
	if _, ok := err.(*StructuredError); ok {
		t.Fatalf("una tabla sin code no debe verse como estructurada: %v", err)
	}
}

// TestIsReservedCode: la lista reservada coincide con §1.4, ni de más ni de
// menos. Protege contra que alguien añada un code al core sin declararlo (o
// declare uno que la espec no reserva).
func TestIsReservedCode(t *testing.T) {
	for _, code := range allReservedCodes {
		if !IsReservedCode(code) {
			t.Errorf("%s debería ser reservado", code)
		}
	}
	if len(reservedCodes) != len(allReservedCodes) {
		t.Errorf("reservedCodes tiene %d entradas, la espec §1.4 lista %d",
			len(reservedCodes), len(allReservedCodes))
	}
	for _, code := range []string{"EPROVIDER", "", "einval", "FOO"} {
		if IsReservedCode(code) {
			t.Errorf("%q no debería ser reservado", code)
		}
	}
}
