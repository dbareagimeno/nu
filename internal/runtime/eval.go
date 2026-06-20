package runtime

import (
	lua "github.com/yuin/gopher-lua"
)

// EvalString compila y ejecuta `code` como un chunk Lua y devuelve sus valores
// de retorno convertidos a string (vía `tostring`), en orden. Es lo que respalda
// `nu -e`: el chunk `return nu.version.api` produce `["1"]`. Un error de compilación
// o de ejecución se devuelve tal cual (en sesiones futuras será una tabla de
// error estructurada, §1.4; eso es S02).
func (rt *Runtime) EvalString(code string) ([]string, error) {
	L := rt.L

	fn, err := L.LoadString(code)
	if err != nil {
		return nil, err
	}

	base := L.GetTop()
	L.Push(fn)
	if err := L.PCall(0, lua.MultRet, nil); err != nil {
		return nil, err
	}

	n := L.GetTop() - base
	results := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		v := L.Get(base + i)
		results = append(results, L.ToStringMeta(v).String())
	}
	L.SetTop(base)
	return results, nil
}
