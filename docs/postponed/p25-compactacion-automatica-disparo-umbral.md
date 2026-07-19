---
title: "P25 — Compactación automática: disparo por umbral y evento agent:compact"
type: "pospuesto"
id: "P25"
status: "implementada"
---
# P25 · Compactación automática: disparo por umbral y evento `agent:compact`

> ✅ **IMPLEMENTADA** (rama `claude/postponed-items-priority`, tests Go que la blindan).

**Dónde se pospuso.** [agente.md](../contracts/agente.md) §8 / §4

**Por qué.** El hook `compact` y el replay desde el último resumen existen, y el store acepta entradas `compact` (append genérico), pero la `0.1.0` no detecta el umbral (`usage.input_tokens` sobre ~80% del `context` del modelo) que dispara la compactación, ni emite el evento `agent:compact` que §4 lista. Escribir el resumen es trivial una vez exista el disparo. Sin esto, una sesión larga puede topar el contexto del modelo

**Disparador de reapertura.** Sesiones que rebasen el contexto en uso real; o cuando se quiera el aviso visual de compactación en `chat` (P27)
