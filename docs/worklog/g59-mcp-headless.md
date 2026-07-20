---
title: "G59: MCP servible en headless `-p` (conexión en la task del turno) + `env` array normalizado en mcp; abre G64/G65"
type: "sesion"
id: "G59"
status: "cerrada"
date: "2026-07-20"
---
# G59 — MCP servible en headless `-p` + `env` array normalizado en mcp

Resolución del hallazgo [G59](../findings/g59-el-auto-connect-de-mcp-toml.md) (fila
`G59` en la bitácora; ciclo `/hallazgo` con implementación). Decisiones operativas:

**Decisión del operador.** Parte 1 → opción **(a)** (conectar MCP en la task del
turno); parte 2 → opción **(c)** (normalizar `env` en mcp) **+ registrar hallazgo** para
la grieta del primitivo. Alcance **headless**; el resto se difiere a companions.

**Investigación de diseño (3 exploraciones + 1 agente de plan).** Destapó tres hechos
que decidieron el diseño: (1) **MCP estaba roto en los DOS modos**, no solo `-p` —el
chat nunca reconecta MCP y congela su snapshot de tools durante `Boot`—; (2) **no hay
superficie pública para spawnear una task de FONDO** (`.bg` solo lo pone
`enu.task.every`), lo que hunde la opción (b) y bloquea el arreglo limpio del
interactivo; (3) en headless **no se emite `core:shutdown`** (el dueño del cleanup final
es `stopAllProcs`). El agentDriver del CLI (`package main` = producto) puede orquestar
MCP sin violar ADR-003/ADR-010.

**Implementación.** (a) Se elimina el auto-connect efímero del `init.lua` de mcp; el
`agentDriver` (`cmd/enu/main.go`) hace `pcall(require,"mcp")` + `connect_configured()`
**en la task del turno, antes de `agent.session`** (para que las tools entren vivas en el
snapshot), y cierra las conns tras `s:close()`. (c) `normalize_env` traduce el `env`
array→tabla **dentro** del `pcall` por-servidor de `connect_configured` (un env malo
degrada solo ese servidor). **No se toca `api.md` ni `enu.version.api`** (composición
pura + normalización en la capa Lua) → sin `juez-filosofia`, sin `sync-web`, sin ADR
(aplicación de ADR-003/ADR-010/ADR-029). El paso 2 del `/hallazgo` («¿ya es
expresable?») queda satisfecho sin inventar API.

**Companions abiertos.** [G64](../findings/g64-auto-connect-mcp-interactivo-sin-task-de-fondo.md)
(el interactivo sigue roto; su arreglo limpio necesita una task de fondo pública o
rediseñar el snapshot del chat) y
[G65](../findings/g65-proc-spawn-ignora-env-array-en-silencio.md) (`enu.proc.spawn`
ignora el `env` array en silencio; incluye la dimensión **reemplazo vs fusión**: un `env`
presente REEMPLAZA el heredado —§6—, así que un servidor MCP que necesite `PATH` se
rompe al declarar un secreto; documentado en `mcp.toml` y diferido).

**Tests (nombran G59).** Tres nuevos en `-p` REAL (`e2e/mcp_test.go`): invocación
concedida (`--auto-permissions`, servidor real ejecuta `tools/call`), **deny→exit 3**
—antes «inalcanzable» porque la tool nunca llegaba viva— y `env` que llega al subproceso
(el servidor de prueba escribe `$MCP_TEST_ENV` a disco al arrancar). Los existentes
(kill-on-exit, toml malformado, command-not-found, escenario 1 programático `-e`) siguen
verdes con comentarios actualizados. Suite completa verde.

**Juicio clean-room (espec + concurrencia + auditor de docs), y sus consecuencias.**
- **Espec: CONFORME.** El driver conecta antes del snapshot; no hay fuga a la superficie
  sagrada; el `init.lua` comentario-only sigue siendo plugin válido (el loader registra
  los módulos antes de los init).
- **Concurrencia: dos hallazgos.** **C1 [MEDIA] REAL, arreglado:** `mcp._has_config()`
  se llamaba SIN `pcall` —`enu.fs.stat` lanza en errores ≠ ENOENT (EACCES/ENOTDIR sobre
  la ruta de config, el escenario CI/Docker)—, abortando el turno pese a ser MCP
  opcional; se envuelve la detección en `pcall` (degrada con log). **C2 [BAJA]
  documentado:** el reader foreground bloqueado en `read_line` puede colgar el drenaje de
  `-p` si un NIETO del servidor retiene stdout y sobrevive al `SIGKILL` del hijo directo;
  raíz **preexistente** de `enu.proc.kill` (mata el proceso directo, no el árbol), no
  introducida por G59; el camino sano termina limpio. Se documenta la limitación junto al
  reader y se corrige el comentario del driver que sobrestimaba la garantía de
  `stopAllProcs`. Candidato a futuro finding de `proc` (kill de grupo) si llega a morder.
- **Auditor de docs: tres incoherencias, corregidas.** D1 (la narrativa de cabecera del
  índice decía «Queda ABIERTA G59» en presente → pasado con puntero), D2 (clave de
  frontmatter `companions` fuera del esquema → retirada; el vínculo vive en `resolution`
  y el cuerpo), D3 (worklog `e2e-plugins.md` ítem 3 sin puntero a la resolución → nota
  `→ G59/G64/G65`). Contadores (63/60/3), enlaces y disparadores P##: coherentes.

**Web / `api.md`.** No se toca `api.md`: sin bump de `enu.version.api` y sin pasada de
`/sync-web`. La wiki ES se publica sola desde `docs/`.

**Nota lateral (fuera de alcance).** El agente de plan reportó `APILevel = 5` en
`internal/runtime/enu.go`, pero `docs/plan/estado.md` dice «APILevel en 2 tras G32»
(hubo adiciones G52/G54/G57 después): deriva doc↔código a corregir cuando se toque
`estado.md`, ajena a G59.
