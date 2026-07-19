---
title: "P46 — Permitir suspensión (⏸) directa en los liberadores de enu.task.cleanup (opción A1 de G60)"
type: "pospuesto"
id: "P46"
status: "vigente"
---
# P46 · Permitir suspensión (⏸) **directa** en los liberadores de `enu.task.cleanup` (opción A1 de G60: p. ej. micro-task por liberador preservando el orden LIFO)

**Dónde se pospuso.** [G60](../findings/g60-el-lock-de-sesion-nace-huerfano.md) / [ADR-029](../decisions/adr/adr-029-resiliencia-lease-reclamable-reconciliacion.md) (2026-07-19)

**Por qué.** G60 mostró que un cleanup corre sin contexto de task y no puede llamar ⏸ (`EINVAL`); la prontitud de liberación de recursos con I/O se resolvió **componiendo con lo existente** (patrón *cleanup→spawn* + drenaje del apagado), sin tocar la API sagrada. Permitir ⏸ directo (A1) sería máxima ergonomía —«registro y me olvido»— pero máximo coste de especificación: qué pasa si un cleanup suspendido se cuelga (¿watchdog? entonces la garantía tiene asterisco), orden entre cleanups suspendientes (serie lenta vs. paralelo sin LIFO), con qué caps/dueño/presupuesto corre (ADR-008), y errores parciales durante el desmontaje. No urge: cleanup→spawn cubre el caso real hoy. `flock`/C1 se descartó **aparte** como columna vertebral (mono-host + NFS; H-E), reconsiderable solo como optimización local

**Disparador de reapertura.** **Fricción real documentada** de autores de harnesses con el patrón cleanup→spawn (que registrar el I/O de cierre en una task spawneada resulte engorroso o propenso a error con frecuencia que duela); primera parada: ronda de pseudocódigo «cierre de recursos con I/O en cleanups»
