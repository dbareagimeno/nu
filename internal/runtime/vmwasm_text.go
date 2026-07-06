package runtime

// Catálogo de nu.text sobre el backend wasm (M13b PARCIAL, §10). Contraparte de
// text.go para las funciones que NO producen Blocks: width (anchura en celdas) y
// truncate (recorte por grapheme). Reusan las mismas funciones puras Go
// (uniseg.StringWidth, truncateText).
//
// Las que SÍ producen Blocks —wrap, markdown, highlight, diff— se enchufan con el
// compositor real en M13c (un Block es un handle cuyo objeto Go es el *block de
// block.go, que debe implementar vmwasm.BlockObj). Aquí sólo las puras.

import (
	"github.com/rivo/uniseg"

	"github.com/dbareagimeno/nu/internal/vmwasm"
)

func registerTextWasm(p *vmwasm.Pool, rt *Runtime) {
	// nu.text.width(s) -> integer: anchura en celdas (graphemes, east-asian, emoji).
	p.Register("text.width", func(inst *vmwasm.Instance, args []any) ([]any, error) {
		return []any{int64(uniseg.StringWidth(argString(args, 0)))}, nil
	})
	// nu.text.truncate(s, width, opts?) -> string: recorte por grapheme con elipsis
	// opcional (opts.ellipsis). width negativo → EINVAL.
	p.Register("text.truncate", func(inst *vmwasm.Instance, args []any) ([]any, error) {
		s := argString(args, 0)
		width := argInt(args, 1)
		if width < 0 {
			return nil, &vmwasm.StructuredError{Code: "EINVAL", Message: "nu.text.truncate: width no puede ser negativo"}
		}
		ellipsis := ""
		if opts, ok := arg(args, 2).(map[string]any); ok {
			ellipsis, _ = opts["ellipsis"].(string)
		}
		return []any{truncateText(s, width, ellipsis)}, nil
	})
}

// argInt lee un entero de args[i] (int64 o float64 según cruce el wire).
func argInt(args []any, i int) int {
	switch v := arg(args, i).(type) {
	case int64:
		return int(v)
	case float64:
		return int(v)
	}
	return 0
}
