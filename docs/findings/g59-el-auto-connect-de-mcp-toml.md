---
title: "El auto-connect de `mcp.toml` es inservible en headless `-p`: la task efímera desconecta las tools antes del turno, y `env` (array) no llega al subproceso"
type: "hallazgo"
id: "G59"
status: "resuelto"
date: "2026-07-18"
origin: "suite e2e de plugins oficiales (e2e/mcp_test.go, cabecera de hallazgos)"
resolution: "Headless: parte 1 → (a) el driver del CLI conecta mcp.toml en la task del turno, antes de agent.session (las tools entran vivas en el snapshot), y las cierra tras el turno; se elimina el auto-connect efímero del init.lua de mcp. Parte 2 → (c) normalize_env traduce el env array→tabla en el borde TOML→spawn (error accionable por servidor). Sin tocar api.md ni enu.version.api. El modo interactivo (roto por la misma raíz) queda en G64; la grieta del primitivo (env array ignorado por enu.proc.spawn) en G65."
affected: ["cmd/enu/main.go (agentDriver)", "internal/runtime/embedded/mcp/init.lua", "internal/runtime/embedded/mcp/lua/mcp/init.lua (normalize_env)", "e2e/mcp_test.go"]
---
# G59 · El auto-connect de `mcp.toml` es inservible en headless `-p`: la task efímera desconecta las tools antes del turno, y `env` (array) no llega al subproceso — extensión `mcp` / `enu.proc` — **RESUELTO**

> ✅ **RESUELTO (2026-07-20, alcance headless `-p`).** Decidido por el operador: parte 1
> → opción **(a)**, parte 2 → opción **(c)** normalizando en mcp + registrando la grieta
> del primitivo. La resolución es **composición pura** (reordenar llamadas en el driver
> de producto) + normalización en la capa Lua: **no toca `api.md` ni `enu.version.api`**
> (sin `juez-filosofia` ni `sync-web`), y es aplicación de ADR-003/ADR-010/ADR-029 (sin
> ADR nuevo). El modo **interactivo** estaba roto por la misma raíz y su arreglo limpio
> necesita una task de fondo pública → se abre como
> [G64](g64-auto-connect-mcp-interactivo-sin-task-de-fondo.md) (no se cierra aquí). La
> grieta del **primitivo** (`enu.proc.spawn` ignora `env` array en silencio) →
> [G65](g65-proc-spawn-ignora-env-array-en-silencio.md).

**Resolución** (2026-07-20).

1. **Parte 1 (task efímera) → opción (a): conexión en la task del turno.** El
   auto-connect efímero del `init.lua` de mcp —que se autolimpiaba durante `Boot`, antes
   del turno— se **elimina**. El driver headless del CLI (`cmd/enu/main.go`,
   `agentDriver`) detecta la extensión con `pcall(require, "mcp")` (respeta el opt-in de
   ADR-010) y, si hay `mcp.toml`, conecta los servidores con `connect_configured()` **en
   la task del turno y ANTES de `agent.session`** —así sus tools entran VIVAS en el
   snapshot que la sesión congela al crearse (agente.md §3)—, y las cierra tras
   `s:close()`. El cleanup task-scoped de `M.connect` queda como red idempotente; el
   subproceso muere, red final, en `stopAllProcs` (`rt.Close()`), porque headless no
   emite `core:shutdown`. Es producto (ADR-003/ADR-010), no kernel: es el patrón que el
   rodeo `-e` del e2e ya ejercía, promovido a camino oficial.
2. **Parte 2 (`env` array) → opción (c): normalizar en mcp.** `normalize_env`
   (`mcp/lua/mcp/init.lua`) traduce el `env` array `["K=V"]`→tabla `{K=V}` en el borde
   TOML→spawn, **dentro** del `pcall` por-servidor, con error accionable por servidor si
   viene mal formado (no el silent-ignore que ocultó la grieta). `mcp.toml` conserva su
   formato array (ergonómico); la superficie sagrada no se toca. La grieta del primitivo
   —que `enu.proc.spawn` siga ignorando un `env` array en silencio— queda registrada en
   [G65](g65-proc-spawn-ignora-env-array-en-silencio.md).

**Aplicada en:** `cmd/enu/main.go` (agentDriver: conexión + cierre MCP en la task del
turno), `internal/runtime/embedded/mcp/init.lua` (elimina el auto-connect efímero),
`internal/runtime/embedded/mcp/lua/mcp/init.lua` (`normalize_env` + docstrings de
`M.connect`/`connect_configured` + formato de `mcp.toml`), y `e2e/mcp_test.go` (tres
tests nuevos contra `-p` REAL: invocación concedida, deny→**exit 3** —antes
"inalcanzable"—, y `env` que llega al subproceso; toda la suite MCP verde). El texto de
abajo queda como registro histórico del problema y las opciones.

---

**Problema.** Dos grietas contiguas, ambas caracterizadas desde fuera del
binario en `e2e/mcp_test.go`:

