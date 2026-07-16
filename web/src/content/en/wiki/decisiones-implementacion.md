# Implementation decisions and deviations

This file collects implementation decisions that were not specified in
detail in the design documents, and one-off deviations from the plan, one
entry per session. It does not replace the design flow (`problemas.md`/`adr.md`):
it collects the operational matter that doesn't rise to a `G##` finding but
that the next session must be able to reconstruct.

## S05 — `enu.task.sleep`/`defer`/`every` + `Timer:stop` (api.md §3)

### Quiescence semantics with active timers (key decision)

`api.md` §3 doesn't say how timers interact with the end of `enu -e`. The
S04 model has `EvalString` run the chunk, release the token, and call
`waitIdle()`, which blocks until the set becomes **quiescent**. It was
necessary to decide what counts as "pending work":

- **`defer(fn)` DOES count.** It's "the next tick": its handler must run
  before `EvalString` returns. It's tracked with a `pending` counter
  (incremented on enqueue, decremented on firing execution); `waitIdle`
  waits for `live == 0 && pending == 0`. Without this, a `defer` enqueued by
  the chunk might never get to run.

- **`every(ms, fn)` does NOT count.** A periodic timer never ends; if it
  counted toward quiescence, `enu -e` would never return. Decision: an
  active `every` is **background convenience**, not foreground work. The
  end of `enu -e` is determined by the chunk + its tasks + its enqueued
  `defer`s; once all of that goes quiescent, `EvalString` returns even if
  timers are still active, and `Runtime.Close` shuts them down (cutting
  their ticker goroutines, no leaks).

  Rationale: in an interactive `enu` (S33+) the loop stays alive because of
  the UI/input events, not because of timers; under `enu -e` (headless, no
  UI) the natural end is foreground quiescence. A timer that should keep
  the process alive indicates the real work is in a task (which does
  count), not in the timer. This is consistent with the de facto criterion
  of S05 in the plan ("`every` fires N times and `stop` cuts it"): the
  tests anchor the runtime with a task while the timer ticks.

### Synchronous handlers on an ephemeral thread (not on `host`)

`defer` and each `every` firing run a **synchronous** handler (not ⏸, §3):
they run under the token, like the chunk and event handlers. They execute
on a **thread dedicated per firing** (`host.NewThread()`), not on the main
state's stack. Reason: while `EvalString` is in `waitIdle`, the `host`
stack still holds the chunk's return values; a `CallByParam` on `host`
could interfere. It's the same strategy tasks use (each on its own `co`).
Cost: one thread per firing, collected by gopher-lua's GC (there's no
`Close` per thread in the API, same as for tasks' `co`s).

### `stop` without a late firing (tick/token race)

An `every` firing can be left waiting for the token right when `stop`
arrives. To guarantee "after `stop`, not one more tick", the firing uses
`runSyncHandlerCancelable`: while waiting for the token it also watches
`stopCh` and, if it was closed, does not execute. `stopTimer`/`stopAllTimers`
close `stopCh` idempotently (only if the timer is still tracked), so a
double `Timer:stop()` doesn't panic.

### Test convention with `-race`

`go test -race` requires cgo; the rest of the project builds with
`CGO_ENABLED=0` (ADR-001). So: `CGO_ENABLED=0 go build ./...` for the
binary, and `CGO_ENABLED=1 go test -race -count=4 ./...` for the suite
with the race detector (same criterion S04 left in the logbook). Timing
tests use short periods (1-5 ms) and generous waits; `-count=4`/`-count=8`
produced no flakiness.

### No findings

The S04 model (goroutine-per-task + token) was enough for S05 without
extending the API. No `G##` was opened.

## S06 — `enu.task.future` (single-use rendez-vous, api.md §3)

### Procedural deviation: branch from `origin/main`

This session was implemented starting from `origin/main`, where the ▶
pointer already marked `S06` (S05 had been merged). The local working
branch was out of sync; `claude/s06-future` was created from `origin/main`
to start from the real state. There is no *scope* deviation: S06 only
depends on S04 (closed), so the dependency graph is respected.

### Quiescence: `set`/`await` do NOT touch `live`/`pending` (key decision)

An awaiter blocked in `Future:await` is a **task already counted in
`live`** (it was counted at `spawn` time); it doesn't finish until its
`await` returns, exactly like a task blocked in `Task:await`. So futures
don't add their own quiescence accounting: they reuse S04/S05's without
touching it. `set` doesn't move the count either: it resolves and wakes,
but neither creates nor destroys foreground work.

Accepted consequence, consistent with the model: a `Future:await` without
a `set` that resolves it hangs `waitIdle` forever — it's the same
"foreground deadlock" as a task waiting on another that never finishes.
Detecting it would require new API (deadlock detection) that api.md §3
doesn't contemplate; it isn't the future's responsibility to invent it.

### Waking multiple awaiters with a single `set`

The `Task:await` pattern is reused: a `resolvedCh` channel that `set`
**closes** (under the token). Channel closing is a natural broadcast —
every awaiter blocked on `<-resolvedCh` wakes at once — and provides the
happens-before that makes `value` visible (written under the token before
the close) when each awaiter reclaims the token. No lock is needed on
`resolved`/`value`: both are touched only under the token (the token *is*
the lock), and the only cross-goroutine crossing is the channel close.
This is what shields the `-race` test.

### `set()` with no argument resolves with `nil`

