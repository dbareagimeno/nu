package runtime

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	lua "github.com/yuin/gopher-lua"
)

// `nu.ui` — construcción de Blocks, estilos y capacidades del terminal (api.md
// §9.2, sesión S22). En S22 se registra **solo** la parte que NO depende de una
// pantalla viva: `nu.ui.block` (construcción manual de Blocks), `nu.ui.caps`
// (capacidades del terminal) y el parseo de `Style`. El compositor (regiones,
// blit, input: §9.1/§9.3) llega en S28–S31, y el **gating headless** (G20:
// "`nu.ui` no existe sin TTY") es S32.
//
// POR QUÉ `nu.ui` EXISTE YA (también headless). El contrato dice que sin TTY el
// módulo `nu.ui` directamente no existe (G20). Pero ese gating es trabajo de S32,
// y S23–S31 necesitan `nu.ui.block`/`caps`/`Style` desde ya para construir e
// inspeccionar Blocks en sus tests (markdown, highlight, diff producen Blocks; el
// theme resuelve `Style`). Así que en S22 `nu.ui` se cuelga siempre; S32 añadirá
// la condición de TTY por encima sin tocar estas firmas. Es deuda explícita
// (NOTA DE FRONTERA del plan), no una contradicción de G20.

// registerUI cuelga `nu.ui` del global `nu` con la superficie de S22:
// `block`/`caps` y, de paso, instala la metatabla del tipo `Block` (block.go).
// El resto de §9 (size/region/input/clipboard) son sesiones posteriores.
func (rt *Runtime) registerUI(nu *lua.LTable) {
	L := rt.L

	// La metatabla del tipo opaco `Block`: la instala UI porque `nu.ui.block` es el
	// constructor manual y el resto de productores (nu.text.*) la comparten.
	rt.registerBlockType()

	uiT := L.NewTable()
	uiT.RawSetString("block", L.NewFunction(rt.uiBlock))
	uiT.RawSetString("caps", L.NewFunction(rt.uiCaps))
	nu.RawSetString("ui", uiT)
}

// uiBlock implementa `nu.ui.block(lines) -> Block` (§9.2): construcción manual de
// un Block. `lines` es un array; cada línea es **un string** (un solo span sin
// estilo) **o** un array de Spans `{text, style?}`. Calcula `.width` (máximo
// ancho de línea en celdas, vía `text.width`) y `.height` (número de líneas) al
// construir (block.go). Un argumento mal formado → `EINVAL` accionable.
func (rt *Runtime) uiBlock(L *lua.LState) int {
	arg := L.CheckTable(1)

	lines := make([][]span, 0, arg.Len())
	var convErr string
	idx := 0
	arg.ForEach(func(k, v lua.LValue) {
		if convErr != "" {
			return
		}
		idx++
		spans, err := rt.parseLine(L, v)
		if err != "" {
			convErr = fmt.Sprintf("nu.ui.block: línea %d: %s", idx, err)
			return
		}
		lines = append(lines, spans)
	})
	if convErr != "" {
		raiseError(L, CodeEINVAL, convErr, lua.LNil)
		return 0
	}

	rt.pushBlock(L, newBlock(lines))
	return 1
}

// parseLine convierte una línea de `nu.ui.block` a una rebanada de spans. Una
// línea puede ser un **string** (un único span sin estilo) o una **tabla** que es
// un array de Spans (`{text, style?}`). Devuelve un mensaje de error (no vacío) en
// vez de lanzar para que `uiBlock` añada el número de línea al contexto.
func (rt *Runtime) parseLine(L *lua.LState, v lua.LValue) ([]span, string) {
	switch line := v.(type) {
	case lua.LString:
		// Una línea-string es un único span sin estilo. Una línea vacía ("") es un
		// span con texto "" (ancho 0): conserva la línea en blanco (afecta a .height).
		return []span{{text: string(line)}}, ""
	case *lua.LTable:
		// Array de Spans. Cada elemento es una tabla `{text, style?}`.
		spans := make([]span, 0, line.Len())
		var spanErr string
		i := 0
		line.ForEach(func(_, sv lua.LValue) {
			if spanErr != "" {
				return
			}
			i++
			st, ok := sv.(*lua.LTable)
			if !ok {
				spanErr = fmt.Sprintf("el span %d debe ser una tabla {text, style?}", i)
				return
			}
			text, ok := st.RawGetString("text").(lua.LString)
			if !ok {
				spanErr = fmt.Sprintf("el span %d necesita un campo `text` de tipo string", i)
				return
			}
			sp := span{text: string(text)}
			if styleVal := st.RawGetString("style"); styleVal != lua.LNil {
				parsed, err := parseStyle(L, styleVal)
				if err != "" {
					spanErr = fmt.Sprintf("el span %d: %s", i, err)
					return
				}
				sp.st = parsed
			}
			spans = append(spans, sp)
		})
		return spans, spanErr
	default:
		return nil, fmt.Sprintf("cada línea debe ser un string o un array de spans, no %s", v.Type().String())
	}
}

