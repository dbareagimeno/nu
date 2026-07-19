---
title: "P47 — Enforcement de capacidades por plugin en la frontera Go (la mitad dura de la propuesta «capability-secure» del feedback 2026-07-19)"
type: "pospuesto"
id: "P47"
status: "vigente"
---
# P47 · Enforcement de capacidades por plugin en la frontera Go (la mitad dura de la propuesta «capability-secure» del feedback 2026-07-19)

**Dónde se pospuso.** [Auditoría del feedback 2026-07-19](../audits/auditoria-feedback-10-de-10-2026-07-19.md), tensión T1 / ADR-008

**Por qué.** El **manifiesto declarativo** de capacidades ([ADR-025](../decisions/adr/adr-025-reposicionamiento-motor-de-harnesses.md) Fase 2) informa y consiente en instalación/actualización, pero **no aísla en runtime**: todos los plugins comparten el estado principal (ADR-008) y la valla dura sigue siendo el worker con `caps` ([guia-plugins.md](../contracts/guia-plugins.md) §5). Aplicar el manifiesto como frontera de ejecución por plugin exigiría un modelo de aislamiento nuevo — exactamente lo que ADR-008 descartó para v1. Se registra aquí para que no entre por la vía de hecho al construir la Fase 2; arrastra consigo la revocación dinámica de permisos y el audit-log de decisiones por plugin

**Disparador de reapertura.** Calcado al de P2 (probablemente son la **misma decisión**): ecosistema con plugins de terceros no confiables populares, o incidentes de estabilidad/seguridad que watchdog + `pcall` + manifiesto verificado no contengan; se reabre junto a P2 y P17 en un diseño de seguridad dedicado
