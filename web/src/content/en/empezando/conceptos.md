---
title: Key concepts
description: nu's mental model — minimal kernel, extensions, the browser concurrency model, workers, and the sacred API.
---

This page gathers the cross-cutting concepts that show up again and again in
the reference. If you understand these five, the rest of the manual falls
into place on its own.

## 1. The core doesn't know what an agent is

`nu`'s kernel only knows **its own capabilities**: primitives (runtime, IO,
network, terminal UI), the plugin loader, and its embedded extensions. The
agent loop, the chat, slash commands, MCP, LLM providers: **it's all Lua
extensions**, including the official ones, with no architectural privilege.

The yardstick for any doubtful case: if something is fully described with
kernel vocabulary (plugins, paths, versions), it belongs to the kernel; if it
needs product vocabulary (agent, chat, tools, token), it belongs to an
extension.

**Corollary:** if an official feature can't be built with the public API,
the API is incomplete —and the fix goes into the API, not into a privileged
shortcut—. That's what keeps the API honest.

## 2. The core API is sacred

The whole API lives under the `nu` global, with identifiers in English and
`snake_case`. It's deliberately **small and boring**, and it **grows only by
addition**: a signature never changes or disappears. Every addition bumps
`nu.version.api` (the current level is `2`).

That's why you detect capabilities with
[`nu.has()`](/nu/en/api/nu/), never by comparing version numbers:
`nu.has("ui")` tells you whether there's a terminal, without your plugin
breaking when the API grows.

## 3. The "browser" concurrency model

`nu` takes its concurrency model from the browser and from Luau:

- **A single-threaded main state** with an event loop. Deterministic: there
  are no data races between your Lua code.
- **Tasks**: coroutines managed by the scheduler. Inside a task, suspending
  functions (⏸) are written in **sequential** style, with implicit *await*
  —no callbacks, no explicit promises—. IO lives here.
- **Synchronous handlers** (input, events): run on the loop and **cannot**
  call ⏸ functions; to do IO, they launch a task with `nu.task.spawn`.
- **Go primitives, parallel on the inside**: search, diff, markdown,
  highlighting, and HTTP streaming are native and take advantage of multiple
  cores without you managing threads.

```lua
-- Sequential style inside a task: no callbacks at all.
nu.task.spawn(function()
  local cfg = nu.fs.read("config.json")   -- ⏸ suspends, returns directly
  local data = nu.json.decode(cfg)
  local res = nu.http.request{ url = data.endpoint }  -- ⏸
  return res.status
end)
```

### Cancellation and the watchdog

Two things abort a task **by unwinding the stack without going through
`pcall`** (if they were normal errors, any `pcall` in the ecosystem would
swallow them):

- **`Task:cancel()`**: cooperative cancellation, takes effect at the next
  suspension point.
- **Watchdog**: every continuous Lua execution *slice* (between two
  suspensions) has a budget (100 ms by default). Exceeding it aborts the
  task.

To release resources no matter what happens —success, error, or abort—
register [`nu.task.cleanup(fn)`](/nu/en/api/task/). The `ECANCELED` and
`EBUDGET` codes only exist to *observe* those aborts (e.g., in
`Task:await`), not to catch them.

## 4. Workers: real parallelism, opt-in

When you need to burn CPU without freezing the loop, you spin up a
[**worker**](/nu/en/api/worker/): a new Lua state in its own goroutine,
with **no shared memory**. Communication happens via **JSON-able message
passing** (copied, not referenced). A worker has no `nu.ui` and no access to
the main event bus, and it can be restricted to a subset of the API with
`caps` (capability-based sandboxing).

The "Lua decides, Go executes" rule means you'll rarely need a worker: if
you're burning CPU in Lua, a Go primitive is probably missing.

## 5. Batteries included, but not plugged in

The binary ships with the official extensions **embedded** (`go:embed`) but
**none active by default**. A freshly installed `nu` is a bare runtime;
plugging them in is explicit but trivial: the first launch with a TTY offers
to activate the official set (the agent, the chat…) with a single key, and
without a TTY the `nu --default-config` flag does the same thing with one
command —in both cases with no network access—. Same mental model as
Neovim: the program doesn't ship with plugins activated.

A [**plugin**](/nu/en/api/plugin/) is a directory with `plugin.toml`
(`name`, `version`, `requires?`) and `init.lua`. The **name is the
identity**, and the loader keeps it unique, which lets event namespaces and
other registries stay collision-free by simple convention (namespace =
plugin name). The user's `init.lua` loads **last**, so it has the final say
(keymaps, theme, overrides) by construction.

## Markers you'll see in the reference

| Marker | Meaning |
|---|---|
| **⏸** | **Suspending** function: can only be called inside a task; yields control until it completes and returns the result directly. |
| **[W]** | Available inside **workers**. Without this mark, the function belongs to the main state only. |

With this, you can now read any page of the
[reference](/nu/en/api/convenciones/).
