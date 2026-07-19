---
title: "P33 — Cancelar una primitiva ⏸ en vuelo: HostFn no recibe context.Context"
type: "pospuesto"
id: "P33"
status: "vigente"
---
# P33 · Cancelar una primitiva ⏸ **en vuelo**: `HostFn` no recibe `context.Context`

**Dónde se pospuso.** `api.md` §1.3 / `vmwasm/scheduler.go`

**Por qué.** Hoy `Task:cancel()` da `ECANCELED` inmediato a la task (que corre sus `cleanup`s), pero la goroutine del hostcall en curso (`fs.write`, `http.request`, `proc.run`…) no tiene forma de enterarse: sigue hasta su fin natural y **sus efectos aterrizan después del cleanup**. Solo `sleep` observa el ctx. Cerrarlo exige cambiar la firma de `HostFn` (o una variante con ctx) y propagar el plazo por todas las primitivas: cirugía transversal del kernel, sin caso real que la exija aún — la semántica actual queda documentada como parte de la cancelación cooperativa (auditoría 2026-07-12, A-35 del informe)

**Disparador de reapertura.** El primer caso real donde un efecto post-cleanup corrompa estado (p. ej. un `write` que aterriza tras cancelar y pisa lo que el cleanup dejó); o el rediseño del bucle del scheduler (G44), que ya tocaría esas firmas
