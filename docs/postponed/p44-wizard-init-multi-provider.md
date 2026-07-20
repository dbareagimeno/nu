---
title: "P44 — El wizard de enu init ofrece también openai-compat/gemini/ollama (multi-provider)"
type: "pospuesto"
id: "P44"
status: "vigente"
---
# P44 · El wizard de `enu init` ofrece también `openai-compat`/`gemini`/`ollama` (multi-provider)

> **REABIERTO (2026-07-20).** El disparador sonó: se decidió atacar el
> multi-provider como parte del rediseño del onramp de primer arranque. La
> dirección acordada **amplía** el alcance original de P44 (que era solo «el wizard
> ofrece los otros providers») a un **onramp PROVIDER-NEUTRAL**:
> - **5 presets sin privilegio** — Anthropic, OpenAI (`openai-compat` +
>   `https://api.openai.com/v1`), Gemini, Ollama (`openai-compat` local, **sin
>   clave**) y Custom (`openai-compat` con `base_url` a mano) —, cada uno solo
>   **infra** (adapter, base_url, api_key_env). Convención de clave:
>   `OPENAI_API_KEY` / `GEMINI_API_KEY`; Ollama/Custom-local sin `api_key_env` (el
>   paso de clave se salta). Los tres adaptadores ya existen (P30 cerrado) y
>   `providers.resolve` ya tolera la falta de clave.
> - **Ningún modelo bendecido**: lo aporta el usuario (debe saber cuáles hay live;
>   listar modelos en vivo → posible P## futuro).
> - **Anthropic pierde su privilegio**: al implementarse, esto **superseda la pieza
>   1 de [ADR-017](../decisions/adr/adr-017-el-onramp-deja-config.md)** («default
>   opinado a Anthropic» + modelo bendecido) y **desestrecha
>   [G61](../findings/g61-el-wizard-de-init-ofrece-providers-sin-plantilla.md)**;
>   `--default-config` se vuelve **neutral** (infra sin modelo; flags
>   `--provider`/`--model`), refinando ADR-015. Consecuencia asumida: **ya no hay
>   "just-works sin config"**.
>
> **Pendiente de ADR + priorización** (registro completo en la ficha de plan del
> onramp; funda la Pieza 2 = el setup navegable de
> [G66](../findings/g66-la-activacion-interactiva-no-siembra-config.md)). Sigue
> **vigente** hasta que se escriba ese ADR.

**Dónde se pospuso.** [ADR-026](../decisions/adr/adr-026-subcomandos-de-gestion-del-binario.md) pieza 2 / [G61](../findings/g61-el-wizard-de-init-ofrece-providers-sin-plantilla.md) (S49, 2026-07-18)

**Por qué.** G61 destapó que la pieza 2 nombra cuatro providers pero solo `anthropic` tiene plantilla (ADR-017). El wizard v1 se estrechó a `anthropic` (el default del producto). Ampliarlo exige diseñar antes las plantillas de los otros: `base_url` por defecto, convención de `api_key_env` (`OPENAI_API_KEY`?, `GEMINI_API_KEY`?), modelo y `context` de referencia — y cómo encaja el flujo sin-key de `ollama` (local, normalmente sin API key), que rompe el paso «clave por variable de entorno» del asistente

**Disparador de reapertura.** Diseño de las plantillas de config de `openai-compat`/`gemini`/`ollama` en `providers.md`/`agente.md` (seguramente junto al trabajo de adaptadores oficiales, cf. P30 ya implementado) + demanda real de configurar esos providers desde el wizard en vez de a mano en `providers.toml`
