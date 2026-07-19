---
title: "Copy de la web a la tesis de motor de harnesses + legibilidad de doc larga (Fase 9, ADR-025 Fase 1)"
type: "sesion"
id: "S47"
phase: 9
status: "cerrada"
---
# S47 — Copy de la web a la tesis nueva + legibilidad (Fase 9 — Producto)

**Qué es.** Segunda sesión de la Fase 9, editorial. Alinea la copy pública de la
web con la tesis de [ADR-025](../decisions/adr/adr-025-reposicionamiento-motor-de-harnesses.md)
(motor de harnesses) —corrigiendo la incoherencia que dejó S46, que solo tocó el
README— y aplica la legibilidad de doc larga que pidió la
[auditoría externa](../audits/auditoria-externa-concepto-2026-07-18.md) §web. Se
rige por el **DoD propio** de la fase (artefacto observable + gates), no por
BDD/TDD.

**Qué se entregó.**
- **Copy de la portada** (`web/src/lib/i18n.ts`, es+en): slogan y cuerpo a la
  tesis nueva. ES «Un coding harness / que reescribes entero.» + cuerpo «Un
  binario estático, sin Node ni npm ni Python. El agente, la TUI, los providers
  y las tools son plugins Lua sobre la misma API pública que usas tú.»; EN «A
  coding harness / you can rewrite.» + su cuerpo homólogo. Sustituye el «Tu
  agente de código. / Tus reglas.» que la auditoría marcó como genérico.
- **Páginas de tesis** (es+en): `empezando/que-es-enu.md` (intro a «motor para
  construir coding harnesses a medida» + matiz pre-1.0 en la idea 4 de la API
  sagrada, ADR-025 pieza 4) y `empezando/primer-agente.md` (la referencia a
  «killer app» pasa a «demo de referencia»).
- **Legibilidad de doc larga**: `--fs-9` (cuerpo de prosa sans, markdown +
  referencia API) 15→16px en `tokens.css`; `.markdown` `max-width` 68→72ch en
  `markdown.css` (rango 70-75 de la auditoría).
- **Cero themes nuevos** (congelación de ADR-025).

**Decisiones operativas (bajo umbral de G##).**
1. **Contraste: sin tocar.** La auditoría pedía «más contraste», pero la paleta
   ya está afinada a WCAG-AA (notas W-02 en `tokens.css`: `--dim`/`--fg` subidos
   a ≈4.6:1 sobre `--bg`). Cambiarla a ciegas —sin poder previsualizar el
   render— arriesgaría ese trabajo de accesibilidad. Se difiere a la pasada
   visual de la portada (P43), que se hace con preview.
2. **Toda la pasada VISUAL de la portada se descopó a [P43](../postponed/p43-pasada-visual-de-la-portada.md)**
   por decisión del operador: la demo del hero (que pega fuerte solo enseñando
   `forge`+`enu init`, y no se puede fabricar honestamente antes), el snippet de
   plugin en la portada, la jerarquía de enlaces primarios sobre atajos y el
   slot de demo son diseño entrelazado con la demo, que se hará en **una sola
   pasada** al final de la Fase 1-2 con material real. S47 se quedó con lo que
   es coherencia y legibilidad, sin riesgo de diseño ni dependencia de forge/init.
3. **`en/wiki/filosofia.md` NO se tocó.** Ese fichero de traducción tiene el
   lema viejo **y** el nombre pre-rename «# nu Philosophy» (staleness anterior a
   esta sesión, del renombrado ADR-022; el wiki español se genera de
   `docs/core/filosofia.md`, ya actualizado en S46). Arreglarlo abre el melón
   del nu→enu en las traducciones en, que es una limpieza propia fuera del
   alcance de S47. **Anotado como pendiente** para una pasada de coherencia de
   las traducciones en (candidato a su propia tarea, junto con la pasada P43).

**DoD (editorial).** No se tocó Go (`go build` intacto). **Los gates de la web
no se pudieron correr localmente**: el proxy del entorno bloquea el registro npm
(E403 tanto contra `registry.npmmirror.com` como contra `registry.npmjs.org`),
así que `npm install` falla y no hay `astro build`/`check:drift`/`check:contraste`.
Los cambios son de bajo riesgo (valores de strings i18n existentes —sin añadir
ni quitar claves, así que el gate i18n no se altera—, prosa markdown y dos
valores CSS; ninguna toca `web/referencia` ni `api.md`, así que check-drift no se
altera) y la **CI del PR valida los gates**. Verificación manual: comillas de
i18n balanceadas, enlaces de las páginas intactos.
