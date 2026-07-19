---
title: "P49 — enu env: reproducibilidad del entorno completo del harness (lock/verify/export/reproduce) — comprometida: tiene que estar"
type: "pospuesto"
id: "P49"
status: "vigente"
---
# P49 · `enu env`: reproducibilidad del entorno completo del harness (`lock`/`verify`/`export`/`reproduce`) — comprometida: tiene que estar

**Dónde se pospuso.** [Auditoría del feedback 2026-07-19](../audits/auditoria-feedback-10-de-10-2026-07-19.md), tensión T4

**Por qué.** El lockfile de la Fase 2 pinea **plugins** (repos + tags/SHA + checksums); el círculo mayor que hace a un harness reproducible en CI/corporativo/air-gapped — binario de enu pineado y verificado, config no secreta, providers y modelos, capacidades concedidas, plantillas/skills/system prompts, versiones de contratos — no lo toca por diseño (el binario no es un plugin). Subsumirlo sobrecargaría la Fase 2 con alcance que su ADR no decidió. **Matiz del operador (2026-07-19): esta entrada no es opcional** — el disparador fija el *cuándo*, no el *si*

**Disparador de reapertura.** Fase 2 estable (lockfile de plugins operativo, sobre el que `enu env` se apoya); primera parada: ronda de pseudocódigo del formato del lock de entorno y de la semántica de `verify`/`reproduce`
