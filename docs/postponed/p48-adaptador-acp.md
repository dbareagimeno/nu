---
title: "P48 — Adaptador ACP (Agent Client Protocol): enu como agente ACP para integrarse en editores compatibles sin extensión por editor"
type: "pospuesto"
id: "P48"
status: "vigente"
---
# P48 · Adaptador ACP (Agent Client Protocol): enu como agente ACP para integrarse en editores compatibles sin extensión por editor

**Dónde se pospuso.** [Auditoría del feedback 2026-07-19](../audits/auditoria-feedback-10-de-10-2026-07-19.md), tensión T3

**Por qué.** Es una **extensión** (vocabulario de producto, nunca kernel) que se monta *encima* del protocolo JSONL/RPC de la Fase 3 (ADR-025), aún no construido: comprometerla hoy sería diseñar de rebote — el mismo motivo por el que P41 espera. El orden correcto que el propio feedback propone (event schema → RPC → ACP → editores) ya es el de la Fase 3; solo la pieza ACP carecía de hogar

**Disparador de reapertura.** RPC/JSONL de la Fase 3 estable + demanda real de integración con un editor ACP-compatible; primera parada: ronda de pseudocódigo «adaptador ACP sobre el RPC»
