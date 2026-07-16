# G15 · El interior de un worker: scheduler propio y watchdog — `api.md` §13 / `modelo-ejecucion.md` — **RESUELTO**

**Resolución** (aplicada en [api.md](api.md) §13): cada worker es un
mini-runtime completo (scheduler propio, multi-task, timers, futures) y
**sin watchdog** — los workers existen para quemar CPU a gusto; el control
es `terminate()` + `caps`. El watchdog configurable se descartó: un mando
sin modelo de amenaza (no hay UI dentro que proteger).

**Problema.** `task` es [W] y el escenario 4 ya asumió multiplexar con
`race` dentro del worker, pero nunca se escribió que cada worker tenga su
propio event loop, ni si admite múltiples tasks y timers, ni si el
watchdog aplica dentro (¿con qué presupuesto, si no hay UI que proteger?).

**Impacto.** Clarificación de contrato; el escenario 4 depende de ello.

**Opciones.** (a) Cada worker = mini-runtime completo (loop propio,
multi-task, timers) sin watchdog (no hay UI que proteger; `terminate()`
es el control); (b) igual pero con watchdog configurable (protege de
workers zombis quemando CPU).
