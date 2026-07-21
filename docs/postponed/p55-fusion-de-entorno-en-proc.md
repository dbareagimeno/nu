---
title: "P55 — Fusión de entorno en `enu.proc`: hoy `opts.env` presente REEMPLAZA el heredado y no hay `enu.sys.environ()` para fusionar en Lua; un servidor de `mcp.toml` que declara un solo secreto pierde PATH/HOME"
type: "pospuesto"
id: "P55"
status: "vigente"
---
# P55 · Fusión de entorno en `enu.proc`: reemplazo-vs-fusión y la ausencia de `enu.sys.environ()`

**Dónde se pospuso.** Resolución de [G65](../findings/g65-proc-spawn-ignora-env-array-en-silencio.md)
(2026-07-21): el operador acotó la resolución al eje (d)+(e) —forma dual de
`opts.env` + `EINVAL` para lo malformado— y dejó fuera, deliberadamente, la
dimensión reemplazo-vs-fusión que la propia ficha había aflorado.

**Por qué.** Un `env` **presente** (aunque vacío) **reemplaza** el entorno
heredado ([api.md](../contracts/api.md) §6: control total por llamada) — semántica
correcta como primitivo, pero footgun para el caso declarativo: un servidor de
`mcp.toml` que declare `env = ["API_KEY=..."]` pierde `PATH`/`HOME` heredados, y
un servidor node/npx/python (que necesita `PATH` para encontrar su intérprete)
**se rompe al declarar un solo secreto** — justo el caso de uso que G59 quería
habilitar. Hoy no se puede fusionar en Lua: `enu.sys.env(name)` lee **una**
variable y el overlay de `enu.sys.setenv` es proceso-global (filtrar el secreto a
todo subproceso iría contra G55); falta o un lector del entorno completo
(`enu.sys.environ()`) o un modo «merge» en `opts.env` — ambas, adiciones a la
superficie sagrada que no se justifican sin el caso real delante.

**Disparador de reapertura.** Un servidor de `mcp.toml` (u otro llamador
declarativo) se rompe al declarar `env` por perder el entorno heredado —el
síntoma: «command not found» o intérprete ausente al añadir un secreto—, o
cualquier extensión necesita enumerar el entorno completo desde Lua. Al reabrir,
evaluar juntas las dos vías (`enu.sys.environ()` vs modo merge) contra G55
(no regalar secretos al hijo).
