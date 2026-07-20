---
title: "Elección por teclado en la pantalla de runtime desnudo (Fase 10 — Convenciones CLI)"
type: "sesion"
id: "S54"
phase: 10
status: "cerrada"
---
# S54 — Elección por teclado en la pantalla de runtime desnudo (Fase 10)

**Qué es.** Completa la activación «de una tecla» de **G21/ADR-010** que S33 dejó
pendiente: la pantalla de runtime desnudo (TTY + ningún plugin activo) pintaba sus
tres acciones (`api.md §14`) pero solo respondía a salir. Ahora las tres responden
al teclado. **No amplía `api.md`** (`enu.version.api` intacto): es lógica de *input
del driver*, del binario. Sesión **🔒**. Depende de S31/S33/CP-7 (todas cerradas).

**Qué se entregó.**
- `internal/runtime/bare_screen.go`: la máquina de estados `bareScreen` (menú ↔
  selección), en Go —nunca como widget de `enu.ui`, §14—. `handleKey` mapea `1`
  (conjunto oficial, `officialProductSet`, ADR-015), `2` (modo selección del
  catálogo de embebidas, cursor `↑/↓`·`j/k`, `enter` activa solo la elegida, `esc`
  vuelve), `3`/`q`/`esc`/`ctrl+c` (salir). Cursor **acotado** (catálogo vacío
  incluido), latch de **re-entrada** (`activated`/`done`: una sola activación),
  error de activación **pintado** (ADR-017). `render` recrea UNA región (no
  acumula); `teardown` la suelta tras activar (ver más abajo).
- `internal/runtime/driver.go`: `PrepareBareScreen` instala la red de salida de
  emergencia (`installKernelExitWasm`, al fondo) + el **reenviador** `installBareInput`
  (encima). `pollBareAction` (gemelo de `pollWasmQuit`) lee la tecla que el
  reenviador anota en `_G.__bare_key` y conduce la máquina Go desde `feed`, por
  evento. `startPump`/`stopPump` hacen arrancable/parable el bombeo del scheduler.
- `internal/runtime/runtime.go`: campo `Runtime.bare *bareScreen`.
- `internal/runtime/bare_screen_s54_test.go`: los tests 🔒 (unidad de `handleKey`
  con `activate` espía + integración por driver con tuberías).

**Decisión de diseño clave — el puente flag+poll (anti-deadlock).** La vía natural
«handler Lua → `activateAndBoot`» **no vale**: un HostFn síncrono corre bajo
`inst.mu`, y `activateAndBoot`→`Boot`→`BootWasm` re-entra la VM vía `Eval` →
deadlock. Se replica el patrón **flag + poll** del quit (`installShutdownHandler`/
`pollWasmQuit`): el `on_input` Lua es un reenviador tonto que anota la tecla; la
lógica corre en Go desde `feed`, con `inst.mu` libre entre `FeedInput`s. El estado
de la elección (modo, cursor, latch) vive en `bareScreen` (Go), como exige §14.

**Hallazgos del juicio clean-room (panel completo) y sus arreglos.** El panel
inicial destapó tres grietas reales que los tests de la primera pasada no cazaban
(ninguno conducía una activación con éxito bajo el driver):
- **C1 [crítico, concurrencia].** `BootWasm` termina en `RunTasks`, **reentrante**
  con el bombeo continuo (`PumpTasks`) que `drive` mantiene vivo (`pumpActive`).
  Activar con un `enu.toml` válido habría fallado **siempre** con el error de
  reentrancia, dejando la pantalla en estado inválido (producto cargado pero
  «activación fallida»). **Fix:** `drive` envuelve `bs.activate` para **parar el
  pump antes** del reboot y **rearrancarlo después**.
- **C2 [alto, concurrencia].** Rebootear desde la goroutine del driver con el pump
  vivo corría `rt.ldr`/`rt.ownerStack` (que el arranque muta sin candado) contra el
  pump. El mismo `stopPump` lo cierra: el reboot queda single-thread.
- **H1 [alto, espec/ADR-017].** Activar una embebida SIN salida propia (`example`/
  `mesh`, o un plugin cuyo init falla) atrapaba la terminal: el reenviador quedaba
  como único handler, inerte tras `done`. **Fix:** `PrepareBareScreen` instala la
  red de emergencia al fondo (como el camino normal), y el reenviador se vuelve
  **transparente** (`_G.__bare_done`) tras activar con éxito.
- **Bleed-through de región** (observación del juez de concurrencia, fuera de su
  mandato). Tras `done`, la región del menú (z=0) no se soltaba. **Fix:** `teardown`
  la elimina del compositor al activar.

Re-juicio en clean-room fresco tras los arreglos: **concurrencia CONFORME**,
**espec CONFORME**. Juez de tests: **SUFICIENTE** (inventario 🔒 satisfecho); se
añadieron además los dos gaps de cobertura baja que señaló (atajos `q`/`ctrl+c` en
selección; alias `j`).

**Mutación (🔒, gremlins modo diff).** Endurecida a **96.97%** (32 KILLED, 1 LIVED,
0 not covered). El único superviviente es un **mutante equivalente probado**:
`bare_screen.go` `moveCursor`, `if bs.cursor < 0` → `<= 0`. El guard previo
garantiza `len(catalog) ≥ 1`, y acotar 0 a 0 es idempotente: ninguna entrada
produce comportamiento observable distinto (confirmado por el juez de tests). No es
hueco de tests; no se blinda.

**DoD.** `CGO_ENABLED=0 go build ./...` verde; `gofmt`/`go vet` limpios; `go test
-race -shuffle=on ./internal/runtime/` y la suite completa verdes, sin regresiones.
La conformidad la confirmó el re-juicio (espec + concurrencia CONFORME). Lo que
queda es **irreductiblemente manual** (CP-7 con TTY): ver la elección y la
transición a producto en un terminal real.

**Para la siguiente.** Con S54, **CP-7 manual queda ejecutable completo** (teclado
+ activación + salida). El puntero vuelve a `—`: la Fase 10 sigue como pista viva
(futuras convenciones CLI entran por `/planificar-sesion`).
