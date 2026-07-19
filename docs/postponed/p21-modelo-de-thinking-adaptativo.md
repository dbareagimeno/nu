---
title: "P21 — Modelo de thinking adaptativo (canónico thinking={budget} vs thinking.adaptive)"
type: "pospuesto"
id: "P21"
status: "decidida"
adr: "ADR-016"
---
# P21 · Modelo de `thinking` adaptativo (canónico `thinking={budget}` vs `thinking.adaptive`)

> ✅ **DECIDIDA (ADR-016 / G34); IMPLEMENTADA.** Sale de pospuestos; ver §Resolución.

**Dónde se pospuso.** [providers.md](../contracts/providers.md) §2.1 / revisión de S37 (`decisiones-implementacion.md`)

**Por qué.** El modelo canónico congela `thinking?: { budget?: integer }` y el adaptador `anthropic` (`adapter_anthropic.lua`) lo traduce a la forma extended-thinking *legacy* `{type="enabled", budget_tokens=N}`. Pero la familia Opus 4.6+ (incl. `claude-opus-4-8`) espera `thinking: {type:"adaptive"}` y ha **retirado** `budget_tokens`: un request con `thinking.budget` sobre esos modelos devolvería 400 contra la API real. No es un defecto del código —el adaptador cumple fielmente el contrato congelado, y los tests usan SSE grabado, no red—, sino una grieta del **modelo canónico**. Se pospone porque el contrato actual es autoconsistente y cambiar el modelo de thinking toca a la vez §2.1, el adaptador y posiblemente el control de razonamiento del agente: rediseño transversal que no urge sin un consumidor real

**Disparador de reapertura.** **DECIDIDA (ADR-016, grieta G34); pendiente solo la construcción.** El disparador ya se cumplió (el modelo por defecto es Opus 4.8). Histórico: cuando se conecte el adaptador `anthropic` contra la API real con un modelo Opus 4.6+ y un request con `thinking` reciba 400, o cuando se quiera soportar thinking **adaptativo** de primera clase en el modelo canónico (p. ej. `thinking = { mode = "adaptive" | "off", budget? }` con traducción por-modelo en el adaptador)

## Resolución

P21 era la **única** entrada de este lote que no era una construcción sino una
**decisión del modelo canónico** (`providers.md` §2.1): cambiar una firma del
contrato se decide explícitamente, no por la vía de hecho de un adaptador. Por
eso se reservó. Ahora se ha **reabierto y resuelto** por el flujo de diseño del
proyecto: validada con pseudocódigo ([Ronda 7, escenario 32](../validation/README.md)),
registrada como grieta [G34](../findings/g34-el-modelo-canonico-de-thinking.md) y decidida en
[ADR-016](../decisions/adr/adr-016-modelo-canonico-de-thinking.md).
Sale de pospuestos; queda solo su construcción.

**El problema.** El canónico congelaba `thinking?: { budget?: integer }` y el
adaptador `anthropic` lo traducía a la forma *legacy* `{type="enabled",
budget_tokens=N}`. La familia Opus 4.6+ (incl. el modelo por defecto,
`claude-opus-4-8`) **retiró `budget_tokens`** y espera `{type:"adaptive"}`: una
petición con `thinking.budget` sobre esos modelos da **400**. Era una grieta del
**modelo canónico** (faltaba expresar el modo adaptativo y el dato de qué forma
entiende cada modelo), no del adaptador. Estaba **latente** (el agente no rellena
`req.thinking` por defecto), pero su disparador ya se había cumplido.

**La decisión (ADR-016).** Dos piezas:

1. **Modelo canónico, por adición** → `thinking?: { mode?: "off"|"adaptive"|"budget",
   budget?: integer }`. `thinking` ausente = sin razonamiento; `{budget=N}` sin
   `mode` = alias compatible de `mode="budget"` (no rompe la firma congelada).
2. **El dialecto de cada modelo es un DATO** del `providers.toml`
   (`thinking = "adaptive"|"budget"|"none"`, default `"budget"`), que el adaptador
   lee para traducir **por-modelo** —`adaptive`→`{type="adaptive"}`,
   `budget`→`{type="enabled", budget_tokens=N}`, degradando entre ambos según el
   dialecto—. El adaptador no hardcodea tablas de versiones de modelos
   (ADR-003/ADR-005); se descartó la heurística por id del modelo.

**Estado: IMPLEMENTADA.** Tras decidirse en el ADR, la sesión de construcción
aplicó el contrato: `thinking_to_wire` en `adapter_anthropic.lua` traduce el
`thinking` canónico por dialecto del modelo, `resolve` lleva `model.thinking` al
`ModelInfo`, y `providers_p21_test.go` cubre las ocho combinaciones (dialecto ×
modo). Lo único que falta es **cablear el control de razonamiento desde el
agente** (hoy `req.thinking` solo se puebla por un hook `request.pre`): cuando el
agente exponga esa opción, mapeará a `thinking` — pero el contrato y el adaptador
ya lo soportan.
