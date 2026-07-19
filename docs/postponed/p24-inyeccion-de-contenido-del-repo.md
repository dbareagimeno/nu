---
title: "P24 — Inyección de contenido del repo en el system prompt: índice de skills y enu.md (+ puerta TOFU)"
type: "pospuesto"
id: "P24"
status: "implementada"
---
# P24 · Inyección de contenido del repo en el system prompt: índice de skills y `enu.md` (+ puerta TOFU)

> ✅ **IMPLEMENTADA** (rama `claude/postponed-items-priority`, tests Go que la blindan).

**Dónde se pospuso.** [agente.md](../contracts/agente.md) §6 / §7 / §11.2

**Por qué.** El contrato describe descubrimiento de skills (`config.dir()/skills/` + `<repo>/.enu/skills/`), `agent.skills.list()`, inyección en dos fases (índice en el system prompt + carga bajo demanda vía tool `skill`) y la inyección de `enu.md` del repo como contexto, todo tras la puerta TOFU (§11.2). El ensamblado de la `0.1.0` es base → `opts.system` (comentario en código: "índice de skills, S39 no las carga"): ni skills, ni `enu.md`, ni su TOFU. La regla de permisos deny-only del repo (§11.1) sí está. Sin esto, el agente no ve contexto del proyecto ni skills

**Disparador de reapertura.** Demanda de reutilizar skills del ecosistema o de que el agente lea `enu.md`; o cerrar §7 (qué piezas ensambla el system prompt) de forma definitiva
