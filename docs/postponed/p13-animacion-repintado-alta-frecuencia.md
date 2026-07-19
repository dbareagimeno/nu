---
title: "P13 — Animación/repintado de alta frecuencia (>~30 fps)"
type: "pospuesto"
id: "P13"
status: "vigente"
---
# P13 · Animación/repintado de alta frecuencia (>~30 fps)

**Dónde se pospuso.** [modelo-ejecucion.md](../core/modelo-ejecucion.md) §limitaciones

**Por qué.** Una TUI pinta por cambios; el coalescing de ~30 ms es deliberado

**Disparador de reapertura.** Probablemente nunca; si una extensión legítima lo necesita, discutir un canal de pintado directo
