---
title: "P54 — Soak tests de sesiones largas: horas de uso con miles de eventos, cientos de tool calls, compactaciones múltiples, reload de plugins, procesos concurrentes y repos grandes"
type: "pospuesto"
id: "P54"
status: "vigente"
---
# P54 · Soak tests de sesiones largas: horas de uso con miles de eventos, cientos de tool calls, compactaciones múltiples, reload de plugins, procesos concurrentes y repos grandes

**Dónde se pospuso.** [Auditoría del feedback 2026-07-19](../audits/auditoria-feedback-10-de-10-2026-07-19.md) (lote L4)

**Por qué.** El estrés de `-race` de `/salud` es concurrencia sintética de test unitario, no una sesión real sostenida. Parte del escenario aún no existe: el reload/update en caliente de plugins de terceros llega con la Fase 2. Cf. P34 — la retención monotónica de handles en sesiones interactivas largas es justo lo que un soak mediría

**Disparador de reapertura.** Fase 2 entregada (reload/update de plugins reales como parte del escenario) + primeras sesiones reales de horas; su primer informe alimenta el disparador de P34
