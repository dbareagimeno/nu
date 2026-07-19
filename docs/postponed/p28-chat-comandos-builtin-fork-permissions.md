---
title: "P28 — chat: comandos builtin /fork y /permissions"
type: "pospuesto"
id: "P28"
status: "implementada"
---
# P28 · `chat`: comandos builtin `/fork` y `/permissions`

> ✅ **IMPLEMENTADA** (rama `claude/postponed-items-priority`, tests Go que la blindan).

**Dónde se pospuso.** [chat.md](../contracts/chat.md) §4

**Por qué.** De los builtins del contrato, faltan `/fork` y `/permissions` (sí existen `/model`, `/sessions`, `/compact`, `/clear`, `/help`, `/quit`). `/fork` depende de `Session:fork` (P22); `/permissions` es UI para ver y editar la política de permisos de la sesión

**Disparador de reapertura.** P22 (para `/fork`); demanda de editar permisos desde la UI
