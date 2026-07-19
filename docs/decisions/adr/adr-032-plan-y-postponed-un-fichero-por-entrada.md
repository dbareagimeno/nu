---
title: "`plan/` y `postponed/` adoptan un fichero por entrada; `estado.md` se reduce a puntero + tablero (el registro por sesión vive en `worklog/`)"
type: "adr"
id: "ADR-032"
status: "aceptada"
date: "2026-07-19"
---
# ADR-032 · `plan/` y `postponed/` adoptan un fichero por entrada; `estado.md` se adelgaza

**Estado:** Aceptada · 2026-07-19 (extiende la convención «un fichero por
entrada» de 2026-07-17 —hoy en `decisions/adr/`, `findings/`, `worklog/`,
`validation/`— a las **dos únicas** carpetas de Capa 2 que nunca se dividieron)

**Contexto.** `docs/plan/` y `docs/postponed/` eran los últimos superdocumentos:
- **`estado.md`** pesaba **298 KB / 132 líneas**. Su puntero ▶ —que su propio
  contrato define como «la única línea imperativa»— había degenerado en **una
  sola línea física de 66.683 caracteres** que acaparaba la narrativa de 11
  cierres. Su bitácora eran **71 filas de hasta 7.500 caracteres**, cada una un
  write-up completo de sesión: exactamente el mismo contenido que `worklog/` ya
  guarda **fichero a fichero** (S05–S53). Era un fichero editado
  mecánicamente por las skills en cada cierre, y crecía sin techo.
- **`pospuesto.md`** eran **46 entradas P## en una tabla única** (filas de hasta
  ~2.000 caracteres), frente a `findings/` que ya tiene un fichero por G##.

La convención de 2026-07-17 («un fichero por entrada + índice `README.md`») ya
demostró ser sostenible en cuatro carpetas; `plan/` y `postponed/` quedaron
fuera por omisión, no por diseño.

**Decisión.** Tres piezas, sin tocar el mecanismo del **puntero ▶**:

1. **`estado.md` se reduce a estado vivo**: el puntero ▶ (recortado a su línea
   imperativa única), el tablero por fases y un puntero «Último cierre». El
   **registro por sesión deja de vivir aquí**: pasa a ser el fichero
   `worklog/sNN-<slug>.md` (que ya existía y ya lo contenía para S05–S53). La
   bitácora histórica completa (S01–S45 + el lote post-plan, incluidas S01–S04
   que no tienen fichero en `worklog/`) se **archiva verbatim** en
   `docs/archive/bitacora-plan.md` (sin pérdida). `estado.md` cae de 298 KB a
   ~4 KB.
2. **`postponed/` adopta un fichero por P##**: `pNN-<slug>.md` (frontmatter
   `type: pospuesto`, `id`, `status` ∈ {vigente, decidida, implementada}, `adr`
   si aplica) + un índice `postponed/README.md` con el contador vivo y la tabla
   P##, espejo de `findings/README.md`. Las 46 entradas se migran verbatim.
3. **`implementacion.md` extrae su inventario 🔒** (`### Inventario de lógica
   clave`, las filas más largas del fichero) a `docs/plan/inventario-tests.md`,
   dejando un puntero. El plan queda como el esqueleto de fases + protocolo.

**La maquinaria del flujo se actualiza en el mismo cambio** (regla de oro:
coherente en todos los documentos): las skills `sesion` (cierre → crea el
`worklog/` de la sesión + avanza puntero/«Último cierre», ya no añade fila de
bitácora), `planificar-sesion`, `hallazgo`, `ronda`, `juicio`, `mutacion`; los
agentes `auditor-docs` (barrido P## sobre `pNN-*.md`) y `juez-tests`
(inventario en `inventario-tests.md`); y `docs/README.md` + `CLAUDE.md` (el
mapa y la tabla de estructura). Las **referencias `[P##]`** de todo el corpus
se reapuntan a su fichero `pNN-*.md` (o al índice cuando citan varias).

**Razonamiento.**
- **Por qué conservar el puntero.** Es lo único genuinamente «vivo» y el ancla
  que `sesion`/`planificar-sesion` leen y escriben mecánicamente. El problema
  no era el puntero sino que `estado.md` había absorbido, además, la historia
  completa —duplicando `worklog/`— en un fichero de una sola línea gigante.
- **Por qué archivar la bitácora en vez de trocearla.** Para S05–S53 el
  write-up ya vive en `worklog/`; trocear las 71 filas crearía duplicados. Para
  S01–S04 y el lote post-plan, el archivo verbatim preserva todo sin trabajo de
  reconstrucción. Nada se pierde; `estado.md` queda sostenible.
- **Por qué `postponed/` como `findings/`.** Simetría de flujo: un G## y un P##
  son la misma clase de registro (uno «grieta que hay que cerrar», otro
  «decisión aplazada con disparador»); el auditor ya recorre `findings/` fichero
  a fichero, y ahora recorre `postponed/` igual. Los `status` por frontmatter
  (`vigente`/`decidida`/`implementada`) hacen el estado grepable por máquina.

**Consecuencias.**
- `estado.md` 298 KB → ~4 KB; `implementacion.md` 553 → 516 líneas;
  `pospuesto.md` (38 KB) → 46 ficheros + índice.
- El histórico se conserva verbatim en `docs/archive/bitacora-plan.md`
  (archivado, no mantenido).
- El protocolo de cierre de `sesion` cambia: **un fichero de `worklog/` por
  sesión** es ahora el registro canónico; `estado.md` solo avanza puntero,
  tablero y «Último cierre».
- **Disparador de reapertura:** si `implementacion.md` (el plan) volviera a
  crecer hasta ser inmanejable, se trocearía por fases o sesiones con el mismo
  patrón; hoy, sectorizado en fases + inventario aparte, es navegable y se deja.
