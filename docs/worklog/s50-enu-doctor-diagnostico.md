---
title: "`enu doctor`: diagnóstico de solo lectura, 7 checks kernel (4 de producto skip por G62) (Fase 9, ADR-026 pieza 3)"
type: "sesion"
id: "S50"
phase: 9
status: "cerrada"
---
# S50 — `enu doctor` (Fase 9 — Producto)

**Qué es.** El segundo subcomando de gestión ([ADR-026](../decisions/adr/adr-026-subcomandos-de-gestion-del-binario.md)
pieza 3): un diagnóstico de solo lectura del binario y su config, con salida
humana o `--json` conforme al esquema `doctor.v1`
([doctor.md](../ops/doctor.md)). Depende de S49 (el dispatcher). Superficie CLI
(`package main`), no API sagrada.

**Estrechado por [G62](../findings/g62-los-checks-de-producto-de-doctor-presuponen-introspeccion-inexistente.md).**
El escenarista BDD destapó que los 4 checks de **producto** (`provider.model`,
`provider.key`, `tools.external`, `provider.reach`) presuponen consultar la
semántica de una extensión **sin efectos** —hoy solo `Boot()` invoca Lua, y
arranca todos los `init.lua`— y, `tools.external`, una API de declaración de
herramientas externas que no existe. G62 se resolvió estrechando `enu doctor`
v1 a los **7 checks kernel**; los 4 de producto salen como `skip` honesto
(`detail` apunta a G62, `remedy: null`), con el diseño diferido a **P45**. La
resolución se aplicó a los documentos ANTES de este código (commit previo).

**Qué se entregó.**
- `doctor.go` (nuevo): `runDoctorMain` (parsea `--json`/`--net`), `runDoctor`
  (núcleo testeable), `collectDoctorChecks` (los 11 en orden de catálogo), los
  siete `checkXxx` kernel, los cuatro skip de producto, y `writeDoctorHuman`.
  Los 7 kernel: `binary.version` (desde los símbolos del binario, **sin**
  `--version` —que no existe, S48/ADR-027—), `config.dir` (ausencia = ok,
  runtime desnudo ADR-010), `config.parse` (sintaxis TOML de los tres
  ficheros), `plugins.enabled`/`plugins.requires`, `sessions.perms` (el `0600`
  de G57), `tty.caps`.
- `internal/runtime/doctor_support.go` (nuevo, package runtime): `ConfigDir()`,
  `DataDir()` y `DiagnosePluginGraph()` — que envuelve `discover()`+`topoSort()`
  del loader **sin `Boot()`** (cero `init.lua`; reusa el loader, no
  re-implementa su semántica). Es la clave de la regla anti-duplicación.
- `init.go`: el `case "doctor"` del dispatch pasa de «reservado» a
  `runDoctorMain`.
- `main_doctor_test.go` (nuevo): esquema `doctor.v1`, exit 0/1/2, cada check
  kernel verde+rojo (+ skip donde aplica), la clave jamás en salida (humana ni
  `--json`), los 4 de producto skip, `--net`.

**Decisiones operativas (bajo umbral de G##).**
1. **`skip` lleva la pista en `detail`, no en `remedy`** (para no contradecir
   la regla del esquema «`remedy` solo en `fail`»). Se corrigió `doctor.md`
   (y, tras el juicio, la prosa RESUELTO de G62, que había quedado desalineada).
2. **`config.parse` es validación de kernel, no de producto**: comprueba la
   *sintaxis* TOML (con la librería del kernel), no la semántica de las
   extensiones, así que no cae bajo la regla anti-duplicación. Un `id`, `detail`
   lista los tres ficheros, `remedy` nombra el/los roto(s).
3. **Dos bugs cazados durante los tests:** (a) `l.enabled` se puebla en `New()`
   al leer `enu.toml`, así que un test de `plugins.enabled` debe escribir
   `enu.toml` **antes** de crear el runtime (en producción `enu doctor` arranca
   un proceso fresco con el `enu.toml` ya en disco — correcto); (b) el routing
   test de S49 tenía `doctor` como «reservado», ahora implementado — se quitó
   de esa tabla (su enrutado lo cubre `TestDoctorUsageExit2` con XDG aislado).

**DoD.** `go build ./...` verde; `gofmt`/`go vet` limpios; `go test -race
-shuffle=on ./ ./internal/runtime/` verde (sin regresiones, incluidos los tests
de S49). **Juicio clean-room** (juez-espec): **CONFORME** — ninguna refutación
prospera en las 8 reglas (esquema `doctor.v1`, los 11 `id` en orden, política
`detail`/`remedy` null, estrechamiento G62 con skip real, códigos 0/1/2,
clave-nunca-en-salida, sin red / sin `Boot()` / anti-duplicación, `binary.version`
sin `--version`, `config.dir` ausente = ok).

**Mutación 🔒 diferida a CP-12** (batch de la Fase 9, decisión del operador):
la pasada de `/mutacion` sobre `doctor.go` + `DiagnosePluginGraph` se corre junto
con la de S49 y S51 al cierre de la fase. Anotado como deuda; CP-12 la salda.
