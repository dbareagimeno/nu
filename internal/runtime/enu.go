package runtime

// Versión del runtime y nivel de la API del core (§2). `APILevel` se incrementa
// con cada adición a la superficie sagrada (api.md §17); arrancó en 1 con la
// primera sesión que inyecta `enu`. Subió a 2 en S38 al añadir `enu.sys.pid()`
// (G32): la PRIMERA adición tras el congelado inicial — adición estricta, no
// rompe ninguna firma del nivel 1.
//
// Subió a 3 al añadir los frames binarios de `enu.ws` (G52/A-38): `opts.binary` en
// `Ws:send` y el segundo retorno `binary` de `Ws:recv` — adición estricta, no rompe
// ninguna firma del nivel 2 (todo llamante existente ignora lo nuevo).
//
// Subió a 4 con el control de redirects de `enu.http` (G54: `opts.max_redirects` en
// `request`/`stream`) y a 5 con el modo de creación de `enu.fs.write` (G57:
// `opts.mode`, chmod explícito no recortado por el umask) — ambas adiciones
// estrictas. Nota: el nivel 4 (G54) se construye en su propia rama; esta rama salta
// de 3 a 5 al integrar G57, y el conflicto de este literal y de api.md §17 se
// reconcilia en el merge (ambas convergen en 5).
//
// El catálogo `enu.*` lo monta el backend wasm (registerWasmCatalog en runtime.go
// + los preludios de internal/vmwasm); estas constantes las inyecta el preludio
// vía `Pool.SetAPIVersion`/`Pool.SetVersion` (buildWasmState).
const (
	VersionMajor = 0
	VersionMinor = 1
	VersionPatch = 4
	APILevel     = 5
)