Consistent with a future being usable as a mere signal ("it already
happened") and not only as a value carrier. It's not new API: `Future:set(v)`
with optional `v` falls into the `LNil` that `L.Get(2)` returns when no
argument was passed. `set()` with nil still consumes the single use (a
second `set` gives `EINVAL`): resolving with nil is resolving.

### No findings

The S04/S06 model was enough for S06 without extending the API. No `G##`
was opened.

## S07 — `enu.task.all` / `enu.task.race` (api.md §3)

### The S07/S08 boundary: internal cancellation substrate (key decision)

`all`/`race` need to "cancel the rest", but PUBLIC cancellation is S08
(`Task:cancel()`, `enu.task.cleanup` with a LIFO stack, observable
`ECANCELED` in `await`, and the formal guarantee that unwinding is **not
catchable** by a user `pcall`). S07 implements only the **minimal internal
substrate** that these two combinators require, designed so S08 can
**reuse and extend it, not rewrite it**:

- **`cancelCh` + `canceled` per task** (`scheduler.go`). Each task has a
  signal channel that closes exactly once (`cancelOnce`). `cancelTask(t)`
  closes it and marks `t.canceled`. It's the substrate's only entry point;
  `all`/`race` call it on the losers.

- **Observation at suspension points.** `suspend` (which everything ⏸
  hangs off of: `sleep`, the test `suspend_echo`, and `all`/`race`
  themselves), `Task:await` and `Future:await` now also `select` on
  `cancelCh`: if the task being suspended is canceled while waiting — or
  was already canceled on arriving at the ⏸ — it aborts at that point.
  This is **cooperative cancellation**: it takes effect at the next ⏸, not
  mid-execution of Lua (that, for pure CPU, is S09's watchdog).

- **Unwinding via sentinel panic** (`abortSignal`, `scheduler.abort`). On
  detecting cancellation, `suspend`/`await` throw `panic(abortSignal{t})`,
  which unwinds the task goroutine's Go stack. `runTask` receives it
  through `CallByParam` (gopher-lua converts any Go panic into an error
  when crossing its internal `PCall`) and, seeing `t.canceled`, discards
  the outcome: a canceled task **delivers neither `results` nor
  `errValue`** and is not logged (the cancellation is deliberate).

- **`coToTask`** (a `sync.Map` in the scheduler): maps each live task's Lua
  thread to its `*task`, so `suspend` can find the `cancelCh` of whoever is
  suspending. It's populated/cleared in `runTask`.

**What S07 deliberately leaves MINIMAL and S08 will formalize:**

1. **Not catchable by a user `pcall`.** In S07 the abort panic COULD be
   caught by a Lua `pcall` wrapping the suspension point — it's the same
   reason as ADR-011: gopher-lua recovers every Go panic in its internal
   `PCall`. For S07 this is enough because `all`/`race`'s losers (and their
   tests) don't wrap their ⏸ in `pcall`. The formal "**not catchable**"
   guarantee (§1.3) is S08: it will require its own mechanism (re-raising
   `abortSignal` past each `pcall` boundary, or marking the thread as
   "aborting" so a user `pcall` doesn't swallow it). The `abortSignal` type
   was left distinguishable so S08 can recognize and reinject it.

2. **`enu.task.cleanup` (LIFO stack) during unwinding.** It doesn't exist
   yet; S08 will run the registered releasers during unwinding.

3. **Observable `ECANCELED`.** A canceled task today simply delivers no
   result; `await` on it would see an empty outcome. S08 will make
   `ECANCELED` observable in `await` (§1.3), without that capturing the
   cancellation.

4. **Public `Task:cancel()`.** S08 will expose `cancelTask` as a method of
   the `Task` handle. S07 adds no public surface: the only new signatures
   are `all` and `race` (§3, sacred API).

5. **Propagating cancellation to in-flight background work.** In S07, a
   task canceled during a `sleep` lets the background `time.After` run to
   its end (its `deliverFn` is discarded). S08/later sessions can pass a
   `context` to background work to abort it immediately; not needed here.

### Concurrent fan-in: detecting the first error without ordering (decision)

`all` must cancel the rest **as soon as** one task fails, not when its
turn comes by array order. A first attempt waiting on `doneCh`s in order
(`for i := range tasks { <-t.doneCh }`) failed: a slow first task delayed
seeing a fast second task's failure, and the slow one managed to complete
before being canceled (caught by `TestAllCancelsOthersOnError`). The
solution is `waitAllOrFirstError`: an ephemeral goroutine per task reports
its closing to a shared channel; the loop returns the index of the first
failure as soon as it happens, or -1 if all finish successfully. `race`
uses the symmetric `waitFirst` (first close wins, whether success or
error).

### G27 alignment: index by position, not by completion

The 🔒 invariant (G27) comes for free from the structure: `all` resolves
the list into a `tasks[]` slice in table order (key 1..n) and fills
`out[i+1]` with `firstResult(tasks[i])`. The order in which `doneCh`s close
doesn't touch the output array: it's indexed by position. `race` returns
the winner's index **+1** (1-based, Lua). Tests with reversed sleeps
(completion 3,2,1 versus entry 1,2,3) shield against completion order
leaking in.

### Input: handles, functions, or a mix

§3 says `Task[]|fn[]`. It's interpreted in the most permissive way,
consistent with the prose ("handles already created OR functions"): each
array element can be a function (it gets `spawn`ed) or a `Task` handle (it
gets attached), and they can be mixed. A value of another type, or an
empty array, is `EINVAL` with a message naming the position. Each task
delivers its **first** return value (§3: `all`'s array and `race`'s
`result` hold one value per entry, not multi-value).

### No findings

The S04/S06 model plus the internal cancellation substrate was enough for
S07 without extending the API or touching `api.md` §3. The S07/S08
boundary is an **implementation order**, not a `G##`: it was resolved with
the minimal substrate described above.

## S08 — Public cancellation: `Task:cancel` + `enu.task.cleanup` + non-catchable unwinding (api.md §1.3, §3)

S08 is in the 🔒 inventory and is a **veto milestone** (it validates
ADR-008). The hard point — the one that could veto the plan — was making
the abort **not catchable by `pcall`** on top of gopher-lua, which recovers
every Go panic in its native `pcall`. The known technique (wrapping
`pcall`/`xpcall`) worked cleanly; **there was no finding/veto**.

### The non-catchable technique: `pcall`/`xpcall` wrapper (key decision)

gopher-lua implements `pcall`/`xpcall` in Go (`basePCall`/`baseXPCall`) on
top of `LState.PCall`, whose `defer/recover()` captures **any** Go panic
and delivers it to Lua as `false, err`. That's why in S07 `abortSignal` (a
Go panic) WAS catchable by a user `pcall` wrapping the ⏸. To shield it
(§1.3), `cancel.go` **replaces the global `pcall` and `xpcall`** (which S01's
baseline opens natively; they're replaced by `installCancelPcall`, called
by `registerNu` after `applySandbox`) with Go versions that:

1. Reproduce `basePCall`/`baseXPCall` (including the "is callable" check
   and multi-return on success), delegating to `LState.PCall`.
2. On a captured error, check the **`task.aborting`** flag of the current
   task (the one running on the current `LState`, via `coToTask`). If it's
   aborting, they **re-raise** `abortSignal{t}` instead of returning
   `false, err`. That way the abort threads through every `pcall`/`xpcall`
   boundary — nested ones included — up to `runTask`'s `CallByParam`, the
   only one that legitimately recovers it.

`scheduler.abort` sets `t.aborting = true` **right before** throwing the
sentinel; `runCleanups` lowers it before running the releasers (so a
`pcall` inside a cleanup goes back to capturing normally).

### Why `task.aborting` and not the panic value (decision)

When crossing `LState.PCall`, a panic that isn't a `*lua.ApiError` becomes
an `*ApiError` with its message via `fmt.Sprint` — the Go type
`abortSignal` is lost. Detecting the abort from the recovered value would
be fragile (it would depend on the textual representation). Instead
`aborting` is a flag of the task itself, written and read by its single
goroutine **under the token**: robust, gopher-lua-panic-representation-
independent detection. The identical re-raise comes for free (we
reconstruct `abortSignal{t}` from the task). S09 will reuse exactly this
path, setting `reason = abortBudget`.

### `xpcall`: the user's `errfn` does NOT see the abort (decision)

Native `xpcall` would run its message handler (`errfn`) **inside**
`LState.PCall`, i.e. over the abort. That would leak the abort into user
code (§1.3 forbids it). The wrapped version passes `nil` as the handler to
the native `PCall` and applies `errfn` **ourselves, only if the error is
NOT an abort**. Accepted cost: `errfn` runs after unwinding (not before,
as in real Lua), but gopher-lua doesn't expose a rich traceback to the
handler, so nothing observable is lost.

### `ECANCELED` semantics in `await` (key decision)

`Task:await` on a **canceled** task delivers structured `ECANCELED`, which
the awaiter **CAN catch with `pcall`**. This is consistent with §1.3
because it's **observation of ANOTHER task's cancellation**, not the
awaiter's own abort: if the awaiter itself were canceled, its unwinding
would be immune; but *observing* that a task being waited on was canceled
is a normal, catchable error. The awaiter stays alive after the `pcall`
(it runs the code that follows). Implementation: `taskAwait` checks
`t.canceled` (before `t.errValue`, which a canceled task never has) and
throws `ECANCELED` with `raiseError`.

### `Task:cancel` on an already-finished task is a no-op (key decision)

Canceling a task that has **already closed its outcome** must NOT
retroactively turn its result into `ECANCELED` — it finished successfully
(or with an error) before the cancellation, and that's what its `await`
must keep delivering. `cancelTask` checks `t.done` and returns without
touching `canceled`. Reading `t.done` there is safe because all calls
(`Task:cancel`, `all`/`race`) run **under the token**, same as `runTask`'s
`t.done = true`. `Task:cancel` **does not suspend** (it's synchronous from
the outside, §3); canceling twice is idempotent (`cancelOnce`);
self-cancellation is legal (it takes effect at the task's own next ⏸).

### LIFO `cleanup` stack: runs in ALL THREE endings (decision)

`task.cleanups []*lua.LFunction`; `enu.task.cleanup(fn)` pushes onto it
(outside a task → `EINVAL`, there's no task to attach the releaser to).
`runCleanups` (in `runTask`, with the token held, after `CallByParam`)
runs ALL of them in reverse registration order — `defer`-like semantics —
no matter what: success, error, or abort. Each releaser runs on an
**ephemeral thread** (like tasks and S05's handlers) under a `pcall` per
boundary (ADR-008): a cleanup that throws is logged (best-effort; formal
event in S10) and doesn't stop the others from running nor crash the
process.

### S07 substrate reused, not rewritten

`cancelCh`/`cancelTask`/`abortSignal`/`coToTask` and the `select`s on
`cancelCh` in `suspend`/`Task:await`/`Future:await` all remain intact. S08
**adds**: the `aborting` flag, `abortReason` (`abortCancel` vs
`abortBudget`, the latter for S09), the `cleanups` stack, the public
methods `Task:cancel`/`enu.task.cleanup`, `ECANCELED` in `await`, and the
`pcall`/`xpcall` wrappers. New public surface = ONLY `Task:cancel` and
`enu.task.cleanup` (sacred API, §3).

### No findings, no veto

gopher-lua v1.1.2 **does** allow a clean non-catchable unwinding via the
`pcall`/`xpcall` wrapper. Normal §1.4 errors weren't broken (they remain
catchable, multi-return included). `api.md` was not extended. No `G##` was
opened. The S08 veto milestone is validated in favor of ADR-008.

### What S09 (watchdog) inherits

S09 reuses the **same non-catchable unwinding**: it will cut off an
exceeded pure-CPU slice by throwing the same `abortSignal` — but from the
watchdog, not from a suspension point — with `reason = abortBudget`. The
`pcall`/`xpcall` wrappers will already make it non-catchable (they check
`aborting`, agnostic to `reason`); `runCleanups` already runs on abort
regardless of reason; `await` will distinguish the reason to observe
`EBUDGET` instead of `ECANCELED`, and S09 will emit
`core:plugin.misbehaved` (verifiable after S10). The missing technical
hook is **interrupting a Lua slice that doesn't suspend** (a pure-CPU
loop): that's S09's own work (instruction hook / `LState` with a limit),
not S08's.

## S09 — Slice watchdog (api.md §1.3)

### Interrupting a pure-CPU slice: `LState.SetContext` (key decision, veto milestone)

The technical hook S08 left pending — cutting off a **pure-CPU** slice that
never suspends (`while true do end`), with no cooperative checkpoint — is
resolved with gopher-lua v1.1.2's `LState.SetContext`, **the supported
mechanism** (not a fragile debug-hook hack). Verified in its source: with a
context set, the thread runs `mainLoopWithContext` (`vm.go`), which on
EVERY interpreter instruction checks `ctx.Done()` and, if canceled, throws
a Lua error (`L.RaiseError(ctx.Err())` → `*ApiError` with the message
"context canceled") that breaks the loop. `spawn` gives each task thread
its **own** `context.Context` (root `Background`, not a child of `host`'s:
isolating one task doesn't affect others, ADR-008) via `SetContext`.
**There's no finding or veto:** the mechanism exists and integrates with
S08's non-catchable unwinding.

### The watchdog runs on its own goroutine, without the token (key decision)

The key to "without freezing the loop" (CP-2). The slice timer is a
`time.AfterFunc(budget)` that is **armed** when the task takes the token to
run Lua (start of `runTask`; re-acquisition after each ⏸ in
`suspend`/`Task:await`/`Future:await`) and **disarmed** right before
releasing it. If it fires, its callback runs on the timer's goroutine —
which **does not hold the token** — which is why it can cut off a task
that's monopolizing it while other tasks and timers wait: after the cut,
the victim unwinds up to `runTask`, releases the token, and the rest
progresses. The default budget is 100 ms, configurable with
`WithSliceBudget` (a Runtime `Option`; a hook S11/S12 will wire to
`enu.toml`); `<=0` disables the watchdog.

### Each slice is measured separately: arm/disarm on every ⏸ (decision)

A ⏸ closes the slice (disarm) and, on re-acquiring the token, opens a new
one (arm). This way a CPU loop interleaved with suspensions never
accumulates time across slices: each contiguous stretch has its own
budget. Hence "no false positives": normal work that yields often (sleeps,
IO) never triggers the watchdog even if its TOTAL time exceeds the budget.

### Split of writes between watchdog and task: S08's invariant intact (key decision)

S08 documented that `aborting`/`reason`/`canceled` are written ONLY by the
task's own goroutine, under the token. S09 respects this even though the
watchdog lives on another goroutine: the watchdog only touches an **atomic
flag** `budgetExceeded` (`atomic.Bool`, crosses goroutines) and cancels the
`ctx` (safe concurrently). The **task's goroutine** is the one that
"claims" the cut (`claimBudgetAbort`): on detecting `budgetExceeded`, it
sets `aborting`/`reason=abortBudget`/`canceled` itself, under the token.
The claim happens in two places: in `reraiseIfAborting` (when a user
`pcall` captured the ctx-error → it re-raises `abortSignal` to thread it
non-catchably) and in `runTask` after `CallByParam` (when the ctx-error
arrived with no wrapping `pcall`). In `suspend`/`*await` the claim also
happens before blocking (a previous slice right at the limit). Idempotent.

### `EBUDGET` vs `ECANCELED` in `await`: distinguished by `reason` (decision)

`Task:await` on a task aborted by the watchdog observes **`EBUDGET`**; on
a canceled one, **`ECANCELED`**. Both are *observation* of ANOTHER task —
the awaiter DOES catch them with `pcall` and survives — not the awaiter's
own abort. The distinction is by `t.reason` (`abortBudget` vs
`abortCancel`); it's checked before `errValue` (an aborted task never has
`errValue`).

### `core:plugin.misbehaved` via internal hook (decision; S10 wires it)

The `enu.events` bus is S10 (doesn't exist yet). Emission is done through
an **internal hook** `rt.emitMisbehaved(owner, reason)` that today logs
best-effort (like the rest of task failures). **S10 will wire it** to
`enu.events.emit("core:plugin.misbehaved", {plugin = owner, reason = ...})`
without touching the watchdog (the call site is already unique). NO public
surface was invented: §1.3 says the watchdog is transparent.

### Scope: only a task's slice (decision)

Synchronous handlers (`defer`/`every`) and `cleanup` run on `host`'s
ephemeral threads, which have no context, so the watchdog doesn't watch
them. S09's scope (api.md §1.3) is a **task's slice**; watching synchronous
handlers would be a separate piece, outside this session. Consistent with
`Close` canceling each task's `ctx` at the end (avoiding a
`context.WithCancel` leak).

### No new public surface, no finding, no veto

S09 adds nothing to `api.md` (the watchdog is transparent; `WithSliceBudget`
is a Go `Option`, not Lua API). gopher-lua v1.1.2 DOES allow interrupting a
pure-CPU slice cleanly, integrable with S08's unwinding. The S08+S09 veto
milestone is validated in favor of ADR-008 (per-task isolation). Green
CP-2 closes Phase 1.

## S10 — `enu.events` event bus (api.md §4)

### The emit queue: flat draining (key G10 decision)

`api.md` §4 requires that a nested `emit` (thrown by a handler) be
**enqueued** and dispatched once the current one finishes — breadth, not
recursion — so that an infinite ping-pong is "a flat loop the watchdog
cuts", never a recursion that overflows the Go stack. The realization
(`events.go`, `scheduler.emit`):

- The bus carries a `queue []pendingEmit` and a `draining` flag. `emit`
  ALWAYS enqueues; if a drain is already in progress (`draining == true`),
  it just enqueues and returns. The **root frame** of the `emit` (the
  first one, which set `draining`) drains the queue in a flat loop
  `for len(queue) > 0 { dispatch(...) }`. So a handler that re-emits
  doesn't nest a `dispatch` call in the Go stack: it leaves work in the
  queue that the root loop picks up after the current dispatch finishes.
- Consequence: the order is **breadth-first** (BFS), not depth-first. An
  "a" handler that emits "b" produces `a:start, a:end, b`, not
  `a:start, b, a:end`. Tests pin this down.

### The watchdog and the infinite ping-pong (the non-obvious nuance)

The requirement "the watchdog cuts the infinite ping-pong" had an
implementation trap that **was verified with a test before being taken for
granted**: S09's cut-off mechanism is canceling the `context.Context` of
the task's `co` thread, which the interpreter watches on every instruction
(`mainLoopWithContext`). But event handlers do NOT run on the task's `co`:
they run on **`host`'s ephemeral threads** (like `defer`/`every`), which
carry no context. During a ping-pong, the task isn't executing Lua on its
`co` — it's orchestrating handlers in Go — so canceling its ctx breaks
nothing and the drain loop would keep going forever. Confirmed: an
infinite ping-pong from a task hung for 5 s without being cut.

Solution, consistent with S09 and **with no new piece or API**: the drain
loop, when the root `emit` was launched inside a task (`taskOf(L)`), checks
`claimBudgetAbort(t)` cooperatively **between handlers** (at the edge of
each loop iteration). When the watchdog fires (setting `budgetExceeded`),
the next edge claims it and calls `abort(t)` — the same non-catchable
sentinel from S08/S09 — which unwinds up to `runTask`: non-catchable
`EBUDGET` + `core:plugin.misbehaved`. This covers the case `api.md` §4
names (the bounce between handlers); a SINGLE handler with
`while true do end` inside it stays OUT of scope (ephemeral thread with no
ctx, exactly like `defer`/`every`, which S09 left out). The watchdog's
limit is the same as in S09; the cooperative edge only extends the cutoff
to the bus's orchestrator, which does run on the task's goroutine.

Robustness detail: since `abort` unwinds via panic mid-queue, a `defer` in
`emit` restores `draining = false`/`queue = nil` on exit; without it, the
bus would be permanently stuck (every future `emit` would see
`draining == true` and just enqueue). The panic keeps rising toward
`runTask`, which recovers it.

### Emitting misbehaved from the task's goroutine (thread safety)

`rt.emitMisbehaved` (S09's hook) now emits `core:plugin.misbehaved` through
the real bus. `runTask` calls it from the task's goroutine — on the `co`
thread, not on `host` — but **with the token held** (before `release`).
The question was whether it's safe to emit toward the main state's bus
from there. Yes, and it's emitted **directly** (synchronously), without
re-enqueuing to another goroutine: the bus touches `host` (the payload
table, handlers' ephemeral threads), not `co`; and what protects those
accesses is the **token**, not which goroutine/thread does them. We
already have the invariant the bus needs (token + main state), so
re-enqueuing would be complexity for no benefit. `rt.L` (host) is passed as
`emit`'s calling thread, not `co`: the misbehaved emission is a single
event (not a task drain that must watch its own watchdog — the task that
triggered it is already aborted), so it doesn't hook into the drain
watchdog's cooperative edge.

### No extra surface, no finding

New surface = exactly `enu.events.on/once/emit` + `Sub:cancel` (§4). The bus
is **main-state only** (not [W]); it doesn't exist in a worker (S34). The
S04–S09 model (token + watchdog + non-catchable unwinding) was enough: the
watchdog's oversight in the drain reuses `claimBudgetAbort`/`abort`,
inventing nothing. APILevel stays at 1 (api.md already described §4; this
isn't a post-freeze addition).

## S11 — plugin loader (api.md §14)

Exact new surface: `enu.plugin.current()`, `enu.plugin.list()`,
`enu.config.dir()` [W] and `enu.config.data_dir()` [W] (§14). The canonical
boot (plugin loading, the user's `init.lua`, `core:ready`) is triggered by
`Runtime.Boot`, an internal Go method, not Lua surface. APILevel stays at 1
(api.md already described §14; this isn't a post-freeze addition). No
finding: the S04–S10 model (token + main state + event bus) was enough.

### Added TOML dependency

`github.com/BurntSushi/toml` (resolved to v1.6.0 via `go get`; **pure-Go**
TOML, consistent with `CGO_ENABLED=0`/ADR-001). It's used to parse
`plugin.toml` (fields `name`, `version`, `requires?`) **internally in the
loader**: it is NOT the Lua API `enu.toml` (that's S18, which will reuse
this same library for `enu.toml.encode/decode`). `go mod tidy` leaves
go.mod/go.sum coherent.

### Loader model

- **Discovery**: for each directory passed with `WithPluginDir`, every
  subdirectory with a `plugin.toml` is a plugin. Name uniqueness is
  validated at discovery time (a `map[name]`); a collision = actionable
  `EINVAL` naming the plugin and **both paths**. The name is identity
  (§14), which keeps event namespaces collision-free by convention (G26).
- **Topological order**: post-order DFS over the `requires` graph (the
  dependency before the dependent). Deterministic visiting (nodes and
  `requires` sorted by name) so boot is reproducible. Two actionable
  errors: a cycle (colored white/gray/black; re-encountering gray
  reconstructs the cycle segment `a -> b -> a`) and a missing dependency
  (a `requires` that doesn't match any discovered plugin). Graph
  validation is **total, up front, before** running a single `init.lua`: a
  broken graph doesn't leave the system half-loaded.
- **Canonical boot** (`Boot`, under the token, in the main state — like a
  `-e` chunk, not like a task): require paths → for each plugin in
  topological order {push owner, run `init.lua`, emit `core:plugin.loaded`}
  → the user's `init.lua` (`config.dir()/init.lua`) **last** →
  `core:ready` **once**. An `init.lua` that throws is **isolated**
  (ADR-008): it's logged, `core:plugin.error` is emitted, and the other
  plugins + the user keep loading; `Boot` only returns an error for an
  invalid **graph** (collision/cycle/missing), not for a runtime failure
  of an init.

### `require` paths

The baseline (S01, sandbox.go §1.2) left `package`/`require` **closed**.
The loader opens `OpenPackage` once, in `setupRequirePaths`, and sets
`package.path` to **only** the plugins' `lua/` (`<dir>/lua/?.lua` and
`<dir>/lua/?/init.lua`). It deliberately does NOT include the `./?.lua`
that gopher-lua brings by default: `require` is for plugin modules, not a
hole for loading arbitrary files from the cwd (it respects the sandbox).
`cpath` empty (no C libraries, CGO_ENABLED=0). The loader uses `L.LoadFile`
to run the `init.lua` files (it's the only thing authorized to touch disk
that way, §1.2); `dofile`/`loadfile` remain disabled as globals.

### Owner stack for `enu.plugin.current` and logging

`rt.owner` (string, S03) was replaced with `rt.ownerStack []*pluginInfo` +
`rt.currentOwner()` (top of the stack, or `"user"` if empty). The loader
pushes a plugin's context before its `init.lua` and pops it when done
(defer). So, DURING a plugin's `init.lua`, `enu.plugin.current()` and the
log's owner are that plugin; outside it (the `-e` chunk, the user's
`init.lua`, handlers) it's `"user"`. The stack is mutated **only under the
token** (boot is synchronous) and read only from Lua code (which also
requires the token): no lock and no race (`-race` green). `current()` is
never `nil`: outside a plugin it returns `{name="user", version="",
dir=config.dir}`. Known limitation (not S11's scope): a task spawned from a
plugin's init runs **after** the owner has been popped, so it'll see owner
"user" — reliable tagging of handles by owner is S13's work (reload),
which is built on top of this stack.

### Boundary with S12/S13

- **S12** (activation via `enu.toml` + `go:embed` embedded extensions): the
  `pluginInfo.Source` (`"user"`/`"builtin"`) and `pluginInfo.Enabled`
  fields are anticipated but in S11 are always `"user"`/`true`.
  `WithSliceBudget`/`WithDataDir`/`WithConfigDir`/`WithPluginDir` are the
  hooks S12 will wire to `enu.toml`. The activation hook (which plugins get
  loaded) lives in `loader.discover`/`Boot` without pre-empting its logic.
- **S13** (`enu.plugin.reload`): relies on `ownerStack`/`currentOwner` to
  tag handles by owner (G2); the reload itself (clearing the require
  cache, `core:plugin.unload`, re-running init) is NOT implemented in S11.

## S12 — activation of embedded extensions governed by `enu.toml` (api.md §14, ADR-010)

S12 assembles what ADR-010 requires: official extensions ship INSIDE the
binary (`go:embed`) but are **INACTIVE by default** — an installed nu is a
bare runtime; the harness is activated, not presumed —. Activation is
governed by `config.dir()/enu.toml`.

### `enu.toml` is core config, not the Lua API `enu.toml`

`config.dir()/enu.toml` (config_toml.go) configures the runtime ITSELF: not
to be confused with `enu.toml`, the codec (Lua API from S18). Both reuse the
same pure-Go TOML library added in S11 (BurntSushi), but they're different
things. v1 fields read: `plugins.enabled` (activation list), `plugins.dirs`
(extra paths), and `watchdog.slice_budget_ms`. Unknown keys are ignored
(forward-compat, same as `plugin.toml`). An ABSENT `enu.toml` is the
runtime's normal bare state: it activates nothing and is not an error.

### Parsed in `New`, config error DEFERRED to `Boot`

`enu.toml` is parsed in `New` (at that point `config.dir()` is already known
and its values must be ready before `Boot`: the watchdog budget goes to
the scheduler `New` builds, and the activation list to the loader). But
`New` **does not return an error** (its signature is sacred, §17).
Decision: a malformed `enu.toml` is saved in `loader.configErr` and
returned by `Boot` (whose signature does allow it), **before** touching any
plugin. This way the config error doesn't leave boot half-done and reaches
`main`/tests through the path that already existed for graph errors.

### `slice_budget_ms` with `*int` and Option precedence

`slice_budget_ms` is `*int` (not `int`) to distinguish "unspecified" (nil →
the 100 ms default or `WithSliceBudget` governs) from "specified as 0" (0 →
explicitly disables the watchdog, S09 semantics). Precedence: **explicit
`WithSliceBudget` Option > enu.toml > default**. `config.sliceBudgetSet` was
added (set by `WithSliceBudget`) so a test that fixes its budget isn't
overridden by the on-disk config. `plugins.dirs` is simply **added** to
`WithPluginDir`'s paths.

### `go:embed` infrastructure and materialization to disk

`embed.go` embeds the `internal/runtime/embedded/` tree with
`//go:embed embedded` (the directory MUST exist for `embed` to compile,
hence the STUB). S11's loader loads plugins from directories ON disk
(reads `plugin.toml` with `os.ReadFile`, runs `init.lua` with `L.LoadFile`,
adds `lua/` to require paths). Decision: for an embedded extension to load
**exactly like** a user plugin (§14), its tree is EXTRACTED from the
`embed.FS` to `<data_dir>/embedded/<name>` (`extractEmbedded`, idempotent:
it overwrites, so a new binary wins over what was extracted before), and
S11's loader is reused. The alternative — teaching the loader to read from
an `fs.FS` — would duplicate discovery for zero gain (the tree is tiny). No
network (ADR-010): everything comes from the binary.

### The `example` STUB extension

The real official extensions (agent, chat, providers, MCP, toolkit) are
Phase 8 and don't exist yet, but the embed + gating mechanism is already
tested in S12. For that, the embedded tree contains a single STUB
extension, `embedded/example/` (`plugin.toml` + `init.lua` that leaves the
mark `_example_embedded_cargada=true`). It exists SOLELY for the gating
tests; when the real official ones arrive they're added under `embedded/`
without touching the mechanism.

### ADR-010 gating in `loader.discover`

After discovering the disk plugins (S11, unchanged), for each name in
`plugins.enabled`:
- if it's already a disk plugin → the **user dir REPLACES** the embedded
  one with the same name (§14): the embedded one is not materialized, the
  user one wins (`source="user"`), they don't coexist;
- if it's an embedded one from the catalog → it's extracted and loaded
  with `source="builtin"`;
- if it's neither → **actionable** `EINVAL` naming the extension and the
  **`plugins.enabled` line of `enu.toml`** that fixes it (§14).

Scope decision: disk plugins (`WithPluginDir`/`plugins.dirs`) keep loading
as in S11, without gating. ADR-010 talks about **embedded official
extensions** being inactive by default; the user's explicit plugins, being
already chosen by definition, get loaded. This also avoids regressing
S11's tests (which boot without `enu.toml`). S12's test cases (embedded
gating, name-based replacement, actionable errors) are all covered.

### No new Lua surface

S12 is internal config/loader work. `enu.plugin.list()` already reflected
`source`/`enabled` from S11; an activated embedded one comes out as
`{source="builtin", enabled=true}`. `api.md` wasn't touched (§14/ADR-010
were enough); APILevel stays at 1. No findings.

### Explicit boundary with S33 (G21)

The **bare-runtime screen** (TTY render of the embedded catalog +
activate/exit, no network) is UI: it was NOT done in S12. It's S30/S33.
S12 left the catalog (`embeddedNames`) and `enu.toml`-based activation
ready, which is what that screen will consume.

## S13 — `enu.plugin.reload` (best-effort, G2) (api.md §14)

### Registry of handles by owner: general, not an events+timers patch (key decision)

`api.md` §14 says `reload` "releases all of the plugin's handles (the core
tags them by owner via `plugin.current()`)". The realization could have
been an ad-hoc aggregate (walking `eventBus.subs` filtering by owner +
walking `scheduler.timers` filtering by owner). This was rejected: the set
of persistent handles will grow (S15 watchers, S16 procs, S29+ UI
input/regions) and a reload that enumerates special cases would rot.
Decision: **a single registry** `scheduler.ownerHandles`
(`map[ownerName][]ownedHandle`) and interface
`ownedHandle{ release(); owner() }`. Every primitive that hands out a
persistent handle tags it with `currentOwner()` (S11) on creation and
calls `track`; on manual release (`Sub:cancel`/`Timer:stop`) it calls
`untrack`. `reload` iterates the owner's list and calls `release()` without
knowing the types. Adding a new primitive = implement `ownedHandle` +
`track`/`untrack`; reload picks it up for free. Consistent with "the core
doesn't know about product" (philosophy §1) and with the sacred API (no
signature is added: the only new surface is `enu.plugin.reload`, already in
§14).

### `untrack` on the manual path, not in `stopTimer`/release (no double cleanup)

`luaTimer.release()` calls `stopTimer` (cuts the goroutine). But
deregistering from `ownerHandles` does NOT go in `release()` nor in
`stopTimer`: it goes in `timerStop`/`subCancel` — the **manual** path.
Reason: `releaseOwnerHandles` (reload's path) already deletes the owner's
entry from the map before iterating; if `release()` also touched the
registry, it would be double cleanup (and, worse, mutating the map mid-
iteration). This way the registry has a single owner of the mutation per
path: reload deletes in bulk; manual cancel/stop remove one. Both
idempotent (a handle that's no longer there gives no error). No leak and
no race (everything under the token).

### Fix: a `once`'s auto-cancellation also deregisters (no leak)

A review found a missing deregistration path: when an
`enu.events.once` **fires**, `dispatch` (events.go) auto-cancels it
(`sub.live = false`) and `purge` removes it from `eventBus.subs`, but it
didn't go through the manual path (`subCancel`), so the dead handle stayed
in `ownerHandles[owner]` forever. For a long-lived owner (e.g. "user")
that uses `once` repeatedly, the map grew without bound — a leak that
violates S13's 🔒 invariant ("no leak in the registry"). Minimal fix: after
marking the `once` dead in `dispatch`, call `s.untrack(sub)` (runs under
the token, in the main state's dispatch; `untrack` is idempotent, so a
`reload` that already emptied the list is unaffected). It's the same
deregistration `subCancel` already did on the manual path, now also on
auto-cancellation. Covered by `TestReloadOnceAutoCancelSinFuga` and
`TestReloadOnceDisparadoAntesDeReload`.

### require cache: enumerate the plugin's `lua/`, don't guess from package.loaded

`package.path` is shared by ALL plugins (S11), so a module
`foo` in `package.loaded` could come from ANY plugin's `lua/`. To flush ONLY
the cache of the plugin being reloaded, `clearRequireCache` **enumerates the
`.lua` files under THAT plugin's `<dir>/lua/`**, translates them to module
names (`foo.lua`→`foo`, `foo/init.lua`→`foo`, `bar/baz.lua`→`bar.baz`, following the
patterns of `setupRequirePaths`) and sets them to `nil` in `package.loaded`. Other
plugins' modules are not purged even if the name matches —the reload is of the
plugin, not of the global module namespace—. It is best-effort (G2): if two plugins
export a module with the same name, who wins `package.path` is the loader's
concern, not the reload's.

### `reload` is ⏸ even though today everything is synchronous under the token

§14 marks `reload` as ⏸. Today all its steps are main-state work
under the token (synchronous emit, releasing handles, re-running the init with `L.LoadFile`),
with no background IO. The marker is respected anyway: (a) it reserves that reading the init
might become genuinely ⏸ in the future without changing the signature; (b) homogeneity —a
development tool is invoked from a task like the rest of async—. Detection is
that of §1.3 (`L == host` → `EINVAL`). The user's `init.lua`
(owner "user") is not reloadable through this path: re-running it would mean re-starting, outside
the scope of G2 (which is "reload a plugin").

## S14 — `enu.fs` (api.md §5)

S14 is 🔒. The surface of §5 was implemented **without touching `api.md`** (there was no
finding): S04's `suspend` bridge (ADR-011) sufficed for all the primitives.
The implementation decisions —none extends the API, all concretize
semantics that §5 leaves to the kernel's discretion— are recorded here.

### The ⏸ IO pattern over `suspend` (the template for S15/S16 and Phase 4)

Every ⏸ `fs` primitive has the same shape:

```
vals := rt.sched.suspend(L, func() deliverFn {
    // BACKGROUND GOROUTINE: blocking IO in Go, outside the token, NEVER touches Lua.
    res, err := os.BlockingAlgorithm(...)
    return func(L *lua.LState) []lua.LValue {
        // With the token ALREADY recovered: safe to touch Lua HERE.
        if err != nil { mapFsError(L, err); return nil }
        return []lua.LValue{ /* Go values → LValue */ }
    }
})
return pushAll(L, vals)
```

The rule that shields the 🔒 "zero data races" invariant of S04: the background
goroutine captures **only Go data** (a `path` string, the bytes read, the raw
error) and **neither builds nor touches any `LValue`**; the OS error is stored as-is
and translated into the §1.4 table **inside the `deliverFn`**, which runs with the
token recovered —because `raiseError`/`L.NewTable` touch Lua—. While the background
goroutine works, the task is blocked without the token, so the loop
doesn't freeze (other tasks make progress). **S15 (`fs.watch`), S16 (`enu.proc`) and all
of the network (Phase 4) reuse this template verbatim**; that's why it is documented as
a pattern rather than an `fs` detail.

Common guard `requireTask(L, name)`: the ⏸ functions require being in a task (`L != host`,
like `cleanup`/`await`/`reload`); outside → actionable `EINVAL`. `cwd` is the **only
exception**: it is not ⏸ (a pure query), so it carries NO guard and works also
in the `-e` chunk.

### Mapping OS errors → §1.4 codes (`mapFsError`)

A single point translates the errno: `errors.Is(err, os.ErrNotExist)` → `ENOENT`,
`os.ErrExist` → `EEXIST`, `os.ErrPermission` → `EACCES`, anything else → `EIO`.
`errors.Is` is used (not direct comparison) because the Go stdlib wraps errnos
in `*os.PathError`; `errors.Is` unwraps them. `EINVAL` is emitted by usage
guards (outside a task), not by `mapFsError`. The message keeps Go's error text
(path included) as an actionable clue; the error is never swallowed.

### Atomic write: temp file in the SAME dir + rename

Normal `write` writes to `.nu-fs-*.tmp` **in the destination directory** (not `/tmp`)
and does `os.Rename`. The temp file goes in the same dir so the rename is
**same-filesystem** and therefore atomic —a rename across different filesystems
is not atomic (and `os.Rename` doesn't even work)—. Guarantee: a concurrent
reader sees either the old content or the new one **whole**, never a half-written file.
A `defer` deletes the temp file if it returns with an error before the rename (no residue left,
covered by a test); after a successful rename the temp file no longer exists under
that name, so the deferred `Remove` is a no-op. `Chmod` 0644 is applied to the
temp file because `os.CreateTemp` creates it as 0600 and a `write` must produce a file
with normal permissions.

### G17 — `write{exclusive=true}` is `O_EXCL`, no temp+rename

The exclusive branch does NOT use temp+rename: the rename would **overwrite** an
existing file, breaking the exclusion. `O_WRONLY|O_CREATE|O_EXCL` is used, which is the
OS primitive that creates **only if it doesn't exist** in an indivisible operation and
fails with `os.ErrExist` (→ `EEXIST`) if it already exists. This is the piece behind
sessions' lockfiles (sesiones.md §6): lock creation must be atomic and fail if another
process already holds it. `append` uses `O_APPEND` (not atomic like `write` —an append
is incremental by nature, for logs/JSONL—; the OS's `O_APPEND` guarantees that
each write goes to the end).

### `stat` on a nonexistent path → `nil`, doesn't throw (the asymmetry with `read`/`list`)

`stat` is the "does it exist and what is it?" query, not a read that fails: a nonexistent
file returns **`nil` without throwing** (§5). Any OTHER error (permission on
a path component, IO) is thrown. It deliberately contrasts with `read` and
`list`, which on a nonexistent path DO throw `ENOENT` —reading/listing what doesn't
exist is a failure, not a valid answer—. `mtime_ms` is given in milliseconds
(`ModTime().UnixMilli()`, consistent with §1.5: the core's times are in ms);
`mode` are the Unix permission bits (`Mode().Perm()`).

### `mkdir` creates parents (`MkdirAll`)

`mkdir` uses `os.MkdirAll`: it creates any **missing parents** and is **idempotent** if
the directory already exists. This is the expected behavior of a terminal
tool (`mkdir -p`): nobody wants to chain mkdirs to create `a/b/c`, nor should it
fail because the directory was already there. If the path exists but is a **file**,
`MkdirAll` fails (a file is not overwritten with a directory). The alternative
(`os.Mkdir`, single level, fails if it already exists) was discarded for ergonomics: §5 does not
require single-level, and a plugin that wants to create a hierarchy shouldn't have to
walk it by hand.

### `remove`: recursive mandatory for a non-empty dir, nonexistent = no-op

Removing a file or an **empty** directory works without further ado. A **non-
empty** directory requires `opts.recursive=true` —without it, `os.Remove` fails and is surfaced as
`EIO`—: it is the safeguard against an accidental `rm -rf`; removing an entire tree
must be explicit. With `recursive=true`, `os.RemoveAll` is used. **Nonexistent is
a no-op** (does not throw `ENOENT`): removing what is already gone leaves the system in the
desired state (the resource does not exist), which is exactly what the call asked for —idempotent
semantics, consistent with `mkdir`—. `RemoveAll` is already a no-op on nonexistent; in
the non-recursive branch, `ErrNotExist` is swallowed explicitly.

### `copy` is files-only, streamed

`copy` uses `io.Copy` (streaming, without loading an entire large file into RAM) and
covers **files only**: copying a directory recursively is higher-level work
(Lua on top of `list`+`copy`), not a core primitive —the core provides the building block,
composition is up to the extension author—. It opens the source first so its
nonexistence/permission issue is the error the user expects to see, and only then creates the
destination (`O_TRUNC`: overwrites).

### `tmpdir` is session-owned, lazy and reused; `cwd` is immutable

`tmpdir` creates **one** temp directory per session (`os.MkdirTemp` under
`os.TempDir()`), **lazily** the first time and **reused** afterward
(cached in `rt.fs.tmpdir`). The creation runs in the background goroutine (it is IO),
so the field is protected by a lock in `fsState` —two concurrent `tmpdir` calls must not
create two directories nor race on the field, and the lock does not depend on
the token (the background goroutine doesn't have it)—. `Runtime.Close` deletes it
recursively (`closeTmpdir`): the scratch space does not survive the process. `cwd` is the
only NON-⏸ function in `fs`: a pure query (`os.Getwd`), [W], **immutable**
for the duration of the session —there is no `chdir`, because changing the process's cwd would be
a global effect that would break per-task isolation (ADR-008); a subprocess that
wants a different dir gets it via `opts.cwd` (§6), without touching the process's cwd—.

### Lua's `io`/`os` are not used

All IO is pure Go (Go stdlib's `os`/`io`). The sandbox baseline (S01,
§1.2) deliberately left out `io` and trimmed Lua's `os`; `enu.fs` is the
**controlled** IO surface that replaces them, with structured errors, ⏸ over the loop
and code mapping. A plugin never touches the filesystem through Lua's `os`
back door.

## S15 — `enu.fs.watch` (api.md §5, §16)

### `watch` is NOT ⏸ and is main-state only (§16)

Unlike the rest of `enu.fs` (all ⏸), `watch` **does not suspend**: it arms the
watcher and returns the `Watcher` on the spot. And it is **main-state only**
(§16): the handler is **synchronous** (like `every`/`on`), runs on the main
state's loop; the delivery bus (token + ephemeral thread) lives there. That's why `watch` is
not "wait for a result" but "register an observer that fires later", which
is exactly what does NOT fit ⏸.

**Correction (host-only guard removed):** "main-state only" (§16) means
**"not in workers"** —where `fs.watch` isn't even registered (S34)—, **not** "not in
tasks". Tasks run on the main state's event loop and share the global `enu`,
so `watch` is callable indifferently from the chunk, a synchronous
handler, `init.lua` **or from within a task**, exactly like its
siblings `every`/`on` (which also don't distinguish host from task): it registers the `Watcher`
synchronously and returns without suspending. The `if L != rt.L { EINVAL }` guard
was removed from `fsWatch` —it was a deviation from §16— and the accomplice test `TestWatchOutsideMainState`
was rewritten as `TestWatchFromTaskWorks` (verifies that a `watch` started inside
a task works and delivers at least one batch after a file change). Blocking
in workers already guarantees that `fs.watch` isn't registered in their LState (S34), with
no need for any guard.

### Debounce + batching is OUR logic, not fsnotify's (G7)

fsnotify forwards each OS event one by one. **Batch coalescing** is done
by us: the background goroutine accumulates events in a buffer and arms (or
re-arms) a `time.Timer` of `debounce_ms`; when that time passes **without new
events**, it dumps the ENTIRE buffer as **a single** `fn(events[])`. The debounce is
**trailing and coalescing** (each event resets the clock), so a continuous
burst —a `git checkout` that touches thousands of files— still gets grouped and comes out
as ONE batch, not N calls (S15's acceptance criterion, G7). `debounce_ms`
defaults to 50 (§5); negative → `EINVAL`. The timer reset uses the standard pattern
(`Stop` + drain `C` if it already fired) so as not to leave a stale fire on the channel.

### Gitignore filtering (G7): both when adding AND when filtering events

`gitignore = true` (default per §5) parses the observed root's `.gitignore`
(`github.com/sabhiram/go-gitignore`, pure-Go: `CompileIgnoreFile` + `MatchesPath`).
Filtering happens in **two places**: (1) when **adding** subdirectories in
recursive mode, ignored ones are skipped (`node_modules/` is NOT WATCHED: it would waste
file descriptors and produce noise); (2) when **classifying** each event, an ignored path is
discarded before entering the buffer —it neither reaches the handler nor counts toward debounce—.
A missing `.gitignore` is not an error (nothing is ignored via that path). The internal
`.git/` is **always** ignored (universal noise of a repo), by checking for any
`.git` path component. Library decision: go-gitignore is simple,
pure-Go and correct for the acceptance criterion (basename, `*.log` glob, `build/` dir);
parsing `.gitignore` by hand would reinvent it worse.

### Scope of `recursive`

fsnotify does NOT recurse: it watches specific directories. With `recursive = true`, the
subtree is **walked** at startup (`filepath.WalkDir`) adding each non-ignored subdirectory
(`SkipDir` on ignored ones, so as not to descend into `node_modules/`); and
a directory **created on the fly** is added to the watcher upon seeing its `create` event (if
it is a dir and not ignored), so that changes under it are reported too. The documented
scope: recursion is **rebuilt by observing directory
creations**; a lone file (`path` not a dir) is watched through its
parent directory, filtered in `classify` for events that don't concern it. Errors
while walking a specific entry are best-effort (skipped, doesn't break the watch).

### Delivery under the token; quiescence like `every`; integration with the handle registry

The background goroutine **never touches Lua**: it filters and accumulates Go data; to
deliver the batch it calls `deliverBatch`, which **takes the token** (like `runSyncHandler` in
timers.go) and runs the handler in an ephemeral thread of the main state under `pcall`
per boundary (ADR-008) —zero data races: paths cross as copied `string`s and
the handler is invoked with the token taken—. An active `Watcher` does **not**
count toward quiescence (it doesn't touch `pending`), just like an `every`: a watcher never finishes and
would hang `enu -e`. `Watcher` implements `ownedHandle` (handles.go, S13): `watch`
tags it with `currentOwner()` and `track`s it; `Watcher:stop()` `untrack`s it (no
leak in the registry) and `enu.plugin.reload` releases it via `release()` —"reload doesn't leave
orphaned handlers" (G2)—. `stop` (and `Runtime.Close` via `stopAllWatchers`) cuts the
goroutine (`stopCh`, idempotent via `stopOnce`) and closes the OS watcher (`fsw.
Close`, freeing descriptors), with no goroutine leak. `deliverBatch` watches
`stopCh` while waiting for the token: after `stop`, no more batches (the contract of `stop`).

### Added deps (pure-Go, `CGO_ENABLED=0` intact)

`github.com/fsnotify/fsnotify` (pure-Go filewatching; its only indirect dep is
`golang.org/x/sys`, also pure-Go) and `github.com/sabhiram/go-gitignore` (`.gitignore`
parsing). Neither uses cgo: the static binary (ADR-001) still compiles with
`CGO_ENABLED=0`.

## S16 — `enu.proc` (api.md §6)

### The cause of the previous attempt's hang, and its fix (the core of this session)

The previous S16 attempt wrote correct `proc.go`/`proc_test.go` but **hung
while running the tests** and never got committed. The cause was NOT in `enu.proc` but
in a **crack in S08's cancellation unwinding** that §6's canonical idiom
(`spawn` + `enu.task.cleanup(function() p:kill() end)`) was the first to
expose.

The mechanism: a `cleanup` almost always **captures a task-local via upvalue**
—here, `proc`—. While the task is running, that upvalue is **open**: it points to a
slot in the registry of the task's thread `co`, not to a copy. On a normal return,
gopher-lua **closes** the upvalues (copies the value inside the `Upvalue`) upon exiting
scope. But our cancellation abort (S07/S08) is a **Go panic**
(`abortSignal`) that unwinds the Go stack **without** performing that Lua
closing, and the `PCall` that recovers the panic in `runTask` **resets `co`'s
registry** (sets those slots to `nil`). Result: when `runCleanups` later executes
`p:kill()`, its upvalue reads an already-`nil` slot → the `kill` operates on `nil` and, caught by the
per-boundary `pcall` of each cleanup (ADR-008), is swallowed into the log without killing anything. The
subprocess (`sleep 30`) stays **alive**, and the test that waits for its death hangs
(in the previous attempt, without `-timeout`, a hard hang; with the rewritten deterministic
suite, a `waitDead` failure at 5s).

**Fix (scheduler.go, `abort`):** **before** launching `panic(abortSignal)`,
we close `co`'s open upvalues with `closeOpenUpvalues(t.co)`. gopher-lua does not
expose `closeAllUpvalues` directly, but its `LState.Error(lv, level)` with a
**non-string** value runs `closeAllUpvalues()` before panicking (verified in its
source, `_state.go`): we exploit that effect by passing a sentinel table and
wrapping the call in a `recover()` that swallows its panic —that panic is only
the **vehicle** for the closing; the actual abort is carried by the subsequent
`panic(abortSignal)`—. This way the captured values survive the registry reset and the
`cleanup`s see them intact. `runCleanups` was already running on ephemeral threads of
`host` (not on `co`), so the already-closed upvalues are exactly what those
cleanups need.

**Why it is NOT a `G##` finding:** it changes no signature or semantics of
`api.md` —the contract in §3/§1.3 always said a `cleanup` runs on cancellation and
that the idiom is `cleanup(function() proc:kill() end)`; what was broken was an
**internal invariant** of S08's unwinding (a cleanup must see the values it
captured). It is an implementation fix, not a spec fix; that's why it is fixed in
the code without going through `problemas.md`. Verified surgically: disabling
`closeOpenUpvalues` makes **only** `TestSpawnKilledByCleanupOnCancel` fail, and
re-enabling it leaves **all** of S08's suite (cancel/cleanup/watchdog) green —the
upvalue closing does not alter any other cancellation semantics—.

### No implicit shell (§6): a structural security decision

`argv` is an **array**: `exec.Command(argv[0], argv[1:]...)` passes the arguments to
the OS without interpretation. Nobody invokes `/bin/sh`, so `run(["echo","$HOME"])`
prints the literal `$HOME`. Whoever wants a shell puts it explicitly
(`["sh","-c","..."]`). Shell injection does not exist if there is no shell.

### Process lifecycle model (the 🔒 logic, §6)

The **primary** way is to kill it via `enu.task.cleanup` in whoever created it: when the
task ends —success, error or cancellation (S08)— the process dies with it. Two **safety
nets**, not the primary path: (1) the **GC finalizer**
(`runtime.SetFinalizer`) kills a `Proc` that lost all Lua references —**non-
deterministic**, not relied upon—; (2) `Runtime.Close`→`stopAllProcs` kills
all living ones when the session closes (scheduler `procs`/`trackProc`, sibling of
`watchers`/`timers`). Like `every`/`watch`, **a live `Proc` does not count toward
quiescence**: waiting for a subprocess to die for `enu -e` to return would
hang it. `*luaProc` implements `ownedHandle` (S13): `reload` kills the processes of
the plugin being reloaded.

### Lock allocation: `kill` with its own lock, never during IO

A `Proc`'s IO (write/read/wait) **blocks** in background goroutines (without the
token); `kill` must be able to **interrupt** that IO —the lifecycle pattern is "the cleanup
kills the hung process so its pending `read`/`wait` unblocks"—. If
`kill` and IO shared a lock, killing a process being read from would
**deadlock** (the lock would be held by the blocked read; kill would wait on a lock that
only gets released when the process dies, which is what kill is trying to do). That's why `kill`
uses its **own** `killMu`, never taken during a blocking operation: closing/
signaling the process is precisely what **unblocks** the read/wait. `wait` is memoized with
`sync.Once` + a `chan` (not a `Mutex`) precisely to avoid holding a lock
during the blocking wait.

### Manual pipes (`os.Pipe`), not `cmd.StdoutPipe`

`cmd.StdoutPipe`/`StderrPipe` close the **read** end as soon as
`cmd.Wait` sees the process exit (documented by os/exec), which would lose data if
we reap as soon as the process dies. With our own pipes, the read end is
**ours**: `Wait` doesn't touch it; we close it when tearing down the `Proc`. This way, reaping
(`go p.wait()`, which collects the zombie without which `alive` would report it alive
forever) and streaming remain **decoupled**. For stdin, `StdinPipe` is fine (it is for
writing; `close_stdin` closes it by hand, signaling EOF).

### `alive` (G17): existence, not identity

`enu.proc.alive(pid)` uses the "signal 0" trick (`kill(pid, 0)`): no error or `EPERM` (exists
but belongs to another user) → alive; `ESRCH` → dead; `pid <= 0` → not alive. It reports
**existence, not identity**: a pid recycled by the OS returns `true` even if it is
another process. This is deliberate —to detect orphaned session locks (sesiones.md
§6) it is enough to know whether "someone" has that pid; identity is given by the
lock's content (hostname, §7), not by this call—. It is not ⏸ (immediate query).

### `run`: the exit code is data, not an error

A `code != 0` **does not throw** (a `grep` with no matches exits with 1 and that's
information, like `enu.http`'s `status`). What DOES throw: a failed launch
(`ENOENT`/`EACCES`/`EIO`) or exceeding `timeout_ms` (kills with SIGKILL, **drains**
the `Wait` of the dead process so as not to leak its goroutine/pipes, and throws `ETIMEOUT`).
`env` present (even if empty) **replaces** the inherited environment; absent, it inherits it.

## S17 — `enu.sys` (api.md §7)

Environment and clock. Thin wrappers over the stdlib (`platform`/`now_ms`/`mono_ms`/
`hostname`); the only proprietary logic is the **`setenv` overlay** and its precedence
when launching subprocesses. No function is ⏸ (they are immediate queries/registrations);
all are [W] (§16, today in the main state: workers are S34). No findings:
§7 and the `procOpts` left by S16 sufficed; APILevel stays at 1 (§7 was already
there).

**`setenv` does NOT mutate the current `enu` process's environment (the central decision, §7).**
No `os.Setenv`: mutating the process's global environment is a shared effect
that (a) would break per-task isolation (ADR-008) —it would be visible from ALL
code, not just from whoever requested it— and (b) would contradict the contract ("affects only
future subprocesses"). Instead, `setenv` writes to an **overlay** of the
Runtime (`sysState.envOver map[string]string`) that `enu.proc` applies when building
the child's environment. The acceptance criterion ("`setenv` is visible in a subprocess
launched afterward, not in the current one") is met by construction: the current
`enu` never changes its environment; only the child ever sees the variable.

**A lock, not the token, for the overlay.** `setenv` writes to the map in the main
state under the token, but it is **read by `enu.proc`'s background goroutines**
(which build the child's environment WITHOUT the token, outside the ⏸ bridge). That's why the
overlay carries its own `sync.Mutex` —that's what avoids the data race `-race`
would catch—, not the token. To avoid sharing the live map with those
goroutines, `envOverlay()` returns a **copy** (negligible cost: few entries).

**A snapshot of the overlay is taken at `run`/`spawn` entry.** Both do
`opts.envOver = rt.sys.envOverlay()` right after `parseProcArgs`, in the main state under the
token. This deterministically fixes which `setenv` each subprocess sees: the ones that
happened BEFORE the call (not the ones after). Since the Lua call happens-before the
background goroutine, any prior `setenv` is visible.

**Child environment precedence (the S16↔S17 integration, `mergedEnv`).** From
lowest to highest: **OS-inherited environment < `setenv` overlay <
explicit `opts.env` of the call**. Reasoning:

- The overlay **overrides what's inherited**: that's the whole point of `setenv` (changing
  what the child sees relative to the process's environment).
- Explicit `opts.env` is **full per-call control** (§6, already decided in S16):
  the more local one wins. Whoever passes `env` in THAT invocation decides those keys over
  the overlay —e.g. to ISOLATE a subprocess from a previous `setenv`—. And,
  consistent with S16, `opts.env` **replaces** the inherited environment (it is part of
  `opts.env`, not of `os.Environ`); with `opts.env` present, the overlay is **not
  applied** (the explicit layer wins entirely).
- Discarded alternative: layering `opts.env` *on top of* (OS + overlay) instead of
  replacing. Discarded for consistency with S16 (`opts.env` already meant
  "full control / replaces inherited"); changing that would have been a silent
  regression of that semantics.

Implementation detail: `opts.env != nil` (even being `[]string{}`) marks
"explicit" —`parseProcArgs` sets a non-nil `[]string{}` when there's an `env` table—. Without
overlay or `opts.env`, `mergedEnv` returns `nil` (the common case: `exec.Cmd`
inherits `os.Environ()` as-is, with no copy cost). `splitEnv` splits "K=V" on
the FIRST `=` (a value can contain `=`). **A single entry per
key** is kept (key→position index) for a clean and deterministic environment.

**`platform`** returns raw `runtime.GOOS`: for supported OSes it is
"linux"/"darwin"/"windows" (what §7 enumerates); for any other, the literal
`GOOS` value —more honest than inventing an enum value—. **`now_ms`** is the wall
clock (`time.Now().UnixMilli`, can jump backward). **`mono_ms`** is
monotonic since `monoOrigin` (fixed when the package loads, `time.Since`): an
arbitrary origin, only the differences between readings are a reliable duration.
**`hostname`** is `os.Hostname`; an OS failure (rare) → `EIO` instead of
inventing a name.

**Tests (`sys_test.go`).** The overlay and its precedence (proprietary logic) carry a
Go test: table-driven `mergedEnv` (overlay overrides the OS / adds a new key
keeping what's inherited / `opts.env` wins over the overlay / `opts.env` replaces what's
inherited / `opts.env={}` → empty environment) and `splitEnv`. The **acceptance criterion**
runs end-to-end through the real ⏸ bridge: a task does
`setenv("NU_TEST_X","42")` + `proc.run(["printenv","NU_TEST_X"])` and a future
publishes the outcome; another task waits on it and asserts stdout=="42\n"/code==0
(`printenv` is coreutils and is invoked WITHOUT a shell, so it also exercises S16's
lack of a shell). The "not in the current one" is checked in Go with
`os.LookupEnv` (still empty after the snippet). The rest (`platform`/`env`/
`now_ms`/non-decreasing `mono_ms`/`hostname`/usage from a task) with a Lua snippet,
as the policy demands for glue over the stdlib. `CGO_ENABLED=1 go test -race
-timeout 120s -count=2 ./internal/...` green.

## S18 — codecs `enu.json` / `enu.toml` / `enu.yaml` (api.md §12)

Three `encode`/`decode` pairs (`codecs.go`), **none ⏸** (pure CPU: they parse or
serialize a string already in memory, no IO to wait on) and all **[W]** (§16;
today in the main state, workers are S34). "Lua decides, Go executes"
(ADR-004): parsing/serialization is Go (stdlib `encoding/json`, BurntSushi/toml
—the same one from S11—, `gopkg.in/yaml.v3`), YAML in particular being "too
treacherous for pure Lua" (§12). **APILevel stays at 1** (§12 was already in
api.md; it is being implemented, not extending the sacred surface).

### The Lua↔Go mapping (shared by all three formats)

The bridge is an intermediate Go value (`interface{}` with
`map[string]interface{}`/`[]interface{}`/`float64`/`string`/`bool`/`nil`) that all
three libraries know how to serialize (`luaToGo`/`goToLua`):

- **`nil` (Lua) → null.** In `decode`, a null → `nil` WOULD LOSE the key in a
  Lua table (`t.k = nil` deletes it), so JSON uses a **`NULL` sentinel** (see
  below). TOML/YAML map nil to Lua `nil` (`useNull=false`): null doesn't occur in their
  typical config shape, and whoever needs the null round-trip uses JSON.
- **boolean → bool; number → float64.** Lua doesn't distinguish int from float; the Go
  side emits an integer when there's no fractional part. JSON `decode` uses `UseNumber` so as not to
  degrade large integers to scientific notation on the round-trip.
- **string → string, with STRICT UTF-8 (G11):** see below.
- **table → array vs object:** a table whose keys are **exactly 1..n
  contiguous** (Lua's sequence convention) → **array**; anything else →
  **object** (keys as string, via `luaKeyToString`: number 1.0 → "1"). The
  detection counts the sequence length and the total number of keys; it's only
  an array if they match and there's at least one. Non-scalar keys (table, function) →
  `EINVAL`.

**Empty table → object (`{}`)** (the ambiguous decision of §12). An empty table
could be `[]` or `{}`; `{}` is chosen because the vast majority of
this project's config tables are maps and an empty list is the rare case.
Documented here and in `codecs.go`'s header. Whoever needs exactly `[]` treats it
as data (a non-empty array is detected unambiguously).

### Strict UTF-8 (G11) — the 🔒 half of S18

`encoding/json` silently **replaces** invalid UTF-8 bytes with U+FFFD.
The contract (§12) demands the opposite: `encode` **throws `EINVAL`** on
invalid bytes —sanitizing is a visible decision made by whoever has the context (the tool),
never the codec's—. Detected with `utf8.ValidString` in `luaToGo` (values) **and in
object keys** (an invalid string-key breaks the document just the same). Applies
to all three formats when encoding. A non-finite number (NaN/Inf) is also rejected,
having no representation in JSON/TOML/YAML.

### The `enu.json.NULL` sentinel — the other 🔒 half

A **single userdata** per Runtime (`rt.jsonNull`, created once in
`registerCodecs`), recognized by **identity**. `decode` delivers the sentinel in
place of `null` (NOT `nil`, which when assigned to a table deletes the key: a round
trip would lose null-valued keys); `encode` recognizes it and emits `null`. It is
the canonical pattern for "null that survives the round-trip." The test explicitly contrasts
it with `{ a = nil, b = 1 }`, which DOES lose the `a` key — exactly what
the sentinel avoids.

### Serialization details

- **JSON `SetEscapeHTML(false):`** by default `encoding/json` escapes `<`/`>`/`&`
  (a defense for embedding in HTML); in a general-purpose codec that's
  surprising (a round-trip would change the text), so it's disabled —whoever
  embeds in HTML escapes it themselves, consistent with sanitizing being the consumer's job (§12)—.
  The trailing `\n` that `json.Encoder.Encode` adds is trimmed.
- **`opts.pretty`** → `SetIndent("", "  ")` (two spaces).
- **TOML root:** a TOML document is a map; `encode` requires the root to be
  an object (array/scalar → actionable `EINVAL`).
- **Parse errors** (`decode` of invalid JSON/TOML/YAML) → `EINVAL` with the
  library's own text (BurntSushi and yaml.v3 include line/column).

### Added deps

`gopkg.in/yaml.v3` v3.0.1 (pure-Go; `go get` + `go mod tidy`, needed network access). Does not touch
`CGO_ENABLED=0`. BurntSushi/toml was already there (S11); `encoding/json` is stdlib.

### CP-4 — `search.files` → `fs.list` adaptation (closes Phase 3)

CP-4's text ("a real tool, only with primitives") mentions
`enu.search.files` to walk the repo, but **that primitive is S27 (Phase 5) and
doesn't exist yet**. It is substituted by a **recursive walk in Lua over
`enu.fs.list`** (available since S14): enumerate the directory + recurse over
subdirectories (skipping `.git`, as `search.files`'s gitignore filtering would). It is
the most faithful substitute —the same work (walking the tree) with the
primitive that DOES exist in Phase 3—; `search.files` (recursion +
filtering in Go) arrives in S27/CP-6. The test (`cp4_test.go`) sets up a temporary
git repo (one committed file + one untracked + one subdir), walks it with
`fs.list` recursively, reads with `fs.read`, launches `git status --porcelain` with
`proc.run` (`opts.cwd`, no shell), and emits a summary with `json.encode` which is then
re-parsed with `json.decode` to validate it (closing the codec loop). **No
network or UI, only core primitives** → exercises the completeness corollary
(philosophy §2): no new primitive was needed, so **no `G##` finding**. If `git`
is not present, the test is skipped (it needs it for `git status`).

### 🔒 Tests (`codecs_test.go`, naming G11)

Strict UTF-8 G11 (loose 0xff byte, nested, and as an object key → `EINVAL`;
exact ASCII+multibyte+emoji round-trip); NULL sentinel round-trip (key `a`
present as `enu.json.NULL`, distinct from nil, iterated with `pairs`; `encode` →
`"a":null`; round-trip; contrast with `nil`, which loses the key); array vs object
and empty table → `{}`; `pretty` indents and is valid JSON; invalid `decode` →
`EINVAL`; `toml.decode` of a real `plugin.toml` (name/version/requires) +
round-trip + non-object root → `EINVAL`; skill YAML frontmatter (keys, lists,
strings) + round-trip; codecs from a task ([W]); non-serializable (function,
NaN, Inf) → `EINVAL`. `CGO_ENABLED=0 go build`/`go vet`/`gofmt -l` clean;
`CGO_ENABLED=1 go test -race -timeout 120s -count=2 ./internal/...` green. The `enu -e`
binary confirms end-to-end encode/decode of all three formats, the
NULL sentinel round-trip, `pretty` and strict UTF-8 (G11 → `EINVAL`).

**No findings:** §12 sufficed as-is. **CP-4 green → Phase 3 closed.**

## S19 — `enu.http.request` (api.md §8)

First session of **Phase 4 (Network)**. Implements **only** `enu.http.request(opts)
-> {status, headers, body}` ⏸ (§8): a **buffered** HTTP request over
S04's `suspend` bridge (ADR-011), the same pattern as `enu.fs`/`enu.proc` —the IO
(the request) runs in the background goroutine, which **never touches Lua**; the response or
the error crosses over to Lua only in the `deliverFn`, under the recovered token—. `stream`
(S20) and `ws` (S21) are deliberately left out. APILevel stays at 1 (§8 was already
in api.md). **No `G##` findings:** §8 sufficed as-is.

### The status is DATA, not an error (§8's key semantics)

A 404 or a 500 return `{status=404, ...}` **without throwing** —the status code
is information the caller decides how to handle (a provider adapter
distinguishes 429 from 500 to retry, ADR-005)—. Only **transport** failures
throw: connection refused / DNS / reset → `ENET`; `timeout_ms` expiring →
`ETIMEOUT`; missing/invalid `url` and other misuse → `EINVAL`. This inverts the
default of many HTTP clients (which throw on 4xx/5xx) and is deliberate: the
status belongs to the extension's logic, not to the transport.

### The client model: reusable vs per-request (the design decision)

**A reusable `*http.Client` for the common case, an ephemeral one per-request for
cases with custom TLS/proxy.** The common case (no `opts.tls`, no
`opts.proxy`, no CA/proxy from `[net]`) reuses a single client cached in
`httpState` (created lazily, with a lock for the race between background goroutines):
this way the **keep-alive connection pool** between requests is leveraged,
which is what makes repeatedly talking to the same endpoint efficient (the agent's
case: many calls to the same provider). A request that requests a different
CA, `insecure`, or its own proxy needs its own `tls.Config`/`Transport`,
so it builds an **ephemeral client just for it**; ephemeral clients are not cached (they are
the exception, and caching them by option combination would add
complexity with no clear benefit in v1). The deadline is NOT enforced via `client.Timeout` but
via a per-request `context.WithTimeout`: this way `ctx.Err()` cleanly distinguishes
the timeout (`ETIMEOUT`) from the rest of transport failures (`ENET`).

### TLS and proxy (G12)

`opts.tls = {ca_file?, insecure?}`: `ca_file` adds a corporate CA **to the
system root** (part of `x509.SystemCertPool`, appending the PEM to it —"adding a CA",
not replacing trust—); `insecure=true` disables verification (test
environments, knowingly exposed). `opts.proxy` sets a proxy per request; without it,
`http.ProxyFromEnvironment` respects the environment's `HTTP_PROXY`/`HTTPS_PROXY`/`NO_PROXY`.
**Global defaults in `enu.toml`'s `[net]`** (`ca_file`, `proxy`),
overridable per request —the precedence is: per-request option > `[net]` >
environment/system—. The `[net]` config is read in `New` (config_toml.go) and passed to
`httpState`; a malformed `enu.toml` doesn't apply it (its error is deferred to `Boot`,
like the rest of the config).

### Response headers with multiple values → join with ", "

`http.Header` is name→[]value (the protocol model allows repeated
headers); the contract (§8) asks for a name→value table. **Decision: join
repeated values with `", "`** —the canonical way to combine headers per RFC
7230 §3.2.2, valid for almost all of them—. The notable exception is `Set-Cookie` (not
split/joined by commas); a consumer that needs raw cookies doesn't have a buffered
API that serves them well (they'll use `stream` when it lands, S20, or this path
doesn't serve them). It is predictable and reversible for the common case (a single value passes
through intact) and avoids exposing arrays where almost all code expects a string.

### `opts` validation (to `EINVAL`, before suspending)

Non-table `opts`, missing/empty `url`, non-positive or wrong-typed `timeout_ms`,
wrong-typed `headers`/`tls` → `EINVAL` thrown in the main state (under the
token), **before** suspending. Fine-grained URL validation
(syntax, scheme) is delegated to `http.NewRequestWithContext` in the background
goroutine; an error from it is also surfaced as `EINVAL` (invalid usage). Missing `timeout_ms`
→ a default ceiling of 30s (a network request with no deadline could
hang a task forever); an explicit `0` is treated as invalid (the contract does not
define it as "infinite").

### Tests (`http_test.go`, hermetic with `net/http/httptest`)

All against **local** servers (`httptest`), **no external network** → not flaky
due to DNS or remote endpoints: 200 with body + correct request/response
headers; 404 and 500 **don't throw** (status as data); POST with body received on the
server; **transport failure** (closed server → closed port) → `ENET`;
**timeout** (server that sleeps >> `timeout_ms`, with a `release` channel that
unblocks it at test end, no hanging goroutines) → `ETIMEOUT`; missing/empty
`url`/non-table `opts` → `EINVAL`; negative/non-numeric `timeout_ms` →
`EINVAL`; `request` outside a task → `EINVAL` (it's ⏸); **TLS G12** against
`httptest.NewTLSServer`: without `insecure` it fails (unknown CA → `ENET`), with
`insecure=true` it passes, and with the server's CA as `ca_file` it passes **without** `insecure`;
multiple headers joined by ", "; 5 concurrent requests progress in parallel
(anti-data-race check for the reusable client). `CGO_ENABLED=0 go build`/`go
vet`/`gofmt -l` clean; `CGO_ENABLED=1 go test -race -timeout 120s -count=2
./internal/...` green, no flakiness. The `enu -e` binary confirms end-to-end:
GET → status=200, 404 → doesn't throw (status=404), closed port → `ENET`, empty
`url` → `EINVAL`.

### What S20 (stream) and S21 (ws) reuse

S20 will reuse the client model (reusable vs per-request), the parsing of
`opts` TLS/proxy (G12) and the transport error mapping (`classifyTransportError`,
`httpError` with its core code already decided outside the token) —what will change is that it
does NOT buffer the body: it returns a `Stream` upon receiving headers and exposes `chunks()`/
`events()` (SSE parser, 🔒) with bounded backpressure (→ `EIO`)—. S21 (ws) is
built over the same ⏸ bridge but with a websockets library; it shares the
`opts` parsing and the `ENET`/`ETIMEOUT` mapping.

**No findings:** §8 sufficed. Pointer ▶ advances to **S20**.

## S20 — `enu.http.stream` + SSE parser (api.md §8, 🔒)

`enu.http.stream` is the **streaming** HTTP response, the other face of
`enu.http.request` (S19, buffered). It returns a `Stream` **upon receiving the
headers** (`Stream.status`/`Stream.headers`), **without reading the body**; the body is
iterated chunk by chunk with `Stream:chunks()` (raw) or `Stream:events()` (built-in
SSE parser, the 🔒 logic). `stream.go` (handle + iterators + opening) and `sse.go`
(parser). Everything S19 left ready is reused as-is: `opts` parsing
(`parseReqOpts`), the reusable-vs-per-request client model (`clientFor`,
with TLS/proxy from G12) and the transport error mapping
(`classifyTransportError`/`httpError`, which already decide the core code outside the
token). The only thing that changes is body consumption.

### The ⏸ bridge and the background goroutine (no new model)

`stream` suspends until the headers; each `next` of `chunks()`/`events()`
suspends until the next chunk/event. A **single** background goroutine
(`readLoop`) reads the body in chunks and **never touches Lua**: it pushes the bytes into an
internal queue and the consumer pulls them out via the ⏸ bridge (the `deliverFn` builds the
string/event with the token recovered). It is the same invariant as S04/S14/S16.

### The incremental SSE parser (the 🔒 logic)

The requirement is that an event can arrive **split across several network
chunks** (TCP respects neither event nor line boundaries). The parser
(`sseParser`) assumes nothing about the splits: it accumulates bytes in a
buffer, extracts only **complete lines**, and keeps the rest for the next
chunk. The delicate case is a `\r` at the **end** of the buffer: it could be a
`\r\n` split across chunks, so it's treated as an incomplete line until we
know what follows it (at EOF, `flush` closes it out). An event is
**dispatched at the blank line**; a final event without its trailing blank
line is dispatched at EOF. It supports the three terminators (`\n`/`\r\n`/`\r`),
multiple `data:` **concatenated with `\n`** (no trailing `\n`), `event:`/`id:`,
`retry:`, and comments (leading `:`) ignored, plus the optional space after the
colon (exactly **one** is stripped). `event`/`id` carry a `has*` flag to
distinguish "absent" from "present but empty" — we don't invent
`event="message"`, which the spec leaves to the consumer.

### The bounded buffer and backpressure → `EIO`

The body is read into an internal queue protected by mutex+cond (NOT the
token: the producer never touches Lua) with a count of **pending bytes**
(`buffered`). If a new chunk would exceed `maxStreamBuffer` (8 MiB) because Lua
is consuming slower than the server is pushing, the stream **fails with
`EIO`** instead of growing unbounded — this is the §8 semantics: the buffer
has a cap, overflowing it is an error, not an infinite wait or a leak. A
**byte-based** cap (not a chunk-count cap) was chosen because that's what
bounds memory predictably, and it's deterministic (it doesn't depend on
timing: with enough volume it always overflows).

### The idle timeout → `ETIMEOUT` (and why `timeout_ms` doesn't cover the body)

An SSE can go **silent forever** without closing the connection, so a total
deadline would cut off a legitimate long-lived stream. That's why
`opts.timeout_ms` covers **only up to the headers** (a `time.AfterFunc` that
cancels the context if they don't arrive in time, and is stopped once they're
received), and the body is protected by `opts.idle_timeout_ms?`: a
`time.Timer` that **re-arms with each chunk** and, when it fires, cancels the
context — the silent `Read` returns and the call gives up with `ETIMEOUT`. A
cancellation due to idleness (`idleFired` → `ETIMEOUT`) is distinguished from
one caused by the user's `close()` (normal end, not an error).

### Close / cleanup / tracking (the stream's lifecycle)

`Stream:close()` cancels the context (unblocking the `Read`), closes the
body, stops the idle timer, and wakes up the consumers (who see `ECLOSED`). It
is **idempotent** (`closeOnce`) and synchronous (not ⏸). The lifecycle idiom
is that of §6: `enu.task.cleanup(function() st:close() end)` — when the task is
canceled/finished, the stream closes without leaking goroutines. As a safety
net, `Runtime.Close` closes all live streams (`stopAllStreams`, tracked in
`scheduler.streams`, a sibling of `procs`/`watchers`; a live stream does
**not** count toward quiescence). **Decision:** the `Stream` is NOT an
`ownedHandle` per owner (unlike `Proc`): a stream belongs to the **task
consuming it** (its lifetime is that of the IO turn), not the plugin, so it's
tied to `cleanup`, not to the `reload` registry. It's still tracked for
`Close`, though.

### `status` and `headers` as userdata fields

The contract requires `Stream.status`/`Stream.headers` as **fields** (not
methods) and `Stream:chunks/events/close` as methods. This is resolved with an
`__index` function that returns `status`/`headers` directly and delegates the
rest to the method table.

### Tests 🔒

`sse_test.go` (pure parser, no network or token): a table with simple/
multiline data, no space after `:`, event+data+id, comment ignored, several
events, `\r\n` and `\r`, event with no event field, empty data, retry ignored,
id present, final event without a trailing blank line. **Each case is run
against several partitions of the same `raw`** (all at once, **byte by
byte**, in chunks of 2/3/7 bytes) → this shields events split across chunks.
Plus two adversarial cases: a `\n\n` split EXACTLY between chunks and a
`\r\n` split between chunks (the `\r` at the end of one chunk, the `\n` at the
start of the next: if the `\r` were treated as a terminator, a spurious blank
line would appear). `stream_test.go` (e2e with `httptest` + `http.Flusher`,
hermetic): `events()` {event,data,id}, an event emitted across N writes
parsed as one, raw `chunks()` + nil at the end, status 404 doesn't throw,
**backpressure → `EIO`** (server dumps ~12 MiB, consumer sleeps 300 ms and
overflows), **idle-timeout → `ETIMEOUT`** (body silent past
`idle_timeout_ms`, with a `release` channel that unblocks it at test end,
no hanging goroutines), idempotent `close`, **close via cleanup on task
cancellation** (measures `NumGoroutine` to rule out leaks), `stream` outside a
task → `EINVAL`, invalid `idle_timeout_ms` → `EINVAL`. `CGO_ENABLED=0 go
build`/`go vet`/`gofmt -l` clean; `CGO_ENABLED=1 go test -race -timeout 120s
-count=2 ./internal/...` green, no flakiness (no regression across S01–S19).
The `enu -e` binary confirms it e2e: status=200 + an event split across
several writes parsed as one (`ping`/`hola mundo`) + an event with no `event`
field (data="fin").

**No findings:** §8 was sufficient. Pointer ▶ advances to **S21**.

## S21 — `enu.ws.connect` (api.md §8; closes Phase 4 — Network, CP-5)

Websockets: `enu.ws.connect(url, opts?) -> Ws` ⏸, `Ws:send(data)` ⏸,
`Ws:recv() -> string?` ⏸ (**`nil` on close**), `Ws:close()` (`ws.go`). It is
the full-duplex counterpart of `enu.http.stream` (S20) and the last piece of
Phase 4.

### The WebSocket library: `github.com/coder/websocket`

`github.com/coder/websocket` (the continuation of `nhooyr.io/websocket`) was
chosen over `github.com/gorilla/websocket`. Reasons:

- **Pure Go and with no transitive dependencies.** `go mod tidy` adds ONLY
  `coder/websocket` (nothing else gets pulled in), so `CGO_ENABLED=0` stays
  green (ADR-001) and the static binary doesn't bloat with a dependency tree.
- **`context.Context`-based API.** `Dial(ctx, url, opts)`,
  `conn.Read(ctx)`, `conn.Write(ctx, typ, p)`: the context is exactly what the
  ⏸ bridge and task cancellation need — canceling the context unblocks a
  hung `Read`/`Write`, which is how `close()` aborts background IO.
- **Serializes writes internally.** `gorilla/websocket` forces the caller to
  hold its own mutex to avoid interleaving writes; `coder/websocket` already
  handles this, which fits with `send` being able to run from its own
  `suspend`'s background goroutine without any extra coordination on our
  part.

### The IO model: NO permanent read goroutine (unlike S20)

S20's `Stream` needs a permanent background goroutine because an SSE body
**arrives whether anyone asks for it or not** (it has to be read so as not to
block the server and to apply backpressure). A websocket is different: it's
**consumer-driven request-response** — Lua calls `recv()` when it wants the
next message. So each `send`/`recv` performs its blocking `Write`/`Read`
**inside the background goroutine of ITS OWN `suspend`**, and there is no
background producer running between calls. It's the pattern of
`Proc:read_line`/`Proc:write` (S16), not that of `Stream`. Simpler and with
no queues: the only state shared between the background goroutine and
`close()` is the `closed` flag (under `mu`, not the token: the producer never
takes the token).

### `recv() -> nil on close`: distinguishing closure from a transport error

S21's acceptance criterion is "recv after close gives nil." `recv()` returns
the message, or **`nil` when the connection closes**: either cleanly (the
other end sent a normal close frame) or because we called `Ws:close()`. The
distinction "close → nil (end of stream)" vs "real failure → throws `ENET`"
is made by `websocket.CloseStatus(err)`: a close of `StatusNormalClosure`
(1000), `StatusGoingAway` (1001), or `StatusNoStatusRcvd` (1005, the other end
cut off without a code) is end of stream; any other read error is a
transport error. Also, if we ourselves closed it (the `closed` flag), the
`Read` aborted by our `cancel` is also end of stream, not an error.

A robustness detail (found by a test): after detecting an orderly close,
`recv` marks the handle closed (calls `close()`, idempotent). Without this, a
subsequent `recv()` would retry a `conn.Read` on an already-closed connection,
which returns a **different** error (not classifiable as a normal close) and
would surface as `ENET` instead of continuing to give `nil`. With the flag
set, every subsequent `recv` cuts short to `nil`.

### Connect: `timeout_ms` covers only the handshake

As with S20's `stream`, the handshake deadline must not cut short the
connection's lifetime (a websocket is long-lived). `dialWs` uses a
`context.WithCancel` for the connection (no deadline, canceled by `close()`)
and, on top of it, a `context.WithTimeout(connCtx, timeout)` ONLY for `Dial`,
which is discarded (`dialCancel`) once it returns. A handshake failure →
`ENET`; its timeout → `ETIMEOUT`, distinguished via `dialCtx.Err()` through
`classifyTransportError` (reused from S19). `send` sends **text** by default
(`MessageText`: the provider speaks JSON over text, ADR-005);
`SetReadLimit(32 MiB)` bounds a huge incoming message (the library's default,
32 KiB, is too small for a large provider turn).

### Close / cleanup / tracking (the websocket's lifecycle)

Identical to S20's `Stream`: `Ws:close()` is idempotent (`closeOnce`), sets
`closed`, sends the normal close frame (best-effort), and cancels the context
(unblocking any hung IO). The lifecycle idiom is
`enu.task.cleanup(function() w:close() end)`; the safety net is
`Runtime.Close` → `stopAllWs` (tracked in `scheduler.ws`, a sibling of
`scheduler.streams`). A live `Ws` does **not** count toward quiescence (the
other end may never close) and is NOT an `ownedHandle` per owner (its
lifetime is that of the IO turn, tied to `cleanup`, not to `reload`).

### Tests (`ws_test.go`, hermetic; CP-5 in `cp5_test.go`)

`enu.ws` is NOT in the 🔒 inventory (it's a wrapper over the library + the ⏸
bridge), but its own logic is equally shielded with **local** servers
(`net/http/httptest` + `websocket.Accept`): echo round-trip (several messages
in order), recv → nil after the server closes (and subsequent recvs keep
giving nil), recv → nil after a local `Ws:close()`, `send` after close →
`ECLOSED`, closed port → `ENET`, silent handshake (a TCP server that doesn't
respond) → `ETIMEOUT`, idempotent close, **close via cleanup on task
cancellation** (no goroutine leak, measured with `NumGoroutine`), outside a
task → `EINVAL`, bad `url`/`opts`/`headers`/`timeout_ms` → `EINVAL`.

**CP-5 (closes Phase 4)** exercises the four network capabilities together:
(a) `http.request` treats a 404 as data; (b) an SSE consumed via
`Stream:events()` **while another counter task keeps advancing** — it checks
`ticks > 0` while the SSE is being consumed, demonstrating that the event
loop does NOT block (the ⏸ bridge releases the token); (c) an echo ws
round-trip; (d) a slow consumer that overflows the buffer → `EIO` (S20's
backpressure).

`CGO_ENABLED=0 go build`/`go vet`/`gofmt -l` clean; `CGO_ENABLED=1 go test
-race -timeout 120s -count=2 ./internal/...` green, no flakiness (no
regression across S01–S20). The `enu -e` binary confirms it e2e against a
local echo server: `send`/`recv` round-trip (`hola`/`mundo`), recv after close
→ `nil`, closed port → `ENET`.

**No findings:** §8 was sufficient. **Phase 4 (Network) closed — CP-5 green.**
Pointer ▶ advances to **S22** (Phase 5 — Text and search).

## S22 — `enu.text` (width/wrap/truncate) + `Block` type + `enu.ui.block`/`caps`/`Style` (api.md §10, §9.2, 🔒)

Opens **Phase 5 (Text and search)**. A foundational session: the `Block` type
fixed here is the common currency built and consumed by S23 (markdown), S24
(highlight), S25 (diff), and S29 (blit/viewport), and `text.width` is the 🔒
logic that ALL layout computation (wrap, truncate, viewport clipping) rests
on.

### The library: `github.com/rivo/uniseg` (pure Go)

Cell width is neither byte count nor rune count nor grapheme count: you have
to account for **grapheme clusters** (an "é" might be base+combining = 2
runes, 1 cell), **east-asian wide** characters (CJK/hangul = 2 cells), and
**emoji with ZWJ sequences** (a family 👨‍👩‍👧‍👦 is 4 emoji joined by U+200D
that the terminal renders as 1 glyph = 1 grapheme of width 2). Reimplementing
Unicode tables in Go would be absurd and fragile: it's delegated to
`rivo/uniseg` (v0.4.7), pure Go (`CGO_ENABLED=0` intact, no transitive
deps), which exposes `StringWidth(s)` (total monospace width) and
`FirstGraphemeClusterInString` (cluster-by-cluster iteration with its width,
to clip without splitting a grapheme). It goes from `// indirect` to direct in
`go.mod` after `go mod tidy`. Discarded alternatives: `go-runewidth` (handles
east-asian but not clusters/ZWJ correctly for our case); `x/text` (doesn't
provide ready-to-use cell width).

### The STRUCTURE of the `Block` type (critical for S23–S29)

A **Block** is an **opaque handle** (`*lua.LUserData` with metatable
`enu.ui.Block`) whose `__index` exposes only `.width` and `.height` (numbers)
to Lua; the content is internal (not exposed as a mutable table: the Block is
opaque, §9.2). Internally (`block.go`):

- `type span struct { text string; st *style }` — a run of text with a
  style. `text` is raw UTF-8; `st` nil = no style (inherits whatever is
  underneath when painted).
- `type style struct { fg, bg string; fgSet, bgSet bool; bold, italic,
  underline, reverse bool }` — a span's style. Colors are stored
  **normalized as strings**: a `"#rrggbb"` literal (validated, lowercased) or
  the 0-255 index as a decimal string (`"42"`). Storing a string (not a
  resolved color type) preserves the literal intent until the compositor
  (S29) degrades it against `enu.ui.caps().colors` — the renderer decides, not
  the Block (§9.2, G22).
- `type block struct { lines [][]span; width, height int }` — a slice of
  lines, each line a slice of spans. `width` = **maximum line width in
  cells** (via `uniseg.StringWidth`), `height` = number of lines. Both are
  computed **once** in `newBlock` (the sole constructor) and **cached**: the
  Block is **immutable** (wrap/markdown/diff return a new one, they don't
  mutate), and the compositor will consult `.width`/`.height` on every blit,
  so recomputing would be the quadratic cost ADR-007 avoids.

**Why spans and not an already-resolved cell grid:** the Block is a
*description* (logical text broken into styled runs), not a *painting*. The
grid, viewport clipping, and color degradation belong to the compositor
(S29); storing spans lets S23/S25 build lines by concatenating runs without
thinking about cells, and it preserves "blit = copy, never re-render" (§9.1).
Helpers that S23–S29 reuse: `newBlock(lines)`, `pushBlock`,
`checkBlock(L, idx)`, `lineWidth(spans)`.

### `enu.text.width/wrap/truncate` — pure CPU, [W], **none of them ⏸**

`text` is [W] (§16) but **none of its functions suspend**: they
measure/rearrange a string already in memory, they don't wait on IO (like the
codecs in S18). That's why they don't use the `suspend` bridge or
`requireTask` — they run synchronously on the main state (and in workers with
S34). [W] means "available in workers," not "suspends."

- `width(s)` → `uniseg.StringWidth(s)`. Empty = 0.
- `wrap(s, width, opts?)` → Block. Word-wrap by word (ASCII spaces), with the
  `\n`s in `s` acting as **hard boundaries** (a `\n\n` leaves a blank line). A
  word **wider than `width`** is **split by grapheme** (`splitWide`) into
  pieces ≤ `width` — splitting is preferable to overflowing the viewport,
  which would clip and silently lose text. `width <= 0` → `EINVAL`.
  `opts.style` applies a default `Style` to each span produced. Wrap collapses
  whitespace (a single space between words on the same line); preserving
  exact spacing isn't a word-wrap's contract.
- `truncate(s, width, opts?)` → string. Clips to ≤ `width` cells **by
  grapheme** (never splits a cluster/emoji). If `s` fits entirely, it's
  returned as-is (no ellipsis). `opts.ellipsis` (e.g. "…") reserves its width
  from the budget; if the ellipsis is **wider than `width`**, it falls back
  to a plain clip with no ellipsis (better plain text than nothing).
  `width == 0` → "". `width < 0` → `EINVAL`.

### `enu.ui.block`/`caps`/`Style` and the BOUNDARY NOTE (G20 is S32)

The contract says that without a TTY `enu.ui` **doesn't exist** (G20). But
that gating is S32, and S23–S31 need `enu.ui.block`/`caps`/`Style` **already**
to build and inspect Blocks in their tests (markdown/highlight/diff produce
Blocks; the theme resolves `Style`). Decision: in S22 `enu.ui` is hung up
**always** (headless too) with only `block`/`caps`; S32 will add the TTY
condition on top without touching these signatures. `enu.has("ui")` stays
**false** until S32 (a capability isn't claimed before it's actually
granted). This is explicit debt, not a contradiction of G20.

- `enu.ui.block(lines)` → Block. Each line is a **string** (an unstyled span)
  or an **array of Spans** `{text, style?}`. `.width`/`.height` are computed
  at construction time. An empty line `""` keeps its slot (it affects
  `.height`).
- `Style` (`parseStyle`/`normalizeColor`): **literal** colors — `"#rrggbb"`
  (6 hex digits) or a 0-255 index (number or numeric string); a **semantic
  name** (`"accent"`) or a malformed hex or an out-of-range index → `EINVAL`
  (names are theme vocabulary, G22).
- `enu.ui.caps()` → `{colors, kitty_keyboard, mouse, images}`. With no live
  terminal to query (that's Phase 6), `colors` is estimated from the
  environment (`COLORTERM=truecolor` → 16M; `TERM` containing "256color" →
  256; empty `TERM` in headless mode → 256 as a reasonable default; `dumb` →
  0; otherwise → 16), and the protocol flags (kitty_keyboard/mouse/images)
  stay `false` (deny-by-default until Phase 6's negotiation, same as
  `enu.has`).

### Tests 🔒 and verification

`text_test.go`: **width**, table-driven and NAMED (empty=0, ascii, ascii with
spaces, CJK wide=2, hangul=2, mixed, simple emoji=2, **ZWJ family emoji=2**,
precomposed é=1, combining é base+mark=1, lone combining mark, several
emoji) + via Lua (incl. combining acute by bytes `\204\129`). **wrap**
(empty→[""], fits, wraps by word, exact-fit word, **word wider than width
gets split**, hard `\n`, blank line between paragraphs, CJK by cells) with
the invariant "no line > width". **truncate** (fits whole/exact, with/without
ellipsis, width 0, multi-cell ellipsis, **doesn't split an emoji**, whole
emoji when it fits, **doesn't split a combining grapheme**, ellipsis wider
than width→plain clip) with the invariant "result ≤ width". **splitWide**
(3× emoji in width 2 → 3 pieces; a 2-cell-wide emoji in width 1 → single
unsplit piece). **ui.block** manually inspected in Go (width=max line,
height=line count, styled spans, blank line preserved), normalized color
index, validations→EINVAL, **caps** (4 keys, colors>0, protocols false),
**normalizeColor** (hex lowercased, indices, rejection of names, G22).
`TestTextNotSuspending` confirms they run outside a task.

`CGO_ENABLED=0 go build`/`go vet`/`gofmt -l` clean; `CGO_ENABLED=1 go test
-race -timeout 120s -count=2 ./internal/...` green, no flakiness (no
regression across S01–S21). The `enu -e` binary confirms it e2e: width (5/4/2/2
for ascii/CJK/emoji/ZWJ), wrap (height/width), truncate with ellipsis,
ui.block (width 6/height 2), caps, and G22 (`fg="accent"` → EINVAL).

**No findings:** §10 and §9.2 were sufficient. Pointer ▶ advances to **S23**
(`enu.text.markdown`, 🔒).

## S23 — `enu.text.markdown` (full render, streaming-safe, themable) (api.md §10, 🔒)

`enu.text.markdown(s, opts) -> Block` renders full markdown into a `Block` of
width `opts.width`. It is **[W] but NONE OF IT ⏸** (pure CPU, like
`width`/`wrap`/`truncate` in S22 and the codecs in S18: it parses a string
already in memory, it doesn't wait on IO; that's why it doesn't use `suspend`
or `requireTask`). It lives in `markdown.go`; it's hung from `registerText`
(text.go) to keep all of `enu.text` in one place.

### Library: goldmark (pure Go, CommonMark)

`github.com/yuin/goldmark` v1.7.8 — pure Go, CommonMark, **no transitive
deps** that would affect `CGO_ENABLED=0` (ADR-001). "Lua decides, Go
executes" (ADR-004): parsing the document to an AST is done by goldmark
(`goldmark.DefaultParser().Parse(text.NewReader(src))`) and we walk the AST
emitting spans into the `Block`. As much as possible is **reused** from S22:
`wrapText`/`splitWide` (word-wrap and grapheme splitting),
`uniseg.StringWidth` (cell width, the same one `text.width` uses), and
`parseStyle`/`normalizeColor` (theme with literal colors, G22). It goes from
`// indirect` to direct after `go mod tidy`.

### Theme model

`opts.theme` is a table with a `Style` per element; keys: `h1`..`h6`,
`code`, `emphasis`, `strong`, `link`, `bullet`, `blockquote`, `rule`. Each is
optional; anything absent falls back to `defaultTheme()`: bold on
headings/strong, italic on emphasis/blockquote, underline on link, **no
color** (we don't impose a palette; the toolkit adds one via `opts.theme`).
Colors are **literal** (`#rrggbb` or a 0-255 index), validated by
`parseStyle`; a semantic name (`"accent"`) → `EINVAL` naming the element
(G22: names belong to the toolkit's theme, not the core). Inline style is
composed with `combineStyle` (ORs the boolean attributes, `add`'s colors
override `base`'s): a `[link]` inside *italics* keeps the italics and adds the
underline.

### Elements supported (and tables: NO)

Headings (restyled with the level's Style over its inner emphasis
styling), paragraphs with word-wrap preserving inline styles
(**bold**/*italic*/`code inline`/[link]/autolink), fenced and indented code
blocks (one line per code line, **WITHOUT wrapping** — code doesn't reflow;
the compositor clips it — one span per line with `theme.code`), lists
`-`/`*`/`1.` (marker + hanging indent; `Start` respected for ordered lists),
blockquotes (`> ` prefix + content), rules `---` (a dashed line), links (text
with `theme.link`).

**Tables: NOT supported** in S23. They're a GFM extension, not base
CommonMark; goldmark without extensions doesn't parse them, so a table falls
back to a plain-text paragraph (cells with `|`) — valid and stable, just
without table formatting. If an extension requests them, this reopens as a
P## and the goldmark extension is enabled.

### Container width (prefix + content ≤ width)

A blockquote (`> `, 2 cells) or a list item (`- `/`1. `, N cells) consumes
width with its prefix. So that prefix+content doesn't exceed `opts.width`,
the inner content is rendered at a **reduced** width (`renderChildrenWidth`
temporarily lowers `r.width` by the prefix's span and restores it on return).
The reduced width is fixed per container (it doesn't depend on subsequent
content), so it doesn't compromise stability. An ordered list's marker can
grow (`9.`→`10.`), a minor reflow confined to the list block (which is a
single top-level block; the invariant only protects *previous* blocks).

### wrapSpans: word-wrap over styled spans (ours)

Generalizes S22's word-wrap to a sequence of styled spans. `tokenizeSpans`
breaks it into words while remembering `sepBefore` (whether it followed a
space in the source): **it doesn't invent a space where the source didn't
have one** — this is what fixes the "code ." bug (an inline `code` glued to
a period), "*doesn't close" (a stray `*` glued to the word), and
"[aqui](http" (an unclosed link). Glued tokens (no separation) form an
**atomic group** that doesn't split when wrapping (it's a visual word);
between groups there's a space, and a group wider than `width` is split by
grapheme with `splitWide`, preserving each token's style.

### STREAMING-SAFE and the stability invariant (the 🔒 logic)

**Incomplete input doesn't break.** goldmark is tolerant: it parses through
to EOF (an unclosed ``` fence is a code block through to the end of the
text, an unclosed `*emphasis` falls back to plain text, an unclosed
`[link](` stays as plain text). We don't rely on the last block being
"closed"; rendering always produces a valid Block (height ≥ 1) with no panic
or error.

**Stability strategy: render by INDEPENDENT top-level blocks.**
`renderMarkdownBlocks` returns `[][][]span` (one slice per direct child of
the document); the Block is their concatenation. The key insight: markdown
is stable across blocks when growing from the end — adding text only affects
the LAST top-level block (the "in progress" one); previous ones are already
delimited by a blank line or a type change. Rendering block by block (not a
global layout) is what prevents an open fence at the end from reflowing the
paragraphs above it.

**EXACT INVARIANT (what the 🔒 test shields):** let `R(s) = [B_1, ..., B_m]`
be the render's decomposition into top-level blocks. For `s_k` a prefix of
`s_{k+1}` (one more token), `B_i(s_k) == B_i(s_{k+1})` for all `i < m_k - 1`
(the already-complete blocks, all but the last one of the short prefix, don't
change; growth only happens at the end). CommonMark's exceptions (a setext
heading that reinterprets the previous paragraph via an `===`/`---`
underline; lazy continuation extending a paragraph) don't break the Block,
they only relax the LAST block, which is why the invariant excludes it. The
test emits block boundaries and compares block by block except the last one
(0 violations over 4 docs chunked per-rune and one chunked per-token).

### Extension point for S24 (highlight)

`renderCodeBlock` today applies ONE span (`theme.code`) per line of code and
already extracts `lang` via `languageOf`. S24 (`enu.text.highlight`) will
replace that plain span with N colored spans per token according to the
language's lexer, keeping the SAME scaffolding (one entry per line of code)
— that's why `lang` is already passed to `renderCodeBlock` even though it's
ignored today.

### Tests 🔒 and verification

`markdown_test.go`: (1) **incomplete input doesn't break** (a table of ~20
cases: fence/list/ordered/italic/bold/code-inline/link/heading/quote/hr/setext
midway, stray backtick/asterisk/bracket, chaotic mix) + via Lua; (2)
**stable growth** per-rune over 4 docs and one per-token chunking, comparing
top-level blocks except the last one (exact invariant, 0 violations); (3)
rendering of every element (headings by level, emphasis/strong/normal, code
block no-wrap + extracted `lang`, bullet/ordered lists with `Start`/hanging
indent, blockquote prefix+style, hr at `width`, underlined link, paragraph
wrap≤width, Block.width≤opts.width); (4) literal theme applied (inspected in
Go) + G22→`EINVAL` naming the element, `opts.width` mandatory (7 paths→
`EINVAL`: no opts, no width, 0, negative, non-integer, non-table opts,
non-number width), doesn't suspend outside a task.

`CGO_ENABLED=0 go build`/`go vet`/`gofmt -l` clean; `CGO_ENABLED=1 go test
-race -timeout 120s -count=2 ./internal/...` green, no flakiness (no
regression across S01–S22). The `enu -e` binary confirms it e2e: a full doc
(height 5, width 23), incomplete fence (height 1), theme `fg="accent"` →
rejected (G22).

**No findings:** §10 was sufficient. Pointer ▶ advances to **S24**
(`enu.text.highlight`, 🔒).

## S24 — `enu.text.highlight` (syntax highlighting, degrades to plain text) (api.md §10)

`enu.text.highlight(code, lang, opts?) -> Block` highlights a snippet into a
`Block` with one span per colored token run. **[W] but NONE OF IT ⏸** (pure
CPU like S22/S23/S18: it tokenizes a string already in memory, it doesn't
wait on IO; neither `suspend` nor `requireTask`). It lives in `highlight.go`
and is hung from `enu.text` alongside `width`/`wrap`/`truncate`/`markdown`.
Not a single extra public function.

### Library: chroma, and the version decision

`github.com/alecthomas/chroma/v2` is used (pure Go, dozens of lexers and
themes that assign `#rrggbb` colors per token type — exactly what the
`Block` wants to store as a LITERAL color, G22). It's the canonical
highlighting choice in Go and fits "Lua decides, Go executes" (ADR-004):
heavy lexing goes in Go.

**Deliberate deviation — pinning v2.14.0, not the latest (v2.27.0).** The
latest chroma declares `go >= 1.25` in its `go.mod`; adding it **would bump
the `enu` module's `go` version from 1.24.7 to 1.25** (a toolchain change for
the whole project, not a decision that belongs to S24). v2.14.0 keeps
`go 1.24.7` intact and brings the same lexers/themes needed. It brings
`github.com/dlclark/regexp2` as a transitive dep (pure Go, `CGO_ENABLED=0`
intact). If the module ever moves to Go ≥ 1.25 for another reason, chroma can
be updated at no extra cost; that's the trigger.

### The degradation to plain text (our own logic)

An **unknown, empty, or nil** `lang` is NOT an error: it degrades to **plain
text** — a `Block` with no style (one span per line via S22's `splitLines`,
with the EXACT text). This is the safety net for S23's fence rendering under
streaming: a fence whose `lang` we don't recognize (or has none) still
yields a legible Block instead of breaking. The signal is that
`lexers.Get(lang)` returns `nil` when there's no lexer for that name (after
also trying by extension); an empty `lang` isn't even looked up. A
tokenization failure (not expected with the embedded lexers) also falls back
to plain text: highlight never breaks the render.

### The tokens→spans mapping

Lexer found → wrapped in `chroma.Coalesce` (merges adjacent tokens of the
SAME type: fewer spans, same result, identical text). Tokenized with
`EnsureLF=false` (don't alter the source text: we want to reconstruct `code`
EXACTLY from the spans). Grouped by line with `chroma.SplitTokensIntoLines`
(one line of code → one Block line) — that function leaves the `\n` as a
suffix on the token that closes each line, trimmed with `TrimSuffix` (the
line break is Block structure, not span text). Each run is emitted as a
`span{text, style}` with `tokenStyle(theme, tok.Type)`: the foreground color
(literal `#rrggbb` from `Colour.String()` if `IsSet()`, G22) and the
bold/italic/underline attributes (Chroma's tri-state `Yes` values); a token
with no color or attributes → `st = nil` (no style), so as not to inflate the
Block. Chroma doesn't expose "reverse," so that attribute stays false. A
blank line of code keeps its slot (an empty, unstyled span); empty code → a
Block of height 1.

### Theme: the name, not a hand-rolled mapping

`opts.theme` is a **string**: the name of a Chroma theme (default `"github"`,
clear and legible). An unknown theme falls back to `styles.Get`'s own
fallback (never nil, never breaks). **No** hand-rolled `Style`-per-token-type
mapping is accepted: Chroma's `TokenType`s are a wide vocabulary (dozens of
subcategories) and exposing them would leak the library's internals into the
public API; the theme name is the only knob, and a Chroma theme already
gives literal colors coherent with G22. §10's signature is
`highlight(code, lang, opts?)`; `opts` only carries `theme?` for now — no
surface expansion.

### Boundary: markdown.go is NOT touched

S23 left `renderCodeBlock` as an "extension point" for S24 to replace its
plain span with N colored spans. **That integration is deferred**: S24
implements `enu.text.highlight` standalone and does NOT modify `markdown.go`,
so as not to risk S23's stability invariant (streaming-safe). Integrating
highlight-inside-markdown is future, reopenable work (an `opts.highlight` or
similar in `markdown`), outside the scope of `highlight`'s §10.

### Tests (`highlight_test.go`)

On the pure core `highlightToBlock` (no LState): Go → several spans with
style and ≥2 distinct `#rrggbb` colors, `.height`=line count;
unknown/""/strange lang → plain text with no style + EXACT text;
json/python/lua → reasonable spans (≥2 colors); blank line keeps its slot;
empty code → height≥1; unknown theme → fallback. Cross-cutting invariant "no
text is lost": concatenating the per-line spans reproduces `code`. Via Lua
(`buildBlock` from text_test.go): Go height/style, unknown→plain text,
readable `.height`, valid no-opts and `opts.theme`; bad calling shapes (lang
non-string, opts non-table, `opts.theme` non-string) → `EINVAL` (nil/""
lang is NOT an error: it degrades).

`CGO_ENABLED=0 go build`/`go vet`/`gofmt -l` clean; `CGO_ENABLED=1 go test
-race -timeout 120s -count=2 ./internal/...` green, no flakiness (no
regression across S01–S23). The `enu -e` binary confirms it e2e: go (height
3), unknown → plain text (height 2), non-string `opts.theme` → `EINVAL`.

**No findings:** §10 was sufficient. Pointer ▶ advances to **S25**
(`enu.text.diff`).

## S25 — `enu.text.diff` (structured hunks + render to Block) (api.md §10, 🔒)

`enu.text.diff(a, b, opts?) -> {hunks, block?}` compares `a` (old) and `b`
(new) **line by line** and returns their hunks (regions of change) and,
optionally, a painted `Block`. **[W] but NONE OF IT ⏸** (pure CPU, like
`width`/`wrap`/`markdown`/`highlight` in S22–S24 and the codecs in S18): it
doesn't use the `suspend` bridge or `requireTask`. It lives in `diff.go` and
reuses S22's Block helpers (`newBlock`/`span`/`style`,
`parseStyle`/`normalizeColor`).

### Algorithm / library: our own line-based LCS, WITH NO new dependency

The task offered the option of using `go-difflib` or `gotextdiff`. The
decision is **to add no dependency**: classic line-based diff (LCS via
dynamic programming → backtrack → grouped into hunks with context) is small,
its edge-case correctness is exactly what the 🔒 test shields, and keeping it
in-house avoids tying the hunk shape (the consumer's public API) to that of
an external library. `go.mod`/`go.sum` remain **untouched** (zero new deps,
consistent with "zero dependency hell," ADR-001/philosophy §6).

The pieces (all pure, no LState, directly testable):

- `splitDiffLines(s)`: splits `s` on `\n`, treating the break as a
  **terminator, not a separator** — `"a\n"` and `"a"` both give `["a"]`; `""`
  gives zero lines; `"a\nb"` gives `["a","b"]`. So **"no trailing newline"**
  doesn't introduce spurious differences against the same text with a
  trailing newline (a 🔒 edge case). The `\r` of a CRLF is kept within the
  line (the diff is by exact content; normalizing line endings is the
  consumer's job).
- `lcsTable(a, b)`: longest-common-subsequence lengths via DP, filled
  back-to-front so the backtrack advances in file order. O(n·m); enough for
  human-sized diffs (Myers O(ND) reopenable if it's ever needed for huge
  files).
- `diffOps(a, b)`: a backtrack that emits the `context`/`del`/`add`
  sequence. The tie-break (on a change, `del` before `add`) is the same as
  unified diff's: a modified line comes out as its `del` followed by its
  `add`.
- `groupHunks(ops)`: groups changes into hunks surrounded by at most
  **`diffContextLines` = 3** lines of context on each side (the de facto
  standard), and **merges** two changes separated by ≤ 2·context into a
  single hunk (their context overlaps), as unified diff does.

### The shape of hunks (the API consumed by the viewer / toolkit)

Each hunk: `{ old_start, old_count, new_start, new_count, lines = { {kind, text},
... } }`, with `kind` ∈ `"context"|"del"|"add"`. Indices are **1-based**
(Lua convention). `old_start`/`new_start` point to the hunk's first line
(context or change) on each side; `old_count`/`new_count` are how many
lines of that side the hunk spans (context+del for old, context+add for
new). When one side **touches none of its own lines** (e.g. `a` empty → `b`:
all add; or `b` empty: all del) its `*_start` and `*_count` are **0** — the
unified-diff convention (0 = insertion position at the start). `a == b` →
empty `hunks` (`#hunks == 0` unambiguously distinguishes "no changes").

### The render (`opts.render = true`)

`renderDiffBlock` paints an `@@ -o,oc +n,nc @@` header (the `header` style,
bold) per hunk, and, below it, one line per operation with a `+ `/`- `/`  `
prefix and the style of that type. The default theme (`defaultDiffTheme`,
G22): add is **green** (LITERAL ANSI index `"2"`), del is **red** (`"1"`),
context unstyled, header bold. **Literal** colors (index or `#rrggbb`),
which compositor S29 will degrade against `caps().colors` (the Block stores
literals, never semantic names). `opts.theme` (keys `add`/`del`/`context`/
`header`) validates each `Style` with `parseStyle` (a semantic name like
`"accent"` → `EINVAL`, G22). No hunks → a valid empty Block (one blank
line, height 1: a Block always has ≥1 line, as with markdown/highlight).

### `diffContextLines` fixed at 3 (no knob in `opts`)

The number of context lines is NOT exposed via `opts`: §10's signature
doesn't provide for it, and 3 is unified diff's de facto standard. Adding a
knob would expand the public surface unnecessarily (sacred API, ADR-003);
reopenable if a concrete consumer asks for it.

### Tests (`diff_test.go`, table-driven, naming the 🔒 edge cases)

`TestComputeDiffEdges` shields: pure insertion, pure deletion, a change
(del+add), a change on the **FIRST** line, a change on the **LAST** line, `a`
empty → all add (old ranges 0,0), `a` → `b` empty all del (new 0,0), both
empty with no hunks, `a == b` with no hunks, a single line, **no trailing
newline == with trailing newline**, no-newline last line changed, insertion
at the start, append at the end, two distant changes → 2 hunks, two close
ones → 1 merged hunk. Plus `TestSplitDiffLines` (terminator vs separator,
CRLF), `TestDiffLinesConsistentWithSources` (each `context`/`del` matches
`a`, `context`/`add` matches `b`), `TestRenderDiffBlock` (+/-/␣ prefixes,
green/red/neutral styles, height = header + ops), `TestRenderDiffBlockEmpty`
(height 1), and via Lua (`TestDiffLua` inspected hunks/`.height`/no render→
`block==nil`, `TestDiffLuaErrors` non-table opts / semantic theme name G22 /
non-table theme → `EINVAL`, `TestDiffLuaTheme` literal colors applied to the
render).

`CGO_ENABLED=0 go build`/`go vet`/`gofmt -l` clean; `CGO_ENABLED=1 go test
-race -timeout 120s -count=2 ./internal/...` green, no flakiness (no
regression across S01–S24). The `enu -e` binary confirms it e2e: a change in
the middle → 1 hunk (context/context/del/add/context), block.height = 6,
old/new ranges 1,4; `a==b` → 0 hunks; `a` empty → old_count 0 / new_count 2;
non-table `opts` → `EINVAL`.

### Fix: merge threshold at the 2·context boundary (off-by-one)

`groupHunks`'s merge threshold had an off-by-one: the condition was
`next - diffContextLines <= end`, which merged context gaps ≤ 5 but
**separated** a gap of exactly 6 (= 2·`diffContextLines`), contradicting the
function's own comment and this entry ("merges two changes separated by ≤
2·context"). `git diff -U3` and `GNU diff -U3` merge up to a gap of 6 and
separate from 7 onward (verified). When the two context blocks end up
**adjacent without overlapping** (gap = 6) they should still be a single
hunk. Minimal fix: `next - diffContextLines <= end + 1` (the `+1` covers the
adjacency). The merged hunk's ranges span both changes plus all the
intervening context. Edge-case tests added to `TestComputeDiffEdges` naming
the case: context gap = 5 → 1 hunk, = 6 → 1 hunk (the one that was failing,
with old/new ranges 1,8 checked), = 7 → 2 hunks; consistent with `diff -U3`.

**No findings:** §10 was sufficient (`diff(a, b, opts?)` was implemented as
is; `opts` uses `render?`/`theme?`). Pointer ▶ advances to **S26** (`enu.re`).

## S26 — `enu.re` (RE2: compile/match/find_all/replace)

`enu.re` implements §10's row (`enu.re.compile(pattern) -> Re` + the `Re`
handle with `match`/`find_all`/`replace`) on top of Go stdlib's `regexp`,
which **is** RE2. Three design decisions of our own — the shape of captures,
the units of the ranges, and the `repl` syntax — and one observation about
the engine choice.

### Why RE2 (and with no new dependency)

The stdlib's `regexp` **is** an RE2 implementation: it guarantees **linear**
time over the input size (automaton, no backtracking) in exchange for not
supporting **backreferences** or lookaround. For a harness this is exactly
what's wanted: a pattern coming from an agent or from configuration can
NEVER hang the runtime with a ReDoS (catastrophic backtracking). The price
is documented and reported: `compile("(a)\\1")` (backreference) → `EINVAL`
with `regexp.Compile`'s message embedded (the stdlib reports it as an
invalid escape sequence), not a silent failure. **Zero new dependencies**
(`go.mod`/`go.sum` untouched, ADR-001). `*regexp.Regexp` is **safe for
concurrent use** (guaranteed by its docs), so a single `Re` is used from
several tasks with no lock (fits the browser concurrency model, ADR-004). It
is **[W] but NONE OF IT ⏸** (pure CPU: it compiles/matches a string in
memory, no IO; like `enu.text` and S18's codecs): neither `suspend` nor
`requireTask`.

### Decision: shape of `caps` in `Re:match` — 1-based array + named groups

`Re:match(s)` returns, on a match, a table with TWO views at once:

  - **Array part, 1-based** (Lua style): `[1]` is the COMPLETE match
    (group 0), `[2]` the first group, `[3]` the second, etc. So `caps[1]`
    is ALWAYS the full match, even if the pattern has no groups (a pattern
    without groups gives `caps[1]` and nothing else).
  - **Named groups** (`(?P<name>...)`, RE2/Go naming syntax)
    ALSO by their **string key**: `caps.name`. They coexist with the array
    part (a named group appears twice: by its positional index and by its
    name), leaving Lua to access it however it prefers.

Discarded alternatives: (a) groups only (without match 0 in `[1]`) — this
broke the "pattern without groups" case and forced a separate field for the
full match —; (b) a `.groups` field separate from `.full` — more verbose
with no gain —. The 1-based array with `[1]`=full match is the most natural
convention in Lua and the least surprising one. No match → `nil` (does not
throw: not matching is a valid result, not an error). An optional group
that did not participate (e.g. `(a)?` without "a") → `""` (empty string):
`FindStringSubmatch` does not distinguish "empty" from "absent" in its
string output, and a Lua array does not allow an intermediate `nil` without
creating a hole.

### Decision: units of `Re:find_all` — byte ranges, 1-based, inclusive

`Re:find_all(s)` returns ALL matches (non-overlapping, left to right) as an
array of ranges `{start, end}` with **BYTE offsets, 1-based, both
inclusive** —the SAME convention as Lua's `string.find`—, so that
`s:sub(start, end)` reconstructs EXACTLY each match.

**Bytes** (not runes/characters) are chosen for two mutually reinforcing
reasons: (a) Lua's `string.sub` indexes by byte, so the range is directly
usable (composing with `s:sub` is the obvious use case: locate/highlight);
(b) Go's `FindAllStringIndex` already returns byte offsets —converting to
runes would force a recount and break that composition—. The convention
conversion: Go gives `[start, end)` 0-based with an **exclusive** end; in
Lua, `start = start+1` (1-based) and `end = end` (an exclusive 0-based end
numerically matches the last 1-based inclusive byte). An **empty** match
(e.g. `x*` on "ab" matches the empty string at each position) gives `end =
start-1` (zero length), consistent with `s:sub(start, start-1)` being `""`
in Lua.

Only the **ranges of the full match** are returned, not those of each
group: it is the common case (highlight/locate where the pattern matches)
and keeps the signature simple. Whoever needs the captures of each match
extracts them with `match` over the segment; if the "per-group ranges"
pattern were to recur, it would be a future addition (API is not
speculated; the §10 surface is sacred).

### Decision: `repl` syntax in `Re:replace` — Go's

`Re:replace(s, repl)` substitutes ALL non-overlapping matches and delegates
to `Regexp.ReplaceAllString`, so `repl` uses **Go's syntax**: `$1`, `$2`,
... refer to groups by number; `${name}` by name; `$0` (or `${0}`) the full
match; `$$` is a literal `$`. A name not delimited by braces extends to the
last alphanumeric character (`$1x` looks for group "1x", not group 1
followed by "x"): `${1}x` is recommended —the same nuance the stdlib
documents—. A reference to a nonexistent group is replaced with empty. No
custom syntax is invented (`\1` or another): reusing the library's is less
surface, fewer surprises, and free documentation. No matches → `s`
unchanged.

### Tests

Via the Lua harness (`re_test.go`), because the shape of the captures table
and of the ranges is only observable from Lua: `TestReMatchPositional`
(`(\d+)-(\d+)` over "12-34" → `[1]`/`[2]`/`[3]`), `TestReMatchNamed` (by
index AND by name), `TestReMatchNoGroups`, `TestReMatchNoMatch` (→nil),
`TestReMatchEmptyString` (`\d+` does not match empty, `\d*` does with an
empty match), `TestReMatchOptionalGroup` (absent group→""),
`TestReFindAllRanges` (`s:sub` reconstructs each match + concrete offsets),
`TestReFindAllNone`, `TestReFindAllUTF8` (BYTE offsets consistent with
`string.sub` over multibyte text), `TestReFindAllEmptyMatch`
(`end=start-1`), `TestReReplaceNumbered`/`Named`/`NoMatch`/`All`,
`TestReCompileBackreference` (`(a)\1`→`EINVAL` with message, de-facto
criterion), `TestReCompileInvalidSyntax` (`(abc`→`EINVAL`), `TestReFromTask`
(use from a task; result exposed via a global after `waitIdle`),
`TestReTypeMismatch` (non-`Re` self→`EINVAL`).

`CGO_ENABLED=0 go build`/`go vet`/`gofmt -l` clean; `CGO_ENABLED=1 go test
-race -timeout 120s -count=2 ./internal/...` green, no flakiness (no
regression on S01–S25). The `enu -e` binary confirms e2e: match with groups
(12-34/12/34), named group, no-match→nil, find_all that reconstructs via
`s:sub`, replace `${b}-${a}`, backreference→`EINVAL`.

**No findings:** §10 was sufficient (`compile`/`match`/`find_all`/`replace`
were implemented as-is). Pointer ▶ advances to **S27** (`enu.search.files`).

## S27 — `enu.search` (files/grep/fuzzy) (api.md §11, 🔒; closes Phase 5 — CP-6)

Repo-scale search: the three primitives of §11 in `search.go`. **[W]**
(§16; today the main state, workers S34). **Not one public function more**
(§11 was already in api.md). **No findings:** the ⏸ bridge from S04, the
iterator pattern from `Stream` in S20, and the libraries already present
(go-gitignore from S15, `regexp`/RE2 from S26) sufficed.

### No new dependency (`go.mod`/`go.sum` untouched)

`files`/`grep` reuse **go-gitignore** (S15) and **`regexp`/RE2** (S26). For
`fuzzy`, adding `github.com/sahilm/fuzzy` was evaluated; **discarded**: the
scorer of a picker is a well-known ~50-line algorithm (subsequence with
bonus, simplified fzf style), easy to shield and to make deterministic;
adding a dependency for that contradicts "zero dependency hell" (philosophy
§6) with no gain (it is not a treacherous parser like YAML, where the
library was indeed justified in S18).

### `files` — traversal and filtering (`walkFiles`)

`filepath.WalkDir` from `root`, in the background goroutine of the ⏸
bridge. Filters, in this order per entry: (1) `.git/` ALWAYS pruned
(`SkipDir`); (2) hidden (name starting with `.`) — without `hidden` the
dirs are pruned and files skipped, with `hidden=true` they are included
(except `.git/`); (3) `.gitignore` (loaded from `root`, checked against the
path RELATIVE to `root` as git does) — what is ignored is pruned (dir) or
skipped (file); (4) `glob` by the file's BASE name. `max` cuts `WalkDir`
short with a sentinel (`errFilesMaxReached`) — there is no other early-stop
mechanism in `WalkDir`. Nonexistent `root` → `ENOENT` (checked with
`os.Stat` before the walk, because `WalkDir` does not fail cleanly if the
root does not exist). **Decision:** `gitignore` is ALWAYS active in `files`
(it is not opt-out as in `watch`): a file picker over `node_modules/` is
pure noise; §11 does not expose a knob to disable it, so none is invented.
The `.gitignore` file itself is just another file (with `hidden=true` it
appears; its patterns do not name it, it does not self-exclude).

### `grep` — the parallel iterator (the delicate part, twin of the `Stream` from S20)

The pattern is that of the stream iterator (S20) generalized to N producers:

- **Enumeration first, under the ⏸ bridge:** `walkFiles` (gitignore+glob)
  lists the candidate files in the background goroutine, so that a
  nonexistent `root` → `ENOENT` when the iterator is CREATED, not midway
  through consumption.
- **Bounded pool:** `grepWorkers` = `runtime.NumCPU()` capped by the number
  of files (do not launch 8 goroutines for 3 files), floor 1. Each worker
  takes files from a work channel and matches line by line
  (`bufio.Scanner`, buffer raised to 1 MiB to avoid aborting on long
  lines).
- **UNbuffered channel (`results`):** natural backpressure — a worker
  blocks when pushing a match until `next` pulls it out. `next` reads
  `<-results` inside the `work` of the ⏸ bridge (outside the token, NEVER
  touching Lua); the `emitted`/`max` count and `it.close()` are touched
  ONLY in `deliverFn` (under the token).
- **EOF:** a closer goroutine does `wg.Wait()` (all workers finished) and
  `close(results)`; `next` distinguishes "end" (`ok=false`) from "next
  match".
- **Cancellation (S08):** when the iterator is created, an
  `enu.task.cleanup` is registered (`registerGrepCleanup`, manipulates the
  task's LIFO stack under the token) that closes the `context`.
  Cancel/terminate the task → dispatcher and workers stop (`ctx.Done`) and
  the closer goroutine closes `results`, unblocking a stuck `next`. Without
  this, a stuck `<-results` after an abort would leave a task goroutine
  waiting forever. Safety net `Runtime.Close`→`stopAllGreps` (tracking
  `scheduler.greps`, twin of `streams`). `grepIter.close` idempotent
  (`closeOnce`).
- **`ranges` consistent with S26:** 1-based inclusive byte
  (`FindAllStringIndex` +1 on the start, end as-is), so that
  `line:sub(start,end)` reconstructs the match — the same convention as
  `enu.re.find_all`.
- **`opts.root` MANDATORY** (where to search?); `case` by string
  `"sensitive"|"insensitive"` (insensitive prepends `(?i)` to the regex).
  Unreadable/binary files are skipped silently (like `grep -r`).

**Delivery order:** NOT deterministic across files (several workers compete
for the channel), but within a file the lines come out in order (a file is
processed by a single worker, top to bottom). §11 only promises "as they
arrive," not a global order — the parallelism test verifies the TOTAL and
the per-file count, not the order.

### `fuzzy` — synchronous, own scorer, stable order

**NOT ⏸** (the hot primitive of the picker, §11): pure CPU over in-memory
data, like `enu.re`/codecs — it does not use the `suspend` bridge.
`fuzzyScore`: case-insensitive subsequence with per-character base + bonus
for contiguity + bonus for word start (after a `/\_-. ` separator or a
lowercase→uppercase camelCase change) + bonus for first character. An empty
`query` matches everything (score 0: freshly opened picker).

**Stability (🔒 inventory):** `sort.SliceStable` by score DESC comparing
**ONLY by score** (NEVER by index). Comparing by index would break
stability against an arbitrary input order; `SliceStable` already preserves
input order on ties, which is exactly what the contract asks for (a picker
with ties shows candidates in their natural order, not shuffled). The
flagship test passes 4 identical candidates and demands the order 1,2,3,4.

### Tests and verification

`search_test.go` (hermetic in `t.TempDir()`): files (gitignore/hidden/glob/
max/errors), fuzzy (order/stability/unit scorer/empty/max/EINVAL), grep
(shape+ranges, glob/case/max, full parallelism 50×3=150 with no loss/
duplicates, early-stop without goroutine leaks). `cp6_test.go` (CP-6,
closes Phase 5): markdown+highlight+diff+grep+fuzzy+files together over a
repo on disk, all inspected without painting the screen.

`CGO_ENABLED=0 go build`/`go vet`/`gofmt -l` clean; `CGO_ENABLED=1 go test
-race -timeout 120s -count=2 ./internal/...` green, no flakiness (no
regression on S01–S26). The `enu -e` binary confirms e2e. **APILevel stays
at 1.**

**CP-6 green → `[x] Phase 5`.** Pointer ▶ advances to **S28** (ADR-007
SPIKE, opens Phase 6).

## S28 — ADR-007 SPIKE (minimal compositor + Lua toolkit; VETO MILESTONE)

S28 is not an API feature: it is the **veto milestone** that validates
ADR-007 (widget toolkit in Lua) before committing to the UI architecture.
The result and the measurements are formally recorded in **ADR-012**
(adr.md); here go the spike's design decisions that are not spec-related.

### Scope: internal and disposable, NOT the public API §9

The spike builds a **minimal, internal** version of what S29 will expose as
`enu.ui` (cells, regions, blit, diff→ANSI), but it is **not hung off** the
`enu` global: it lives in `internal/runtime/spike_compositor.go` (the
primitive) and `spike_shim.go` (the Lua bridge, registered only from tests
via `registerSpikeShim`, NEVER from `registerNu`). This way the veto is
measured without freezing anything or extending the sacred surface (api.md
untouched, APILevel stays at 1). S29 replaces the spike with the production
compositor; these files are disposable. If §9 had not been enough for the
toolkit it would have been a `G##`, but it was enough.

### The minimal compositor (implementation decisions)

- **Flat grid** (`[]scell` indexed by `y*w+x`, not `[][]scell`) for cache
  locality: the diff walks the whole thing every frame. Each cell is
  `{rune (as a string, so as not to split a wide grapheme/ZWJ), *style,
  width}`.
- **Double buffer** (back = frame under composition, front = last emitted).
  The back buffer is **reused** across frames (`clear` in place, no
  realloc) so as not to pressure the GC on the hot path (one frame per
  token).
- **`blitBlock` = viewport copy (G28).** Stamps the S22 `*block` onto the
  region at local coordinates `(ox, oy)` which can be **negative**
  (negative offset = start further down/right in the Block = scroll); the
  excess is clipped at the region's edge. It is a cell-by-cell copy, never
  a re-render (§9.1): scrolling costs a window copy, not recomputing the
  Block. Wide graphemes (w=2) leave the next cell as a continuation
  (`r=""`, `w=0`).
- **Diff → ANSI by runs.** Walks by rows; where a cell differs from the
  front buffer it starts a *run* with a single cursor move (`ESC[y;xH`,
  1-based) and extends it while it keeps differing; it emits SGR
  (`ESC[...m`) only when the style changes relative to the previously
  emitted cell (minimizes bytes). Literal colors (§9.2): `#rrggbb`→
  truecolor (`38;2;r;g;b`), index→256 (`38;5;n`). Fine degradation with
  `caps().colors` is S29; the cost of building the string is of the same
  order.
- **Coalescing:** `frame()` returns the number of changed cells; 0 changes
  = 0 bytes emitted (an identical frame produces no output), realizing "the
  UI repaints on events, not at 60 fps" (ADR-007).

### The minimal Lua shim (what it measures)

`__spike.composer/markdown/fuzzy_window` + `Composer:region/begin/frame`
and `Region:blit/fill` methods. The "minimal toolkit in Lua" that the veto
evaluates **is the benchmark's Lua script** that orchestrates these
primitives: per frame it does `begin → fill/blit → frame` (~3 Go↔Lua
crossings). `markdown` reuses `renderMarkdownBlocks` (S23) and
`fuzzy_window` reuses `fuzzyScore` (S27) + builds the Block of the visible
window (top N) — "filtering is a Go primitive, Lua repaints what's
visible."

### The threshold and the methodology of the veto (honesty)

- **Pre-committed threshold:** case (a) streaming markdown 120×40 ≤ **8 ms/
  frame** (¼ of the 30 fps budget, slack for HTTP/SSE/parse and slow
  hardware); case (b) picker 100k ≤ **50 ms/keystroke** (the "instant"
  cutoff).
- **Attribution criterion (the key one):** the ADR-007 question is not "is
  the render fast?" but "does the *overhead of orchestrating from Lua*
  break fluidity compared to Go?". That is why the veto only fires if a
  case falls outside budget **AND** the cause is Lua overhead (not the Go
  primitive, which moving the toolkit to Go would not fix). Pure Go vs
  Lua-orchestrated are measured and the delta is reported.
- **`-race` does NOT decide the veto.** The race detector instruments every
  access and inflates times ~7× (verified: case (b) p99 goes from ~52 ms to
  ~354 ms under `-race`): valid for CORRECTNESS, useless for a PERFORMANCE
  veto. It is detected via build tags (`spike_race_{on,off}_test.go` →
  `spikeRaceEnabled`) and under `-race` `TestSpikeMeasureVeto` only reports
  numbers (veto "undecided," does not fail). The firm verdict is the run
  without `-race`.
- **Declared headless limitation:** without a TTY the diff goes to an
  in-memory buffer, not a terminal. The **compute cost** is measured
  (compose+diff+encode + Lua crossing), not the physical pty latency —
  which is identical whether Lua or Go decides, so it does not bias the
  decision—. That is exactly what the veto puts at stake.

### The result and the picker observation

**The veto does NOT fire:** Lua overhead is negligible in both cases (case
(a) ±tens of µs; case (b) within noise, Lua sometimes faster) because all
the heavy work is a Go primitive and Lua only crosses ~3 times per frame.
Toolkit in Lua (S42); Phase 8 unreordered; ADR-007 → Accepted.

**Observation (not a veto, not a `G##`):** case (b)'s p99 (~52–74 ms in
pure Go) skims/exceeds the budget, but the outlier is the **1-character**
keystroke (matches ~all of the 100k) and the cost lives in `fuzzyScore` (a
Go primitive that scans 100k), **not** in the crossing to Lua. If this
bothers production, the fix is in `enu.search.fuzzy` (parallelize the
scoring, or a minimum query-length threshold in the toolkit), not in the UI
architecture. Noted in ADR-012 as a future optimization note.

### Verification

`spike_bench_test.go`: functional tests (`-race`) of viewport/scroll (G28),
horizontal clipping, coalescing + damage tracking, SGR, Lua orchestration;
+ `TestSpikeMeasureVeto` (prints p50/p99 of both workloads, Go vs Lua, and
the verdict) + 3 benchmarks. `CGO_ENABLED=0 go build`/`go vet`/`gofmt -l`
clean; `CGO_ENABLED=1 go test -race -timeout 120s ./internal/...` green (no
regression on S01–S27). **APILevel stays at 1.** Pointer ▶ advances to
**S29** (real compositor).

## S29 — `enu.ui` real compositor (§9.1)

- **Composition model (one grid per region).** The compositor holds a
  screen grid (back/front for the diff) and a list of regions; each region
  has **its own grid** of its logical size. `blit`/`fill`/`clear` write
  into the region's grid (persisting across frames, like a window). Every
  paint composes by stacking the regions in z-order onto the screen grid,
  clipping each one to its visible rectangle. Separating content (region
  grid) from presentation (composite) makes G1 and G28 trivial and correct
  by construction.
- **G1 (resize):** a region off-screen is not touched; the composite clips
  it; it reappears when the screen grows (coords and grid intact).
- **G28 (blit = copy, never re-render):** `blit` copies the visible window
  of the Block (negative offset clips the start, excess the end); another
  offset = another copy; it never reconstructs the Block (cheap scroll).
- **Coalescing:** changes accumulate and are painted at most every ~30 ms
  (no manual flush); the run-based diff emits only what changed; an
  identical frame emits 0 bytes. In headless/test an internal (non-public)
  path is exposed to force/inspect the composed frame.
- **Headless `size()`:** with a real TTY it reads from the terminal;
  without a TTY an injectable default for tests (gating "enu.ui does not
  exist without a TTY," G20, is S32).
- **S28 spike removed:** `spike_compositor.go`/`spike_shim.go`/
  `spike_*_test.go` deleted; the model was promoted to production's
  `compositor.go`. ADR-012 retains the veto measurements.
- **Main state only (ADR-008):** all mutations under the Lua token; no own
  lock. `Region` is `ownedHandle` (reload S13 destroys it).
- **Boundary:** S30 (Region lifecycle), S31 (input), S32 (headless gating
  G20) NOT pulled forward. api.md untouched (APILevel 1).
- **Process note:** the implementation subagent wrote and verified the code
  but hung before committing and before the trail; the orchestrator
  re-verified (build/vet/gofmt/`go test -race` all green, spike retired,
  §9.1 surface exact) and completed the trail + commit + push.
  Implementation and tests are the subagent's work; the closing (trail/
  commit) was done by the orchestrator.

## S30 — `Region` lifecycle (move/resize/raise/lower/show/hide/destroy/cursor) (api.md §9.1)

A session with no deviations from the spec: §9.1 sufficed for the eight
signatures, implemented exactly over the `uiRegion`/`compositor` from S29
(api.md not extended, `enu.version.api` stays at 1; **no `G##` findings**).
Modeling decisions (where §9.1 leaves freedom) and their rationale:

- **raise/lower by reassigning `z`, not by reordering a list.** `raise()`
  sets `z = max(z of the other live ones)+1`; `lower()`, `min−1`. Discarded
  alternative: keeping a sorted list and moving the element to the end/
  front. Reassignment was chosen because the stacking criterion already
  lives in one place —`regionLess` sorts by `(z, seq)` (S29)— so a later
  `composite` or `blit` respects it with no extra state or a second
  invariant to maintain. It preserves the relative order of the rest: only
  the affected region jumps to the top or bottom. Creation `seq` still
  breaks ties on equal z (stability).

- **resize preserves content in the top-left corner.** §9.1 leaves open
  what happens to the content on resize; it was decided to **preserve the
  intersection** (copy the common (0,0) corner onto the new canvas; what
  exceeds it is discarded, the new area is background) rather than reset
  the canvas. Reason: consistency with the "region is a window" model from
  S29 —enlarging a real window does not erase what it already showed—.
  `w/h<0` → `EINVAL`, same as `enu.ui.region`.

- **hide preserves canvas and coordinates; show returns it as-is.** `hide`
  destroys nothing: it toggles a `visible` flag that `composite` checks to
  skip the region. It is the cheap symmetric of `show`; both are
  idempotent. A hidden region that held the cursor **releases** it (a
  region that is not shown cannot hold the cursor).

- **destroy: `untrack` + `release`, idempotent, later methods fail
  cleanly.** `destroy` deregisters from the per-owner handle registry (S13,
  no leak) and then does `release` (unhooks from the compositor, releases
  the cursor if it was its own, marks `alive=false`). It is **idempotent**
  (second call is a no-op). After destruction, the other methods throw
  `EINVAL` "already destroyed" via `checkRegion` —an actionable usage
  error, not a silent no-op—. Nuance: `destroy` validates the type by hand
  (not via `checkRegion`) so that its own idempotency does not throw on the
  already-dead region; the asymmetry is deliberate (a dead Region is the
  expected case for idempotency; passing something that is not a Region is
  a type error that should throw).

- **cursor: single ownership, "last one wins," released on hide/destroy.**
  The compositor tracks the cursor's owner (`cursorOwner` + LOCAL coords +
  `cursorOff` flag). `Region:cursor(x,y)` claims the cursor and **displaces
  the previous owner** (its previous `cursor()` call is lost, as §9.1
  requires: "only one region can hold it; the last call wins"). `cursor
  (nil)`/`cursor()` hides it (the region remains the owner, turned off).
  `hide`/`destroy`/reload of the owner release the cursor (`dropCursorIf`);
  destroying/hiding ANOTHER region does not touch it. The frame emits it in
  `paint` (`encodeCursor`): positions+shows (`ESC[y;xH`+`ESC[?25h`, screen
  coords = local + origin, 1-based) or hides (`ESC[?25l`) if there is no
  owner, it is off, or it **falls off-screen** (G1: the cursor is never
  positioned out of bounds). It is **damage-tracked** (`lastCursor`): a
  frame that does not change the cursor does not re-emit its sequence, so a
  frame with no changes at all still emits 0 bytes and does NOT break the
  coalescing from S29 (this was validated by re-running the S29 tests
  `TestCoalescingSingleFrame`/`TestDiffEmitsOnlyChanged`).

- **`cursor` signature.** `cursor(nil)` or `cursor()` hide it; `cursor(x,
  y)` requires both integers (`L.CheckInt(2)`/`(3)`): §9.1's signature is
  `(x, y | nil)`, not `(x)`, so a single loose integer is a usage error.

- **Main state only (ADR-008), synchronous (not ⏸, not [W]).** Like
  `blit`/`fill`/`clear` from S29: they mutate the compositor under the
  token, with no own lock. Verified with `CGO_ENABLED=1 go test -race
  -timeout 120s -count=2 ./internal/...` (green, no data races, including
  the live painter goroutine `TestUIPainterLive`).

## S31 — input (`enu.ui.on_input` / `keymap`) (api.md §9.3)

A 🔒 session (input stack + sequence resolution with timeout). No `G##`
findings: §9.3 sufficed to implement the two signatures exactly; the
sequence/timeout logic and the G30 dump are core-owned, as the contract
requires. api.md not extended (APILevel stays at 1). Decisions:

- **`keymap` as SUGAR over the same stack, not a global registry.** §9.3
  says "sugar over the stack" and "conflicts: the stack decides." I modeled
  it literally: a keymap is one more `inputHandler` in the same stack as
  `on_input` (with `maps []*seqMap` instead of `raw`). So there is ONE
  order of conflict resolution (the stack: the one on top wins), with no
  separate priority table. A global `seq -> fn` registry was discarded: it
  would have duplicated the ordering criterion and contradicted "the stack
  decides."

- **Sequence resolution = pending buffer + generation + `oneShot`.** The
  pending state (`pendingBuf`/`pendingHandler`/`pendingTimeout`) lives in
  `inputState` (main state, under the token). The timeout is a new
  ONE-shot timer (`oneShot` in `timers.go`): a pure-Go `time.Timer` whose
  goroutine, on expiry, **takes the token** and runs the callback —the same
  pattern as the S29 painter, which already takes the token to paint—. I
  did not reuse `enu.task.every` (it is periodic and its `fn` is Lua) nor an
  `enu.fs` ⏸ (input dispatch is synchronous, not a task): the timeout
  callback is pure Go and runs once. **The generation counter
  (`pendingGen`)** is the anti-race piece: a timer that already fired but
  whose sequence was resolved/rearmed before it took the token checks
  `pendingGen != gen` and does nothing. So a best-effort `stop` suffices
  (no need to wait for the goroutine).

- **Aborting a sequence = re-injecting the buffered input BELOW the owning
  keymap.** §9.3: "if the timeout passes or something arrives that does
  not continue it, whatever there is gets resolved (or the input passes
  through)." I interpreted this as: the key(s) held by the keymap are
  redispatched as loose events, but ONLY below the handler that held them
  (`dispatchFrom(ev, idx-1)`) —if they were re-injected through the whole
  stack, the keymap itself would hold the first key again and loop—. The
  current key (the one that aborted) is processed afterward from the top,
  normally.

- **Deterministic tests: `feedInput` + `feedTimeout` (internal, non-public
  paths).** The environment is headless (no TTY): there is no byte reader.
  I built the pipeline to inject synthetic events (`feedInput`) and fire
  the timeout SYNCHRONOUSLY (`feedTimeout`), so the test for "the timeout
  passes between the two g's → does not fire" does not depend on the clock
  (not flaky). The real timer is only exercised in
  `TestInputSequenceTimerLive` (with `timeout_ms=20` and polling by
  condition under the token). **What is driver vs. tested logic:** the
  DRIVER (raw mode + ANSI parsing into events, TTY reader) is S32+ (manual
  CP-7); what is 🔒 (stack + sequences with timeout + G30) is shielded here
  with injected events.

- **G30 (image paste): SYNCHRONOUS dump with a direct Go write.** §9.3/G30:
  a paste of non-text content is dumped to `enu.fs.tmpdir` and delivered as
  `paste` with `path`, not `text`; the bytes never cross into Lua
  (consistent with G11). The input event arrives SYNCHRONOUSLY at dispatch
  (under the token, NOT in a ⏸ task), so the dump cannot be an `enu.fs.write`
  ⏸. I resolved this with `writePasteImage`: reuses `fs.ensureTmpdir` (the
  machinery of `enu.fs.tmpdir`) and a direct Go `os.WriteFile`
  (`paste-N.bin`, 0600). The cost is a write of a few KB/MB for a pasted
  image, negligible compared to the human latency of pasting. If the dump
  fails, the event is delivered as an inert paste (with neither `text` nor
  `path`) and it is logged: a better an empty paste than losing the
  invariant of not crossing binary bytes.

- **`materializePaste` before dispatch; `eventTable` chooses `path` xor
  `text`.** The bytes→file conversion happens in `feedInput`, once, before
  any handler sees the event. The event's Lua table (`eventTable`) sets
  `path` if the event carries it (image) or `text` if not (text), never
  both.

- **Main state only (ADR-008).** `inputState` lives in `uiState`, under the
  token; the `oneShot` takes the token on firing. Verified with
  `CGO_ENABLED=1 go test -race -timeout 120s -count=2 ./internal/...`
  (green, no data races, no flakiness).

## S32 — rest of `enu.ui`: OSC 52 clipboard + `ui:*` events + headless gating G20 (api.md §9.2, §9, §4, §2)

### HEADLESS GATING (G20, the central decision)

§9/G20: without an interactive TTY (`enu -e`, CI, redirected output) the
`enu.ui` module **flatly DOES NOT EXIST**, and detection is `enu.has("ui")`
(never try-and-catch), the same model as the workers' caps ("ungranted
surface is not there"). I implemented it as follows:

- **`registerUI` is called only if `rt.uiActive`** (in `registerNu`). In
  headless mode `enu.ui` is neither hung off the global nor is the
  compositor built (`rt.ui = nil` via `maybeUIState`); `armPainter`/
  `stopPainter`/`Close` already tolerated `rt.ui == nil`.
- **`uiActive` is decided by `New`:** `WithForceUI(active)` overrides
  (precedence, `forceUISet`); without it, `detectTTY()`. So the `enu` binary
  (which calls `runtime.New()` with no Options) applies real gating, and
  tests force the UI.
- **`detectTTY()` requires BOTH stdout AND stdin to be a TTY**
  (`golang.org/x/term.IsTerminal`): a full-screen UI writes the render
  (stdout) and reads keys (stdin); if either is redirected there is no
  viable surface. `x/term` is pure Go (no CGO, ADR-001).
- **`enu.has` becomes per-runtime** (`rt.caps()`), not a global map:
  `"ui"` depends on the concrete runtime (uiActive). `"ui.images"`/
  `"net.tcp"` remain false (deny-by-default; the image protocol will be
  negotiated by the S33+ driver).

### Forced activation for testing (NOT breaking S22–S31)

Tests run headless (no TTY), so with real gating `enu.ui` would not exist
and the whole S22–S31 UI suite (block/region/input) would fail. The path:
the **`WithForceUI(true)`** Option, which the base harness (`newHarness`)
and the UI harnesses (`newHarnessUI`/`newHarnessBudget`) activate. I also
adjusted the few tests that construct `New(...)` by hand (`ui_test.go`) to
add it. No test was deleted: real (TTY-based) gating remains covered by
`TestGatingHeadlessNoUI` (which builds the runtime with `WithForceUI(false)`
to observe headless behavior).

### OSC 52 clipboard (`osc52.go`): driver vs. tested logic

§9.2: `clipboard_set`/`clipboard_get` "via OSC 52 when the terminal
supports it."

- **`set` is NOT ⏸**, **`get` IS ⏸**: `set` writes a few bytes and the
  terminal does not respond; `get` sends the query and **waits** for the
  response (hence ⏸, over the S04 `suspend` bridge: releases the token,
  reads in the background goroutine that never touches Lua).
- **Why OSC 52 and not a native clipboard:** "zero dependency hell"
  (ADR-001) rules out linking X11/Wayland/AppKit; OSC 52 is in-band, works
  over SSH, and adds no system dependencies. Its limitation (the terminal
  must support reading, many disable it) is modeled honestly: `get`
  returns `nil`, not an empty clipboard.
- **DRIVER vs. tested logic (as in S31):** the output is
  `uiState.clipWriter` (`os.Stdout` in production, a buffer in tests) and
  the response arrives from `uiState.clipReader` (the TTY stream provided
  by the S33+ DRIVER; nil in headless → `get` resolves to `nil`). The own,
  risky logic —encoding `set` (base64) and **parsing** the reply
  (`parseOSC52Reply`: BEL/ST terminator, ignored selector, noise tolerated,
  base64/empty/`?`-bounced→nil)— is unit-shielded with synthetic bytes
  (`osc52_test.go`). The real round trip with a live TTY belongs to the
  driver.
- **`set` does not throw on a write failure to the TTY:** copying to the
  clipboard is ancillary; an error goes to the log best-effort, it does not
  bring down the caller.

### `ui:*` events (`ui_events.go`): wired emission, source in the driver

§4: the core emits `ui:resize`/`ui:focus`/`ui:suspend`/`ui:resume`; §9.1:
size changes → `ui:resize`. The real SOURCE (SIGWINCH, focus sequences,
SIGTSTP) is the S33+ TTY DRIVER (manual CP-7). S32 wires the EMISSION via
`enu.events` and leaves the paths:

- **`resizeUI(w,h)`** resizes the compositor (clips regions, G1) and emits
  `ui:resize {w,h}` —**only if the size changed** (not a spurious event)—.
- **`emitUIFocus(b)`** → `ui:focus {focused}`; **`emitUISuspend`/
  `emitUIResume`** → `ui:suspend`/`ui:resume` (no payload).
- `ui:` is a namespace reserved for the core (§4). Emission presupposes the
  token (main state, ADR-008): the driver will enqueue the OS event to the
  loop, just as the painter takes the token to paint. All are no-ops if
  there is no UI (`rt.ui == nil`).

### Dependency: `golang.org/x/term`

`x/term v0.13.0` (direct, pure Go). The latest (v0.44.0) requires go >=
1.25; it was pinned at v0.13.0 to avoid bumping the repo's toolchain (go
1.24.7) and to reuse the x/sys v0.13.0 already present. `go mod tidy`
consistent; `CGO_ENABLED=0 go build ./...` green.

### No API extension

`api.md` was NOT touched, nor `enu.version.api` (APILevel stays at 1): the
`clipboard_set`/`clipboard_get` signatures, the `ui:*` events, and the
gating were already specified. Per-runtime `enu.has` is implementation, it
does not change the `enu.has(cap) -> boolean` signature. No `G##` findings.
`CGO_ENABLED=1 go test -race -timeout 120s -count=2 ./internal/...` green,
no data races.

## S33 — bare-runtime screen (api.md §14, G21; closes Phase 6, manual CP-7)

When nu starts with an interactive TTY and NO active plugin, the kernel
paints —BEFORE running any product Lua— a FIXED screen made only of its own
capabilities: version + API level (`enu.version`), config and plugin paths
(`config.dir`, `pluginDirs`), the catalog of AVAILABLE embedded extensions
(`embeddedNames`, embed.go), and the actions (activate the official set /
activate loose ones / quit). FIXED render (Block over the S29 compositor),
pre-Lua, with no product widgets or logic (philosophy §2: the kernel talks
about its own business). Does NOT extend `api.md` (G21 was already in §14;
APILevel stays at 1); not one new Lua surface function. No `G##` findings.
All in `bare_screen.go` + `bare_screen_test.go`; minimal wiring in
`main.go`.

### Condition and where it is wired

The screen is shown IFF `rt.uiActive` (interactive TTY, or `WithForceUI` in
tests) AND there are no active plugins (`loader.hasActivePlugins`: `len
(enabled) > 0` or some subdir with `plugin.toml` in the plugin dirs —a
LIGHT check, without materializing embedded ones or validating the graph,
which is the real `Boot`'s job—). I chose to wire it in **`main`**, not
inside `Boot`: `main` (without `-e`) checks `rt.BareScreenActive()` and, if
so, paints and flushes the lines; if not, the usual canonical startup
continues. This way NO S01–S32 test that calls `rt.Boot()` directly changes
behavior (Boot keeps loading plugins + user init + `core:ready`): the
screen is a binary-level decision, which is where the TTY lives. Without a
TTY (`enu` without `-e` in CI) → `BareScreenActive` is false → usage is
printed (starts bare), confirmed with the binary.

### Actions: activate → write enu.toml → continue Boot (no network)

`activateAndBoot(names)` writes `names` into `plugins.enabled` of
`config.dir()/enu.toml`, reloads the config in the loader, resets `booted`,
and calls **`rt.Boot()`** (not `ldr.Boot()`, so the compositor's painter is
also armed: after activation, the extensions' UI must repaint).
`ActivateOfficial()` = `activateAndBoot(embeddedNames())`; activate loose =
`activateAndBoot([]string{"repl"})`. No network: the embedded ones ship in
the binary (ADR-010, reuses `extractEmbedded`/`discover` from S12). The
KEYBOARD choice is wired by the S33+/manual-CP-7 TTY driver; the logic
remains invocable through a testable internal path.

### Writing enu.toml: preserve the rest of the file, atomic

`writeEnabledPlugins` reads the existing `enu.toml` into a generic
**`map[string]any`** (NOT into `runtimeConfig`, which would lose the keys
the core ignores for forward-compat), sets `plugins.enabled` while
preserving the rest of `[plugins]` and the other unknown sections/keys, and
rewrites EVERYTHING with BurntSushi **atomically** by reusing S14's
`writeAtomic` (temp file in the same dir + `rename`: never leaves a
half-written `enu.toml`). A MALFORMED `enu.toml` is **not blindly
overwritten** (it would lose user config): it returns an actionable
`EINVAL` and leaves the file intact. A missing file is created (first
startup); `config.dir` is created if missing.

### CP-7 (closes Phase 6) — MANUAL with a TTY, NOT runnable in headless CI

CP-7 is a **manual smoke test with a TTY** (start with no plugins → see the
screen; activate the official set; a plugin paints streaming markdown and
responds to a keymap; resize; paste an image → path). In this environment,
which is **HEADLESS with no TTY**, the interactive part could NOT be run
(environment limitation). What IS covered by automated tests
(`bare_screen_test.go`):

- **Condition** TTY+no-plugins (with UI and no plugins → activates; without
  UI → does not; with an embedded one activated → does not; with a
  disk plugin → does not).
- **Content / FIXED render to buffer**: the model and the **compositor
  grid** (`back`) contain version+API, paths (config and plugin dir), the
  catalog of embedded ones (`example`), and the actions; the emitted ANSI
  frame is not empty.
- **Activate official set → enu.toml → Boot**: writes `plugins.enabled` with
  the catalog, and the Boot that follows loads the embedded one with
  `source="builtin"` and runs its init (no network).
- **Activate loose** (`example`): writes only that one.
- **Preserve config**: writing `enabled` preserves `dirs`, `watchdog`, and
  unrelated keys/sections; the result is a valid `enu.toml`.
- **Malformed enu.toml** is not overwritten (EINVAL, file intact).
- **No regression**: headless `enu -e` still works; `enu` without `-e` and no
  TTY prints usage.

STILL PENDING a human with a TTY (manual CP-7): KEYBOARD interaction to
choose an action, visible token-by-token streaming, and VISIBLE resize/
paste. The screen render, the condition, and the activate→enu.toml→Boot
chain are automated; the board marks `[x] Phase 6` with this note.

`CGO_ENABLED=0 go build ./...` and `go vet ./...` green; `gofmt -l` clean;
`CGO_ENABLED=1 go test -race -timeout 120s -count=2 ./internal/...` green,
no data races (no regression on S01–S32).

## S34 — `enu.worker.spawn` + caps (G6) + send/recv with bounded queues (api.md §13, 🔒; opens Phase 7)

First session of Phase 7 (Workers). Implements **opt-in parallelism**
(ADR-008): `enu.worker.spawn` spins up a NEW, isolated Lua state in its own
goroutine, with its own scheduler, communicating with the parent via
bounded queues of copied JSON-able messages. `caps` filtering (G6) and
queue backpressure are the 🔒 logic.

### The worker's mini-runtime (G15): scheduler reuse WITHOUT a watchdog

A worker is a "trimmed" `*Runtime` (`newWorkerRuntime`, worker_registry.go):
the same engine as the main one —sandboxed Lua state (`applySandbox`) + its
own scheduler (`newScheduler`)— but with `isWorker=true` and, above all,
**zero slice budget** → `armWatchdog` is a no-op (G15: workers exist to
burn CPU; control is `terminate()` and `caps`, not the watchdog). Key
decision: **nothing in the event loop is reimplemented**; the worker REUSES
the S04 machinery (token/`suspend`/`runTask`/`waitIdle`). The worker's
module runs **as a task** (not as a chunk over `host`): a chunk could not
suspend (`requireTask` requires `L != rt.L`), and the worker's natural
pattern is an `enu.worker.parent.recv()` (⏸) loop. That is why `run` does
`s.spawn(require(module))` and then `waitIdle`. A worker is always headless
(`uiActive=false`, `ui=nil`): no `enu.ui`.

### Caps filtering (G6): deny-by-default, two granularities

`registerWorkerNu` registers the WHOLE [W] surface in the worker's `enu` by
reusing the same `registerXxx` functions as the main one (a single source
of truth), and THEN **prunes the tree** by `caps` (`pruneByCaps`).
Register-then-prune is simpler and more robust than registering
function-by-function. Three granularities of decision per module `M`:
- `caps["M"]` (e.g. `"fs"`) → whole module kept.
- some `caps["M.fn"]` (e.g. `"fs.read"`) → only those functions; if none
  exist, M is removed entirely.
- neither one nor the other → M removed (deny-by-default).

No `caps` (`capsGiven=false`) → the whole [W] API. With `caps={}` (empty) →
almost nothing. What is not granted **DOES NOT EXIST** (it is `nil`), the
same model as `enu.ui`'s gating (G20): it does not "throw EACCES," the
surface simply is not there, so a plugin cannot even name it.
**Deny-by-default for new surface**: a function added later to a module is
NOT granted by an old function-granularity `caps` (only what was enumerated
survives); a whole `"M"` does grant M's future additions, by design. NOT
[W] (never reach the worker): `enu.ui`, `enu.events`, `enu.fs.watch`,
`enu.worker.spawn` (no nesting), nor `enu.plugin` (§16). `enu.version`/
`enu.has` and `enu.worker.parent` always go through (they are not prunable
surface: capability detection and the channel with the parent).
`enu.config.dir`/`data_dir` are [W] (§14).

### Bounded queues / backpressure (§13)

Two bounded Go channels (`workerQueueCap=16`) per worker: `toWorker` (parent→worker) and
`fromWorker` (worker→parent), plus a `done` that closes on termination. `Worker:send` (parent)
and `enu.worker.parent.send` (worker) are ⏸: they enqueue by SUSPENDING if the queue is full
(backpressure, §13/§8) —the actual send happens OUTSIDE the token, in the background goroutine
of the `suspend` bridge, so another task of the SAME state progresses while the send waits for
room—. Unlike the streams of §8 (which fail with `EIO` on overflow), the worker's send
**suspends** (the queue is a paced rendez-vous point, not a buffer that overflows). `recv`
suspends until a message arrives. A closed endpoint (`done`): `send` → `ECLOSED`; `recv`
→ `nil` (end of channel, consistent with `Ws:recv`), draining whatever was still queued first.

### Copying JSON-able messages (ADR-008 isolation)

NOTHING Lua crosses between states. `Worker:send` converts the Lua value to its neutral Go
representation with `luaToGo` (the codec from §12/S18) UNDER THE SENDER'S TOKEN, BEFORE
suspending —which validates that it is JSON-able and rejects closures/userdata/threads/Blocks
with `EINVAL`—; the neutral Go value (not an LValue) is the only thing that crosses the channel
between goroutines; the receiver reconstructs it with `goToLua` under ITS token. So a table gets
COPIED (mutating it after sending does not affect the other side) and the two Lua states never
share memory. `useNull=false`: messages are ordinary JSON-able values, not JSON documents (the
`enu.json.NULL` sentinel is per-state userdata and could not cross either way). This is where
"zero data races" comes from with TWO schedulers: each `*lua.LState` is only ever touched by its
own goroutine under its own token; the crossing is copy + channel happens-before.

### Immediate, leak-free `terminate` (fix from the S34 review)

`Worker:terminate()` must be **immediate and safe** (§13). S34's first cut only closed `done`,
which only `send`/`recv` on the queues observe: a worker task suspended in
`enu.task.sleep`/`http`/`proc`/`await`/... did NOT wake up, so `driveUntilDone`→`waitIdle` blocked
until quiescence and a `sleep(60000)` would hang for ~60 s. Result: after `terminate()`+`Close()`
the worker's goroutine was still alive (a LEAK); since the worker shares the parent's
`log`/`data_dir` and its `Close` does not close the log (`isWorker`), the leaked goroutine kept
touching the dataDir while the test was deleting it → intermittent "directory not empty" failures
under `-race -count`. That was the review's blocker.

**The fix, reusing S07/S08's cancellation substrate:** `terminate()` now, BEFORE closing `done`,
calls `scheduler.cancelAllTasks()` on the WORKER's scheduler. That:

1. Closes a new scheduler channel `cancelAll` (idempotent, `cancelAllOnce`). `suspend` and
   `taskAwait` observe it in their `select` **in parallel with the per-task `cancelCh`**: any
   task suspended in ANY ⏸ wakes up HERE and aborts via the same path (`abort` → `cleanup`) as an
   individual cancellation. It is S07/S08's cooperative cancellation triggered on ALL live tasks
   at once, instead of one at a time by its handle. In the PRINCIPAL state `cancelAll` is never
   closed (its end of life is `Close`, which cuts background resources, not Lua tasks); only a
   worker closes it.
2. Cancels the `context` of every live task (iterating `coToTask`): it is the only thing that
   breaks a slice of **pure CPU** that never suspends (a worker has no watchdog, G15). This way
   even a `while true do end` does not leave the worker's goroutine hanging.

With (1)+(2) `waitIdle` reaches quiescence immediately; the worker's goroutine
(`driveUntilDone`+`shutdown`) closes its `*lua.LState`, marks `terminated`, and dies. It STILL
waits for quiescence before closing the state —closing it mid-task would be a race— but now
quiescence arrives at once, not when the `sleep` expires. `terminate()` from Lua does NOT block
(it is "immediate"). Thread safety: `cancelAllTasks` is called by the PARENT's goroutine (without
the worker's token), but it only closes a channel and calls `context.CancelFunc` (safe from any
goroutine); it does not touch Lua or the tasks' abort fields (`aborting`/`reason`/`canceled`),
which the goroutines themselves keep writing under their own token upon waking —S08's invariant
intact—.

**The parent's `Close`/`stopAllWorkers` WAIT for the worker's goroutine.** After `terminate`,
the parent calls `w.wait()` (← `terminated`): it does not return control until the worker's
goroutine closed its `*lua.LState`. Without that wait, the leak/cleanup race described above
remained. `terminate` is fired to ALL workers first and waited on afterward (shutdown in
parallel). The principal state NEVER touches the worker's Lua: the worker's goroutine owns its
Lua and is the one that calls `wrt.Close()`. The log is SHARED with the parent: `Runtime.Close`
does NOT close it when `isWorker`.

**BOUNDARY with S35** (NOT in S34): `Worker:on_message` (mutually exclusive with `recv`, G8 →
`EINVAL` on the spot) and the thorough test of several tasks/timers/futures INSIDE the worker.
The mini-runtime already SUPPORTS them (it reuses the scheduler), but their exhaustive validation
is S35.

### No API expansion, no findings

`§13` was EXACTLY enough: not one extra public function; `APILevel` stays at 1 (§13 was already
in api.md). No `G##` findings: the Go/Lua split and S04's `suspend` bridge provided everything.

### Tests 🔒 (worker_test.go), naming G6/G15

- **caps G6** (`TestWorkerCapsTwoGranularities`): the worker INSPECTS its own API and reports
  back to the parent. Four cases: no caps (the whole API [W]); `caps={"fs"}` (all of fs, no
  http); `caps={"fs.read"}` (fs.read YES, fs.write NO, http NO); `caps={}` (almost nothing). In
  all of them: `ui`/`events`/`worker.spawn` absent (§16), `version`/`worker.parent` present.
- **backpressure** (`TestWorkerBackpressure`): a worker that does not consume fills the queue;
  the producer SUSPENDS (does not complete the 1000 sends) and a witness task of the parent
  PROGRESSES (the loop does not freeze).
- **copy** (`TestWorkerMessageCopied`): the parent mutates its table after sending it; the
  worker sees the value at send time (7), not the mutation (999).
- **non-serializable** (`TestWorkerSendNonSerializable`): sending a function → `EINVAL`.
- **round-trip** (`TestWorkerRoundTrip`): parent send → worker parent.recv → worker
  parent.send → parent recv (echo with transformation).
- **no watchdog G15** (`TestWorkerNoWatchdog` functional: a long CPU computation COMPLETES;
  `TestWorkerSchedulerHasNoWatchdog` structural: `wrt.sched.budget<=0` even though the parent has
  a watchdog — does not depend on timing, robust under `-race`).
- argument validation (`TestWorkerSpawnValidation`), `requireTask` (`TestWorkerSendRecvRequireTask`),
  `recv` after `terminate` → `nil` (`TestWorkerRecvAfterTerminate`).
- **immediate, leak-free `terminate` (review)** (`TestWorkerTerminateInterruptsSleep`): a worker
  suspended in `enu.task.sleep(60000)` is cut off on the spot by `terminate()` —`terminate`+`Close`
  complete WELL under the sleep time— and `runtime.NumGoroutine()` after `Close` returns to the
  level prior to spawn (the worker's goroutine ended, it did not remain hanging around touching
  the `data_dir`/`log`). (`TestWorkerTerminateInterruptsCPULoop`): a worker in a pure-CPU loop
  (`while true do end`, with no suspension point) also gets cut off —context cancellation—
  without `terminate`+`Close` hanging.

`CGO_ENABLED=0 go build ./...`, `go vet ./...` green; `gofmt -l` clean;
`CGO_ENABLED=1 go test -race -timeout 120s -count=4 ./internal/...` and `-count=8 -run Worker`
green, **no data races, no flakiness, no cleanup failures ("directory not empty")** (it is the
most demanding race test yet: TWO scheduler goroutines in parallel). No regression on S01–S33.
Pointer ▶ stays at **S35**.

## S35 — `Worker:on_message` (mutually exclusive with `recv`, G8) + tasks/timers/futures inside the worker + `terminate` (api.md §13, 🔒; closes Phase 7 — CP-8)

S35 closes Phase 7. The surface feature is `Worker:on_message`; the rest of the scope
(tasks/timers/futures inside the worker, robust `terminate`) was already left implemented by
S34, and S35 SHIELDS it with tests. Not one extra public function: §13 was already in `api.md`,
`APILevel` stays at 1. No `G##` findings.

### `on_message` model: background drainer + delivery in the principal state

`Worker:on_message(fn) -> Sub` is the CALLBACK ALTERNATIVE in the PRINCIPAL STATE to
`Worker:recv`. The design question was "how does one drain the worker→parent queue and call
`fn(msg)` in the principal state without leaks". The answer, consistent with the model and with
no new piece, mirrors `enu.task.every`'s ticker (timers.go):

- A **background-goroutine drainer** (`luaWorker.drainOnMessage`) does a `select` over
  `fromWorker`/`done`/`stopCh`. For each message it takes the parent's token
  (`acquire`/`release`), checks `sub.live`, and calls `fn(msg)` on an ephemeral thread of the
  principal state under `pcall` (`scheduler.callOnMessage`), reconstructing the value with
  `goToLua` UNDER THE TOKEN —same as `Worker:recv`—. No `LValue` crosses goroutines (ADR-008
  isolation); the neutral Go value already arrived copied from the worker's side.
- The drainer is NOT a task and does not count toward the parent's quiescence (it is a
  background facility, like `every`): its end of life is `Sub:cancel` (`stopCh`), `terminate`
  (`done`), or `Close`. **Consequence for the tests:** delivery is ASYNCHRONOUS with respect to
  `eval` (which only waits for foreground tasks), so the delivery tests POLL (`pollEval`) until
  the messages arrive.
- **Draining what's queued before exiting via `done`** (same as `recvOnBoundedChan`): Go's
  `select`, with both `done` and `fromWorker` ready, picks at random; that's why, upon waking via
  `done`, a NON-blocking pull from `fromWorker` is attempted —if the queue is empty, the sender
  is really done—. Without this, a worker that sends N messages and terminates would lose
  whichever ones came after the first (uncovered by `TestWorkerOnMessageDelivery`: it delivered 1
  out of 5). `Sub:cancel`, on the other hand, does NOT drain: it is an explicit cutoff.
- A `fn` that throws is isolated in the log (best-effort, ADR-008) and draining CONTINUES with
  the next message (`TestWorkerOnMessageHandlerThrows`).

`on_message` is an **owner-scoped handle** (`workerSub` implements `ownedHandle`, S13): `reload`
releases it via `releaseOwnerHandles`; a manual `cancel` does `untrack` so it does not linger in
`ownerHandles`. Principal state only (not [W]: there is no `Worker` inside the worker).

The `Sub` from `on_message` is its OWN handle type (`workerSubTypeName`, carries `*workerSub`),
NOT the `Sub` from `enu.events` (carries `*subscriber`): same public method `:cancel()`, different
type. A new type was chosen instead of reusing the events one because `subCancel` validates the
concrete userdata type (`*subscriber`) and mixing types would be fragile.

### G8 exclusivity (the 🔒 part): explicit rejection ON THE SPOT, never silent priority

`on_message` and `recv` on the SAME worker are MUTUALLY EXCLUSIVE. The mechanics, all under the
parent's token (which serializes it, no lock needed):

- `Worker:recv` carries a `recvPending` counter in the `luaWorker`: `++` UNDER THE TOKEN before
  suspending, `defer --` upon re-acquiring. The `defer` runs even if `terminate` aborts the
  suspended task (`suspend`'s `abort` panics with the token re-acquired, and Go's `defer`s run
  while unwinding): this way `recvPending` never stays inflated.
- `on_message` stores `onMsg` (the active `Sub`).
- Registering `on_message` with `recvPending > 0` → `EINVAL` on the spot; doing `recv` with
  `onMsg != nil` → `EINVAL` on the spot; a SECOND `on_message` while one is active → `EINVAL`
  (a single logical consumer of the channel). One is NEVER picked while the other is silently
  ignored.
- `Sub:cancel` sets `onMsg = nil` (in `release`): frees the worker to go back to `recv`.

### tasks/timers/futures inside the worker (G15) and `terminate`: shielded, nothing to fix

The worker's mini-runtime (S34's own scheduler, reusing S04) already supports the whole of
`enu.task` [W]. S35 SHIELDS it with a test (`TestWorkerInternalTasksTimersFutures`): a worker runs
several tasks (`spawn`/`await`), a `future` (`set`/`await`), `sleep`, and a periodic `every`,
WITHOUT a watchdog. There was no need to touch the worker's scheduler. `terminate` was made
immediate and safe in S34 (`cancelAllTasks` + `Close` waits for the goroutine);
`TestWorkerTerminateDoesNotAffectParent` confirms idempotency and that the parent keeps running.

### CP-8 (closes Phase 7)

`TestCP8WorkerIndexesRepo`: a worker with `caps={"fs.read","search"}` indexes a test repo
(walks it with `enu.search.files`, reads with `enu.fs.read`) and returns a digest to the
principal via `send`/`recv`; INSIDE the worker `enu.fs.write` and `enu.ui` do NOT exist
(deny-by-default, G6, checked with `assert` from inside the worker itself); a second worker
`terminate`-d midway does not affect the parent. The backpressure of `send` when the bounded
queue fills up is covered by `TestWorkerBackpressure` (S34), consistent with CP-5; it is not
duplicated.

### Result

`CGO_ENABLED=0 go build ./...` and `go vet ./...` green; `gofmt -l` clean;
`CGO_ENABLED=1 go test -race -timeout 120s -count=2 ./internal/...` green, and
`-race -count=5 -run Worker`/`-run 'CP8|OnMessage'` green, **no data races, no flakiness**
(delivery of `on_message` is timing-sensitive —background drainer + token— and held up under
polling with `-race`). No regression on S01–S34. Closes Phase 7 (Workers); Phase 8 starts.
Pointer ▶ advances to **S36**.

## S36 — official `providers` extension (TOML registry + adapter contract + `approx_tokens`) (providers.md)

First extension of **Phase 8**: the kernel is no longer touched, this is **Lua written over the
frozen public API** (ADR-003, no kernel privilege; the core does not know what a provider is).
The contract is [providers.md](providers.md), not api.md.

### Extension structure (embedded plugin)

Embedded plugin under `internal/runtime/embedded/providers/`, materialized and loaded by the
loader like any plugin (S12's mechanism, ADR-010: INACTIVE by default):

- `plugin.toml` — `name = "providers"`, `version = "0.1.0"`. No `requires` (it does not depend
  on another plugin; it depends on core primitives, not extensions).
- `init.lua` — only WIRES things up: `require("providers")` and registers the official
  adapters. In S36 only the `stub`; the real `anthropic` (S37) will be added here the same way:
  `providers.register_adapter("anthropic", ...)`.
- `lua/providers/init.lua` — the public module (`require("providers")`): registry reader,
  `resolve`/`list`/`register_adapter`/`approx_tokens`/`reload`.
- `lua/providers/adapter_stub.lua` — a STUB adapter that materializes and tests the §3 contract
  against a simulated request (no network).

**Why a `require`-able module and not a `enu.providers` namespace:** the core reserves the `enu`
namespace; extensions expose their API via `require(<plugin-name>)` (api.md §14, convention:
namespace = plugin name). The agent and the UI will consume `require("providers")`. The
extension's event namespace would be `providers:` (no events are emitted in S36, but it is
reserved by convention).

### Interpretation decisions for providers.md

1. **`providers.toml` is read lazily, which is why `resolve`/`list` SUSPEND (⏸).**
   providers.md §1 says it "lives in `enu.config.dir()`" but does not fix *when* it is read. It is
   read with `enu.fs.read` (⏸, api.md §5) the first time anyone resolves or lists, and is cached;
   `reload()` invalidates the cache. A consequence inherited from the API: since `enu.fs.read`
   suspends, `resolve`/`list` only run inside a task — which is exactly the context of the
   agent's loop. It is consistent with the rest of the API (IO = ⏸) and requires no new
   primitive.

2. **Missing `providers.toml` = empty registry, not an error.** A freshly installed nu with no
   models configured must start clean; `list()` returns `[]`. `ENOENT` (missing → empty) is
   distinguished from a real IO failure (`EACCES`/`EIO`, which propagates) by the `code` of
   `enu.fs.read`'s structured error.

3. **Registry errors = actionable `EPROVIDER`** (providers.md §3 coins `EPROVIDER`;
   CLAUDE.md/api.md §1.4: extensions coin their own with the same shape). Malformed TOML, a
   provider without `adapter`, a model without `id`, a nonexistent model/ref, an unregistered
   adapter → `EPROVIDER` with `detail` and a message that names the provider/ref. Validation of
   the API's own *arguments* (`approx_tokens(123)`, `resolve("")`) uses `EINVAL` (it is a caller
   error, not a provider error).

4. **`api_key` ALWAYS from the environment** (providers.md §1: "never the key in the file"). It
   is read with `enu.sys.env(prov.api_key_env)`. If the provider does not declare `api_key_env`
   (e.g. local Ollama), the `config` goes without `api_key` and it is not an error: the adapter
   decides whether it needs it.

5. **Adapter resolution by name via `require`** (providers.md §1/§4: the TOML can declare
   `adapter = "my-plugin/corp-gateway"`). `get_adapter` first looks at the live registry
   (official + `register_adapter`) and, if not found there, tries `require(name)` (resolvable
   against the plugins' `lua/` paths, api.md §14) and validates its shape. Caches the result.
   `register_adapter` with an already-registered name REPLACES it (a plugin can intentionally
   override an official adapter); it is not an error.

6. **`approx_tokens` counts BYTES, `ceil(#s/4)`** (providers.md §4, G23). `#s` in Lua is length
   in bytes, which is the best approximation of BPE tokenization over mixed text and what the
   core used to do. `ceil` (integer arithmetic `floor((n+3)/4)`) so as not to underestimate;
   empty string = 0. It is a heuristic, not exactness — that's what the adapter's `count_tokens?`
   is for.

7. **The STUB deliberately declares `caps.tools = false`** to be able to exercise "declared
   degradation" (§3 obligation 5: a request with tools + an adapter without support →
   `EINVAL`, not silent simulation). Its `stream` returns a Lua iterator (a function that
   yields one `Event` per call and `nil` when exhausted) — the same protocol as
   `Stream:events()` (api.md §8) that S37's real adapter will wrap. It emits
   `text`,`text`,`usage`,`done`, with `done` carrying the assembled canonical `Message` (§2.3):
   the agent does not re-assemble deltas.

### Finding (completeness corollary) — RESOLVED without touching api.md

`enu.toml.decode` **was stringifying arrays-of-tables** (`[[providers.x.models]]`), the CENTRAL
format of `providers.toml`. Cause: BurntSushi/toml, when decoding an array-of-tables into
`map[string]interface{}`, delivers the concrete type `[]map[string]interface{}` (not the "open"
`[]interface{}`); the `goToLua` bridge (codecs.go, S18) only accounted for `[]interface{}` and
`map[string]interface{}` and fell through to the stringification `default` (`models` came out as
the string `"[map[id:big-1]]"`). Without this, the extension was **unbuildable** on top of the
public API (philosophy §2).

**It is not a gap in api.md**: the documented signature `enu.toml.decode -> v` already promised
to convert the document into a Lua table —including its arrays-of-tables—; it was a bug in the
codec's *implementation*, not in the spec. The fix is therefore MINIMAL and in the codec, not in
api.md (which remains INTACT): a reflection-based fallback in `goToLua` for slices and maps of
any concrete type (it also covers `[]string`, `map[string]string`, etc., robust against any
library), with sorted keys for determinism. Shielded by `TestTOMLDecodeArrayDeTablas`
(codecs_test.go), naming the case. It is the only line of kernel Go touched, justified by "the
bare minimum needed for the extension to work".

### Result

`CGO_ENABLED=0 go build ./...` and `go vet ./...` green; `gofmt -l` clean;
`CGO_ENABLED=1 go test -race -timeout 120s -count=2 ./internal/...` green. Tests in
`providers_test.go` (TOML registry, stub against simulated request, approx_tokens) +
`TestTOMLDecodeArrayDeTablas`. No regression on S01–S35. Pointer ▶ advances to **S37** (real
`anthropic` adapter, depends on S20 and S36).

---

## S37 — Anthropic adapter (first real dialect); CP-9

I implemented the `anthropic` adapter (providers.md §3) as the module
`internal/runtime/embedded/providers/lua/providers/adapter_anthropic.lua`, registered from
providers' `init.lua` with `register_adapter("anthropic", ...)`. The scope, the DoD, and the
contract (providers.md §2/§3) left almost no room; these are the interpretation decisions I made
where the contract was not literal.

### How I translate the Anthropic dialect to the canonical one

**Canonical request → Messages API.** `to_wire` maps block by block: `text`→text,
`image`→`image {source: base64}`, `thinking`→thinking, `tool_call {id,name,args}`→`tool_use
{id,name,input}`, `tool_result {id,content,is_error?}`→`tool_result {tool_use_id,...}`.
`system` goes to Anthropic's `system` field; `tools {name,description,schema}` to
`{name,description,input_schema}`; `thinking {budget}`→`{type="enabled",budget_tokens}`.
`max_tokens` is MANDATORY in Anthropic: if the canonical request doesn't carry it, it falls back
to the resolved ModelInfo's `max_output` and, as a last resort, to 4096. The key goes in
`x-api-key` (NOT `Authorization: Bearer`) plus `anthropic-version: 2023-06-01`.

**`meta` round-trip (§2.2 meta rule / §3 obl. 4).** Each block's opaque `meta` is merged with
`pairs` into the wire block. For `thinking`, the `signature` travels in `meta` (the adapter sets
it when assembling the block from `signature_delta`) and is reinjected as-is in subsequent turns;
for others, it covers `cache_control`/internal ids without contaminating the canonical model.

**SSE → canonical stream (§2.3).** A **state machine keyed by block INDEX** over
`Stream:events()` (S20). The case that forces state is **tool_use input**: it arrives chunked in
`input_json_delta.partial_json`; I accumulate it as text and decode it with `enu.json.decode` WHEN
CLOSING the block (`content_block_stop`/`message_stop`). Malformed args JSON does not abort the
stream: the canonical block ends up with `args = {}` (the agent sees it; the adapter does not
make things up, §3 obl. 3). `stop_reason` mapped: `tool_use`→"tool_calls",
`max_tokens`→"max_tokens", `refusal`→"refusal", everything else→"end". `ping` is ignored
(keep-alive). The iterator returns one Event per call and CLOSES with `done {stop_reason,
message}` carrying the assembled Message (§2.1): the agent does not re-assemble deltas.

**Specific decisions:**
- **Double `usage`.** I emit an early `usage` at `message_start` (input_tokens/cache_read,
  useful for the UI's context-fill display) and another final one at `message_delta`
  (output_tokens). Both are valid per §2.3 (the `usage` event is not unique); the type sequence
  reflects it.
- **Errors: two paths to EPROVIDER (§3 obl. 2).** HTTP status ≥ 400 (data, api.md §8 does not
  throw) → `EPROVIDER` with `detail.status`/`provider_code`/`retryable`, reading the JSON error
  body with `chunks()` (on an HTTP error Anthropic sends JSON, not SSE); 429 and 5xx are
  retryable. An `error` event in the middle of the SSE → `EPROVIDER` with `provider_code` and
  `retryable` (overloaded_error/api_error are retryable). Marking `retryable` is the only failure
  intelligence the contract asks for; retrying is the agent loop's job.
- **`count_tokens?`.** Anthropic has an exact endpoint (`/v1/messages/count_tokens`), but I use
  S36's `approx_tokens` heuristic over system + text/thinking blocks: no network needed, and
  sufficient for the PRIOR estimate (providers.md §5: the source of truth for context fill is the
  turn's own `usage`). The exact endpoint remains a future improvement.

### CP-9 (hot path, perf veto milestone)

Since there is no network, I RECORDED a realistic Anthropic SSE (`recordedSSE` in
`providers_anthropic_test.go`: message_start with usage, ping, thinking with signature, markdown
text in 3 deltas, tool_use with input chunked across two `input_json_delta`s, message_delta with
usage/stop, message_stop) and I serve it from a local `httptest` with per-line flush (S20's test
pattern). `TestCP9CaminoCaliente` runs the COMPLETE hot path: one conversation turn → the adapter
consumes the SSE via `enu.http.stream` → emits the canonical stream → for each text delta I
recompose the accumulated markdown with `enu.text.markdown` (streaming-safe, S23) and blit it to a
region (`Region:blit`, S29).

**Render verification decision:** the Block is OPAQUE (api.md §9.2: only `.width`/`.height`, not
its content). To confirm that the final render corresponds to the COMPLETE markdown without
accessing its interior, I compare its height against a fresh render of the entire text (they
match if streaming accumulated correctly) and check that it is multi-line (heading + body). The
textual content is already validated separately (the `done`'s assembled Message).

**Need for `WithForceUI(true)`:** `bootWithToml` does not force the UI, so in the headless test
environment `enu.ui` does not exist (G20 gating). To exercise the hot path's blit, `bootAnthropic`
boots the runtime with `WithForceUI(true)` (the same device as `newHarness`, S32); the REAL
TTY-based gating still applies to the binary.

**Observed fluidity (measurement):** all the hot path's heavy work —SSE parsing, JSON decode,
markdown render, blit— is Go primitives; Lua only orchestrates the delta loop (reusing S28's
lesson/ADR-012). The `Anthropic|CP9` suite completes in ~0.06 s and the full
`-race -count=2 ./internal/...` in ~50 s with no data races. The hot path in Lua is
**acceptable**: there is no CPU burning in Lua, the perf veto (limitation #8 of
modelo-ejecucion.md) does not trigger.

### Finding

None. The public API was exactly enough (`enu.http.stream`+`Stream:events()` §8, `enu.json` §12,
`enu.text.markdown` §10, `enu.ui.region`/`Region:blit` §9). api.md INTACT; APILevel stays at 1.
Pointer ▶ advances to **S38** (sessions extension; depends on S14, S16).

## S38 — Sessions extension (JSONL, lockfiles) + enu.sys.pid (G32, APILevel 1→2)

**Finding G32 (completeness corollary).** The lockfile from `sesiones.md §6` records the
writer's identity `{pid, hostname, started}` with the OWN process's pid, but the public API did
not expose it: `enu.sys` provided platform/env/setenv/now_ms/mono_ms/hostname and
`enu.proc.alive(pid)` validates OTHER processes' pids, not one's own. It is the loose end of G17
(which closed `fs.write{exclusive}`, `proc.alive`, and `sys.hostname` but forgot one's own pid).
The official `sessions` extension was unbuildable without this → a finding, not a shortcut.

**Resolution (design flow: docs first, then code).** A pure addition to the sacred surface:
`enu.sys.pid() -> integer` [W] (not ⏸; a local query with no IO, like `hostname`/`now_ms`), a
wrapper around `os.Getpid()`. Since it is the FIRST addition after the freeze, `enu.version.api`
goes from 1 to 2 (api.md §17/§2; `APILevel` in `nu.go`). G32 RESOLVED in `problemas.md`; api.md
§7 and §16 updated; `sesiones.md §6` uses `enu.sys.pid()`. It is a strict addition: it does not
change any existing signature (ADR-003).

**Decisions for the `sessions` extension.**
- **Session id**: ms timestamp (fixed-width hex, sorts lexicographically = temporally) + random
  suffix. The PRNG is seeded ONCE with `now_ms` + `pid` (without a seed, gopher-lua would give
  the same sequence across boots → two processes at the same ms would collide on the suffix; the
  pid separates them).
- **Lockfile** (§6, G5): `<session>.jsonl.lock` with `fs.write{exclusive}`; contents
  `{pid=enu.sys.pid(), hostname, started}`. Conflicts resolved by inspection: same hostname + dead
  pid (`proc.alive`=false) → orphan, silently reclaimed; live pid → ESESSION busy; different
  hostname → ESESSION foreign (not verifiable remotely). Released by `enu.task.cleanup`.
- **read_only** does not take a lock (several concurrent readers). Error code `ESESSION`
  (ADR-009 shape, coined by the extension).
- **replay** discards the last line if it is truncated (a crash mid-append): JSONL is
  append-only and `fs.append` writes a complete line, so only the last one can be split.

**Process note.** The implementation subagent left the code and docs written and the suite
green, but stopped before the `git commit`. `go build`/`go vet`/`gofmt` and
`go test -race -timeout 120s -count=2 ./internal/...` were verified (all green, APILevel 2) and
it was committed/pushed after completing S38's logbook row, this entry, and the PRNG seed.

## S39 — official `agent` extension (headless engine: turn, tools, permissions, hooks, `agent:*` events); CP-10 (agente.md)

Fourth link of Phase 8: the harness's **headless engine**, pure Lua over the frozen public API
(ADR-003) and over the `providers` (S36/S37) and `sessions` (S38) extensions. Embedded plugin
`internal/runtime/embedded/agent/` (`plugin.toml` name="agent", `requires=["providers",
"sessions"]` —loader §14 topologically orders them beforehand—; a wiring `init.lua` + the
`lua/agent/init.lua` module + `lua/agent/tools_fs.lua`). INACTIVE by default (ADR-010),
activatable via `enu.toml` `plugins.enabled=["providers","sessions","agent"]` (all three explicit:
`requires` only orders, it does NOT auto-discover/activate — the loader requires the dependency
to be in the discovered set, which for embedded plugins is whatever `enabled` names).

**Does NOT expand api.md** (completeness corollary satisfied): `enu.events` (§4),
`enu.task.future`/`spawn` (§3), `enu.has("ui")` (§9, G20), `enu.fs`/`enu.toml`/`enu.config.dir`, and
the `providers`/`sessions` modules were exactly enough. APILevel stays at 2. **No `G##`
findings.** The extension's error code: `EAGENT` (ADR-009 shape).

**The TURN (`Session:send`, agente.md §2), the heart of it:** appends the user message to the
history (and to the transcript if persisting), `resolve`s the model (providers), and enters a
loop: assembles the canonical request (§7: system from base pieces + `opts.system`; messages =
history; tools = registered ToolDefs) → `request.pre` hooks (can mutate/veto) →
`adapter.stream(req, config)` → **consumes the Events iterator** (providers.md §2.3: `for ev in
iter`), re-emitting each delta on the bus as `agent:delta` and saving the `done`. The agent does
NOT re-assemble deltas: it uses the complete `Message` from `done` (§2.3). It persists the
assistant's message with `usage`/`model` (sesiones.md §3). If `stop_reason == "tool_calls"`: it
runs each `tool_call` of the message IN ORDER (P12: parallel postponed), appends the
`tool_result`s as a `user`-role message (providers.md §2.2) and **asks again**. It ends when the
model stops without tools, or when `max_turns` runs out (actionable EAGENT, loop protection,
§10).

**TOOL registration (`agent.tool`, agente.md §3):** `{name, description, schema, handler,
permissions?}`. A SINGLE process-wide registry (§9). `M.tools()` enumerates the ToolDefs.
`run_tool` executes a tool call: permissions → `tool.pre` → handler (under pcall) → `tool.post` →
`tool_result`. Any failure (permission denied, handler that throws, hook veto, unknown tool) does
NOT break the loop: it produces a `tool_result` with `is_error=true` and actionable text that the
model SEES (§3). The handler receives `ctx = {session, cwd, progress(text), ask(question)}`.
Basic file tools (§3 dogfooding): `read_file` (default="allow", never asks for permission, not
even headless, §5 buffer 1) and `write_file` (default "ask" → DENY headless: it's the one CP-10
denies).

**PERMISSIONS (agente.md §5), per-tool-call pipeline:** (1) default="allow" grants directly; (2)
a policy `deny` cuts it short; (3) `allow` grants it; (4) `permission` hooks (deny /
`{grant=true}`); (5) nobody decided → if the tool's default="deny", denied; if `mode="auto"`,
granted (explicit and noisy, buffer 3); if `mode="ask"` AND `enu.has("ui")` → emits
`agent:permission.asked` and WAITS on a `future` with no timeout (G3), answerable with
`agent.permission.respond(id, granted)`; if `mode="ask"` WITHOUT UI (HEADLESS, G20) → **DEFAULT
DENY** with an ACTIONABLE error (buffer 2: names the tool, the `allow` pattern to add, and
mentions `--auto-permissions`). Patterns `tool[:argument]` with `*` wildcard (glob → Lua
pattern); heuristic `arg_text` (command/cmd/path/file).

**HOOKS-MIDDLEWARE (`agent.hook`, agente.md §4): its OWN registry, NOT the bus.** v1 points:
`request.pre`/`tool.pre`/`tool.post`/`permission`/`compact`. `fn(payload, ctx)` → nil (does not
weigh in) | replacement payload (continues) | `{deny="reason"}` (cuts short, the FIRST deny
wins). Order: ascending priority, then registration order. Each hook under `pcall` (robust
boundary, ADR-008): one that throws is logged and ignored. `Hook:remove()` deactivates it.
`agent._reset_hooks()` (a test helper, not contractual) clears the registry between cases.

**`agent:*` events (agente.md §4, notifications via `enu.events`):** session.start/end,
turn.start/end, delta, message, tool.start/progress/end, permission.asked, error. **Mandatory
attribution (G3):** a single `emit(session_id, name, payload)` helper ALWAYS sets
`payload.session` — impossible to forget. `agent:` is the plugin's namespace (not a core
reservation, ADR-003).

**Persistence (sesiones.md):** `agent.session{...}` creates/resumes via `sessions.open`
(inherits the writer lock, §6) unless `no_store=true` (in-memory test sessions). Every message
(user, assistant with usage/model, tool_results) is persisted with `Session:append_message`.
**Resumption (G18):** `opts.resume=<id>` replays the transcript and repopulates the in-memory
history (the replay policy for the LLM —since the last `compact`— lives here). `Session:set_model`
(G19) validates against providers and writes an `event` entry (sesiones.md §3).

**Decisions / deviations.**
- **`requires` does not auto-activate**: the test `enu.toml` enumerates all three extensions;
  `requires` only gives the load order (verified by reading `loader.go`: `topoSort` operates over
  what is discovered, and an embedded plugin is only discovered if `enabled` names it). Documented
  for S43/S45.
- **System prompt (§7) partial in S39**: only base + `opts.system`. The skills index (§6), the
  repo's `enu.md`, and TOFU/trust (§11) are later work (not in S39's scope).
- **Compaction (§8)** not implemented in S39 (the `compact` hook exists in the point registry,
  but automatic triggering and the default strategy are later work). Does not block CP-10.
- **The handler's `ask` (ctx.ask)**: headless without UI returns `false` (consistent with §5
  default deny); with UI it uses the same `future` flow as permissions.
- **Handler result** normalized to `content: Block[]`: string→text block; table with `type`→one
  block; table without `type`→assumed to be Block[].

**Test adapter (`toolstub`)**: the official stub declares `tools=false` (declared degradation,
§3), so the tests register a `toolstub` adapter from Lua with `tools=true` that emits a tool call
on the 1st turn and, on the 2nd (when the LAST message carries a tool_result), answers with text
and stops. Looking only at the last message (not the whole history) makes it correct when
RESUMING (a resumed session already contains tool_results from previous turns) — a subtlety that
cost a debugging cycle in CP-10.

**🔎 CP-10 green (minimal, usable headless agent):** `TestCP10AgenteHeadless` boots the runtime
HEADLESS (`WithForceUI(false)`, `enu.has("ui")`=false), runs a turn with the real file tool
`read_file` (reads a file from disk with `enu.fs`, granted since it's read-only, its content is
fed back and the final done closes it), PERSISTS the session in JSONL (it is verified that the
file under `data_dir/sessions/` contains `meta`, the `message`s, the tool's name, the content
read, and the final answer), and then RESUMES the session (replay repopulates the history) and
requests `write_file` → permission DENIED, actionably (names "headless"/"write_file"/"allow"),
the turn does NOT break, and the file is NOT created. All WITHOUT a single line of UI (G20).

**Tests** (`agent_test.go`, S12's harness with all three extensions via `enu.toml`): load+
activate; full turn (tool call, result fed back, final done, 4-message history); headless denied
permission → actionable tool_result is_error; permission granted by `allow`; tool.pre/post hooks
(rewrite args/result) and veto via `{deny}`; `agent:*` events emitted with `session` (G3); CP-10
(persistence + headless resumption with file tool + denied permission).
`CGO_ENABLED=0 go build`/`go vet ./...` green; `gofmt -l` clean; `CGO_ENABLED=1 go test -race
-timeout 120s -count=2 ./internal/...` green (~54 s); no regression on S01–S38.

**What S40 (subagents) will reuse:** `agent.caps.*` (named caps bundles, §9, already defined as
inspectable tables), the single tool registry (handlers run in the principal via a proxy, §9),
the centralized permissions/hooks (the worker cannot bypass them), and `opts.parent` (a child
session with `meta.parent`). **What S43 (chat) will reuse:** the
`agent:delta`/`agent:message`/`agent:permission.asked` events (to paint streaming and dialogs),
`agent.permission.respond` (to answer the user's ask), and `agent.session`/`Session:send` as a
contract consumed just like a third party.

**Process note.** After leaving the code, the tests, the docs (pointer, logbook, this entry) and
verifying build/vet/gofmt/race-count=2 green, it was committed and pushed WITHOUT delay (a lesson
from S38).

## S40 — Agent subagents (workers + trimmed caps + digest to the parent) (agente.md §9)

### What the session called for

S40 extends the `agent` extension (S39) with SUBAGENTS (agente.md §9): an agent that runs
ISOLATED and returns a DIGESTED RESULT to the parent. The contract: `Session:spawn(opts) -> Sub`,
`Sub:run(prompt) ⏸ -> digest`, `Sub:cancel()`, with two modes (`worker=false`: a task in the
principal sharing tools; `worker=true`: a loop in an `enu.worker` with trimmed `caps` and tool
handlers executed in the principal via a message proxy), plus the named caps bundles
`agent.caps.*`. All Lua over the frozen API (ADR-003): `enu.worker` §13 + `enu.task` + the
`providers` module. **Does NOT expand api.md** (APILevel stays at 2; not one new public
function).

### Chosen architecture (two modules)

- `lua/agent/subagent.lua`: the `Sub` handle, the two modes, and the tool PROXY on the PARENT's
  side. It is wired over the already-built `agent` module with `subagent.attach(M)` (injection to
  avoid a circular require), exposing `M._subagent.spawn` (used by `Session:spawn`).
- `lua/agent/subagent_worker.lua`: the subagent's LOOP that runs INSIDE the worker. It is the
  `module` that `enu.worker.spawn("agent.subagent_worker", {caps=...})` loads.

### Interpretation decisions for agente.md §9

1. **The digest** (agente.md §9 says "digested result, not the raw stream") materializes as
   `{ text, message, stop_reason, usage, turns }`: `text` is the plain text of the final message
   (a shortcut the parent integrates as a tool_result/message), `message` is the complete
   canonical Message (JSON-able), `usage` is the last turn's provider usage. JSON-able on
   purpose: it crosses the worker boundary without Blocks/closures (api.md §13).

2. **The tool proxy** (worker mode). The worker does NOT execute handlers: for every tool_call
   it sends `{kind="tool_call", id, name, args}` to the parent via `enu.worker.parent.send` and
   waits for `{kind="tool_result", result}`. The parent runs the tool with
   `M.run_tool_proxy(proxy_session, call)` = the very same `run_tool` from the turn (permissions →
   hooks → handler → tool_result). This way security stays centralized (the worker cannot dodge
   the pipeline because execution never happens on its side) and there is ONE single tool
   registry. The `proxy_session` is a real child `agent.session` (it provides inherited-and-
   trimmed permissions, cwd, and the child transcript if persisting).

3. **TWO FENCES** (agente.md §9, literally): the *caps* limit what the worker's Lua code can do
   (G6, the core's sandbox); the *permissions* (inherited from the parent, trimmed by
   `opts.permissions`, never expanded) limit which tools it uses —and since the tools run in the
   parent, its §5 permissions pipeline is the effective fence—.

4. **Default caps of a subagent-worker: read-only.** `FS_RO` (fs.read/stat/list/cwd) +
   `SEARCH` + the LOOP MINIMUMS (`task`/`json`/`toml`/`config.dir`/`log`/`fs.read`). Reason for
   the minimums: the worker must be able to orchestrate (task), serialize the digest/the proxy's
   messages (json), and RESOLVE the model —`providers.resolve` reads `providers.toml` from disk
   with `enu.fs.read`+`enu.toml.decode` from `enu.config.dir`—. Without them the worker could not
   even run the turn or return anything. `normalize_caps` always adds them to a user list, without
   expanding the fs/net surface the user chose.

### Deviation: `opts.adapter_modules` (extension opt, NOT a core opt)

agente.md §9 lists `opts` = those of `agent.session` plus `{ worker?, caps? }`. An EXTRA
extension opt was added (not from the core): `opts.adapter_modules` (a list of require-able
adapter module NAMES that the worker registers before resolving). **Why it's necessary:** the
`init.lua` of `providers` — which imperatively registers the official adapters — does NOT run
inside a worker (a worker only executes `require(module)`, with no plugin lifecycle, api.md
§13). So the live adapter registry starts EMPTY in the worker; the bootstrap fills it by
requiring the named modules (the official ones ARE require-able: `providers.adapter_anthropic`).
It's re-running what init.lua would do, with no kernel privilege. It has a sensible default
(`{ "providers.adapter_anthropic" }`), so the normal case doesn't need it; the tests use it to
inject a require-able stub. It's an addition to the opts of ONE extension, not to `api.md`.

### Why api.md didn't need to be extended

The subagent-worker is expressed entirely with the public API: `enu.worker.spawn` with `caps`
(api.md §13, G6) for HARD isolation; `Worker:send`/`recv` + `enu.worker.parent.send`/`recv` for
the init/tool_call/tool_result/done protocol (JSON-able messages copied); `enu.task` for the
loop; the `providers` module (resolve + register_adapter) and the `agent` module
(run_tool_proxy, caps). The completeness corollary is satisfied: an official feature built
without a kernel shortcut. APILevel stays at 2.

### The subagent-worker is HEADLESS by construction

Inside the worker there is neither `enu.events` (main bus) nor `enu.ui` (api.md §16). The
subagent-worker loop therefore DISCARDS stream deltas (there's no one to emit them to) and only
emits the DIGEST to the parent. Consistent with agente.md §9: the parent receives digested data,
not the raw stream. In task mode (worker=false), `agent:*` events ARE emitted (it runs on the
main thread).

### Tests (`subagent_test.go`)

Harness with providers+sessions+agent plus a user plugin contributing two require-able modules:
`wstub` (a stub adapter that decides its behavior by looking at the REQUEST, not at main-thread
globals — which don't cross into the worker) and `wprobe` (a worker module that reports which
API exists inside). Cases: `Session:spawn`/`Sub` surface; task mode (digest with text+usage from
the last turn); worker mode e2e (isolated turn with `wstub` → digest integrated by the parent);
ISOLATION FROM THE INSIDE (`wprobe` with default caps: `fs.write`/`http`/`ui`/`events` do NOT
exist, `fs.read`/`task`/`json`/`toml` DO — direct verification of the "trimmed API" criterion);
tool PROXY (a tool whose handler flags a MAIN-thread global: if it changed, it ran on the
parent); malformed caps → EINVAL; `agent.caps.*` packages without `fs.write`.

`CGO_ENABLED=0 go build`/`go vet ./...` green; `gofmt -l` clean; `CGO_ENABLED=1 go test -race
-timeout 120s -count=2 ./internal/...` green (~53 s); no regression on S01–S39. No `G##` findings.

### What S41/S43 will reuse

- **S41 (MCP):** the single tool registry + `run_tool_proxy`/centralized permissions (an MCP
  server's tools are registered with `agent.tool` just like file tools, and the subagent uses
  them through the same proxy); the worker↔parent messaging pattern.
- **S43 (chat):** `Session:spawn`/`Sub:run`/the digest as a contract consumed just like a third
  party, and (in task mode) the subagent's `agent:*` events.

**Process note.** After code + tests + docs (pointer to S41, logbook, this entry) + green
build/vet/gofmt/race-count=2, commit and push WITHOUT delay (lesson from S38/S39).

## S41 — Official `mcp` extension (layer 2: JSON-RPC/stdio client; tool + trust mapping) (arquitectura.md §layer 2, closes open question no.4)

Sixth link of Phase 8. **Pure Lua over the frozen public API** (ADR-003, no kernel privilege —
the core does NOT know what MCP is). Implements **layer 2** of arquitectura.md ("external
processes via subprocess, JSON-RPC/stdio; MCP lives here as an official Lua extension over
`io.spawn` + codecs") and **closes open question no.4** of arquitectura.md (the MCP extension
contract: configuration, lifecycle, tool mapping and trust).

New embedded plugin `internal/runtime/embedded/mcp/`: `plugin.toml` (name="mcp",
`requires=["agent"]`), `init.lua` (wires up + lazy auto-connect of `mcp.toml` in a task) and the
`lua/mcp/init.lua` module. INACTIVE by default (ADR-010); enabled via `enu.toml`
`plugins.enabled=[..., "mcp"]`, `source="builtin"`. The `embed.FS` discovers it on its own (any
subdirectory of `embedded/` with a `plugin.toml`), without touching the S12 mechanism.

### The JSON-RPC 2.0 client over stdio (`Conn`)

`mcp.connect{ name, command, cwd?, env? } ⏸ -> Conn` launches the server with `enu.proc.spawn`
(S16) and talks to it over stdin (JSON requests via `enu.json.encode` + `Proc:write`), reading
responses from stdout line by line (`Proc:read_line` + `enu.json.decode`). Demultiplexing: a
**dedicated reader task** (`dispatch_loop`) reads stdout and dispatches each response to its
pending request by `id` (each `request` registers an `enu.task.future` that the reader resolves),
allowing several requests in flight without mixing up responses. Server notifications (no id)
are ignored in v1.

### Extension decisions (don't touch the core; close no.4)

1. **Newline-delimited framing.** One line = one JSON message ending in `\n`. This is the
   framing of the MCP stdio transport in its simple form. The **Content-Length** alternative
   (LSP-style headers) was discarded for v1: it adds parsing complexity with no benefit to the
   harness, and the line-based transport composes exactly with `Proc:read_line` (api.md §6)
   without extra buffering. Documented in the module; if a server required Content-Length, it
   would be a future iteration (the client reads/writes at a single point, easy to extend).
2. **`mcp__<server>__<tool>` prefix.** MCP tools are registered with the agent under this name.
   It's the namespacing convention of the MCP ecosystem: avoids clashes between servers and
   between an MCP tool and one of your own, and makes the permission pattern readable
   (`allow = {"mcp__github__*"}`).
3. **Trust = `permissions.default = "ask"`.** MCP tools are THIRD-PARTY; they are registered
   with default "ask" (agente.md §5), never the "allow" of your own read-only tools. This
   requires EXPLICIT permission, and in headless mode without `allow` the §5 pipeline DENIES
   them with an actionable error. There's no special case in the agent: an MCP tool goes through
   the same gate (permissions → hooks → handler) as any other. Consistent with agente.md §3
   ("MCP fits here with no special case").
4. **`mcp.toml` as the configuration format** (data/code split, ADR-005):
   `[servers.<name>] command = [...] cwd? env?`. Absent → nothing connects (the normal case).
   `mcp.connect_configured` launches them from a task; one server failing doesn't block the
   others.

### Process lifecycle (api.md §6)

The server is launched, lives as long as the `Conn` exists, and is killed cleanly: `Proc:kill`
registered in `enu.task.cleanup` (dies when the owning task ends) and an explicit, idempotent
`Conn:close()`. A server that DIES (EOF on stdout) makes `dispatch_loop` mark the connection as
down and wake up ALL pending requests with `EMCP` (nobody hangs forever). On close, the server's
tools are re-registered with a handler that fails actionably (the `agent` extension doesn't
expose a public de-registration — a re-registration REPLACES, agente.md §3 — and leaving tools
that invoke a dead connection would be worse: the error comes back as a tool_result is_error
that the model sees).

### Result mapping

The result of an MCP `tools/call` (`{ content = [{type="text",text},...], isError? }`) is
translated to the agent handler's format (string | Block[]): text blocks are concatenated; an
`isError = true` is propagated by throwing `EMCP` (the loop turns it into a tool_result
is_error). Images and other block types are left for a later iteration (v1 covers text, the
central case).

### Does NOT extend api.md (completeness corollary satisfied)

`enu.proc` §6 (spawn/write/read_line/kill) + `enu.json` §12 + `enu.task` §4 (spawn/future/cleanup) +
`enu.fs`/`enu.toml`/`enu.config.dir` + the `agent` module (`agent.tool`, `agent.tools`) were EXACTLY
enough to build MCP. APILevel stays at **2**; not one extra public core function. Extension
error: `EMCP` (ADR-009 form). No `G##` findings.

### Tests (`mcp_test.go`)

The test MCP server is a **mini Go program** (source embedded in the test) compiled to a
temporary binary with `go build` (no network, no dependencies beyond Go, guaranteed in the
environment — the most robust option suggested by the spec). It speaks JSON-RPC/stdio:
responds to `initialize`, `notifications/initialized`, `tools/list` (announces `echo` and
`boom`) and `tools/call` (executes them; `boom` returns `isError=true`). Cases: load+activate
(builtin); connect + handshake + tools/list + registration with prefix and trust; **FULL CYCLE**
(the test adapter requests `mcp__srv__echo`, the handler does `tools/call`, "echo: hello MCP" is
fed back to the model); headless trust (MCP tool without allow → actionable DENY naming
"headless"/tool/"allow"); server `isError` propagated to tool_result is_error; lifecycle (pid
alive after connect, dead after `close()`, via `pidAlive`/`waitDead` from proc_test).

**Anti-race note:** registering Go globals (`SetGlobal`) AFTER Boot is a race with the scheduler
(mcp's auto-connect is already running); the lifecycle test installs its helpers
(`__publish_pid`, `__mcp_pid`) BEFORE Boot (`bootMCPWith(preBoot)`). The rest of the tests don't
touch globals after Boot.

`CGO_ENABLED=0 go build`/`go vet ./...` green; `gofmt -l` clean; `CGO_ENABLED=1 go test -race
-timeout 120s -count=2 ./internal/...` green (~54 s), no flakiness; no regression on S01–S40.

### What S43 (chat) will reuse

`require("mcp")` (`mcp.connect`/`mcp.servers`/`mcp.get`) like any third-party extension, and the
MCP tools already registered with the agent that chat lists/invokes through the §5 permission
pipeline just like its own (the UI paints an MCP tool's permission like any other's).

**Process note.** After code + tests + docs (pointer to S42, logbook, closing of arquitectura
no.4, this entry) + green build/vet/gofmt/race-count=2, commit and push WITHOUT delay.

## S42 — Widget toolkit (dirty tree, slots, focus, themes G22) (arquitectura.md §kernel/ui note)

Seventh extension of Phase 8. **Pure Lua over the frozen API** (ADR-003 / ADR-012): the core
does NOT know what a widget is; the toolkit is an official extension with no privilege. Embedded
plugin `internal/runtime/embedded/toolkit/` (`plugin.toml` name="toolkit", no `requires`) with
modules `lua/toolkit/{init,theme,widget,layout,widgets,app}.lua`. Implements the arquitectura.md
§kernel note about `ui` (the toolkit "retained internally: tree + dirty nodes … provides slots,
focus, composition between plugins, and the theme system") and, together with S43 which consumes
it, closes open question no.3 (the toolkit's public API).

### The model (what arquitectura.md left open, fixed here)

`arquitectura.md` names the ingredients (tree, dirty nodes, slots, focus, themes) but not the
widget catalog or the exact layout model. A **minimal coherent set** is implemented, sufficient
for S42's fact criterion and for S43 (chat) to build its anatomy (chat.md §1: transcript/input/
statusline column + modal layers).

- **Retained tree** (`toolkit.widget`): each node knows its `parent`/`children`, its local area
  `(x,y,w,h)` — ASSIGNED by the parent's layout, a leaf doesn't decide where it goes — and
  `compose(w,h) -> Block` (the only thing specific to each type; the rest — tree, dirty, focus —
  is shared). `derive()` builds metatables that inherit from the base Widget for concrete types
  without duplicating machinery.

- **Dirty tracking** (key decision, the why is ADR-007: don't recompose everything every frame).
  Each node caches its last `Block` (`_block`) and a `dirty` flag. `mark_dirty()` dirties ONLY
  that node (invalidates its cache) and NOTIFIES upward to the app (`_notify` →
  `app:_request_paint`), **without dirtying siblings or ancestors** (their Blocks stay valid;
  what changed is a descendant that the app re-blits). `render()` only recomposes if the node is
  dirty or if its SIZE changed relative to the cache. Important subtlety: **moving without
  resizing (only `x/y`) does NOT recompose** — the content is the same, only where it's blitted
  changes — only a `w/h` change invalidates the Block. That's the real saving: not RECOMPOSING
  (measuring text, rendering markdown) which is the expensive part; the blit is a cheap copy
  (api.md §9.1). Verified by instrumenting `compose` in the test (counting recompositions).

- **Slots/layout** (`toolkit.layout`): three containers that do NOT paint themselves, they PLACE
  their children by dividing up their area. `vbox`/`hbox` divide one axis; a child declares how
  it occupies the main axis with `flex` (>0: proportional share of the leftover) or a fixed size
  (`pref_h`/`pref_w`); a child with neither flex nor a fixed size occupies 0 (explicit decision:
  whoever doesn't say how much they occupy doesn't hog space). The **last flexible one** takes
  the remainder of the *slack* (leftover space after the fixed ones), not `main - pos` — the
  initial bug: with `main - pos` an intermediate flexible child would steal the space of later
  fixed children; it was fixed to "remaining slack of the last flexible", which respects fixed
  children that come afterward. `stack` overlays all children on the same area (insertion order
  = logical z): the basis for modal layers.

- **Focus** (`toolkit.app`): the root app keeps ONE focused widget, collects the focusable ones
  in PREORDER (natural tab order), cycles them with `focus_next`/`focus_prev` (wraps at the
  ends), and routes input to the FOCUSED one. `handle_key(ev)` delivers to the widget's `on_key`;
  what the widget doesn't consume, the app LETS PASS (returns false), respecting the core's stack
  (api.md §9.3: "whoever doesn't consume, lets it pass"), so an upper-layer keymap can pick it
  up. `tab`/`shift+tab` move focus by default. The app places the REAL cursor on the focused
  input with `Region:cursor`. Emits `toolkit:focus {app,widget}` on focus change — in the
  PLUGIN's namespace (`toolkit`), NOT `ui:focus`: `ui:` is reserved for the core (api.md §4),
  which already emits its own `ui:focus {focused}` with ANOTHER semantics (the TERMINAL's focus,
  ui_events.go); overwriting it would break its subscribers. WIDGET focus is toolkit vocabulary
  (§9.3).

- **Themes (G22)** (`toolkit.theme`): THIS is the point of G22. The core only understands literal
  colors (`#rrggbb`/0-255); semantic names (`accent`/`error`/`dim`…) are theme vocabulary, which
  RESOLVES them to literals before building the Block/Style. `theme:color(name)` (literal →
  untouched; name → literal; unknown → actionable EINVAL: an incomplete theme is noticed, not
  silently degraded); `theme:style(spec)` converts semantic `fg`/`bg` to literals, copying the
  attributes. `theme.new{colors}` VALIDATES that the palette consists of literals (a theme that
  mapped "accent" to another name would fail later inside `enu.ui.block`; validating it at
  construction anchors it to the theme). `is_literal_color` is replicated in Lua (same shape as
  the core's `normalizeColor`) to distinguish "already a literal" from "a name to resolve"
  WITHOUT trying to build a Block and catch the error. `theme.default` provides a minimal palette
  with the names chat.md §7 requires.

- **No collision between plugins** (fact criterion): each `toolkit.app` is INDEPENDENT — its own
  `Region` (own z-order, api.md §9.1), its own tree, its own focus, its own `on_input` in the
  stack. Two plugins that each mount their own app compose in separate regions and input flows
  through the stack (whoever consumes wins; whoever doesn't lets it pass down, which may be
  another app). There's no shared global state between apps: all retention lives in the
  instance.

### Base widgets implemented

- **label**: a single line of styled text (statusline, headers). Not focusable. `pref_h=1` by
  default (a label occupies its row, not 0). Composes with `enu.ui.block` + `theme:style`.
- **text**: multiline markdown block (`enu.text.markdown`, streaming-safe) or word-wrap
  (`enu.text.wrap`), with viewport SCROLL. Composes the COMPLETE Block; scroll is an offset
  (`scroll_to` only requests a repaint, doesn't dirty: "scroll = re-blit with another offset",
  api.md §9.1).
- **input**: SINGLE-line editor, FOCUSABLE. `on_key` consumes printable characters, backspace,
  arrows, home/end, and maintains a caret (in bytes; the rich/multiline editor is the natural
  later extension, chat.md §3). enter/tab are LET PASS (handled by the app: submit/change focus).
  `caret_col()` gives the real cursor column.

### Implementation decision: clipping to the band via a viewport region (Y-scroll overflow)

The core's clipping is by REGION, not by widget band (api.md §9.1: the region is the viewport,
`blit(0,-3,doc)` trims the leading edge but clips at the REGION's edge). Since the app's region
spans the ENTIRE tree, blitting a `text`'s Block there clips it to the region, not to its band —
and `text` composes its COMPLETE Block (may exceed its band's `h`, widgets.lua). This BLEEDS in
TWO cases:
  * **scroll** (`scroll>0`, negative offset): the `text` would start from a later row, spilling
    over the widget ABOVE;
  * **overflow** (Block taller than the band, even with `scroll==0`): the `text` would write
    extra rows over the widget BELOW.
The core's correct model is **one region per viewport**: that's why a `text` that is either
scrolled **or** overflowing its band gets its OWN child region (created on the fly, `z = app.z
+ 1`, owned by the app, destroyed in `App:close()`), clipped to its band; there the offset clips
cleanly at BOTH ends (G28) and nothing escapes the band. Widgets that FIT in their band and
aren't scrolled are blitted directly into the app's region (fast path: no child region or extra
z for a short label/input/text). If a `text` that was overflowing fits again, its
viewport-region is HIDDEN (its old content, at `z+1`, must not keep covering what the app
paints; it's shown again if needed again). The gate is `oy ~= 0 or blk.height > node.h`. This is
correct use of the primitive, not a core extension. (The S42 review caught the overflow-without-
scroll bleed: the original gate only covered `scroll~=0`.)

### Synchronous render in `_request_paint` (simplicity + deterministic tests)

`_request_paint` paints SYNCHRONOUSLY (`paint()`). In a live app the core's compositor already
coalesces blits and paints at most every ~30 ms (api.md §9), so blitting extra is cheap (it's a
copy, not a re-render); the dirty tracking's gain is not RECOMPOSING the Blocks (the expensive
part), not avoiding the blit. Synchronous painting keeps the code simple and lets tests see the
result instantly (inspecting the compositor's grid after `APP:paint()`).

### Does NOT extend api.md (completeness corollary satisfied)

The toolkit was built EXACTLY on top of API §9 (`enu.ui.region`/`blit`/`fill`/`clear`/`cursor`/
`size`, `enu.ui.block`/`Style`, `enu.ui.on_input`) + §10 (`enu.text.markdown`/`wrap`/`truncate`/
`width`) + §4 (`enu.events.emit`, with its own `toolkit:focus` event in the plugin's namespace) +
§2 (`enu.has`). Not one extra public function; APILevel stays at 2. No `G##` findings. Confirms
that the low-level UI API (ADR-007) is enough for a high-level toolkit in Lua (ADR-012: ADR-007's
veto was never exercised).

### Tests and result

`toolkit_test.go` (S12 harness with `WithForceUI(true)`+`WithUISize` — the toolkit is UI, in
headless mode `enu.ui` doesn't exist, G20 — the Block is opaque to Lua, so its CONTENT is
inspected from Go by looking at the compositor's grid, just like `compositor_test.go`, and the
toolkit's logic is inspected from Lua over its own tables): load+activate (builtin); theme G22;
dirty tracking; layout+focus between two widgets (fact criterion); no collision between two trees
(fact criterion); unconsumed input is let pass; vbox distribution; scroll-viewport; **overflow
without scroll** (a `text` taller than its band, with `scroll==0`, above a label: clipping to the
band prevents it from spilling onto the one below); `app` without `enu.ui`→EINVAL.
`CGO_ENABLED=0 go build`/`go vet ./...` green; `gofmt -l` clean; `CGO_ENABLED=1 go test -race
-timeout 120s -count=2 ./internal/...` green (~55 s; the toolkit stable under `-race -count=4`).
Note: `TestMCPToolServerError` (S41) is a known flake under the full suite with `-race -count=2`
(it compiles and launches an external process; under CPU/IO contention from the full set its
JSON-RPC handshake occasionally exceeds timing); it passes in isolation and on re-runs of the
full suite; it is ORTHOGONAL to S42 (the toolkit is Lua over `enu.ui`/`enu.text`, doesn't touch
proc/network). No regression on S01–S41.

### S42 review note (two fixes before approval)

The review found two defects, both fixed (the S42 commit was amended):

1. **[Blocking] Event collision with the core.** `App:set_focus` emitted `ui:focus` with payload
   `{app,widget}`, overwriting the `ui:focus {focused}` that the core emits for TERMINAL focus
   (ui_events.go, shielded in `gating_test.go`): any subscriber to the core's `ui:focus` would
   break (its `ev.focused` would disappear). `ui:` is reserved for the core (api.md §4) and
   WIDGET focus is toolkit vocabulary (§9.3), so the event was RENAMED to **`toolkit:focus`**
   (namespace = plugin name). The prose in `app.lua`, `init.lua`, the logbook
   (implementacion.md), and this entry was adjusted. The core's `ui:focus` remains intact (its
   test is still green, doesn't depend on the toolkit).
2. **[Minor] `text` bleeding without scroll.** `paint()` only used the clipped viewport-region
   when `scroll~=0`; with `scroll==0` a `text` taller than its band would blit directly onto the
   shared region and spill rows onto the widget BELOW (the core's clipping is by REGION, not by
   band). The gate was widened to `oy ~= 0 or blk.height > node.h`: any `text` that overflows its
   band or is scrolled paints through its viewport-region clipped to the band (a `text` that fits
   again hides its viewport to avoid leaving remnants). New test:
   `TestToolkitTextDesbordeSinScroll` (a 6-line `text` in a 3-line band above a label → the label
   is NOT overwritten). Detail in "clipping to the band via a viewport region".

**Process note.** After code + tests + docs (pointer to S43, logbook, this entry) + green
build/vet/gofmt/race-count=2, commit and push WITHOUT delay.

## S43 — Official `chat` extension (harness UI over toolkit + agent; streaming markdown); CP-11 (chat.md)

Eighth extension of Phase 8 and nu's visible face. **Pure Lua over the frozen API** (ADR-003):
the core does NOT know what a chat is; it's an official extension with no privilege that
consumes the agent's public API (agente.md), the widget toolkit (S42), and the event bus — a
third-party UI could do the same. New embedded plugin `internal/runtime/embedded/chat/`
(`plugin.toml` name="chat", `requires=["toolkit","agent", "providers","sessions"]` — the loader
§14 orders them topologically beforehand; `init.lua` that wires up + starts on a TTY; modules
`lua/chat/{init,transcript,input,statusline,commands, permission}.lua`), INACTIVE by default
(ADR-010), enabled via `enu.toml` `plugins.enabled=[...,"chat"]`, `source="builtin"`. Implements
chat.md and, together with S42, closes open question no.3 (the toolkit's public API, now truly
consumed).

### Layout (chat.md §1)

A `toolkit.app` (S42) whose root is a `stack` with two layers: (1) a `vbox` with the chat's three
bands — **transcript** (`toolkit.text{markdown=true}`, `flex=1`: takes up the leftover height,
scrollable via viewport), **multiline input** (`chat.input`, `pref_h=3`) and **statusline**
(`hbox` of two `toolkit.label`, `pref_h=1`) — and (2) a modal layer (`modal_layer`, overlaid) for
the permission dialog and pickers (chat.md §1/§5). The app creates its region at full screen
(`enu.ui.size()`), routes input to the focused widget (the editor), and repaints via dirty nodes
(all from S42, without touching the toolkit). Focus starts on the editor.

### The streaming markdown flow (chat.md §2, the HEART of S43)

Chat subscribes to the core's `agent:*` bus events (api.md §4), **filtering by the active
session** (G3: `payload.session == self.session.id`; other sessions' activity goes to the
statusline counter, not the transcript). Turn rendering:

- `agent:delta` of type `text` → `transcript:append_delta(text)` accumulates in the assistant
  item CURRENTLY IN PROGRESS and `Chat:_refresh_transcript()` dumps the accumulated markdown into
  the `toolkit.text` widget via `set_text`: the widget RE-RENDERS the markdown (streaming-safe,
  S23) and the Block GROWS incrementally. **This is exactly CP-9's hot path (delta → accumulated
  markdown → blit), now inside chat** — the heavy part (measuring/rendering markdown) is a Go
  primitive in the widget; Lua only orchestrates the delta loop (ADR-012).
- `agent:delta` of type `thinking` → dimmed reasoning block (markdown quote; chat.md §2's
  interactive folding is a later improvement).
- `agent:message` → `transcript:seal_assistant(message)` SEALS the message with the `done`'s
  canonical Message (replaces the deltas with the final render, providers.md §2.1).
- `agent:tool.start`/`tool.end` → tool blocks (header with name + status).
- `agent:permission.asked` → modal dialog (§5).
- `agent:error` → error block.

**Transcript model (`transcript.lua`).** A single `toolkit.text{markdown=true}` with the ENTIRE
conversation serialized to a markdown string (the "items" — user/assistant/tool/thinking/error —
are rendered to markdown fragments and concatenated). It's the simplest thing that meets the
fact criterion and reuses the toolkit's viewport/scroll as-is; rebuilding the string per delta is
cheap (Lua concatenation), what's expensive is rendering (Go). DEVIATION: a widget-per-message
(for chat.md §2's pluggable tool-result renderers) is the natural evolution on top of this same
model (the items are already separate); v1 serializes them. `chat.renderer` is exposed (it
registers rendering) but v1 uses the text fallback.

### Multiline input (chat.md §3)

`chat.input` is a custom widget that EXTENDS the `toolkit.input` contract (focusable + `on_key` +
caret) to multiline — the toolkit leaves the catalog open and multiline is "the natural
extension of the same contract" (S42). The text is stored as an array of lines and a caret
`(row,col)` in bytes (v1 simple ASCII/UTF-8, like toolkit.input; the grapheme-based editor comes
later). **enter WITHOUT a modifier is NOT consumed by the editor: it is LET PASS** (returns
false) so a chat-level global keymap can pick it up as "send" (chat.md §3, the same pattern as
toolkit.input); **shift+enter / alt+enter** (depending on terminal, `ev.mods`, api.md §9.3)
insert a line (consumed). Arrows/home/end navigate (with edge jumps); `↑/↓` AT THE EDGE request
history (`on_history_prev/next` callbacks that chat wires up, without the editor knowing about
sessions); correct multiline paste (split by `\n`). The REAL multiline cursor is placed by chat
with `Region:cursor` by adding `caret_row` (`toolkit.app` only places one row; multiline
placement belongs to chat, the toolkit isn't touched).

### Permission dialog (chat.md §5)

On `agent:permission.asked` (from the active session) a modal opens (`chat.permission`, a
focusable widget in the `stack`) with the tool and its **full args** (nothing dangerous
truncated, chat.md §5) and responds with `agent.permission.respond(id, granted)` — the decision
the turn is waiting on (agente.md §5, future with no timeout, G3). Keys: `a`/`y` = allow once
(true), `d`/`n`/`esc` = deny (false). **FIFO queue, one visible modal** (chat.md §1/§5): several
asks queue up; answering one shows the next. DEVIATION: "always allow" (adding the pattern to the
session's policy / the user's global `agent.toml`, chat.md §5) and editing the proposed pattern
before accepting require writing `agent.toml` and a pattern editor; the agent (S39) doesn't yet
expose live policy editing, so v1 offers once/deny, SHOWS the suggested pattern, and documents
"always" as an improvement. The modal's contract (a focusable widget that responds to the ask) is
exercised.

### Slash commands (chat.md §4) and statusline (chat.md §6)

`chat.command{name, description, args?, complete?, handler ⏸}` registers commands; a `/` at the
start of input dispatches them (`commands.dispatch`); an unknown command is "handled" by showing
an error (not sent to the model). Builtins (dogfooding, using the same `chat.command`): `/help`,
`/quit`, `/clear`, `/model [ref]` (with arg: `Session:set_model`, G19; without arg: lists
`providers.list()`), `/sessions` (lists `sessions.list`), `/compact`. DEVIATION: `/model`/
`/sessions`/`/fork` with an interactive fuzzy PICKER (chat.md §4) accept the argument as TEXT in
v1; the visual picker is a modal layer (`toolkit.stack`) — a documented improvement.
`/permissions` and the full `/fork` depend on later agent surface (agente.md §8 leaves
triggering compaction for later). The **statusline** (`chat.statusline.add{id,side,priority,
render}`) provides default segments (model · context % from `Session.usage` · cost · cwd ·
permission mode · pending-asks counter for other sessions, G3), extensible by third parties.

### Theming (chat.md §7, G22)

chat hardcodes NOT a single color: the transcript's final render is done by `enu.text.markdown`
(themable, api.md §10) and the widgets (input/statusline/dialog) resolve their SEMANTIC styles
(`accent`/`error`/`dim`/`warn`) against the app's theme (`toolkit.theme`, G22), which converts
them to literals when composing. The default shortcut table `chat.keys` is public and remappable
(chat.md §7).

### Startup (chat.md §8, G20)

`chat.start(opts?) ⏸ -> Chat` requires `enu.ui` (interactive TTY): in headless mode (no UI,
`enu.has("ui")`=false, G20) it's an **actionable EINVAL** (naming "headless"/"ui"; chat.md §8 —
for headless use there's the direct agent). Creates/resumes the session (`agent.session`, with
`resume` → G18: reflects the repopulated history in the transcript). `init.lua` only starts chat
on a TTY (subscribes to `core:ready` and mounts the app in a task, because `start` suspends); in
headless mode it leaves the module accessible via `require("chat")` without touching `enu.ui`.
Subscribes to `ui:resize` ("your region, your ui:resize", api.md §9.1): redoes layout on resize.

### Does NOT extend api.md (completeness corollary satisfied)

`enu.events.on`/`emit` (§4), `enu.ui` (§9, via the toolkit), `enu.text.markdown` (§10, via the
toolkit), `enu.task.spawn`/`sleep`/`future` (§4), `enu.has("ui")` (§2/§9), `enu.ui.keymap` (§9.3),
`enu.json` (§12, in the dialog) plus the `toolkit`/`agent`/`providers`/`sessions` modules were
EXACTLY enough for chat. **APILevel stays at 2; not one extra public core function. No `G##`
findings.** Chat uses the core's codes (`EINVAL`) for its usage errors; it doesn't mint its own
code (didn't need to).

### CP-11 (dogfooding) — ADAPTATION DUE TO ENVIRONMENT LIMITATION

CP-11 (implementacion.md) asks for "a real end-to-end chat session against a REAL provider". **In
this CI environment there is NO network or credentials**, so the real provider is NOT runnable
headless. ADAPTATION (like CP-9): `TestCP11ChatStreamingE2E` exercises the chat session END TO
END against a RECORDED SSE from the `anthropic` adapter (served by a local `httptest`, reusing
CP-9's `recordedSSE` plus a second recorded SSE `recordedFinalSSE` for the turn's 2nd round). The
test: the user "sends" (`chat.input:set_value` + `Chat:submit`) → the agent runs the FULL TURN
(`Session:send`, two rounds: the 1st requests the `get_weather` tool registered with default
allow, the 2nd answers with final text after the tool_result) → the streaming `agent:delta` is
painted as markdown in the transcript → **the transcript's composed Block GROWS with the
streaming text** (non-decreasing heights, final > initial) → the REAL content reaches the
composed screen (verified in the compositor's grid). This is the FIRST time the **chat → agent →
stream → markdown → toolkit** path runs together (CP-9 ran it OUTSIDE of chat).

**What the automated test DOES cover:** the full chat→agent→stream→markdown→toolkit path, with a
real interleaved tool call, session persistence (no_store=false), and incremental rendering
verified both in the transcript model and in the compositor's grid. **What it does NOT
(environment limitation, left for a human with credentials):** the REAL provider (network +
Anthropic credentials). The path is identical — only the SSE source changes, from the network
instead of `httptest`; the `anthropic` adapter is already tested against simulated network in
S37.

**Phase 8 is NOT closed yet**: S44 (repl) and S45 (CLI) remain. The board keeps Phase 8 open; the
pointer advances to S44.

### Tests (`internal/runtime/chat_test.go`, S12 harness with the five extensions via `enu.toml`, `WithForceUI(true)`+`WithUISize`)

Load+activate (builtin, module surface); `chat.start` headless → actionable EINVAL (G20,
WithForceUI(false)); LAYOUT (the three widgets with area, focus on the editor); MULTILINE INPUT
(enter lets pass, shift/alt+enter new line, backspace joins lines, caret row/col); STREAMING
MARKDOWN (text deltas grow the transcript — the fact criterion —, message seals, content in the
compositor's grid); filtering by session (G3: a delta from another session doesn't touch the
transcript); permission dialog (modal responds via `agent.permission.respond`); slash command
(/help, unknown); statusline (default segments with model/permissions); **CP-11 e2e** (two-round
turn with tool against recorded SSE; the transcript grows and shows on screen).
`CGO_ENABLED=0 go build`/`go vet ./...` green; `gofmt -l` clean; `CGO_ENABLED=1 go test -race
-timeout 120s -count=2 ./internal/...` green (~61 s); no regression on S01–S42.

### Deviations (summary)

1. **Transcript = a single markdown `toolkit.text`** (not a widget-per-message): the minimal
   coherent choice; the pluggable tool-result renderers (chat.md §2) are an evolution on top of
   the already-separate items. `chat.renderer` is exposed with a text fallback.
2. **Fuzzy pickers (chat.md §3 `@` mentions, §4 `/model`/`/sessions`) by text in v1**: the visual
   picker (a `toolkit.stack` modal layer + `enu.search.fuzzy`) is a later improvement; the
   contract (commands, statusline, modal layer) is exercised.
3. **"Always allow" in the permission dialog (chat.md §5) → only once/deny in v1**: persisting
   the pattern to `agent.toml` / editing the pattern requires agent surface that S39 doesn't
   expose; the suggested pattern is shown.
4. **Multiline cursor placed by chat** (not by `toolkit.app`, which only places one row): correct
   use of `Region:cursor`; doesn't touch the toolkit.
5. **CP-11 against recorded SSE, not a real provider** (CI environment limitation: no network or
   credentials): documented above; the automated path covers everything except the real network.

No deviation touches the core or extends api.md.

**What S44/S45 will reuse.** S44 (repl): the toolkit (`toolkit.app`/`input`/`text`) for its
interactive UI if it has one, and the TTY startup pattern (`enu.has("ui")`, subscription to
`core:ready`); the repl can be activated ALONE (without the harness), just like chat activates
alone. S45 (CLI): the `--continue` sugar relies on `agent.session{resume}` (G18) which chat
already consumes; chat is the agent's interactive consumer that the CLI complements in headless
mode (`enu -e`, `--auto-permissions`). The `agent:*` event contract that chat consumes is stable
for any other UI/observer.

**Process note.** After code + tests + docs (pointer to S44, board, logbook, this entry) + green
build/vet/gofmt/race-count=2, commit and push WITHOUT delay.

## S44 — Official `repl` extension (Lua REPL over the public API, standalone-activatable, G21) (arquitectura §Distribution)

Ninth link of Phase 8: **pure Lua over the frozen API** (ADR-003, no kernel privilege — the core
does NOT know what a REPL is). New embedded plugin `internal/runtime/embedded/repl/`
(`plugin.toml` name="repl" **WITHOUT `requires`** — the repl activates ALONE, G21, without
dragging in the harness; `init.lua` that wires up + starts on a TTY; module `lua/repl/init.lua`),
INACTIVE by default (ADR-010), enabled via `enu.toml` `plugins.enabled=["repl"]`,
`source="builtin"`.

### HOW it evaluates arbitrary Lua (S44's delicate point; completeness corollary)

The central question: a REPL NEEDS to compile and run user code, and the sandbox baseline (§1.2,
S01) disabled blocking IO. Is the public API enough, or is a primitive missing (`enu.eval`/
reopening `load`) → a finding? **This was investigated thoroughly BEFORE deciding** (with
temporary `loadstring`/`load`/`dofile`/`loadfile` probes against the real runtime, later
deleted):

- **`sandbox.go` (S01) withdraws `dofile`/`loadfile`** (they load FILES from disk, bypassing the
  loader) and `io`/`os.execute`… **but NOT `load`/`loadstring`**: gopher-lua's `OpenBase` defines
  them and the sandbox doesn't touch them. They compile an IN-MEMORY string, with no blocking IO,
  so they do NOT violate the baseline's stated reason ("all IO must go through the core's async
  primitives"). They remain available to user Lua (verified:
  `type(load)`/`type(loadstring)` == "function"; `dofile`/`loadfile` == nil).
- **Conclusion: the public API is EXACTLY enough for a REPL. NO new primitive was needed (neither
  `enu.eval` nor reopening `load` in a controlled way); APILevel stays at 2, api.md UNCHANGED.**
  No `G##` finding. The sandbox had already left just the right door open: the REPL is
  expressible with what exists (completeness corollary satisfied, as in S36/S39–S43). The "stop
  and report it" protocol (in the style of `enu.sys.pid` in S38) did NOT trigger because the
  investigation showed the pattern was already buildable.

### The REPL's MODEL (the proven logic, headless)

`repl.eval(src) -> { ok, values?, n, display, error?, incomplete? }` is the PURE core:

1. **return<expr> / statement trick** (`compile`): first `loadstring("return "..src)` — so a
   bare EXPRESSION (`1+1`, `enu.version.api`) is evaluated without the user writing `return`, the
   classic Lua REPL trick — if it doesn't compile, `loadstring(src)` as-is (a STATEMENT: `x=5`,
   `for...`). Both available because `loadstring` is available.
2. **Execution under `pcall`**: the user's error is CAUGHT (doesn't crash the repl — a boundary
   with `pcall`, in the spirit of ADR-008); `ok=false` with the error and its `display`.
3. **Formatting**: return values via `tostring` (a string is quoted with `%q` to distinguish it
   from an identifier), joined by tab; interspersed `nil`s are preserved with an explicit `n`
   counter (not `#values`). Zero returns → `display=""` (a statement doesn't print).
4. **Structured core error** (§1.4): shown as `code: message` (the recognizable form, e.g.
   `ENOENT: does not exist`); the bridge does NOT degrade it (S02 invariant): the repl receives
   it whole (`r.error.code`).
5. **Multiline / incompleteness**: gopher-lua flags INCOMPLETE input (unclosed block/function/
   string) with **`at EOF`** in the message (versus a real error, which carries
   `line:N(column:M) near '<token>'`). `repl.eval` distinguishes them: incomplete input is NOT an
   error (`incomplete=true`), it's the signal for "give me another line". This is the basis of
   multiline mode.

`repl.eval_in_task(src, cb)` evaluates the same logic INSIDE a task and delivers the result to
`cb`: this way a line calling a **⏸** core function (`enu.fs.read`, `enu.http.request`,
`enu.search.grep`…) works — they only run inside a task, §1.3. It's the same pattern as
`chat:submit` (S43) with `Session:send`. Most of the API (non-⏸: `enu.version`, `enu.text.*`,
`enu.json.*`) doesn't need it and goes through `repl.eval` directly.

### TTY DRIVER vs TESTED LOGIC

- **TESTED LOGIC (headless, no TTY)**: `repl.eval` (expression/statement/API-call/structured and
  flat error/syntax/incompleteness), `repl.eval_in_task` (⏸ via task, reading a
  real file + a ⏸ that throws), `repl.banner`, standalone activation (G21). It's the bulk of S44 and
  where the risk lives (the compile/format/detect-incompleteness machine).
- **TTY DRIVER (`repl.start`, needs `enu.ui`, G20)**: mounts a `toolkit.app` (S42) with a
  `vbox` of a `toolkit.text` (transcript: banner + echoes + results, flex) and an input
  row (`hbox` of a `>`/`..` label-prompt + a `toolkit.input`). Enter evaluates (global keymap;
  the input lets "bare" enter through, like the chat editor), ctrl+d exits. The toolkit is a
  **SOFT** dependency (not in `requires`: the repl activates standalone): it's `require`d lazily
  under `pcall` inside `start`; without it, `start` gives an actionable EINVAL, but `repl.eval` ALONE
  keeps working. In headless mode, `start` gives an actionable EINVAL (names TTY and `repl.eval`). The
  AUTOMATABLE part of the driver is tested with `WithForceUI(true)`+`WithUISize` (banner painted,
  input→`_submit`→eval→result in the compositor's grid, recovery after error, multiline
  continuation prompt); the choice with a REAL KEYBOARD (seeing the effect in a terminal) is
  manual with TTY (like CP-7 for the bare screen).

The **bare runtime screen** (S33) already offered "activate loose extensions (e.g. repl
alone)"; now `repl` exists in the embedded catalog, so that action actually activates it. The
catalog grew (the `bare_screen` tests check by SUBSTRING `example`, not by an exact
list: they don't break).

### Does NOT extend api.md

Completeness corollary satisfied: `load`/`loadstring` from baseline §1.2 (EVALUATION) +
`enu.task.spawn` §3 (eval_in_task) + `enu.version`/`enu.has` §2 + the `toolkit` module (the UI) +
`enu.ui.keymap`/`enu.events.on` §9.3/§4 were EXACTLY enough for a standalone-activatable
interactive Lua REPL; APILevel stays at **2**; not one extra public core function. Errors: reuses
core `EINVAL` (doesn't coin its own code; didn't need to). **No `G##` findings.**

### Deviations (minor; none touches the core)

1. **The toolkit is a SOFT dependency, not `requires`.** If it were `requires=["toolkit"]`, activating
   `repl` would ALWAYS pull in the toolkit, breaking "standalone activatable" (G21). Instead, `start`
   `require`s it lazily under `pcall`: repl-alone evaluates via `repl.eval`; the UI is the plus
   that the toolkit provides (activatable via `plugins.enabled=["toolkit","repl"]`). Extension-level decision.
2. **The output format** (a string with `%q`, returns joined by tab, `code: message` for the
   structured error, `display=""` for a statement) is set by this extension: it's product
   vocabulary (how a REPL PRINTS), not the core's. A faithful serializer is `enu.json.encode`, which the
   user calls if they want.
3. **Minimal repl commands** (`/q`/`/quit`/`/exit` to exit, only on the first line of a
   block): UI convenience, not contract. ctrl+d is the keyboard equivalent.
4. **The incompleteness probe in the driver** (`Repl:_compile_probe`) dry-compiles (without
   executing) to decide accumulate vs. evaluate; `repl.eval` (which does execute) does its own
   probe. The double compilation is cheap (memory) and avoids running side effects when
   probing whether the block is complete.

### What S45 will reuse (CLI, the last link of Phase 8)

S45 = **CLI surface** (open question #5 of architecture): `enu -e` flags,
`--auto-permissions`, headless, exit codes, `--continue` sugar over
`agent.session{resume}` (G18). What it inherits: `repl.eval` as a single-line evaluator without a TTY
(`enu -e` already evaluates via `EvalString`/Go; the repl is its interactive counterpart). The
"standalone-activatable on TTY vs. headless logic" pattern (G20/G21) that the CLI replicates for `enu -e`. With S45
Phase 8 CLOSES (all official extensions).

## S45 — CLI surface (flags, --auto-permissions, --continue/G18, exit codes); closes Phase 8 and the plan (architecture #5)

**What it is.** The LAST link of the plan: the command-line surface of the `enu` binary.
Closes open question #5 of [arquitectura.md](arquitectura.md), Phase 8, and the entire
plan (45/45). It lives in `main.go` (the binary), **NOT in the sacred `enu.*` API** (api.md): it's the
interface of the executable, not a Lua surface. The core still doesn't know what an agent is
(ADR-003): the CLI orchestrates the `agent`/`sessions` extensions through the public API, exactly
as a user's `init.lua` could.

**Decision 1 — how the binary invokes the agent (Lua), respecting ADR-003.** The agent's
turn is ⏸ (`Session:send`), so it CANNOT run in `EvalString`'s main chunk
(which runs on the main state, where ⏸ throws EINVAL). Options considered: (a) a
Go method `RunAgentTurn` in `package runtime` — rejected: it puts PRODUCT vocabulary (agent)
into the kernel—; (b) CP-10's two-phase pattern (`EvalString` that `enu.task.spawn`s + globals +
a second `EvalString` to read the result) — works but the mapping to exit codes is
clumsy—; (c) **a GENERAL Go method `Runtime.EvalTaskString(code) -> ([]string, error)`** that
runs a Lua chunk **as a task** to completion and returns its returns / `*StructuredError`.
**Chosen (c):** it's a legitimate, agnostic kernel capability ("run a suspendable Lua chunk
to completion"), the ⏸ counterpart of `EvalString`; the agent's DRIVER (which does know about
`agent`/`sessions`) is a Lua const **in main.go** (the binary, not the kernel). This way the core
doesn't coin product vocabulary and the CLI orchestrates like a user would. `EvalTaskString`/`SetStringGlobal`
are Go interface of the binary (like `EvalString`/`Boot`/`RenderBareScreen`), **outside api.md**:
api.md stays INTACT, APILevel stays at 2, no `G##` finding (corollary satisfied).

**Decision 2 — passing CLI arguments to the driver WITHOUT injection.** The prompt may carry
quotes/newlines; interpolating it into the driver's code would be an injection. `Runtime.SetStringGlobal(name, value)`
was added (sets a Lua string global from Go, under the token); the
binary sets `NU_CLI_PROMPT`/`NU_CLI_MODEL`/`NU_CLI_CONTINUE`/`NU_CLI_AUTOPERM` and the driver
reads them. Booleans go as "1"/"" (the CLI only needs strings). Zero fragile escaping.

**Decision 3 — the exit code convention (architecture #5 / agente.md §5).**
`0` success; `1` execution error (chunk/turn/provider threw, or `Boot` failed); `2` invalid
usage (flags/arguments: agent mode without a prompt, or no args and no TTY); `3` permission denied
in headless mode. **3 is deliberately DIFFERENT from 1**: in headless mode a denied sensitive
tool does NOT break the turn (the agent returns the error to the model as a tool_result, which can recover,
agente.md §5), so the turn "ends fine"; but for a script/CI it's an actionable signal
distinct from an execution failure — "the model couldn't act due to permissions; add `allow` or
`--auto-permissions`" —. The stderr message names it.

**Decision 4 — `--continue` (G18), resume sugar.** G18 left `--continue` out of
the contracts as belonging to the CLI surface; here it's decided: resumes the MOST RECENT session
of the project (cwd). "Most recent" = `sessions.list(cwd)` sorting ids descending (lexicographic
order = temporal, sesiones.md §2/§7) and taking the first one, which is passed as `resume` to
`agent.session{...}`. `--continue` with no prior sessions → actionable EAGENT (code 1), not a
silent new session. The project is `enu.fs.cwd()` (where `enu` was launched).

**Decision 5 — flags.** `-e '<lua>'` (from S01, consolidated); `-p '<prompt>'` (headless
agent turn; prints the assistant's final text to stdout); `--auto-permissions` (→
`permissions.mode = "auto"`); `--model 'prov/model'` (→ `opts.model`, overrides `agent.toml`);
`--continue`/`-c`. Agent mode requires a non-empty prompt (without one, the modifiers have no
turn to run → code 2). Separating parsing (`run`) from execution (`runWith(rt, opts)`)
makes the CLI testable without launching the process.

**IMPLEMENTATION FINDING (not `G##`; no doc change except this record).** The
detection of "permission denied" in the driver is done by observing the `agent:tool.end` event
(agente.md §4) and looking at its error text (stable coupling, like CP-10: the wording is set by
the `agent` extension frozen in S39). **While implementing it, it was discovered that a SCALAR upvalue
mutated from the event handler did NOT propagate** back to the driver's thread: the detection
would fail intermittently with `local denied = false; ... denied = true`. **Cause:** `enu.events`
handlers run on an EPHEMERAL thread (ADR-008/S10, `callEventHandler`); mutating the scalar
upvalue from that thread is not reliably visible from the driver's thread, but mutating the CONTENTS
of a captured TABLE IS (the table is a shared reference). **Fix (without touching the
core):** the driver stores state in a table (`local state = { denied = false }`;
`state.denied = true`) — the same pattern already used by the agent and the chat for state
shared between handlers. It's not a `G##` (no missing API and no contract crack: it's a
discipline for using upvalues with handlers on an ephemeral thread, which the extensions already followed).

**Supporting refactor.** `structuredFromError` (errors.go) now delegates to a new
`structuredFromValue(LValue)`, which recovers the structured error (§1.4) from the raw `errValue`
(an `LValue`) that a task stores when it throws — `EvalTaskString` needs it, where the error
doesn't arrive as a Go `error` but as the Lua value. S02's 🔒 invariant intact (doesn't swallow or
rewrite the code).

**Tests** (`main_test.go`, package main, HERMETIC): `runWith` over a Runtime with test
dirs and headless. `enu -e` success (0) + structured error (1, stderr with code, nothing on stdout) +
syntax (1); agent with `--auto-permissions` (write_file GRANTED, 0, file created) vs without it
(DENIED, 3, stderr names `--auto-permissions`/`allow`, file NOT created); read_file (read
only) without auto-permissions → 0; `--continue` resumes the MOST RECENT (3 sessions set up; the
latest one's JSONL grows and contains the new prompt, the old ones untouched); `--continue` with no
sessions → 1; agent mode without a prompt → 2. Compiled-binary smoke test: `-e` (0/1), usage without args
without TTY (2), `-p` without the extension active (actionable EAGENT, 1).

**Verification.** `CGO_ENABLED=0 go build ./...` and `go vet ./...` green; `gofmt -l` clean;
`CGO_ENABLED=1 go test -race -timeout 120s -count=2 ./internal/...` green (~69 s) and the root
(`.`) green under `-race -count=2`; no regression on S01–S44.

**CLOSURE.** With S45, Phase 8 and the entire plan are COMPLETE (45/45 sessions, all 8 phases
marked). Only what's MANUAL and not runnable in headless CI remains: CP-7 (real-TTY keyboard) and
CP-11 against a real provider (network/credentials).

## Post-plan coherence closure — P21 (adaptive thinking, postponed) + fix for `EvalTaskString`'s spurious log

This **is not a plan session** (the plan is closed, 45/45; the pointer doesn't move and no
row is added to `implementacion.md`'s logbook): it's a coherence closure that captures, through the
design flow (CLAUDE.md), two loose ends that the reviews uncovered. The sacred API
(`api.md`) is NOT touched; the providers contract (`providers.md`) isn't either (P21 is POSTPONE, not
resolve).

**1. P21 — mismatch of the canonical `thinking` model (POSTPONED, not resolved).** The S37
review (Anthropic adapter) made clear that `providers.md` §2.1's canonical model freezes
`thinking?: { budget?: integer }`, and `adapter_anthropic.lua` (`to_wire`) translates it into the
*legacy* extended-thinking form `{type="enabled", budget_tokens=N}`. Opus 4.6+ models (incl.
`claude-opus-4-8`) have **retired** `budget_tokens` and expect `thinking: {type:"adaptive"}`: a
request with `thinking.budget` against them would get a 400 from the real API. **It's not a code
defect** — the adapter faithfully honors the frozen contract and its tests use recorded SSE, not
network — but a crack in the **canonical model** that's better NOT decided yet: changing the
thinking model is cross-cutting (§2.1 + the adapter + possibly the agent's reasoning control)
and isn't urgent without a real consumer. It stays as **[P21](pospuesto.md)** with its trigger
(connecting the adapter to the real API with an Opus 4.6+ and getting a 400, or wanting
first-class adaptive thinking). Neither `providers.md` nor the adapter was touched.

**2. Fix for `EvalTaskString`'s spurious log (S45 cosmetic debt).** `EvalTaskString` (eval.go)
is the binary's headless executor (an agent turn, `--continue`...): it launches the chunk as a
task and, when it finishes, **collects its outcome** — including an error, via `t.errValue` — and
returns it to the caller (which the CLI maps to an exit code). But the task was launched with
`spawn`, which leaves it with `awaited=false`; on a LEGITIMATE error path (e.g. `--continue` with no
sessions, a turn that throws `EPROVIDER`), `runTask` (scheduler.go) would write the best-effort
line *"a task finished with an error and nobody awaited it"* — noise, because the error DOES
propagate, it isn't lost—.

*Fix (minimal and race-free).* The best-effort line warns about errors that get LOST (fire-and-
forget); when the host consumes the outcome, it doesn't apply. `spawnConsumed` is added
(scheduler.go), which pre-marks `awaited=true` on the `task` **before** launching its goroutine
(`go runTask(t)`), and `EvalTaskString` uses it instead of `spawn`. The key to the synchronization: the
flag is set before the goroutine is created, which establishes the happens-before; that way
`runTask`'s read (which evaluates the log under the token) sees `awaited=true` **without a data race**
and in time. The alternative of marking it from the host after `spawn` had a double crack: (a) a
data race with that read under the token, and (b) it would arrive late if `runTask` had already run
the log (the host releases the token before `spawn`, so `runTask` can complete before the host
touches anything again). `spawn` is refactored into a common body `spawnTask(fn, args, awaited)`;
the other callers (`taskSpawn`, `all`/`race`) keep passing `awaited=false`, untouched. The
scheduler's model, cancellation, and the watchdog don't change. The semantics of `t.awaited` is
broadened in its comment: "someone awaited it **or the host consumes the outcome synchronously**".

*Tests.* `TestEvalTaskStringErrorNoSpuriousLog` (scheduler_test.go): a task launched by
`EvalTaskString` that throws a structured error (a) still returns it as a `*StructuredError`
with the `code` intact and (b) does NOT leave the "nobody awaited it" line in the log. Its
counterpart `TestUnhandledTaskErrorLogged` (fire-and-forget via `enu.task.spawn` through `EvalString`,
which does go through `spawn` with `awaited=false`) stays green: the legitimate warning for a lost
error is NOT disabled — the fix is strictly for tasks consumed by the host—.

**Verification.** `CGO_ENABLED=0 go build ./...` and `go vet ./...` green; `gofmt -l` clean;
`CGO_ENABLED=1 go test -race -timeout 120s -count=2 ./...` (includes package main) green. The
pre-existing S41 flake `TestMCPToolServerError` is prior debt, not from this change.

