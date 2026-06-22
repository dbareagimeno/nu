package runtime

import (
	"regexp"

	lua "github.com/yuin/gopher-lua"
)

// `nu.re` — expresiones regulares RE2 (api.md §10, sesión S26). Cuatro
// operaciones sobre un patrón compilado: `compile` lo prepara y devuelve un
// handle `Re` con tres métodos —`match` (primera coincidencia con sus
// capturas), `find_all` (todas las coincidencias como rangos) y `replace`
// (sustituye todas)—.
//
// TODAS SON [W] PERO NINGUNA ⏸ (§10, §16). Son **CPU puro**: compilan o casan
// un patrón contra un string ya en memoria, sin IO que esperar —como los
// codecs de S18 y el resto de `nu.text`—. Por eso NO usan el puente `suspend`
// ni `requireTask`: corren síncronas en el estado principal (y en workers
// cuando lleguen, S34). [W] marca "disponible en workers", no "suspende".
//
// POR QUÉ RE2 (el `regexp` de Go). La librería estándar de Go es una
// implementación de RE2: garantiza tiempo lineal sobre el tamaño de la entrada
// (sin backtracking catastrófico) a cambio de **no** soportar backreferences
// ni lookaround. Eso es exactamente lo que un harness quiere: un patrón
// venido de un agente o de la configuración NUNCA puede colgar el runtime con
// un ReDoS. El precio —no hay `\1` ni `(?=...)`— se documenta y se reporta
// como un `EINVAL` claro (el mensaje de `regexp.Compile` nombra qué construye
// no se soporta), no como un fallo silencioso.

// reTypeName identifica la metatabla del handle `Re` (lo que devuelve
// `nu.re.compile`). De ella cuelga el `__index` con `match`/`find_all`/`replace`.
const reTypeName = "nu.re.Re"

// luaRe es el contenido Go de un handle `Re`: el patrón ya compilado. El
// `*regexp.Regexp` de la stdlib es **seguro para uso concurrente** (su doc lo
// garantiza), así que un mismo `Re` puede casarse desde varias tasks sin
// candado —encaja con el modelo de concurrencia del navegador (ADR-004)—.
type luaRe struct {
	re *regexp.Regexp
}

// registerRe cuelga `nu.re` del global `nu` con `compile`, e instala la
// metatabla del tipo `Re`. Lo llama `registerNu` (nu.go).
func (rt *Runtime) registerRe(nu *lua.LTable) {
	L := rt.L
	reT := L.NewTable()
	reT.RawSetString("compile", L.NewFunction(rt.reCompile))
	nu.RawSetString("re", reT)
	rt.registerReType()
}

// registerReType instala la metatabla del tipo `Re` con `match`/`find_all`/
// `replace`. El handle es opaco: solo se opera sobre él por sus métodos, no se
// expone el `*regexp.Regexp` interno a Lua.
func (rt *Runtime) registerReType() {
	L := rt.L
	mt := L.NewTypeMetatable(reTypeName)
	methods := L.NewTable()
	methods.RawSetString("match", L.NewFunction(rt.reMatch))
	methods.RawSetString("find_all", L.NewFunction(rt.reFindAll))
	methods.RawSetString("replace", L.NewFunction(rt.reReplace))
	L.SetField(mt, "__index", methods)
}

// checkRe recupera el `*luaRe` del userdata `self` (primer argumento de los
// métodos). Lanza `EINVAL` si no es un handle de `Re`.
func checkRe(L *lua.LState) *luaRe {
	ud := L.CheckUserData(1)
	r, ok := ud.Value.(*luaRe)
	if !ok {
		raiseError(L, CodeEINVAL, "Re: se esperaba un handle de Re", lua.LNil)
		return nil
	}
	return r
}

// --- nu.re.compile ------------------------------------------------------------

// reCompile implementa `nu.re.compile(pattern) -> Re` (§10). Compila `pattern`
// como una expresión RE2 y devuelve un handle `Re`. Un patrón inválido —error
// de sintaxis o una construcción que RE2 no soporta, en particular una
// **backreference** (`\1`) o un lookaround— produce `EINVAL` con el mensaje
// de `regexp.Compile`, que nombra qué falla (p. ej. "invalid escape sequence"
// para `\1`). No suspende: la compilación es CPU puro.
func (rt *Runtime) reCompile(L *lua.LState) int {
	pattern := L.CheckString(1)
	re, err := regexp.Compile(pattern)
	if err != nil {
		// El error de la stdlib ya es accionable (cita la posición y la causa);
		// se incrusta en el mensaje para que quien capture el EINVAL sepa por qué
		// su patrón no compiló (incluido el caso de backreferences, que RE2 no
		// admite y la stdlib reporta como secuencia de escape inválida).
		raiseError(L, CodeEINVAL, "nu.re.compile: patrón inválido: "+err.Error(), lua.LNil)
		return 0
	}
	ud := L.NewUserData()
	ud.Value = &luaRe{re: re}
	L.SetMetatable(ud, L.GetTypeMetatable(reTypeName))
	L.Push(ud)
	return 1
}

// --- Re:match -----------------------------------------------------------------

