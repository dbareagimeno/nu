# Informe del spike: PUC-Lua sobre wazero como VM de nu

Fecha: 2026-07-03 · Rama: `claude/spike-lua-wasm` · Duración real: una sesión.
Contexto: gopher-lua (la VM actual) está sin mantenimiento efectivo — la
v1.1.2 pineada es la última release, `state.go` no se toca desde dic-2023 y el
bug de G41 lleva reportado desde 2023 sin respuesta ([#448]). Este spike
responde si el "motor de repuesto" — el **Lua oficial de PUC compilado a
WebAssembly, corriendo sobre wazero (Go puro, sin CGO, mantenido)** — es
viable, con qué semántica y a qué coste.

**Criterio de decisión acordado**: *"si todo funciona, o incluso mejora,
aunque haya trabajo que hacer, vale la pena"*.

## Veredicto: SÍ vale la pena — funciona todo, y lo importante mejora

- **Funciona todo lo que se probó**: arranque, stdlib, errores estructurados,
  pcall anidado, corrutinas, yield con valores en ambas direcciones, y la
  reusabilidad tras cientos de errores. Cero parches a las fuentes de Lua.
- **Mejora lo que motivó el spike**: la semántica es la de *referencia* — la
  repro de G41 devuelve `42` (no `nil`) y **`coroutine.yield` cruza `pcall`**,
  la imposibilidad de gopher-lua (G31) que obligó a inventar el scheduler sin
  yields de ADR-011. La clase entera de bugs "la copia diverge del original"
  desaparece, y el runtime que queda debajo (wazero) está vivo y con respaldo
  industrial.
- **Mejora incluso el rendimiento de VM pura**: fib 1.24× más rápido; el
  benchmark de tablas, 2.4× más rápido que gopher-lua.
- **El coste está donde se esperaba** — las fronteras (llamadas host, throws,
  yields) — y es asumible porque el diseño de nu ya minimiza cruces de
  frontera ("Lua decide, Go ejecuta": primitivas gruesas, pocas y con trabajo
  pesado dentro). Con dos deberes reales antes de migrar (§5).

## 1. Qué se construyó

```
Go (tests/benchs) ── wazero 1.12 (Go puro, CGO_ENABLED=0)
                        └── lua.wasm (477 KB): PUC-Lua 5.4.7 SIN PARCHES
                              + shim C (exports spike_*, primitiva ⏸ de juguete)
                              + trampolín de desenrollado (sin setjmp/longjmp)
```

La pieza inventada es el **trampolín de desenrollado**: este wasi-libc no trae
`setjmp` (hallazgo 1), pero Lua concentra todo su unwinding en dos macros
definibles desde fuera (`LUAI_THROW`/`LUAI_TRY`, ldo.c:48) que se expanden en
un único sitio. Se redefinen para que el cuerpo protegido corra re-entrando en
wasm *desde Go* y el throw sea un **`Snapshot`/`Restore` de wazero** (su API
experimental de checkpoints: un setjmp/longjmp del propio motor). El
`__stack_pointer` (shadow stack de C) se salva/restaura en paralelo. Resultado:
el Lua de referencia corre **tal cual**, con `-include spike_unwind.h` como
única "modificación".

## 2. Hallazgos — pregunta 1: ¿compila y arranca? SÍ

| # | Hallazgo | Detalle |
|---|---|---|
| 1.1 | wasi-libc no trae setjmp/longjmp | El riesgo nº1 previsto. Resuelto SIN emulación: el trampolín (arriba). Un `setjmp.h` vacío sombrea el de glibc que el include-path pescaba |
| 1.2 | Señales y clocks | `-D_WASI_EMULATED_SIGNAL`/`_PROCESS_CLOCKS` + sus libs; sin más fricción |
| 1.3 | Libs excluidas | `io`/`os`/`debug`/`package` fuera del build — **exactamente lo que el sandbox de nu ya elimina** (api.md §1.2): la restricción de WASI coincide con la filosofía |
| 1.4 | Tamaño | `lua.wasm` 477 KB; wazero como dependencia ≈ +0,2 MB sobre gopher-lua; delta total estimado del binario de nu ≈ **+0,7 MB** |
| 1.5 | Toolchain | clang 18 + wasi-libc de Ubuntu bastan (`build.sh`); sin emscripten, sin wasi-sdk, sin asyncify, sin excepciones WASM |

## 3. Hallazgos — pregunta 2: ¿semántica y suspensión? TODO VERDE

| Test | Resultado |
|---|---|
| Repro de G41 (`pcall` + upvalue vivo) | **`42`** — semántica estándar; el bug por el que abrimos este melón no existe |
| `coroutine.yield` a través de `pcall` de Lua | **Funciona** — la limitación G31/ADR-011 no existe en el Lua real; el puente ⏸ puede ser corrutinas nativas |
| Error tras reanudar, dentro del pcall que cruzó el yield | Capturado correctamente |
| pcall anidados, errores estructurados (tablas), 200 errores seguidos | Correctos; el estado sobrevive |
| Trampolín anidado (dos `lua_pcall` en sándwich por C) | Correcto |

Bug propio encontrado y documentado por el camino: `api.Function` de wazero
**no es reentrante** — cachear el objeto y reusarlo en llamadas anidadas
corrompe el frame exterior (síntoma: `invalid table access` con argumentos
basura). Remedio: `ExportedFunction()` fresco por invocación (coste incluido en
los benchmarks).

## 4. Hallazgos — pregunta 3: el peaje (benchmarks, `-benchtime 2s`)

| Benchmark | PUC-Lua/wazero | gopher-lua | Ratio |
|---|---|---|---|
| VM pura: fib(24) | 14,0 ms | 17,4 ms | **0,80× (más rápido)** |
| VM pura: tablas (20k alloc+suma) | 9,8 ms | 23,9 ms | **0,41× (2,4× más rápido)** |
| VM pura: strings (concat 2000) | 1,79 ms | 1,13 ms | 1,6× |
| pcall+throw (por throw) | 40 µs | 5,9 µs | 6,8× |
| Llamada host (por llamada, con int) | 1,07 µs | 0,20 µs | 5,3× |
| Host con string 1KB ida+vuelta | 2,4 µs | 1,1 µs | 2,2× |
| yield+resume (por ciclo) | 26–192 µs (§4.1) | 0,3 µs | ~90–650× |
| Arranque de instancia (módulo ya compilado) | 586 µs | 228 µs | 2,6× |

Lectura honesta:

- **La VM pura es igual o mejor** — el intérprete de PUC bajo el JIT de wazero
  compite de tú a tú con gopher-lua y gana en los casos con presión de tablas.
- **Cada cruce de frontera cuesta ~1 µs** (vs ~0,2 µs). Para nu esto muerde
  poco por diseño: las primitivas son gruesas (un `nu.search.grep` cruza una
  vez y trabaja en Go) y el camino caliente del streaming cruza pocas veces
  por chunk. El patrón "CPU ardiendo en Lua = falta una primitiva" ya empuja
  los cruces a mínimos.
- **El throw a 40 µs** solo importaría en código que use `pcall`+`error` como
  control de flujo caliente — raro, y 6,8× sobre una base de µs.

### 4.1 La única luz ámbar: el coste del yield

`Snapshot()` de wazero **clona la pila nativa completa** del motor y `Restore`
la adopta: el ciclo yield+resume cuesta 26 µs en frío y degrada hacia
~40–190 µs con la presión de GC de los clones (medido a 500/5k/43k ciclos; es
coste por-operación + churn, no un leak — la contabilidad LIFO del trampolín
queda balanceada). Para el puente ⏸ real — un yield por operación de IO, que
tarda ms — es irrelevante. Para un hipotético bucle caliente de yields, no.
Vías si algún día molesta: snapshot incremental aguas arriba (el motor sabe
cuánta pila está viva), afinar el tamaño de pila del motor, o un diseño de
scheduler que solo suspenda en la frontera del slice.

## 5. Qué costaría la migración real (si se decide)

Estimación sin cambios respecto a lo hablado: **10–15 sesiones** sobre la zona
S01-S11 del kernel (sandbox, scheduler, puente ⏸, codecs en la frontera,
watchdog, workers), con la suite de conformidad existente (G31/G41, watchdog,
cancelación, workers) como contrato ejecutable. Los dos deberes de entrada:

1. **El puente definitivo**: decidir corrutina-nativa-por-task (posible ya:
   yield cruza pcall) vs conservar goroutine+token; y estabilizar el coste del
   yield si se elige lo primero (§4.1).
2. **El baseline de lenguaje**: PUC 5.4 implica pasar el api.md §1.2 de
   "Lua 5.1" a "Lua 5.4" (cambios menores tipo `unpack`→`table.unpack`, y el
   sandbox ya curaba las zonas conflictivas) — o compilar PUC 5.1.5, sin
   cambio visible pero renunciando a `lua_yieldk`. Decisión de contrato, no
   técnica.

Riesgo residual señalado: la API de snapshots de wazero es **experimental**
(estable de facto — la usa su soporte de emscripten — pero sin garantía de
firma); y el truco de la no-reentrancia de `api.Function` es un detalle de
implementación a vigilar en upgrades. Ambos quedan cubiertos por la suite de
conformidad del día que se migre.

## 6. Contra el criterio acordado

> "Si todo funciona, o incluso mejora, aunque haya trabajo que hacer, vale la
> pena hacerlo."

- ¿Funciona todo? **Sí** (§2, §3; cero parches al Lua de referencia).
- ¿Mejora? **Sí, en lo estructural**: semántica de referencia (G31 y G41
  imposibles por construcción), runtime mantenido, aislamiento de memoria real
  como regalo futuro para workers/caps (P2), y VM pura igual o más rápida.
- ¿Trabajo que hacer? Sí y acotado: 10–15 sesiones + 2 decisiones de diseño.

**Veredicto: vale la pena.** El orden sugerido si se avanza: (1) decidir el
baseline 5.1/5.4 y el modelo del puente (dos discusiones de diseño con sus
ADR); (2) la "interfaz de VM" en el kernel como primera sesión (compra
opcionalidad y no compromete); (3) la migración por fases contra la suite de
conformidad. Nada de esto urge: gopher-lua + blindajes sigue siendo un statu
quo operable — este spike convierte la alternativa de hipótesis en opción
medida.

---

*Reproducir: `./build.sh` (clona Lua 5.4.7, compila `lua.wasm`), después
`cd go && go test ./...` y `go test -run XX -bench . -benchtime 2s`. Las
fuentes de Lua y los `.wasm` no se versionan (`.gitignore`): MIT de terceros
fuera del repo, artefactos reproducibles.*

[#448]: https://github.com/yuin/gopher-lua/issues/448
