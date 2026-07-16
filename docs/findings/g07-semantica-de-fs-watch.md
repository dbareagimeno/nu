# G7 · Semántica de `fs.watch` — `api.md` §5 — **RESUELTO**

**Resolución** (aplicada en [api.md](api.md) §5): `watch(path, opts?, fn)`
con `recursive`, `gitignore = true` por defecto y entrega en lotes con
debounce (`fn(events[])`, ~50 ms). La versión mínima se descartó: habría
obligado a cada consumidor a reimplementar recursión+ignores+debounce en
Lua — trabajo proporcional al repo en el estado principal, contra "Lua
decide, Go ejecuta".

**Problema.** Sin definir: ¿recursivo?, ¿respeta `.gitignore`?
(vigilar `node_modules/` = ruido infinito), ¿coalescing de ráfagas?
(un `git checkout` toca miles de ficheros → miles de callbacks).

**Impacto.** Cualquier plugin de auto-contexto o recarga; riesgo de
rendimiento en el estado principal.

**Opciones.** (a) `watch(path, opts, fn)` con `opts = { recursive,
gitignore = true, debounce_ms = 50 }` y entrega de eventos en lotes
(`fn(events[])`); (b) mínimo v1: un path, sin recursión (los plugins
componen), y a pospuestos lo demás.
