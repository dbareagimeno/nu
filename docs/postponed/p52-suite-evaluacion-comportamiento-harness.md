---
title: "P52 — Suite de evaluación del comportamiento del harness: tareas deterministas (edición multiarchivo, bug fixing, navegación de repos grandes, recuperación tras errores, respeto de permisos, eficiencia de contexto) para comparar versiones de enu con el mismo modelo y detectar regresiones del loop del agente"
type: "pospuesto"
id: "P52"
status: "vigente"
---
# P52 · Suite de evaluación del comportamiento del harness: tareas deterministas (edición multiarchivo, bug fixing, navegación de repos grandes, recuperación tras errores, respeto de permisos, eficiencia de contexto) para comparar versiones de enu con el mismo modelo y detectar regresiones del loop del agente

**Dónde se pospuso.** [Auditoría del feedback 2026-07-19](../audits/auditoria-feedback-10-de-10-2026-07-19.md) (lote L2)

**Por qué.** Nada del proyecto evalúa hoy el **loop**: `/salud` y `/juicio` evalúan código y specs. Comparar ejecuciones exige trazas estables y capturables — el sustrato es el `trace`/RPC de la Fase 3; evaluarlo sin ellas sería frágil

**Disparador de reapertura.** Fase 3 entregada (`trace` y RPC estables para capturar y comparar runs); primera parada: decidir si vive como skill de la familia `/salud` (QA del harness — la tendencia) o como extensión oficial reutilizable
