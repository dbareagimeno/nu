---
title: "P26 — chat: menciones @ con picker difuso de ficheros"
type: "pospuesto"
id: "P26"
status: "implementada"
---
# P26 · `chat`: menciones `@` con picker difuso de ficheros

> ✅ **IMPLEMENTADA** (rama `claude/postponed-items-priority`, tests Go que la blindan).

**Dónde se pospuso.** [chat.md](../contracts/chat.md) §3

**Por qué.** El contrato describe `@` abriendo un picker difuso (`enu.search.files` + `enu.search.fuzzy`) que inyecta la ruta para que el agente decida leerla. La `0.1.0` no lo implementa (sí hay pickers para permisos y `/sessions`, no para menciones). El input multilínea, historial y pegado funcionan; las primitivas `enu.search.*` ya existen

**Disparador de reapertura.** Uso real donde referenciar ficheros a mano moleste
