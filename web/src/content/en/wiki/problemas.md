# Open problems

Living work list: cracks found in the validation rounds
([pseudocodigo.md](pseudocodigo.md)) and later reviews that are
**pending resolution**.
Method: they are resolved one by one, discussing options; once decided, the
resolution is applied to the affected documents and the entry moves to
"Resolved" with a link to the change. Different from [pospuesto.md](pospuesto.md):
that is what we decided not to decide yet; this is holes that v1 does
need closed.

**Status: 48 recorded, 48 resolved, 0 open** (G52 added
2026-07-14 from A-38 of the comprehensive audit — `Ws:send` with no binary path and
`Ws:recv` without distinguishing the frame type — resolved by addition to `api.md`
§8, API level 2→3; G44–G51
added 2026-07-12 from the comprehensive audit
([auditoria-2026-07-12.md](audits/auditoria-2026-07-12.md)): G47–G51 —documentation
inconsistencies— resolved the same day; G44 —the scheduler's pumping— resolved
and **built** on 2026-07-13 with option (b), persistent `RunTasks`
(logbook of [implementacion.md](implementacion.md)); G45 —the [W]
surface of workers— resolved and **built** on 2026-07-13 with option (a),
worker-safe mark via preamble snippet; G46 —the `event` replay—
resolved and **built** on 2026-07-13 with option (a) plus (c):
precedence `opts > transcript > agent.toml` and allow/deny reapplied in
order. Numbers G42–G43 are **reserved**: they are
used by branch `claude/ux-producto-pulido` (retry with backoff and structured
`agent:error`), still unmerged. G41 added 2026-07-03 from
construction — a handler that wrote to an upvalue of a suspended task
"lost" the write: a gopher-lua bug in `pcall` unwinding,
shielded in the kernel the same day; G38-G40
added 2026-07-02 from round 8 of pseudocode — a distributed mesh of
agents over git, with Role+Job specs and fork-as-replication — and resolved the
same day: G38, the slug of
`sessions/<project>/` left unspecified — the
algorithm becomes part of the format and the extension exposes
`sessions.slug/dir`; G39, `Session:fork` with no `opts` and with
`at` without a unit — `fork(at?, opts?)` and `close()`
enter the contract, inheritance gets specified and copying
the prefix is blessed (self-contained child); G40, permission denials not
observable as data — event `agent:permission.denied` + the same object
in the `tool_result`'s `meta`, and `tool.end` specified for denials;
G36 and G37 added 2026-06-28 while polishing the
official extensions' UI/UX to make them feel like a product: G36, the double
auto-mount of chat+repl; G37, a latent bug in `blitBlock`'s X axis; G35 added
2026-06-27 while using the binary after the ADR-015 onramp; G34 added 2026-06-27 while
validating with pseudocode the
reasoning control; G33 added 2026-06-23 while testing the
binary with the official extensions; G32 added 2026-06-22 from the
construction of the sessions extension). The sixteen from
rounds 3-4, the six from the review of coherence across the full
documentation (G17-G22, mostly contracts that presupposed nonexistent API) and
those from the philosophical-technical review of the project (G23, product
vocabulary in the sacred API; G26, extension namespaces reserved to the
core) are closed. Numbering jumps from G23 to G26 because G24-G25 are
cracks from the same review in progress, recorded on their own
branches; G27 comes out of round 5 of pseudocode (agent orchestration by a
third party). G28-G30 come out of round 6 (rebuilding a claude-code-style
coding harness on top of `enu.ui`): G28 (blit clips at both ends,
scrollback), G29 (mouse hit-testing belongs to the toolkit, same split as
G1/G22) and G30 (pasting images injects a path). G31 is the first crack
that comes out of **construction** and not out of a pseudocode round: gopher-lua
does not let a coroutine yield through `pcall`/tail call, which forced
building the scheduler without yields (ADR-011). G32 is the second one out of
construction (the sessions extension, S38): the §6 lock needs the *own*
process's pid and the API did not expose it — the loose end from G17. G33 is the
third from construction and the first from *using* the finished binary: the
no-TTY startup had no onramp (G21's bare screen is TTY-only) and
"the official set" was undefined against `example` — resolved with the
`nu --default-config` flag and ADR-015 (without touching the sacred API: it's
CLI surface). G35 is the **second** from *using* the finished binary: that same onramp
activates the seven plugins but **leaves no agent config** (model/provider), so
the first `enu` dies without a model and leaves the UI stuck — resolved with ADR-017
(active templates in the onramp + graceful degradation of chat). The list stands
as a record of the process; new problems that arise (spike included) are
added here using the same method.

---

## G1 · Behavior on resize — `api.md` §9 — **RESOLVED**

**Resolution** (applied in [api.md](api.md) §9.1 and
[guia-plugins.md](guia-plugins.md) §6): hard rule in the core — off-screen
regions are clipped without error and keep their
coordinates; repositioning belongs to the owner (the "your region, your
`ui:resize`" convention); automatic relayout belongs to the toolkit. Declarative
anchors in `region{}` discarded: it would freeze a mini layout language into
the sacred API — the house pattern is "the core gives guarantees,
not conveniences."

**Problem.** A region that ends up (fully or partially) off screen after a
resize has undefined behavior, and there is no convention on who repositions
what: scenario 12's picker ends up broken or floating.

**Impact.** Every plugin with its own UI; the toolkit needs it resolved
before the spike.

**Options.** (a) Hard rules only: regions clip to screen
without error, and the convention is "your region, your `ui:resize`"; (b) also,
declarative anchors in `region{}` (`x = "center"`, `w = "80%"`) that the
compositor reapplies only on each resize; (c) delegate it all to the toolkit and
have raw `enu.ui` explicitly "on your own."

## G2 · Plugin hot-reload (development cycle) — loader / `api.md` §14 — **RESOLVED**

**Resolution** (applied in [api.md](api.md) §14 and §4):
`enu.plugin.reload(name)` best-effort — handles tagged by owner,
`core:plugin.unload` event so extensions can clean up their
registrations, require cache cleared, init.lua reloaded. A development tool, not
a production guarantee. Restart-with-`--continue` was
discarded as DX history (loses UI/plugin state); postponing hurt
right where the first authors are won.

**Problem.** Iterating on a plugin requires restarting nu: `require` caches,
re-running `init.lua` would duplicate registrations, and although every
registration returns a handle, nobody tracks them by plugin (there is no "undo
everything from X"). The same applies to hot-reloading `providers.toml` /
`enu.toml`.

**Impact.** DX of the plugin community — the project's target audience.
Does not block contracts.

**Options.** (a) The core tracks handle ownership per plugin (it already
knows `plugin.current()` at each registration) and offers `enu.plugin.reload(name)`;
(b) no reload: a quick-restart command for nu that restores the session
(`--continue` already almost gives this); (c) postpone with a trigger (new P).

## G3 · Multi-session: event attribution and concurrent modals — `agente.md` §4 / `chat.md` — **RESOLVED**

**Resolution** (applied in [agente.md](agente.md) §4-§5 and
[chat.md](chat.md) §1/§2/§5): `session` mandatory on every `agent:*`
payload (emitted via a single helper); `chat` renders only the active session and
flags the rest in the statusline; modals in a FIFO queue tagged by
session, **no timeout** on asks (a timeout→deny would be non-
deterministic) — the UI makes pending ones visible. Discarded
per-session namespacing in the event name (the bus has no wildcards and a
field solves it for free).

**Problem.** `agent:*` payloads don't require carrying `session_id`
(two concurrent sessions would mix deltas), `chat.md` doesn't specify
filtering, and two simultaneous `permission.asked` would open two modals over
the same input stack with no defined order.

**Impact.** Subagents already make this real in v1 — not a future
case. A freezable contract is affected.

**Options.** (a) `session_id` mandatory on every `agent:*` payload +
`chat` filters by active session + FIFO queue of modals (one visible at a
time); (b) additionally, per-session event namespacing
(`agent:<id>:delta`) for cheap selective subscriptions.

## G4 · Reentrancy of `Session:send` — `agente.md` §2 — **RESOLVED**

**Resolution** (applied in [agente.md](agente.md) §2): `send` with a turn in
flight enqueues; the loop injects what's queued when assembling the next request
(never mid-stream). `cancel()` does not empty the queue
(separate `clear_queue()`). `EBUSY` discarded (each UI would reimplement the
queue in a subtly different way — exactly what we wanted to avoid).

**Problem.** Calling `send` with a turn in flight is undefined:
error, queue, or cancel-and-replace? Each UI would improvise a different
semantics.

**Impact.** Freezable contract; affects basic UX (impatient enter).

**Options.** (a) `EBUSY` and let the UI decide (minimal, predictable); (b) the
engine queues messages and appends them to the next turn (what mature
harnesses do); (c) configurable per session.

## G5 · Double resumption of the same session — `sesiones.md` — **RESOLVED**

**Resolution** (applied in [sesiones.md](sesiones.md) §6): one writer per
session via a `<session>.jsonl.lock` lockfile with `{pid, hostname, started}`;
readers without a lock; orphaned locks (dead local pid) are cleaned up
silently; a real conflict → a warning with fork by default / read-only / force
with confirmation. `flock` discarded (unpredictable semantics
on Windows/network); silent auto-fork discarded (forks without the user's
knowledge).

**Problem.** Two nu processes can open the same JSONL and produce interleaved
appends: silent corruption. There is no lock.

**Impact.** Loss of user data; cheap to close now, costly
later.

**Options.** (a) Lockfile next to the JSONL (`.lock` with pid; the second
process gets a clear error and is offered a fork); (b) OS advisory lock
(flock) — portability on Windows?; (c) detect-and-fork automatically: the
second `--continue` silently creates a fork.

## G6 · Granularity of `caps` — `api.md` §13 — **RESOLVED**

**Resolution** (applied in [api.md](api.md) §13, [agente.md](agente.md)
§9, guide §3; new ADR-010): per-function mechanism in the core (two
granularities: `"fs"` module, `"fs.read"` function; deny-by-default for
future functions), vocabulary as inspectable tables of the
agent extension (`agent.caps.FS_RO`). Curated packages in the core were
discarded (they hide judgment calls and retroactively redistribute power
as the API grows); path scoping goes to [P17](pospuesto.md). Derived:
ADR-010 — official extensions ship embedded but
**inactive by default**, explicit one-key activation.

**Problem.** `caps` grants whole modules: `"fs"` includes `write` and
`remove`. The read-only auditor subagent — sandboxing's flagship
case — cannot be expressed.

**Impact.** One of the differentiating features (hard permissions) falls
short in its best use case.

**Options.** (a) Caps with a mode suffix: `"fs:ro"` (a short,
curated list of variants per module, without inventing a policy language);
(b) per-function caps (`"fs.read"`, `"fs.stat"`): expressive but
N×functions worth of surface to freeze; (c) path scoping in addition to
the mode (`fs:ro:/repo`): the most powerful and the most expensive to
specify well; (d) leave whole-module in v1 and note it in postponed.

## G7 · Semantics of `fs.watch` — `api.md` §5 — **RESOLVED**

**Resolution** (applied in [api.md](api.md) §5): `watch(path, opts?, fn)`
with `recursive`, `gitignore = true` by default and delivery in batches
with debounce (`fn(events[])`, ~50 ms). The minimal version was
discarded: it would have forced every consumer to reimplement
recursion+ignores+debounce in Lua — work proportional to the repo on the
main state, against "Lua decides, Go executes."

**Problem.** Undefined: recursive? does it respect `.gitignore`?
(watching `node_modules/` = infinite noise), coalescing of bursts?
(a `git checkout` touches thousands of files → thousands of callbacks).

**Impact.** Any auto-context or reload plugin; performance risk
on the main state.

**Options.** (a) `watch(path, opts, fn)` with `opts = { recursive,
gitignore = true, debounce_ms = 50 }` and event delivery in batches
(`fn(events[])`); (b) minimal v1: one path, no recursion (plugins
compose it), and everything else to postponed.

## G8 · Simultaneous `on_message` vs `recv` — `api.md` §13 — **RESOLVED**

**Resolution** (applied in [api.md](api.md) §13): mutually exclusive,
`EINVAL` on the spot when registering one with the other pending. Silent
priority discarded (hides the bug); competing via a queue discarded
(non-determinism of order).

**Problem.** They are "alternatives" but nothing prevents using both on the
same worker: who receives the message? Undefined.

**Impact.** Minor, but it's exactly the kind of underspecification that
generates irreproducible bugs.

**Options.** (a) Mutually exclusive: registering `on_message` with a
pending `recv` (or vice versa) throws `EINVAL`; (b) `on_message` always
wins and `recv` after it throws; (c) single queue and any consumer
competes for it (non-deterministic — probably discardable).

## G9 · Windows scope in v1 — cross-cutting — **RESOLVED**

**Resolution**: v1 supports native Linux and macOS; on Windows, **nu is used
inside WSL2** (documented as a requirement, not an apology). Decisive
advantage: inside WSL2 the POSIX contract holds in full — zero
conditional specification, zero portable shell, zero dual signal
semantics. Native Windows stays in postponed ([P18](pospuesto.md)) with its
trigger. The "cross-compile to every platform" promise is qualified in
the architecture: the binary *compiles* for Windows, v1 support is WSL2.

**Problem.** The `bash` tool assumes `sh`, `Proc:kill` speaks POSIX
signals, and terminal input differs (IME, keys). Go cross-compiles to Windows,
but "compiles" isn't "works well." Without a scope decision, every
contract silently assumes POSIX.

**Impact.** More a product decision than a technical one; conditions
distribution promises ("one binary for every platform").

**Options.** (a) v1 = first-class Linux/macOS + best-effort documented
Windows (the bash tool requires WSL or git-bash); (b) first-class Windows
from v1 (high cost: portable shell, kill semantics, terminal
tests); (c) v1 without Windows, explicitly.

## G10 · Reentrancy of the event bus — `api.md` §4 — **RESOLVED**

**Resolution** (applied in [api.md](api.md) §4): dispatch over a snapshot
of subscribers; cancellation with immediate effect; those who subscribe during
dispatch only see future events; nested emits queued
(breadth, not depth — the infinite ping-pong becomes a flat loop that the
watchdog cuts off). Depth recursion discarded (stack overflow + surprising
order); mandatory `defer` discarded (the UI would lag one tick behind).

**Problem.** `emit` inside a handler (recursion or queue?), subscribing
or cancelling during dispatch (does the new handler see the in-flight event?
does one cancelled mid-way still run?): all undefined. Produces bugs
dependent on plugin load order.

**Impact.** Core of the extension model; cheap to define now, impossible
to change later.

**Options.** (a) Dispatch over a snapshot of the handler list + nested
emits queued at the end of the current dispatch (no recursion); (b)
depth-recursive dispatch with an anti-cycle limit; (c) nested emits via
mandatory `task.defer` (simpler in the core, more surprising for the
author).

## G11 · Non-UTF-8 data at JSON boundaries — `api.md` §12 / cross-cutting — **RESOLVED**

**Resolution** (applied in [api.md](api.md) §12 and guide §5): the codec is
strict (`encode` throws `EINVAL` on invalid UTF-8) and tools sanitize
at the source, visibly (`[binary output: NKB omitted]`). Automatic base64
discarded (unexpected blob for the LLM, ambiguity for the
reader); silent `U+FFFD` in the codec discarded (hides corruption at
every boundary — sanitizing is a decision made with context).

**Problem.** A tool result with binary bytes (a cat of a PNG) crosses
three boundaries that assume JSON/UTF-8 (request to the provider,
transcript JSONL, worker messages) with no defined rule: throw, replace,
base64? The bug would surface far from its origin.

**Impact.** Basic robustness of the `bash` tool — will hit on day
one.

**Options.** (a) `enu.json.encode` throws `EINVAL` on invalid UTF-8 and
tools sanitize (lossy replacement + note "binary output truncated") —
a rule in the guide and in the official tool; (b) automatic base64 with a
marker; (c) silent replacement with U+FFFD in the codec (convenient, but hides
corruption).

## G12 · TLS/proxy for corporate endpoints — `api.md` §8 — **RESOLVED**

**Resolution** (applied in [api.md](api.md) §8): `opts.tls = { ca_file?,
insecure? }` in `request`/`stream`; the `HTTP_PROXY`/`HTTPS_PROXY`/`NO_PROXY`
environment variables are respected by default (the de facto corporate
standard); global defaults in `[net]` of `enu.toml` overridable per
request.

**Problem.** The "corporate proxy" is a case called out in the philosophy,
but `enu.http` has no TLS options (own CA, insecure) nor a proxy policy (is
`HTTPS_PROXY` respected?). The case cannot be configured.

**Impact.** Enterprise adoption — the natural audience for a dependency-free
binary.

**Options.** (a) `opts.tls = { ca_file?, insecure? }` + respect
`HTTP(S)_PROXY`/`NO_PROXY` by default (documented); (b) additionally,
global configuration in `enu.toml` to avoid repeating it per request.

## G13 · Subscription-based providers (OAuth) — `providers.md` / `api.md` — **RESOLVED**

**Resolution** (applied in [providers.md](providers.md) §4 and guide §7):
v1 path without a listener — device flow or manual code pasting (the
`gh`/`gcloud` pattern), writable with `http.request` + `enu.proc`; tokens in
`data_dir()/plugins/<name>/` with `0600`, in the clear (consistent with P7). The
localhost listener (`listen_once`) goes to [P19](pospuesto.md) with the
trigger "a real provider with neither device flow nor code paste."

**Problem.** The device flow is writable with what exists (polling +
opening a URL), but the flow with a localhost callback is not: there is no
HTTP listener primitive. And there is no convention for where/how an
adapter stores its refresh tokens.

**Impact.** Subscription plans (not API key) are increasingly
common; decides whether nu supports them first-class.

**Options.** (a) Bless the device flow as the v1 path + a token storage
convention (`plugins/<name>/`, `0600`) and no
listener; (b) add a minimal HTTP listener (`enu.http.listen_once` for
OAuth callbacks, ephemeral, loopback-only) — small and bounded
surface; (c) postpone OAuth entirely with a trigger.

## G14 · Trust model for repo content — `agente.md` §6-§7 / cross-cutting — **RESOLVED**

**Resolution** (applied in [agente.md](agente.md) §11): the repo is not the
user. (1) Repo config **only trims** permissions: its `deny` is honored, its
`allow`/`mode` are ignored. (2) **One-key TOFU** per repo
for skills and `enu.md` (Neovim's `:trust` pattern); without explicit yes
(headless included), nothing gets injected. MCP tool descriptions remain
the user's responsibility (installing a server is a conscious act).

**Problem.** Opening nu in a cloned repo already runs the repo's will:
its `.nu/skills/` get injected into the system prompt and its
`.nu/agent.toml` can widen permissions (`allow = ["bash:*"]`) via the
project > global precedence. Third-party MCP servers' tool descriptions are
the same hole (untrusted text to the model). There is no
trust-on-first-use nor a distinction between innocuous and dangerous
config.

**Impact.** **The most serious security problem on the list**: turns
"clone and open" into an attack vector. Must be resolved before
freezing the agent contract.

**Options.** (a) Trust-on-first-use per directory (first startup in
a repo: "do you trust this?" dialog; without trust: repo skills and config
are ignored); (b) granular TOFU: repo config splits into innocuous
(always) and sensitive (permissions: NEVER widenable from the repo, only
trimmable — project `allow`s require explicit confirmation);
(c) both: TOFU for skills/context + a hard rule "the repo only trims
permissions, never widens them."

## G15 · The inside of a worker: its own scheduler and watchdog — `api.md` §13 / `modelo-ejecucion.md` — **RESOLVED**

**Resolution** (applied in [api.md](api.md) §13): each worker is a
complete mini-runtime (own scheduler, multi-task, timers, futures) and
**no watchdog** — workers exist to burn CPU freely; the control
is `terminate()` + `caps`. Configurable watchdog discarded: a knob
with no threat model (there's no UI inside to protect).

**Problem.** `task` is [W] and scenario 4 already assumed multiplexing with
`race` inside the worker, but it was never written whether each worker has its
own event loop, whether it supports multiple tasks and timers, or whether the
watchdog applies inside (with what budget, if there's no UI to protect?).

**Impact.** Contract clarification; scenario 4 depends on it.

**Options.** (a) Each worker = complete mini-runtime (own loop,
multi-task, timers) without watchdog (there's no UI to protect;
`terminate()` is the control); (b) same but with a configurable watchdog
(protects against zombie workers burning CPU).

## G16 · Parallel subagents writing the same files — `agente.md` §9 — **RESOLVED**

**Resolution** (applied in [agente.md](agente.md) §9): known limitation
documented + prescribed remedy (split territory via prompt, like reference
harnesses do). Lock in official tools discarded: false
security — bash and third-party tools write without going through it, it would promise
an unfulfillable guarantee ("almost right is worse than not"). After-the-fact
detection discarded for the same coverage gap.

**Problem.** Parallel subagents' tool calls interleave into the
main one, but nothing coordinates two writes to the same path:
silent last-write-wins.

**Impact.** Quality of results with parallel subagents; reference
harnesses don't solve it either (they mitigate by splitting
territory via prompt).

**Options.** (a) Document as a known limitation + guide ("split
territory among subagents"); (b) advisory per-file lock within the
session (official write tools respect it, warning on collision);
(c) after-the-fact detection (warning if two subagents touched the same
path).

## G17 · The sessions lockfile is not implementable with the current API — `api.md` §5-§7 / `sesiones.md` §6 — **RESOLVED**

**Resolution** (applied in [api.md](api.md) §1.4/§5/§6/§7 and
[sesiones.md](sesiones.md) §6): three minimal generic primitives —
`opts.exclusive = true` in `enu.fs.write` (atomic
only-if-not-existing creation via `O_EXCL`, no temp+rename, throws the
new reserved code `EEXIST`), `enu.proc.alive(pid)` (existence, not identity: a
recycled pid returns `true`) and `enu.sys.hostname()`. The lockfile remains
agent extension logic, in Lua. A dedicated `enu.fs.lockfile` was
discarded (it would put session policy — pids, orphans, hostnames — into
the kernel: the core gives guarantees, not conveniences); best-effort
was discarded ("almost right is worse than not").

**Problem.** G5's resolution requires three pieces [api.md](api.md)
doesn't have: (1) **exclusive** file creation — `enu.fs.write` is atomic
via temp+rename, but rename *overwrites*: two processes can
"win" the lock at once; (2) checking whether a foreign `pid` is alive
(`enu.proc` only manages its own children) — needed to clean up orphaned
locks; (3) `hostname` (not in `enu.sys`) — needed for the
lock's content.

**Impact.** G5 was resolved in prose but cannot be written with the
specified API; the session corruption it closed is still
possible. The same kind of crack the pseudocode rounds hunted for —
this one slipped through because G5 was resolved without writing the code.

**Options.** (a) Three minimal primitives: `opts.exclusive = true` in
`enu.fs.write` (throws if the file exists), `enu.proc.alive(pid) ->
boolean`, `enu.sys.hostname() -> string`; (b) one dedicated primitive
`enu.fs.lockfile(path, meta) -> Lock` packaging the full semantics
of sesiones.md §6 (less general surface, more opinionated); (c) downgrade
G5 to best-effort (assume the race is unlikely) — probably
discardable: "almost right is worse than not."

## G18 · Resuming a session has no API — `agente.md` §2 — **RESOLVED**

**Resolution** (applied in [agente.md](agente.md) §2 and
[chat.md](chat.md) §4/§8): `agent.session{ resume = id }` — one single
function, two modes. Reopens with a transcript replay (sesiones.md §3) and
writer lock acquisition (§6); the rest of `opts` is ephemeral state, not
persisted. A separate `agent.resume()` was discarded (duplicate
signature with no gain); resume-as-fork was discarded (forks the
history on every resumption). The CLI sugar (`nu --continue`) stays
deliberately outside the contracts: it belongs to the CLI surface
(open question 5 of [arquitectura.md](arquitectura.md)).

**Problem.** `agent.session(opts)` only creates new sessions (its `opts`
don't accept an id). But [chat.md](chat.md) §8 (`nu --continue`, the `/sessions`
picker) presupposes resumption, and [sesiones.md](sesiones.md) §7
describes the listing that feeds it. The entry point is missing.

**Impact.** Freezable contract; the feature is promised in two
documents.

**Options.** (a) `agent.resume(id) -> Session` (replay from sesiones.md §3
+ lock from §6); (b) `agent.session{ resume = id, ... }` (one single
function, two modes); (c) resuming = fork from the last point (unifies mechanics
with §5 but forks the history on every resumption — probably
discardable).

## G19 · Mid-session model switch has no API — `agente.md` §2 / `chat.md` §4 — **RESOLVED**

**Resolution** (applied in [agente.md](agente.md) §2 and
[chat.md](chat.md) §4): `Session:set_model("provider/model")` — validates
against the provider registry, writes the `event` entry to the
transcript (sesiones.md §3) and applies from the next request; with a
turn in flight, when assembling the next iteration (like G4's queue),
never mid-stream. Mutable `Session.model` discarded (no clear
validation point nor transcript record); fork-per-model
discarded (fragments sessions for an everyday operation).

**Problem.** `/model` exists in `chat` (a picker from `providers.list()`)
and [sesiones.md](sesiones.md) §3 gives "mid-session model change"
as the canonical example of an `event` entry, but `Session` exposes no
way to change it: `opts.model` only exists at creation.

**Impact.** Basic UX feature, presupposed by two contracts.

**Options.** (a) `Session:set_model("provider/model")`: validates against
the registry, writes the `event` entry and applies from the next
request; (b) mutable `Session.model` (less explicit, no clear
validation point); (c) no hot change: `/model` forks with the new
model (consistent with append-only, but fragments sessions).

## G20 · Interactivity detection (TTY/headless) — `api.md` / `agente.md` §5 / `chat.md` §8 — **RESOLVED**

**Resolution** (applied in [api.md](api.md) §2/§9, [agente.md](agente.md)
§5 and [chat.md](chat.md) §8): in headless the `enu.ui` module directly
**does not exist**; the test is `enu.has("ui")` — consistent with the
deny-by-default of worker `caps` (surface not granted is
not there) and without a new primitive. `enu.ui.interactive()` was
discarded (a UI module present but "off" invites calls that paint nothing);
exposing boot mode in `enu.sys` was discarded as redundant with
the above.

**Problem.** The default-deny of permissions in headless and "chat only
activates on interactive TTY" depend on knowing whether there's a terminal; no
primitive says so (the turn's pseudocode uses an `interactive()` that
doesn't exist).

**Impact.** The permission pipeline — a security decision — rests its
main branch on an unspecified function.

**Options.** (a) `enu.ui.interactive() -> boolean` (or a cap:
`enu.has("ui.tty")`); (b) in headless the `enu.ui` module directly does not
exist and the test is `enu.has("ui")` — consistent with worker caps
(deny-by-default of surface); (c) expose boot mode in
`enu.sys` (`enu -e` = headless by definition).

## G21 · ADR-010's first boot has no owner — ADR-010 / `api.md` §14 — **RESOLVED**

**Resolution** (applied in [api.md](api.md) §14,
[filosofia.md](filosofia.md) §2 and [arquitectura.md](arquitectura.md)):
option (a), reframed with the principle's general formulation — **the
kernel only knows its own capabilities** —, under which this is not
an exception: embedded extensions and their activation are the loader's
capability, so the question belongs to the kernel. The bare runtime (TTY +
no active plugin) paints a **fixed runtime screen**: version and
API, paths, embedded extensions and actions (activate the
official set, activate individually, quit) — fixed rendering, pre-Lua, with no
product logic; it is nu's permanent face without plugins, not a first-time
dialog. The appetite for "something usable without the harness" is covered by
one more official extension: **`repl`** (a Lua REPL over the public API),
activatable alone from that screen. Discarded: an always-active
bootstrap extension (a privileged plugin with no precedent, and would require
adding runtime plugin activation to the sacred API just for that
screen) and print-and-exit (contradicts ADR-010's "one key" and
philosophy §5).

**Problem.** With official extensions inactive by default and a
core that neither paints nor knows about agents (`enu.log` "never to the
screen"), what code shows the first-boot "one-key" activation offer?
ADR-010's central consequence has no mechanism.

**Impact.** The user's first experience — exactly what
ADR-010 says it protects.

**Options.** (a) A minimal, declared exception in the loader: if there are no
active plugins and there is a TTY, the core paints a fixed activation
prompt (the core's only UI, deliberately trivial); (b) an
always-active official `bootstrap` extension that does just this (does it
contradict ADR-010's "none activates itself"?); (c) no UI: the binary prints
instructions (`nu --enable-official`) and exits — austere but hostile.

## G22 · Resolution of semantic colors between core and toolkit — `api.md` §9.2 — **RESOLVED**

**Resolution** (applied in [api.md](api.md) §9.2,
[arquitectura.md](arquitectura.md) and guide §6): option (b) — the core only
accepts **literal** colors (`#rrggbb`, index 0-255; degraded to
`enu.ui.caps().colors` at paint time); semantic vocabulary and themes
are entirely the toolkit's, which resolves name → literal when building
the Blocks. Decisive reason: not to freeze a single theming model into the
sacred API — a global core palette would constrain
alternative toolkits with richer models; within the extension space theming
can compete and iterate. Mitigations for the known costs: the toolkit's
retained tree only re-renders on theme change (its consumers change
live for free); raw `enu.ui` plugins that use theme colors subscribe to its
change event (the same convention as `ui:resize`: your region, your
repaint); live change for non-cooperating plugins is assumed imperfect. Discarded:
(a) an `enu.ui.theme` table in the core (blesses a single model and puts
theming vocabulary in the sacred API); (c) style-by-reference (too much
surface for the same outcome).

**Problem.** A core `Style` accepts semantic names (`"accent"`,
`"error"`), but themes are toolkit plugins
([chat.md](chat.md) §7): it isn't defined who translates name → concrete
color, nor when (when building the Block or when painting?).

**Impact.** `Style` is sacred API; the entire theming system (and the
"only semantic colors" rule from guide §6) depends on this piece.

**Options.** (a) A minimal registry in the core — `enu.ui.theme(table)`
defines the semantic palette; themes (toolkit plugins) call it and
the compositor resolves at paint time (switching themes repaints everything, the
Blocks are not rebuilt); (b) semantic names don't belong to the core: the
toolkit resolves to concrete colors before building Blocks and `Style`
only accepts literal colors (purer core; but each Block ends up
"baked" with its theme and guide §6 would become a toolkit rule);
(c) indirection by reference in the Block, resolved at paint time (the most
flexible and the most expensive to specify).

## G23 · LLM vocabulary in the sacred API (`enu.text.approx_tokens`) — `api.md` §10 / `providers.md` §5 — **RESOLVED**

**Resolution** (applied in [api.md](api.md) §10, [providers.md](providers.md)
§4/§5 and [agente.md](agente.md) §8): the primitive **leaves the core**. It
fails both yardsticks at once: "LLM token" is product vocabulary
([filosofia.md](filosofia.md) §2), and the heuristic (~4 bytes/token) is a
plain Lua division — with no heavy work there's no primitive to justify
("Lua decides, Go executes"). Unlike markdown/highlighting, whose
concession is sustained by performance, this one had no support. The helper moves
to the providers extension — the owner of token vocabulary and of
`count_tokens?` exactness — as `providers.approx_tokens(s)`, in Lua.
Renaming it in the core to something neutral was discarded (any name would
still exist only to estimate tokens: makeup, not a resolution); keeping
it as a documented concession was discarded (with no performance cost to
justify it, it would set the precedent that philosophy §2's yardstick is
negotiable within the sacred surface itself).

**Problem.** `api.md` §10 exposed `enu.text.approx_tokens(s)` documented
as an "LLM token heuristic estimate", while `providers.md` §5
stated in the same breath that token counting is "never the core's job
(ADR-003: the core doesn't know what an LLM is)." Philosophy §2's yardstick —
product vocabulary = extension — was undermined within the
sacred API itself.

**Impact.** More philosophical than functional, but on the surface being
frozen: what enters with product vocabulary cannot be unfrozen,
and it weakens the minimal-kernel argument for every future dubious
case.

**Options.** (a) Rename it in the core to neutral vocabulary
(`bytes_estimate` or similar); (b) keep it as a documented concession,
in the style of markdown/highlighting; (c) remove it from the core and move the
helper to the providers extension (one line of Lua).

## G26 · Extension namespaces reserved to the core — `api.md` §4 / guide §7 / `agente.md` §4 — **RESOLVED**

**Resolution** (applied in [api.md](api.md) §4 and §14, [guia-plugins.md](guia-plugins.md)
§7 and [agente.md](agente.md) §4): a **two-level** scheme, with no extension
names reserved in the core. (1) The core reserves only its own —
`core:` and `ui:`, the surfaces the kernel itself emits. (2) Every other
namespace belongs to a plugin by convention (namespace = plugin name), and the
collision between extensions is closed by the loader, which guarantees that
a plugin's name is unique — it is its identity (storage `plugins/<name>/`,
`requires` resolution, name-based replacement of embedded ones; two
identical names = load error). So `agent:` stops being a core
reservation and becomes the namespace of the `agent` plugin, protected the same way as
`mi-plugin:` — no privilege: nobody else can be named `agent`, and the agent
cannot claim `mi-plugin`. Discarded: reserving `agent:` (and the
namespaces of the other official extensions) in the core: a kernel reserving a name
on behalf of an extension is exactly what «the kernel only knows
its own capabilities» prohibits ([filosofia.md](filosofia.md) §2, ADR-003) — the
same yardstick that closed G21 and G23. Also discarded: a central
namespace registry in the core (again, extension vocabulary in the
sacred surface).

**Problem.** The guide (§7) listed `core:`, `ui:` **and `agent:`** as
reserved event namespaces, while [api.md](api.md) (§4, §17) reserves
only `core:`/`ui:`. The inconsistency hid a deeper one: `agent` is an
official extension, not the core; having the core reserve its namespace forces it to
know an extension by name, against ADR-003. And without that reservation
it remained unanswered what prevents two extensions from declaring the same
namespace.

**Impact.** Coherence of the extension model on the surface being
frozen; touches the minimal-kernel principle that underlies G21/G23. Cheap
now, costly after freezing.

**Options.** (a) Reserve `agent:` (and the other official ones) in the core —
convenient, but puts extension names into the sacred API; (b) a
namespace registry in the core that extensions claim on load — resolves
collisions but at the cost of surface and of the core knowing about product
namespaces; (c) two levels by convention: the core reserves only `core:`/`ui:`,
and plugin name uniqueness (a loader guarantee) protects
extensions from each other — `agent:` is just one more plugin namespace.

## G27 · `enu.task.all` doesn't specify result order — `api.md` §3 — **RESOLVED**

**Resolution** (applied in [api.md](api.md) §3): `enu.task.all` returns
results **aligned with the inputs** (`out[i]` is the one from `fns[i]`),
regardless of completion order — `Promise.all` semantics. Not new API:
it fixes the order semantics of a primitive that already existed. It passes
philosophy §4's yardstick, which rules out the alternatives: *allSettled*
(wrapping each branch in `pcall`) and a concurrency limit (a semaphore
made of `enu.task.future`) are composed in Lua by a plugin, so they stay in
userland; a core primitive's order **cannot** be fixed from
the outside, so it's its contract. Completion-order discarded: it breaks
the result↔input correlation and forces every caller to re-tag, exactly
the friction that "composes better across layers" (§1.4) wants to avoid;
aligning is furthermore free (write into the indexed slot on resolution, without
losing parallelism). A new `enu.task.all_settled`/`map_limit` function was
discarded: it would be ad hoc sacred surface for what Lua already does
(philosophy §3/§6).

**Problem.** The signature `(fns) -> any[]` says "wait for all" but not that
`out[i]` corresponds to `fns[i]` — tasks finish in any order.
For deterministic parallel orchestration (a fan-out of subagents over
territories) that's exactly what's needed guaranteed: without positional
alignment there's no way to correlate a result with a territory except by
manually stuffing the index inside each payload. Same class of underspecification
that rounds 3-4 hunted (cf. G8, G10): behavior that would vary with the
scheduler inside the sacred API.

**Impact.** Any consumer of `task.all` with more than one result;
blocks round 5's deterministic parallel orchestration. Cheap now,
impossible to change after freezing.

**Options.** (a) Specify `Promise.all` semantics (input order,
not completion order); (b) leave it in completion order and have the caller
carry the index (friction on every use, against §1.4); (c) add
new variants (`all_settled`, `map_limit`) — ad hoc surface for what Lua
already composes.

## G28 · `Region:blit` with negative local coordinates (viewport/scrollback) — `api.md` §9.1 — **RESOLVED**

**Resolution** (applied in [api.md](api.md) §9.1 and
[guia-plugins.md](guia-plugins.md) §6): option (a) with three nails. (1)
`blit` clips at **both ends**: negative `x/y` clip the Block's
leading edge (`blit(0, -3, doc)` shows `doc` starting from its fourth row),
symmetric to over-limit clipping — a viewport over a Block larger than
the region. (2) Explicit guarantee: blitting the same Block with a different
offset is **a copy, never a re-render** (the cost of scrolling is that of copying
the visible window). (3) **Virtualization** (not building the whole Block for
huge histories) belongs to the toolkit, not the core. Discarded: a dedicated
viewport primitive (b): adds surface for what the negative
already gives; discarded clipping in Lua (c) for the cost on the main state.
The pattern "cache the Block, move the offset" stays in guide §6 (with its
antipattern: rebuilding the Block on every scroll).

**Problem.** `Region:blit(x, y, block)` "clips to bounds", but the
spec only covers **over-limit** clipping (the part of the Block that
goes past the region's edge). A transcript with scroll needs the
opposite: stamping a tall Block with a **negative** `y` to clip its
first rows and "peek" it from below — a viewport over a large Block,
where scroll = re-blit with a different offset (round 6, scenario 28). It isn't
written whether negative local coordinates are legal nor what they do.

**Impact.** Any UI with scrollback — `chat`'s transcript the
first one; the toolkit needs it resolved before the spike. If it weren't
legal, every plugin would have to clip the Block in Lua before every
`blit` (work proportional to content on the main state, against
"Lua decides, Go executes").

**Options.** (a) `blit` accepts negative `x/y` and clips the leading edge
(initial rows/columns) as well as the trailing one — a viewport
over the Block at no cost in Lua; (b) a dedicated viewport primitive in
`Region` (`Region:scroll(block, offset)`) that encapsulates the clamp and the
offset; (c) leave it to the plugin: clip the Block in Lua before
`blit` (rejectable given the cost on the main state).

## G29 · Mouse in global coordinates without translation to region (hit-testing) — `api.md` §9.1/§9.3 — **RESOLVED**

**Resolution** (applied in [guia-plugins.md](guia-plugins.md) §6): option
(c) — the screen→content mapping belongs to the **toolkit**, not the core, by
the same split as G1 (relayout) and G22 (theming): whatever depends on the
layout the plugin owns belongs to the plugin. The decisive reason is that
`Region:hit` (a) could only do the **trivial half** — subtracting the origin
`x,y` that the plugin itself set —, while the valuable half (which
block/line of a **scrolled** wrapped Block was clicked) needs the scroll
offset and the content layout, which the core does not retain (the blit from
G28 is ephemeral). Adding `Region:hit` would be sacred surface for something
the plugin already gets for free, and it would also ignore z-order/occlusion
(an occluded region would return coordinates all the same). Discarded (b)
delivering the mouse in local coordinates: routing by geometry inside the
core puts a piece of the toolkit in the kernel, against the stack model of
§9.3. If the toolkit demonstrates that it repeats the same calculation
everywhere, *then* a primitive gets promoted — with evidence, not ahead of
time.

**Problem.** The mouse event (`ev.type == "mouse"`) carries `x, y` in
**screen** coordinates, but regions live in **local** coordinates (and their
content, moreover, offset by the G28 scroll). There is no
`Region:contains(x,y)` nor global→local translation. To click a widget — a
tool block's header to collapse it, a modal's button — the plugin manually
tracks the geometry of each region (which it set itself) and resolves the
hit-test by adding/subtracting origin and offset (round 6, scenario 31).

**Impact.** Every clickable widget of the toolkit reimplements the same
calculation; repeated friction in the layer that will use it the most.

**Options.** (a) `Region:hit(x, y) -> (bx, by) | nil` — translates
screen→local and returns `nil` if the point falls outside (with G28,
counting the scroll offset); (b) deliver the mouse event already in local
coordinates to the region under the pointer (changes the input stack model
of §9.3, which today is global and consumption-based); (c) document that the
mapping is the toolkit's responsibility, since the plugin knows the geometry
it set (cheap, but leaves hit-testing outside the core forever).

## G30 · Pasting an image: the `paste` event only carries text — `api.md` §9.3 — **RESOLVED**

**Resolution** (applied in [api.md](api.md) §9.3): pasting **non-text**
content from the clipboard (an image) **injects a path**, not the bytes. The
core dumps the image to a session temp file (`enu.fs.tmpdir`) and delivers a
`paste` event with `path` (without `text`); the UI inserts the path exactly
like an `@` mention, and the agent decides whether to read it (the content is
not blindly embedded, just like the mentions in [chat.md](chat.md) §3).
Binary bytes never cross text/JSON boundaries (consistent with G11). This is
**different from P6** (rendering images in the transcript, postponed): that
one is about painting, this one is about input. Discarded delivering the
bytes in the event (reintroduces binary at the input boundary that G11
closed) and discarded folding it into P6 (P6 is output; pasting a path is
useful even if the image is never painted).

**Problem.** A coding harness (claude-code style) pastes images from the
clipboard, but the `paste` event only carries `text` and `clipboard_get`
returns `string`: pasting an image could not be expressed (round 6, scenario
29).

**Impact.** Everyday flow of a coding harness; cheap to close now on the
surface being frozen.

**Options.** (a) The `paste` event for non-text content delivers `path`
(dumped temp file), insertable as `@` — the chosen one; (b)
`enu.ui.clipboard_get_image() -> path?` as a separate call (extra surface for
the same thing); (c) leave it out of v1, folded into P6 (discarded: P6 is
output).

---

## G31 · The ⏸ bridge cannot yield through `pcall`/tail call in gopher-lua — `api.md` §1.3/§1.4 — **RESOLVED**

**Resolution** (decision in [adr.md](adr.md) ADR-011; no changes in
[api.md](api.md): the API was correct, the realization technique was what
failed). The scheduler is realized **without coroutine yields**: one
goroutine per task + a single Lua execution token. A ⏸ primitive releases
the token, does the blocking work in a background goroutine, and reclaims it
on return; since there is no yield, `pcall`, tail calls, and error unwinding
are gopher-lua's native ones and survive the suspension. Implemented in S04
(`internal/runtime/scheduler.go`), validated with `-race`.

## G32 · The session lock needs its OWN pid and the API does not expose it — `api.md` §7 / `sesiones.md` §6 — **RESOLVED**

**Resolution** (applied in [api.md](api.md) §7/§16/§17 and
[sesiones.md](sesiones.md) §6): a minimal primitive —`enu.sys.pid() ->
integer`, the pid of the current `enu` process—, immediate local query (not
⏸) and [W] like the rest of `enu.sys`. Together with `enu.sys.hostname()` it
forms the **writer identity** that the lock records (`{ pid, hostname,
started }`, §6). It is the fourth piece that the completeness corollary
(filosofia.md §2) demands: G17 added `fs.write{exclusive}` +
`enu.proc.alive(pid)` + `enu.sys.hostname()` to *create* the lock and
*validate other pids*, but it missed how to know the **own** pid that goes
inside the lock. Since this is the **first addition to the sacred surface
after the freeze**, `enu.version.api` goes from 1 to **2** (api.md §17: it
grows only by addition, the counter is incremented with each one); it is a
strict addition, breaking no signature. The dedicated primitive is justified
like G17's: it is **kernel** vocabulary (a pid belongs to the process, not
the product) and does not compose with what exists —`enu.proc` only knows
the pids of children it spawns, never `enu`'s own—. Discarded deriving it
from a subprocess (`enu.proc.run(["sh","-c","echo $PPID"])` is fragile,
expensive and POSIX-only) and discarded folding it into `enu.proc.alive` (that
is existence of a given pid, not discovery of one's own).

**Problem.** The lockfile from [sesiones.md](sesiones.md) §6 records
`{ pid, hostname, started }` with the **pid of the writing process**, but
[api.md](api.md) does not expose it: `enu.sys` gives
`platform`/`env`/`setenv`/`now_ms`/`mono_ms`/`hostname` (no pid) and
`enu.proc.alive(pid)` validates **other** pids (to detect orphaned locks) but
there is no way to get one's **own**. Without it the sessions extension (S38)
cannot write the specified lock: same class of crack as G17 (correct
resolution in prose, not writable with the specified API), and born the same
way while *building* the counter-code (S38), not in a pseudocode round.

**Impact.** Blocks S38 (the sessions extension); effectively reopens
G5/G17 (session corruption that they closed becomes possible again if the
lock cannot be written as specified). Cheap to close now, on the surface
being frozen.

**Options.** (a) `enu.sys.pid() -> integer` (the chosen one): minimal, kernel
vocabulary, sibling of `hostname`; (b) extend `enu.proc` with a
`enu.proc.self()` — puts the own pid in the *subprocess* module, where it
does not fit (proc manages children); (c) reduce the lock's content to just
`{ hostname, started }` and trust `O_EXCL` for uniqueness — loses the orphan
detection by pid from §6 (a crash would leave the lock forever), can be
discarded.

**Problem.** Arose while **building** the keel (S04), not in a pseudocode
round. gopher-lua (Lua 5.1 semantics) does not let a coroutine yield across a
Go call boundary. Verified against v1.1.2: (1) `pcall(fn)` with an `fn` that
suspends **aborts** the coroutine at the `pcall` instead of yielding — but
§1.4 promises capturing structured errors with `pcall`, and the pseudocode
does so around IO operations (⏸); (2) `return ⏸fn()` in tail position loses
the continuation (`OP_TAILCALL` elides the frame before the yield). Same
root: the yield does not cross Go boundaries.

**Impact.** Foundational: without this, the error model of §1.4 (pcall over
code that suspends) does not hold, and the whole ⏸ API has footguns in tail
position. It is the keel — cheap to close here, very expensive later.

**Options.** (a) **Goroutine-per-task + Lua token** (no yield): native
pcall/tail call/errors — the chosen one (ADR-011); (b) keep the coroutine
bridge and build a *yieldable* `pcall` (pcall as a sub-coroutine) + Lua
trampolines for tail calls: more invasive, would defer a `pcall` broken by
default and still fragile; (c) switch Lua runtimes — disproportionate
(ADR-002 already decided). The **non-capturable** unwinding of S08
(cancellation/watchdog) will be designed on top of (a) with its own sentinel
panic, not with the yield discarded here.

## G33 · Startup without a TTY has no onramp and "the official set" is undefined — `api.md` §14 / ADR-010 / G21 — **RESOLVED**

**Resolution** (recorded in [ADR-015](adr.md#adr-015--conjunto-oficial-de-producto-y-onramp-no-interactivo), which **refines** ADR-010; applied in [api.md](api.md) §14, [arquitectura.md](arquitectura.md) §5, [filosofia.md](filosofia.md) §5 and the docs site): two pieces, neither in the sacred API.

1. **Non-interactive onramp: the `nu --default-config` CLI flag.** The bare
runtime screen from G21 solved the first startup **TTY-only** (it is UI;
§14 closes it with "No TTY, no screen: starts bare"). The no-TTY case —CI,
Docker, scripts— was left with no single step to activate the official set:
one had to write `config.dir()/enu.toml` by hand. The flag covers it with
**two modes**: alone (`nu --default-config`) **writes** `plugins.enabled`
with the product set and exits (idempotent, atomic, preserving the rest of
the file — reuses `writeEnabledPlugins`, the same path as the TTY action);
combined with a headless action (`--default-config -p '…'` / `-e '…'`) **does
not touch disk**: it activates the set **only for that process** (internal
`WithEnabledPlugins` option) and runs the action. It lives in the **binary**
(`main.go`), not in `enu.*`: it is the CLI surface of S45 —like
`-e`/`-p`/`--continue`—, so **`enu.version.api` does not change** (unlike
G17/G32, which did extend the sacred surface). The core still does not know
what an agent is (ADR-003): the flag orchestrates extensions through the
public API, like an `init.lua` could.

2. **Definition of "the official product set".** Until now "the official
set" was, in fact, `embeddedNames()` (*everything* embedded), which includes
`example` — the scaffolding plugin that exists **only to test the gating**
from ADR-010 ([implementacion.md](implementacion.md), Phase 8). Putting it in
the user's default config is noise. The set is fixed at the **seven product
ones** —`providers, sessions, agent, mcp, chat, repl, toolkit`— = the
embedded catalog **minus `example`**, closed under dependencies
(`agent → providers, sessions`; `mcp → agent`; `chat → toolkit, agent,
providers, sessions`). For **consistency** (golden rule of the flow: a
semantic is not contradicted across documents), G21's TTY action now
activates **the same** set: the bare screen and the flag plug in the same
thing. The "product vs. everything embedded" distinction lives in a single
source (`officialProductSet`, derived from `embeddedNames` filtering out
`example`).

**Same set in both modes**, including `chat`: even though `chat`/`repl` need
a TTY, their `init.lua` already self-gate with `if enu.has("ui")` — without a
UI surface they stay inert on their own (G20/§9). Activating them in headless
does not get in the way, and omitting them would require a second list and
an edge case with no gain. Discarded.

**Problem.** Two cracks that surface when *using* the finished binary to try
it out with its official extensions (not in a pseudocode round nor building
the kernel: using it). (a) ADR-010 leaves the official ones **inactive by
default** and G21 gave the onramp for the first startup, but **TTY-only**;
in headless (`enu -e`/CI/Docker) there is no single step to activate the set:
one has to edit `enu.toml` by hand, effectively contradicting the "one-key"
ergonomics ADR-010 promises. (b) "The official set" was never precisely
defined against `example`: `ActivateOfficial()` activates the whole
`embeddedNames()`, so today's TTY action already puts the test plugin into
the user's config.

**Impact.** It is the **first experience** for whoever installs `enu` and
wants the harness in CI/a container — exactly what ADR-010 says it protects,
but on the non-interactive side that G21 did not cover. It does not block
any build session (the plan is closed, 45/45); it is product debt, cheap to
settle on the already-frozen CLI surface (S45), without touching the sacred
API.

**Options.** (a) **The `nu --default-config` flag** (the chosen one):
non-interactive mirror of the screen's action 1, with an ephemeral mode for
immutable Docker; lives in the binary, does not touch `enu.*`. (b) Expose the
write to Lua (`enu.config.enable_official()`) and solve it with `enu -e`:
**extends the sacred API** (`enu.version.api`++) to *worsen* the ergonomics
(`enu -e 'enu.config.enable_official()'` is no easier than the flag) —
contradicts the goal; discarded. (c) An `nu init` subcommand: semantically
honest, but it would introduce the binary's **first subcommand** (today only
flags), a gateway to `nu run`/`nu chat`… which S45 avoided by keeping the
binary thin; premature for a single need. (d) Do nothing and document "edit
`enu.toml`": austere and hostile, exactly what ADR-010 wanted to avoid (it is
option (c) discarded in G21, now for the no-TTY case).

## G34 · The canonical `thinking` model does not express adaptive mode (Opus 4.6+ 400s with `budget_tokens`) — `providers.md` §2.1/§3 — **RESOLVED**

**Resolution** (recorded in [ADR-016](adr.md#adr-016--modelo-canónico-de-thinking-con-mode-y-traducción-por-modelo-en-el-adaptador), which **reopens and closes** [P21](pospuesto.md); applied in [providers.md](providers.md) §2.1/§3 and the `anthropic` adapter's `⚠` note): the canonical parameter grows **by addition** to `thinking?: { mode?: "off"|"adaptive"|"budget", budget? }` —with `{budget=N}` as a **compatible alias** of `mode="budget"`, so the frozen form remains valid—, and each model's **reasoning dialect is declared as DATA** in `providers.toml` (`thinking = "adaptive"|"budget"|"none"`, default `"budget"`), which travels in `ModelInfo` and the adapter reads to translate **per-model** (`adaptive` → `{type="adaptive"}`, `budget` → `{type="enabled", budget_tokens=N}`, degrading between the two according to the dialect; `none`/absent → nothing is sent, degradation declared §3 ob.5). The adapter remains a **pure translator** (ADR-003/ADR-005): zero model-version tables in code. The sacred `enu.*` surface does not change (this is an extension contract). **Implemented** (a build session following the ADR, as the "the contract leads, the code follows" protocol mandates): `thinking_to_wire` in `adapter_anthropic.lua` translates by dialect, `resolve` carries `model.thinking` into `ModelInfo`, and `providers_p21_test.go` shields the eight combinations (dialect × mode); the legacy unconditional `budget_tokens` block no longer exists.

**Problem.** The canonical form froze `thinking?: { budget?: integer }` and
the `anthropic` adapter emits it as `{type="enabled", budget_tokens=N}`
(legacy extended thinking). The Opus 4.6+ family —including the default
model `claude-opus-4-8`— retired `budget_tokens` and expects
`{type="adaptive"}`: the request gets a **400** against the real API. This
is not a defect in the adapter (it fulfills the frozen contract) but in the
**canonical model**, which lacks (1) vocabulary to request adaptive mode and
(2) the data on which form each model understands. Validated in
[pseudocodigo.md](pseudocodigo.md) Round 7 (scenario 32): the "budget over
legacy" branch is expressible, the "adaptive over Opus 4.6+" branch has
**no** code that can write it. It was postponed as P21; the trigger (the
default model already being Opus 4.8) reopens it.

**Impact.** **Latent** —the headless agent does not fill in `req.thinking`
when assembling the turn, so the 400 only appears via a `request.pre` hook
or a future reasoning-control feature— but it **blocks the capability** of
using extended reasoning with modern Opus models, which for a coding harness
is top-tier. Cheap to close in the contract now; expensive later, with
thinking wired in and consumers assuming the old canonical form.

**Options.** (a) `mode` in the canonical form + per-model dialect as TOML
data (**the chosen one**, ADR-016): pure translator, growth by addition; (b)
heuristic on the model id in the adapter (`model:match("opus%-4%-[6-9]")`)
— fragile, puts product-version knowledge into a translator, fails with
renamed ids; (c) **replace** `budget` with the new form — breaks the frozen
signature and recorded tests; (d) leave it postponed — discarded: the
trigger (default model Opus 4.8) is already active.

## G35 · ADR-015's onramp activates the plugins but leaves no agent config: the first `enu` dies with no model and leaves the UI stuck — ADR-015 / `chat.md` §8 / `agente.md` §10 — **RESOLVED**

**Resolution** (recorded in [ADR-017](adr.md#adr-017--el-onramp-deja-config-de-agente-usable-y-el-chat-degrada-con-gracia), which **refines** ADR-015; applied in [chat.md](chat.md) §8, [agente.md](agente.md) §10, [providers.md](providers.md) and the binary): two pieces, neither in the sacred API (it is CLI surface, loader, and extension Lua; `enu.version.api` does not change).

1. **Complete onramp: `nu --default-config` leaves USABLE agent config.**
The persistent mode, besides writing `plugins.enabled` in `enu.toml` (G33),
writes **active templates** of `agent.toml` (`model = "anthropic/opus"`,
`max_turns`) and `providers.toml` (provider `anthropic` with `base_url`,
`api_key_env = "ANTHROPIC_API_KEY"` and the model
`claude-opus-4-8`/alias `opus`) **only if they don't already exist** (never
overwrites; atomic, idempotent — reuses `writeAtomic` and the "don't clobber
existing TOML" pattern from `writeEnabledPlugins`). Default **opinionated
toward Anthropic** (the product's identity, a claude-code-style harness).
The key **never** goes into the file (providers.md §1): it lives in the
environment (`api_key_env`). The success message stops being misleading
("you can now run the agent: enu -p") and becomes **honest and
actionable**: it lists the files written and reminds the user to export
`ANTHROPIC_API_KEY` (or edit `providers.toml`) before starting.

2. **Graceful degradation of the chat (robustness, principle 5).** If
`chat.start` cannot build the initial session (`agent.session` throws
`EINVAL` for a missing model, `EPROVIDER` for an unresolvable
provider/model, or `EAGENT`/`EPROVIDER` for broken TOML), the chat **does
not die into the log**: it mounts a **minimal, actionable, exitable UI** —a
text explaining how to configure (`agent.toml`, `providers.toml`, the API
key) and an exit keymap (`esc`/`q`/`ctrl+c` → `core:shutdown`)—.
**Unexpected** errors (not config-related) still propagate as today. As a
kernel **safety net**, interactive mode additionally installs an
emergency-exit handler at the bottom of the input stack (any mounted app
covers it), so that **no** path leaves the terminal in raw mode with no way
to exit via keyboard.

**Why active templates and not commented-out ones.** With the key in the
environment, `enu` *just works* after a single command (ADR-015's
"batteries-included" promise, now real). Without the key,
`providers.resolve` **does not fail** (it leaves `api_key=nil`): the chat
mounts anyway, the statusline shows the model, and the error for the
missing key comes out **in-transcript** on the first turn (`agent:error` →
`transcript:add_error`, which the chat already renders) — much better than
a dead screen. Commented-out templates would force editing TOML before the
first startup, exactly the friction the onramp was meant to remove.

**Problem.** Surfaces when *using* the finished binary (like G33, not in
pseudocode nor while building): after `nu --default-config`, running `enu`
leaves the terminal blank. The log says it: `ERROR [user] chat: could not
start: agent.session requires model ("provider/model") in opts or in
agent.toml`. Two layers: (a) the onramp activates the seven plugins but
**leaves no `model`/`provider`**, so `core:ready` → `chat.start` →
`agent.session({model=nil})` throws `EINVAL`; (b) the chat's `init.lua`
`pcall` sends the error to `enu.log.error` (to disk, never to screen, §15)
and **mounts nothing**, and since the bare screen (the only path that
installs an emergency-exit handler) is not taken with plugins active, the
user **gets stuck** —in raw mode `ctrl+c` does not generate `SIGINT`—.

**Impact.** It is the **first experience** for whoever follows ADR-015's
onramp to the letter: the command that promised to leave the harness ready
leaves it broken and unusable. It fully blocks the product's interactive
startup. Cheap to close on the already-frozen CLI surface (S45) and the
extensions' Lua, without touching the sacred API.

**Options.** (a) **Complete onramp + graceful degradation** (the chosen
one, ADR-017): the onramp leaves usable config *and* the chat survives the
absence of config. (b) Degradation only: the chat mounts an actionable UI,
but the first `enu` still has no model and requires editing TOML by hand —
undoes ADR-015's ergonomics. (c) Onramp only: write the templates, but the
chat would still die if the user deletes/breaks the config — leaves the
second defect (stuck UI) unclosed. (d) A **default model hardcoded in the
agent** without `providers.toml`: puts product vocabulary (which model,
which endpoint, which env) into the engine, against ADR-003/ADR-005;
discarded. (e) Do nothing and document "edit `agent.toml`/`providers.toml`":
hostile, exactly what ADR-010/ADR-015 wanted to avoid.

## G36 · The official product set auto-mounts two UIs (chat and repl): quitting the chat leaves the REPL underneath — ADR-015 / `arquitectura.md` §Distribución / `chat.md` §8 — **RESOLVED**

**Resolution** (applied in the `repl` extension's `init.lua`, without
touching the sacred API; documented in [arquitectura.md](arquitectura.md)
§Distribución and [chat.md](chat.md) §8): the repl **yields the screen to
the chat**. Its auto-mount on `core:ready` becomes conditional: it only
mounts its UI if `chat` is **not** among the active plugins (checked via
`enu.plugin.list()`, without `require`ing chat —the repl must be able to
activate SOLO, G21). With the official set active, only the chat opens; the
repl remains an accessible module (`require("repl")`, `repl.eval`) but
inert as a UI. With only `repl` active (G21), the REPL opens. In headless,
neither mounts a UI. Additionally, `Chat:quit` (and `ctrl+c`) emit
`core:shutdown`: **closing the chat shuts down the binary** instead of
returning the user to a lower layer.

**Problem.** Surfaces when *using* the product, not in pseudocode. ADR-015
set the official set as "the seven embedded ones minus `example`",
including `repl`, reasoning **only about the headless case** ("chat/repl
self-gate with `enu.has("ui")` and stay inert without a UI, so activating
them together does not get in the way"). But **with a TTY** —the product's
real experience— the `init.lua` files of both chat *and* repl subscribe to
`core:ready` and **both** mount a full-screen `toolkit.app` over the same
compositor. They overlap; and since the chat did not shut down the runtime
on exit, closing the chat left the Lua REPL mounted underneath: the
sensation, described by the user, of "exiting the chat extension and then
the lua interpreter." ADR-015's reasoning had a gap: *activating them in
headless* does not get in the way, but *activating them together with a
TTY* does.

**Impact.** It is the finished product's first impression: instead of a
single, polished TUI, the user perceives layers that need to be closed one
by one. Cheap to close on the extensions' Lua (the repl checks the already
existing loader registry) without touching the sacred API nor ADR-015's set
(the repl stays in it, installed and accessible; it just does not compete
for the screen).

**Why the repl yields instead of being removed from the set.** Removing
`repl` from `officialProductSet` would uninstall it from the product (it
would not be available to be activated alone from a session with the
official set). The repl is valuable as a tool for extension authors (G21);
what is excessive is not its *presence* but its *competing* for the screen.
Yielding it —the "a single extension owns the primary UI" pattern—
preserves ADR-015 and resolves the overlap. The chat, the harness's UI, is
the one in charge when present.

## G37 · `blitBlock` inverts the sign of the X offset relative to Y and to the `Region:blit` contract — `api.md` §9.1 / `compositor.go` — **RESOLVED**

**Resolution** (applied in `compositor.go`; no change in api.md —it fixes
the *implementation* to comply with the already-documented contract):
`blitBlock` stamps the Block's origin at `(ox, oy)` with the **same** sign
on both axes. The X axis goes from `lx = col - ox` to `lx = col + ox`, just
like the Y axis already did with `by = ly - oy` (a negative `oy` trims the
initial edge). G28's horizontal viewport test is corrected to the
consistent semantics: `blit(-2,0)` trims the start ("CDEF…"), `blit(+2,0)`
shifts right ("  AB").

**Problem.** [api.md](api.md) §9.1 documents `Region:blit(x, y, block)` as a
symmetric viewport: "`x/y` can be **negative** and they trim the block's
*initial* edge (`blit(0,-3,doc)` shows `doc` from its fourth row on)." The Y
axis complied with it; X was **inverted** (`lx = col - ox`: it was the
*positive* value that trimmed the start). It was never noticed because **no
widget was ever blitted at x>0**: the chat, the bare screen, and the repl
all stacked everything against the left margin. When `padding`/alignment
was introduced in the toolkit (G36), a widget placed at x=1 lost its first
column (a box's left border, a line's bullet), because the app calls
`region:blit(ax, ay)` expecting to *position* and got a *scroll* on the X
axis.

**Impact.** Latent but real: it blocks any layout with horizontal
margin/padding/centering —that is, almost all product UI (boxes, centered
modals, a padded statusline)—. Discovered while building the first border
widget. The fix aligns the implementation with the contract; it does not
extend or change the API (`enu.version.api` does not move).

## G38 · The project slug for `sessions/<proyecto>/` is unspecified — `sesiones.md` §2/§7 — **RESOLVED**

**Resolution** (applied in [sesiones.md](sesiones.md) §2): option (c), with
the **current algorithm frozen as-is**. Two pieces:

1. **The slug becomes part of the format.** §2 specifies the encoding the
implementation was already doing: every character outside `[A-Za-z0-9.-]` →
`_`, trimming `_` from both edges, empty → `"root"`. It is frozen with its
properties honestly declared: **readable and lossy** — not reversible,
collisions possible between pathologically similar `cwd`s (`/a/b` and
`/a_b`). It is not an identity but a **grouping key**: the session's
canonical identity travels inside the file (the `meta` line, with `cwd` and
`id`), and disambiguating a collision means reading `meta`. A reversible
encoding (percent-encoding) was discarded: it would buy a property nobody
asked for at the cost of readability and migrating every existing
directory.
2. **The extension exposes the encoding as pure functions**:
`sessions.slug(cwd) -> string` and `sessions.dir(cwd) -> string`, alongside
`open`/`list` in `require("sessions")`. Same split as G6/G22: the contract
gives the guarantee (the specified algorithm, for external tools that
compose paths without nu), the extension gives the convenience (plugins
don't reimplement it).

Note for the build session: the crack was already biting **inside the
repo** — three copies of the algorithm kept in sync by faith (`slug` in
`sessions/init.lua`, `trust_slug` literally duplicated in `agent/init.lua`,
and the Go replica in `main_test.go` with the comment "must match `slug`
from sessions/init.lua"). When building: `sessions.slug` becomes the single
Lua source of truth (the agent `require`s it for `trust.json` keys), and the
Go test's copy — unavoidable, Go cannot call Lua — starts replicating the
*specification*, citing it, not the code.

**Problem.** [sesiones.md](sesiones.md) §1 is documented as a public
convention ("any extension or external tool can read sessions without going
through the agent") and §2 places transcripts under `sessions/<proyecto>/`,
with "`<proyecto>` = cwd encoded as a slug" — but the cwd→slug algorithm is
not written down anywhere. The promise of third-party reading cannot be
exercised: whoever wants to *locate* a session's file knowing the `cwd` and
the id has to guess (or reverse-engineer) the encoding. Surfaced in round 8
of pseudocode ([pseudocodigo.md](pseudocodigo.md), scenarios 33-35), where a
distributed mesh needs it three times: committing the transcript inside the
result branch, reading the transcript to pick a `fork(at)` point, and
importing someone else's session by copying the JSONL to its place.

**Impact.** Any external consumer of the format: orchestrators, exporters,
cost statistics, third-party pickers. It also bites *inside* the process: a
plugin that wants to read the transcript of a session it opened itself has
no contractual way to find the file. Cheap to close (it's specifying what
the implementation already does, or exposing a helper); expensive to change
later, once external tools depend on a guessed encoding.

**Options.** (a) Specify the slug algorithm in sesiones.md §2
(deterministic, stateless, documented as part of the format); (b) don't
specify it and expose a helper from the extension (`agent.sessions.dir(cwd)
-> string` or `agent.sessions.path(cwd, id)`), leaving the encoding as an
internal detail — but then *external* tools (outside nu) still cannot
resolve paths; (c) both: the specified algorithm is the truth for external
tools and the helper is the convenience for plugins.

## G39 · `Session:fork` does not relocate: no `opts` (cwd/permissions/model) and `at` with no defined unit — `agente.md` §2 / `sesiones.md` §5 — **RESOLVED**

**Resolution** (applied in [agente.md](agente.md) §2 —signature, "Fork and
close" paragraph, and status note— and [sesiones.md](sesiones.md) §5):
option (c) with its three sub-decisions. Growth by addition: `fork(at?)`
remains valid.

1. **`Session:fork(at?, opts?)`** — the direct path to relocation: the
`opts` override what is inherited with the same ephemeral semantics as
`resume` (G18: not persisted, does not rewrite history), and permissions
**only narrow** (the `spawn` rule, §9/§11). The variant is born already in
its worktree, without the intermediate window of the detour (a live session
pointing at the wrong cwd).
2. **`Session:close()` enters the contract's signature.** It already
existed de facto (implemented, idempotent, releases the writer lock from
sesiones.md §6) and other flows need it: the lock conflict in §6 and any
orchestrator that opens N sessions and must release them deterministically.
House rule: close explicitly via `enu.task.cleanup`; GC as a non-deterministic
safety net (same pattern as the `Proc` objects in api.md §6).
3. **Semantics nailed down.** `at` indexes the **current message history**
(post-compaction; what the implementation already did) — and
`meta.parent.entry` is documented as a **navigational** link, not a replay
pointer. **Inheritance is fully specified** ("all of the parent's ephemeral
opts except for overrides"), which turns the current drift —v1's fork copies
a partial list that loses `skills` and `thinking`— into a nameable bug
backed by a contract. And the v1 **deviation is blessed**: the fork **copies
the prefix** into the child's transcript (sesiones.md §5 goes from "replay
reads from the parent" to copying) — the self-contained child is exactly
what makes transcripts travel between machines (round 8, scenario 35; P9).

Option (b) alone (blessing only the detour fork→close→resume) was
discarded: two steps and a double lock cycle for what is conceptually one
operation, with the loaded gun of a misplaced intermediate session. `close`
gets added regardless because it is lifecycle hygiene that was missing
independent of the fork.

Note for the build session: implement `fork`'s `opts?` and full inheritance
(today: a partial list at `agent/init.lua:1139` that omits `skills` and
`thinking`); prefix copying and `close` already comply.

**Problem.** Fork-as-replication —K variants sharing the transcript's exact
prefix (and its prompt cache) and competing in a tournament— requires each
variant to run in its own worktree (a distinct `cwd`: G16's remedy for
parallel writes) and sometimes with narrowed permissions or an alternate
model. But `Session:fork(at?) -> Session` accepts no `opts`, and what the
child session inherits from the parent is not written down. The natural
detour (closing the fork and reopening it with `agent.session{ resume = id,
cwd = ... }`, G18's ephemeral opts) *almost* works, but it relies on
`Session:close()`, which §2's status note takes for granted as implemented
and the **contract's signature omits**. Also `at` does not define what it
indexes (a JSONL entry, a message, a turn?) — `meta.parent = {id, entry}`
from sesiones.md §5 suggests entries, but the correspondence is implicit.
Surfaced in round 8 ([pseudocodigo.md](pseudocodigo.md), scenarios 34-35).

**Impact.** The fork tournament (local and distributed) is left one step
away from being faithfully expressible; plan B (fresh subagents with the
plan re-injected via prompt) loses exactly the fork's value — the shared
prefix and its cache. It also affects the lock-conflict flow in
sesiones.md §6, whose default exit is "fork": if the fork cannot relocate,
it inherits the same cwd that caused the conflict.

**Options.** (a) `fork(at?, opts?)` with the same ephemeral semantics as
`resume` (the opts are process state: not persisted nor rewriting history;
permissions only narrow, as in `spawn`); (b) bless the detour: add
`Session:close()` to the contract's signature and document the
fork→close→resume-with-opts pattern; (c) both — `fork(at?, opts?)` as the
direct path and `close` in the contract because it already exists de facto
and other flows need it. In any case: specify that `at` indexes
**transcript entries** (the unit of `meta.parent.entry`) and what the fork
inherits in the absence of opts.

## G40 · Permission denials are not observable as data — `agente.md` §4/§5 — **RESOLVED**

**Resolution** (applied in [agente.md](agente.md) §4 —the new event in the
notification list— and §5 —the "denial travels as data" paragraph—):
option (c) plus sub-decision (d). The principle: actionable prose is
*presentation*, not the carrier (consistent with the structured errors of
api.md §1.4). Every denial produces the object
`{ id, tool, args?, source = "deny"|"hook"|"default"|"headless", pattern?, suggested? }`
**exactly once**, with two destinations for two different consumers:

1. **`agent:permission.denied` event** (symmetric to `permission.asked`,
G3's attribution): for **live** observers — the node driver, telemetry,
UIs that aggregate denials.
2. **The same object in the denied `tool_result`'s `meta`** (providers.md
§2.2), which sesiones.md §3 persists intact: the denial **travels with the
transcript**, and a controller reading the session after the fact — even on
another machine, reading round 8's result branch — extracts it without
parsing prose. An event alone was not enough (it doesn't travel); a `meta`
alone wasn't either (it forces live observers to read transcripts).

In addition, what scenario 36 found ambiguous and the implementation was
already doing gets specified: **`tool.end` is also emitted for denied
calls** (every `tool.start` has its `tool.end`; UIs pair them), with
`is_error = true` — a *generic* failure channel, while `permission.denied`
is the *specific* one. §5's second buffer's prose does not change: it
remains what the model sees and the human reads.

The implementation made the best case for it: the data **existed and was
discarded at the boundary** — `check_permission` computes `suggested`
(`agent/init.lua:377`) and formats it into the string; `permission.asked`
already emitted `{ id, tool, args, suggested }` as data (line 397) while the
denial —the same crossing to the other output— emitted prose; and the four
denial sources produced four different prose formats. Note for the build
session: fill in the event's payload and the `tool_result`'s `meta` from
`check_permission`/`err_result` (have `check_permission` return the object
in addition to the reason); the `tool.end` emission on denials already
complies.

**Problem.** In headless mode with default deny (§5), a tool call denial
only exists as **prose**: the `tool_result` with `is_error` carries the
actionable error ("denied `bash:npm install`; add `allow =
[\"bash:npm *\"]`") — perfect for a human, useless for a program. The three
structured observation paths fail: the pipeline is deny → allow → hooks, so
a policy deny **cuts off before** reaching the `permission` hooks (invisible
to them); `agent:permission.asked` is only for the interactive flow (a
policy deny does not ask); and it is not even specified whether
`agent:tool.end` is emitted for a denied call whose handler never ran (nor
would its payload carry the pattern). Surfaced in round 8
([pseudocodigo.md](pseudocodigo.md), scenario 36): the asynchronous
escalation loop —denial → a human amends the Role → idempotent re-run—
turns default deny from friction into a mechanism, but it needs the denied
pattern **as data** and today would have to parse it from prose.

**Impact.** Any headless orchestrator wanting to turn denials into policy
amendments; also permission auditing and telemetry ("what did this session
deny and via which path?") and any UI wanting to aggregate denials without
re-deriving them. The permissions layer is among the most sensitive parts of
the contract; better to close observability before freezing it.

**Options.** (a) A notification event `agent:permission.denied` with a
structured payload (`{ session, tool, args?, pattern, source:
"deny"|"headless"|"hook" }`), symmetric to `permission.asked`; (b)
structured `detail` in the denial error/`tool_result` (the exact pattern as
a field, the prose as `message`), consistent with api.md §1.4's structured
errors; (c) both — the event for observers (orchestrators, UIs, telemetry)
and the `detail` for the caller holding the error in hand; (d) additionally
specify whether `tool.end` is emitted on denials (probably not: nothing
started — the new event covers that gap unambiguously).

## G41 · An error caught by `pcall` closes upvalues of LIVE frames (and the abort was not closing its own under the fix) — gopher-lua / `cancel.go` / `scheduler.go` — **RESOLVED**

**Resolution** (applied in `cancel.go` and `scheduler.go`; the public API
does not change — quite the opposite: Lua's semantics become the STANDARD
one, with no asterisks to document in api.md). Three pieces that hold
together:

1. **The `hasErrorFunc` flag stays armed while there is an active wrapping
`pcall`/`xpcall`** (a per-thread depth counter; upstream's flag is not a
stack, the counter is — this closes the nested-pcalls hole). This way
`raiseError`/`Error` stop running their `closeAllUpvalues()`, which closed
the upvalues of the ENTIRE stack — including those of live frames below the
pcall that catches. The field is unexported: it is written via
reflect+unsafe with offsets computed in `init()` that PANIC at startup if a
gopher-lua upgrade moves them (a loud failure, never a silent one).
2. **A Go trampoline interposed between the `PCall` and the protected
function closes, IN-FLIGHT during the panic, the upvalues of the unwound
segment** (`closeUnwoundUpvalues`, standard Lua's `luaF_close` semantics).
The WHEN is critical: gopher-lua's `PCall` recovery truncates the registry,
and closing afterward would only snapshot nils (verified: it crashes the VM
with nil-interface LValues). Without this closure, the upvalue cache
re-links dead-frame entries with new locals at the same indices — unrelated
closures sharing a cell.
3. **The ABORT's upvalue closure is done directly.** S16's trick
(`co.Error(tabla)` as the vehicle for the pre-abort `closeAllUpvalues`) was
**gated by the same flag** that piece 1 arms: a task canceled inside a
`pcall` (the agent's turn body) skipped the closure, its cleanup read `nil`,
and the turn died without resolving to anyone — the deadlock that
`TestSessionCancel` exposed under `-race`. `closeOpenUpvalues` now calls
`closeUnwoundUpvalues(co, 0)` without a panic-vehicle, immune to the flag;
`reraiseIfAborting` does the same before re-raising (also covers the
watchdog abort reclaimed in the wrapper).

**Problem.** A gopher-lua v1.1.2 bug (upstream): `raiseError` with
`hasErrorFunc == false` —every `pcall` with no message handler— runs
`closeAllUpvalues()` over the thread's ENTIRE stack, not just over the
unwound frames as standard Lua does. Three-line repro: after
`pcall(function() error("x") end)`, a previous closure that captured a live
local writes to a detached cell while its owner reads its local from the
registry — the write is silently lost. With ADR-011's scheduler this bit
hard: an event handler writing to a suspended task's upvalue lost the write
if ANY error had been caught before on that thread (e.g. the
`pcall(enu.fs.read)` of a missing `agent.toml`). Surfaced while building
G40's tests (round 8); the retrospective giveaway: **every Go test in the
project captured into globals** — someone tripped over this while building
and worked around it by instinct, without recording it.

**Impact.** The "subscribe, wait suspended, read what was captured"
pattern is idiomatic Lua and used to fail silently. Any third-party plugin
will write it. The discarded alternative —documenting it as a limitation in
api.md §1.3— would have frozen an "almost Lua" semantics with a permanent
asterisk; fixing it was chosen instead (project decision: "it has to be
solved, one way or another").

**Upstream (verified 2026-07-03).** Not fixed in gopher-lua: v1.1.2, which
this project pins in `go.mod`, is the **latest published release**,
`state.go` on master has been untouched since December 2023, and the bug is
reported as [issue #448](https://github.com/yuin/gopher-lua/issues/448)
("bug: pcall affecting function upvalues"), **open since July 2023** with no
response. The kernel's shielding is therefore necessary indefinitely; if
upstream ever closes it, the trigger to remove the shielding is that the
`TestG41*` tests pass with it disabled after the upgrade.

**Shielding.** `upvalues_g41_test.go`: the minimal repro, nested pcalls
(upstream's non-stacked flag hole), the real case (handler + suspended task
+ previously caught error), the closure boundary (what's live below stays
linked; what's dead above closes with its own value, not the truncated
registry's nil), the abort with cleanup (the interaction with S16), and the
abort's non-capturability (§1.3 intact). Full suite green under `-race` too.
Candidate for an upstream PR: the clean fix in gopher-lua would be for
`PCall`'s recovery to close from its `base` (like `luaF_close`), instead of
the current pair of over-closing-on-raise / no-closure-with-handler.

## G44 · The scheduler isn't pumped outside `Eval` calls: interactive mode doesn't run tasks and background timers die on every quiescence — `api.md` §3 / `modelo-ejecucion.md` — **RESOLVED**

**Resolution** (2026-07-13; option (b) — **persistent `RunTasks`**. Decided and **built the same day** on the same branch: the `G44 (kernel)` session in the [implementacion.md](implementacion.md) logbook; `scheduler.go`/`driver.go` are a 🔒 zone and carry their tests naming G44). Three pieces:

1. **The pumping state moves up to the `Instance`.** The results channel, the `outstanding` counter, and the cancellation map stop being local to each `RunTasks` invocation: background work survives across turns, and a late result no longer lands on an abandoned channel (nor leaks its emitting goroutine with >64 pending).
2. **Quiescence stops running down the background.** With `liveFg == 0` the loop no longer does `cancelAll()` + return: it waits. The `every`s keep their request in flight — they *pause* in the sense that there's no foreground, they aren't killed. `cancelAll` is now reserved for real shutdown (`ctx.Done`). Closes manifestation (2).
3. **Kick channel.** `EmitEvent`, `FeedInput`, and `CoSpawn` publish on a buffer-1 channel (the bell stays rung until someone looks — no lost wakeups) that forms the third case of the `select`: work queued from outside wakes the loop immediately. Closes the unbounded delay of manifestation (3).

Interactive mode launches that long-lived `RunTasks` (`PumpTasks`) alongside the driver — closing manifestation (1) —; `inst.mu` remains the **sole entry token to the VM**, and the discipline the resident loop needs already exists (`schedStep` takes and releases the lock per step, `scheduler.go`; waits happen without it): no new concurrency mechanism is introduced, just one more user of the existing one — the residual risk is one of *liveness*, not corruption, and the session's tests cover it. Headless mode keeps its semantics as the exit condition of the same loop (returning on foreground quiescence). **Doesn't touch `api.md`** (APILevel intact): the `enu.task.every` contract now holds exactly as written. [P33](pospuesto.md) (ctx in `HostFn`) remains intact and in view: its trigger cites this redesign.

**Construction** (same day; details in the `G44 (kernel)` row of the [implementacion.md](implementacion.md) logbook). Faithful to the three pieces, with two detail decisions: the contexts of in-flight requests hang off `inst.ctx` — not the loop's ctx —, so that whoever reclaims the paused background is the targeted cancellation of its task (§1.3) or `Close`, never the return of an invocation; and a CAS guard detects two simultaneous loops over the same state instead of corrupting it. Construction **uncovered a latent data race** that pumping made real: the compositor was mutated under `inst.mu` (the `enu.ui` hostcalls during a Call) but the driver (`attachOutput`), resize, and the bare screen touched it only under the scheduler's token — a coincidence impossible without continuous pumping, a race caught by `-race` on the driver's first test. Closed by making `inst.mu` the compositor's **sole** lock (`withUILock` on every access outside a Call), consistent with the role the `mu` comment already documented for it (A-26). Shielding 🔒: `scheduler_g44_test.go` (the every survives quiescence and keeps ticking under pumping; the kick wakes within bounded time with a long sleep in flight; reentry detected; shutdown via ctx; `Close` reclaims the background) and `driver_g44_test.go` (keymap → `enu.task.spawn` → ⏸ → repaint end to end over the driver — the skeleton of the chat turn —, responsive input while the task sleeps, clean shutdown of loop and pump). Full suite green with `-race`.

**Problem.** The wasm scheduler's lifecycle loop is **per invocation**: `RunTasks` only runs during `Boot` and the two headless `Eval`s (`vmwasm_loader.go:102`, `eval.go`). Three manifestations of the same crack, all empirically verified in the audit (ids A-34/A-01/A-03 of the report): (1) the interactive loop `drive()` (`driver.go:130-158`) only does FeedInput/Eval/flushFrame — any `enu.task.spawn` from a keymap or handler queues in `__ready` and **no one ever resumes it**; the `chat` extension runs the agent's turn exactly that way (`chat/init.lua`), so the killer app can't run over the TTY driver (the code itself flags it as pending in `vmwasm_loader.go:100-101`). (2) On reaching foreground quiescence, `RunTasks` does `cancelAll()` and returns (`scheduler.go:143-146`): the in-flight `sleep` of every `enu.task.every` receives an uncatchable `ECANCELED` and the timer's coroutine **dies outright** — it doesn't pause —, with no error or log; a later `RunTasks` doesn't revive it. (3) Work queued from external `Eval`s serialized by `inst.mu` (fs watchers, signals, input) doesn't wake `RunTasks`'s `select` (`scheduler.go:149-154`): the new task waits for the nearest in-flight request to expire — an unbounded delay, total loss if that request never finishes.

**Impact.** Structural: it's the runtime's biggest pending piece. Without it, interactive mode can't run the agent's turn (official chat included), extensions' `every`s (`chat` spinner, `toolkit`) die after startup, and the reactivity of any handler that fires async work is coupled to the luck of whatever IO is in flight. It de facto blocks the interactive product.

**Options.** (a) **Integrated loop:** `drive()` starts pumping the scheduler — a single loop that does `select` over input, hostcall results, and new-work signal, with `RunTasks` rewritten as a reentrant step (`schedStep` + signaled wait) instead of a loop-to-quiescence. (b) **Persistent `RunTasks`:** the results channel and the counter move to the `Instance` (not local to the invocation), foreground quiescence does NOT cancel background requests (the `every`s survive paused), and `EmitEvent`/`FeedInput`/`CoSpawn` publish on a *kick* channel that forms the third case of the `select`; interactive mode launches that long-lived `RunTasks` alongside the driver. (c) **Point patches** without an interactive loop (just the kick + not cancelling the background): fixes (2) and (3) but leaves (1), the manifestation that blocks the product. **(b)** was chosen: it keeps the current architecture (a single thread enters the VM, `inst.mu` as token), resolves all three manifestations, and doesn't rewrite the driver. (a) was discarded for coupling layers (`internal/runtime` ↔ `internal/vmwasm`) and rewriting the driver unnecessarily — it remains a natural evolution if the two-loop model ever ends up hurting (the unified `select` can absorb the existing kick and results channel). (c) was discarded for leaving manifestation (1) untouched, exactly the one that blocks the product.

## G45 · The [W] surface promised in `api.md` §16 doesn't reach workers: `extraPreludio`'s Lua wrappers don't cross over — `api.md` §16 / `vmwasm/worker.go` — **RESOLVED**

**Resolution** (2026-07-13; option (a), built the same day — details in the
`G45 (kernel)` row of the [implementacion.md](implementacion.md) logbook).
`AddPreludio` gains the **`AddPreludioW(snippet, needs...)`** variant that tags
the fragment as [W] and declares the **thunks it wraps** (`needs`, e.g.
`"re._compile"`); `spawnWorker` copies into the worker's preludio the tagged
ones **whose `needs` pass `workerGrants`** — the same authority that prunes
the thunks prunes their wrappers, so "what isn't granted doesn't exist"
(api.md §14) also holds at the Lua layer: a worker without the `http` cap has
no `enu.http`, not even as a table, and detecting surface by existence (the
one that shields subagent isolation, agente.md §9) remains reliable. The
seven [W] wrappers cross over (`log`, `re.compile`, `text.*`, `proc.spawn`,
`ws.connect`, `http.stream`, `search.grep`); `fs.watch` stays
main-thread-only with the unmarked variant, as the problem's note demanded.
Construction **uncovered a second layer of the same crack**: **handle
methods** (`Re:match`, `Proc:read_line`, `GrepIter:next`...) didn't cross
over either — `registerHandleDispatch` started the worker pool with an empty
method map, so even with the wrappers copied every handle was unusable; the
parent's map is copied whole, unpruned (what's unreachable is inert: a method
only gets dispatched on a handle already created by a granted thunk of the
instance itself). **Doesn't touch `api.md`**
(APILevel intact): §16 now holds exactly as written. Shielding 🔒:
`worker_g45_test.go` (parity with §16's table from inside a worker,
wrappers working end to end, and pruning by caps for the wrappers too).

**Problem.** `api.md` §16 declares available in workers ([W]) `re`, `ws`, `search`, `log`, `proc`, `http`, and `text` in full, but a good part of that surface isn't catalog thunks but **Lua wrappers** registered with `Pool.AddPreludio` (`enu.log.*`, `enu.re.compile`, `enu.text.wrap/markdown/highlight/diff`, `enu.proc.spawn` and its methods, `enu.ws.connect`, `enu.http.stream`, `enu.search.grep`). `spawnWorker` (`vmwasm/worker.go:137-179`) copies the modules and the registry's primitives but **never `extraPreludio`**: the worker's preludio runs without those wrappers and the modules end up absent (empirically verified: the six tested, `nil`). The host thunks do cross over; exactly the wrapper layer is missing. Note: the `enu.fs.watch` wrapper also lives in `extraPreludio` but watch is NOT [W] — the solution must discriminate, not copy in bulk.

**Impact.** Every plugin that follows §16 and moves heavy work to a worker (the central use case for workers: search, rendering, subagents) blows up with `attempt to index a nil value` when touching any of those modules. The sacred surface's promise is broken in the code.

**Options.** (a) **Worker-safe mark per preludio:** `AddPreludio` gains a variant/option that tags the fragment as [W] (`log`, `re`, `text`, `proc`, `ws`, `http.stream`, `search.grep` yes; `fs.watch`, ui no), and `spawnWorker` copies the tagged ones — the `caps` gating still happens via `workerGrants` on the underlying thunks (a wrapper without its thunk fails with the cap error, consistent). (b) **Downgrade §16** by removing the [W] marker from those modules — breaks the spec's promise and cripples workers; discardable except in an emergency. (c) **Move the wrappers to the Pool's base preludio** (shared by main thread and workers) — doesn't discriminate `fs.watch` nor future main-thread-only wrappers without adding a mark anyway, which is option (a). Recommendation: (a), with a parity test that walks §16's table from inside a worker.

## G46 · `resume`'s replay ignores `event` entries: live changes persisted are lost on resume — `sesiones.md` §3 / `agente.md` §2 (G18/G19 tension) — **RESOLVED**

**Resolution** (2026-07-13; option (a) **plus (c)** — the registry's full
recommendation—, built the same day in the `agent` extension,
`G46 (extension)` row of the [implementacion.md](implementacion.md) logbook).
The G18/G19 tension is closed by declaring **explicit precedence** in
[agente.md](agente.md) §2: **resume opts > transcript `event` >
`agent.toml`** — `opts` remain ephemeral (G18) but only override the
transcript *when given*; when they're silent, what's recorded governs. The
replay of `agent.session{resume=...}` reapplies the agent's `event`s: the
repeatable ones (`set_model`, `set_thinking`) with last-wins (the rule in
sesiones.md §3, whose canonical example stops being dead letter), and the
cumulative ones (`allow`/`deny`) **reapplied in order** over the base policy,
with hot-change semantics (idempotent) and without re-persisting — losing a
safety lever on resume is surprising, so no opts overrides them. The `event`s
are re-read from the **entire** transcript, not from the last `compact` (compaction
resumes messages, not configuration; noted in sesiones.md §3). If the recorded
model no longer resolves, resuming fails with `EPROVIDER` on open — better
than on the first turn—; the escape hatch is an explicit `opts.model`, which
has precedence. No kernel or `api.md` changes. Shielding: `agent_g46_test.go`
(precedence in both directions, last-wins with repeated changes, allow/deny
reapplied without duplication).

**Problem.** `Session:set_model`/`set_thinking`/`allow`/`deny` persist `event`
entries in the transcript, and `sesiones.md` §3 defines an explicit replay
rule for them ("for repeatable data… the last one wins", with the model
change as the canonical example). But `agent.session{resume=...}`'s replay
(`agent/init.lua`) only reconstructs `message` and `compact`: the `event`s are
received from the store and discarded. A session that changed model on the
fly reverts to the `opts`/`agent.toml` model on resume, with no warning. The
crack carries a prior spec tension: G18 declared `opts` **ephemeral** (reapplied
on every resume), which for the model case collides head-on with sesiones.md
§3's last-wins — the precedence needs deciding, not just implementing.

**Impact.** Resuming lies: the session doesn't continue "where it was" as far
as model/reasoning goes. For `allow`/`deny` (cumulative, not covered by the
last-wins rule) replay doesn't reapply them either, though their resume
semantics weren't even specified.

**Options.** (a) Explicit precedence `resume opts > transcript event > agent.toml`:
replay applies the repeatable `event`s (`set_model`, `set_thinking`) unless
`resume` brings the explicit option — resolves the G18/G19 tension by
declaring opts are ephemeral *when given*, and the transcript governs when
they're silent. (b) Downgrade sesiones.md §3: `event`s are just an auditable
record and resume doesn't apply them — honest about the current code, but
turns the spec's canonical example into dead letter. (c) (a) plus defined
semantics for the cumulative ones: replay also re-applies `allow`/`deny` in
order. Recommendation: (a) now, and decide (c) in the same resolution
(live `allow`/`deny` are a safety lever; losing them on resume is surprising).
Touches `agente.md` §2, `sesiones.md` §3, and the extension's replay.

## G47 · `api.md` §1.5 promises universal `opts.timeout_ms` and doesn't define the value 0, which today diverges between modules — `api.md` §1.5/§5/§6/§8 — **RESOLVED**

**Resolution** (applied in [api.md](api.md) §1.5; option (a)). The promise
is **scoped to the signatures that list it** — `enu.proc.run`, `enu.http.request`,
`enu.http.stream`, `enu.ws.connect` —, which is what the code implements and
what §5-§8's own signatures always said: the universal phrasing in §1.5 was
the anomaly, not the code. And the boundary value is defined where it
exists: in `proc.run`, `0` (the default) means *unlimited* (a local process
can legitimately have no ceiling); in `http`/`ws` the deadline always exists
(default 30 000 ms) and `0` is `EINVAL` — a network request with no ceiling
isn't a supported case—. The divergence stops being silent: it's documented
semantics with its rationale. Adding `timeout_ms` to more signatures (e.g.
`enu.fs.*` over network mounts) remains a **future addition**, compatible (the
API grows only by addition); it isn't promised until it exists.

**Problem.** §1.5 flatly stated "Every function with IO accepts
`opts.timeout_ms` (throws `ETIMEOUT`)", but almost no IO primitive honors it
or lists it in its signature (`enu.fs.read(path)` doesn't even have an opts
table; `Proc:read/write/wait`, `Ws:send/recv`, `enu.search.*` don't either).
On top of that, the value `0` diverged undocumented: `proc.run` accepts it as
"unlimited" while `http.request`/`ws.connect` throw `EINVAL`. Found in the
full audit (A-24/A-30 of the report), verified against code and signatures.

**Impact.** A plugin author reading §1.5 would expect `ETIMEOUT` from a
`enu.fs.read` over a hung NFS mount (blocks forever) and portability of
`{timeout_ms=0}` across modules (it blows up or not depending on the
module).

**Options.** (a) Scope §1.5 to the real signatures + define the 0 per module
with its rationale (chosen). (b) Add `opts.timeout_ms` to every IO signature —
major spec and kernel surgery, with no real demand yet. (c) Unify the 0
(EINVAL everywhere, or unlimited everywhere) — breaks `proc.run` or opens
network requests with no ceiling.

## G48 · `EAGENT` is used in `chat.md`/`adr.md` (and in the extension) but `agente.md` never coins it — `agente.md` §10 — **RESOLVED**

**Resolution** (applied in [agente.md](agente.md) §10): the `agent`
extension **formally coins `EAGENT`** as its structured error code (form of
api.md §1.4, the same pattern with which providers.md §3 coins `EPROVIDER`):
errors of the engine itself —malformed `agent.toml`, `max_turns` exhausted, a
broken subagent— travel as `{ code = "EAGENT", message, detail? }`. The
implementation already threw it (`eagent()` in `agent/init.lua`,
`subagent_worker.lua`); `chat.md` §8 and ADR-017, which already cited it,
remain correct untouched. The defect was the missing coinage in the
normative contract.

**Problem.** `chat.md` §8 and ADR-017 list `EAGENT` among the errors
`agent.session` can throw, but the contract that defines `agent.session`
(agente.md) only mentioned `EINVAL` in the whole document: a reader of the
contract had no way to know that code exists or what it means. (A-29 of the
report.)

**Impact.** Whoever handled agent errors by `code` was coding against an
undocumented code.

**Options.** (a) Coin it in agente.md §10 (chosen: the implementation and
two documents already assumed it existed). (b) Remove it from chat.md/adr.md
and collapse to EINVAL — impoverishes diagnostics and rewrites an ADR,
against discipline.

## G49 · `chat.md` §5 teaches `agent.permission.respond(id, "once")`, which the real API interprets as DENIAL — `chat.md` §5 / `agente.md` §5 — **RESOLVED**

**Resolution** (applied in [chat.md](chat.md) §5): the example switches to
`agent.permission.respond(id, true)` and the prose clarifies that **"allow
once" and "always allow" grant the same** (`granted = true`); they only
differ in whether the pattern is additionally persisted (session policy /
global `agent.toml` via `persist_allow`). The defect was in the document: the
API (`respond(id, granted)`, boolean — `p.future:set(granted == true)`) and
the official UI were already correct; a third-party integrator following the
contract to the letter **denied believing they were granting**
(`"once" == true` is `false` in Lua). (A-31 of the report.)

**Problem/Impact/Options.** See title and resolution: a doc fix with no
reasonable alternative (changing the API to accept strings would break
existing boolean callers and add a second vocabulary for the same thing).

## G50 · ADR-002 remains "Accepted" with its implementation decision (gopher-lua / Lua 5.1) obsolete and without a replacement note — `adr.md` — **RESOLVED**

**Resolution** (applied in [adr.md](adr.md) ADR-002): a **status note** in
the style of ADR-011's — the core of the decision (Lua as the extension
language, versus Starlark/JS/WASM) **remains in force**; its realization
(gopher-lua, Lua 5.1, and the consequence "not thread-safe conditions
concurrency") was **replaced by ADR-019/ADR-020** (PUC-Lua 5.4 compiled to
WASM over wazero; the M17 retirement removed gopher-lua from the binary and
from `go.mod`). The body isn't rewritten (the ADR discipline); the note
keeps a reader of adr.md from seeing as current a baseline that api.md §1.2
already contradicts. (A-27 of the report.)

**Problem.** The same migration that led to marking ADR-011 "Replaced by
ADR-020" left ADR-002 unannotated, even though its implementation decision
became just as obsolete: an asymmetry in the registry's upkeep.

## G51 · `arquitectura.md`'s primitive inventory omits `enu.search` and the YAML codec — `arquitectura.md` / `api.md` §11-§12 — **RESOLVED**

**Resolution** (applied in [arquitectura.md](arquitectura.md), the kernel
table): the **io** row names the tree's parallel search (`files`/`grep`,
api.md §11) and the **data** row lists YAML alongside JSON and TOML (api.md
§12, needed for agente.md §6's skills). Whoever reads only the table as "the
inventory" no longer misses two modules of the sacred surface. (A-33 of the
report; the supposed omission of `enu.sys` was refuted in verification — it's
represented as "environment" in the io row.)

## G52 · `enu.ws` has no binary path: `Ws:send` always sends a text frame and `Ws:recv` doesn't distinguish the frame type — `api.md` §8 / `runtime/ws.go` — **RESOLVED**

**Resolution** (2026-07-14; addition to [api.md](api.md) §8, API level
2→3). `Ws:send(data, opts?)` gains `opts.binary?: boolean`: with it, the
frame goes out binary (`MessageBinary`); without it, the current behavior
(text frame) is preserved intact — compatible with every existing caller.
And `Ws:recv()` returns a **second value** `binary: boolean` that
distinguishes the incoming frame's type (current callers, who only take the
first value, notice nothing: a pure Lua addition). Autodetection (sending
binary when `data` isn't valid UTF-8) was discarded: a frame-type change
depending on *content* is fragile magic — the same program would send
frames of different types depending on the payload, and a strict consumer
on the other end would see an inconsistent protocol. The frame type is
protocol semantics and is declared by the sender. Implementation: kernel
(`ws.go` + wrapper), with tests citing A-38/G52.

**Problem.** `ws.go:148` hardwires `websocket.MessageText` into every
`send`: non-UTF-8 bytes → a conforming server closes with 1007 (RFC 6455
§5.6 requires UTF-8 in text frames), and `api.md` neither restricted `data`
to text nor offered an alternative. On receive, `recv` already delivered the
bytes of any frame (discards the `MessageType`), so an incoming binary frame
*worked* but was indistinguishable from text: a faithful proxy/echo was
inexpressible. Found in the full audit (A-38 of the report).

**Impact.** Any binary (or mixed) WS protocol was unusable from `enu`: MCP
over WS with compressed payloads, LSP/DAP binary framing protocols, or a
simple faithful relay.

**Options.** (a) `opts.binary` in `send` + second return value in `recv`
(chosen: explicit, minimal, backward-compatible). (b) Autodetection via the
payload's UTF-8 validity (discarded: frame type dependent on content).
(c) Per-connection mode in `ws.connect` (discarded: mixed protocols exist
and would force either two connections or a "raw" mode just as explicit).
