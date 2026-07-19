---
title: "P27 — chat: render en vivo de agent:tool.progress y marca de agent:compact"
type: "pospuesto"
id: "P27"
status: "implementada"
---
# P27 · `chat`: render en vivo de `agent:tool.progress` y marca de `agent:compact`

> ✅ **IMPLEMENTADA** (rama `claude/postponed-items-priority`, tests Go que la blindan).

**Dónde se pospuso.** [chat.md](../contracts/chat.md) §2

**Por qué.** La `0.1.0` consume `delta/message/tool.start/tool.end/error/permission.asked`, pero no se suscribe a `tool.progress` (progreso en vivo de una tool) ni a `agent:compact` (marca "historia compactada arriba"). Consumirlos es trivial (mismo patrón que el resto); la marca de compactación depende además de que el agente emita el evento (P25)

**Disparador de reapertura.** Tools de larga duración cuyo progreso se quiera ver; junto con P25 para la marca
