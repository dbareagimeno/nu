---
title: "Discusiones pospuestas (P##)"
description: "Lo que se decidió no decidir todavía (P##), un fichero por entrada, cada uno con su disparador de reapertura."
type: "indice"
status: "vigente"
---
# Discusiones pospuestas

Registro de todo lo que se ha decidido **no decidir todavía**. Cada entrada es
un fichero `pNN-<slug>.md` con su *disparador*: la señal que indica que toca
reabrirla. Nada de esta lista está rechazado; está esperando su momento. Cuando
una entrada se reabra y se decida, sale de aquí (su fichero se marca
`decidida`) y la decisión entra en el ADR.

> **Estado: 55 registradas — 43 vigentes, 2 decididas ([P4](p04-package-manager-de-plugins.md)→ADR-025, [P21](p21-modelo-de-thinking-adaptativo.md)→ADR-016) y 10 implementadas (P22–P31).**

**Implementación diferida (P22–P31).** Un grupo de entradas no esperaba una
*decisión* sino una *construcción*: su diseño ya vivía en el contrato de su
extensión (`agente.md`, `chat.md`, `providers.md`), pero la primera versión de
las extensiones oficiales las dejó fuera de alcance. **Ya están IMPLEMENTADAS**
(rama `claude/postponed-items-priority`, con tests Go que las blindan); sus
ficheros conservan el rastro del diseño, y su *disparador* ya no aplica.

**P21 DECIDIDA** ([ADR-016](../decisions/adr/adr-016-modelo-canonico-de-thinking.md),
grieta [G34](../findings/g34-el-modelo-canonico-de-thinking.md)): era la única
entrada que no era una construcción sino una decisión del modelo canónico; se
reabrió y se resolvió por el flujo de diseño. La resolución completa vive en su
propio fichero.

