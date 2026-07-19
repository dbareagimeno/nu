---
title: "CP-12 — cierre de la Fase 9: funnel smoke + mutación 🔒 batcheada (S49/S50/S51)"
type: "checkpoint"
id: "CP-12"
phase: 9
status: "cerrada"
---
# CP-12 — El camino del desconocido (cierre de la Fase 9)

Checkpoint de cierre de la Fase 9 (Producto) y del plan de construcción (9/9
fases). Dos partes, ambas verdes.

## 1. Funnel smoke — «enu se adopta sin ayuda»

Prueba de humo del funnel del README contra el **binario estático real**
(compilado con las flags de release, `CGO_ENABLED=0 -trimpath -ldflags "-s
-w"`), en un `HOME`/`XDG_*` temporal limpio:

| Paso del README | Resultado |
|---|---|
| Bare runtime headless (`enu -e 'return enu.version.api'`) | `5` (arranca sin config) |
| `enu --default-config` | exit 0 · activa el conjunto oficial + siembra `agent.toml`/`providers.toml` |
| `enu init --yes` (primer contacto, otro HOME) | exit 0 · idéntico onramp |
| `enu doctor` | exit 0 · **5 ok · 0 fail** · skips honestos (sin sesiones, headless, 4 de producto G62, `--net`) |
| `enu doctor --json` | valida `doctor.v1` |

Además, los **dos subcomandos nuevos como PROCESOS reales** (nunca ejecutados
fuera de los unit tests):
- `enu update`: alcanzó la API real de GitHub releases, resolvió la última
  estable (v0.2.0) y **no-op'eó** honestamente («ya estás en v0.2.0») — el
  camino feliz-de-no-op validado contra red real.
- `enu uninstall --purge` (con `y`): borró binario + `config.dir()` y dejó
  `data_dir()` **intacto** (fichero de sesión centinela superviviente). La
  invariante 🔒 «los datos no se tocan nunca» validada como proceso real, no
  solo por unit test.

**No ejecutable aquí:** el primer paso literal del README, `curl -fsSL
…/install.sh | sh`, necesita una **release publicada** (no la hay aún). Ese
camino queda cubierto por la **matriz de S48** (arranque del binario en
contenedores mínimos limpios) y el **job `checksum` de S51** (verificación shell:
corrupto rechazado + íntegro aceptado). El resto del funnel —lo que sí depende
del binario y de la config— se sostiene contra el binario real. Es la misma
honestidad de CP-11 (adaptado sin red) y CP-7 (parte manual): se marca verde lo
verificable y se nombra explícitamente lo que un entorno con release cubrirá.

## 2. Mutación 🔒 batcheada (S49 + S50 + S51)

Las tres sesiones de código 🔒 de la Fase 9 difirieron su `/mutacion` individual
(decisión del operador) y se corre aquí junta, con **gremlins 0.6.0 pineado**.

**Incidencia de tooling anotada:** gremlins copia el módulo entero a un workdir
por worker y su copiador **tropieza al recorrer `.git`** (panic «error, this is
temporary» en `wdDealer.Get`); no es disco ni permisos (29 GB libres, sin dirs
read-only). Solución: ejecutar desde una **copia limpia por `tar`** del árbol de
trabajo excluyendo `.git`/`.claude`/`spike`. Es exactamente el riesgo que la
propia skill advierte («tool 0.x, un upgrade de Go puede romperla»); por eso
gremlins nunca es gate de CI.

Dos pasadas acotadas (`--timeout-coefficient 300`, exclusiones hasta dejar solo
los ficheros de las sesiones):

| Pasada | Ficheros | Antes | Después |
|---|---|---|---|
| A | `init.go` `doctor.go` `update.go` `uninstall.go` | 93 killed · 86.9% | **99 killed · 92.5%** |
| B | `bare_screen.go` `doctor_support.go` | 24 killed · 81% cob. | **29 killed · 97% cob.** |

**5 tests nuevos** que matan los mutantes de **lógica real**:
- `TestJsonStringValue` (main) — límites `< 0` del extractor del parseo de
  releases (marcador/comilla en índice 0).
- `TestDoctorHumanDetailGating` (main) — el contrato de `doctor.md`: el detalle
  se imprime SOLO en `fail`/`skip`, nunca en `ok` (un `ok` que filtrara su
  detalle volcaría info que el modo humano resume a propósito).
- aserción de **conteo positivo** en el detalle de `sessions.perms` — mata el
  `seen--` (que rendiría «-1 transcript(s)»).
- `TestDiagnosePluginGraph` (runtime) — las ramas de error del diagnóstico del
  grafo (discover falla por colisión; requires falla por ciclo/ausente), antes
  solo ejercitadas desde `package main`.
- `TestWriteInitConfig` (runtime) — la semántica por-fichero de `enu init` en su
  propio paquete (con/sin activación, segunda pasada respeta plantillas).

**LIVED restantes, todos ANOTADOS como legítimos** (no son huecos de la suite):
- **Glue de wrappers `*Main`** (`runUpdateMain`/`runUninstallMain`/`runDoctorMain`:
  parseo de flags, `os.Executable`, `EvalSymlinks`) y el **adaptador HTTP real**
  (`httpReleaseFetcher`) — NOT COVERED por diseño: la política de tests exime el
  glue fino sin lógica; los núcleos testeables (`runUpdate`/`runUninstall`/
  `runDoctor`) están totalmente muertos.
- **Mensajes cosméticos** del wizard `enu init` (`init.go` 130/167/170) — ramas
  de `emitf` informativas; ningún invariante 🔒 depende de ellas.
- **`jsonStringValue` q2<0** (`update.go` 339) — mutante **equivalente**: con la
  comilla de cierre en índice 0 (valor vacío) ambas ramas rinden `""`.
- **Rendering de la pantalla desnuda de S33** (`bare_screen.go` 255/258/274, el
  método `lines()`) — **fuera del batch** de la Fase 9: es código de S33, no una
  adición de S49/S50/S51. Las adiciones de S49 a ese fichero (`WriteInitConfig`,
  `agentTomlFor`, `writeTemplateIfAbsent`) sí quedan muertas.

## Cierre

Suite completa `go test -race -shuffle=on ./ ./internal/runtime/` verde;
`build`/`vet`/`gofmt` limpios. **Fase 9 cerrada, plan de construcción completo
(9/9 fases).** El puntero pasa a `—`: el trabajo futuro entra por
`/planificar-sesion` o por el flujo de diseño (G##/ADR), no por el plan.
