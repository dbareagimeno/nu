---
title: "El binario vive en `cmd/enu/`; la raíz del repo queda sin código Go (refina la ubicación de ADR-026)"
type: "adr"
id: "ADR-031"
status: "aceptada"
date: "2026-07-19"
---
# ADR-031 · El binario vive en `cmd/enu/`; la raíz del repo queda sin código Go

**Estado:** Aceptada · 2026-07-19 (**refina** la *ubicación* de la superficie
CLI de [ADR-026](adr-026-subcomandos-de-gestion-del-binario.md) y complementa
[ADR-013](adr-013-integracion-continua-y-publicacion.md); no toca la frontera
gestión/producto ni la API sagrada)

**Contexto.** El `package main` del binario vivía **suelto en la raíz del
repo**: 5 fuentes (`main.go`, `doctor.go`, `init.go`, `update.go`,
`uninstall.go`) y 6 tests (`main_*_test.go`), junto a `go.mod`, `README`,
`LICENSE` y las carpetas del proyecto. No es la disposición idiomática de Go
(golang-standards/project-layout): un binario se ubica bajo `cmd/<nombre>/`, y
la raíz queda para el módulo y los metadatos. Además los tests llevaban el
prefijo plano `main_` (`main_doctor_test.go`) en vez de casar con su fuente. El
`internal/` (kernel: `runtime`, `vmwasm`) y `e2e/` ya seguían la convención;
solo la raíz desentonaba. No hay más ejecutables: es un único binario.

**Decisión.** Mover el `package main` completo a **`cmd/enu/`**, sin partirlo en
paquetes:

1. **Las 5 fuentes** se mueven a `cmd/enu/` con `git mv` (preserva historia).
   Siguen siendo `package main`; no cambian imports (los de `internal/runtime`
   son por ruta de módulo, no relativos) ni hay `go:embed` en la raíz (el embed
   vive en `internal/runtime`).
2. **Los 6 tests** se mueven y se **renombran para casar con su fuente**:
   `main_doctor_test.go`→`doctor_test.go`, `main_init_test.go`→`init_test.go`,
   `main_update_test.go`→`update_test.go`,
   `main_uninstall_test.go`→`uninstall_test.go`,
   `main_version_test.go`→`version_test.go`; `main_test.go` se conserva.
3. **El build pasa de apuntar al paquete raíz `.` a `./cmd/enu`.** Se actualizan
   todas las invocaciones que compilaban/ejecutaban la raíz: `ci.yml`,
   `release.yml`, `smoke-instalacion.yml`, `docker/Dockerfile`,
   `docker/docker-compose.yml`, el harness de `e2e/`, `install.sh`, la doc de
   instalación de la web (ES/EN) y los ejemplos (`go run ./cmd/enu`).
   `go build ./...`, `gofmt -l .`, `go vet ./...` y golangci sobre todo el repo
   **siguen intactos**: `cmd/enu` es un paquete más bajo `./...`.
4. **No se toca `internal/`, ni `e2e/`, ni la API `enu.*`** (`enu.version.api`
   intacto): es un movimiento de ficheros y del *target* de build, nada de
   semántica.

**Razonamiento.**
- **Por qué `cmd/enu/` y no `internal/cli/` + `main.go` fino.** Es un único
  binario; partir el `package main` en un paquete `cli` exportado añadiría
  fronteras y superficie exportada sin beneficio hoy. La convención Go para
  «un binario» es `cmd/<nombre>/`; la de «lógica reutilizable/testeable como
  paquete» es `internal/…`, y esa necesidad no existe (los tests ya viven junto
  al código en `package main`). Se elige lo mínimo idiomático.
- **Por qué renombrar los tests.** `main_doctor_test.go` era un vestigio del
  layout plano; junto a su fuente, `doctor_test.go` es lo que espera un lector
  de Go.
- **Por qué no reescribir la prosa congelada.** ADR-026 §«la superficie CLI
  vive en `main.go`» y otros textos históricos (auditorías cerradas, worklog,
  el ejemplo de ADR-013) **no se reescriben** (los ADR nunca se reescriben):
  este ADR refina la *ubicación* y el lector la resuelve aquí. La única prosa
  **viva** que lo afirmaba, `docs/core/arquitectura.md`, sí se actualiza a
  `cmd/enu/`.

**Consecuencias.**
- Raíz del repo sin `.go`: solo `go.mod`/`go.sum`, metadatos (`README*`,
  `LICENSE`, `NOTICE`, `CONTRIBUTING.md`, `CLAUDE.md`), `install.sh` y las
  carpetas (`cmd docker docs e2e examples internal web .github .claude`).
- La frontera gestión/producto de ADR-026 (pieza 1) **no cambia**: el binario
  sigue siendo la superficie CLI que orquesta extensiones por la API pública; se
  ha movido de sitio, no de rol.
- `release.yml` sigue produciendo el mismo artefacto `enu`; solo cambia el
  *target* de `go build`.
- **Disparador de reapertura:** si el proyecto pasara a tener **varios
  ejecutables**, cada uno entra como `cmd/<nombre>/`; si la lógica de
  subcomandos necesitara **compartirse o testearse como paquete** fuera de
  `package main`, se extraería a `internal/cli/` — hoy es innecesario y se evita
  por YAGNI.
