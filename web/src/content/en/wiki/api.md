# nu core API — v1 specification (draft)

Status: **draft for discussion**. Once frozen, this surface is the "sacred
API" (ADR-003): it only grows by addition. Everything not here (widget
toolkit, agent, MCP, providers) is an extension and versioned separately.

Conventions used in this specification:

- Signatures use the notation `enu.mod.fn(arg: type, opts?: table) -> type`.
- **⏸ suspends**: the function can only be called inside a task (coroutine);
  it yields control until completion and returns the result directly.
  Calling it outside a task is an error.
- **[W]**: available inside workers. Unmarked: main state only.

---

## 1. Cross-cutting conventions (ADR-009)

### 1.1 Namespace

The entire API lives under the global `enu`. `require` is reserved for
plugin modules and pure Lua libraries. Identifiers are in English,
`snake_case`.

### 1.2 Lua environment baseline

Lua 5.4 (PUC-Lua, compiled to WASM and running on the embedded runtime —
see [migracion-vm.md](archive/migracion-vm.md)). Available: `string`, `table`,
`math`, `coroutine`, `utf8`, `pairs/ipairs/pcall/error/load/...`.
**Disabled**: `io`, `os.execute`, `os.exit`, `os.remove`, `os.rename`,
`os.getenv`, `print` (redirected to `enu.log.info`), `dofile`/`loadfile`
outside the loader. Reason: all IO must go through the core's async
primitives; blocking IO from the stdlib would freeze the event loop.

`load(s)` is available (it compiles an IN-MEMORY string, no IO): it's what a
Lua REPL needs on top of the public API (see the `repl` extension).
Migration note 5.1→5.4: `loadstring` disappears (`load` accepts the string
directly), `unpack` becomes `table.unpack`, `setfenv`/`getfenv` don't exist
(the environment is `_ENV`), and the incomplete input a REPL detects is
marked with `<eof>` in the error message (previously `at EOF`). The
official extensions' code assumes the 5.4 baseline.

### 1.3 Async model

- Main state is single-threaded with an event loop (ADR-004).
- A **task** is a coroutine managed by the scheduler. Inside a task, ⏸
  functions are written in sequential style (implicit await).
- **Synchronous handlers** (input, events) run on the loop and cannot call
  ⏸ functions; to do IO, they spawn a task with `enu.task.spawn`.
- **Watchdog**: every continuous Lua execution *slice* (between two
  suspension points) has a budget, 100 ms by default (configurable in
  `enu.toml`). Exceeding it aborts the task and emits
  `core:plugin.misbehaved`.
- **Cancellation and aborts are NOT catchable.** `Task:cancel()` and the
  watchdog abort the task at its next suspension point (or slice) by
  **unwinding the stack without going through `pcall`** — if they were
  normal errors, any `pcall` in the ecosystem would catch them and the
  program would carry on as if nothing happened. To release resources no
  matter what, register `enu.task.cleanup(fn)`. `ECANCELED` is reserved for
  *observing* cancellation (e.g. in the result of `Task:await`), not for
  catching it.

### 1.4 Errors

Core functions **throw** (via `error()`) structured tables:

```
{ code: string, message: string, detail?: any }
```

Reserved v1 codes: `ENOENT`, `EEXIST`, `EACCES`, `EIO`, `EHTTP`, `ENET`,
`ETIMEOUT`, `ECANCELED`, `EBUDGET`, `EINVAL`, `ECLOSED`. They're caught with
`pcall` — with two exceptions: `ECANCELED` and `EBUDGET` name the
non-catchable aborts from §1.3 (cancellation and watchdog, respectively)
and only serve to *observe* them, e.g. in the result of `Task:await`.
Extensions coin their own codes with the same shape, outside this reserved
list (e.g. `EPROVIDER`, [providers.md](providers.md) §3). Reason versus
the `res, err` style: structured errors compose better across layers of
extensions and are never silently ignored.

