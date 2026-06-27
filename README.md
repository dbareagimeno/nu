# nu

[![CI](https://github.com/dbareagimeno/nu/actions/workflows/ci.yml/badge.svg)](https://github.com/dbareagimeno/nu/actions/workflows/ci.yml)
[![Licencia: Apache 2.0](https://img.shields.io/badge/licencia-Apache%202.0-blue.svg)](LICENSE)

> Un runtime de Lua orientado a terminal cuya killer app es un coding harness.
> Un binario Go, kernel mínimo, y todo lo demás — incluido el propio agente —
> extensiones Lua.

`nu` es un único binario estático: un **kernel diminuto** de primitivas (sistema
de ficheros, procesos, HTTP, búsqueda, UI de terminal, concurrencia) sobre un
intérprete de Lua. El core **no sabe lo que es un agente**: el harness de código
—el agente, el chat, los providers de LLM, MCP— son **extensiones Lua**, las
mismas que podría escribir cualquiera. Esa es la idea entera (modelo
Emacs/Textadept, no Neovim): un runtime que resulta que trae un agente de serie,
no un agente con plugins.

**Estado: kernel construido.** Las 45 sesiones del [plan de
implementación](docs/implementacion.md) están cerradas: un binario Go estático
(sin CGO) con las extensiones oficiales embebidas. El método sigue siendo el que
lo hizo posible —el diseño se decide en [`docs/`](docs/) y la API se valida
escribiendo pseudocódigo contra ella antes de congelarla—, así que esos
documentos *son* la espec y el código la implementa, nunca al revés.

---

## Índice

- [Instalación](#instalación)
- [Inicio rápido](#inicio-rápido)
- [Uso](#uso)
  - [Modos del binario](#modos-del-binario)
  - [El chat interactivo](#el-chat-interactivo)
  - [El agente headless (scripts y CI)](#el-agente-headless-scripts-y-ci)
  - [El REPL de Lua](#el-repl-de-lua)
- [Configuración](#configuración)
  - [Dónde vive la configuración](#dónde-vive-la-configuración)
  - [Activar las extensiones (`nu.toml`)](#activar-las-extensiones-nutoml)
  - [Modelos y claves (`providers.toml`)](#modelos-y-claves-providerstoml)
  - [El agente (`agent.toml`)](#el-agente-agenttoml)
  - [Permisos](#permisos)
  - [Contexto de proyecto y skills](#contexto-de-proyecto-y-skills)
  - [Personalización con `init.lua`](#personalización-con-initlua)
- [Las extensiones oficiales](#las-extensiones-oficiales)
- [Escribir un plugin](#escribir-un-plugin)
- [Documentación](#documentación)
- [Contribuir](#contribuir)
- [Licencia](#licencia)

---

## Instalación

**La vía rápida.** El script detecta tu sistema (linux/darwin × amd64/arm64),
descarga el binario de la última release estable, **verifica el checksum** y lo
instala en tu `PATH`:

```sh
curl -fsSL https://raw.githubusercontent.com/dbareagimeno/nu/main/install.sh | sh
```

Por defecto instala en `~/.local/bin`; fija el destino con `NU_INSTALL_DIR` o la
versión con `NU_VERSION`. Si prefieres revisarlo antes, descarga
[`install.sh`](install.sh), léelo y córrelo a mano — es un script POSIX corto.

**Instalación manual.** Descarga el `.tar.gz` de tu sistema de la [última
release](https://github.com/dbareagimeno/nu/releases/latest), verifícalo y ponlo
en el `PATH`:

```sh
# Ajusta VERSIÓN y la plataforma (linux/darwin × amd64/arm64).
tar -xzf nu-vVERSIÓN-linux-amd64.tar.gz
sha256sum -c checksums.txt          # verifica la integridad
chmod +x nu
sudo mv nu /usr/local/bin/
nu -e 'return nu.version.api'       # comprueba la instalación (headless, sin TTY)
```

**Compilar desde el código** (necesitas Go ≥ la versión de [`go.mod`](go.mod)):

```sh
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o nu .
```

El binario no tiene dependencias dinámicas (`CGO_ENABLED=0`): corre tal cual en
cualquier distro o contenedor. En **Windows** se usa vía **WSL2** con el binario
de `linux/amd64` (el Windows nativo está pospuesto,
[`docs/pospuesto.md`](docs/pospuesto.md) P18).

---

## Inicio rápido

Recién instalado, `nu` es un **runtime desnudo**: las extensiones oficiales
vienen embebidas pero **inactivas por defecto** ([ADR-010](docs/adr.md)) — el
harness es una elección tuya, no un hecho consumado. De cero a tu primer turno
de agente en tres pasos:

```sh
# 1. Activa el conjunto oficial de producto (agent, chat, providers, sessions,
#    mcp, toolkit, repl). Escribe `plugins.enabled` en ~/.config/nu/nu.toml.
nu --default-config

# 2. Declara un modelo y exporta su clave (ver «Modelos y claves» más abajo).
cat > ~/.config/nu/providers.toml <<'TOML'
[providers.anthropic]
adapter     = "anthropic"
base_url    = "https://api.anthropic.com"
api_key_env = "ANTHROPIC_API_KEY"

[[providers.anthropic.models]]
id      = "claude-opus-4-8"
context = 200000
aliases = ["opus"]
TOML
export ANTHROPIC_API_KEY="sk-..."

# 3. Lanza un turno headless...
nu -p 'Resume qué hace este repositorio' --model anthropic/opus

#    ...o abre el chat interactivo (en un terminal con TTY):
nu
```

Sin el paso 1, `nu` arranca en la **pantalla de runtime desnudo** (con TTY) o
falla con un error accionable que nombra la línea exacta de `nu.toml` que falta
(sin TTY). Nada ocurre por arte de magia: cada paso es explícito y reversible.

---

## Uso

### Modos del binario

El binario `nu` decide su modo según los flags y según haya o no un terminal
interactivo (TTY). La superficie completa:

| Invocación | Qué hace |
|---|---|
| `nu` | **Arranque interactivo** (con TTY). Si hay extensiones activas, corre su UI (el chat); si no hay ninguna, pinta la pantalla de runtime desnudo. Sin TTY, imprime el uso. |
| `nu --default-config` | Activa el **conjunto oficial de producto**: escribe `plugins.enabled` en `~/.config/nu/nu.toml` y sale (atómico e idempotente; preserva el resto del fichero). El onramp sin TTY. |
| `nu -e '<lua>'` | Evalúa un chunk **Lua headless** (sin TTY) e imprime sus valores de retorno. Útil para inspeccionar el runtime o automatizar. |
| `nu -p '<prompt>'` | Ejecuta un **turno de agente headless** e imprime el texto final del asistente a stdout. Para scripts y CI. |
| `nu -p '<...>' --continue` | Igual, pero **reanuda la última sesión** del proyecto (el `cwd`) antes de enviar el prompt. Alias: `-c`. |
| `nu -p '<...>' --model 'prov/modelo'` | Selecciona el modelo/provider del turno (anula el de `agent.toml`). |
| `nu -p '<...>' --auto-permissions` | Permisos del agente en modo «auto»: concede las tools sensibles sin preguntar. Solo para sandboxes y contenedores desechables — el riesgo se elige, no se hereda. |

`nu --default-config` combinado con `-p`/`-e` es **efímero**: activa el conjunto
solo para ese proceso, sin tocar disco (ideal para Docker/CI inmutable).

**Códigos de salida** (coherentes para scripts y CI):

| Código | Significado |
|---|---|
| `0` | Éxito. |
| `1` | Error de ejecución o de arranque (el chunk, el turno o el provider lanzaron; grafo de plugins inválido; `nu.toml` roto). |
| `2` | Uso inválido (flags incompatibles o argumento ausente). |
| `3` | Permiso denegado en headless: una tool sensible se denegó por falta de `--auto-permissions` (código distinto para que CI distinga «el modelo no pudo actuar por permisos» de un fallo de ejecución). |

### El chat interactivo

`nu` en un terminal interactivo, con el conjunto oficial activo, arranca el
**chat** ([docs/chat.md](docs/chat.md)): una columna con el transcript
(mensajes, bloques de tools, razonamiento), un editor de input multilínea y una
statusline (modelo · % de contexto · coste · cwd · modo de permisos).

- **Enviar:** `enter`. **Línea nueva:** `shift+enter`. **Cancelar el turno:** `esc`.
- **Comandos slash** (`/` al inicio del input): `/model` (muestra o cambia el
  modelo), `/sessions` (lista las sesiones guardadas), `/compact`, `/clear`,
  `/help`, `/quit`. Son un punto de extensión de primera clase: un plugin
  registra los suyos con `chat.command{}`.
- **Diálogo de permisos:** cuando una tool sensible necesita autorización, el
  chat abre un modal con el comando o la ruta completos y las opciones permitir
  una vez / permitir siempre (sesión o global) / denegar.

> **Nota de estado.** La extensión `chat` (`0.1.0`) ya cubre las menciones `@`
> con picker difuso de ficheros, los comandos `/fork` y `/permissions`, persistir
> «permitir siempre» en `agent.toml` y el autocompletado visual de comandos
> (ver [docs/pospuesto.md](docs/pospuesto.md) P26–P29).

### El agente headless (scripts y CI)

El motor del agente es **headless por diseño** ([docs/agente.md](docs/agente.md)
§1): no pinta nada, ejecuta el loop. Eso da modo scripting y CI gratis.

```sh
# Un turno suelto; el texto final va a stdout, apto para pipes.
nu -p 'Genera un mensaje de commit para los cambios staged' --auto-permissions

# Encadena turnos sobre la misma sesión del proyecto.
nu -p 'Añade tests al módulo de parsing'
nu -c -p 'Ahora ejecútalos y corrige lo que falle'
```

En headless **no hay UI que pregunte**, así que las tools sensibles (escribir,
ejecutar, red) se **deniegan por defecto** con un error accionable que nombra el
patrón `allow` a añadir. Concede explícitamente con `--auto-permissions` o
declarando `allow` en `agent.toml` (ver [Permisos](#permisos)). Las tools de
solo lectura (leer, grep, glob) nunca piden permiso.

### El REPL de Lua

`nu` no es solo el agente. La extensión `repl` es un **intérprete de Lua
interactivo** sobre la API pública `nu.*`, activable **sin arrastrar el harness**:

```sh
# En ~/.config/nu/nu.toml, activa solo el REPL...
#   [plugins]
#   enabled = ["repl"]
# ...y arráncalo en un terminal interactivo:
nu
```

Es la prueba de que el runtime sirve para más que el agente, y el punto de
partida natural para quien escribe extensiones.

---

## Configuración

### Dónde vive la configuración

`nu` sigue la convención XDG. Dos directorios:

| Directorio | Por defecto | Contiene |
|---|---|---|
| `config.dir()` | `~/.config/nu` | `nu.toml`, `providers.toml`, `agent.toml`, `skills/`, tu `init.lua` y los plugins de disco. |
| `data_dir()` | `~/.local/share/nu` | Sesiones (`sessions/`), almacenamiento de plugins, logs, extensiones embebidas extraídas. |

Ambos respetan `XDG_CONFIG_HOME` / `XDG_DATA_HOME`. Apuntar `XDG_CONFIG_HOME` a
otro sitio te da perfiles de configuración aislados (así corren los
[ejemplos](examples/)).

La configuración por proyecto vive en `<repo>/.nu/` (p. ej. `<repo>/.nu/agent.toml`,
`<repo>/.nu/skills/`). La precedencia es la estándar:
**defaults < global < proyecto < sesión**, con una salvedad de seguridad: la
config del proyecto la escribió un tercero, así que **solo puede recortar
permisos, nunca ampliarlos** ([docs/agente.md](docs/agente.md) §11).

### Activar las extensiones (`nu.toml`)

`config.dir()/nu.toml` gobierna al propio runtime: qué plugins se cargan, rutas
extra de plugins y el presupuesto del watchdog. La forma mínima la escribe
`nu --default-config` por ti:

```toml
[plugins]
# Las extensiones embebidas se cargan solo si se nombran aquí (ADR-010).
enabled = ["providers", "sessions", "agent", "mcp", "toolkit", "chat", "repl"]

# Opcional: directorios donde buscar plugins de disco propios.
dirs = ["~/.config/nu/plugins"]
```

Un grafo de plugins inválido (colisión de nombres, ciclo, dependencia ausente) o
un `nu.toml` mal formado es un **error de arranque accionable** que apunta a la
línea que lo arregla.

### Modelos y claves (`providers.toml`)

Los modelos disponibles se declaran en `config.dir()/providers.toml`. **TOML
declara los datos, Lua implementa el protocolo** ([ADR-005](docs/adr.md)): añadir
un modelo con un adaptador oficial es cero código. La clave de API **nunca** va
en el fichero — se lee de la variable de entorno que nombres en `api_key_env`.

```toml
# Provider con adaptador oficial (anthropic): solo datos.
[providers.anthropic]
adapter     = "anthropic"
base_url    = "https://api.anthropic.com"
api_key_env = "ANTHROPIC_API_KEY"

[[providers.anthropic.models]]
id         = "claude-opus-4-8"
context    = 200000
max_output = 32000
cost       = { input = 5.0, output = 25.0 }   # USD/Mtok, informativo
aliases    = ["opus"]
thinking   = "adaptive"                        # dialecto de razonamiento (ADR-016):
                                               # "adaptive" (Opus 4.6+), "budget" o "none"

# Un endpoint compatible-OpenAI, p. ej. Ollama local (sin clave). El adaptador
# `openai-compat` va embebido (junto a `anthropic` y `gemini`).
[providers.local]
adapter  = "openai-compat"
base_url = "http://localhost:11434/v1"

[[providers.local.models]]
id      = "qwen3:32b"
context = 32768
```

Un modelo se referencia como `"proveedor/id-o-alias"`: `anthropic/opus`,
`local/qwen3:32b`. Los tres adaptadores oficiales van embebidos: `anthropic`,
`openai-compat` (todo el ecosistema Chat Completions: OpenAI, Together, Groq,
OpenRouter, vLLM, Ollama `/v1`) y `gemini` ([docs/providers.md](docs/providers.md)
§3); cualquier otro protocolo se cubre con un adaptador en un plugin de terceros.
Un `providers.toml` **ausente** es válido: da un registro vacío.

### El agente (`agent.toml`)

`config.dir()/agent.toml` ajusta el comportamiento del agente. Todo es opcional;
sin él, valen los defaults.

```toml
model     = "anthropic/opus"   # modelo por defecto del turno
max_turns = 50                 # tope de iteraciones (protección contra loops)

# Compactación automática del contexto.
[compaction]
threshold = 0.8                # comprime al superar el 80% del contexto del modelo
# model   = "anthropic/haiku"  # modelo del resumen (por defecto, el de la sesión)

# Permisos globales (ver más abajo).
[permissions]
mode  = "ask"
allow = ["edit", "bash:git *"]
deny  = ["bash:rm -rf *"]
```

### Permisos

Cada tool que muta algo (escribir, ejecutar, red) pasa por un pipeline de
permisos ([docs/agente.md](docs/agente.md) §5):

```toml
[permissions]
mode  = "ask"                  # "ask" (por defecto) o "auto"
allow = ["edit", "bash:npm *", "bash:git *"]   # patrones tool[:argumento]
deny  = ["bash:rm *"]          # deny gana siempre
```

- **`deny`** corta primero, **`allow`** concede después; lo que nadie decide y
  `mode = "ask"` dispara el diálogo (en el chat) o se **deniega** (en headless).
- Las tools de **solo lectura** nunca piden permiso, ni en headless.
- El **modo auto** (`mode = "auto"` o el flag `--auto-permissions`) concede todo
  sin preguntar: para sandboxes y contenedores desechables, donde el riesgo se
  elige conscientemente.
- Por seguridad, los `allow` y el `mode` del `agent.toml` **de un repo** se
  ignoran (solo se honran sus `deny`): clonar y abrir un repo nunca ejecuta su
  voluntad ([docs/agente.md](docs/agente.md) §11).

### Contexto de proyecto y skills

- **`nu.md`** en la raíz del repo: si existe, su contenido se inyecta en el
  system prompt como contexto del proyecto (el equivalente a un `CLAUDE.md`).
- **Skills:** directorios con un `SKILL.md` (frontmatter `name` + `description`),
  buscados en `config.dir()/skills/` (tuyos) y `<repo>/.nu/skills/` (del
  proyecto). Se inyecta solo el índice en el system prompt; el contenido completo
  se carga bajo demanda. Compatibles con el formato del ecosistema existente.
- **TOFU:** la primera vez que abres un repo con `.nu/skills/` o `nu.md`, `nu`
  pregunta una sola vez si confías en ese contenido (se recuerda por repo). Sin
  un sí, no se inyecta — el mismo patrón `:trust` de Neovim.

### Personalización con `init.lua`

`config.dir()/init.lua` se ejecuta **el último** en el arranque, así que tienes
la última palabra: remapear atajos del chat (`chat.keys`), añadir comandos slash
(`chat.command{}`), registrar tools (`agent.tool{}`), instalar hooks
(`agent.hook(...)`), cambiar el theme. No hay nada mágico inaccesible: el chat y
el agente se configuran con la misma API pública que usaría cualquier plugin.

---

## Las extensiones oficiales

Todo el harness son extensiones Lua embebidas en el binario, **sin privilegio de
kernel** — una alternativa de terceros puede sustituir cualquiera. El conjunto de
producto (lo que activa `--default-config`):

| Extensión | Rol | Contrato |
|---|---|---|
| `providers` | Registro de modelos (TOML) y adaptadores de LLM (`anthropic`, `openai-compat` y `gemini`, embebidos). | [providers.md](docs/providers.md) |
| `sessions` | Persistencia de conversaciones: JSONL append-only en `data_dir()/sessions/`. | [sesiones.md](docs/sesiones.md) |
| `agent` | El motor headless: turno, tools, permisos, hooks, subagentes, compactación. | [agente.md](docs/agente.md) |
| `chat` | La UI de terminal: transcript, input, comandos slash, statusline. Solo en TTY. | [chat.md](docs/chat.md) |
| `mcp` | Puente con servidores MCP: registra cada tool remota como una tool del agente. | — |
| `toolkit` | Toolkit de widgets sobre `nu.ui` (lo que usa el chat para pintar). | — |
| `repl` | Un REPL de Lua sobre la API pública; activable solo, sin el harness. | — |

Las dependencias se resuelven solas (orden topológico): activar `chat` arrastra
`agent`, `providers`, `sessions` y `toolkit`. La extensión `example` viene
embebida solo como andamiaje de pruebas y **no** entra en el conjunto de producto.

---

## Escribir un plugin

Un plugin es un directorio con un `plugin.toml` (`name`, `version`, `requires?`)
y un `init.lua`. Se descubre poniéndolo bajo un directorio de `plugins.dirs` y
activándolo en `plugins.enabled`. La superficie que tiene a su disposición:

- La **API del core** `nu.*` ([docs/api.md](docs/api.md)): fs, proc, http, ws,
  search, text, re, ui, events, task, workers, codecs. Pequeña y estable.
- Los contratos de las **extensiones oficiales** para colgarse de ellas:
  `agent.tool{}`, `agent.hook(...)`, `chat.command{}`, `chat.renderer(...)`,
  `providers.register_adapter(...)`.

El modelo de ejecución es «del navegador»: estado principal single-threaded con
event loop (async por corrutinas, await implícito) + workers opt-in sin memoria
compartida + primitivas Go paralelas por dentro. La regla de oro es **Lua decide,
Go ejecuta**: el trabajo pesado es una primitiva Go, nunca un bucle caliente en
Lua.

Empieza por la [**guía de plugins**](docs/guia-plugins.md) (sabiduría práctica +
checklist) y mira [`examples/`](examples/), que trae una TUI funcional escrita en
Lua puro sobre la API del core:

```sh
# Una TUI de demostración (regiones, teclado, reloj en vivo, resize).
XDG_CONFIG_HOME=examples nu
```

---

## Documentación

Por defecto el proyecto se diseña en `docs/`: esos documentos son la
especificación, y el código los implementa. Orden de lectura sugerido:

1. [Filosofía](docs/filosofia.md) — principios y lo que nu **no** es
2. [Arquitectura](docs/arquitectura.md) — la forma del sistema (vista estática)
3. [Modelo de ejecución](docs/modelo-ejecucion.md) — concurrencia y limitaciones (vista dinámica)
4. [API del core](docs/api.md) — la superficie sagrada v1
5. [ADR](docs/adr.md) — registro de decisiones y su razonamiento

**Contratos de las extensiones oficiales:**
[Providers](docs/providers.md) ·
[Agente](docs/agente.md) ·
[Sesiones](docs/sesiones.md) ·
[Chat](docs/chat.md)

**Para autores de plugins:** [Guía de plugins](docs/guia-plugins.md)

**Proceso y registro de trabajo:**
[Pseudocódigo de validación](docs/pseudocodigo.md) ·
[Problemas abiertos](docs/problemas.md) ·
[Pospuesto](docs/pospuesto.md) ·
[Plan de implementación](docs/implementacion.md)

---

## Contribuir

Las aportaciones son bienvenidas; lee [CONTRIBUTING.md](CONTRIBUTING.md) antes de
enviar un Pull Request. El proyecto tiene un método de trabajo explícito (diseño
por documentos, validación por pseudocódigo, ADRs que no se reescriben): el mejor
primer paso es leer [docs/filosofia.md](docs/filosofia.md) y
[docs/adr.md](docs/adr.md) para entender el *porqué* antes de proponer el *qué*.

El autor conserva la titularidad del proyecto, por lo que al incorporar código de
terceros puede pedir una cesión de derechos o un acuerdo de contribución (CLA).

## Licencia

`nu` es software libre bajo la [Apache License 2.0](LICENSE) (permisiva, con
concesión de patentes). Eres libre de usarlo, estudiarlo, modificarlo y
distribuirlo, incluso comercialmente. Copyright de Diego Barea; ver
[NOTICE](NOTICE).
</content>
</invoke>
