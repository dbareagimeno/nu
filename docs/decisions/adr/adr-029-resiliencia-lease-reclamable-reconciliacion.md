---
title: "Resiliencia de recursos persistentes: lease reclamable por identidad verificable + reconciliación (doctrina de extensión, consecuencia de ADR-025)"
type: "adr"
id: "ADR-029"
status: "aceptada"
date: "2026-07-19"
---
# ADR-029 · Recursos persistentes = lease reclamable + reconciliación

**Estado:** Aceptada · 2026-07-19 (consecuencia de
[ADR-025](adr-025-reposicionamiento-motor-de-harnesses.md); resuelve la doctrina
de [G60](../../findings/g60-el-lock-de-sesion-nace-huerfano.md))

**Ámbito.** Esta es una **doctrina de extensión**, no una regla del kernel. Vive
en los contratos de las extensiones oficiales (`sesiones.md` §6 para `sessions`,
`malla.md` §3 para `mesh`) y se construye **enteramente con la API pública v1**
(`enu.fs.write{exclusive}`, `enu.sys.now_ms/hostname`, `enu.proc.alive`,
`enu.proc.run(["git",...])`). El core no adquiere vocabulario de «lock», «lease»
ni «sesión»: sigue siendo ciego a lo que es un recurso persistente (idea central
1). Se registra como ADR —y no solo como prosa de §6— porque es **transversal**
(gobierna a la vez el lock de `sessions` y el claim de `mesh`) y **cambia una
regla del juego** ya escrita, precedente que existe para otras extensiones
([ADR-005](adr-005-providers-de-llm-registro.md) providers,
[ADR-023](adr-023-los-permisos-de-bash.md) permisos del agente).

**Contexto.** [G60](../../findings/g60-el-lock-de-sesion-nace-huerfano.md)
reprodujo con el binario que el `.jsonl.lock` de una sesión **nace huérfano al
arrancar** el chat: el `enu.task.cleanup` que prometía soltarlo corre en la
frontera de terminación **sin contexto de task** y no puede llamar a
`enu.fs.remove` (⏸, `EINVAL`), de modo que la garantía de `sesiones.md` §6 —«se
libera al salir vía cleanup, pase lo que pase»— era **inimplementable** tal como
estaba escrita (corolario de completitud). La investigación acotó tres hechos
que reencuadran el problema:

- La liveness que §6 fijaba (pid + `enu.proc.alive`) tiene un **punto ciego**: un
  pid reciclado por el SO clasifica un lock muerto como «vivo» (H-F).
- El rechazo de `flock` por «semántica predecible en Windows» quedó **caduco**
  cuando G9 sacó Windows nativo de la v1 (solo WSL2, POSIX íntegro — H-E); pero
  `flock` sigue siendo **mono-host** e inservible para la malla, cuyos claims
  cruzan máquinas.
- [malla.md](../../contracts/malla.md) §3 **ya** protegía sus claims con
  heartbeat renovable (`--force-with-lease`), umbral de staleness generoso y
  «robar = `release` + `claim`»: encarnaba de facto una doctrina de lease que
  `sessions` no compartía.

El reposicionamiento de
[ADR-025](adr-025-reposicionamiento-motor-de-harnesses.md) —motor para construir
harnesses en entornos corporativos, con `mesh` como columna vertebral y la
**resiliencia a fallos como pilar**— asciende esos recursos distribuidos de
«borrador» a caso primario y hace de la garantía de liberación una propuesta de
valor.

**Decisión.** Un recurso persistente se protege haciéndolo **reclamable desde
fuera por identidad verificable**, no prometiendo liberarlo al morir (ningún
proceso controla su propia muerte):

1. El **dueño renueva** su tenencia mientras vive (marca de frescura: re-grabar
   `started`/mtime en el lock local; `mesh.heartbeat` en el claim distribuido).
2. Lo que queda **rancio** (frescura no renovada en un umbral **generoso** —
   minutos, no segundos, porque los relojes no están sincronizados) lo
   **reconcilia** el siguiente proceso que abre, o un reaper.
3. `enu.proc.alive` degrada a **señal secundaria** (refuerza, no funda: no cruza
   máquinas y confunde el pid reciclado).

Jerarquía de garantías, contable a un cliente:

- `enu.task.cleanup` (síncrono, solo-memoria) → **prontitud en memoria**.
- **drenaje del apagado** (el runtime cancela las tasks vivas y bombea con un
  plazo antes de demoler la VM; interno del kernel, sin API nueva —
  [modelo-ejecucion.md](../../core/modelo-ejecucion.md) §limitaciones) →
  **prontitud de I/O en salida limpia**.
- **lease renovable + reconciliación** → **corrección pase lo que pase**, incluso
  quitando el enchufe.

**Consecuencias.**

- `sesiones.md` §6 reescribe su liveness: lease renovable + reclamación por
  rancidez (cierra H-F). `Session:close` es ⏸ y se llama explícitamente antes de
  `core:shutdown`, bajo task de vida larga, **nunca** desde un cleanup
  (`agente.md` §2, `api.md` §3).
- `malla.md` §3 reconoce que su claim/heartbeat **es** esta doctrina en el plano
  distribuido; el worktree (§5) se libera por cierre explícito / cleanup→spawn
  (su `remove` es ⏸), y los huérfanos los reconcilia el reaper.
- El **drenaje del apagado** (llamado A2 en G60) alinea la implementación
  interactiva con la promesa ya escrita en `api.md` §1.3 (los cleanups corren
  «pase lo que pase»): es un **bug fix hacia la espec**, no una espec nueva por
  la vía de hecho — por eso no necesita ADR propio y va a `modelo-ejecucion.md` +
  la sesión de construcción. Su plazo se dimensiona como presupuesto **genérico**
  de drenaje (al estilo del watchdog de `api.md` §1.3, configurable en
  `enu.toml`), **no** a la medida de ningún recurso concreto — o se colaría
  conocimiento de producto en una constante del kernel.
- El patrón **cleanup→spawn** (un cleanup lanza una task que hace el I/O de
  cierre) se eleva a contrato consciente: fija que `enu.task.spawn` es válido sin
  contexto de task. No añade superficie de API (no hay bump de `enu.version.api`).
- `flock` (C1) queda descartado como columna vertebral (mono-host + NFS),
  reconsiderable solo como optimización local. Permitir ⏸ **directo** en cleanups
  (A1) se pospone como **P46** con disparador (fricción real de autores con
  cleanup→spawn).
- La **construcción** (drenaje, orden de `Session:close`, ciclo de vida del chat
  bajo task de vida larga, renovación/reaper del lock) queda como trabajo de
  implementación, registrable vía `/planificar-sesion`. La aserción retirada «el
  `.jsonl.lock` desaparece» (`e2e/chat_test.go`) se restaura una vez construido.
