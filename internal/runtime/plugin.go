package runtime

// Superficie Lua del loader (api.md §14): `nu.plugin.current/list` y
// `nu.config.dir/data_dir`. Es glue de paso sobre el estado del loader (loader.go):
// la lógica clave (orden topológico, unicidad de nombre, arranque canónico) vive
// allí y se blinda con tests Go; aquí solo se expone a Lua.
//
// `nu.plugin` es **solo estado principal** (§16): el ciclo de vida de plugins no
// existe en un worker. `nu.config.dir`/`data_dir` SÍ son **[W]**: un worker puede
// necesitar saber dónde vive la config/los datos (p. ej. para componer rutas), y
// son funciones puras que devuelven un string fijo.

import (
	lua "github.com/yuin/gopher-lua"
)

// registerPlugin cuelga `nu.plugin` (current/list) y `nu.config` (dir/data_dir) del
// global `nu`. Lo llama `registerNu` (nu.go).
func (rt *Runtime) registerPlugin(nu *lua.LTable) {
	L := rt.L

	plugin := L.NewTable()
	plugin.RawSetString("current", L.NewFunction(rt.pluginCurrent))
	plugin.RawSetString("list", L.NewFunction(rt.pluginList))
	nu.RawSetString("plugin", plugin)

	config := L.NewTable()
	config.RawSetString("dir", L.NewFunction(rt.configDir))
	config.RawSetString("data_dir", L.NewFunction(rt.configDataDir))
	nu.RawSetString("config", config)
}

// pluginCurrent implementa `nu.plugin.current() -> {name, version, dir}` (§14): el
// plugin en cuyo contexto corre el código ahora mismo. Durante el `init.lua` de un
// plugin es ese plugin (el loader lo empujó al `ownerStack`); fuera de todo plugin
// —el chunk de `-e`, el `init.lua` del usuario, un handler sin plugin dueño—
// devuelve el contexto del usuario `{name="user", version="", dir=config.dir}`. Así
// `current()` nunca es `nil`: siempre hay un contexto, aunque sea el del usuario.
func (rt *Runtime) pluginCurrent(L *lua.LState) int {
	t := L.NewTable()
	if n := len(rt.ownerStack); n > 0 {
		p := rt.ownerStack[n-1]
		t.RawSetString("name", lua.LString(p.Name))
		t.RawSetString("version", lua.LString(p.Version))
		t.RawSetString("dir", lua.LString(p.Dir))
	} else {
		// Contexto del usuario/core: no es un plugin del disco, pero su "dir" natural
		// es el directorio de config (de donde sale su `init.lua`).
		t.RawSetString("name", lua.LString(ownerUser))
		t.RawSetString("version", lua.LString(""))
		t.RawSetString("dir", lua.LString(rt.ldr.configDir))
	}
	L.Push(t)
	return 1
}

// pluginList implementa `nu.plugin.list() -> {name, version, source, enabled}[]`
// (§14): los plugins cargados, en el orden topológico en que corrieron. En S11
// todos son `source="user"`, `enabled=true`; S12 añade las embebidas ("builtin") y
// la activación por `nu.toml`.
func (rt *Runtime) pluginList(L *lua.LState) int {
	arr := L.NewTable()
	for i, p := range rt.ldr.ordered {
		entry := L.NewTable()
		entry.RawSetString("name", lua.LString(p.Name))
		entry.RawSetString("version", lua.LString(p.Version))
		entry.RawSetString("source", lua.LString(string(p.Source)))
		entry.RawSetString("enabled", lua.LBool(p.Enabled))
		arr.RawSetInt(i+1, entry)
	}
	L.Push(arr)
	return 1
}

// configDir implementa `nu.config.dir() -> string` [W] (§14): `~/.config/nu` (o el
// equivalente por plataforma / lo fijado por `WithConfigDir`).
func (rt *Runtime) configDir(L *lua.LState) int {
	L.Push(lua.LString(rt.ldr.configDir))
	return 1
}

// configDataDir implementa `nu.config.data_dir() -> string` [W] (§14):
// `~/.local/share/nu`. Promociona la `defaultDataDir` de S03 (log.go), ahora
// también superficie pública.
func (rt *Runtime) configDataDir(L *lua.LState) int {
	L.Push(lua.LString(rt.ldr.dataDir))
	return 1
}
