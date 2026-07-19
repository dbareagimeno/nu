---
title: "P42 — Port automático de extensiones de Pi (TypeScript) a plugins Lua vía forge"
type: "pospuesto"
id: "P42"
status: "vigente"
---
# P42 · Port automático de extensiones de Pi (TypeScript) a plugins Lua vía `forge`

**Dónde se pospuso.** [ADR-025](../decisions/adr/adr-025-reposicionamiento-motor-de-harnesses.md), pieza 3 / [auditoría externa 2026-07-18](../audits/auditoria-externa-concepto-2026-07-18.md)

**Por qué.** La guía manual de equivalencias (`pi.registerTool()` → `agent.tool{}`, etc.) basta para la Fase 3 y no requiere maquinaria; un traductor automático solo tiene sentido con `forge` maduro y demanda de migración real

**Disparador de reapertura.** `forge` estable + los primeros casos reales de migración desde Pi donde la traducción manual resulte el cuello de botella
