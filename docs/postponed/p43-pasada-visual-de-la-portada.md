---
title: "P43 — Pasada visual de la portada (demo del hero + snippet de plugin + jerarquía de enlaces primarios sobre atajos + slot de demo)"
type: "pospuesto"
id: "P43"
status: "vigente"
---
# P43 · Pasada visual de la portada (demo del hero + snippet de plugin + jerarquía de enlaces primarios sobre atajos + slot de demo)

**Dónde se pospuso.** [ADR-025](../decisions/adr/adr-025-reposicionamiento-motor-de-harnesses.md), pieza 3 (Fase 1) / S47 (descopada por el operador 2026-07-18)

**Por qué.** Todo esto es **diseño de la portada** («la web es un terminal», hero centrado a pantalla única, menú `[i][d][a][g]` como nav): entrelazado con la demo, que pega fuerte solo cuando puede enseñar el flujo insignia real —`forge` (enu construyéndose un plugin) y `enu init` (el onboarding)— y no se puede fabricar antes sin fabricar un registro falso (una demo de chat necesita modelo; un screencast de algo que el producto no hizo está descartado). Se hace en **una sola pasada coherente** al final, no a ciegas y a trozos. El copy a la tesis nueva y la legibilidad (S47) no son diseño y van ya

**Disparador de reapertura.** `forge` (Fase 2) y `enu init` (S49) listos: se graba un asciinema/GIF **real** del flujo (init → forge construye un plugin → se instala) y se hace la pasada de diseño de la portada (snippet, jerarquía, slot) con ese material
