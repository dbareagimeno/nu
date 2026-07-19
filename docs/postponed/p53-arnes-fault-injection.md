---
title: "P53 — Arnés de fault injection: I/O adversa contra el binario completo — HTTP 429/500, streams truncados, respuestas inválidas, disco lleno, fs read-only, permisos que cambian, procesos que no terminan, plugins que bloquean o panican, workers que mueren sin responder"
type: "pospuesto"
id: "P53"
status: "vigente"
---
# P53 · Arnés de fault injection: I/O adversa contra el binario completo — HTTP 429/500, streams truncados, respuestas inválidas, disco lleno, fs read-only, permisos que cambian, procesos que no terminan, plugins que bloquean o panican, workers que mueren sin responder

**Dónde se pospuso.** [Auditoría del feedback 2026-07-19](../audits/auditoria-feedback-10-de-10-2026-07-19.md) (lote L4)

**Por qué.** Es de naturaleza distinta a `/salud` (fuzzing/race/mutación sobre `go test`): orquesta el **binario como caja negra**, y meterlo allí desnaturalizaría la skill. SEC-01 de la [auditoría de seguridad](../audits/auditoria-seguridad-2026-07-16.md) (un panic de Go en un HostFn tumba el runtime) es exactamente el caso «plugin que panica» y sigue sin dueño formal — este arnés lo haría visible de forma sistemática

**Disparador de reapertura.** Antes de recomendar enu para producción/CI de terceros; montable ya contra el binario actual si se prioriza (no depende de Fases 2-3)
