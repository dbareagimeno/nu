---
title: "P17 — Scoping de caps por rutas (fs.read:/repo)"
type: "pospuesto"
id: "P17"
status: "vigente"
---
# P17 · Scoping de `caps` por rutas (`fs.read:/repo`)

**Dónde se pospuso.** [problemas.md](../findings/README.md) G6

**Por qué.** Contención de rutas correcta (symlinks, `..`, case-insensitivity) es un proyecto de seguridad en sí; hacerlo *casi* bien es peor que no tenerlo

**Disparador de reapertura.** Demanda real de contención por directorio + diseño de seguridad dedicado; la sintaxis ya le reserva sitio
