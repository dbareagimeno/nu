# Decisiones y desviaciones de implementación

Este fichero recoge decisiones de implementación que no estaban especificadas al
detalle en los documentos de diseño y desviaciones puntuales del plan, una
entrada por sesión. No sustituye al flujo de diseño (`problemas.md`/`adr.md`):
recoge lo operativo que no llega a hallazgo `G##` pero que la sesión siguiente
debe poder reconstruir.

## S05 — `nu.task.sleep`/`defer`/`every` + `Timer:stop` (api.md §3)

### Semántica de quiescencia con timers activos (decisión clave)

`api.md` §3 no dice cómo interactúan los timers con el fin de `nu -e`. El
modelo de S04 hace que `EvalString` corra el chunk, suelte el token y llame a
`waitIdle()`, que bloquea hasta que el conjunto queda **quiescente**. Había que
decidir qué cuenta como "trabajo pendiente":

- **`defer(fn)` SÍ cuenta.** Es "el siguiente tick": su handler debe correr
  antes de que `EvalString` devuelva. Se contabiliza con un contador `pending`
  (incrementado al encolar, decrementado al ejecutar el disparo); `waitIdle`
  espera a `live == 0 && pending == 0`. Sin esto, un `defer` encolado por el
  chunk podría no llegar a correr nunca.

- **`every(ms, fn)` NO cuenta.** Un timer periódico no termina jamás; si contara
  para la quiescencia, `nu -e` no volvería nunca. Decisión: un `every` activo es
  **facilidad de fondo**, no trabajo de primer plano. El fin de `nu -e` lo
  determinan el chunk + sus tasks + sus `defer` encolados; cuando todo eso queda
  quiescente, `EvalString` vuelve aunque haya timers activos, y `Runtime.Close`
  los apaga (corta sus goroutines de ticker, sin fugas).

  Justificación: en un `nu` interactivo (S33+) el loop sigue vivo por la UI/los
  eventos de entrada, no por los timers; bajo `nu -e` (headless, sin UI) el fin
  natural es la quiescencia del primer plano. Un timer que debiera mantener vivo
  el proceso indica que el trabajo real está en una task (que sí cuenta), no en
  el timer. Esto es coherente con el criterio de hecho de S05 en el plan
  ("`every` dispara N veces y `stop` lo corta"): los tests anclan el runtime con
  una task mientras el timer tickea.

### Handlers síncronos sobre thread efímero (no sobre `host`)

`defer` y cada disparo de `every` ejecutan un handler **síncrono** (no ⏸, §3):
corren bajo el token, como el chunk y los handlers de eventos. Se ejecutan sobre
un **thread Lua dedicado por disparo** (`host.NewThread()`), no sobre la pila del
estado principal. Motivo: mientras `EvalString` está en `waitIdle`, la pila de
`host` aún custodia los valores de retorno del chunk; un `CallByParam` sobre
`host` podría interferir. Es la misma estrategia que las tasks (cada una sobre su
`co`). Coste: un thread por disparo, recogido por el GC de gopher-lua (no hay
`Close` por thread en la API, igual que para los `co` de las tasks).

### `stop` sin disparo tardío (carrera tick/token)

Un disparo de `every` puede quedar esperando el token justo cuando llega `stop`.
Para garantizar "tras `stop`, ni un tick más", el disparo usa
`runSyncHandlerCancelable`: mientras espera el token atiende también a `stopCh` y,
si se cerró, no ejecuta. `stopTimer`/`stopAllTimers` cierran `stopCh` de forma
idempotente (solo si el timer sigue rastreado), así que `Timer:stop()` doble no
entra en pánico.

### Convención de tests con `-race`

`go test -race` exige cgo; el resto del proyecto compila con `CGO_ENABLED=0`
(ADR-001). Por tanto: `CGO_ENABLED=0 go build ./...` para el binario, y
`CGO_ENABLED=1 go test -race -count=4 ./...` para la suite con detector de
carreras (igual criterio que dejó S04 en la bitácora). Los tests de timing usan
periodos cortos (1-5 ms) y esperas holgadas; `-count=4`/`-count=8` no produjeron
flaky.

### Sin hallazgos

El modelo de S04 (goroutine-por-task + token) bastó para S05 sin ampliar la API.
No se abrió ningún `G##`.

## S06 — `nu.task.future` (rendez-vous de un solo uso, api.md §3)

### Desviación de procedimiento: rama desde `origin/main`

Esta sesión se implementó partiendo de `origin/main`, donde el puntero ▶ ya
marcaba `S06` (S05 quedó mergeada). El ramaje local de trabajo estaba desfasado;
se creó `claude/s06-future` desde `origin/main` para arrancar sobre el estado
real. No hay desviación de *alcance*: S06 depende solo de S04 (cerrada), así que
el grafo de dependencias se respeta.

### Quiescencia: `set`/`await` NO tocan `live`/`pending` (decisión clave)

Un awaiter bloqueado en `Future:await` es una **task que ya está contada en
`live`** (se contó al hacer `spawn`); no termina hasta que su `await` retorna,
exactamente igual que una task bloqueada en `Task:await`. Por tanto los futures
no añaden contabilidad de quiescencia propia: reusan la de S04/S05 sin tocarla.
`set` tampoco mueve el conteo: resuelve y despierta, pero no crea ni destruye
trabajo de primer plano.

Consecuencia aceptada y coherente con el modelo: un `Future:await` sin un `set`
que lo resuelva cuelga `waitIdle` para siempre —es el mismo "deadlock de primer
plano" que una task esperando a otra que nunca acaba—. Detectarlo exigiría API
nueva (detección de deadlock) que api.md §3 no contempla; no es responsabilidad
del future inventarla.

### Despertar de múltiples awaiters con un único `set`

Se reusa el patrón de `Task:await`: un canal `resolvedCh` que `set` **cierra**
(bajo el token). El cierre de canal es un broadcast natural —todos los awaiters
bloqueados en `<-resolvedCh` despiertan a la vez— y aporta el happens-before que
hace visible el `value` (escrito bajo token antes del cierre) cuando cada awaiter
recupera el token. No hace falta candado propio en `resolved`/`value`: ambos se
tocan solo bajo el token (el token *es* el candado), y el único cruce entre
goroutines es el cierre del canal. Esto es lo que blinda el test `-race`.

### `set()` sin argumento resuelve con `nil`

Coherente con que un future pueda usarse como mera señal ("ya ocurrió") y no solo
como portador de valor. No es API nueva: `Future:set(v)` con `v` opcional cae en
el `LNil` que devuelve `L.Get(2)` cuando no se pasó argumento. `set()` con nil
sigue consumiendo el único uso (un segundo `set` da `EINVAL`): resolver con nil
es resolver.

### Sin hallazgos

El modelo de S04/S05 bastó para S06 sin ampliar la API. No se abrió ningún `G##`.