### 1.5 Units and common types

Times in **milliseconds**. Paths as UTF-8 strings. The IO deadline is
`opts.timeout_ms` (throws `ETIMEOUT`) **in the signatures that list it** —
`enu.proc.run`, `enu.http.request`, `enu.http.stream`, `enu.ws.connect`—; the
rest of v1's IO does not accept a deadline (G47 — extending it to more
signatures is a future compatible addition, not a current promise). The
boundary value is defined wherever the deadline exists: in `proc.run`, `0`
(the default) means *no limit* — a local process may legitimately have no
ceiling—; in `http`/`ws` the deadline always exists (default 30,000 ms) and
`0` is `EINVAL` — there's no network request without a ceiling—. Core
handles (Task, Region, Proc...) are opaque userdata with methods.

---

## 2. `enu` (root)

| Signature | Semantics |
|---|---|
| `enu.version -> {major, minor, patch, api: integer}` [W] | Runtime version and API level. |
| `enu.has(cap: string) -> boolean` [W] | Capability detection (`"ui"`, `"ui.images"`, `"net.tcp"`, ...) for portable extensions. Also covers entire modules: in headless mode `enu.ui` doesn't exist (§9). |

---

## 3. `enu.task` — scheduler [W]

| Signature | Semantics |
|---|---|
| `enu.task.spawn(fn, ...) -> Task` | Spawns a task; extra arguments are passed to `fn`. |
| `enu.task.sleep(ms)` ⏸ | Suspends the current task. |
| `enu.task.all(fns: Task[]\|fn[]) -> any[]` ⏸ | Waits for all of them; if one throws, it cancels the rest and rethrows. Results are returned **aligned with the inputs** (`out[i]` is the one for `fns[i]`), never in completion order (G27) — this is what lets you correlate result with input in a fan-out without hand-carrying the index. |
| `enu.task.race(fns) -> (winner_index, result)` ⏸ | First to finish wins; cancels the rest. |
| `enu.task.every(ms, fn) -> Timer` | Periodic timer (synchronous handler). `Timer:stop()`. |
| `enu.task.defer(fn)` | Runs `fn` on the next loop tick. |
| `enu.task.future() -> Future` | Single-use rendez-vous: `Future:set(v)` (synchronous, one time only; later calls throw `EINVAL`) and `Future:await() -> v` ⏸ (several can wait; if already resolved, returns immediately). This is the piece for "one task waits for a value some other code will produce" (dialogs, pickers, proxies) without polling. |
| `Task:cancel()` | Cooperative cancellation: aborts the task at its next suspension point (not catchable, §1.3); its `cleanup`s run. |
| `enu.task.cleanup(fn)` [W] | Registers a (synchronous) releaser on the current task's LIFO stack; all of them run on termination — success, error, or abort. The `defer` of this house: processes, regions, input handlers. |
| `Task:await() -> any` ⏸ | Waits for another task's result. |

---

## 4. `enu.events` — event bus

The core doesn't know what an agent is: this generic bus is where
extensions define their own hooks (e.g. the official agent extension emits
`agent:tool.start`; its middleware hooks like `tool.pre` go through their
own registry, not the bus — [agente.md](agente.md) §4). Naming convention:
`"namespace:event"`, at **two levels** (G26). The core reserves only its
own — `core:` and `ui:`, the surfaces the kernel itself emits. Any other
namespace belongs to a plugin by convention (namespace = its name); since
the loader guarantees a plugin's name is unique (§14), two extensions
can't collide. The official ones have no privilege here: `agent:` is the
namespace of the `agent` plugin just like `my-plugin:` is yours — the core
doesn't reserve it (it doesn't know `agent` exists, ADR-003).

| Signature | Semantics |
|---|---|
| `enu.events.on(name, fn) -> Sub` | Subscribes. Synchronous handlers, in registration order, each under `pcall` (ADR-008). `Sub:cancel()`. |
| `enu.events.once(name, fn) -> Sub` | Once only. |
| `enu.events.emit(name, payload?)` | Synchronous dispatch on main state. |