| # | Tema | Dónde se pospuso | Estado | Fichero |
|---|------|------------------|--------|---------|
| P1 | TCP/sockets crudos (enu.net.tcp) | [api.md](../contracts/api.md) §8 | vigente | [p01-tcp-sockets-crudos.md](p01-tcp-sockets-crudos.md) |
| P2 | Actores por plugin (isolated = true) | ADR-008 | vigente | [p02-actores-por-plugin.md](p02-actores-por-plugin.md) |
| P3 | Plugins WASM (segunda capa) | ADR-002 | vigente | [p03-plugins-wasm-segunda-capa.md](p03-plugins-wasm-segunda-capa.md) |
| P4 | Package manager de plugins | [filosofia.md](../core/filosofia.md) / [arquitectura.md](../core/arquitectura.md) | decidida | [p04-package-manager-de-plugins.md](p04-package-manager-de-plugins.md) |
| P5 | Embeddings y endpoints no-chat | [providers.md](../contracts/providers.md) §5 | vigente | [p05-embeddings-endpoints-no-chat.md](p05-embeddings-endpoints-no-chat.md) |
| P6 | Imágenes/archivos generados por el modelo | [providers.md](../contracts/providers.md) §5 | vigente | [p06-imagenes-archivos-generados-modelo.md](p06-imagenes-archivos-generados-modelo.md) |
| P7 | Cifrado en reposo / redacción de secretos en transcripts | [sesiones.md](../contracts/sesiones.md) §8 | vigente | [p07-cifrado-en-reposo-secretos-transcripts.md](p07-cifrado-en-reposo-secretos-transcripts.md) |
| P8 | Índice global de sesiones | [sesiones.md](../contracts/sesiones.md) §7 | vigente | [p08-indice-global-de-sesiones.md](p08-indice-global-de-sesiones.md) |
| P9 | Sincronización de sesiones entre máquinas | [sesiones.md](../contracts/sesiones.md) §8 | vigente | [p09-sincronizacion-sesiones-entre-maquinas.md](p09-sincronizacion-sesiones-entre-maquinas.md) |
| P10 | Política de retención/GC de sesiones | [sesiones.md](../contracts/sesiones.md) §8 | vigente | [p10-retencion-gc-sesiones.md](p10-retencion-gc-sesiones.md) |
| P11 | Workers anidados | [api.md](../contracts/api.md) §13 | vigente | [p11-workers-anidados.md](p11-workers-anidados.md) |
| P12 | Ejecución paralela de tool calls de un mismo turno | [agente.md](../contracts/agente.md) §4 | vigente | [p12-ejecucion-paralela-tool-calls.md](p12-ejecucion-paralela-tool-calls.md) |
| P13 | Animación/repintado de alta frecuencia (>~30 fps) | [modelo-ejecucion.md](../core/modelo-ejecucion.md) §limitaciones | vigente | [p13-animacion-repintado-alta-frecuencia.md](p13-animacion-repintado-alta-frecuencia.md) |
| P14 | Splits / vista multi-sesión en chat | [chat.md](../contracts/chat.md) §1 | vigente | [p14-splits-vista-multi-sesion-chat.md](p14-splits-vista-multi-sesion-chat.md) |
| P15 | Búsqueda dentro del transcript | [chat.md](../contracts/chat.md) §10 | vigente | [p15-busqueda-dentro-transcript.md](p15-busqueda-dentro-transcript.md) |
| P16 | Modo vim en el editor de input | [chat.md](../contracts/chat.md) §10 | vigente | [p16-modo-vim-editor-input.md](p16-modo-vim-editor-input.md) |
| P17 | Scoping de caps por rutas (fs.read:/repo) | [problemas.md](../findings/README.md) G6 | vigente | [p17-scoping-de-caps-por-rutas.md](p17-scoping-de-caps-por-rutas.md) |
| P18 | Windows nativo (sin WSL2) | [problemas.md](../findings/README.md) G9 | vigente | [p18-windows-nativo-sin-wsl2.md](p18-windows-nativo-sin-wsl2.md) |
| P19 | Listener HTTP mínimo (listen_once) para callbacks OAuth | [problemas.md](../findings/README.md) G13 | vigente | [p19-listener-http-minimo-callbacks-oauth.md](p19-listener-http-minimo-callbacks-oauth.md) |
| P20 | Rotación/límite del fichero de enu.log | [api.md](../contracts/api.md) §15 / [pseudocodigo.md](../validation/README.md) ronda 4 | vigente | [p20-rotacion-limite-fichero-enu-log.md](p20-rotacion-limite-fichero-enu-log.md) |
| P21 | Modelo de thinking adaptativo (canónico thinking={budget} vs thinking.adaptive) | [providers.md](../contracts/providers.md) §2.1 / revisión de S37 (`decisiones-implementacion.md`) | decidida | [p21-modelo-de-thinking-adaptativo.md](p21-modelo-de-thinking-adaptativo.md) |
| P22 | Métodos de control de sesión Session:cancel/fork/compact/clear_queue | [agente.md](../contracts/agente.md) §2 | implementada | [p22-metodos-de-control-de-sesion.md](p22-metodos-de-control-de-sesion.md) |
| P23 | Cola de reentrada de Session:send (G4) | [agente.md](../contracts/agente.md) §2 (Reentrada G4) | implementada | [p23-cola-de-reentrada-de-send.md](p23-cola-de-reentrada-de-send.md) |
| P24 | Inyección de contenido del repo en el system prompt: índice de skills y enu.md (+ puerta TOFU) | [agente.md](../contracts/agente.md) §6 / §7 / §11.2 | implementada | [p24-inyeccion-de-contenido-del-repo.md](p24-inyeccion-de-contenido-del-repo.md) |
| P25 | Compactación automática: disparo por umbral y evento agent:compact | [agente.md](../contracts/agente.md) §8 / §4 | implementada | [p25-compactacion-automatica-disparo-umbral.md](p25-compactacion-automatica-disparo-umbral.md) |
| P26 | chat: menciones @ con picker difuso de ficheros | [chat.md](../contracts/chat.md) §3 | implementada | [p26-chat-menciones-picker-difuso-ficheros.md](p26-chat-menciones-picker-difuso-ficheros.md) |
| P27 | chat: render en vivo de agent:tool.progress y marca de agent:compact | [chat.md](../contracts/chat.md) §2 | implementada | [p27-chat-render-vivo-tool-progress.md](p27-chat-render-vivo-tool-progress.md) |
| P28 | chat: comandos builtin /fork y /permissions | [chat.md](../contracts/chat.md) §4 | implementada | [p28-chat-comandos-builtin-fork-permissions.md](p28-chat-comandos-builtin-fork-permissions.md) |
| P29 | chat: permitir siempre persistente y autocompletado visual de / | [chat.md](../contracts/chat.md) §5 / §3 | implementada | [p29-chat-permitir-siempre-autocompletado.md](p29-chat-permitir-siempre-autocompletado.md) |
| P30 | Adaptadores oficiales openai-compat y gemini | [providers.md](../contracts/providers.md) §4 | implementada | [p30-adaptadores-oficiales-openai-compat-gemini.md](p30-adaptadores-oficiales-openai-compat-gemini.md) |
| P31 | Prompt caching automático (cache_control) en el adaptador anthropic | [providers.md](../contracts/providers.md) §3 (obligación 6) | implementada | [p31-prompt-caching-automatico-anthropic.md](p31-prompt-caching-automatico-anthropic.md) |
| P32 | Reducir el peaje de rendimiento del backend wasm (veto 2 de M15) | [migracion-vm.md](../archive/migracion-vm.md) §5 / bitácora M15 | vigente | [p32-reducir-peaje-rendimiento-backend-wasm.md](p32-reducir-peaje-rendimiento-backend-wasm.md) |
| P33 | Cancelar una primitiva ⏸ en vuelo: HostFn no recibe context.Context | `api.md` §1.3 / `vmwasm/scheduler.go` | vigente | [p33-cancelar-primitiva-en-vuelo.md](p33-cancelar-primitiva-en-vuelo.md) |
| P34 | Ciclo de vida de los handles wasm: la tabla es monotónica | `vmwasm/handle.go` / `vmwasm_ws.go` | vigente | [p34-ciclo-vida-handles-wasm.md](p34-ciclo-vida-handles-wasm.md) |
| P35 | enu.plugin.reload best-effort ante colisión de nombres de módulo entre plugins (G2) | `vmwasm/loader.go` / decisión de S13 (`decisiones-implementacion.md`); auditoría 2026-07-12, A-41 | vigente | [p35-reload-colision-nombres-modulo.md](p35-reload-colision-nombres-modulo.md) |
| P36 | Provider openai-oauth: modelos OpenAI con la suscripción ChatGPT | [providers.md](../contracts/providers.md) §4; investigación 2026-07-14 | vigente | [p36-provider-openai-oauth-suscripcion-chatgpt.md](p36-provider-openai-oauth-suscripcion-chatgpt.md) |
| P37 | Plugin oficial de websearch con scraping propio de HTML | [api.md](../contracts/api.md) §8/§10 / [agente.md](../contracts/agente.md) §3; consulta de viabilidad 2026-07-14 | vigente | [p37-plugin-websearch-scraping-html.md](p37-plugin-websearch-scraping-html.md) |
| P38 | Dividir el monorepo en repos unitarios bajo una organización | [CLAUDE.md](../../CLAUDE.md) §Convenciones de Git / ADR-010; consulta 2026-07-16 | vigente | [p38-dividir-monorepo-repos-unitarios.md](p38-dividir-monorepo-repos-unitarios.md) |
| P39 | Emparejamiento de permisos de bash sobre el programa parseado | [agente.md](../contracts/agente.md) §5 / [problemas.md](../findings/README.md) G53 / ADR-023 | vigente | [p39-permisos-bash-programa-parseado.md](p39-permisos-bash-programa-parseado.md) |
| P40 | Registry central de plugins (más allá del gestor git de P4→ADR-025) | [ADR-025](../decisions/adr/adr-025-reposicionamiento-motor-de-harnesses.md), pieza 3 / [auditoría externa 2026-07-18](../audits/auditoria-externa-concepto-2026-07-18.md) | vigente | [p40-registry-central-plugins.md](p40-registry-central-plugins.md) |
| P41 | Cola durable de tasks: trabajos que sobreviven al proceso, se encolan y se reintentan | [ADR-025](../decisions/adr/adr-025-reposicionamiento-motor-de-harnesses.md), pieza 3 / discusión de [G60](../findings/g60-el-lock-de-sesion-nace-huerfano.md) (2026-07-18) | vigente | [p41-cola-durable-de-tasks.md](p41-cola-durable-de-tasks.md) |
| P42 | Port automático de extensiones de Pi (TypeScript) a plugins Lua vía forge | [ADR-025](../decisions/adr/adr-025-reposicionamiento-motor-de-harnesses.md), pieza 3 / [auditoría externa 2026-07-18](../audits/auditoria-externa-concepto-2026-07-18.md) | vigente | [p42-port-automatico-extensiones-pi.md](p42-port-automatico-extensiones-pi.md) |
| P43 | Pasada visual de la portada (demo del hero + snippet de plugin + jerarquía de enlaces primarios sobre atajos + slot de demo) | [ADR-025](../decisions/adr/adr-025-reposicionamiento-motor-de-harnesses.md), pieza 3 (Fase 1) / S47 (descopada por el operador 2026-07-18) | vigente | [p43-pasada-visual-de-la-portada.md](p43-pasada-visual-de-la-portada.md) |
| P44 | El wizard de enu init ofrece también openai-compat/gemini/ollama (multi-provider) — **reabierto** como onramp provider-neutral | [ADR-026](../decisions/adr/adr-026-subcomandos-de-gestion-del-binario.md) pieza 2 / [G61](../findings/g61-el-wizard-de-init-ofrece-providers-sin-plantilla.md) (S49, 2026-07-18) | vigente (reabierto 2026-07-20, pendiente de ADR) | [p44-wizard-init-multi-provider.md](p44-wizard-init-multi-provider.md) |
| P45 | Los checks de producto de enu doctor: mecanismo de consulta a extensiones sin efectos + API de declaración de herramientas externas | [ADR-026](../decisions/adr/adr-026-subcomandos-de-gestion-del-binario.md) pieza 3 / [G62](../findings/g62-los-checks-de-producto-de-doctor-presuponen-introspeccion-inexistente.md) (S50, 2026-07-18) | vigente | [p45-checks-producto-enu-doctor.md](p45-checks-producto-enu-doctor.md) |
| P46 | Permitir suspensión (⏸) directa en los liberadores de enu.task.cleanup (opción A1 de G60) | [G60](../findings/g60-el-lock-de-sesion-nace-huerfano.md) / [ADR-029](../decisions/adr/adr-029-resiliencia-lease-reclamable-reconciliacion.md) (2026-07-19) | vigente | [p46-suspension-directa-en-cleanups.md](p46-suspension-directa-en-cleanups.md) |
| P47 | Enforcement de capacidades por plugin en la frontera Go (la mitad dura de la propuesta «capability-secure» del feedback 2026-07-19) | [Auditoría del feedback 2026-07-19](../audits/auditoria-feedback-10-de-10-2026-07-19.md), tensión T1 / ADR-008 | vigente | [p47-enforcement-capacidades-por-plugin.md](p47-enforcement-capacidades-por-plugin.md) |
| P48 | Adaptador ACP (Agent Client Protocol): enu como agente ACP para integrarse en editores compatibles sin extensión por editor | [Auditoría del feedback 2026-07-19](../audits/auditoria-feedback-10-de-10-2026-07-19.md), tensión T3 | vigente | [p48-adaptador-acp.md](p48-adaptador-acp.md) |
| P49 | enu env: reproducibilidad del entorno completo del harness (lock/verify/export/reproduce) — comprometida: tiene que estar | [Auditoría del feedback 2026-07-19](../audits/auditoria-feedback-10-de-10-2026-07-19.md), tensión T4 | vigente | [p49-enu-env-reproducibilidad-entorno.md](p49-enu-env-reproducibilidad-entorno.md) |
| P50 | Régimen de compatibilidad post-1.0: mecanismo de deprecación con ventanas y warnings accionables, guías/tooling de migración (enu migrate, enu plugin check --against), versiones LTS y política pública de soporte | [Auditoría del feedback 2026-07-19](../audits/auditoria-feedback-10-de-10-2026-07-19.md), tensión T8 | vigente | [p50-regimen-compatibilidad-post-1-0.md](p50-regimen-compatibilidad-post-1-0.md) |
| P51 | Conformance suite ejecutable para terceros: el artefacto que un autor externo corre para certificar su plugin/provider contra los contratos (incluye tests contractuales de providers) | [Auditoría del feedback 2026-07-19](../audits/auditoria-feedback-10-de-10-2026-07-19.md), tensión T8 | vigente | [p51-conformance-suite-terceros.md](p51-conformance-suite-terceros.md) |
| P52 | Suite de evaluación del comportamiento del harness: tareas deterministas (edición multiarchivo, bug fixing, navegación de repos grandes, recuperación tras errores, respeto de permisos, eficiencia de contexto) para comparar versiones de enu con el mismo modelo y detectar regresiones del loop del agente | [Auditoría del feedback 2026-07-19](../audits/auditoria-feedback-10-de-10-2026-07-19.md) (lote L2) | vigente | [p52-suite-evaluacion-comportamiento-harness.md](p52-suite-evaluacion-comportamiento-harness.md) |
| P53 | Arnés de fault injection: I/O adversa contra el binario completo — HTTP 429/500, streams truncados, respuestas inválidas, disco lleno, fs read-only, permisos que cambian, procesos que no terminan, plugins que bloquean o panican, workers que mueren sin responder | [Auditoría del feedback 2026-07-19](../audits/auditoria-feedback-10-de-10-2026-07-19.md) (lote L4) | vigente | [p53-arnes-fault-injection.md](p53-arnes-fault-injection.md) |
| P54 | Soak tests de sesiones largas: horas de uso con miles de eventos, cientos de tool calls, compactaciones múltiples, reload de plugins, procesos concurrentes y repos grandes | [Auditoría del feedback 2026-07-19](../audits/auditoria-feedback-10-de-10-2026-07-19.md) (lote L4) | vigente | [p54-soak-tests-sesiones-largas.md](p54-soak-tests-sesiones-largas.md) |
| P55 | Fusión de entorno en `enu.proc`: `opts.env` presente reemplaza el heredado y no hay `enu.sys.environ()` para fusionar en Lua; un servidor de `mcp.toml` que declara un solo secreto pierde PATH/HOME | Resolución de [G65](../findings/g65-proc-spawn-ignora-env-array-en-silencio.md) (2026-07-21) | vigente | [p55-fusion-de-entorno-en-proc.md](p55-fusion-de-entorno-en-proc.md) |
