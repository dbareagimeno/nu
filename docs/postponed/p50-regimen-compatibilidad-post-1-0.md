---
title: "P50 — Régimen de compatibilidad post-1.0: mecanismo de deprecación con ventanas y warnings accionables, guías/tooling de migración (enu migrate, enu plugin check --against), versiones LTS y política pública de soporte"
type: "pospuesto"
id: "P50"
status: "vigente"
---
# P50 · Régimen de compatibilidad post-1.0: mecanismo de deprecación con ventanas y warnings accionables, guías/tooling de migración (`enu migrate`, `enu plugin check --against`), versiones LTS y política pública de soporte

**Dónde se pospuso.** [Auditoría del feedback 2026-07-19](../audits/auditoria-feedback-10-de-10-2026-07-19.md), tensión T8

**Por qué.** [api.md](../contracts/api.md) §17 solo regula la **adición** (nivel de API + `enu.has()`), y pre-1.0 las roturas van por ADR (ADR-025 pieza 4): todo el contenido de esta entrada presupone una API ya congelada de verdad, y decidirlo antes sería especular. La parte útil de la crítica «solo añadir acumula decisiones viejas» vive aquí

**Disparador de reapertura.** Inicio de la planificación de la 1.0 — el momento en que la válvula de roturas-por-ADR se cierra y hace falta el régimen que la sustituye
