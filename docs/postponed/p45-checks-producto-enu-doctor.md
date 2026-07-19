---
title: "P45 — Los checks de producto de enu doctor: mecanismo de consulta a extensiones sin efectos + API de declaración de herramientas externas"
type: "pospuesto"
id: "P45"
status: "vigente"
---
# P45 · Los checks de producto de `enu doctor` (`provider.model`/`provider.key`/`tools.external`/`provider.reach`): mecanismo de consulta a extensiones sin efectos + API de declaración de herramientas externas

**Dónde se pospuso.** [ADR-026](../decisions/adr/adr-026-subcomandos-de-gestion-del-binario.md) pieza 3 / [G62](../findings/g62-los-checks-de-producto-de-doctor-presuponen-introspeccion-inexistente.md) (S50, 2026-07-18)

**Por qué.** G62 estrechó `enu doctor` v1 a los 7 checks kernel; los 4 de producto salen como `skip`. Activarlos exige dos piezas de diseño que hoy no existen: (1) una forma de **consultar la semántica de una extensión sin efectos** —hoy solo `Boot()` invoca Lua, y arranca el `init.lua` de TODOS los plugins— para `provider.model`/`provider.key` (la API `providers.resolve`/`secret_env_vars` ya existe, falta cómo invocarla sin bootear todo); y (2) una **API de introspección de herramientas externas** por la que cada extensión declare sus binarios (`git`, `rg`…) de forma consultable (campo en `plugin.toml` o función pública que agregue lo declarado), para `tools.external`. `provider.reach` hereda (1) + el opt-in de red ya especificado. Son decisiones de contrato (api.md/agente.md/providers.md), no de rebote en una sesión de código

**Disparador de reapertura.** Demanda real de que `enu doctor` diagnostique el estado de PRODUCTO (modelo/clave/tools/alcanzabilidad), no solo el kernel; o un **segundo consumidor** que necesite introspección de extensiones sin efectos (p. ej. otro subcomando de gestión o una UI). Primera parada: decidir el mecanismo de consulta sin efectos (¿arranque parcial? ¿contrato de `init.lua` sin efectos?) — probablemente una ronda de pseudocódigo
