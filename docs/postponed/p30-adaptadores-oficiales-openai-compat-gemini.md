---
title: "P30 — Adaptadores oficiales openai-compat y gemini"
type: "pospuesto"
id: "P30"
status: "implementada"
---
# P30 · Adaptadores oficiales `openai-compat` y `gemini`

> ✅ **IMPLEMENTADA** (rama `claude/postponed-items-priority`, tests Go que la blindan).

**Dónde se pospuso.** [providers.md](../contracts/providers.md) §4

**Por qué.** El contrato dice que `anthropic`, `openai-compat` y `gemini` van embebidos. La extensión `0.1.0` incluye solo `anthropic` (+ `adapter_stub` de pruebas). Un `providers.toml` con `adapter = "openai-compat"` falla al resolver (intenta `require("openai-compat")`). El contrato del adaptador (§3) está validado por `anthropic`; añadir los otros dos es escribir dos traductores más, sin tocar el modelo canónico

**Disparador de reapertura.** Demanda de usar modelos OpenAI-compatibles (incl. locales tipo Ollama) o Gemini sin escribir un adaptador propio
