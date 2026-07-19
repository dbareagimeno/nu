---
title: "P38 — Dividir el monorepo en repos unitarios bajo una organización"
type: "pospuesto"
id: "P38"
status: "vigente"
---
# P38 · Dividir el monorepo en repos unitarios bajo una organización (core Go / web / plugins oficiales)

**Dónde se pospuso.** [CLAUDE.md](../../CLAUDE.md) §Convenciones de Git / ADR-010; consulta 2026-07-16

**Por qué.** Hoy el monorepo no es comodidad sino **consecuencia de tres acoplamientos de diseño**. (1) Las extensiones oficiales van **embebidas en el binario** con `go:embed` (ADR-010): el build de Go necesita los fuentes Lua en el mismo árbol; separarlas exigiría submodules o vendoring — el *dependency hell* que la filosofía declara enemigo. (2) La web es **derivada de `docs/`**, la fuente de verdad: el gate «Coherencia web ↔ api.md» (`check-drift`, `/sync-web`) puede fallar en el **mismo PR** que introduce la incoherencia solo si ambos viven juntos; entre repos degradaría a detección *a posteriori*. (3) El flujo de trabajo exige **atomicidad multi-documento**: una resolución `G##` se aplica a todos los contratos en un commit, y una sesión cierra feature + puntero + worklog juntos; entre repos no existe el commit atómico y los jueces/auditores de `.claude/` perderían la visión de conjunto. Además, pre-v1 y sin consumidores externos, no hay cadencias de release independientes que comprar: la separación operativa ya la da la CI filtrada por rutas (`ci.yml`/`docs.yml`/`release.yml`). El coste de coordinación se pagaría hoy; el beneficio no llegaría hasta que exista quien consuma las piezas por separado

**Disparador de reapertura.** (1) Un ecosistema de **plugins de comunidad** con ciclo de vida propio — ahí encaja un repo/registry aparte para los *no* embebidos (cf. P4), nunca para los oficiales del binario; (2) que la **web deje de ser derivada** (contenido propio sustancial —blog, playground— y contribuidores que no tocan el core); o (3) **API v1 congelada y estable**, de modo que los plugins puedan desarrollarse contra binarios *publicados* en vez de contra el árbol de trabajo y la atomicidad deje de ser crítica
