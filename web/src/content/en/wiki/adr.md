# Technical decision record (ADR)

Lightweight format: context → decision → consequences. One entry per
decision, numbered; entries are never rewritten: if a decision changes, a
new one is added that replaces it (supersedes).

States: **Accepted** · **Proposed** · **Open** (still undecided) ·
**Replaced by ADR-NNN**.

---

## ADR-001 · Go as the core language

**Status:** Accepted · 2026-06

**Context.** The project was born as a reaction to the dependency hell of
JS/TS in current harnesses. We need: a single binary with no runtime,
trivial cross-compilation, good concurrency support (SSE streaming,
subprocesses, concurrent UI), and high iteration speed while the extension
API is still in flux. Candidates evaluated: Go, Rust, Zig, C.

**Decision.** Go, with `CGO_ENABLED=0`.

**Reasoning.**
- A static binary and cross-compilation solve distribution (the antithesis
  of npm).
- The harness's real workload (concurrent IO) is Go's strong suit.
- Direct prior art: Crush (Charm) and OpenCode's original TUI are Go.
- Rust (ratatui + mlua) was the second serious candidate; it's discarded for
  iteration speed during the design phase, not for capability. Codex CLI
  (rewritten from TS to Rust) validates that both paths work.
- Zig/C discarded: months of infrastructure that Go/Rust give away for
  free.

**Consequences.** We give up embedded LuaJIT (it would require cgo).
Scripting performance is bounded by gopher-lua → reinforces ADR-004.

---

## ADR-002 · Lua (gopher-lua) as the extension language

**Status:** Accepted · 2026-06 · *Note (2026-07-12, G50): the core of the
decision —Lua as the extension language, versus Starlark/JS/WASM— remains
valid. Its realization (gopher-lua, Lua 5.1, and the consequence "not
thread-safe conditions concurrency") was replaced by ADR-019/ADR-020:
PUC-Lua 5.4 compiled to WASM over wazero; the M17 removal eliminated
gopher-lua from the binary and from `go.mod`.*

**Context.** Extensibility is the product. Candidates: Lua (gopher-lua or
LuaJIT/cgo), Starlark, Risor/Tengo, JS via goja, WASM.

**Decision.** Lua 5.1 embedded via gopher-lua (pure Go).

**Reasoning.**
- Lua is culturally proven as an extension language (Neovim, wezterm, mpv,
  hammerspoon): user familiarity is a feature.
- gopher-lua keeps the binary static without cgo (consistent with ADR-001).
  LuaJIT would give real performance but breaks cross-compilation and the
  single binary.
- Starlark: parallelizable but deliberately limited (no while, no
  recursion); incompatible with "Lua can do anything."
- goja (JS): same single-threaded model, and reintroduces the culture we're
  avoiding.
- WASM: sandboxing and multi-language, but authoring DX far inferior to 30
  lines of Lua. It will be reconsidered only if third-party sandboxing
  becomes a hard requirement.

**Consequences.** Lua 5.1 (not 5.4). Interpreter performance: heavy work
must live in Go primitives (ADR-004). gopher-lua is not thread-safe →
conditions the entire concurrency model.

---

## ADR-003 · Minimal core: the agent and MCP are official extensions

**Status:** Accepted · 2026-06

**Context.** Two possible models: core-with-hooks (Neovim: the main program
in native code, extensions decorate it) or kernel-runtime (Emacs/Textadept:
the entire program written in the extension language on top of a kernel of
primitives).

**Decision.** Kernel-runtime. The Go core contains no agent, MCP, chat, or
command logic: all of that consists of official Lua extensions, embedded in
the binary with `go:embed` but with no architectural privilege whatsoever.

**Reasoning.**
- "Lua can do anything" requires official features to be buildable with the
  public API; if not, the API is incomplete. Structural dogfooding (like pi
  with its own features).
- The radical user doesn't fork: they replace extensions.
- `go:embed` preserves the batteries-included experience.

**Consequences.** The kernel's surface of primitives grows (HTTP/SSE, spawn
with streams, full UI): the conceptually minimal core needs a large stdlib.
Core API stability becomes critical from v1 on: breaking changes break us
first and the ecosystem second.

---

## ADR-004 · Hybrid concurrency model ("browser model")

**Status:** Accepted · 2026-06

**Context.** An agent is inherently concurrent (token streaming, parallel
tool calls, simultaneous UI input). gopher-lua is not thread-safe. The
Neovim model (everything on one thread) produces the hangs under heavy work
that we want to avoid. Alternatives evaluated: (1) single state + event
loop, (2) pure actors with message passing per extension, (3) extensions as
subprocesses, (4) switching runtimes (Starlark/WASM).

**Decision.** A three-legged hybrid:
1. Single-threaded main Lua state with an event loop and async via
   coroutines (Node/libuv/`vim.uv` pattern) for UI, hooks, and
   orchestration.
2. Explicit workers (`worker.spawn()`): additional Lua states in their own
   goroutines, with no shared memory, message passing.
3. Internally parallel Go primitives for everything universally heavy
   (search, diff, parsing, highlighting, markdown).

Golden rule: **"Lua decides, Go executes."**

**Reasoning.**
- A harness is not an editor: it doesn't keep giant buffers re-highlighted
  on every keystroke. Its heavy tasks can be delegated to parallel
  primitives.
- Single-threading in the main state is a feature (determinism, zero data
  races) for 95% of plugins; the remaining 5% has opt-in workers.
- Subprocesses as the main model: unacceptable latency for UI hooks, and it
  reintroduces distribution friction (it remains as Layer 2).
