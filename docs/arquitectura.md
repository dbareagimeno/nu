# Arquitectura de nu

Estado: borrador fundacional. Esto describe la forma del sistema, no una
especificación cerrada. Las decisiones y su razonamiento viven en
[adr.md](adr.md).

## Vista general

```
┌─────────────────────────────────────────────────────────┐
│                  Extensiones de usuario (Lua)           │
├─────────────────────────────────────────────────────────┤
│         Extensiones oficiales (Lua, go:embed)           │
│   agente · MCP · UI de chat · comandos · providers      │
├─────────────────────────────────────────────────────────┤
│                    API del core (v1)                    │
├─────────────────────────────────────────────────────────┤
│                   Kernel (Go, binario único)            │
│  scheduler · IO · red · UI terminal · texto · codecs    │
└─────────────────────────────────────────────────────────┘
```

El kernel es un **runtime**: una stdlib de primitivas más un event loop. No
contiene lógica de agente, ni de MCP, ni de chat. Cuanto más pequeño es el core
conceptualmente, más completa tiene que ser su superficie de primitivas: Lua
puro no puede hacer TLS ni pintar un terminal, así que el kernel se lo da.

## El kernel: inventario de primitivas

| Módulo | Responsabilidad |
|---|---|
| **scheduler** | Event loop, timers, puente coroutines-Lua ↔ goroutines, workers |
| **io** | Filesystem, spawn de procesos con streams, entorno |
| **net** | Cliente HTTP/HTTPS con respuesta en streaming (SSE), TCP/websocket |
| **ui** | Primitivas de terminal, eventos de input, keymaps |
| **text** | UTF-8/graphemes, regex, render de markdown, syntax highlighting |
| **data** | Codecs JSON y TOML |
| **loader** | `require`, rutas de plugins, extensiones embebidas |

Notas:

- **text** incluye markdown y highlighting como builtins aunque viole la pureza
  del kernel mínimo: en Lua interpretado serían dolorosamente lentos. Es la
  misma concesión que hace Neovim embebiendo tree-sitter (ADR-004, regla
  "Lua decide, Go ejecuta").
- La API de **ui** (¿buffers/ventanas estilo Neovim, árbol de widgets, o
  superficie de celdas?) es la primitiva más difícil de diseñar y está
  deliberadamente sin decidir (ADR-007).

## Modelo de concurrencia: el modelo del navegador

Tres patas (ADR-004):

1. **Estado Lua principal, single-threaded.** UI, keymaps, hooks y
   orquestación. El monohilo aquí es una *feature*: orden determinista de
   eventos y cero data races para el 95% de los plugins. El IO nunca bloquea:
   las goroutines de Go hacen el trabajo y publican resultados en la cola que
   el loop de Lua consume; de cara al autor de extensiones todo es async vía
   coroutines (estilo `await`).
2. **Workers explícitos.** Una primitiva tipo `worker.spawn()` levanta otro
   estado Lua en otra goroutine, sin memoria compartida, comunicado por paso
   de mensajes. Paralelismo real, opt-in, para la extensión que necesite
   masticar datos. Los workers **no tienen acceso al módulo `ui`**: la
   pantalla solo se pinta desde el estado principal (como los Web Workers
   respecto al DOM). Los mensajes son copias — un worker devuelve resultados
   digeridos, no datos crudos masivos.
3. **Primitivas Go paralelas por dentro.** `core.search()` y compañía saturan
   todos los cores sin que Lua se entere. El rendimiento bruto nunca depende
   de la velocidad del intérprete.

Restricción técnica que motiva el diseño: gopher-lua **no es thread-safe**; un
estado Lua solo puede tocarse desde una goroutine. El patrón es el de
Node/libuv/`vim.uv`, ya validado.

El aislamiento es **por tarea, no por plugin** (ADR-008): todos los plugins
conviven en el estado principal — lo que permite que se `require` entre sí y
compongan, como en Neovim — y la robustez se obtiene con dos guardas del core:

- **Watchdog**: cada handler tiene un presupuesto de tiempo en el estado
  principal; si lo excede, se aborta vía cancelación por contexto y el plugin
  se marca como sospechoso.
- **`pcall` en cada frontera de hook**: un error en un plugin nunca tumba el
  event loop ni a los demás plugins.

## Capas de extensión

- **Capa 1 — Lua embebido.** El mecanismo universal: hooks del ciclo de vida,
  comandos, UI, keybindings, y también el propio agente y los adaptadores de
  protocolo de los LLMs. Distribución v1: `~/.config/nu/plugins/` + git clone;
  sin package manager propio de momento.
- **Capa 2 — Procesos externos.** Herramientas pesadas o en otros lenguajes
  vía subproceso (JSON-RPC/stdio). MCP vive aquí, **implementado como
  extensión oficial Lua** sobre las primitivas `io.spawn` + codecs: el core no
  sabe qué es MCP.

## Providers de LLM

División datos/código (ADR-005):

- **TOML** declara el registro: endpoints, API keys, modelos, límites de
  contexto. Configuración, no programación.
- **Adaptadores de protocolo en Lua** (extensiones oficiales) implementan cada
  dialecto (Anthropic, OpenAI, Gemini, Ollama...): formato SSE, tool calls,
  system prompts, thinking blocks. Parsear SSE en Lua es viable: es texto a
  velocidad de lectura humana.

Añadir un provider raro (vLLM, proxy corporativo) es un fichero Lua, no una
recompilación.

## Distribución

- Binario estático Go, `CGO_ENABLED=0`, cross-compile a todas las plataformas.
- Extensiones oficiales embebidas con `go:embed`; sobreescribibles por el
  usuario desde su directorio de config.

## Cuestiones abiertas

1. **API de UI** (ADR-007): buffers vs widgets vs celdas. Condiciona qué
   extensiones serán fáciles o imposibles de escribir. Dato a favor de
   diseños simples tras ADR-008: solo el estado principal pinta, así que el
   modelo de UI no necesita ser thread-safe ni multiplexar autores
   concurrentes.
2. **Política fina del watchdog**: valor del presupuesto por handler, si es
   configurable por plugin, y el flujo de deshabilitación/aviso al usuario.