Dispatch semantics (G10): each `emit` runs over the **snapshot** of
subscribers taken at emit time; canceling a subscription takes effect
immediately (if your turn hasn't come yet, you no longer run); subscribers
added during a dispatch only see future events; nested `emit`s **get
queued** and are dispatched once the current one finishes (breadth, not
depth — no recursion or overflow; an infinite ping-pong between plugins
becomes a flat loop that the watchdog cuts off).

Events the core emits: `core:ready`, `core:shutdown`,
`core:plugin.loaded`, `core:plugin.unload`, `core:plugin.error`,
`core:plugin.misbehaved`, `ui:resize`, `ui:focus`,
`ui:suspend`/`ui:resume`.

---

## 5. `enu.fs` — filesystem [W]

| Signature | Semantics |
|---|---|
| `enu.fs.read(path) -> string` ⏸ | Reads the entire file. |
| `enu.fs.write(path, data, opts?)` ⏸ / `enu.fs.append(path, data)` ⏸ | Atomic write (write via temp file + rename). `opts.exclusive = true` (G17): creates **only if it doesn't exist**, in a single indivisible operation (`O_EXCL` — there's no temp+rename here: rename would overwrite); if the file already exists it throws `EEXIST`. This is the piece for lockfiles ([sesiones.md](sesiones.md) §6). |
| `enu.fs.stat(path) -> {size, mtime_ms, is_dir, mode}?` ⏸ | `nil` if it doesn't exist (doesn't throw `ENOENT`). |
| `enu.fs.list(dir) -> {name, is_dir}[]` ⏸ | Non-recursive; for recursive see `enu.search.files`. |
| `enu.fs.mkdir(path)` ⏸ / `enu.fs.remove(path, opts?)` ⏸ / `enu.fs.rename(from, to)` ⏸ / `enu.fs.copy(from, to)` ⏸ | `remove` requires `opts.recursive=true` for non-empty directories. |
| `enu.fs.tmpdir() -> string` ⏸ | The session's own temporary directory. |
| `enu.fs.cwd() -> string` [W] | Working directory (immutable during the session; subprocesses can receive a different one via `opts.cwd`). |
| `enu.fs.watch(path, opts?, fn) -> Watcher` | `opts`: `recursive?`, `gitignore = true` (ignores what git ignores: watching `node_modules/` is noise), `debounce_ms = 50`. Delivers **in batches**: `fn(events[])` with `{path, kind: "create"\|"modify"\|"remove"}` — a `git checkout` that touches thousands of files arrives as a single batch (G7). Synchronous handler. `Watcher:stop()`. Main state only. |

---

## 6. `enu.proc` — subprocesses [W]

| Signature | Semantics |
|---|---|
| `enu.proc.run(argv: string[], opts?) -> {code, stdout, stderr}` ⏸ | Convenience with buffers. `opts`: `cwd`, `env`, `stdin`, `timeout_ms`. No implicit shell: `argv` is an array; whoever wants a shell invokes it explicitly. |
| `enu.proc.spawn(argv, opts?) -> Proc` | Fine-grained control with streams. |
| `Proc:write(data)` ⏸ / `Proc:close_stdin()` | Streaming stdin. |
| `Proc:read_line(which: "stdout"\|"stderr") -> string?` ⏸ | `nil` on EOF. |
| `Proc:read(which, n?) -> string?` ⏸ | Raw read. |
| `Proc:wait() -> {code}` ⏸ / `Proc:kill(signal?)` | `signal` defaults to TERM. |
| `enu.proc.alive(pid: integer) -> boolean` | Is there a live process with that `pid` on this machine? (G17). Reports **existence, not identity** — a recycled pid gives `true`. For detecting orphaned locks ([sesiones.md](sesiones.md) §6). |