- It's the model already validated by the web platform and by Luau
  (Roblox's actors).

**Consequences.** We need to build the equivalent of "luv for Go" (event
loop + coroutine bridge): the core's largest initial engineering cost.
Markdown/highlighting enter the kernel as builtins for performance,
consciously violating the purity of the minimal kernel. Isolation
granularity remains open (ADR-008).

---

## ADR-005 · LLM providers: TOML registry + Lua adapters

**Status:** Accepted · 2026-06

**Context.** Providers differ in protocol (SSE, tool calls, system prompts,
thinking blocks): that's code. But endpoints, keys, models, and limits are
data. Where does each thing live?

**Decision.** TOML declares the registry (data); protocol adapters are
official Lua extensions (code). The kernel only provides the HTTP/SSE
primitive.

**Reasoning.**
- Consistent with ADR-003: implementing protocols in the core would
  contradict the minimal kernel.
- Parsing SSE in Lua is feasible: text at human-reading speed.
- Adding an unusual provider (Ollama, vLLM, corporate proxy) becomes a Lua
  file, with no recompiling or waiting for a release.
- The common user's configuration remains declarative and simple (TOML).

**Consequences.** The kernel's HTTP client must expose first-class response
streaming from v1.

---

## ADR-006 · TUI: kernel library

**Status:** Proposed · 2026-06

**Context.** Candidates in Go: Bubble Tea + Lipgloss (+ glamour for
markdown) or tview. The choice is coupled to ADR-007 (what UI API is
exposed to Lua): the kernel could even use its own terminal primitives.

**Decision (provisional).** Bubble Tea + Lipgloss as a starting point, to
be revisited once ADR-007 is closed.

**Consequences.** None irreversible as long as the Lua UI API doesn't
expose Bubble Tea concepts directly (it shouldn't: the public API is ours,
the library is an implementation detail).

---

## ADR-007 · UI API exposed to Lua

**Status:** Accepted · 2026-06 (the *pending spike validation* was closed
by the S28 spike without triggering the veto:
[ADR-012](#adr-012--result-of-the-adr-007-spike-the-toolkit-is-built-in-lua))

**Context.** If the chat UI is an extension (ADR-003), the UI API must be
rich enough to build it entirely from Lua. Options evaluated: (A)
Neovim-style buffers and windows, (B) a widget tree retained in the core,
(C) an immediate-mode cell surface. Analysis:

- **A (buffers)**: a model familiar to the audience with good
  composability, but a harness's UI isn't plain text — mapping chat,
  collapsible tool calls, and diffs onto buffers is the same contortion
  (extmarks, virtual text) suffered by the chats-in-Neovim we're fleeing.
  Discarded.
- **B (widgets in the core)**: the best fit for a harness's UI and the best
  performance with gopher-lua (Lua mutates nodes, Go does
  layout/diff/render), but the project's biggest risk: freezing a GUI
  framework badly inside the core's sacred API, and the most opinionated
  option (tension with "Lua can do anything").
- **C (cells)**: a minimal core API, trivial to freeze, maximum
  philosophical coherence, but the worst performance (Lua inside the render
  loop, no JIT) and no built-in composition between plugins.

**Decision.** A B+C synthesis, in series: each option neutralizes the
other's worst flaw.

1. **Core primitive: cells + regions + compositor in Go.** Not just "put a
   character at (x,y)": regions with z-order, blitting of pre-rendered
   blocks, and damage tracking. The compositor, diffing, and painting live
   in Go.
2. **Expensive rendering is a Go primitive** (the `text` module): markdown
   → styled lines, wrapping, width measurement. Lua places blocks, not
   cells, on the hot paths.
3. **The widget toolkit is an official Lua extension**, internally retained
   (it keeps its own tree, recalculating only dirty nodes). It provides
   slots, focus, and composition across plugins. It's versioned separately
   from the core: it can iterate and break before its 1.0 without touching
   the sacred API.
4. **Coalescing in the core**: changes are batched and repainting happens
   at most every ~30 ms (the UI repaints on events, not at 60 fps).

This is the ADR-003 pattern applied a second time: the core doesn't know
what a widget is; if the toolkit can't be built well on top of the cells,
the primitive is incomplete.

**Pending validation (pre-committed veto criterion).** Spike: cells/regions
primitive + compositor + minimal Lua toolkit (container, text, input,
list), tortured with two cases: (a) full-screen token streaming with
markdown, (b) a fuzzy picker over ~100k files (filtering as a Go primitive,
Lua only repaints what's visible). If the Lua toolkit doesn't keep both
smooth, **fallback**: move the toolkit implementation to Go (the classic
option B) *keeping the same public API* toward extensions — the toolkit's
API design isn't thrown away. Once the spike passes, this ADR is promoted
to Accepted.

**Consequences.** The ecosystem's success depends on the official toolkit
being good from day one (extensions will inherit its quality). The frozen
v1 API is only the small one (cells/regions/input/text). Alternative UIs
(even a Neovim-style buffer-based one) can coexist as extensions competing
with the official toolkit. Reinforces ADR-006: the Go TUI library remains
an implementation detail of the compositor.

---

## ADR-008 · Isolation granularity: workers per task, shared main state

**Status:** Accepted · 2026-06

**Context.** With ADR-004 decided, a finer question remains: is isolation
opt-in per task (all extensions share the main state and spawn ephemeral
workers when needed) or per plugin (each extension permanently lives in
its own actor)? It affects: composability between plugins (requiring one
another), failure containment, latency of synchronous UI hooks, and API
complexity.

**Decision.** Per-task: all plugins share the main state by default;
isolation is opt-in per task via `worker.spawn()`. With three rules:

1. **Workers have no access to the `ui` module.** The screen is only
   painted from the main state (like Web Workers with respect to the DOM).
   The worker returns results by message and the main state updates the
   UI.
2. **Watchdog with cancellation.** Each handler in the main state has a
   time budget; if it exceeds it, the core aborts it via gopher-lua context
   cancellation and flags the plugin as suspect/disableable.
3. **`pcall` at every hook boundary.** An error in a plugin never brings
   down the event loop or affects other plugins.

Messages between a worker and the main state are **copies** (Lua tables
don't cross states): a worker must return digested results, not massive
raw data.

**Reasoning.**
- Composability is the secret ingredient of the Neovim ecosystem: plugins
  that `require` each other, plugin-libraries (plenary), extensions of
  extensions (telescope). With isolated actors, "using another plugin"
  would become asynchronous RPC with serialization — closures can't be
  passed through a channel — and that ecosystem could never be born.
- Synchronous hooks (keymaps, render) need an immediate response; with
  actors they would become blocking round-trips with deadlock risk, or
  every hook would have to become async.
- Per-plugin actors: N states = N stdlibs in memory, copies at every
  boundary, a harder API for the 20-line plugin.
- The watchdog + pcall cover most of the robustness gap: containment of
  errors and infinite loops (more than Neovim offers out of the box).

**Consequences.** Consciously accepted risks: a memory leak in one plugin
bloats the whole process, and the watchdog doesn't protect against "death
by a thousand cuts" (many slow handlers each within budget). Per-plugin
actors remain a possible future evolution (e.g., `isolated = true` in the
manifest for untrusted plugins), but not in v1: two execution modes would
duplicate the semantics of every hook. The workers-without-UI rule
simplifies ADR-007: only the main state paints, so the UI model doesn't
need to be thread-safe or multiplex concurrent authors.

---

## ADR-009 · API conventions: global namespace, async via coroutines, structured errors

**Status:** Proposed · 2026-06 (accepted once [api.md](api.md) is frozen)

**Context.** Before writing code, the v1 API is formally defined
([api.md](api.md)). Three cross-cutting decisions need their own record.

**Decision.**

1. **Global namespace `enu`** with submodules (`enu.fs`, `enu.ui`, ...), like
   Neovim's global `vim`; `require` is reserved for plugin modules. Lua's
   blocking stdlib (`io`, `os.execute`, ...) is disabled: all IO goes
   through the core's async primitives, or it would freeze the event loop.
2. **Async via suspending functions**: inside a task (a scheduler
   coroutine), IO primitives are called in sequential style and suspend
   until completion (implicit await, OpenResty's cosockets pattern).
   Synchronous handlers (input, events) cannot suspend: they spawn tasks.
   No nested callbacks or explicit promises in the API.
3. **Thrown structured errors** (`error({code, message, detail})`,
   catchable with `pcall`) instead of the `res, err` style. Reserved codes
   (`ENOENT`, `ETIMEOUT`, `ECANCELED`, `EBUDGET`, ...). Reason: thrown
   errors compose across layers of extensions and aren't silently ignored;
   `res, err` gets lost at the first slip.

**Consequences.** The trivial-plugin DX is sequential code with no visible
async concepts. Disabling `io`/`os` breaks compatibility with pure Lua
libraries that use them (assumed: the target ecosystem writes against
`enu.*`). The scheduler's coroutine↔goroutine bridge is the kernel's central
piece (consistent with ADR-004).

---

## ADR-010 · Official extensions: distributed with nu, not active by default

**Status:** Accepted · 2026-06 (modifies a consequence of ADR-003 and
philosophy principle 5) · **Refined by
[ADR-015](#adr-015--official-product-set-and-non-interactive-onramp)**
(what "the official set" is and how it's activated without a TTY; it
doesn't replace it: "inactive by default" remains this ADR's)

**Context.** ADR-003 decided to embed the official extensions (`go:embed`)
"preserving the batteries-included experience," which implied activating
them by default. When resolving G6 (caps packages as tables of the agent
extension) the question was reopened and a more austere model was decided.

**Decision.** Official extensions (agent, chat, providers, MCP, toolkit,
`agent.caps.*` packages) **are not active by default**: they're distributed
with nu, but whoever wants them activates them. Activation is explicit and
trivial (config, or first boot, one keystroke). Distribution: they remain
embedded in the binary — inactive — so as not to break the "one binary,
offline" promise (activating requires no network).

**Reasoning.** Radical coherence with "the core doesn't know what an agent
is": it doesn't presuppose one either. Installed nu is a bare runtime; the
harness is a user choice, not a fait accompli. Same mental model as Neovim
(the editor doesn't ship with plugins activated) — the target audience
expects it this way.

**Consequences.** The first boot must offer activation of the official set
(without it, the first experience would be an empty screen); "agent working
within the first minute" goes from automatic to "one keystroke away."
Philosophy §5 is rewritten. `enu.toml` moves from `plugins.disabled` to
governing activation (`plugins.enabled` or equivalent — a loader detail).

---

## ADR-011 · Scheduler realization: goroutine-per-task + Lua execution token

**Status:** **Replaced by [ADR-020](#adr-020--el-puente--definitivo-tasks-como-corrutinas-lua-nativas-reemplaza-adr-011-en-la-conmutación)**
· the **M16** switch made wasm the default backend and the **M17** removal
([migracion-vm.md](archive/migracion-vm.md)) eliminated gopher-lua from
`go.mod` and from the binary, erasing the goroutine-per-task scheduler this
ADR realized; the definitive ⏸ bridge (tasks as native Lua coroutines) is
now described by ADR-020. As the project's workflow mandates, the body is
not rewritten: it remains as a historical record of *how* ADR-004 was
realized on top of gopher-lua. · Originally Accepted · 2026-06 (refined
*how* ADR-004 is realized on top of gopher-lua; it didn't change its
observable semantics or the [api.md](api.md) API)

**Context.** ADR-004 fixed the "browser model" (single-threaded main Lua
state, async via implicit await) and anticipated "the coroutine bridge"
(event loop + Lua-coroutines ↔ goroutines) as its largest cost. When
implementing the keel (S04), a runtime crack was discovered (problemas.md
G31): **gopher-lua —Lua 5.1 semantics— doesn't allow a coroutine to yield
across a Go call boundary.** Specifically, verified against gopher-lua
v1.1.2:

1. `pcall(fn)` where `fn` suspends: the coroutine **aborts** at the `pcall`
   instead of yielding. But [api.md](api.md) §1.4 promises that structured
   errors "are caught with `pcall`," and the pseudocode
   ([pseudocodigo.md](pseudocodigo.md) §§ tool runner, parallel branches)
   wraps IO-performing operations (⏸) in `pcall`. The entire error model
   rested on something the runtime doesn't support.
2. `return ⏸fn()` in tail position: `OP_TAILCALL` elides the caller's frame
   *before* the Go function yields, losing the continuation; the task
   "ends" instead of suspending.

Both share the same root cause (coroutine `yield` doesn't cross Go
boundaries) and aren't fixed in the spec: the API is correct; what fails is
the *technique* used to realize the bridge.

**Decision.** Realize the scheduler **without coroutine yields**: a
**goroutine per task** + a **single Lua execution token** ("GIL"):

1. Each task runs in its own goroutine, on its own Lua thread
   (`*lua.LState` child of the main one; they share the `G` globals).
2. A token (capacity-1 channel) guarantees that **only one goroutine
   touches Lua at a time** — the single-threaded invariant of
   ADR-004/008.
3. A ⏸ primitive doesn't yield a coroutine: it **releases the token**, does
   the blocking work on a background goroutine (which never touches Lua)
   and, on completion, **reacquires the token** and returns normally.

Since there's no yield, the task's Lua stack lives on its Go stack:
`pcall`, tail calls, and error unwinding are gopher-lua's **native** ones
and survive suspension. `Task:await` becomes a pure Go function that
re-raises the awaited task's error with `L.Error` (catchable with `pcall`).

**Reasoning.** This is the other canonical realization of the "browser
model" on a Lua-in-Go runtime (the cooperative "giant lock" pattern). It
kills both cracks at the root instead of patching them (Lua trampolines for
the tail case, and a `pcall` rendered as a sub-coroutine for case 1 — both
more invasive and still fragile). ADR-004's observable semantics remain
intact: logically single-threaded Lua, implicit await, zero data races (now
via the token, with channel handoff = *happens-before*; validated with
`-race`).

**Consequences.** ADR-004's "event loop + event queue" is realized as token
+ goroutines, not as a loop that resumes coroutines; S04's description in
[implementacion.md](implementacion.md) should be read through that lens.
The per-task cost rises from a coroutine to a goroutine (+ a Lua thread) —
cheap in Go and acceptable for a harness's task volume. Detecting "am I in
a task" (to veto ⏸ outside a task, §1.3) is done by execution state: the
main chunk and synchronous handlers run on the `host` state; tasks run on
their own thread. The pieces that presupposed a central loop resuming
coroutines (S05 timers, S10 event dispatch) are built on this model: a
"loop tick" is work that takes the token in the main state. S09's
**watchdog** (per-slice budget) and S08's **non-catchable unwinding**
(cancellation/`EBUDGET` that `pcall` doesn't catch) are designed already
knowing that `pcall` is gopher-lua's native one: the non-catchable abort
will need its own mechanism (a sentinel panic the kernel recognizes and
doesn't let the user's `pcall` swallow), not the `yield` discarded here.

---

## ADR-012 · Result of the ADR-007 spike: the toolkit is built in Lua

**Status:** Accepted · 2026-06 (closes the *pending validation* of
[ADR-007](#adr-007--ui-api-exposed-to-lua) and **open question #1** of
[arquitectura.md](arquitectura.md); ADR-007 is promoted to Accepted as a
result)

**Context.** ADR-007 fixed the UI API (cells + regions + compositor in Go,
expensive rendering in Go, **widget toolkit as a Lua extension**) with a
**pre-committed veto**: if a Lua toolkit doesn't keep the UI smooth, the
toolkit implementation moves to Go (classic option B) while keeping the
public API. Session S28 ([implementacion.md](implementacion.md), veto
milestone) built a **minimal, internal** version of the primitive —cell
grid (`rune`+`style`), regions, `blit` of a Block (S22) with viewport and
clipping on both ends (G28), grid diff → ANSI to an in-memory buffer, frame
coalescing— plus a **Lua shim** that orchestrates it, and tortured it with
the two agreed-on workloads, measuring **the compute cost of the
compose+diff+encode pipeline plus the overhead of orchestrating from Lua**
against doing everything in Go.

**Environment limitation (declared).** The spike ran **headless** (no
TTY): the diff is serialized to an in-memory buffer, not to a terminal.
Therefore what's measured is the **compute cost** (compose + diff + encode
to ANSI + the Go↔Lua crossing), **not** the terminal's physical latency
(pty bandwidth, vsync), which is identical whether Lua or Go is chosen.
This is exactly what the veto puts at stake —Lua's performance without JIT
on the hot path, limitation #8 of
[modelo-ejecucion.md](modelo-ejecucion.md)—; TTY physics doesn't
discriminate between the two options, so excluding it doesn't bias the
decision.

**Smoothness threshold (pre-committed).** Case (a) full-screen markdown
streaming (120×40): one frame (compose+diff+encode **+** Lua overhead)
**≤ 8 ms** (a quarter of a 30 fps frame's budget, ~33 ms; leaves headroom
for the rest of the turn —HTTP/SSE/parse— and for slower hardware). Case
(b) fuzzy picker over ~100k files: one keystroke (fuzzy over 100k +
rendering the visible window) **≤ 50 ms** (the bound below which filtering
feels instant). **Attribution criterion:** since ADR-007's question isn't
"is rendering fast?" but "does *Lua overhead* break smoothness compared to
Go?", the veto only fires if a case goes over budget **and** the culprit is
the overhead of orchestrating from Lua (not the Go primitive, which the
veto wouldn't fix).

**Measurements** (Intel Xeon @ 2.10 GHz, 4 cores; real times, **without**
`-race` —the race detector inflates ~7× and doesn't represent production
cost—; `go test`/`go test -bench`):

| Case | Metric | Pure Go | Lua-orchestrated | Budget |
|---|---|---|---|---|
| (a) markdown streaming, 311 frames | p50 / p99 per frame | ~0.4 ms / ~1.8 ms | ~0.4 ms / ~1.8 ms | ≤ 8 ms |
| (b) picker 100k, 7 keystrokes | p50 / p99 per keystroke | ~31–45 ms / ~52–74 ms | ~30–38 ms / ~40–53 ms | ≤ 50 ms |

Benchmarks (`ns/op`): the compose+diff+encode pipeline isolated to full
screen (`BenchmarkSpikeComposeOnly`) **~0.37 ms/frame**; with per-token
markdown re-rendering (`BenchmarkSpikeStreamGo`) **~0.72 ms/frame**; one
picker keystroke over 100k (`BenchmarkSpikeFuzzyKeyGo`, typical query)
**~31 ms**.

**The key finding.** The **overhead of orchestrating from Lua is
negligible** in both cases (case (a): the Lua−Go difference is within the
noise, ±tens of µs; case (b): Lua matches or beats Go within variance). The
reason is structural and confirms the ADR-004/ADR-007 design: **all the
heavy work is a Go primitive** (S23's markdown rendering, S27's fuzzy
scorer, and the compose/diff/encode pipeline itself), and **Lua does only
~3 Go↔Lua crossings per frame** (request the Block, blit it, fire the
frame). The hot loop doesn't run heavy logic in the Lua interpreter, so its
lack of a JIT never comes into play.

**Decision.** **The veto does NOT fire.** The widget toolkit (S42) is
built **in Lua**, as ADR-007 proposed. Phase 8 of the implementation plan
stays as is (S42 = Lua extension); it is **not** reordered. ADR-007 is
promoted from *Proposed (pending validation)* to **Accepted**.

**Consequences.**
- Case (a) fits with **two orders of magnitude of headroom** (p99 ~1.8 ms
  against an 8 ms budget): full-screen token streaming with markdown is
  not a performance problem for a Lua toolkit.
- Case (b): p50 (~31–45 ms) fits within budget; **p99 (~52–74 ms in pure
  Go) skirts or exceeds it**, but the outlier is the **single-character**
  keystroke (`"r"`), which matches ~all 100k files —a pathological case a
  real picker rarely hits— and the cost lives in the **Go primitive**
  (`fuzzyScore` scanning 100k candidates), **not** in the Lua crossing:
  moving the toolkit to Go wouldn't fix it. It remains a **performance
  observation, not a veto**: if in production the picker over huge repos
  feels slow with very short queries, the fix belongs to the
  `enu.search.fuzzy` *primitive* (S27) —e.g., parallelizing the scoring, or
  a minimum query-length threshold in the toolkit— not to the UI
  architecture. It doesn't open a `G##`: API §11 and §9 suffice; it's a
  note for future optimization.
- The real compositor (S29) and the region lifecycle (S30–S33) inherit the
  model validated here (flat cells, run-based diff, blit as a copy with
  viewport G28, coalescing). The spike's code is **internal and
  disposable** (`internal/runtime/spike_*.go`): it is not the public API
  §9, nor does it extend it; S29 replaces it with the production
  implementation.

---

## ADR-013 · Continuous integration and release publishing

**Status:** Accepted · 2026-06 · **Refined by [ADR-021](#adr-021--baseline-completo-y-reproducible-de-lint-antes-de-congelar-v1)**
(the transitional `only-new-issues` mode is retired once the debt reaches
zero; the rest of the decision remains in force)

**Context.** With the 45 sessions of the [implementation
plan](implementacion.md) closed, the kernel and the official extensions are
real code (a Go binary plus `internal/runtime`). Until now, quality
discipline lived only in the [CLAUDE.md](../CLAUDE.md) protocol —"every
session leaves `go build ./...` green," the 🔒 inventory of mandatory
tests— and was enforced by hand in every session. There was no continuous
integration, no configured linting, no mechanism for distributing the
binary. This decision records **how `enu` is validated and published**.
It's operator DevOps: the implementation (the `.github/workflows/*.yml`
files) is NOT part of the sacred API ([api.md](api.md)) nor of the
extension contracts; this ADR captures the *decisions*, not the YAML
*steps*. It sits alongside ADR-001 (Go, `CGO_ENABLED=0`) and ADR-010
(embedded inactive extensions), which describe distribution without having
fixed its pipeline.

**Decision.**

1. **CI** (`.github/workflows/ci.yml`) on every PR and push to `main`:
   formatting (`gofmt`), `go vet`, clean modules (`go mod verify` +
   diff-free `tidy`), `golangci-lint` (minimal set, see point 5), `go build
   ./...`, building the static binary with release flags, a **headless
   smoke test** (`enu -e 'return enu.version.api'`, with no secrets), and `go
   test -race` over an **`ubuntu` + `macos` matrix** (the two v1 target
   platforms). `-race` always: the 🔒 inventory includes concurrency tests
   (S07–S10) that only reveal data races under the detector. No Go version
   matrix: `enu` is distributed as a binary, not as a library that third
   parties compile; the version that matters is the one in `go.mod`, read
   via `go-version-file`.

2. **Releases** (`.github/workflows/release.yml`) on pushing a `vX.Y.Z`
   tag: cross-compiles to **`linux/amd64`, `linux/arm64`, `darwin/amd64`,
   `darwin/arm64`**, packages a `tar.gz` per platform plus a
   `checksums.txt` (SHA256), and creates the GitHub Release with
   autogenerated notes. Native Windows is **not** published: it's
   postponed ([pospuesto.md](pospuesto.md) P18) and Windows goes through
   WSL2, which uses the `linux/amd64` binary; a `.exe` would give a false
   signal of support.

3. **Versioning — "constants as source of truth" strategy.** The version
   lives in `internal/runtime/nu.go`'s constants (`VersionMajor/Minor/Patch`,
   exposed as `enu.version`). The release **does not inject** the version
   via `-ldflags -X`: it **verifies** it against the tag in a gate job and
   aborts if they diverge. The gate reads the version **by running the
   runtime** (`go run . -e '…enu.version…'`), not with a `grep` of the file:
   it uses the same composition logic (`registerNu`) as the real binary, so
   it validates exactly what the user will see, with no fragility from the
   order of the constants.

4. **Reproducible build contract.** All binaries are compiled with
   `CGO_ENABLED=0` (static, ADR-001), `-trimpath` (no CI-machine paths →
   reproducible), and `-ldflags "-s -w"` (no symbol table or DWARF →
   smaller binary; ~12 MB).

5. **Tooling: the bare minimum.** The workflows invoke `go` directly and
   create the release with a standard action
   (`softprops/action-gh-release`); GoReleaser is **not** adopted.
   `golangci-lint` is included with a deliberately small set (`govet`,
   `errcheck`, `staticcheck`, `ineffassign`, `unused`) and
   `only-new-issues: true`, so as not to block on preexisting debt.

**Reasoning.**
- **Strategy A vs `-ldflags` injection.** Injection would create two
  sources of truth (the Lua constant and `main`'s variable) that would need
  to be kept in sync, and would force adding a mutable variable in `main`
  and a `--version` flag for a purely packaging-related reason. The chosen
  strategy has **a single source of truth**, doesn't mutate code at build
  time (what's published is bit-for-bit what's in the repo, reinforcing
  `-trimpath`), and is consistent with "Lua decides, Go executes":
  `enu.version` is already the observable truth; packaging derives from it.
  The constants are **not** part of the sacred surface (they live in
  `internal/runtime`, not in `api.md`): the gate *reads* them, it doesn't
  extend them, so it doesn't brush against the §4 protocol.
- **By hand vs GoReleaser.** The scope is small and stable (4 targets, 1
  binary, no native packages, no brew tap, no Docker). GoReleaser would
  bring in an external tool with its own version, config, and "magic" —
  exactly what [philosophy §6](filosofia.md) ("zero dependency hell")
  avoids in the product and is worth avoiding in its pipeline too—. The
  hand-rolled workflow fits in readable YAML and adds nothing to maintain.
  If Homebrew tap, native packages, or Docker images are added in the
  future, this choice gets reopened.

**Consequences.**
- The [CLAUDE.md](../CLAUDE.md) protocol ("green build," the 🔒 inventory)
  stops depending only on manual diligence: CI enforces it on every PR. The
  `tidy`-check materializes "zero dependency hell" as an automatic gate.
- **Publishing implies bumping the version by hand before the tag.** The
  flow is: edit the constants in `nu.go`, commit, tag `vX.Y.Z`, push. If
  the tag doesn't match, the release fails at the gate with an actionable
  message and publishes nothing. It's deliberate friction (a check, not an
  automatism that guesses).
- **macOS in the matrix costs more minutes** than Linux. For a
  single-developer repo with low PR volume the absolute cost is small and
  is accepted in exchange for covering the second target OS; if the
  expense mattered, the lever is to keep macOS only on `push: main`. To
  *compile* the release's darwin binaries a macOS runner is **not** needed
  (Go's cross-compile runs on Linux); macOS in CI is only to *run* the
  tests natively.
- **License:** resolved in [ADR-014](#adr-014--license-apache-20) (Apache
  2.0). The release's `tar.gz` files include the binary; `LICENSE` and
  `NOTICE` live at the repo root.
- **Pending on the project owner, outside this ADR:** a `--version` flag in
  the CLI would be a product nice-to-have (it touches S45's CLI surface),
  not a requirement of this pipeline; signing binaries (cosign/GPG), a brew
  tap, and Docker remain future improvements that would reopen point 5.

---

## ADR-014 · License: Apache 2.0

**Status:** Accepted · 2026-06

**Context.** The kernel is already real code and is going to be
distributed (ADR-013), but the repo had no license: without one, legally
nobody can use or redistribute `enu`. The author wants two things at once,
seemingly in tension: (1) for it to be **truly open source**, to
contribute to the community and maximize adoption, and (2) to keep the
option of **commercializing or selling it** in the future if the project
takes off (the pattern of products like pi/pdf.ai, where the owner was able
to sell). The key —and the reason there's no contradiction— is that the
power to sell/relicense **doesn't come from the license, but from
copyright ownership**: whoever owns 100% of the code can always, besides
publishing it under an open license (which is non-exclusive), offer a
proprietary license or transfer the entire project. The risk to that
ownership isn't the chosen license, but **accepting third-party code
without a rights assignment**.

On authorship: `enu`'s sole author is **Diego Barea**. The `Candela1011
<candelabr72@gmail.com>` identity that appears in the git history is not a
second author: it's the `git config` left over from the borrowed computer;
there is no co-ownership. The repo's identity was fixed to the author's
name so the authorship trail is coherent.

**Decision.** **Apache License 2.0**, copyright Diego Barea. Added to the
root: `LICENSE` (full Apache 2.0 text), `NOTICE` (the attribution the
license recommends), and `CONTRIBUTING.md`. External contributions are
handled **case by case, with no formal CLA for now**, but
`CONTRIBUTING.md` **expressly reserves** the maintainer's right to request
a rights assignment or a contribution agreement before merging third-party
code. This way ownership stays unified and the option to commercialize
stays alive, without yet imposing the friction of a CLA.

**Reasoning.**
- **Why permissive and not copyleft (AGPL/GPL).** The goal is broad
  adoption and "giving to the community." An AGPL would make `enu` viral
  copyleft (whoever runs it modified as a service must publish their
  changes), which **reduces** adoption and is used when one wants to
  continuously *force* commercial buyers — not the case here. For the
  "sellable someday" goal, ownership is enough; a permissive license
  doesn't take that away.
- **Why Apache 2.0 and not MIT.** Both are permissive and both preserve
  the right to sell. Apache 2.0 adds an **explicit patent grant** (protects
  the author and users if this becomes a business) and a contribution
  clause (§5) that fits a future CLA. The cost is a longer `LICENSE` and a
  `NOTICE`; it's worth it for a product with commercial ambition.
- **Why no CLA yet.** Today the author owns 100% and can sell without
  asking anyone's permission; a CLA is only needed once outside code comes
  in. Setting up the CLA now would be premature friction. The
  `CONTRIBUTING.md` clause avoids the real risk (someone assuming their PR
  goes in with their copyright intact) while keeping it cheap.

**Consequences.**
- `enu` is free to use, study, modify, and distribute (even commercially)
  under Apache 2.0; CI and the release can already publish with a valid
  license.
- The author retains ownership and, therefore, the ability to offer a
  proprietary version or sell the project. **Reopening trigger:** if the
  volume of external contributions grows, formalize a CLA (text + a
  CLA-assistant-type bot) so as not to have to negotiate assignments one
  by one; the framework is already announced in `CONTRIBUTING.md`.
- If an entity/company is created in the future to commercialize `enu`, the
  copyright name is updated; it doesn't require changing licenses.
- No license headers are added per `.go` file (the root `LICENSE` is
  enough for Apache 2.0 in a single-owner module); if third-party code is
  ever accepted, it will be reviewed whether marking per-file authorship
  makes sense.

---

## ADR-015 · Official product set and non-interactive onramp

**Status:** Accepted · 2026-06 (**refines**
[ADR-010](#adr-010--official-extensions-distributed-with-nu-not-active-by-default);
resolves
[G33](problemas.md#g33--el-arranque-sin-tty-no-tiene-onramp-y-el-conjunto-oficial-está-sin-definir))
· **Refined by [ADR-017](#adr-017--el-onramp-deja-config-de-agente-usable-y-el-chat-degrada-con-gracia)**
(the onramp also leaves agent config usable) and by **ADR-018** (what "the
official set" means with a TTY: the repl hands the screen to chat, G36);
neither replaces it: the "official set" and the two modes remain this
ADR's

**Context.** ADR-010 left the official extensions **inactive by default**
and [G21](problemas.md#g21--el-primer-arranque-de-adr-010-no-tiene-dueño--adr-010--apimd-14)
gave them the first-boot onramp: the **bare-runtime screen**. But that
screen is UI —it exists **only with an interactive TTY**—; [api.md](api.md)
§14 closes it explicitly: "No TTY, no screen: it boots bare." When *using*
the already-finished binary to try it with its harness in CI/Docker/scripts
(no TTY), two loose ends ADR-010 didn't tie up appear: (1) **there is no
step** to activate the official set without a TTY —you have to write
`config.dir()/enu.toml` by hand, which contradicts the "one-keystroke"
ergonomics ADR-010 itself promises—; and (2) **"the official set" was never
precisely defined**: today `ActivateOfficial()` activates *all* of
`embeddedNames()`, which includes `example` —the scaffolding plugin that
exists solely to test the gating ([implementacion.md](implementacion.md),
Phase 8)—, so the TTY action already puts the test plugin into the user's
config.

**Decision.** Two pieces, **neither in the sacred API** `enu.*` (it's CLI
and loader surface, not `enu.version.api`):

1. **A CLI flag, `nu --default-config`**, a non-interactive mirror of the
   bare screen's "activate the official set" action, with **two modes**:
   - **Alone** (`nu --default-config`): writes `plugins.enabled` with the
     product set to `config.dir()/enu.toml` —preserving the rest, atomic,
     idempotent, reusing the same `writeEnabledPlugins` as the TTY
     action— and **exits**.
   - **With a headless action** (`--default-config -p '…'` / `-e '…'`):
     **doesn't touch disk**; it activates the set **only for that
     process** (a new runtime option, `WithEnabledPlugins`, that fixes
     `enabled` in memory before `Boot`) and runs the action. This is the
     immutable-Docker case: running with everything active without
     rewriting config on every `docker run`.

2. **"The official product set"** is fixed at the **seven** embedded
   product extensions —`providers, sessions, agent, mcp, chat, repl,
   toolkit`— = the embedded catalog **minus `example`**. It's closed under
   dependencies (`agent → providers, sessions`; `mcp → agent`; `chat →
   toolkit, agent, providers, sessions`). A single source of truth,
   `officialProductSet` (derived from `embeddedNames` filtering out
   `example`); G21's TTY action now uses it too, so **the bare screen and
   the flag activate exactly the same thing**.

The set is **identical in both modes**, including `chat`: although
`chat`/`repl` need a TTY, their `init.lua` already self-gate with `if
enu.has("ui")` and stay inert without a UI surface (G20), so activating them
headless doesn't get in the way; having a second "no UI" list would be an
edge case with no payoff.

**Reasoning.**
- **Why a flag and not extending the API (`enu.config.enable_official()` +
  `enu -e`).** Exposing it to Lua would **expand the sacred surface**
  (`enu.version.api`++, the project's most expensive cost, and what
  [api.md](api.md) §17 shields) to *worsen* ergonomics: `enu -e
  'enu.config.enable_official()'` is no easier to remember or type than the
  flag. It fails the stated goal (easy installation) while paying the
  highest price.
- **Why a flag and not a `nu init` subcommand.** It would be honest (a
  verb for an action with disk effect), but it would debut the binary's
  **first subcommand**, which today is flags-only (`-e`, `-p`,
  `--continue`…): a door to `nu run`/`nu chat`… that S45 deliberately
  avoided, keeping the binary thin and delegating to extensions. If
  several management actions appear later, `nu config <verb>` will justify
  itself; for a single need it's premature. **Reopening trigger:** a third
  or fourth configuration action for the binary.
- **Why exclude `example` from the set.** It isn't product: it's test
  scaffolding for ADR-010's gating. That the TTY action activates it today
  is a tolerable oversight only because it's visible on screen; putting it
  into a "default config" turns it into a surprise. It remains
  activatable **loosely** (the "activate loose extensions" action and a
  hand-written `plugins.enabled = ["example"]`), which is all it needs.
- **Why it lives in the binary and doesn't break ADR-003.** The CLI
  orchestrates extensions through the public API just as a user's
  `init.lua` could: the core still doesn't know what an agent is. It's
  exactly S45's boundary (the CLI surface lives in `main.go`, not in
  `enu.*`).

**Consequences.**
- Installing `enu` and having it "batteries-included" in CI/Docker is **one
  command** (`nu --default-config`), with no hand-editing of TOML.
  ADR-010's "one keystroke" promise now also holds without a TTY.
- "The official set" has a **single definition** (`officialProductSet`);
  the bare screen (G21) and the flag can't diverge. `ActivateOfficial()`
  stops activating `example`: an observable behavior change, covered by
  its test.
- The sacred surface **does not grow**: `enu.version.api` stays the same.
  The only new API is internal to the runtime (`WithEnabledPlugins`, an
  option of `runtime.New`, not `enu.*`).
- **No network** (ADR-010): activation comes from the embedded binary, in
  both modes.
- **Reopening trigger:** if the binary accumulates more configuration
  actions (several `--…-config` flags or equivalents), reconsider the `nu
  config` subcommand discarded here.

## ADR-016 · Canonical `thinking` model with `mode` and per-model translation in the adapter

**Status:** Accepted · 2026-06 (resolves [G34](problemas.md#g34--el-modelo-canónico-de-thinking-no-expresa-el-modo-adaptativo-opus-46-400ea-con-budget_tokens); **reopens and closes** [P21](pospuesto.md), which leaves the postponed list)

**Context.** The canonical model ([providers.md](providers.md) §2.1) froze
`thinking?: { budget?: integer }` and the `anthropic` adapter translates it into the
*legacy* extended-thinking form `{type="enabled", budget_tokens=N}`. The
Opus 4.6+ family —including the project's default model, `claude-opus-4-8`—
**removed `budget_tokens`** and expects `{type="adaptive"}`: a request with
`budget_tokens` against those models returns **400**. The crack isn't in the code
(the adapter complies with the frozen contract to the letter) but in the **canonical
model**, which (1) only knows how to request reasoning by *budget* and (2) has no
way to request the *adaptive mode* that modern models require. Validated in
[pseudocodigo.md](pseudocodigo.md) (Round 7, scenario 32) and logged as
[G34](problemas.md#g34). It was postponed as **P21** while there was no
consumer; the trigger —the default model is now Opus 4.8— reopens it. Today
the crack is **latent** (the agent doesn't populate `req.thinking` by default), and it
is decided now, before wiring up reasoning, so as not to build that feature on
top of a broken canonical.

**Decision.** Two pieces, **neither in the sacred `enu.*` API** (this is the model
canonical to the `providers` extension, not `enu.version.api`):

1. **The canonical parameter grows by addition** to
   `thinking?: { mode?: "off" | "adaptive" | "budget", budget?: integer }`:
   - `thinking` absent = no reasoning (today's behavior).
   - `mode = "adaptive"`: adaptive reasoning (the model decides the effort).
   - `mode = "budget"` with `budget = N`: reasoning with a budget of N tokens.
   - `mode = "off"`: explicitly disables it (to override a default).
   - **Compatibility:** `{ budget = N }` *without* `mode` is interpreted as
     `mode = "budget"` — the frozen form remains valid and means the same thing.
     Strictly additive; it breaks no signature nor any recorded tests.

2. **Each model's reasoning dialect is DATA in the registry**, not
   knowledge hardcoded into the adapter (ADR-005: *TOML declares the data, Lua
   implements the protocol*). `providers.toml` gains an optional per-model field,
   `thinking = "adaptive" | "budget" | "none"`, which travels in the
   `ModelInfo` (providers.md §2.1/§3). The adapter translates the canonical `mode`
   by reading that data:
   - `"adaptive"` dialect: `mode=adaptive` → `{type="adaptive"}`; `mode=budget`
     → also `{type="adaptive"}` (Opus 4.6+ ignores the figure: the intent "reason"
     is honored, not the dead budget).
   - `"budget"` dialect: `mode=budget` → `{type="enabled", budget_tokens=N}`;
     `mode=adaptive` → `{type="enabled", budget_tokens=<default>}` (degrades to
     the form the model understands).
   - `"none"` dialect (or absent on a model that doesn't reason): `thinking` is
     not sent; if it was requested, this is a **declared degradation** (like
     `caps`, providers.md §3 obligation 5) — the adapter doesn't invent anything.
   - `mode=off`/absent: `thinking` is never sent, whatever the dialect
     (safe on every model).
   - **Default of the field when missing:** `"budget"` (preserves legacy
     behavior). An Opus 4.6+ model is declared with `thinking = "adaptive"` in its
     entry; omitting it and requesting reasoning is an **actionable configuration
     error** (the 400 becomes an error of the user's `providers.toml`, which the
     message names), not a translator bug.

**Rationale.**
- **Why `mode` and not replacing `budget`.** The canonical model's surface grows
  by addition just like the sacred one (api.md §17): breaking `{budget}` would
  break anyone already using it and the recorded tests. `mode` subsumes it
  (`budget` = `mode:"budget"`) without breaking anything.
- **Why the dialect lives in the TOML and not in the adapter.** A "which family
  uses which form" table inside the adapter is product knowledge that goes stale
  with every new model —exactly what ADR-003/ADR-005 avoid. As registry data,
  the user (or the distributed `providers.toml`) declares it alongside `context`
  and `max_output`, and the adapter stays a pure translator. **Rejected**: inferring
  it from the model id (`model:match("opus%-4%-[6-9]")`): fragile, it puts a
  version-guessing heuristic inside a translator, and it fails with non-canonical
  ids or gateways that rename models.
- **Why default to `"budget"` and not `"adaptive"`.** There is no universally
  safe choice; the default must preserve existing behavior (legacy models) and
  let new ones declare themselves. The cost —one line of TOML per Opus
  4.6+ model— is minimal, and the error if it's omitted is actionable. (Rejected
  defaulting to `"adaptive"`: it would break legacy models for no reason.)
- **Why now, if it's latent.** Fixing the contract is cheap today and unlocks a
  first-class capability (reasoning with modern models); doing it later, with
  thinking already wired up and consumers assuming the old canonical, is
  expensive. It's the same economics as the rest of the workflow: close the
  crack in the spec before building on top of it.

**Consequences.**
- The canonical model can now **express adaptive reasoning**; Opus 4.6+
  models (including the default) are usable with reasoning without a 400.
- The sacred `enu.*` surface **doesn't change** (it's a contract of the
  `providers` extension); `enu.version.api` stays the same. `providers.toml` gains an
  optional per-model field `thinking` (compatible: absent = `"budget"`).
- **Implementation pending** (a construction session, NOT this commit, per the
  "the contract leads, the code follows" protocol): the new `to_wire` of the
  `anthropic` adapter, reading `model.thinking` in `resolve`, and —once the agent
  exposes reasoning control— mapping its option to the canonical `thinking`. The
  adapter's `⚠` note already points here.
- **Reopening trigger:** a provider with a third reasoning dialect that
  `"adaptive"|"budget"|"none"` doesn't capture (e.g., discrete "low/medium/high"
  levels); then the field's value gets generalized.

## ADR-017 · The onramp leaves usable agent config, and the chat degrades gracefully

**Status:** Accepted · 2026-06 (**refines** [ADR-015](#adr-015--conjunto-oficial-de-producto-y-onramp-no-interactivo); resolves [G35](problemas.md#g35--el-onramp-de-adr-015-activa-los-plugins-pero-no-deja-config-de-agente-el-primer-nu-muere-sin-modelo-y-deja-la-ui-atrapada))

**Context.** ADR-015 delivered the non-interactive onramp (`nu --default-config`)
that activates the **official product set** in `enu.toml`. But "activating the
plugins" isn't "having a usable harness": the agent and the chat need a
**model**, a **provider**, and an **API key** that the onramp doesn't provide. When
using the finished binary after `nu --default-config`, running `enu` leaves the
terminal blank; the log says so: `chat: could not start: agent.session requires
model ("provider/model") in opts or in agent.toml`. There are **two defects**
([G35](problemas.md#g35--el-onramp-de-adr-015-activa-los-plugins-pero-no-deja-config-de-agente-el-primer-nu-muere-sin-modelo-y-deja-la-ui-atrapada)):
(1) the onramp doesn't write `agent.toml`/`providers.toml`, so `core:ready` →
`chat.start` → `agent.session({model=nil})` throws `EINVAL`; (2) the chat catches
that failure with `pcall` and sends it only to the log (`enu.log.error`, never to
the screen, §15) without mounting anything, and since the bare screen —the only
path that installs an emergency exit handler— isn't taken with plugins active,
the user is left **trapped** (in raw mode `ctrl+c` doesn't generate `SIGINT`). The
command that promised "batteries-included" leaves the product broken and
unusable on its very first run.

**Decision.** Two pieces, **neither in the sacred `enu.*` API** (this is CLI + loader
+ extension Lua surface; `enu.version.api` doesn't change):

1. **The onramp leaves USABLE agent config.** `nu --default-config` (persistent
   mode) writes, in addition to `enu.toml`, **active templates** for:
   - `agent.toml`: `model = "anthropic/opus"`, `max_turns = 32`.
   - `providers.toml`: provider `anthropic` (`base_url`, `api_key_env =
     "ANTHROPIC_API_KEY"`) with the model `claude-opus-4-8` (alias `opus`,
     `context`, `thinking = "adaptive"` per ADR-016).

   They're written **only if they don't already exist** (never overwriting the
   user's config; atomic, idempotent — reusing `writeAtomic` and the
   don't-overwrite-existing-TOML pattern from `writeEnabledPlugins`, G33/ADR-015).
   The **key never goes into the file** (providers.md §1): it lives in the
   environment. The success message becomes **honest**: it lists the files
   created and reminds the user to export `ANTHROPIC_API_KEY` (or edit
   `providers.toml`) before starting — no longer the misleading promise "you can
   now run the agent: enu -p".

   **Ephemeral** mode (`--default-config -p/-e`, immutable Docker) still doesn't
   touch disk: there, config comes from the environment or mounted files, and
   the degradation (piece 2) plus the `agent:error` render cover its absence.

2. **The chat degrades gracefully.** If `chat.start` can't build the initial
   session because of a **configuration** failure (`agent.session` throwing
   `EINVAL` for a missing model, `EPROVIDER` for an unresolvable model/provider,
   or `EAGENT`/`EPROVIDER` for broken TOML), the chat mounts a **minimal
   actionable UI** instead of dying to the log: text explaining how to configure
   (`agent.toml`, `providers.toml`, the API key) and a keymap to exit
   (`esc`/`q`/`ctrl+c` → `core:shutdown`). **Unexpected** errors (not
   configuration-related) propagate as today. As a kernel **safety net**,
   interactive mode additionally installs an emergency exit handler at the
   **bottom** of the input stack (any mounted app covers it), guaranteeing that
   no path leaves the terminal without a keyboard exit.

**Rationale.**
- **Why active templates and not commented-out ones.** With the key in the
  environment, `enu` *just works* after a single command — ADR-015's promise, now
  real. Without the key, `providers.resolve` **doesn't fail** (it leaves
  `api_key=nil`): the chat still mounts, the statusline shows the model, and the
  missing-key error surfaces **in-transcript** on the first turn (`agent:error`
  → `transcript:add_error`, which the chat already renders), much better than a
  dead screen. Commented-out templates would force editing TOML before the
  first run, the very friction the onramp removes.
- **Why an Anthropic default.** `enu` is a claude-code-style coding harness; the
  opinionated default is coherent with its identity and with the project's
  default model (`claude-opus-4-8`, ADR-016). The user changes it by editing
  two lines; the templates show the format.
- **Why not a default model hardwired into the agent.** Putting which model,
  which endpoint, and which env var into the engine is product vocabulary in
  the kernel/engine, against ADR-003/ADR-005. Config lives in TOML, declared;
  the engine only reads it.
- **Why degradation in addition to the onramp.** The onramp covers the happy
  path, but the chat mustn't die silently if the user deletes or breaks the
  config: robustness via `pcall` at boundaries and an always-available exit are
  principle 5 of the philosophy. The two pieces are complementary, not
  alternatives.
- **Why it doesn't touch the sacred API.** Same as ADR-015: the onramp belongs
  to the binary (`main.go`/loader) and the degradation is Lua in the `chat`
  extension. `enu.*` and `enu.version.api` stay intact.

**Consequences.**
- `nu --default-config` leaves the harness **actually** ready (with the key
  exported, one command suffices); without it, the first `enu` opens the chat with
  an actionable error instead of a dead screen.
- `chat.start` stops being a silent point of failure: any missing or broken
  config produces a screen that **explains and closes**.
- No interactive path can trap the terminal (kernel exit safety net).
- Observable change covered by tests: `WriteDefaultConfig` writes three files
  (not one), the flag's message changes, and `chat.start` no longer throws on
  missing config.
- **Reopening trigger:** if the onramp had to seed config for more than one
  provider, or secrets that don't fit in environment variables, reconsider a
  guided configuration flow (tied to ADR-015's `nu config` trigger).

## ADR-018 · Official extensions are a PRODUCT: the toolkit decorates and the harness UI looks finished

**Status:** Accepted · 2026-06 (**refines** [ADR-015](#adr-015--conjunto-oficial-de-producto-y-onramp-no-interactivo) —what "the official product set" means when there's a TTY— and consumes [ADR-012](#adr-012--el-toolkit-de-widgets-vive-en-lua-spike-de-s28); resolves [G36](problemas.md#g36) and [G37](problemas.md#g37))

**Context.** With the construction plan closed (45/45) the binary *worked*, but
*using* it the experience was "little more than a blank terminal": the chat
transcript was monochrome prose glued to the 0 margin, the input was a
frameless band, the statusline was a gray indistinguishable from the body,
there was no welcome or activity indicator, and —worse— the official set
mounted the chat and the REPL at the same time, so exiting the chat left the
interpreter underneath. The kernel and the contracts were there; what was
missing was for the official extensions to **look like a finished product**,
not a kernel with demo widgets. Two audits (chat and toolkit) agreed on the
root cause: the toolkit had no **decoration** primitives (border/box, padding,
spinner, multi-span text) and **didn't wire the theme into markdown**, so the
whole UI was condemned to stack plain text no matter how much it was dressed up.

**Decision.** Raise the official extensions to product quality, **entirely in
Lua on top of the already-frozen API** (completeness corollary: no need to
extend `enu.*`; `enu.version.api` doesn't move). Three fronts:

1. **The toolkit decorates.** Added to the catalog (open question #3 of
   arquitectura.md, which ADR-012 left for the toolkit to settle): `box` (rounded/
   straight border frame, title, padding, focus highlight), `spinner` (animated
   via `enu.task.every`), `richtext` (a line of several spans with alignment), and
   in containers `padding`/`gap`/`align`/`justify`. The `theme` moves from a
   placeholder palette to a **curated palette** (warm accent, roles, surfaces,
   selection, code/links/diff) and exposes `Theme:markdown_opts()`, which
   **wires the semantic names into `enu.text.markdown`'s render** (api.md §10) —
   the highest-impact change: the transcript stops being monochrome.

2. **The chat looks finished.** Welcome on startup (banner, model, cwd,
   shortcuts); **framed** input with prompt `› ` and a visible placeholder;
   **activity spinner** while the turn is running ("Thinking…/Running <tool>…
   · esc to interrupt"); statusline as a **bar** with background and colored
   segments (context warning, abbreviated cwd); **tool cards** with their
   arguments and status; **framed and centered** modals. No kernel privilege:
   everything consumes the toolkit and `agent:*` events like a third-party UI
   could (ADR-003).

3. **A single primary UI owns the screen.** The official set (ADR-015) still
   includes `repl`, but the repl **yields to the chat** (G36): it only
   auto-mounts its UI if the chat isn't active. And **closing the chat shuts
   down the binary** (`core:shutdown`), instead of dropping the user to a lower
   layer.

As a byproduct, building the first border widget uncovered
[G37](problemas.md#g37) (a latent bug in `blitBlock`'s X axis, never exercised
because until now nothing had been painted at x>0), fixed to comply with the
`Region:blit` contract in api.md §9.1.

**Rationale.**
- **Why in Lua and not in the core.** The audits confirmed the primitives
  needed already existed in the frozen API: Blocks with styled spans (§9.2),
  **themable** `enu.text.markdown` (§10), `enu.text.highlight`/`diff`,
  `enu.task.every` to animate, `enu.plugin.list` for the repl to detect the chat.
  The completeness corollary holds: the API was exactly enough; what was
  missing was *using* it from the toolkit. The only exception was G37, an
  *implementation* bug in the compositor (not a spec issue): the code is fixed
  to comply with the contract, not the other way around.
- **Why the repl yields instead of leaving the set.** The repl is valuable as
  a starting point for extension authors (G21); what was excessive wasn't its
  presence but its competing for the screen. Yielding the screen preserves
  ADR-015 (it's still installed and separately activatable) and closes the
  overlap. Detail and alternatives in G36.
- **Why an opinionated default palette.** A generic theme (black/gray) reads
  as a placeholder. The harness's visual identity (warm accent, surface
  hierarchy) is part of "looking like a product." It remains **only the
  default**: an alternative theme is a toolkit plugin, and the user derives it
  with `:with{…}` (chat.md §7, G22).

**Consequences.**
- The product's first screen stops being a blank terminal: a colored welcome,
  framed input, status bar, and a single app that shuts down the binary when
  closed.
- The toolkit's catalog grows (box/spinner/richtext + padding/align), but it's
  **toolkit surface**, versioned separately, not `enu.*` (the sacred API doesn't
  move).
- Observable changes covered by tests: the transcript emits color (themable
  markdown), the statusline is a bar of spans, the editor sits in a box, the
  repl doesn't mount its UI with the chat active, and `blitBlock` positions
  correctly at x>0.
- **Reopening trigger:** if a third-party UI needed a decoration primitive the
  toolkit doesn't offer (tables, visible scrollbar, mouse/hit-testing), it gets
  added to the toolkit's catalog (not the core) following this same pattern;
  several candidates are noted in the audits as P2.

## ADR-019 · The kernel's target VM is PUC-Lua over wazero; gopher-lua goes into maintenance

**Status:** Accepted · 2026-07 (direction; execution is phased, with no
committed date. Based on the spike [spike/lua-wasm/INFORME.md](../spike/lua-wasm/INFORME.md); related to ADR-011 —which the migration phase will replace— and to [G31](problemas.md#g31)/[G41](problemas.md#g41))

**Context.** gopher-lua —the VM that all of nu's Lua runs on— has no effective
maintenance: the pinned v1.1.2 is its last release, `state.go` hasn't been
touched since December 2023, and the G41 bug has been reported upstream since
2023 with no response (issue #448). The kernel already carries two structural
scars from it: ADR-011's yield-less scheduler (because its coroutines don't
yield through `pcall`, G31) and G41's shielding (which writes to an unexported
field of the dependency via `unsafe`). The risk isn't acute —the status quo
works, shielded and tested— but it's compounding: every construction session
adds code on top of an orphaned base, and the cost of leaving grows over time.
A spike on a separate branch (`spike/lua-wasm`) measured the alternative:
**PUC's official Lua (5.4.7, without a single patch)** compiled to WebAssembly
and run on **wazero** (a WASM runtime in pure Go, maintained and with
industrial backing; `CGO_ENABLED=0` intact), with Lua's unwinding realized via
a trampoline over wazero's `Snapshot/Restore` API (no setjmp/longjmp, no
emscripten, no asyncify). Results: **reference semantics** (the G41 repro
returns `42`; `coroutine.yield` crosses `pcall` — the limitation that
motivated ADR-011 doesn't exist), a **pure VM equal to or faster than**
gopher-lua (fib 0.8×, tables 0.41×), costs concentrated at the boundaries (host
call ~1 µs, throw ~40 µs, yield 26-192 µs — see §4 of the report), and ~+0.7 MB
of binary size.

**Decision.** Five pieces:

1. **Committed direction:** the kernel's target VM is PUC-Lua over wazero.
   gopher-lua moves into **maintenance mode**: pinned at v1.1.2, existing
   shields are kept, and NOTHING new is built that depends on its internals
   beyond what's already there.
2. **Language baseline: Lua 5.4.** During the migration, api.md §1.2 will move
   from "Lua 5.1 (gopher-lua)" to "Lua 5.4 (PUC)" — minor changes
   (`unpack`→`table.unpack` and similar) in areas the sandbox already smoothed
   over. Decided now so that no new code relies on 5.1 particulars that won't
   carry over.
3. **The ⏸ bridge will be via native coroutines** (`lua_yieldk`; the yield
   crosses `pcall` in real Lua): the design ADR-011 wanted and G31 vetoed. When
   the migration is executed, its design ADR will **replace** ADR-011 (which
   isn't rewritten, per the project's workflow).
4. **Phased execution, no date:** (a) the **VM interface** in the kernel as its
   own session — extracting behind a Go interface the operations the kernel
   uses from the VM; cheap, no commitment, improves the kernel even if the
   migration never landed; (b) the **design ADR for the definitive bridge**,
   which must resolve the amber light on the yield's cost (`Snapshot` clones
   the engine's stack; paths in the report's §4.1) BEFORE migrating; (c) the
   **phased migration** against the existing conformance suite (G31/G41,
   watchdog, cancellation, workers), estimated at 10-15 sessions. Acceleration
   trigger: if gopher-lua bites again with something unshieldable from the
   kernel, the migration becomes the next construction phase.
5. **Spike anti-staleness:** `spike/lua-wasm` (build.sh + tests + benchmarks)
   is kept reproducible; a wazero upgrade that changes the experimental
   snapshot API must be caught there before it reaches the kernel.

**Consequences.**

- The whole class of "the reimplementation diverges from the reference" bugs
  (G31, G41) becomes impossible by construction at the destination; meanwhile,
  the shielded status quo remains operable.
- WASM's per-instance linear memory opens up **physical isolation** for
  workers and `caps` — [P2](pospuesto.md) (isolated actors) and [P3](pospuesto.md)
  (WASM plugins) gain a natural and cheap path on migration day.
- Costs accepted: ~+0.7 MB of binary size; more expensive boundaries
  (irrelevant with the "Lua decides, Go executes" pattern, which already
  minimizes crossings); an experimental API watched by the spike; and the
  minor, announced breakage of the 5.1→5.4 baseline in api.md §1.2 when it
  lands.
- Plugin authors **notice nothing**: same Lua, same `enu.*` API, same
  contracts — the sacred API is exactly the layer that makes it possible to
  change the engine without touching the ecosystem (ADR-003, structural
  dogfooding).

## ADR-020 · The definitive ⏸ bridge: tasks as native Lua coroutines (replaces ADR-011 at the switchover)

**Status:** Accepted · 2026-07 (designs the ⏸ bridge for [ADR-019]'s wasm
backend; **replaces [ADR-011](#adr-011--realización-del-scheduler-goroutine-por-task--token-de-ejecución-lua)** once wasm becomes the default VM —switchover
M16 of [migracion-vm.md](archive/migracion-vm.md)—; until then both coexist
behind the backend selector, ADR-011 for gopher and this one for wasm).
Doesn't change the observable semantics of [api.md](api.md) §1.3 nor any
signature.

**Context.** ADR-011 realized the *yield-less* scheduler —goroutine per task +
an execution token— because gopher-lua (a reimplementation of Lua 5.1)
**doesn't let a coroutine yield through a `pcall`** (crack
[G31](problemas.md#g31)). It was a forced workaround: the "coroutine bridge"
that ADR-004 anticipated as the natural model couldn't be built. ADR-019's
spike proved (test 🔒 `TestYieldATravesDePcall`, M02) that **PUC's official
Lua over wazero DOES yield through `pcall`**: crack G31 doesn't exist in the
reference implementation. Therefore the wasm backend can —and must— realize
the bridge as ADR-004 wanted.

**Decision.** The wasm backend's ⏸ bridge is **native Lua coroutines**, with a
scheduler loop in Go driving them:

1. **A task is a Lua coroutine** (`lua_newthread` + `lua_resume`, exposed by
   the shim: `nu_co_spawn`/`nu_co_resume`, M02). The main state remains
   single-threaded (ADR-004): coroutines are scheduled **cooperatively**, one
   running at a time, sharing the instance's memory. No token/GIL: the
   coroutine truly yields.
2. **⏸ = `lua_yield` with a request.** A suspending primitive (or
   `enu.task.sleep`) yields with a *work descriptor* (M05 wire: work type +
   args). The Go loop reads what was yielded, launches the blocking work in a
   **background goroutine** (which never touches the VM), and when it finishes
   it **resumes** the coroutine with the result (`nu_co_resume`). The Lua code
   is written sequentially (implicit await), as §1.3 mandates.
3. **The scheduler loop** maintains a set of ready and suspended tasks; resumes
   the ready ones until they yield or finish; delivers background-goroutine
   results over a channel. It's ADR-004's event loop, now without the token
   dance.
4. **The complete `enu.task`** (spawn/sleep/await/all[G27]/race/every/defer/
   future/cleanup) is implemented on top of this loop. Observable semantics
   are identical to gopher's (the `scheduler_test`/`allrace`/`future`/`timers`
   tests are the parity contract).

**The cost of yielding (the amber light from the report's §4.1).** Every
`lua_resume` goes through `luaD_rawrunprotected` → `LUAI_TRY` → the trampoline,
which takes a wazero `Snapshot` (cloning the engine's stack). Measured in the
spike: 26 µs cold, degrading toward tens of µs under GC pressure. For the real
case —a yield per IO operation that takes milliseconds— this is noise.
**M15's veto 2** (≤ 50 µs sustained) audits it with real numbers on the actual
scheduler. **Reserved mitigation lever** if it's exceeded: lighten the
trampoline's no-error path (the `Snapshot` is only needed if a `LUAI_THROW` is
going to occur; a resume that doesn't throw wastes it). Not optimized
preemptively: measure first.

**Consequences.**

- **The kernel gets simpler** (C4 census): no token, no goroutine-per-task, no
  `suspend` that releases/re-acquires the token. Cancellation and the watchdog
  (M07) are realized over native Lua abort + wazero epoch interruption,
  without `installCancelPcall` (ADR-011's `pcall`/`xpcall` wrapper dies at M17).
- **ADR-011 gets replaced** at the switchover (M16); its gopher code is
  retired along with gopher-lua (M17). As the project's workflow mandates,
  ADR-011 isn't rewritten: it gets marked "Replaced by ADR-020" once the
  switchover closes.
- **Temporary coexistence**: while the selector offers both backends
  (M04-M16), ADR-011 governs the gopher path and this one the wasm path; both
  pass the same conformance suite (plan §3).
- Risk being watched: the cost of yielding (above) and the trampoline's
  dependency on wazero's experimental snapshot API (gate-test 🔒 in M03).

## ADR-021 · Full, reproducible lint baseline before freezing v1

**Status:** Accepted · 2026-07 (**refines** [ADR-013](#adr-013--integración-continua-y-publicación-de-releases), point 5; doesn't change the rest of its CI policy nor [api.md](api.md)'s surface)

**Context.** ADR-013 introduced a small set of linters with
`only-new-issues: true` so that debt already in the code wouldn't block
construction. It was a migration concession, not the desired state for
freezing v1. The cleanup after M17 fixed the 26 findings left by gopher-lua's
removal, and `golangci-lint` v2.12.2's full analysis now sits at **zero
findings**. Keeping the changed-lines filter no longer protects a transition:
it lets a diagnostic introduced by indirect changes or by a tool upgrade stay
invisible as long as it doesn't match the diff.

**Decision.** The lint baseline covers **the entire repository** on every Pull
Request and push to `main`; `only-new-issues` is turned off. The workflow pins
the `golangci-lint` binary at v2.12.2, the version with which the clean
baseline was established. Upgrading it is a deliberate change: the new version
must run against the full tree and land only once it again produces zero
findings. "Clean baseline" refers to ADR-013's explicit set (`govet`,
`staticcheck`, `errcheck`, `ineffassign`, `unused`); expanding that set still
requires independent justification. No exclusions or `nolint` directives are
added to reach zero.

**Consequences.**

- Any finding from the five linters blocks CI even if it's outside the lines
  touched by the PR; new debt can't accumulate silently.
- Pinning the binary keeps a linter release from shifting the gate without
  review. In exchange, upgrades become explicit maintenance and must again
  demonstrate the full baseline.
- The policy hardens the quality gate ahead of v1, but doesn't modify any
  signature, semantics, or version of the sacred API.