// reMatch implementa `Re:match(s) -> caps?` (§10): la **primera** coincidencia
// de `s`. Sin coincidencia → `nil` (no lanza: no casar es un resultado válido,
// no un error). Con coincidencia, devuelve una tabla con la **forma de
// capturas** documentada (claude_decisions.md S26):
//
//   - Parte array, 1-based (estilo Lua): `[1]` es la coincidencia COMPLETA
//     (el grupo 0), `[2]` el primer grupo, `[3]` el segundo, etc. Así
//     `caps[1]` es siempre el match entero aunque el patrón no tenga grupos.
//   - Grupos con nombre (`(?P<name>...)`) ADEMÁS por su nombre como clave
//     string: `caps.name`. Conviven con la parte array (un grupo con nombre
//     aparece dos veces: por su índice posicional y por su nombre), lo que
//     deja a Lua acceder al grupo como prefiera.
//
// Un grupo opcional que NO participó en la coincidencia (p. ej. `(a)?` sin
// "a") se entrega como `""` (string vacío). RE2/`FindStringSubmatch` no
// distingue "grupo vacío" de "grupo ausente" en su salida de strings; el
// string vacío es la representación natural en Lua (donde no hay `nil` en un
// array sin agujerearlo).
func (rt *Runtime) reMatch(L *lua.LState) int {
	r := checkRe(L)
	if r == nil {
		return 0
	}
	s := L.CheckString(2)

	sub := r.re.FindStringSubmatch(s)
	if sub == nil {
		L.Push(lua.LNil)
		return 1
	}

	caps := L.NewTable()
	// Parte array 1-based: [1] = match completo (grupo 0), [2..] = grupos.
	for i, g := range sub {
		caps.RawSetInt(i+1, lua.LString(g))
	}
	// Grupos con nombre por su clave string. SubexpNames() alinea índices con
	// la salida de FindStringSubmatch; el índice 0 y los grupos sin nombre
	// tienen "" como nombre y se omiten.
	for i, name := range r.re.SubexpNames() {
		if name != "" && i < len(sub) {
			caps.RawSetString(name, lua.LString(sub[i]))
		}
	}
	L.Push(caps)
	return 1
}

// --- Re:find_all --------------------------------------------------------------

// reFindAll implementa `Re:find_all(s) -> ranges` (§10): **todas** las
// coincidencias (no solapadas, de izquierda a derecha) de `s`, como una tabla
// array de rangos. Sin coincidencias → tabla vacía.
//
// FORMA Y UNIDADES (claude_decisions.md S26). Cada rango es una tabla
// `{start, end}` con **offsets de BYTE, 1-based, ambos inclusive** —el mismo
// convenio que `string.find` de Lua—, de modo que `s:sub(start, end)`
// reconstruye exactamente la coincidencia. Se eligen bytes (no runes) porque
// (a) es lo que `string.sub` indexa en Lua, así que el rango es directamente
// utilizable, y (b) `FindAllStringIndex` de Go ya devuelve offsets de byte;
// convertir a runes obligaría a recontar y rompería la composición con
// `string.sub`. Una coincidencia VACÍA (p. ej. el patrón `a*` sobre "bbb")
// produce un rango con `end = start - 1` (longitud cero), coherente con que
// `s:sub(start, start-1)` es "" en Lua.
//
// Solo se devuelven los rangos de la coincidencia COMPLETA (no los de cada
// grupo): es el caso común (resaltar/localizar dónde casa el patrón) y mantiene
// la firma simple. Quien necesite las capturas de cada coincidencia las saca
// con `match` sobre el tramo, o el caso se cubre en una futura adición si el
// patrón se repite (no se especula API, §api sagrada).
func (rt *Runtime) reFindAll(L *lua.LState) int {
	r := checkRe(L)
	if r == nil {
		return 0
	}
	s := L.CheckString(2)

	// -1 = sin límite de coincidencias. Devuelve pares [inicio, fin) en BYTES,
	// fin exclusivo (convenio de Go).
	idxs := r.re.FindAllStringIndex(s, -1)
	ranges := L.NewTable()
	for i, pair := range idxs {
		rg := L.NewTable()
		// Go da [inicio, fin) 0-based con fin exclusivo. A Lua: start = inicio+1
		// (1-based), end = fin (que ya apunta al último byte en 1-based inclusive,
		// porque fin exclusivo 0-based == último byte 1-based inclusive). Una
		// coincidencia vacía (inicio==fin) da end = start-1 → s:sub(start,end)=="".
		rg.RawSetInt(1, lua.LNumber(pair[0]+1))
		rg.RawSetInt(2, lua.LNumber(pair[1]))
		ranges.RawSetInt(i+1, rg)
	}
	L.Push(ranges)
	return 1
}

// --- Re:replace ---------------------------------------------------------------

// reReplace implementa `Re:replace(s, repl) -> string` (§10): sustituye
// **todas** las coincidencias no solapadas de `s` por `repl`, devolviendo el
// string resultado. Sin coincidencias → `s` sin cambios.
//
// SINTAXIS DE `repl` (la de Go `Regexp.ReplaceAllString`, claude_decisions.md
// S26): `$1`, `$2`, ... refieren a los grupos por número; `${name}` a los
// grupos con nombre; `$0` (o `${0}`) a la coincidencia completa; `$$` es un
// `$` literal. Un nombre no delimitado por llaves se extiende hasta el último
// carácter alfanumérico (`$1x` busca el grupo "1x", no el grupo 1 seguido de
// "x"): se recomienda `${1}x` para evitar la ambigüedad —el mismo matiz que
// documenta la stdlib—. Una referencia a un grupo inexistente se reemplaza por
// vacío (comportamiento de la stdlib).
func (rt *Runtime) reReplace(L *lua.LState) int {
	r := checkRe(L)
	if r == nil {
		return 0
	}
	s := L.CheckString(2)
	repl := L.CheckString(3)
	L.Push(lua.LString(r.re.ReplaceAllString(s, repl)))
	return 1
}
