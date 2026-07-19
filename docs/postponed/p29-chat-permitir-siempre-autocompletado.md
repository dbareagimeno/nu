---
title: "P29 — chat: permitir siempre persistente y autocompletado visual de /"
type: "pospuesto"
id: "P29"
status: "implementada"
---
# P29 · `chat`: "permitir siempre" persistente y autocompletado visual de `/`

> ✅ **IMPLEMENTADA** (rama `claude/postponed-items-priority`, tests Go que la blindan).

**Dónde se pospuso.** [chat.md](../contracts/chat.md) §5 / §3

**Por qué.** El diálogo de permisos ofrece "permitir una vez" y "denegar"; faltan "permitir siempre" (añadir el patrón a la política de la sesión, o persistir a `agent.toml` global con modificador) y la edición del patrón propuesto. Tampoco el autocompletado visual de comandos `/` (el backbone `complete()` existe; falta la capa modal). Es pulido de las capas modales del chat

**Disparador de reapertura.** Fricción real de re-conceder permisos cada turno; o demanda de descubribilidad de comandos