Process lifetime: the rule is to kill it explicitly via `enu.task.cleanup`
in whoever creates it; as a safety net, a `Proc` with no references
eventually gets killed by the GC (non-deterministic — don't rely on it).

---

## 7. `enu.sys` — environment and clock [W]

| Signature | Semantics |
|---|---|
| `enu.sys.platform() -> "linux"\|"darwin"\|"windows"` | |
| `enu.sys.env(name) -> string?` / `enu.sys.setenv(name, value)` | `setenv` only affects future subprocesses. |
| `enu.sys.now_ms() -> number` / `enu.sys.mono_ms() -> number` | Wall clock / monotonic clock. |
| `enu.sys.hostname() -> string` | Machine name (G17; content of session locks, [sesiones.md](sesiones.md) §6). |
| `enu.sys.pid() -> integer` | Pid of the current `enu` process (local query, like `hostname`/`now_ms`). Together with `hostname` it forms the **writer identity** of session locks (G32; [sesiones.md](sesiones.md) §6). Different from `enu.proc.alive(pid)`, which validates *other* pids: `pid()` is your *own*. |

---

## 8. `enu.http` and `enu.ws` — networking [W]

Response streaming is first-class (ADR-005: provider adapters live in Lua
and consume SSE).

| Signature | Semantics |
|---|---|
| `enu.http.request(opts) -> {status, headers, body}` ⏸ | `opts`: `url`, `method?`, `headers?`, `body?`, `timeout_ms?`, `tls?`, `proxy?`, `max_redirects?` (per-request TLS/proxy, see note G12 below; redirects, note G54). Buffered response. Doesn't throw on status >= 400 (status is data); throws `ENET`/`ETIMEOUT` on transport failures. |
| `enu.http.stream(opts) -> Stream` ⏸ | Returns upon receiving headers: `Stream.status`, `Stream.headers`. `opts.timeout_ms` covers up to the headers; `opts.idle_timeout_ms?` throws `ETIMEOUT` if N ms pass without receiving body bytes (an SSE can go silent forever). It also accepts `opts.max_redirects?` (note G54 below). |
| `Stream:chunks() -> iterator` ⏸ | Raw body chunks as they arrive. |
| `Stream:events() -> iterator` ⏸ | Built-in SSE parser: iterates `{event?, data, id?}`. |
| `Stream:close()` | Aborts the connection. |

Backpressure: streams are buffered in Go while Lua consumes at its own
pace; the buffer has a limit and exceeding it fails the stream with `EIO`.

TLS and proxy (G12): `request` and `stream` accept
`opts.tls = { ca_file?, insecure? }` (per-request corporate CA);
`HTTP_PROXY`/`HTTPS_PROXY`/`NO_PROXY` from the environment are respected
by default. Global defaults in the `[net]` section of `enu.toml` (`ca_file`,
proxy), overridable per request.

Redirects (G54): `request` and `stream` accept `opts.max_redirects?: number` —
the budget of redirects the client follows automatically. Default **10** (the
policy the client used to apply implicitly becomes contract); `0` = follow
none. When the budget runs out **no error is thrown**: the last `3xx`
response is delivered **as data** — consistent with "the status is data" —
with its `location` in `headers`; whoever needs to observe or validate the
chain hop by hop passes `0` and follows it by hand (a `302` towards
`169.254.169.254` must not be able to evade the validation performed on the
initial URL). And on every **cross-host** hop the client **strips every
header the caller set in `opts.headers`** before resending the request. The
exact rule: a hop is cross-host if the host of the destination URL (name and
port) differs from that of the initial `opts.url`, **or** if the scheme
downgrades from `https` to `http` even when the host is preserved (the
header would travel in the clear over an interceptable channel); once
stripped, headers are **not restored** even if a later hop returns to the
initial host — the chain went through a third party and is no longer
trusted. *Everything* from the caller is stripped, with no allowlist of
"safe" headers, on top of what the client already stripped across domains
(`Authorization`, `Cookie`): credentials increasingly live in custom headers
(`x-api-key`, `x-goog-api-key`) no denylist would know, and a different
destination is a different party that doesn't inherit what the caller told
the first one.
| `enu.ws.connect(url, opts?) -> Ws` ⏸ | `Ws:send(data, opts?)` ⏸ — `opts.binary?: boolean` sends a **binary** frame; without it, a text frame (the protocol requires valid UTF-8 in text: arbitrary bytes go with `binary`, or a conformant server closes with 1007) (G52). `Ws:recv() -> data: string?, binary: boolean` ⏸ (`nil` on close; the second value distinguishes the incoming frame type) (G52). `Ws:close()`. |

Reserved for the future (not v1): `enu.net.tcp`.

---

## 9. `enu.ui` — cells, regions, and compositor

Main state only (ADR-008). The compositor, diffing, and painting live in
Go; changes are coalesced and painting happens at most every ~30 ms
(ADR-007). There's no manual "flush".

**Headless (G20)**: without an interactive TTY (`enu -e`, CI, redirected
output), the `enu.ui` module simply **doesn't exist** — the same model as
worker `caps`: ungranted surface isn't there. Detection is `enu.has("ui")`,
never try-and-catch.

### 9.1 Surface

| Signature | Semantics |
|---|---|
| `enu.ui.size() -> {w, h}` | Terminal size in cells. Changes → `ui:resize` event. |
| `enu.ui.region(opts) -> Region` | `opts`: `x, y, w, h, z?`. Regions are the unit of composition: rectangles with z-order owned by whoever creates them. **Resize (G1)**: a region fully or partially off-screen gets clipped without error (it never paints out of bounds; if nothing fits, nothing is painted); its coordinates aren't touched — if the screen grows again, it reappears as-is. Repositioning is the owner's responsibility (convention "your region, your `ui:resize`"); automatic relayout is the toolkit's job, not the core's. |
| `Region:blit(x, y, block: Block)` | Stamps a pre-rendered block (see `enu.text`) at local coordinates of the region. **Clips on both ends (G28)**: `x/y` can be **negative** and clip the block's *starting* edge (`blit(0, -3, doc)` shows `doc` starting from its fourth row), just as excess clips the end — a **viewport** over a Block bigger than the region, where *scroll = re-blit with a different offset*. It's **copy, never re-render**: blitting the same Block with a different offset recomputes nothing (the cost of scrolling is that of a copy of the visible window). Virtualization (not building the entire Block for huge histories) belongs to the toolkit, not the core. |
| `Region:fill(style?)` / `Region:clear()` | |
| `Region:move(x, y)` / `Region:resize(w, h)` / `Region:raise()` / `Region:lower()` | |
| `Region:show()` / `Region:hide()` / `Region:destroy()` | |
| `Region:cursor(x, y \| nil)` | Places the terminal's real cursor (or hides it with `nil`). Only one region can hold it; the last call wins. |

### 9.2 Blocks and styles

A **Block** is an opaque handle to styled lines, produced by `enu.text.*`
or built by hand. It has `.width` and `.height`.

| Signature | Semantics |
|---|---|
| `enu.ui.block(lines: (string\|Span[])[]) -> Block` | Manual construction. A `Span` is `{text, style?}`. |
| `Style` | Table `{fg?, bg?, bold?, italic?, underline?, reverse?}`; **literal** colors: `"#rrggbb"` or index 0-255 (rendering degrades them to whatever the terminal supports, `enu.ui.caps().colors`). Semantic names (`"accent"`, `"error"`, ...) **aren't the core's**: they're vocabulary of the toolkit's theme, which resolves them to literals when building Blocks (G22). |
| `enu.ui.caps() -> {colors, kitty_keyboard, mouse, images}` | Terminal capabilities. |
| `enu.ui.clipboard_set(s)` / `enu.ui.clipboard_get() -> string?` ⏸ | Via OSC 52 when the terminal supports it. |

### 9.3 Input

Stack model: input flows to the topmost handler; whoever doesn't consume
it lets it pass through. Fine-grained focus routing is the toolkit's
(extension's) job, not the core's.

| Signature | Semantics |
|---|---|
| `enu.ui.on_input(fn) -> InputHandle` | Pushes a synchronous handler `fn(ev) -> boolean` (true = consumed) onto the stack. `ev`: `{type: "key"\|"mouse"\|"paste", key?, mods?, x?, y?, text?, path?}`. `InputHandle:pop()`. |
| `enu.ui.keymap(seq: string, fn, opts?) -> Keymap` | Sugar over the stack: `seq` in `"ctrl+k"`, `"alt+enter"` notation, `"g g"` sequences. `Keymap:unmap()`. Sequence resolution with a timeout in the core. Conflicts: the stack rules — the most recently registered active one wins (and the user's `init.lua` loads last, §14). **Consumption:** a keymap consumes the key by default (triggering the shortcut counts as handling it); its `fn` can return EXPLICIT `false` to **yield** the key so it keeps going down the stack (used by chat to step aside for `esc`/`enter` when a modal is open and the key should reach the focused widget). |

Pasting an image (G30): when the clipboard carries **non-text** content
(an image), the core dumps it to a temporary session file
(`enu.fs.tmpdir`) and delivers the `paste` event with `path` (the dumped
path) instead of `text`. The UI inserts that path just like an `@` mention
and the agent decides whether to read it (content isn't blindly
embedded); this way binary bytes never cross the text/JSON boundaries
(consistent with G11, §12). Painting the image on screen is a separate
matter ([pospuesto.md](pospuesto.md) P6).

---

## 10. `enu.text` — rendering and processing [W]

Quadratic-on-screen operations live here, in Go (ADR-004/007).

| Signature | Semantics |
|---|---|
| `enu.text.width(s) -> integer` | Width in cells (graphemes, east-asian, emoji). |
| `enu.text.wrap(s, width, opts?) -> Block` | Word-wrap; `opts.style?` (a Style §9.2) applies to each line. |
| `enu.text.truncate(s, width, opts?) -> string` | With optional ellipsis. |
| `enu.text.markdown(s, opts) -> Block` | Full markdown rendering at `opts.width`, themable. Accepts incomplete input (streaming-safe). |
| `enu.text.highlight(code, lang, opts?) -> Block` | Syntax highlighting. |
| `enu.text.diff(a, b, opts?) -> {hunks, block?}` | Structured diff; `opts.render=true` additionally returns the painted Block. |
| `enu.re.compile(pattern) -> Re` | RE2 regex. `Re:match(s) -> caps?`, `Re:find_all(s) -> ranges`, `Re:replace(s, repl) -> string`. |

Note (G23): there's no LLM token estimation here — "token" is product
vocabulary, and the heuristic (~4 bytes/token) is a pure-Lua division that
doesn't justify a primitive ("Lua decides, Go executes"). It lives in the
providers extension: `providers.approx_tokens` ([providers.md](providers.md) §4).
This module's concessions (markdown, highlighting) stay because
performance justifies them; that one didn't.

---

## 11. `enu.search` — repo-scale search [W]

| Signature | Semantics |
|---|---|
| `enu.search.files(root, opts?) -> string[]` ⏸ | Recursive listing respecting `.gitignore`. `opts`: `glob`, `hidden`, `max`. |
| `enu.search.grep(pattern, opts) -> iterator` ⏸ | Parallel internally; iterates `{path, line_no, line, ranges}` as they arrive. `opts`: `root`, `glob`, `case`, `max`. |
| `enu.search.fuzzy(query, candidates: string[], opts?) -> {index, score}[]` | Ordered fuzzy matching, for pickers. Synchronous and bounded (it's the picker's hot primitive). |

---

## 12. `enu.json` / `enu.toml` / `enu.yaml` — codecs [W]

| Signature | Semantics |
|---|---|
| `enu.json.encode(v, opts?) -> string` / `enu.json.decode(s) -> v` | `opts.pretty`. `null` ↔ `enu.json.NULL` (sentinel) so keys aren't lost. **Strict about UTF-8** (G11): `encode` throws `EINVAL` on invalid bytes — sanitizing is a visible decision made by whoever has context (the tool), never the codec. |
| `enu.toml.encode(v) -> string` / `enu.toml.decode(s) -> v` | |
| `enu.yaml.encode(v) -> string` / `enu.yaml.decode(s) -> v` | Needed for metadata from the existing ecosystem (skill frontmatter); YAML is too treacherous to parse in pure Lua. |

---

## 13. `enu.worker` — opt-in parallelism (ADR-008)

| Signature | Semantics |
|---|---|
| `enu.worker.spawn(module: string, opts?) -> Worker` | Spins up a new Lua state in its own goroutine, loading `module` (resolvable by the loader). The loader's `require` paths (plugin Lua modules) are available inside the worker; what doesn't exist is the `enu.plugin` API (lifecycle). No `enu.ui`, no `enu.events` (main bus), no nested workers. `opts.caps?: string[]` restricts the worker's API to what's listed, with **two granularities** (G6): `"fs"` grants the entire module; `"fs.read"` grants a specific function. What's not granted **doesn't exist** inside the state — capability-based sandboxing; functions added to the API in the future are never granted by old lists (deny-by-default for new surface). Without `caps`, the worker gets the entire [W] API. Named packages (e.g. read-only): tables from the agent extension (`agent.caps.*`), not the core. |
| `Worker:send(msg)` ⏸ / `Worker:recv() -> msg` ⏸ | Messages = JSON-able values, **copied** (tables don't cross states). Closures, userdata, and Blocks don't cross either: a worker sends digested data and main state renders. Queues are **bounded**: `send` suspends if full (backpressure, consistent with §8) — from a synchronous handler, `task.spawn` as always. |
| `Worker:on_message(fn) -> Sub` | Callback-based alternative on main state. **Mutually exclusive with `recv`** (G8): registering one while the other is pending (or vice versa) throws `EINVAL` on the spot — never silent priority. |
| `Worker:terminate()` | Immediate and safe (isolated states). |
| *(inside the worker)* `enu.worker.parent.send(msg)` ⏸ / `...recv() -> msg` ⏸ | Channel with main state; same bounded queues. |

Inside a worker (G15): each worker is a **complete mini-runtime** — its own
scheduler, multiple tasks, timers, and futures (all `enu.task` [W]). **No
watchdog**: workers exist precisely to burn CPU freely; control is
`terminate()` from the main state plus `caps`.

---

## 14. `enu.plugin` and the loader

A plugin is a directory with `plugin.toml` (`name`, `version`,
`requires?: string[]`) and `init.lua`, which runs on load. The plugin's
`lua/` directory is added to `require` paths (so plugins can require each
other: composability from ADR-008). Official embedded extensions
(`go:embed`) load first and are replaceable by name from the user
directory. **Name is identity** for a plugin, and the loader keeps it
unique: the user directory *replaces* the embedded one of the same name
(they don't coexist), and two plugins with the same name are an
actionable load error. That uniqueness is what lets event namespaces (§4)
and other registries stay collision-free by simple convention (namespace
= plugin name), without the core reserving any extension name (G26).

**Runtime configuration**: `config.dir()/enu.toml` governs the core
itself — plugin activation (embedded official extensions are **inactive
by default**, ADR-010; the first launch offers to enable the **official
product set** —the embedded ones minus the `example` scaffold plugin,
ADR-015), extra plugin paths, watchdog budget.

**Bare runtime screen (G21)**: with an interactive TTY and no plugin
active, the kernel paints a fixed screen made only of its own
capabilities — version and API level, config and plugin paths, available
embedded extensions — and its actions: enable the official set (writes
`plugins.enabled` and continues the canonical startup, no network), enable
individual extensions (e.g. just `repl`), or quit. This isn't a product's
UI but the runtime's: embedded extensions and their activation are loader
capability, so the kernel talks about its own business
([filosofia.md](filosofia.md) §2) — fixed render, pre-Lua, no widgets or
logic. It's what you see whenever nu starts with nothing active, not a
first-time dialog. Without a TTY there's no screen: it starts bare, and
errors from an inactive extension are actionable (they name the
`enu.toml` line that fixes it, like the permission errors in
[agente.md](agente.md) §5). The no-TTY onramp (CI, Docker, scripts) is the
CLI flag `nu --default-config` (ADR-015, G33): it writes that same product
set to `enu.toml` —and active `agent.toml`/`providers.toml` templates if
they don't exist, so the harness is left usable, ADR-017/G35— and exits,
or —combined with `-p`/`-e`— activates it only for that process without
touching disk. This is CLI surface of the binary, not sacred API: it adds
nothing to `enu.*` and doesn't move `enu.version.api`.

**Canonical startup order**: core → activated plugins (topological by
`requires`) → user's `init.lua` → `core:ready` event. The user's init goes
**last** on purpose: just as the most recently registered handler wins on
the input stack, the user has the last word (keymaps, theme, overrides)
by construction, with no priority system.

| Signature | Semantics |
|---|---|
| `enu.plugin.current() -> {name, version, dir}` | Plugin in whose context the code runs. |
| `enu.plugin.list() -> {name, version, source: "builtin"\|"user", enabled}[]` | |
| `enu.plugin.reload(name)` ⏸ | Development tool, **best-effort** (G2): releases all of the plugin's handles (the core tags them by owner via `plugin.current()`), emits `core:plugin.unload` (extensions clean up their registries: tools, commands...), clears the plugin's `require` cache, and reloads its `init.lua`. A plugin with exotic global effects might not unload cleanly — for iterating, not for production. |
| `enu.config.dir() -> string` [W] / `enu.config.data_dir() -> string` [W] | `~/.config/enu` and `~/.local/share/enu` (or platform equivalents). |

---

## 15. `enu.log` [W]

| Signature | Semantics |
|---|---|
| `enu.log.debug/info/warn/error(fmt, ...)` | To a file in `data_dir`, annotated with the originating plugin. `print` is an alias for `info`. Never to the screen: UI belongs to extensions. |

---

## 16. Worker availability summary

| Available [W] | Main state only |
|---|---|
| `task`, `fs` (except `watch`), `proc`, `sys`, `http`, `ws`, `text`, `re`, `search`, `json`, `toml`, `yaml`, `log`, `config.dir`, `config.data_dir` | `ui`, `events`, `fs.watch`, `worker.spawn`, `plugin` |

---

## 17. Stability and evolution

- Freezing v1 = freezing **this document**: signatures and semantics only
  change by addition; `enu.version.api` increments with each addition.
  **Current level: `api = 4`** — level 1 was the initial freeze;
  `enu.sys.pid()` (G32) bumped it to 2; `enu.ws`'s binary frames (G52:
  `opts.binary` in `Ws:send`, second return value of `Ws:recv`) bumped it
  to 3; `enu.http`'s redirect control (G54: `opts.max_redirects` in
  `request`/`stream` and header stripping on cross-host hops) bumped it
  to 4. An addition never breaks existing signatures: code written against
  level 1 remains valid at subsequent levels.
- Capability detection with `enu.has()`, never version sniffing.
- `core:`/`ui:` event namespaces and the error codes from §1.4 are
  reserved.
- Out of this specification (deliberately): widget toolkit, agent hooks
  (`agent:*`), MCP, `providers.toml` format. These are contracts of their
  own extensions, versioned separately. The providers one already has a
  draft: [providers.md](providers.md).
