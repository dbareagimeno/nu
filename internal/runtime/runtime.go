// Package runtime levanta el intérprete Lua del core de nu: construye el estado
// gopher-lua, aplica el baseline del sandbox (api.md §1.2), inyecta el global
// `nu` y expone la evaluación de código. Es la quilla sobre la que las sesiones
// posteriores cuelgan cada submódulo de la API (task, fs, http, ...).
package runtime

import (
	lua "github.com/yuin/gopher-lua"
)

// Runtime envuelve un estado Lua ya sandboxeado y con el global `nu` inyectado.
// El estado principal es single-threaded (ADR-004); un Runtime se usa desde una
// sola goroutine.
type Runtime struct {
	L *lua.LState
}

// New construye un Runtime listo para ejecutar Lua: abre solo las librerías
// permitidas por el baseline (§1.2), recorta `os`, elimina `io`/`dofile`/
// `loadfile`, redirige `print` e inyecta el global `nu` con sus submódulos
// disponibles en esta sesión.
func New() *Runtime {
	// SkipOpenLibs: abrimos a mano solo lo que el baseline permite, en vez de
	// abrir todo y desactivar después; así una librería peligrosa nueva de
	// gopher-lua no entra por defecto (deny-by-default, coherente con las caps
	// de los workers, §13).
	L := lua.NewState(lua.Options{SkipOpenLibs: true})

	rt := &Runtime{L: L}
	applySandbox(L)
	registerNu(L)
	return rt
}

// Close libera el estado Lua subyacente.
func (rt *Runtime) Close() {
	rt.L.Close()
}
