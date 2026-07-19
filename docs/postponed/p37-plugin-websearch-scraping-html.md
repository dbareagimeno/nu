---
title: "P37 — Plugin oficial de websearch con scraping propio de HTML"
type: "pospuesto"
id: "P37"
status: "vigente"
---
# P37 · Plugin oficial de **websearch** con **scraping propio de HTML** (bajar una página arbitraria y extraer su texto legible)

**Dónde se pospuso.** [api.md](../contracts/api.md) §8/§10 / [agente.md](../contracts/agente.md) §3; consulta de viabilidad 2026-07-14

**Por qué.** Un plugin de websearch es **vocabulario de producto** → extensión Lua, nunca kernel (idea central 1). Lo importante: la **búsqueda en sí ya es expresable con la API v1 congelada, sin tocar `api.md`** (corolario de completitud, verificado). El transporte es `enu.http.request`/`stream` (§8, `[W]`, primitiva Go paralela); el registro como tool es `agent.tool{...}` (agente.md §3 — MCP encaja ahí *sin caso especial*, y una tool `web_search` es el mismo patrón, marcable `default="allow"` por ser solo lectura); y la respuesta de una API de búsqueda/*reader* (Brave/Tavily/Exa, o `r.jina.ai` que devuelve markdown) se decodifica con `enu.json.decode` (§12) o se renderiza con `enu.text.markdown` (§10). Alternativa sin plugin propio: **web search nativo del provider** (Anthropic/OpenAI lo exponen server-side; encaja en providers.md, cerca de P5/P6). Por eso esto **no** se registra como grieta `G##`: se construye hoy. El **único** caso que forzaría API nueva es el scraping de HTML **crudo** → texto: el core no tiene parser de HTML (`enu.text.markdown` *renderiza* markdown, no *parsea* HTML), y hacerlo en Lua puro sería CPU ardiendo (idea central 3). Se evita delegando en un *reader* que devuelva markdown o en el web-search del provider

**Disparador de reapertura.** Demanda de un plugin oficial de websearch que haga **scraping propio de HTML arbitrario** (no vía *reader* externo tipo Jina ni vía web-search nativo del provider) — ahí se abre una grieta `G##` por la primitiva de extracción HTML→texto (¿`enu.html`?, ¿ampliar `enu.text`?), que se decide primero en `problemas.md`→`api.md` antes de implementar. Mientras la lectura pase por una API de búsqueda/*reader* que devuelva JSON o markdown, o por el provider, no hay nada que decidir