// parseStyle convierte una tabla `Style` Lua (`{fg?, bg?, bold?, italic?,
// underline?, reverse?}`, §9.2) a un `*style` Go, validando los colores. Los
// colores son **literales**: un string "#rrggbb" o un índice 0-255 (número o
// string numérica); los nombres semánticos NO son del core (G22), así que un
// string que no sea "#rrggbb" ni un número en rango es `EINVAL`. Devuelve un
// mensaje de error (no vacío) en lugar de lanzar, para componer el contexto.
func parseStyle(L *lua.LState, v lua.LValue) (*style, string) {
	t, ok := v.(*lua.LTable)
	if !ok {
		return nil, "`style` debe ser una tabla"
	}
	s := &style{}

	if fg := t.RawGetString("fg"); fg != lua.LNil {
		norm, err := normalizeColor(fg)
		if err != "" {
			return nil, "style.fg: " + err
		}
		s.fg, s.fgSet = norm, true
	}
	if bg := t.RawGetString("bg"); bg != lua.LNil {
		norm, err := normalizeColor(bg)
		if err != "" {
			return nil, "style.bg: " + err
		}
		s.bg, s.bgSet = norm, true
	}
	s.bold = lua.LVAsBool(t.RawGetString("bold"))
	s.italic = lua.LVAsBool(t.RawGetString("italic"))
	s.underline = lua.LVAsBool(t.RawGetString("underline"))
	s.reverse = lua.LVAsBool(t.RawGetString("reverse"))
	return s, ""
}

// normalizeColor valida y normaliza un color literal de `Style` (§9.2) a su forma
// canónica en string. Acepta:
//   - un string "#rrggbb" (seis dígitos hex tras '#'), normalizado a minúsculas;
//   - un índice 0-255, como número Lua o como string numérica, normalizado al
//     decimal en string ("42").
//
// Cualquier otra cosa (un nombre semántico como "accent", un hex de longitud
// equivocada, un índice fuera de rango) es error: los nombres son del theme del
// toolkit (G22), no del core.
func normalizeColor(v lua.LValue) (string, string) {
	switch c := v.(type) {
	case lua.LNumber:
		f := float64(c)
		i := int(f)
		if float64(i) != f || i < 0 || i > 255 {
			return "", fmt.Sprintf("índice de color debe ser un entero 0-255, no %v", f)
		}
		return strconv.Itoa(i), ""
	case lua.LString:
		s := string(c)
		if strings.HasPrefix(s, "#") {
			if !isHexColor(s) {
				return "", fmt.Sprintf("color hex debe ser \"#rrggbb\" (6 dígitos hex), no %q", s)
			}
			return strings.ToLower(s), ""
		}
		// Una string numérica también es un índice válido (azúcar para quien guarde
		// el índice como texto). Un nombre semántico cae aquí y se rechaza.
		if i, err := strconv.Atoi(s); err == nil {
			if i < 0 || i > 255 {
				return "", fmt.Sprintf("índice de color debe ser 0-255, no %d", i)
			}
			return strconv.Itoa(i), ""
		}
		return "", fmt.Sprintf("color debe ser \"#rrggbb\" o un índice 0-255, no %q (los nombres semánticos los resuelve el theme, G22)", s)
	default:
		return "", fmt.Sprintf("color debe ser un string \"#rrggbb\" o un índice 0-255, no %s", v.Type().String())
	}
}

// isHexColor comprueba que `s` tenga la forma "#rrggbb": una almohadilla seguida
// de exactamente seis dígitos hexadecimales.
func isHexColor(s string) bool {
	if len(s) != 7 || s[0] != '#' {
		return false
	}
	for _, r := range s[1:] {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}

// uiCaps implementa `nu.ui.caps() -> {colors, kitty_keyboard, mouse, images}`
// (§9.2): las capacidades del terminal. En S22 no hay un terminal vivo del que
// interrogar protocolos (eso es la Fase 6), así que se detecta lo que se puede de
// forma estática por el entorno (`COLORTERM`/`TERM` → número de colores) y el
// resto se deja en un default conservador (false): kitty_keyboard/mouse/images se
// confirman con una negociación de protocolo que aún no existe. Es deny-by-default
// (como `nu.has`, §2): no afirmar una capacidad que no se ha podido comprobar.
func (rt *Runtime) uiCaps(L *lua.LState) int {
	caps := L.NewTable()
	caps.RawSetString("colors", lua.LNumber(detectColors()))
	caps.RawSetString("kitty_keyboard", lua.LBool(false))
	caps.RawSetString("mouse", lua.LBool(false))
	caps.RawSetString("images", lua.LBool(false))
	L.Push(caps)
	return 1
}

// detectColors estima el número de colores del terminal por el entorno, sin tocar
// el terminal (la negociación real es Fase 6). `COLORTERM=truecolor`/`24bit` →
// 16M (1<<24); un `TERM` con "256color" → 256; un `TERM` no vacío → 16; sin TERM
// (headless/CI/redirigido) → 256 como default razonable (la mayoría de terminales
// modernos lo soportan, y el render degrada a lo que de verdad haya, §9.2). No es
// un sniffing frágil: es una pista, y el compositor (S29) degrada con seguridad.
func detectColors() int {
	if ct := strings.ToLower(os.Getenv("COLORTERM")); ct == "truecolor" || ct == "24bit" {
		return 1 << 24
	}
	term := os.Getenv("TERM")
	switch {
	case term == "":
		return 256 // headless / sin TTY: default razonable
	case strings.Contains(term, "256color"):
		return 256
	case strings.Contains(term, "truecolor"):
		return 1 << 24
	case term == "dumb":
		return 0
	default:
		return 16
	}
}
