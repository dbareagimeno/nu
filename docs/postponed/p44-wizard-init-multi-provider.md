---
title: "P44 — El wizard de enu init ofrece también openai-compat/gemini/ollama (multi-provider)"
type: "pospuesto"
id: "P44"
status: "vigente"
---
# P44 · El wizard de `enu init` ofrece también `openai-compat`/`gemini`/`ollama` (multi-provider)

**Dónde se pospuso.** [ADR-026](../decisions/adr/adr-026-subcomandos-de-gestion-del-binario.md) pieza 2 / [G61](../findings/g61-el-wizard-de-init-ofrece-providers-sin-plantilla.md) (S49, 2026-07-18)

**Por qué.** G61 destapó que la pieza 2 nombra cuatro providers pero solo `anthropic` tiene plantilla (ADR-017). El wizard v1 se estrechó a `anthropic` (el default del producto). Ampliarlo exige diseñar antes las plantillas de los otros: `base_url` por defecto, convención de `api_key_env` (`OPENAI_API_KEY`?, `GEMINI_API_KEY`?), modelo y `context` de referencia — y cómo encaja el flujo sin-key de `ollama` (local, normalmente sin API key), que rompe el paso «clave por variable de entorno» del asistente

**Disparador de reapertura.** Diseño de las plantillas de config de `openai-compat`/`gemini`/`ollama` en `providers.md`/`agente.md` (seguramente junto al trabajo de adaptadores oficiales, cf. P30 ya implementado) + demanda real de configurar esos providers desde el wizard en vez de a mano en `providers.toml`
