---
title: "P2 — Actores por plugin (isolated = true)"
type: "pospuesto"
id: "P2"
status: "vigente"
---
# P2 · Actores por plugin (`isolated = true`)

**Dónde se pospuso.** ADR-008

**Por qué.** Mata la composabilidad como modo por defecto; dos modos de ejecución duplican la semántica de hooks

**Disparador de reapertura.** Ecosistema con plugins de terceros no confiables populares, o incidentes de estabilidad que watchdog+pcall no contengan
