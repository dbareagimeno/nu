---
title: "P23 — Cola de reentrada de Session:send (G4)"
type: "pospuesto"
id: "P23"
status: "implementada"
---
# P23 · Cola de reentrada de `Session:send` (G4)

> ✅ **IMPLEMENTADA** (rama `claude/postponed-items-priority`, tests Go que la blindan).

**Dónde se pospuso.** [agente.md](../contracts/agente.md) §2 (Reentrada G4)

**Por qué.** G4 especifica que un `send` con un turno en vuelo **encola** el mensaje y el loop lo inyecta entre iteraciones (corregir al agente mientras trabaja: "usa pnpm, no npm"). La `0.1.0` ejecuta `send` de forma síncrona, sin cola ni resolución de varios `send` al mismo turno. Es la pieza más arquitectónica del grupo (toca el loop del turno) y se apoya en `Session:cancel` (P22). El flujo de un mensaje por turno funciona; falta la interrupción cooperativa

**Disparador de reapertura.** Uso interactivo real donde corregir al agente a mitad de turno aporte; requiere también P22
