---
title: "P36 — Provider openai-oauth: modelos OpenAI con la suscripción ChatGPT"
type: "pospuesto"
id: "P36"
status: "vigente"
---
# P36 · Provider `openai-oauth`: consumir modelos OpenAI con la **suscripción ChatGPT** (flujo "Sign in with ChatGPT" de Codex CLI) en vez de API key

**Dónde se pospuso.** [providers.md](../contracts/providers.md) §4; investigación 2026-07-14

**Por qué.** Técnicamente reproducible hoy: PKCE contra `auth.openai.com` con el `client_id` público de Codex (`app_EMoamEEZ73f0CkXaXp7hrann`), tokens en `~/.codex/auth.json`, y consumo de `chatgpt.com/backend-api/codex/responses` con `Authorization: Bearer` + `ChatGPT-Account-ID` + `originator: codex_cli_rs`; un ecosistema amplio ya lo hace (plugin LLM de Willison, opencode, Cline, Zed) y OpenAI **tolera sin sancionar** (declaraciones públicas de DevRel a favor; solo un gate blando de headers). Pero es zona gris **no bendecida**: no existe programa de registro de clients de terceros para inferencia por suscripción, los ToU tienen tres prohibiciones generales aplicables (extracción programática, reverse engineering, circumvención de límites), y el precedente Anthropic (feb 2026: términos explícitos + baneos + 401 fuera del path sancionado) marca el techo del rug pull posible. El riesgo recae en la **cuenta del usuario**. Si algún día se hace, la forma menos agresiva es opt-in experimental que **reutilice el login de Codex** (leer `~/.codex/auth.json`, no reimplementar el browser-flow) con disclaimer, sin spoofear headers; la vía oficial y estable sigue siendo la API key, que es lo que providers.md ya presupone

**Disparador de reapertura.** Que OpenAI publique un programa oficial de "Sign in with ChatGPT" con **inferencia por suscripción** para apps de terceros (hoy solo concede identidad + créditos de API); o demanda real de usuarios que acepten explícitamente el riesgo de un provider experimental no sancionado
