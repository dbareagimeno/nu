---
title: "P51 — Conformance suite ejecutable para terceros: el artefacto que un autor externo corre para certificar su plugin/provider contra los contratos (incluye tests contractuales de providers)"
type: "pospuesto"
id: "P51"
status: "vigente"
---
# P51 · Conformance suite ejecutable para terceros: el artefacto que un autor externo corre para certificar su plugin/provider contra los contratos (incluye tests contractuales de providers)

**Dónde se pospuso.** [Auditoría del feedback 2026-07-19](../audits/auditoria-feedback-10-de-10-2026-07-19.md), tensión T8

**Por qué.** La conformance del **core** ya existe en forma de proceso (rondas de pseudocódigo, checkpoints 🔎, inventario 🔒): valida que enu cumple sus contratos, pero no da a un tercero nada ejecutable contra *su* código. Construirla sin autores externos sería un edificio vacío (la doctrina de P40)

**Disparador de reapertura.** El primer autor externo real de plugin/provider — **deliberadamente el mismo evento** que el criterio de corte de la 1.0 (ADR-025 pieza 4): cuando aparezca, necesitará el artefacto que le diga si su extensión es conforme
