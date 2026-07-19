---
title: "P41 — Cola durable de tasks: trabajos que sobreviven al proceso, se encolan y se reintentan"
type: "pospuesto"
id: "P41"
status: "vigente"
---
# P41 · Cola durable de tasks: trabajos que sobreviven al proceso, se encolan y se reintentan (caso «tres tareas por Slack: que se hagan las tres»)

**Dónde se pospuso.** [ADR-025](../decisions/adr/adr-025-reposicionamiento-motor-de-harnesses.md), pieza 3 / discusión de [G60](../findings/g60-el-lock-de-sesion-nace-huerfano.md) (2026-07-18)

**Por qué.** Es semántica de job queue (estados pendiente→en curso→hecha/fallida, persistencia fuera del proceso, reintento tras crash), no un `enu.task.spawn` con otro nombre. Sus dos prerrequisitos ya tienen dueño: el transporte natural es el RPC/JSONL de la Fase 3, y la garantía de reintento tras `kill -9` es exactamente la doctrina lease+reconciliación **decidida en G60 ([ADR-029](../decisions/adr/adr-029-resiliencia-lease-reclamable-reconciliacion.md))**. Las mitades existentes (sesiones JSONL reanudables, mesh sobre git como medio durable) sugieren que será componible como extensión, pero decidirlo ahora sería diseñar de rebote

**Disparador de reapertura.** Fase 3 entregada (RPC estable) —**G60 ✅ ya resuelta** con la doctrina de lease (ADR-029), uno de los dos prerrequisitos ya cumplido— y un consumidor real del caso (p. ej. la extensión de Slack u otro frontend remoto que encole trabajo); primera parada: ronda de pseudocódigo «cola durable sobre la API actual»