1. **La task del auto-connect es efímera y se desconecta antes del turno.**
   El auto-connect de servidores declarados en `mcp.toml`
   (`embedded/mcp/lua/mcp/init.lua:35`) hace
   `pcall(mcp.connect_configured)` y retorna. Al terminar esa task, su
   `enu.task.cleanup` (registrado dentro de `M.connect`) cierra cada conexión,
   mata el subproceso del servidor y re-registra sus tools como stubs de
   "servidor desconectado" (`permissions.default = "deny"`, handler que lanza
   `EMCP`). Todo esto ocurre **durante `Boot`**, porque `RunTasks` drena la
   task hasta quiescencia antes de que arranque el turno de `-p` — así que
   cuando el agente por fin puede pedir la tool, el servidor real ya está
   muerto. Comprobado desde fuera: tras el boot, `mcp.servers()` queda vacío
   pero `mcp__srv__echo` sigue en `agent.tools()` (el stub); un `-p` que pide
   esa tool no invoca el servidor real y no da `exit 3` (el stub de deny
   devuelve `tool_result` con `is_error`, así que el proceso sale con 0). El
   propio módulo se contradice: `connect_configured` se documenta como
   pensado para correr en una **task de larga vida**
   (`mcp/lua/mcp/init.lua:463`), pero la task que la invoca en el auto-connect
   es efímera por construcción.
2. **`env` de `mcp.toml` (array) no llega al subproceso.** `mcp.toml`
   documenta `env = ["K=V", ...]` (array de strings;
   `embedded/mcp/lua/mcp/init.lua:428`), y ese array se pasa tal cual a
   `enu.proc.spawn`. Pero la primitiva **solo interpreta `env` como tabla**
   `{ K = V }` (mapa string→string; `internal/runtime/vmwasm_proc.go:250`):
   un array Lua es `[]any`, no `map[string]any`, así que se ignora en
   silencio y el subproceso hereda el entorno del padre sin las claves
   declaradas. Verificado e2e: un servidor MCP configurado con `env` no
   recibe la variable.

**Impacto.** Un servidor MCP declarado en `mcp.toml` es, hoy, **inservible
desde `enu -p`** (el modo headless de un solo turno): el auto-connect lo
lanza y lo mata antes de que el agente pueda usarlo, y aunque sobreviviera,
cualquier configuración que dependa de `env` para autenticarse o parametrizar
el servidor tampoco llegaría. Afecta a cualquier integración MCP pensada para
correr desatendida (CI, automatización) — el caso de uso que `-p` existe para
servir. `e2e/mcp_test.go` documenta ambas grietas en su cabecera de
"HALLAZGOS que esta suite destapó" y las **rodea sin trampa**: el escenario 1
(mínimo imprescindible) se conduce con `enu -e` + `connect_configured` +
`agent.session` en una única task —que sigue leyendo `mcp.toml` real y
ejerciendo servidor/stdio/HTTP reales—, y el escenario 2 original (deny →
`exit 3` vía tool MCP en `-p`) se recortó por inalcanzable, ya que la tool MCP
real nunca llega viva a un turno de `-p`. Los ajustes del servidor de prueba
se pasan por **argv** en vez de por `env` precisamente porque `env` está
verificado como roto.

**Opciones a explorar** (no se decide en esta entrada; el arreglo queda
pospuesto):
- **(a) Para la task efímera: fusionar `connect_configured` + `agent.session`
  en una sola task de vida más larga**, en vez de dos pasos donde el primero
  se autolimpia antes del segundo — es la vía que ya usa el escenario 1 de la
  suite e2e como rodeo, y candidato natural a convertirse en el camino oficial
  del auto-connect headless.
- **(b) Para la task efímera: no registrar el `cleanup` de desconexión al
  auto-connect**, dejando la conexión viva hasta el apagado real del binario
  — requiere decidir quién es entonces el dueño del cleanup final (¿el propio
  `core:shutdown`, con el mismo mecanismo que G58 investiga?).
- **(c) Para `env`: traducir el array de `mcp.toml` a mapa antes del
  `spawn`** dentro de `connect_configured`, sin tocar la primitiva — mínimo
  cambio, pero dos formatos de `env` conviviendo en el ecosistema (el que
  documenta `mcp.toml` y el que espera `enu.proc.spawn`) es una superficie
  confusa para cualquier otro plugin que dispare `mcp.toml`-alike.
- **(d) Para `env`: `enu.proc.spawn` acepta también array `["K=V", ...]`**
  además de la tabla — corolario de completitud si el patrón `env` como array
  resulta ser el vocabulario natural de más de un llamador; toca `api.md` y
  exige el mismo escrutinio que cualquier adición a la superficie sagrada.

**Disparador de reapertura.** Cuando se toque el ciclo de vida del
auto-connect de `mcp.toml` (`embedded/mcp/lua/mcp/init.lua`, `M.connect` /
`connect_configured`) o el parseo de `env` de `enu.proc.spawn`
(`internal/runtime/vmwasm_proc.go`).
