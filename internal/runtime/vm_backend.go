package runtime

import "os"

// VMBackend selecciona el motor de VM sobre el que corre el estado Lua del
// Runtime (migracion-vm.md M04, DM2). Es el eje del **patrón estrangulador**: el
// backend `wasm` (PUC-Lua oficial sobre wazero, internal/vmwasm) se construye en
// paralelo al `gopher` actual, detrás de este selector, hasta que la paridad sea
// total (M15) y la conmutación (M16) cambie el default. gopher-lua se retira en
// M17 y este selector con él.
type VMBackend int

const (
	// VMGopher es el backend actual (gopher-lua). Default hasta M16.
	VMGopher VMBackend = iota
	// VMWasm es el backend nuevo (PUC-Lua sobre wazero). Seleccionable ya, pero su
	// superficie nu.* se completa a lo largo de M05-M13.
	VMWasm
)

func (b VMBackend) String() string {
	if b == VMWasm {
		return "wasm"
	}
	return "gopher"
}

// parseVMBackend traduce un nombre ("gopher"|"wasm") a VMBackend. Desconocido o
// vacío → gopher (el default seguro hasta M16).
func parseVMBackend(name string) VMBackend {
	if name == "wasm" {
		return VMWasm
	}
	return VMGopher
}

// resolveVMBackend fija el backend de esta construcción con la precedencia de
// DM2: la variable de entorno `NU_VM` (la vía de los tests: `NU_VM=wasm go test`)
// gana sobre `nu.toml [vm] backend`, que gana sobre el default (gopher). La
// Option `WithVMBackend` (si se pasó) gana sobre todo (precedencia de test
// explícito, como el resto de Options).
func resolveVMBackend(cfg *config, tomlBackend string) VMBackend {
	if cfg.vmBackendSet {
		return cfg.vmBackend
	}
	if env := os.Getenv("NU_VM"); env != "" {
		return parseVMBackend(env)
	}
	if tomlBackend != "" {
		return parseVMBackend(tomlBackend)
	}
	return VMGopher
}
