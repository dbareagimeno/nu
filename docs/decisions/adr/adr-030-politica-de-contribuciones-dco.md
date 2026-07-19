---
title: "Política estable de contribuciones: DCO (Developer Certificate of Origin); el contribuidor conserva su copyright y se retira la reserva de cesión/CLA caso por caso"
type: "adr"
id: "ADR-030"
status: "aceptada"
date: "2026-07-19"
---
# ADR-030 · Política de contribuciones: DCO

**Estado:** Aceptada · 2026-07-19 (decidida en la
[auditoría del feedback «10/10»](../../audits/auditoria-feedback-10-de-10-2026-07-19.md),
tensión T6)

**Contexto.** `CONTRIBUTING.md` gestionaba las aportaciones externas «caso por
caso»: el mantenedor *podía* pedir una cesión de derechos o la firma de un CLA
antes de fusionar, para preservar la opción de comercializar o relicenciar el
proyecto de forma unificada ([ADR-014](adr-014-licencia-apache-2-0.md):
Apache 2.0 con titularidad del autor). El feedback externo del 2026-07-19 lo
señaló sin rodeos: «puede que te pida una cesión después de que hagas el
trabajo» es una mala experiencia que genera desconfianza, aunque la intención
sea legítima — y una política negociada después de cada PR no es una política.
El proyecto ya tiene su primera contribución externa fusionada (PR #108,
[ADR-028](adr-028-imagen-de-contenedor-publicada.md)), así que el marco deja
de ser hipotético.

**Decisión.** La política estable de contribuciones es el **DCO** (Developer
Certificate of Origin, v1.1):

1. Cada commit de una contribución externa lleva `Signed-off-by:`, que
   certifica que el autor tiene derecho a aportar ese código bajo la licencia
   del proyecto (Apache 2.0).
2. **El contribuidor conserva su copyright.** No se pide cesión de derechos ni
   firma de CLA, ni caso por caso ni en general; la reserva que
   `CONTRIBUTING.md` mantenía queda retirada.
3. La titularidad del *proyecto* (nombre, dirección, decisión sobre releases y
   versiones comerciales del binario) sigue siendo del autor original; lo que
   cambia es que las aportaciones externas entran como Apache 2.0 puro, sin
   acuerdo adicional.

**Razonamiento.**
- **Previsibilidad sobre opcionalidad.** El valor de una política de
  contribuciones es que se conozca *antes* de trabajar. DCO es el estándar de
  mínima fricción (kernel de Linux, GitLab, la mayoría del ecosistema CNCF) y
  no requiere infraestructura de firma.
- **El trade-off se asume con los ojos abiertos.** Sin CLA, un relicenciamiento
  futuro del conjunto exigiría el consentimiento de cada contribuidor externo.
  Se acepta: Apache 2.0 ya permite ofrecer versiones comerciales del binario y
  servicios alrededor sin relicenciar nada, que es el caso de uso real que la
  reserva protegía; el relicenciamiento total era una opción teórica cuyo
  precio (desconfianza de cada contribuidor potencial) se paga por adelantado
  y en la moneda que más escasea pre-1.0 — los «tres autores ajenos» del
  criterio de corte de [ADR-025](adr-025-reposicionamiento-motor-de-harnesses.md).
- **Alternativas descartadas.** CLA estándar (Apache ICLA): preserva el
  relicenciamiento pero añade la fricción exacta que el feedback critica;
  cesión explícita: reduciría aún más la predisposición a contribuir; statu
  quo: era la opción «ninguna política», que es la que se corrige.

**Consecuencias.**
- `CONTRIBUTING.md` §«Titularidad y licencia» se reescribe a DCO (aplicado en
  el mismo cambio que este ADR, aún en español).
- Acción derivada (sesión futura, [auditoría 2026-07-19](../../audits/auditoria-feedback-10-de-10-2026-07-19.md)
  T7): reescritura completa de `CONTRIBUTING.md` en inglés, creación de
  `SECURITY.md` (canal privado de disclosure) y plantillas mínimas de
  issue/PR, coherente con el frente público en inglés de ADR-025 pieza 5.
- Operativa: los PR externos deben venir con `Signed-off-by` (activable como
  comprobación en GitHub cuando haya volumen; hasta entonces, revisión
  manual). Los parches triviales sin sign-off se piden corregir, no se
  rechazan de plano.
- `CLAUDE.md` no cambia: los commits del mantenedor y de sus agentes no
  necesitan DCO (el autor no se certifica origen a sí mismo), aunque añadirlo
  no estorba.
