---
title: "Auditoría del feedback externo «el camino al 10/10» — 19 de julio de 2026"
type: "auditoria"
date: "2026-07-19"
status: "cerrada"
---
# Auditoría del feedback externo «el camino al 10/10» — 19 de julio de 2026

Segundo feedback externo sustancial, recibido el 2026-07-19 — un día después
del triaje de la [auditoría externa de concepto](auditoria-externa-concepto-2026-07-18.md)
que produjo [ADR-025](../decisions/adr/adr-025-reposicionamiento-motor-de-harnesses.md).
Este texto no evalúa el estado actual sino que dibuja **qué le faltaría a enu
para ser «10/10»**: cuatro pilares, diez aspectos de excelencia, una definición
de «1.0 excelente» en 17 criterios y una priorización en 10 pasos. Se auditó
punto a punto contra el estado real del repo y el mapa de decisiones
(ADR-001–028, P1–P45, G1–G62), con seis lotes de revisión independientes y
verificación factual de cada cita; las ocho tensiones que exigían decisión se
resolvieron con el operador el mismo día. Este documento conserva el resumen
fiel, la tabla de veredictos y el triaje.

> **Nota de reconciliación (mismo día).** Mientras esta auditoría se cerraba
> aterrizaron en `develop`, en paralelo, la resolución de G60 (PR #115:
> [ADR-029](../decisions/adr/adr-029-resiliencia-lease-reclamable-reconciliacion.md),
> lease reclamable + reconciliación, con la opción A1 pospuesta como P46) y el
> alta de la sesión S54 (PR #116: el puntero ▶ reabre la Fase 10). Por eso:
> (1) las altas de esta auditoría se renumeraron al integrar — el ADR del DCO
> es **ADR-030** y las pospuestas nuevas son **P47–P54**; (2) las menciones a
> G60 en la tabla se leen como *resuelta*; y (3) las cifras del encuadre
> («53/53 sesiones, puntero en `—`», «ADR-001–028, P1–P45») describen el mapa
> **al inicio de la auditoría**, no el estado tras esos merges.

## Veredicto del feedback (resumen fiel)

«Cuando estén implementados `forge`, el gestor de plugins, RPC/JSONL, `trace`,
`worktree`, el onboarding y la nueva web, enu podría estar en un 8,5–9/10
técnico.» El último punto no se consigue con más features sino haciendo
**aburridas y predecibles** ocho situaciones: instalarlo en una máquina
desconocida; actualizarlo sin romper; ejecutar código de terceros; recuperarse
de un cierre abrupto; integrarlo en un editor o en CI; reproducir la
configuración de otro usuario; diagnosticar por qué un agente hizo algo;
mantener compatibilidad durante años. «Un proyecto 10/10 es el que ha eliminado
casi todas las razones razonables para no adoptarlo.»

Sus cuatro pilares: (1) distribución sin runtime externo (ya conseguida, pero
insuficiente como única defensa frente a Pi); (2) harness **reproducible**
(`enu env lock/verify/export/reproduce`); (3) **seguridad por capacidades**
aplicada en la frontera Go, por plugin, con manifiesto TOML
(`fs`/`proc`/`net`/`env`), denegación por defecto, secret broker y diff de
permisos al actualizar — «la mayor oportunidad de diferenciación frente a Pi»;
(4) **autoconstrucción verificable**: `forge` como pipeline completo
(describe → specify → generate → validate → test → audit → review → install)
usando solo la API pública. Posicionamiento propuesto: «a reproducible and
capability-secure coding harness».

Los diez aspectos: producto cotidiano (el agente oficial debe ser excelente,
no solo la plataforma), plataforma de plugins con calidad de package manager,
interoperabilidad (RPC/JSONL y **ACP** como servidor oficial, antes que
extensiones por editor), fiabilidad adversarial (crash consistency, fuzzing,
fault injection, soak), cadena de suministro (firmas, SBOM, provenance,
CodeQL, actions por SHA, SECURITY.md), rendimiento demostrado (benchmarks
públicos contra Pi + presupuestos), contratos y compatibilidad (deprecación,
LTS, conformance), observabilidad (`trace` como infraestructura con replay y
export OTel, telemetría solo opt-in), `doctor` completo con remedios
ejecutables, y UX/documentación con un camino por tipo de usuario. Añade una
crítica de gobernanza (el CLA «caso por caso» de CONTRIBUTING.md «genera
desconfianza») y una lista de anti-features que coincide casi por completo con
las pospuestas del proyecto. Veredicto final: 9,5/10 con todo implementado;
«el 10/10 solo llega cuando varios usuarios independientes lo someten a cargas
que tú no diseñaste».

## Verificación factual

Todas las afirmaciones del feedback sobre el estado del repo se verificaron
**ciertas**: el README comunica la propuesta y el funnel de instalación
existe (checksum, `mv` atómico, sin sudo; `init`/`doctor`/`update`/
`uninstall`); `doctor` tiene salida humana y JSON versionado (`doctor.v1`),
red solo con `--net`, y los 4 checks de producto en `skip` (G62/P45); la
arquitectura declara el aislamiento por tarea (ADR-008); la API usa niveles
incrementales con `enu.has()` (api.md §17, nivel 5). La única cita no
verificable aquí es la que describe a Pi (fuente externa). El feedback llega,
eso sí, **en parte por detrás de ADR-025**: buena parte de lo que pide ya
está decidido (no construido) en sus Fases 2–3.

## Tabla de veredictos

Taxonomía: **YA-HECHO** · **YA-DECIDIDO** (sin construir; Fase 2/3 de
ADR-025) · **POSPUESTO** (P## con disparador vigente) · **NUEVO**
(correcto y sin hogar en el mapa) · **CHOCA-CON-ADR** · **ERRÓNEO**
(premisa falsa) · **TENSIÓN** (resuelta por el operador, ver §Tensiones).

| Punto del feedback | Estado real (evidencia) | Veredicto | Destino |
|---|---|---|---|
| Pilar 1: distribución sin runtime | Fase 1 completa (S46–S53); la mitigación de «no puede ser la única defensa» es exactamente la combinación de piezas de ADR-025 | YA-HECHO | — |
| Pilar 2: `enu env lock/verify/reproduce` (entorno completo) | No existe; el lockfile de Fase 2 cubre solo plugins | NUEVO → TENSIÓN T4 | **P49** (comprometida) |
| Pilar 3: manifiesto de capacidades TOML por plugin + diff de permisos en update | ADR-025 Fase 2 lo incluye («manifiesto de capacidades»); `plugin.toml` hoy solo `name/version/requires` | YA-DECIDIDO | Diseño de Fase 2 |
| Pilar 3: **enforcement por plugin en la frontera Go** | ADR-008: aislamiento por tarea; los plugins comparten el estado principal; la valla dura son los workers con `caps` (guia-plugins.md §5) | CHOCA-CON-ADR → TENSIÓN T1 | **P47** |
| Pilar 3: denegación por defecto | Ya es el modelo de `caps` (G6: deny-by-default) a nivel worker | YA-HECHO | — |
| Pilar 3: secret broker (env por variable) | `enu.sys.env(name)` ya lee por nombre (no hay environ masivo); el allowlist por variable no es expresable con `caps` módulo/función | NUEVO | **P17 ampliada** |
| Pilar 3: restricciones de red por host | Mismo patrón que el scoping por rutas de P17, sobre `enu.http` | NUEVO | **P17 ampliada** |
| Pilar 3: allowlist de procesos/argumentos | ADR-023 (por subcomando, fail-closed); el parseo fino es P39 | YA-HECHO + POSPUESTO (P39) | — |
| Pilar 3: revocación de permisos, auditoría de decisiones | Caps inmutables al spawn (ADR-024); el audit-log encaja en `trace` (Fase 3) | NUEVO (menor) | Diseño Fase 2/3; registrado en P47 |
| Pilar 3: plugins no confiables en workers recortados | Es ya el modelo recomendado (guia-plugins.md §5); el automatismo por plugin es P2 | YA-HECHO + POSPUESTO (P2) | — |
| Pilar 4: `forge` (concepto y prueba de completitud) | ADR-025 Fase 2; coincide con el corolario de completitud | YA-DECIDIDO | Las 8 etapas del pipeline, como insumo de la ronda de pseudocódigo de `forge` |
| Producto cotidiano: cancelación, reintentos/rate-limits, cambio de modelo, compactación, subagentes, recuperación | Ya en agente.md (§8, G19, G42/G43, P22–P25 implementadas); G60 (lock huérfano) resuelta el mismo día — ver nota de reconciliación | YA-HECHO (contratos) | — |
| Producto cotidiano: undo/checkpoints, contexto Git, selección de archivos | Sin hogar documental; no son decisiones pospuestas sino diseño no hecho del tooling de edición | NUEVO (producto) | Sin P##: entra por `/planificar-sesion` cuando se construya ese tooling |
| Plataforma de plugins: `add/remove/update/lock` sobre git, lockfile+checksums, sin registry | ADR-025 Fase 2; registry = P40 | YA-DECIDIDO + POSPUESTO (P40) | El detalle fino (staging atómico, provider simulado, diagnóstico de conflictos), como checklist de diseño de Fase 2 |
| Interop: RPC/JSONL con esquemas versionados | ADR-025 Fase 3; el inventario de eventos ya vive en los contratos (`agent:*`, G40/G43) | YA-DECIDIDO | Diseño de Fase 3 |
| Interop: **ACP como servidor oficial** | Cero menciones en docs/ (ni ADR ni P##) — hueco genuino | NUEVO → TENSIÓN T3 | **P48** |
| Interop: no plugins por editor antes de ACP | Coincide con la doctrina del repo (nada construido por editor) | YA-HECHO (por ausencia) | — |
| Observabilidad: `trace` (timeline, tools, permisos, coste, subagentes) | ADR-025 Fase 3 | YA-DECIDIDO | Diseño Fase 3; `replay`/`compare`/`export --otel` y la redacción de prompts, como insumos |
| Observabilidad: telemetría solo opt-in | No existe telemetría alguna; coincidencia total | YA-HECHO (por ausencia) | Restricción de diseño explícita del `trace` de Fase 3: export opt-in, cero envío por defecto |
| Observabilidad: suite de evaluación del harness (el loop) | Nada la cubre (`/salud` y `/juicio` evalúan código, no el loop) | NUEVO | **P52** |
| Fiabilidad: crash de escritura de sesión / lockfile | JSONL append-only + `exclusive`; G60 (lock huérfano) era exactamente este caso y se resolvió el mismo día con la doctrina de lease reclamable + reconciliación (ADR-029, PR #115 — ver nota de reconciliación) | YA-DECIDIDO | Kill-tests como criterio de aceptación cuando la resolución de G60 se construya |
| Fiabilidad: crash de update del binario | `atomicReplace` (sidecar + rename mismo fs) ya implementado | YA-HECHO | Falta el kill-test deliberado (acción menor, /salud o smoke) |
| Fiabilidad: fuzzing | 6 dianas reales en `fuzz_test.go` vía `/salud` (corpus acumulativo, hallazgos reales) | YA-HECHO (parcial) | Dianas nuevas (TOML/JSON de config, JSONL de sesión, manifiestos, mensajes de workers) como ampliación de `/salud` |
| Fiabilidad: fault injection | No existe arnés (429/500, disco lleno, fs read-only, workers muertos); SEC-01 (panic en HostFn) sigue sin dueño formal | NUEVO | **P53** |
| Fiabilidad: soak tests | No existen; el estrés de `-race` de `/salud` no es soak de sesión real | NUEVO | **P54** |
| Supply chain: firmas, provenance, SBOM | SEC-06 abierto desde la [auditoría de seguridad](auditoria-seguridad-2026-07-16.md); `provenance: false` fue decisión deliberada (release.yml); ADR-013 §5: «mejora futura» | TENSIÓN T5 | **G63** (el operador lo eleva a grieta) |
| Supply chain: actions por SHA, CodeQL, secret scanning, Dependabot, govulncheck en CI, SECURITY.md | Gaps reales de coste bajo (govulncheck ya corre en `/salud`; SECURITY.md no existe) | NUEVO (infra, no contrato) | Acciones operativas derivadas (§Triaje) |
| Supply chain: permisos mínimos en workflows | Ya al estándar pedido: `contents: read` global, `write` solo en el job de release | YA-HECHO | — |
| Supply chain: build reproducible | `CGO_ENABLED=0`, `-trimpath`, versión verificada contra el tag (ADR-013) | YA-HECHO | — |
| Supply chain: auditoría externa de seguridad pre-1.0 | Solo existe la interna de 2026-07-16 | NUEVO | Registrada dentro de G63 como paso del hito 1.0 |
| Rendimiento: benchmarks públicos + presupuestos + comparación con Pi | Benchmarks Go internos existen (`bench_test.go` del bridge, veto de M15); nada público ni con presupuestos; la comparación con Pi ya es pieza de ADR-025 | YA-DECIDIDO (comparación) + NUEVO (presupuestos) | Acción derivada post-Fase 2 (cuando haya qué medir del producto real) |
| Contratos: API versionada, `enu.has()`, contratos independientes | api.md §17 (nivel 5); contratos por extensión «versionados aparte» | YA-HECHO | — |
| Contratos: «solo añadir acumula decisiones incorregibles» | ADR-025 pieza 4: pre-1.0 admite roturas justificadas por ADR — la crítica ataca una posición que enu no sostiene | ERRÓNEO | §Desacuerdos |
| Contratos: deprecación, ventanas, LTS, política de soporte, tooling de migración | Hueco genuino sin P## | NUEVO → TENSIÓN T8 | **P50** |
| Contratos: conformance suite ejecutable para terceros | La conformance conceptual existe (rondas + 🔎 + 🔒); el artefacto ejecutable para un autor externo, no | NUEVO → TENSIÓN T8 | **P51** |
| Doctor completo | 7 checks kernel reales + 4 de producto en `skip` honesto (G62 → P45); los checks lockfile-dependientes esperan a Fase 2 | YA-HECHO + POSPUESTO (P45) | — |
| UX/docs: API reference verificada, guía primer plugin, migración desde Pi | check-drift como gate de CI (hecho); guía Pi = Fase 3; port automático = P42 | YA-HECHO / YA-DECIDIDO / POSPUESTO | — |
| UX/docs: «la web es simulación teatral»; demo real, GIF, snippet | Converge exactamente con P43 (pasada visual descopada esperando `forge` real; «un screencast de algo que el producto no hizo está descartado») | POSPUESTO (P43) | El feedback se cita como refuerzo externo del disparador de P43 |
| UX/docs: changelog, página de seguridad, matrices | No existen; ítems mecánicos de una sesión de docs futura, no decisiones | NUEVO (menor) | Acciones derivadas |
| Gobernanza: CLA «caso por caso» genera desconfianza | Cierto textualmente (CONTRIBUTING.md §Titularidad) | TENSIÓN T6 | **ADR-030: DCO** |
| Gobernanza: CONTRIBUTING/SECURITY/plantillas a inglés | README ya en inglés (S46); CONTRIBUTING en español; SECURITY.md y plantillas no existen | TENSIÓN T7 | Acción derivada (sesión futura) |
| Anti-features (registry, marketplace, multiagente, más providers, themes, cloud, telemetría por defecto) | Registry y marketplace = P40; más providers en el wizard = P44; themes congelados por ADR-025; telemetría = ausencia deliberada. El multiagente distribuido no tiene P## literal, pero su prerrequisito (la cola durable, P41) ya exige Fase 3 + G60 antes de moverse; el cloud propio y un lenguaje de configuración propio no tienen registro — los rechaza de facto la arquitectura (binario sin host runtime; config en TOML) | YA-HECHO / POSPUESTO | — |
| Definición de 1.0: checklist de 17 criterios | ADR-025 fija un corte **externo** (3 autores ajenos); miden cosas distintas | TENSIÓN T8 | La checklist se adopta como **DoD técnica de la 1.0**, complementaria de la señal externa |
| Priorización: RPC/ACP (paso 3) antes que `forge` (paso 4) | Invierte parcialmente la secuencia Fase 2 → Fase 3 de ADR-025 | TENSIÓN T2 | Se mantiene ADR-025 |

## Tensiones resueltas por el operador (2026-07-19)

- **T1 — Capacidades por plugin.** El manifiesto declarativo + verificación en
  instalación/actualización va al diseño de la Fase 2 (compatible con
  ADR-008). El **enforcement duro por plugin en la frontera Go** se registra
  como **P47** con disparador calcado al de P2, para que no entre por la vía
  de hecho. El claim público se matiza: «capacidades declaradas y
  verificadas», no «capability-secure» (coherente con guia-plugins.md §5).
- **T2 — Secuencia.** Se mantiene Fase 2 → Fase 3: los «tres autores ajenos»
  del criterio de corte salen de `forge` + plugin manager, no de ACP. El orden
  del feedback solo se adoptaría ante demanda real de integración editor/CI —
  esa demanda es el disparador para reabrir esta decisión.
- **T3 — ACP.** Nueva pospuesta **P48**: adaptador ACP sobre el RPC de Fase 3,
  con disparador «RPC/JSONL estable + demanda de un editor ACP-compatible».
  Es extensión (vocabulario de producto), no kernel; comprometerlo hoy sería
  diseñar de rebote sobre algo sin construir.
- **T4 — `enu env` (reproducibilidad del entorno completo).** Nueva pospuesta
  **P49**, con un matiz dictado por el operador: **no es opcional** — «es una
  feature que tiene que estar sí o sí». El disparador (Fase 2 estable) fija el
  *cuándo*, no el *si*; P49 queda marcada como **comprometida**.
- **T5 — Supply chain.** El operador **invierte el triaje de la auditoría de
  seguridad**: SEC-06 (releases sin firma ni atestación) deja de ser «bug de
  infra con dueño difuso» y se eleva a grieta **G63**, abierta, a resolver por
  el flujo de diseño (opciones → decisión → runbook). La higiene de coste bajo
  (SHA-pinning, CodeQL, secret scanning, Dependabot, govulncheck como gate,
  SECURITY.md) se registra como acciones operativas derivadas — es CI/infra,
  no contrato, y no requiere G## propio.
- **T6 — Contribuciones.** Se abandona el «caso por caso»: la política estable
  es **DCO** (el contribuidor conserva su copyright y certifica origen con
  `Signed-off-by`). Decidido en **ADR-030**; CONTRIBUTING.md §Titularidad se
  actualiza en este mismo cambio.
- **T7 — Idioma del frente de gobernanza.** CONTRIBUTING.md pasa a inglés, se
  crea SECURITY.md (inglés, con canal privado de disclosure) y plantillas
  mínimas de issue/PR — como **acción derivada** (sesión futura), coherente
  con ADR-025 pieza 5.
- **T8 — Compatibilidad y 1.0.** Dos pospuestas nuevas: **P50** (régimen de
  compatibilidad post-1.0: deprecación, ventanas, LTS, política de soporte,
  tooling de migración; disparador: inicio de la planificación de la 1.0) y
  **P51** (conformance suite ejecutable para terceros; disparador: el primer
  autor externo — coincide deliberadamente con el criterio de corte de
  ADR-025). La checklist de 17 criterios del feedback se adopta como **DoD
  técnica de la 1.0**: condición necesaria de preparación, complementaria —
  nunca sustituta — de la señal externa de ADR-025.

Tres cierres menores sin tensión, por recomendación de los lotes: el scoping
fino de secretos (`env` por variable) y de red (por host) **amplía P17** a
«scoping por sub-recurso» en vez de abrir pospuestas hermanas (la doctrina
«hacerlo *casi* bien es peor que no tenerlo» aplica idéntica a los tres
recursos); la suite de evaluación del harness, fault injection y soak entran
como **P52/P53/P54** con disparadores propios (timing distinto: fault
injection es montable hoy; el soak necesita que exista el reload de plugins de
Fase 2 como parte del escenario); y el pulido de producto del agente
(undo/checkpoints/contexto Git/selección de archivos) **no** se registra como
pospuesta — nadie lo discutió y aparcó; es diseño no hecho que entra por
`/planificar-sesion` cuando se construya el tooling de edición.

## Triaje final

**Altas (este mismo cambio):**

| Artefacto | Contenido |
|---|---|
| **P47** | Enforcement de capacidades por plugin en la frontera Go (la parte de la propuesta que choca con ADR-008) |
| **P48** | Adaptador ACP (Agent Client Protocol) sobre el RPC de Fase 3 |
| **P49** | `enu env`: reproducibilidad del entorno completo del harness — **comprometida** |
| **P50** | Régimen de compatibilidad post-1.0 (deprecación, LTS, soporte, migración) |
| **P51** | Conformance suite ejecutable para terceros (plugins y providers) |
| **P52** | Suite de evaluación del comportamiento del harness (el loop del agente) |
| **P53** | Arnés de fault injection contra el binario |
| **P54** | Soak tests de sesiones largas |
| **P17** | Ampliada: scoping de `caps` por sub-recurso (rutas de fs, hosts de red, nombres de env) |
| **G63** | Las releases se publican sin firma ni atestación de procedencia (eleva SEC-06 a grieta; abierta) |
| **ADR-030** | Política de contribuciones: DCO |

**Acciones operativas derivadas** (sin G##/P##; pendientes de ejecutar fuera
de esta auditoría): fijar las actions de los workflows por SHA de commit;
activar CodeQL, secret scanning y Dependabot; promover `govulncheck` de
`/salud` a gate de CI; crear SECURITY.md (inglés, disclosure privado);
reescribir CONTRIBUTING.md en inglés (ya con DCO) y añadir plantillas de
issue/PR; kill-test de `enu update`; ampliar las dianas de fuzzing de `/salud`
(TOML/JSON de config, JSONL de sesión, manifiestos, mensajes de workers);
formalizar presupuestos de rendimiento y la comparación pública con Pi cuando
la Fase 2 dé material real; changelog y página de seguridad en la web.

**Insumos de diseño conservados para las fases ya decididas** (no requieren
registro propio; se citan desde aquí): el pipeline de 8 etapas de `forge` y la
inferencia de capacidades mínimas (ronda de pseudocódigo de Fase 2); el
detalle fino del plugin manager (staging+reemplazo atómico, provider simulado
para `plugin test`, diff de código/permisos/dependencias en `update`,
diagnóstico de conflictos de comandos/hooks/keymaps); los campos del `trace`
(replay, compare, export OTel, redacción configurable de prompts) y su
restricción de privacidad (export opt-in, cero telemetría por defecto); los
esquemas del RPC/JSONL mapeados desde los eventos `agent:*` ya especificados.

**Descartado con razón:** el enforcement por plugin *inmediato* (choca con
ADR-008; queda registrado en P47, no ejecutado); las extensiones por editor
(el propio feedback las descarta antes de ACP); el scoring numérico (juicio
editorial no accionable); la resecuenciación RPC-antes-que-forge (T2).

## Desacuerdos registrados con el feedback

1. **«Solo añadir para siempre acumula decisiones que nunca se pueden
   corregir.»** Ataca una posición que enu no sostiene: ADR-025 pieza 4
   admite roturas de firma pre-1.0 justificadas por ADR (nunca por la vía de
   hecho), y el corte de la 1.0 es justamente el momento en que esa válvula se
   cierra. La parte útil de la crítica — qué pasa *después* de congelar — es
   real y queda registrada como P50.
2. **«Capability-secure» sobre-promete.** guia-plugins.md §5 prohíbe vender
   `allow`/`deny` como sandbox, y el aislamiento duro es por worker, no por
   plugin. El posicionamiento honesto hoy es «capacidades declaradas y
   verificadas» (manifiesto de Fase 2); el claim fuerte solo sería legítimo si
   P47 se reabre y se construye.
3. **«La web es una simulación teatral de terminal.»** La estética TTY es una
   decisión (ADR-025, themes congelados) y no se reabre; lo que el feedback
   de verdad señala — ausencia de demo real — es exactamente el disparador de
   P43, que descarta explícitamente fabricar un screencast falso y espera a
   `forge`. Convergencia, no conflicto.
4. **Supply chain como omisión.** La no-firma de releases no era un descuido
   sino una decisión deliberada y documentada (`provenance: false` con
   justificación; ADR-013 §5). El operador la reabre ahora **con conocimiento
   de causa** (G63) — la corrección del feedback es de prioridad, no de
   diagnóstico. Y uno de sus gaps era falso: los permisos de los workflows ya
   son mínimos.
5. **El 10/10 del feedback ya era la tesis del proyecto.** «Solo llega cuando
   usuarios independientes lo someten a cargas que tú no diseñaste» es,
   palabra por palabra, el criterio externo de corte de la 1.0 de ADR-025
   (tres autores ajenos con extensiones no anticipadas). No es una crítica:
   es una confirmación independiente de la decisión ya tomada.
