---
title: "P22 — Métodos de control de sesión Session:cancel/fork/compact/clear_queue"
type: "pospuesto"
id: "P22"
status: "implementada"
---
# P22 · Métodos de control de sesión `Session:cancel/fork/compact/clear_queue`

> ✅ **IMPLEMENTADA** (rama `claude/postponed-items-priority`, tests Go que la blindan).

**Dónde se pospuso.** [agente.md](../contracts/agente.md) §2

**Por qué.** El contrato los lista en la firma de `Session` (v1), pero la extensión `agent` `0.1.0` solo implementó `send/spawn/set_model/close`. El núcleo del turno (anexar, stream, tools, permisos, subagentes) funciona; quedaron fuera el cancelar el turno en vuelo, el `fork` de sesión (el store ya soporta `parent`, sesiones.md §5), la compactación manual y el vaciar la cola de reentrada (P23). No bloquean el flujo headless/CI, donde un turno corre hasta completar

**Disparador de reapertura.** Una UI o script que necesite cancelar un turno largo, bifurcar una sesión o compactar a mano; o antes de congelar el contrato de `agent`
