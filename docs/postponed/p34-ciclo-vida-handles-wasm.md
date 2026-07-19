---
title: "P34 — Ciclo de vida de los handles wasm: la tabla es monotónica"
type: "pospuesto"
id: "P34"
status: "vigente"
---
# P34 · Ciclo de vida de los handles wasm: la tabla es **monotónica** (solo `Region:destroy` libera su entrada)

**Dónde se pospuso.** `vmwasm/handle.go` / `vmwasm_ws.go`

**Por qué.** Decisión consciente: liberar el handle en `close` rompería la idempotencia del contrato (`Ws:close()` ×2 daría `ECLOSED` al resolver el handle). El coste: cada `Ws`/`Proc`/`Stream`/`Watcher`/`GrepIter`/`Re`/`Block` retiene su `handleEntry` **y el objeto Go** hasta la muerte de la Instance; el caso caliente son los `Block` de text/markdown (uno por render en una sesión interactiva larga). Además, el mecanismo `dispatchHandle` que permite a un método liberar su propio handle solo existe en el despacho síncrono (la carrera real de M15 lo vetó en el suspendente): un liberador ⏸ futuro no tiene vía (auditoría 2026-07-12, A-37 del informe)

**Disparador de reapertura.** Memoria observable en sesiones interactivas largas (crecimiento por renders); o la primera necesidad de un método liberador suspendente; o un GC de handles (p. ej. contador de generación que preserve la idempotencia sin retener el objeto)
