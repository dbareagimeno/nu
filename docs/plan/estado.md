---
# editado mecánicamente por las skills sesion/planificar-sesion
title: "Estado de la implementación"
description: "El estado vivo del plan: puntero ▶ y tablero de fases. El registro por sesión vive en docs/worklog/."
type: "estado"
status: "vivo"
---
# Estado de la implementación

El estado vivo del [plan de implementación](implementacion.md): el **puntero ▶**
y el **tablero por fases**. Las skills `sesion` y `planificar-sesion` leen y
escriben **este** fichero; el plan (protocolo, fases, política de tests) no
cambia al avanzar el estado.

El **registro por sesión** (qué se entregó, decisiones, `-race`, hallazgos) ya
**no** vive aquí: cada cierre es un fichero en [docs/worklog/](../worklog/) (un
fichero por sesión + índice) — ver [§Cierres](#cierres). Así `estado.md` se
mantiene pequeño: solo dónde estamos, no la historia de cómo llegamos.

## El puntero

> **▶ Próxima sesión: —** (no hay ninguna planificada). La **Fase 10**
> (Convenciones CLI) queda como pista viva: futuras convenciones de CLI que el uso
> real revele entran por `/planificar-sesion`. Con **S54** cerrada, la activación
> «de una tecla» de G21/ADR-010 queda completa y **CP-7 manual es ejecutable
> completo** (solo pendiente el paso irreductiblemente manual: mirar el TTY real).

## El tablero por fases

- [x] **Fase 0** — Esqueleto y banco de pruebas (S01–S03) · CP-1 verde
- [x] **Fase 1** — Scheduler (S04–S09) · CP-2 verde
- [x] **Fase 2** — Eventos y loader (S10–S13) · CP-3 verde
- [x] **Fase 3** — IO, sistema y codecs (S14–S18) · CP-4 verde
- [x] **Fase 4** — Red (S19–S21) · CP-5 verde
- [x] **Fase 5** — Texto y búsqueda (S22–S27) · CP-6 verde
- [x] **Fase 6** — UI + spike de veto (S28–S33) · CP-7 driver de TTY implementado
  (`driver.go`/`tty.go`, blindado headless por inyección); solo queda manual lo
  visual (mirar el terminal real)
- [x] **Fase 7** — Workers (S34–S35) · CP-8 verde
- [x] **Fase 8** — Extensiones oficiales (S36–S45) · CP-9/CP-10 verdes; CP-11
  adaptado (SSE grabado, sin red en CI)
- [x] **Fase 9** — Producto (ADR-025, Fase 1: S46–S52) · CP-12 verde (funnel
  smoke ejecutable + mutación 🔒 batcheada de S49/S50/S51)
- [ ] **Fase 10** — Convenciones CLI (post-adquisición): lo que el uso real
  espera de cualquier binario. **S53** cerrada (`--version`/`-V`). **S54** cerrada
  (elección por teclado en la pantalla desnuda, G21/ADR-010; CP-7 manual completo).
  Pista viva: futuras convenciones CLI entran por `/planificar-sesion`.

> **✅ Plan completo:** 9 fases marcadas (kernel S01–S45 + Producto S46–S52,
> CP-12 verde; el nivel de API vivo lo lleva `api.md` §17 — en 6 tras las
> adiciones G32/G52/G54/G57/G65). La
> Fase 10 (Convenciones CLI) queda como pista viva para lo que el uso real
> revele. Pendiente solo lo irreductiblemente **manual**: mirar la TUI en un
> terminal real (lo visual de CP-7) y CP-11 contra un provider real
> (red/credenciales).

## Orden del backlog (post-plan) — adopción primero (2026-07-20)

Con el plan del kernel completo, el trabajo restante son findings/postponed y las
fases de producto de ADR-025. **Decisión del operador (2026-07-20): adopción
primero.** La **1.0 no es objetivo cercano** — llega cuando el producto esté muy
maduro, con la importancia que merece; su camino crítico (forge+plugin como
criterios de corte, RPC/ACP de la Fase 3) **no dirige** el orden inmediato. Orden
establecido:

1. **G64** — bug pequeño de correctitud (auto-connect MCP interactivo sin task
   de fondo pública). Ya caracterizado. Rápido. **G65 resuelto** (2026-07-21:
   `enu.proc` acepta `env` como tabla o array POSIX y el malformado lanza
   `EINVAL`, `api = 6`; la dimensión reemplazo-vs-fusión →
   [P55](../postponed/p55-fusion-de-entorno-en-proc.md)).
2. **Onramp de primer arranque** — la primera impresión interactiva. Tres piezas:
   *provider-neutral* ([P44](../postponed/p44-wizard-init-multi-provider.md)
   reabierto → ADR que superseda ADR-017 p.1), *setup navegable en Lua*
   ([G66](../findings/g66-la-activacion-interactiva-no-siembra-config.md)) y
   *reset* (subcomando propio). Ver esas fichas.
3. **ADR-025 Fase 2** — `enu plugin add/remove/update/lock`
   ([P4](../postponed/p04-package-manager-de-plugins.md), decidida) + **`forge`**
   (autoconstrucción verificable; demo insignia).
4. **[P43](../postponed/p43-pasada-visual-de-la-portada.md)** — pasada visual de la
   web / demo; **cuelga de `forge`**.
5. **[P49](../postponed/p49-enu-env-reproducibilidad-entorno.md) (`enu env`)** —
   reproducibilidad del harness completo; comprometida («tiene que estar») pero
   mercado corporativo/air-gapped; **cuelga del lockfile de Fase 2**.
6. **[G63](../findings/g63-las-releases-se-publican-sin-firma-ni-atestacion.md)** —
   firma/procedencia de releases (seguridad); independiente.
7. **[P40](../postponed/p40-registry-central-plugins.md)** — registry central;
   lejos (espera masa de plugins de terceros).
8. **ADR-025 Fase 3** — plataforma (RPC/JSONL, ACP, mesh); lejos (territorio 1.0).

**Qué puede ir en paralelo** (superficies que no se pisan):
- **G64** y **G63** son fixes pequeños e independientes: caben en cualquier
  hueco o en paralelo a lo demás.
- El **onramp** (superficie: degradación del chat en Lua + plantillas de providers
  + CLI) y **Fase 2** (superficie: plugin manager + forge) son **independientes**:
  pueden avanzar a la vez si hay capacidad; adopción-primero pone el onramp delante
  cuando haya que elegir uno.
- Dentro de la Fase 2 hay dependencias internas: `forge` se apoya en el plugin
  manager; **P43** necesita `forge`; **P49** necesita el lockfile de Fase 2.

## Cierres

El registro por sesión vive en **[docs/worklog/](../worklog/)** — un fichero por
sesión (`sNN-<slug>.md`) más su [índice](../worklog/README.md). Esa es la forma
sostenible: cada cierre es su propio fichero, no una fila gigante en un
superdocumento. El índice de `worklog/README.md` es la fuente de «dónde
retomar».

**Último cierre:** S54 — elección por teclado en la pantalla de runtime desnudo
(2026-07-20). Ver [s54-teclado-pantalla-desnuda.md](../worklog/s54-teclado-pantalla-desnuda.md).

**Al cerrar una sesión** (skill `sesion`): se crea su `worklog/sNN-<slug>.md`
(con su fila en `worklog/README.md`), se avanza el puntero ▶, se actualiza
«Último cierre» y —si cierra fase— el tablero. **Ya no** se añade fila a una
bitácora aquí.

El **histórico del plan** (la bitácora original: S01–S45 + el lote post-plan, en
forma de tabla con los write-ups completos, incluidas S01–S04 que no tienen
fichero en `worklog/`) está archivado verbatim en
[docs/archive/bitacora-plan.md](../archive/bitacora-plan.md).
