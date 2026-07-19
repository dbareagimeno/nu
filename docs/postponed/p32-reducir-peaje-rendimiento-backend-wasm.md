---
title: "P32 — Reducir el peaje de rendimiento del backend wasm (veto 2 de M15)"
type: "pospuesto"
id: "P32"
status: "vigente"
---
# P32 · Reducir el peaje de rendimiento del backend wasm: el sub-criterio "camino caliente ≤ 2× gopher" del **veto 2 de M15**

**Dónde se pospuso.** [migracion-vm.md](../archive/migracion-vm.md) §5 / bitácora M15

**Por qué.** El veto 2 de M15 lo evaluó con números: el **puente ⏸ pasa** (~27 µs frío ≤ 50 µs) pero la cláusula "camino caliente ≤ 2×" **falla** contra el stub sin red (turno de agente ~5×, render de markdown ~2,6×). El humano **decidió proceder (opción b)**: el stub sin red amplifica un peaje que en uso real es ruido (un turno real es IO-bound —ms de red por token—, y el sobrecoste del backend son µs; el propio spike ya aceptó el yield 90-650× por eso). Se aplicó ya la palanca de asignación barata (pool de `nu_call_pfunc` por nivel, GC del turno 10%→~3%), y el perfil demostró que **el resto es arquitectónico**: ~50% intérprete PUC-en-wasm + ~33% cruces de frontera, no asignación. Cerrar el 2× exige un proyecto grande, no un retoque, y no urge sin un consumidor real que lo note

**Disparador de reapertura.** (1) **Medible** (afilado por A-39 de la [auditoría 2026-07-12](../audits/auditoria-2026-07-12.md), ahora que G44 dejó vivo el modo interactivo): un camino caliente CPU-bound del producto —el render de markdown/transcript de un turno en el chat— cuyo coste de CPU por pintado supere el presupuesto de un frame del painter (**>30 ms**, ADR-007), medido con perfil sobre una sesión interactiva real; mientras el pintado quepa en su frame, el peaje wasm es invisible por construcción; o (2) que **wazero gane excepciones nativas de wasm** (haría el `pcall`/throw sin cruce de frontera ni `Snapshot`); o (3) rediseñar el agente/scheduler para **suspender sólo en la frontera del slice** (menos yields en el camino caliente, la mitigación que ya apuntó el spike). Palanca menor aún reservada en [ADR-020](../decisions/adr/adr-020-el-puente-definitivo-tasks.md): saltar el `Snapshot` en el camino sin-throw del trampolín (~2-3% de CPU; hoy no compensa el riesgo)
