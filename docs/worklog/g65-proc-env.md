---
title: "G65: `opts.env` dual (tabla o array POSIX) + EINVAL para lo malformado en `enu.proc` (api = 6); la fusión de entorno se escinde a P55"
type: "sesion"
id: "G65"
status: "cerrada"
date: "2026-07-22"
---
# G65 — `opts.env` dual + EINVAL en `enu.proc` (api = 6)

Resolución del hallazgo
[G65](../findings/g65-proc-spawn-ignora-env-array-en-silencio.md) (ciclo
`/hallazgo` con implementación). Decisiones operativas:

**Decisión del operador.** Opciones **(d)+(e)** compuestas: forma dual de
`opts.env` (tabla `{K=V}` o array POSIX `["K=V"]`) + fail-closed `EINVAL` para
el env malformado. La dimensión reemplazo-vs-fusión queda **fuera** y se escinde
al nuevo [P55](../postponed/p55-fusion-de-entorno-en-proc.md) con su disparador.

**Juez de filosofía: APROBADA CON MATICES (4, incorporados).** M1 — (d) no se
vende como corolario de completitud (la conversión ya se componía en Lua:
`normalize_env`); se justifica por vocabulario POSIX legítimo + el parser se
tocaba igualmente por (e). M2 — la semántica «presente aunque vacío REEMPLAZA»
se escribe por fin en §6 (vivía en worklogs y contratos satélite). M3 — bordes
fijados en §6: split por el primer `=`, last-wins entre claves repetidas, array
vacío = reemplazo-con-vacío. M4 — la dimensión diferida gana hogar propio (P55).
Sin ADR: la permisividad rota vivía solo en el comentario de cabecera del
parser, nunca en `api.md` (ADR-025 pieza 4 solo exige ADR para roturas de firma).

**Decisiones de implementación.**

- **Sin dedupe manual en el parser**: `mergedEnv` (proc.go) ya colapsa claves
  repetidas last-wins con su índice `put` — no dependemos del dedupe de
  `os/exec`, y el array pasa tal cual a `opts.env`.
- **Caso borde `{}`**: la tabla Lua vacía cruza el wire como array vacío (rama
  `[]any`); pasa de «ignorada → heredaba» (bug latente contra la letra de §6) a
  reemplazo-con-vacío.
- **mcp conserva `normalize_env`** como validación temprana por-servidor (sus
  errores `emcp` nombran el servidor; el `pcall` por-servidor degrada solo ese
  servidor; resuelve la tabla mixta que la frontera no distingue) y, nuevo,
  devuelve `nil` ante un env declarado VACÍO — vacío = «nada que añadir» =
  heredar — para que un `env = []` en `mcp.toml` no mate al servidor por perder
  `PATH` con el reemplazo-con-vacío nuevo del primitivo.

**Panel `revision-limpia`** (jueces espec/tests/concurrencia + verificador por
hallazgo): espec y concurrencia CONFORMES; tests encontró **3 reales** (media),
los tres cerrados en esta misma rama: (1) fila **🔒 G65 (kernel)** añadida a
[inventario-tests.md](../plan/inventario-tests.md); (2) caso `{"=x"}` (clave
vacía en array, la mitad `eq == 0` de la guardia `eq <= 0`) añadido al subtest
EINVAL — el mutante `eq <= 0`→`eq < 0` muere (verificado por reversión); (3)
e2e nuevo `TestMcpE2EConfiguredEnvEmptyInheritsG65`: `env = []` en `mcp.toml` →
el servidor HEREDA (el spawn recibe `nil`) — el mutante «eliminar `n==0→nil`»
muere (verificado por reversión). El auditor de docs detectó además dos desfases
en `estado.md` (backlog con G65 en presente; «APILevel en 2», roto desde G52),
corregidos.

**Mutación (gremlins v0.6.0, acotada a `vmwasm_proc.go`)**: 37 runnable → **34
KILLED, 2 LIVED, 1 TIMED OUT, 6 NOT COVERED** (eficacia 94,4%). Diagnóstico de
los LIVED: (a) `295:123` (el `i+1` del índice 1-based en el MENSAJE de error del
array) — **mutante equivalente a efectos del contrato** (solo cosmética del
mensaje; el código EINVAL, que es lo contratado, se afirma), no re-investigar;
(b) `312:11` (`tm < 0` → `tm <= 0` en `timeout_ms`) — hueco **preexistente**
(no de la rama env), cerrado aquí con `TestProcWasmTimeoutMsCero` (muerte
verificada por reversión). El TIMED OUT (`194:27`, negación del guard de
`Proc:kill`) cuelga la suite al no matar el proceso → cuenta como detectado.
Los NOT COVERED (`153`, `159`, `199`) son rutas viejas de `Proc:read` (n
negativo, envoltura EIO) y `Proc:kill` (float no entero) fuera del alcance G65:
wrappers finos preexistentes, anotados y sin acción.

**Sync-web**: `referencia/proc.md` (ES+EN) gana la sección «El entorno:
`opts.env`»; ejemplos verificados contra el binario real (`-e` con escritura a
fichero: el `return` del chunk corre ANTES del drenaje y `print` en tasks no
llega a stdout — el patrón de la propia página). check-drift a cero (sin firmas
nuevas) y build de Astro verde.

**Verificación**: `go build ./...` · `go test -race ./internal/runtime/` ·
`go test -race ./e2e/ -run Mcp` · suite completa `-race` al cierre — todo verde.
