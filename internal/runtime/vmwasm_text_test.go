package runtime

// Tests de M13b (parcial): nu.text.width / nu.text.truncate sobre wasm (§10). Las
// funciones que producen Blocks (wrap/markdown/highlight/diff) llegan con M13c.

import (
	"testing"

	"github.com/dbareagimeno/nu/internal/vmwasm"
)

func wasmTextInst(t *testing.T) *vmwasm.Instance {
	t.Helper()
	p, err := vmwasm.NewPool()
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })
	registerTextWasm(p, &Runtime{})
	inst, err := p.NewInstance()
	if err != nil {
		t.Fatalf("NewInstance: %v", err)
	}
	t.Cleanup(func() { _ = inst.Close() })
	return inst
}

// M13b.text.1: width cuenta celdas (ASCII 1, east-asian wide 2).
func TestTextWasmWidth(t *testing.T) {
	inst := wasmTextInst(t)
	out := evalWasm(t, inst, `
		return tostring(nu.text.width("abc")) .. ":" .. tostring(nu.text.width("日本"))`)
	// "abc" = 3 celdas; "日本" = 2 caracteres wide = 4 celdas.
	if out != "3:4" {
		t.Fatalf("width: got %q (esperado 3:4)", out)
	}
}

// M13b.text.2: truncate recorta a width celdas con elipsis; si cabe, sin tocar.
func TestTextWasmTruncate(t *testing.T) {
	inst := wasmTextInst(t)
	out := evalWasm(t, inst, `
		local cabe = nu.text.truncate("hola", 10)
		local corta = nu.text.truncate("hola mundo", 6, { ellipsis = "…" })
		return cabe .. "|" .. corta .. "|" .. tostring(nu.text.width(corta) <= 6)`)
	// "hola" cabe entero; "hola mundo" recortado a <=6 celdas con elipsis.
	if got := out; got[:5] != "hola|" || got[len(got)-4:] != "true" {
		t.Fatalf("truncate: got %q", out)
	}
}

// M13b.text.3: truncate con width negativo → EINVAL.
func TestTextWasmTruncateInvalido(t *testing.T) {
	inst := wasmTextInst(t)
	out := evalWasm(t, inst, `
		local ok, e = pcall(function() return nu.text.truncate("x", -1) end)
		return tostring(ok) .. ":" .. tostring(e.code)`)
	if out != "false:EINVAL" {
		t.Fatalf("truncate inválido: got %q", out)
	}
}
