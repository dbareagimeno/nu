# Problemas abiertos

Lista de trabajo viva: grietas encontradas en la ronda 3 de validación
([pseudocodigo.md](pseudocodigo.md)) que están **pendientes de resolver**.
Método: se resuelven una a una, discutiendo opciones; al decidirse, la
resolución se aplica a los documentos afectados y la entrada pasa a
"Resuelto" con enlace al cambio. Distinto de [pospuesto.md](pospuesto.md):
aquello es lo que decidimos no decidir; esto son agujeros que la v1 sí
necesita cerrados.

Orden sugerido: primero los que condicionan contratos congelables (G1, G3,
G4, G5), luego los de alcance (G6, G9), luego DX (G2, G7, G8).

---

## G1 · Comportamiento ante resize — `api.md` §9 — **Pendiente**

**Problema.** Una región que queda fuera (o parcialmente fuera) de la
pantalla tras un resize tiene comportamiento indefinido, y no hay
convención sobre quién recoloca qué: el picker del escenario 12 queda roto
o flotando.

**Impacto.** Todo plugin con UI propia; el toolkit lo necesita resuelto
antes del spike.

**Opciones.** (a) Solo reglas duras: las regiones se recortan a pantalla
sin error, y la convención es "tu región, tu `ui:resize`"; (b) además,
anclajes declarativos en `region{}` (`x = "center"`, `w = "80%"`) que el
compositor reaplica solo en cada resize; (c) delegarlo todo al toolkit y
que el raw `nu.ui` sea explícitamente "a tu suerte".

## G2 · Hot-reload de plugins (ciclo de desarrollo) — loader / `api.md` §14 — **Pendiente**

**Problema.** Iterar sobre un plugin exige reiniciar nu: `require` cachea,
re-ejecutar `init.lua` duplicaría registros, y aunque todos los registros
devuelven handles, nadie los rastrea por plugin (no existe "deshaz todo lo
de X"). Lo mismo aplica a recargar `providers.toml` / `nu.toml` en
caliente.

**Impacto.** DX de la comunidad de plugins — el público objetivo del
proyecto. No bloquea contratos.

**Opciones.** (a) El core rastrea ownership de handles por plugin (ya sabe
`plugin.current()` en cada registro) y ofrece `nu.plugin.reload(name)`;
(b) sin reload: comando de reinicio rápido de nu que repone la sesión
(`--continue` ya casi lo da); (c) posponer con disparador (P-nuevo).

## G3 · Multi-sesión: atribución de eventos y modales concurrentes — `agente.md` §4 / `chat.md` — **Pendiente**

**Problema.** Los payloads `agent:*` no obligan a llevar `session_id`
(dos sesiones concurrentes mezclarían deltas), `chat.md` no especifica
filtrado, y dos `permission.asked` simultáneos abrirían dos modales sobre
la misma pila de input sin orden definido.

**Impacto.** Los subagentes ya hacen esto real en v1 — no es un caso
futuro. Contrato congelable afectado.

**Opciones.** (a) `session_id` obligatorio en todo payload `agent:*` +
`chat` filtra por sesión activa + cola FIFO de modales (uno visible a la
vez); (b) además, namespacing de eventos por sesión
(`agent:<id>:delta`) para suscripciones selectivas baratas.

## G4 · Reentrada de `Session:send` — `agente.md` §2 — **Pendiente**

**Problema.** Llamar `send` con un turno en vuelo no está definido:
¿error, cola, o cancelar-y-reemplazar? Cada UI improvisaría una semántica
distinta.

**Impacto.** Contrato congelable; afecta a la UX básica (enter impaciente).

**Opciones.** (a) `EBUSY` y que la UI decida (mínimo, predecible); (b) el
motor encola mensajes y los anexa al siguiente turno (lo que hacen los
harnesses maduros); (c) configurable por sesión.

## G5 · Doble reanudación de la misma sesión — `sesiones.md` — **Pendiente**

**Problema.** Dos procesos nu pueden abrir el mismo JSONL y hacer appends
intercalados: corrupción silenciosa. No hay lock.

**Impacto.** Pérdida de datos del usuario; barato de cerrar ahora, caro
después.

**Opciones.** (a) Lockfile junto al JSONL (`.lock` con pid; el segundo
proceso recibe error claro y ofrece fork); (b) lock advisory del SO
(flock) — ¿portabilidad Windows?; (c) detectar-y-fork automático: el
segundo `--continue` crea fork silenciosamente.

## G6 · Granularidad de `caps` — `api.md` §13 — **Pendiente**

**Problema.** `caps` concede módulos enteros: `"fs"` incluye `write` y
`remove`. El subagente auditor de solo lectura — el caso estrella del
sandboxing — no se puede expresar.

**Impacto.** Una de las features diferenciales (permisos duros) se queda
corta en su mejor caso de uso.

**Opciones.** (a) Caps con sufijo de modo: `"fs:ro"` (lista corta y
curada de variantes por módulo, sin inventar un lenguaje de policies);
(b) caps por función (`"fs.read"`, `"fs.stat"`): expresivo pero
N×funciones de superficie a congelar; (c) scoping por ruta además del
modo (`fs:ro:/repo`): el más potente y el más caro de especificar bien;
(d) dejar módulo-entero en v1 y anotar en pospuestos.

## G7 · Semántica de `fs.watch` — `api.md` §5 — **Pendiente**

**Problema.** Sin definir: ¿recursivo?, ¿respeta `.gitignore`?
(vigilar `node_modules/` = ruido infinito), ¿coalescing de ráfagas?
(un `git checkout` toca miles de ficheros → miles de callbacks).

**Impacto.** Cualquier plugin de auto-contexto o recarga; riesgo de
rendimiento en el estado principal.

**Opciones.** (a) `watch(path, opts, fn)` con `opts = { recursive,
gitignore = true, debounce_ms = 50 }` y entrega de eventos en lotes
(`fn(events[])`); (b) mínimo v1: un path, sin recursión (los plugins
componen), y a pospuestos lo demás.

## G8 · `on_message` vs `recv` simultáneos — `api.md` §13 — **Pendiente**

**Problema.** Son "alternativas" pero nada impide usar ambas sobre el
mismo worker: ¿quién recibe el mensaje? Indefinido.

**Impacto.** Menor, pero es exactamente el tipo de indefinición que
genera bugs irreproducibles.

**Opciones.** (a) Mutuamente excluyentes: registrar `on_message` con un
`recv` pendiente (o viceversa) lanza `EINVAL`; (b) `on_message` gana
siempre y `recv` tras él lanza; (c) cola única y cualquier consumidor
compite (no determinista — probablemente descartable).

## G9 · Alcance Windows en v1 — transversal — **Pendiente**

**Problema.** La tool `bash` asume `sh`, `Proc:kill` habla señales POSIX,
y el input de terminal difiere (IME, teclas). Go cross-compila a Windows,
pero "compila" no es "funciona bien". Sin decisión de alcance, cada
contrato asume POSIX en silencio.

**Impacto.** Decisión de producto más que técnica; condiciona promesas de
la distribución ("un binario para todas las plataformas").

**Opciones.** (a) v1 = Linux/macOS de primera + Windows best-effort
documentado (la tool bash exige WSL o git-bash); (b) Windows de primera
desde v1 (coste alto: shell portable, semántica kill, pruebas de
terminal); (c) v1 sin Windows, explícitamente.
