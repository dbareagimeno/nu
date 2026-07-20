---
title: "El auto-connect de `mcp.toml` sigue roto en modo interactivo: sin una task de fondo pública, la conexión no sobrevive entre `Boot` y el pump, y el chat congela su snapshot de tools antes de que MCP viva"
type: "hallazgo"
id: "G64"
status: "abierto"
date: "2026-07-20"
origin: "investigación de diseño de G59 (parte 1): el arreglo headless destapó que el interactivo estaba roto por la misma raíz"
affected: ["extensión mcp (internal/runtime/embedded/mcp/init.lua)", "extensión chat (ciclo de vida de agent.session)", "enu.task (api.md §3)", "internal/vmwasm (foreground/background)"]
---
# G64 · El auto-connect de `mcp.toml` sigue roto en modo interactivo — extensión `mcp` / `chat` / `enu.task`

**Problema.** Al resolver [G59](g59-el-auto-connect-de-mcp-toml.md) (auto-connect
MCP inservible en headless `-p`) la investigación de diseño confirmó que **el modo
INTERACTIVO estaba igual de roto**, por la misma raíz, y que su arreglo limpio NO era
expresable con la API actual. Tres hechos encadenados:

1. **El chat nunca reconecta MCP.** `internal/runtime/embedded/chat/init.lua` solo
   engancha `core:ready` para montar la UI; no hace `require("mcp")` ni
   `connect_configured`. La única auto-conexión vivía en el `init.lua` de mcp, y G59
   la eliminó por inservible (se autolimpiaba durante `Boot`).
2. **El chat congela su snapshot de tools durante `Boot`.** `chat.start` crea su
   única `agent.session` en `chat/lua/chat/init.lua` **dentro del drenaje de `Boot`**
   (antes de que arranque el pump interactivo), y `agent.session()` congela un
   snapshot del registro de tools al crearse (`agent/lua/agent/init.lua`, §3). Aunque
   MCP se conectara en ese momento, la sesión ya habría snapshotado.
3. **Mantener una conexión viva entre `Boot` y el pump exige una task de FONDO, y no
   hay superficie pública para spawnearla.** El scheduler distingue foreground/fondo
   (`internal/vmwasm`: `RunTasks` retorna cuando `live_fg == 0`), pero el marcador de
   fondo (`.bg`) solo lo pone internamente `enu.task.every`; `enu.task.spawn` crea
   SIEMPRE foreground, y `api.md` §3 no expone nada de fondo. El *reader* de una
   conexión MCP (`mcp/lua/mcp/init.lua`, `dispatch_loop` en `enu.task.spawn`) es
   foreground: una conexión viva al cerrar `Boot` **bloquearía** el `Boot` (RunTasks lo
   espera hasta el timeout). Por eso el arreglo headless de G59 fusiona conexión+turno
   en una sola task del driver; pero en interactivo no hay "una task del turno": hay un
   pump (`PumpTasks`) que sostiene N turnos hasta `core:shutdown`.

**Impacto.** Un servidor MCP declarado en `mcp.toml` **no está disponible en el chat
interactivo** —la superficie de producto principal—. Con la resolución de G59, el
interactivo pasó de "tools deny-stub fantasma" (peor: parecen existir y fallan) a
"tools ausentes" (más limpio y honesto), pero sigue sin poder usar MCP. Afecta a
cualquier flujo interactivo que espere sus servidores MCP listos, que es el caso de uso
natural de declararlos en config.

**Opciones a explorar** (no se decide aquí):

- **(a) Primitivo público de task de FONDO** (p. ej. `enu.task.spawn(fn, { background
  = true })`, o `enu.task.background`): una task que no cuenta para la quiescencia de
  primer plano, así que el reader de una conexión MCP de **vida-proceso** sobrevive a
  `Boot` sin bloquearlo y hasta `core:shutdown`. Es una **adición a la API sagrada**
  (`enu.version.api`++, `juez-filosofia`), y habilitaría un auto-connect interactivo
  bajo `core:ready`. Es también la pieza que la opción (b) de G59 necesitaba. Cuidado:
  una task de fondo "para siempre" cambia el modelo de quiescencia; hay que definir su
  interacción con el drenaje del apagado (ADR-029) y con el watchdog (ADR-004).
- **(b) Rediseñar el ciclo de vida del chat**: conectar MCP bajo el pump (tras
  `core:ready`) y crear/**refrescar** la `agent.session` DESPUÉS de conectar, para que
  el snapshot capture las tools MCP vivas. No exige primitivo nuevo, pero toca el
  arranque del chat y el momento del snapshot.
- **(c) Snapshot de tools re-evaluable**: que una `agent.session` ya creada pueda
  re-snapshotar cuando el registro de tools cambia (evento `agent:tools.changed`), de
  modo que conectar MCP tras crear la sesión surta efecto. Toca el contrato del agente
  (`agente.md` §3) y es más general que (b), pero mayor superficie.

**Disparador de reapertura.** Cuando se toque el auto-connect de MCP en interactivo,
el ciclo de vida del snapshot de tools del chat (`chat/lua/chat/init.lua`,
`agent.session`), o cuando se proponga un primitivo de task de fondo por cualquier otro
motivo (entonces (a) se compone con esta grieta). Ligado a la opción (b) de
[G59](g59-el-auto-connect-de-mcp-toml.md), que descansaba en la misma pieza ausente.
