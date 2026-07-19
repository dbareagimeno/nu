---
title: "P31 — Prompt caching automático (cache_control) en el adaptador anthropic"
type: "pospuesto"
id: "P31"
status: "implementada"
---
# P31 · Prompt caching automático (`cache_control`) en el adaptador `anthropic`

> ✅ **IMPLEMENTADA** (rama `claude/postponed-items-priority`, tests Go que la blindan).

**Dónde se pospuso.** [providers.md](../contracts/providers.md) §3 (obligación 6)

**Por qué.** La obligación 6 manda que el adaptador coloque los breakpoints `cache_control` de Anthropic mecánicamente (tools + system + últimos mensajes). La `0.1.0` preserva y reinyecta el `meta` opaco (incluido un `cache_control` que venga del modelo canónico) pero no genera los breakpoints por su cuenta. Es funcionalmente correcto —las peticiones funcionan— solo más caro: sin caché, factura más alta. No afecta la corrección, solo el coste

**Disparador de reapertura.** Coste real de Anthropic que justifique el ahorro de caché; o antes de congelar el contrato del adaptador
