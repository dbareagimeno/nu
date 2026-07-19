---
title: "P4 — Package manager de plugins"
type: "pospuesto"
id: "P4"
status: "decidida"
adr: "ADR-025"
---
# P4 · Package manager de plugins

> ✅ **DECIDIDA (ADR-025, pieza 3, Fase 2).** Pendiente solo la construcción.

**Dónde se pospuso.** [filosofia.md](../core/filosofia.md) / [arquitectura.md](../core/arquitectura.md)

**Por qué.** `git clone` en `plugins/` basta para arrancar (modelo vim-pathogen)

**Disparador de reapertura.** **DECIDIDA ([ADR-025](../decisions/adr/adr-025-reposicionamiento-motor-de-harnesses.md), pieza 3, Fase 2): `enu plugin add/remove/update/lock` sobre git (tags/SHA + lockfile + checksums + manifiesto de capacidades), sin registry central (que queda pospuesto como P40). Pendiente solo la construcción (sesiones del plan vía `/planificar-sesion`).** Histórico: el disparador era el dolor real de versionado con decenas de plugins; se adelantó porque el reposicionamiento hace del ecosistema instalable una condición de producto, no una consecuencia
