---
title: "El `.jsonl.lock` nace huérfano en el arranque del chat: `enu.task.cleanup` no puede ⏸, así que la promesa de liberación de `sesiones.md` §6 es inimplementable tal como está escrita"
type: "hallazgo"
id: "G60"
status: "abierto"
date: "2026-07-18"
origin: "investigación de la opción (c) de G58 (reproducido empíricamente con el binario)"
affected: ["api.md §3 (`enu.task.cleanup`)", "sesiones.md §6", "guia-plugins.md", "extensión sessions (init.lua)", "extensión chat (ciclo de vida de la sesión)"]
---
# G60 · El `.jsonl.lock` nace huérfano en el arranque del chat: `enu.task.cleanup` no puede ⏸ y la promesa de `sesiones.md` §6 es inimplementable — `api.md` §3 / `sesiones.md` §6 / sessions / chat

**Problema.** La investigación de la opción (c) de [G58](g58-el-chat-no-se-cierra-hasta.md)
refutó la hipótesis de que el `.jsonl.lock` sobreviviera por el camino de
apagado: el lock **no muere huérfano al salir — nace huérfano al arrancar**.
Reproducido con el binario real, la cadena tiene tres capas, de la más
superficial a la más profunda:

1. **Bug de orden en `sessions`.** `Session:close`
   (`sessions/lua/sessions/init.lua:288`) marca `closed=true` **antes** de
   intentar borrar el lock (`:292` precede a `:300`), y el borrado va dentro de
   un `pcall` que traga el error. Si el borrado falla, la sesión queda
   envenenada: cualquier cierre posterior retorna en el guard
   `if self.closed then return end` — **no-op** con el fichero intacto.
   (`Session:append`, que solo comprueba `read_only`, sigue escribiendo el
   transcript pese al `closed=true`, por eso el síntoma pasa desapercibido.)
2. **Bug de ciclo de vida en `chat`.** El chat abre la sesión dentro de una
   task **efímera** (`chat/init.lua:42-48`: el `enu.task.spawn` de `core:ready`
   arranca y retorna). Al morir esa task corre su `enu.task.cleanup`
   (`sessions/init.lua:365`, el que promete «soltar el lock pase lo que
   pase»)… en el momento equivocado: **al acabar el arranque**, no al salir.
   Ese cleanup dispara la capa 1 y deja `closed=true` + lock en disco antes de
   que el usuario escriba nada; el `Chat:quit` posterior, que sí podría borrar
   el lock (corre en task viva), es no-op. Misma familia que la task efímera
   de [G59](g59-el-auto-connect-de-mcp-toml.md).
3. **La grieta de contrato (la de verdad).** Los `enu.task.cleanup` corren en
   la frontera de `__finish`, **sin contexto de task** (`__current == nil`,
   `internal/vmwasm/host.go:515-541`): cualquier primitiva ⏸ dentro de un
   cleanup lanza `EINVAL`. El `enu.fs.remove` del lock es ⏸
   (`internal/runtime/vmwasm_fs.go`), así que el liberador registrado **no
   puede funcionar jamás**. Eso vuelve inimplementable la promesa de
   [sesiones.md](../contracts/sesiones.md) §6 de liberar el lock vía cleanup
   «pase lo que pase con la task» — y, en general, la liberación por cleanup
   de **cualquier recurso cuyo cierre necesite I/O suspendente**. Hoy la única
   recuperación real del lock es la **reclamación de huérfano** del siguiente
   proceso que abre (mismo host + pid muerto, `sessions/init.lua:163-201`).

**Impacto.** Tras cada arranque del chat, el `.jsonl.lock` de la sesión queda
huérfano en disco durante toda la vida del proceso y tras su salida (por
cualquier camino: `/quit`, `ctrl+c`, crash). En la práctica la reclamación por
pid muerto lo recupera en la siguiente apertura, así que el daño visible es
bajo — pero el contrato de `sesiones.md` §6 cuenta una garantía (el cleanup
como red de seguridad) que el kernel no permite implementar: exactamente el
corolario de completitud, o una doctrina mal contada. La suite e2e lo conoce
y **retiró** deliberadamente la aserción «el `.jsonl.lock` desaparece»
(`e2e/chat_test.go`), de modo que hoy ningún gate cubre la liberación del lock.

**Opciones a explorar** (no se decide en esta entrada; en discusión):

- **(i) Permitir ⏸ en los cleanups.** Que el scheduler ejecute cada cleanup
  dentro de un contexto de task (p. ej. una micro-task por liberador,
  preservando el orden LIFO), de modo que un cleanup pueda hacer I/O
  suspendente y el patrón «registro el liberador y me olvido» sea real. Es lo
  que la intuición del autor de plugins espera, pero es un cambio de scheduler
  de nivel ADR-004 y abre preguntas nuevas: ¿qué pasa si un cleanup se
  suspende y no vuelve (¿watchdog?)? ¿un cleanup es cancelable, y entonces
  quién limpia al limpiador? ¿qué orden ven los cleanups de tasks distintas?
- **(ii) Cambiar la doctrina, no el scheduler.** Los cleanups se quedan
  síncronos (solo estado Lua, nada de I/O), la restricción se documenta en
  `api.md` §3 y `guia-plugins.md`, y los recursos con liberación suspendente
  se cierran **explícitamente en una task viva**; `sesiones.md` §6 se
  reescribe para contar la verdad: la garantía robusta no es el cleanup (que
  nunca cubriría `kill -9`) sino la **reclamación de huérfano por pid
  muerto**, que ya existe y cubre incluso el caso peor. Exige además arreglar
  las capas 1 y 2 (orden de `Session:close`; el chat cierra su sesión desde
  una task viva al salir, o la mantiene bajo una task de vida larga).
- **(iii) Combinación con pospuesto.** Aplicar la (ii) ahora y posponer la (i)
  como `P##` con disparador: «si aparece un segundo recurso real cuya
  liberación necesite ⏸ y al que la reclamación por identidad muerta no le
  valga, reabrir».

Las capas 1 y 2 son arreglos necesarios bajo **cualquiera** de las vías; la
decisión de diseño es solo la capa 3.

**Disparador de reapertura.** — (abierto). Afecta a: cualquier sesión que
toque `enu.task.cleanup`, el ciclo de vida de la sesión del chat, o el texto
de `sesiones.md` §6.
