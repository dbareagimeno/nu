---
title: "P12 — Ejecución paralela de tool calls de un mismo turno"
type: "pospuesto"
id: "P12"
status: "vigente"
---
# P12 · Ejecución paralela de tool calls de un mismo turno

**Dónde se pospuso.** [agente.md](../contracts/agente.md) §4

**Por qué.** Secuencial es más seguro (tools que editan ficheros) y más fácil de razonar; es lo que hacen los harnesses de referencia

**Disparador de reapertura.** Evidencia de turnos dominados por tools lentas e independientes (solo lectura)
