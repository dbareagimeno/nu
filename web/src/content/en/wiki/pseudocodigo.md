# Validation exercise: end-to-end pseudocode

Status: validation exercise prior to freezing the API. Rule of the game:
**only what is specified** in [api.md](api.md),
[providers.md](providers.md), [sesiones.md](sesiones.md),
[agente.md](agente.md), and [chat.md](chat.md) may be used. Every point where
the code could not be written is a finding (list at the end). The code is
illustrative, neither normative nor complete.

---

## Scenario 1: Anthropic adapter (providers.md §3)

```lua
-- providers extension / adapters/anthropic.lua
return {
  name = "anthropic",
  caps = { tools = true, images = true, thinking = true, system = true, usage = true },

  stream = function(req, provider)
    local s = nu.http.stream{
      url = provider.base_url .. "/v1/messages",
      method = "POST",
      headers = {
        ["x-api-key"] = provider.api_key,
        ["anthropic-version"] = "2023-06-01",
        ["content-type"] = "application/json",
      },
      body = nu.json.encode(to_wire(req)),          -- canonical → dialect
      idle_timeout_ms = 60000,                       -- [FINDING H2]
    }

    if s.status >= 400 then
      local body = {}
      for chunk in s:chunks() do body[#body + 1] = chunk end
      local err = nu.json.decode(table.concat(body))
      error({ code = "EPROVIDER", message = err.error.message,
              detail = { status = s.status,
                         retryable = s.status == 429 or s.status >= 500 } })
    end

    -- Anthropic SSE → canonical Event vocabulary
    local assembling = new_message_assembler()       -- pure Lua
    return function()                                 -- iterator<Event>
      for sse in s:events() do                        -- ⏸ api.md §8
        local d = nu.json.decode(sse.data)
        if d.type == "content_block_delta" and d.delta.type == "text_delta" then
          assembling:push_text(d.index, d.delta.text)
          return { type = "text", text = d.delta.text }
        elseif d.type == "content_block_start" and d.content_block.type == "tool_use" then
          assembling:open_tool(d.index, d.content_block)
          return { type = "tool_call.begin",
                   id = d.content_block.id, name = d.content_block.name }
        elseif d.type == "message_delta" then
          assembling:set_stop(d.delta.stop_reason)
          return { type = "usage", output_tokens = d.usage.output_tokens }
        elseif d.type == "message_stop" then
          return { type = "done", stop_reason = assembling.stop_reason,
                   message = assembling:finish() }   -- thinking metadata included
        end
        -- ...remaining analogous types
      end
      return nil
    end
  end,
}
```

Verdict: written entirely with `nu.http.stream` + `s:events()` +
`nu.json`. Only friction point: the idle timeout ([H2](#findings)).

## Scenario 2: the agent's turn (agente.md §2) and waiting for permissions

```lua
-- agent extension (loop core, simplified)
function Session:send(content)
  self:append{ t = "message", ts = nu.sys.now_ms(),
               message = { role = "user", content = as_blocks(content) } }

  while true do
    local req = run_hook_chain("request.pre", self:assemble_request(), self.ctx)
    local msg = with_retries(function()              -- backoff: nu.task.sleep
      return consume_stream(self, req)               -- scenario 1 + agent:delta
    end)

    self:append{ t = "message", ts = nu.sys.now_ms(),
                 message = msg, usage = self.last_usage, model = self.model }
    nu.events.emit("agent:message", { session = self.id, message = msg })

    local calls = tool_calls_in(msg)
    if #calls == 0 then return msg end

    for _, call in ipairs(calls) do                  -- sequential (P12)
      local result = self:run_tool(call)             -- below
      self:append_tool_result(call, result)
    end
    self:maybe_compact()                             -- §8 of the contract
  end
end

function Session:run_tool(call)
  local verdict = static_policy(self.permissions, call)        -- deny/allow/?
  if verdict == nil then
    verdict = run_hook_chain("permission", call, self.ctx)     -- middleware
  end
  if verdict == nil and self.permissions.mode == "ask" then
    if not interactive() then return denied("headless")  end   -- default deny
    local fut = nu.task.future()                     -- [FINDING H1]
    pending_asks[call.id] = fut
    nu.events.emit("agent:permission.asked", { id = call.id, call = call })
    verdict = fut:await()                            -- ⏸ until respond()
  end
  if verdict.deny then return denied(verdict.deny) end

  local tool = registry[call.name]
  local args = run_hook_chain("tool.pre", call.args, self.ctx)
  local ok, result = pcall(tool.handler, args, self.ctx)       -- errors → is_error
  if not ok then return { is_error = true, content = err_text(result) } end
  return run_hook_chain("tool.post", result, self.ctx)
end

-- The other half of the rendezvous (called by the chat extension):
function agent.permission.respond(id, verdict)
  local fut = pending_asks[id]; pending_asks[id] = nil
  fut:set(verdict)                                   -- wakes up the turn
end
```

Verdict: everything exists **except the rendezvous**: a task (the turn)
must sleep until other code (the UI dialog) wakes it with a value. Without
a primitive, the only option was a polling loop with `task.sleep` —
unacceptable as a foundational pattern ([H1](#findings)).

## Scenario 3: a tool with progress (agente.md §3)

```lua
agent.tool{
  name = "bash", description = "Runs a shell command",
  schema = { type = "object", properties = { command = { type = "string" } } },
  permissions = { default = "ask" },                 -- mutates: asks for permission
  handler = function(args, ctx)
    local p = nu.proc.spawn({ "sh", "-c", args.command }, { cwd = ctx.cwd })
    local out = {}
    while true do
      local line = p:read_line("stdout")             -- ⏸
      if line == nil then break end
      out[#out + 1] = line
      ctx.progress(line)                             -- agent:tool.progress
    end
    local st = p:wait()                              -- ⏸
    if st.code ~= 0 then error({ code = "EIO", message = "exit " .. st.code,
                                 detail = { output = table.concat(out, "\n") } }) end
    return table.concat(out, "\n")
  end,
}
```

Verdict: clean. `read_line` + `progress` give live output streaming with
nothing new needed.

## Scenario 4: subagent in a worker with a tool proxy (agente.md §9)

```lua
-- Main side: spawning and servicing the proxy
function Session:spawn_worker_sub(opts)
  local w = nu.worker.spawn("agent.sub_loop", { caps = opts.caps })
  w:on_message(function(m)
    if m.type == "tool" then
      nu.task.spawn(function()
        local result = self:run_tool(m.call)         -- full pipeline (§2)
        w:send{ type = "tool_result", id = m.call.id, result = result }
      end)
    elseif m.type == "delta" then
      nu.events.emit("agent:delta", { sub = m.sub_id, text = m.text })
    end
  end)
  w:send{ type = "start", opts = strip_to_json(opts) }   -- data only
  return wrap_sub(w)
end

-- Worker side (agent/sub_loop.lua): no nu.ui, no nu.events
local adapter = require("providers.adapters.anthropic")  -- [FINDING H3]
local start = nu.worker.parent.recv()                    -- ⏸
for _ = 1, start.opts.max_turns do
  for ev in adapter.stream(req, cfg) do
    if ev.type == "text" then
      nu.worker.parent.send{ type = "delta", text = ev.text }
    elseif ev.type == "done" then msg = ev.message end
  end
  local calls = tool_calls_in(msg)
  if #calls == 0 then break end
  for _, call in ipairs(calls) do
    nu.worker.parent.send{ type = "tool", call = call }
    local r = nu.worker.parent.recv()                    -- ⏸ result from the proxy
    append_result(req, call, r.result)
  end
end
```

Verdict: the proxy works as designed (JSON-able args/results). Friction
point: the worker needs `require` for plugin Lua modules (the adapter) and
the specification did not explicitly say that the loader resolves inside
workers ([H3](#findings)).

## Scenario 5: a fuzzy picker over raw `nu.ui` (api.md §9 and §11)

Without a toolkit (deliberately unspecified): this validates that the
primitive is enough to build one.

```lua
function fuzzy_picker(title)
  local size = nu.ui.size()
  local reg = nu.ui.region{ x = 4, y = 2, w = size.w - 8, h = 20, z = 100 }
  local fut, query, files, sel = nu.task.future(), "", {}, 1   -- [H1] again

  nu.task.spawn(function()
    files = nu.search.files(nu.fs.cwd())             -- ⏸ respects .gitignore
    repaint()
  end)

  local function repaint()
    local ranked = nu.search.fuzzy(query, files, { max = 18 })  -- synchronous
    local lines = { { { text = title .. " " .. query, style = { bold = true } } } }
    for i, m in ipairs(ranked) do
      lines[#lines + 1] = { { text = files[m.index],
                              style = i == sel and { reverse = true } or {} } }
    end
    reg:clear(); reg:blit(0, 0, nu.ui.block(lines))
    reg:cursor(nu.text.width(title) + 1 + nu.text.width(query), 0)
  end

  local input = nu.ui.on_input(function(ev)          -- top of the stack
    if ev.type ~= "key" then return true end
    if ev.key == "escape" then fut:set(nil)
    elseif ev.key == "enter" then fut:set(current_selection())
    elseif ev.key == "up" or ev.key == "down" then move_sel(ev.key)
    elseif ev.text then query = query .. ev.text end
    repaint(); return true                           -- consumes everything: modal
  end)

  repaint()
  local choice = fut:await()                         -- ⏸ until enter/escape
  input:pop(); reg:destroy()
  return choice
end
```

Verdict: regions + blocks + input stack + `search.fuzzy` compose a modal
picker in ~30 lines. Main state never does work proportional to the repo
size (ranking is a Go primitive). `future` shows up again as the piece
that was missing for "wait for the selection."

## Scenario 6: a complete third-party plugin (chat.md §9)

```lua
-- plugins/pytest-runner/init.lua
agent.tool{
  name = "run_tests", description = "Runs the test suite",
  schema = { type = "object", properties = { filter = { type = "string" } } },
  permissions = { default = "allow" },               -- only reads and runs tests
  handler = function(args, ctx)
    local r = nu.proc.run({ "pytest", "-q", args.filter or "." },
                          { cwd = ctx.cwd, timeout_ms = 120000 })
    return nu.json.decode(parse_summary(r.stdout))   -- structured table
  end,
}

chat.renderer("run_tests", function(result, width)
  local lines = {}
  for _, t in ipairs(result.failures) do
    lines[#lines + 1] = { { text = "✗ " .. t.name, style = { fg = "error" } } }
  end
  lines[#lines + 1] = { { text = result.passed .. " passed", style = { fg = "ok" } } }
  return nu.ui.block(lines)
end)

chat.command{
  name = "test", description = "Asks the agent to fix any failing tests",
  handler = function(args, ctx)
    ctx.session:send("Run run_tests and fix any failures you find") -- ⏸
  end,
}

chat.statusline.add{
  id = "pytest", side = "right", priority = 50,
  render = function() return { { text = last_status, style = { fg = "dim" } } } end,
}
```

Verdict: `chat`'s four extension points plus the tool registry compose a
real plugin without touching anything internal.

---

## Findings

**H1 — Missing a rendezvous primitive (`nu.task.future`).** Showed up
three times (waiting for permissions, the modal picker, and generally
"one task waits for a value another piece of code will produce"). Without
it, the pattern would be polling with `task.sleep` — unacceptable as a
foundational pattern. Resolution: add to [api.md](api.md) §3
`nu.task.future() -> Future`, with `Future:set(v)` (synchronous, once
only) and `Future:await() -> v ⏸` (several can wait; if already resolved,
returns immediately).

**H2 — Idle timeout on streams.** `timeout_ms` reasonably covers up to
receiving headers, but an SSE can go silent forever. Resolution:
`opts.idle_timeout_ms` in `nu.http.stream` (raises `ETIMEOUT` if N ms pass
without bytes).

**H3 — `require` inside workers.** Scenario 4 needs to load the adapter
module inside the worker. Resolution: clarify in [api.md](api.md) §13
that the loader's `require` paths (plugin Lua modules) are available in
workers; what does not exist is the `nu.plugin` API (lifecycle).

None of the other points in the six scenarios required inventing new API.
With H1-H3 applied, the corpus is ready to be frozen.

---

# Round 2: the ugly paths

Same rule, worse intent: cancellations mid-flight, orphaned resources,
flooded queues, the radical user, and startup. Findings F1-F5 at the end.

## Scenario 7: esc mid-turn with everything in flight

State: an open SSE stream, a `bash` tool with a process running, and a
permission dialog pending for another tool. The user presses `esc` →
`Session:cancel()`.

```lua
-- What SHOULD happen? The turn's task aborts at its next suspension...
-- but trying to write it out revealed two cracks:

-- CRACK A [F1]: the loop wraps handlers in pcall (errors → is_error).
local ok, result = pcall(tool.handler, args, ctx)
-- If cancellation is delivered as a "normal" ECANCELED error, this pcall
-- CATCHES IT: the cancellation turns into an error tool_result and the
-- turn carries on with the next tool as if nothing happened. Cancellation
-- needs to be a NON-catchable abort, or every pcall in the ecosystem breaks it.

-- CRACK B [F2]: the bash handler had a live process:
handler = function(args, ctx)
  local p = nu.proc.spawn({ "sh", "-c", args.command }, { cwd = ctx.cwd })
  nu.task.cleanup(function() p:kill() end)   -- ← DID NOT EXIST; without this, the
  ...                                         --   abort leaves the process orphaned
end
-- Same with the picker in scenario 5: if the task calling it is aborted,
-- who does input:pop() and reg:destroy()? Without a cleanup mechanism
-- tied to the task, every cancellation leaves garbage behind (processes,
-- regions, stacked input handlers).

-- The pending permission future: the aborted turn stops waiting; a late
-- respond() does set() on a future nobody is waiting on — harmless. ✓
```

## Scenario 8: the full MCP extension (long-lived process + JSON-RPC)

```lua
-- mcp/client.lua — an MCP server over stdio, alive for the whole session
local M = { pending = {}, next_id = 0 }

function M.connect(argv)
  M.proc = nu.proc.spawn(argv, {})
  nu.task.spawn(function()                       -- permanent reader task
    nu.task.cleanup(function() M.proc:kill() end) -- [F1/F2] again
    while true do
      local line = M.proc:read_line("stdout")    -- ⏸
      if line == nil then return M.reconnect() end
      local msg = nu.json.decode(line)
      local fut = M.pending[msg.id]               -- correlate by id:
      if fut then M.pending[msg.id] = nil; fut:set(msg) end  -- futures ✓ (H1)
    end
  end)
end

function M.request(method, params)                -- concurrent without friction
  M.next_id = M.next_id + 1
  local fut = nu.task.future()
  M.pending[M.next_id] = fut
  M.proc:write(nu.json.encode{ jsonrpc = "2.0", id = M.next_id,
                               method = method, params = params } .. "\n") -- ⏸
  return fut:await()                              -- ⏸
end

-- Clean shutdown: nu.events.on("core:shutdown", function() M.proc:kill() end) ✓
-- Startup: connect() is NOT called when the module loads (guide §1), but on
-- first use or on core:ready. ✓
```

Verdict: the future-per-id pattern resolves concurrent JSON-RPC with
elegance. The need for `cleanup` shows up again ([F1](#findings-round-2)).

## Scenario 9: a worker flooding the main thread

A subagent in a worker emits `delta` for every token; the main thread is
slow (painting). What happens to the message queue?

```lua
nu.worker.parent.send{ type = "delta", text = ev.text }
-- The specification said NOTHING about queue size or what happens when
-- it fills up [F3]. Without a limit: unbounded memory (the same hole we
-- already closed for streams in §8). With a limit and an error: does the
-- worker blow up for going too fast? The answer coherent with the rest of
-- the design is backpressure: send suspends (⏸) until there's room — the
-- worker throttles itself to the consumer's pace, just like a stream.
```

## Scenario 10: the radical user and startup

Wants: to disable the official `chat` extension, load their own, and have
their keymaps win over any plugin's.

```lua
-- 1. Disable chat? There was no mechanism [F4]: nu.plugin.list() shows
--    "enabled" but nothing governs it. → user's nu.toml:
--      [plugins]
--      disabled = ["chat"]

-- 2. Do their keymaps win? Depends on STARTUP ORDER, which was not
--    specified [F4]. Without a defined order, "who wins" is a race.
--    → Canonical order: core → plugins (topological, respecting disabled)
--      → user's init.lua → core:ready.
--    init.lua goes last: the input stack means the most recently
--    registered handler wins → the user gets the last word by
--    construction, with no special priority mechanism.

-- 3. Their alternative chat: agent.* + nu.events "agent:*" + toolkit — all
--    public (already validated in earlier rounds). ✓
```

## Scenario 11: cost of re-rendering while streaming (analysis, no code)

Every `agent:delta` appends text to the message in progress; is
re-rendering the whole message's markdown on every token quadratic?
Answer: repainting is coalesced to ~30 ms (ADR-007), so the correct
pattern is to re-render the in-progress message **once per paint tick**,
not per delta — and `nu.text.markdown` over a few KB in Go takes
microseconds. It's not an API crack; it's a pattern that must be written
into the guide ([F5](#findings-round-2)).

---

## Findings (round 2)

**F1 — Cancellation cannot be a catchable error, and `nu.task.cleanup`
was missing.** If the abort (via `cancel()` or the watchdog) is delivered
as a normal error, any `pcall` in the ecosystem catches it and the program
carries on as if nothing happened (scenario 7). Resolution in
[api.md](api.md) §1.3 and §3: the abort unwinds the task **without going
through `pcall`**, and `nu.task.cleanup(fn)` registers LIFO releasers that
always run (success, error, or abort) — this house's `defer`.

**F2 — Resource lifetime tied to the task.** Processes, regions, and
input handlers did not die with the task that created them. Resolution:
the convention is `cleanup` (F1) in whoever creates them; as a safety
net, a `Proc` with no references ends up killed by the GC
(non-deterministic — explicit cleanup is the rule, guide §3).

**F3 — Backpressure in worker↔main channels.** Bounded queues; `send`
(both sides) becomes suspending ⏸: whoever produces faster than the other
consumes gets throttled — consistent with the streams in §8. From
synchronous handlers: `task.spawn` as always.

**F4 — Startup and plugin governance unspecified.** There was no way to
disable a plugin nor a defined load order (who wins a keymap?).
Resolution in [api.md](api.md) §14: runtime configuration file
`config.dir()/nu.toml` (`plugins.disabled`, watchdog budget) and canonical
order **core → plugins → user's init.lua → `core:ready`** — the user wins
by going last, with no priority system.
*Later note: ADR-010 flipped the default — official extensions ship
**inactive** and `nu.toml` governs activation, not deactivation. The
`plugins.disabled` from this finding and from scenario 10 reflects the
state prior to that ADR.*

**F5 — Streaming render pattern** (no API change): re-render the
in-progress message once per paint tick, not per delta. Goes into the
guide (§6).

---

# Round 3: the untortured zones

Change of method: this round **applies no resolutions** — every crack
goes to the open-problems list ([problemas.md](problemas.md)) to be
resolved one at a time.

## Scenario 12: terminal resize with a modal open

```lua
-- The scenario 5 picker, with the terminal at 120 columns:
local reg = nu.ui.region{ x = 4, y = 2, w = nu.ui.size().w - 8, h = 20, z = 100 }
-- The user shrinks the terminal to 60 columns. Now what?
--   · The region has w = 112 on a 60-wide screen: does it get clipped? Error?
--     The spec defines clipping of blit INSIDE the region, but not what
--     happens to a region that falls outside the screen.               [G1]
--   · Nobody repositions the picker: it never subscribed to "ui:resize". Is it
--     a convention, declarative anchors (x = "center"), or every plugin
--     fends for itself?                                                [G1]
```

## Scenario 13: the plugin author's development cycle

```lua
-- I edit my plugin and want to test it WITHOUT restarting nu:
nu.plugin.reload("my-plugin")   -- ← does not exist
-- And even if it did: require caches modules; re-running init.lua
-- would duplicate tools, commands, keymaps, and hooks (no mass
-- deregistration exists). All registrations return a handle (Sub,
-- Keymap, Hook...), but nothing tracks them per plugin → there is no
-- way to undo "everything from my-plugin".
-- Today the only path is to restart nu on every iteration.             [G2]
-- (Same minor hole: hot-editing providers.toml or nu.toml.)
```

## Scenario 14: two agent sessions in the same UI

```lua
-- A running subagent + the main session, both emitting:
nu.events.emit("agent:delta", { text = ev.text })        -- WHOSE is it?
-- The contracts do not REQUIRE a session_id in every agent:* payload;
-- chat.md doesn't say it filters either. Two concurrent turns would mix
-- deltas in the same block.                                            [G3]

-- And if both sessions ask for permission at once: two simultaneous
-- modals over the same input stack — is there a modal queue? Undefined. [G3]

-- Reentry: the user presses enter with a turn in flight:
session:send("something else")   -- EBUSY? Queued? Cancel and replace?
-- Undefined; every UI would improvise a different semantics.           [G4]
```

## Scenario 15: the same session resumed in two terminals

```lua
-- Terminal A: nu --continue  → opens sessions/proj/2026-...jsonl
-- Terminal B: nu --continue  → opens the SAME file!
-- Two processes doing interleaved fs.append on a single JSONL: silent
-- corruption (interleaved lines). sesiones.md does not contemplate any lock.
--                                                                       [G5]
```

## Scenario 16: the read-only subagent cannot be expressed

```lua
-- I want an auditor subagent: reads EVERYTHING, writes NOTHING.
local w = nu.worker.spawn("auditor", { caps = { "fs", "text", "search" } })
-- caps grants ENTIRE MODULES: "fs" includes write, remove, rename...
-- There is no "read-only fs" nor caps per function or per path. The
-- whole-module granularity falls short exactly in the flagship
-- sandboxing case.                                                     [G6]
```

## Scenario 17: loose ends found without their own scenario

```lua
-- a) nu.fs.watch(path, fn): recursive or a single path? Does it respect
--    .gitignore? (watching node_modules/ = infinite burst) Does it
--    coalesce bursts (git checkout touches 5000 files)?                [G7]

-- b) Worker:on_message(fn) and Worker:recv() are "alternatives," but
--    nothing forbids using both: who receives the message? Undefined.  [G8]

-- c) Windows: the bash tool does { "sh", "-c", ... } (sh doesn't exist),
--    Proc:kill talks about POSIX signals, and terminal input (IME,
--    keys) differs. What's the v1 scope on Windows?                    [G9]
```

Findings G1-G9 consolidated with impact and options in
[problemas.md](problemas.md).

---

# Round 4: new angles (completeness verification)

Explicit question: was everything covered? Answer: no. This round attacks
the event bus under reentry, binary-data boundaries, corporate and
subscription providers, the trust model for repo content, and the inside
of workers. Findings G10-G16, unresolved, go to
[problemas.md](problemas.md).

## Scenario 18: the event bus under reentry

```lua
nu.events.on("agent:message", function(p)
  nu.events.emit("my-plugin:summary", digest(p))   -- emit INSIDE an emit
end)
nu.events.on("agent:message", function(p)
  sub:cancel()                                     -- what if it cancels a sub
  another = nu.events.on("agent:message", g)        --  or subscribes NEW ones
end)                                                --  during dispatch?
-- Does the nested emit dispatch in depth (recursion) or get queued?
-- Does a freshly subscribed handler see the event IN PROGRESS? What
-- about one cancelled mid-way? All undefined — and it's the kind of
-- undefinedness that produces bugs depending on plugin load order.     [G10]
```

## Scenario 19: bytes that aren't text

```lua
-- The bash tool cats a PNG by mistake:
local r = nu.proc.run({ "cat", "logo.png" }, {})
return r.stdout   -- arbitrary bytes → tool_result → three JSON boundaries:
-- 1) nu.json.encode toward the provider: JSON requires valid UTF-8. Does
--    it raise? Replace? Stay silent?
-- 2) the transcript JSONL's `message` entry: same question.
-- 3) a Worker:send with that result: is it really "JSON-able"?
-- Without a rule, every boundary improvises and the bug shows up far
-- from its origin.                                                     [G11]
```

## Scenario 20: the corporate proxy we put in the philosophy doc

```lua
-- providers.toml promised "corporate proxy" as a flagship case:
[providers.corp]
adapter  = "openai-compat"
base_url = "https://llm.interna.corp"   -- self-signed corporate CA
-- nu.http has no TLS options: no ca_file, no insecure, no explicit
-- proxy (is HTTPS_PROXY from the environment respected? unspecified).
-- The announced use case cannot be configured.                         [G12]
```

## Scenario 21: subscription provider (OAuth)

```lua
-- An adapter for a subscription plan (not an API key): OAuth device flow
-- IS writable (http.request in a polling loop + opening a URL with
-- nu.proc). But the flow with a localhost callback is NOT: there is no
-- server/listener HTTP primitive. And where does the adapter store the
-- refresh token? (plugins/<name>/? In cleartext?) No convention.       [G13]
```

## Scenario 22: the malicious repo (trust model)

```lua
-- nu is opened on a repo cloned from the internet. The repo brings:
--   .nu/skills/innocent/SKILL.md   → its index gets injected into the
--                                     system prompt (agent §6-§7) WITHOUT asking
--   .nu/agent.toml                 → could bring allow = ["bash:*"]!
--                                     (precedence: project > global)
-- Result: cloning a repo and opening nu is already executing the repo's
-- will. Same problem with third-party MCP server tool descriptions
-- (untrusted text injected into the model). There is no trust model:
-- neither trust-on-first-use, nor which repo config gets honored
-- without asking.                                                      [G14]
```

## Scenario 23: inside a worker, what exactly is there?

```lua
-- worker with task [W]: does the worker have its OWN scheduler/event loop?
nu.task.spawn(...)   -- multiple tasks inside a worker? Timers?
nu.task.race(...)    -- (scenario 4 already assumed this to multiplex
                     --  the stream and cancellation... without it being written)
-- Does the watchdog apply inside the worker? With what budget?         [G15]

-- And two parallel subagents editing the SAME file via the tool proxy:
-- the tool calls interleave on the main side but nothing coordinates
-- writes to the same path — silent last-write-wins.                    [G16]
```

Minor items noted in passing: rotation of the `nu.log` file
(→ [P20](pospuesto.md)); ownership of `Timer`s (do they die with the task?
→ `cleanup` convention); version constraints in `requires` (folds into
[P4](pospuesto.md) when reopened).

---

# Round 5: a third party builds agent orchestration

The stress-test question: if the official `agent` extension exists, can
**another** plugin build deterministic agent loops on top of it and run
them in parallel, using only the public contract ([agente.md](agente.md))
+ `nu.task` + `nu.worker`? Same rule as always. Two axes to pull to their
limits: **determinism** (a loop reproducible in its control flow) and
**parallelism** (N agents at once). Deliberately out of scope: the
model's *sampling* non-determinism (temperature/seed) is
[providers.md](providers.md) territory, not the orchestrator's.

## Scenario 24: deterministic loop driver (third-party plugin)

A fixed pipeline plan → implement → test → review, sequential and
bounded. Written by someone who just does `require("agent")`.

```lua
-- plugins/pipeline/init.lua
local agent = require("agent")          -- the loader puts its lua/ in require ✓ (§14)

-- DETERMINISTIC compaction: a mechanical rule, no LLM → same input gives the
-- same summary. The compact hook (§8) receives the conversation and returns
-- the summary message; nothing requires it to be an LLM.
agent.hook("compact", function(convo, ctx)
  return keep_system_and_last_k(convo, 12)        -- pure Lua, reproducible ✓
end)

local STEPS = { "plan", "implement", "run tests and fix", "review the diff" }

function run_pipeline(goal)
  local s = agent.session{
    model = "anthropic/claude-...",
    permissions = {                                -- headless = default deny (§5);
      mode = "auto",                               -- the allowlist is declared, not inherited
      allow = { "read", "grep", "glob", "edit", "bash:pytest *", "bash:git *" },
      deny  = { "bash:rm *", "bash:curl *" },
    },
    -- max_turns comes from agent.toml (§10): a hard cap against divergence
  }
  local outcomes = {}
  for i, step in ipairs(STEPS) do
    local msg = s:send(goal .. " — phase: " .. step)    -- ⏸ full turn (§2)
    outcomes[i] = decide(msg)                          -- deterministic branch
    if outcomes[i].halt then break end                 -- flow control, not the model's
  end
  return outcomes
end
```

Control flow is deterministic and the `for` does not suffer G4's reentry:
each `send` is awaited before the next, the queue never even activates.

**Friction point — observing what happened INSIDE the turn.** `send`
returns only the assistant's final message; for a deterministic branch on
"did the tests pass?" the driver needs the tool's result, not the model's
prose. No new API needed: subscribe to `agent:tool.end` filtering by
`payload.session` (the mandatory attribution from G3) or read the JSONL
transcript ([sesiones.md](sesiones.md)). It works, but it's sideways
observation, not a return value — noted without elevating it to a finding.

```lua
local seen = {}
nu.events.on("agent:tool.end", function(p)
  if p.session == s.id and p.tool == "run_tests" then seen[#seen+1] = p.result end
end)
```

Verdict: the deterministic loop is written entirely with `agent.session`
+ `send` + `agent.hook("compact")`. Mechanical compaction via a hook is
the piece that saves determinism without touching the core.

## Scenario 25: parallel fan-out of N subagents

A "map" over disjoint territories. I want: a concurrency limit (not
opening 50 streams and eating a 429), *allSettled* semantics (one failure
doesn't kill the rest), and results **aligned with the input**.

```lua
-- Semaphore built SOLELY out of nu.task.future (just as the picker built
-- the modal): validates once more that the primitive is enough.
local function semaphore(n)
  local free, waiters = n, {}
  return {
    acquire = function()                              -- ⏸
      if free > 0 then free = free - 1; return end
      local f = nu.task.future(); waiters[#waiters + 1] = f; f:await()
    end,
    release = function()
      local w = table.remove(waiters, 1)
      if w then w:set(true) else free = free + 1 end
    end,
  }
end

function fan_out(root, territories, limit)
  local sem = semaphore(limit)
  local fns = {}
  for i, terr in ipairs(territories) do
    fns[i] = function()
      sem.acquire()                                   -- ⏸ respects the limit
      nu.task.cleanup(sem.release)                    -- releases on success/error/abort (F1)
      local ok, res = pcall(function()
        return root:spawn{                            -- task by default (§9)
          permissions = narrow(root.permissions, terr),   -- never widens (§11)
          skills = terr.skills,
        }:run(terr.prompt)                            -- ⏸
      end)
      return { ok = ok, value = res }                 -- allSettled: never rethrows
    end
  end
  return nu.task.all(fns)                             -- ⏸ waits for all  [FINDING G27]
end
```

This **is real parallelism where it matters**: each `spawn{}:run()` runs
as a task and suspends on its `nu.http.stream` call to the LLM; while one
waits, the others progress, and the network goroutines truly run in
parallel (§9, the hot case from
[modelo-ejecucion.md](modelo-ejecucion.md)). The per-branch `pcall` gives
me the *allSettled* that `task.all` doesn't offer out of the box (it only
ships fail-fast: "if one throws, cancel the rest and rethrow"). The
`future`-based semaphore gives me the concurrency limit without new API.

**[FINDING G27] — `nu.task.all` does not promise to align results with
inputs.** The signature says `(fns) -> any[]` and "waits for all," but
**not** that `out[i]` corresponds to `fns[i]` (tasks finish in any
order). For a deterministic orchestration this is exactly what needs to
be guaranteed: without positional alignment, the "map" cannot correlate
result with territory except by manually threading the index inside each
payload. Proposed resolution: specify `Promise.all` semantics — results
in **input order**, independent of completion order.

Verdict: bounded, failure-robust parallel fan-out is written with
`spawn` + `task.all` + `future`. A single real crack (G27), and it's a
*specification* crack, not a mechanism one.

## Scenario 26: a two-level parallel tree? (the honest limit)

The temptation: "for real parallelism, workers." I want 3 leaders in
parallel, each with their own 3 workers in parallel.

```lua
local leader = root:spawn{ worker = true, caps = agent.caps.FS_RO }  -- loop in a worker (§9)
-- ...and inside the leader (which runs in the worker) I want its own fan-out:
--   nu.worker.spawn(...)   → does NOT exist inside a worker (P11): no nesting.
--   sub-workers as tasks inside the worker → the worker DOES have its own
--     scheduler (G15), they concur over IO. But their tool calls: the
--     leader's sub_loop already proxies its own to the main side (§9); a
--     second level needs a SECOND proxy that §9 describes as a leaf, not
--     as a node that re-spawns.
--                                                            [P11 + nuance §9]
```

But the nuance that defuses almost the entire problem: **agent workloads
don't need workers.** A worker's payoff is parallel Lua CPU work; an
agent's hot path is LLM + IO, which **already** overlaps across tasks
(everything suspends). The task-based subagents in scenario 25 already
give real parallel streams. The worker only wins if the subagent burns
CPU in Lua — rare in an agent. Conclusion: P11's limit (no nested
workers) and the worker-subagent's leaf nature (§9) **barely bite** in
practice; the parallelism tree an agent orchestrator actually needs is
made of tasks, and that has no depth limit.

Verdict: confirms P11 and refines §9 (the worker-subagent is a leaf: it
doesn't re-spawn). Not a blocker for the use case — it's the wrong case
to worry about.

## Scenario 27: determinism ⟂ shared mutable state (analysis)

Where the two axes collide. Three fronts, none needing new API — they're
the boundary the orchestrator must be aware of:

```lua
-- 1) Two parallel branches touching the same path: silent last-write-wins
--    (G16, already known). RESULT determinism requires disjoint territories
--    per prompt, or isolating each subagent in its own worktree:
--      root:spawn{ ... }  with cwd = git_worktree(i)   -- opts.cwd exists (§2)
--    and merging at the end. Territory division is the orchestrator's job.

-- 2) Cancellation mid-write: task.all does fail-fast and aborts the rest.
--    Cleanup (F1/F2) kills the tool's process, but a half-written file is
--    left → non-deterministic partial state. The remedy is the same: a
--    worktree per branch, discard the aborted ones. Inherent to "parallel +
--    IO + cancellation," not a contract crack.

-- 3) TOTAL reproducibility (replaying model responses): there is no
--    response hook in §4 (request.pre/tool.pre/tool.post/permission/compact,
--    never "response"). Correct: recording/replaying model outputs is a
--    provider ADAPTER's job (providers.md §3), not the agent's — a "replay
--    adapter" reads from a fixture instead of the network. The layer is the
--    right one.
```

Verdict: the determinism↔parallelism tension lives entirely in shared
mutable state, and the contract already names the remedy (G16: divide
territory; `opts.cwd` to isolate). A deterministic-and-parallel
orchestrator is expressible **if** its branches are independent; with
shared state, the right answer is to serialize.

---

## Findings (round 5)

**G27 — `nu.task.all` must guarantee results aligned with inputs.**
The only new mechanism finding. Without a specified positional order, a
deterministic parallel orchestration cannot correlate result with input
without manually carrying the index along. Proposed resolution:
`Promise.all` semantics in [api.md](api.md) §3 — `out[i]` is the result of
`fns[i]`, independent of completion order. **Resolved**: applied to
[api.md](api.md) §3 and logged in [problemas.md](problemas.md) (G27).

Confirmations (no new API): the deterministic loop is built on the public
contract (§24); bounded, *allSettled* parallel fan-out is composed with
`spawn` + `task.all` + `future` + `pcall` (§25); the nested-worker limit
(P11) and the worker-subagent's leaf nature (§9) **do not bite** in agent
workloads, which are LLM+IO and already overlap across tasks (§26); the
determinism↔parallelism tension reduces to shared mutable state, with an
already-named remedy (G16 + `opts.cwd`, §27).

Patterns for the guide (no API change): mechanical compaction via the
`compact` hook for reproducible loops (§24); a `nu.task.future`-based
semaphore to bound a fan-out's concurrency (§25); *allSettled* by
wrapping each branch in `pcall` before `task.all` (§25); a worktree per
subagent to isolate parallel writes (§27).

---

# Round 6: rebuilding a claude-code-style harness on top of `nu.ui`

The stress-test question: can a coding-harness TUI (claude-code-style) be
built **entirely** on raw `nu.ui` + the [chat.md](chat.md) contract? The
short answer is that `chat.md` already *is* that harness; so this round
doesn't rewrite what's already validated (transcript, modals, slash
commands, statusline — scenario 5 covered the modal picker) but tortures
what `chat.md` takes for granted: the transcript's **scrollback**, the
multiline editor's **real cursor**, the **live spinner**, and the
**mouse** over collapsible blocks. Three cracks come out of that, all in
`nu.ui` §9. Findings G28-G30 at the end.

## Scenario 28: the three zones and the transcript's scrollback

```lua
-- plugins/cc-ui/init.lua — a coding-harness-style UI on top of nu.ui
local function layout()
  local s = nu.ui.size()
  return {
    transcript = nu.ui.region{ x = 0, y = 0,       w = s.w, h = s.h - 4 },
    input      = nu.ui.region{ x = 0, y = s.h - 4, w = s.w, h = 3,  z = 10 },
    status     = nu.ui.region{ x = 0, y = s.h - 1, w = s.w, h = 1,  z = 10 },
  }
end

-- The transcript is a tall Block (the whole rendered history) that "peeks"
-- through the region via a vertical offset. Scroll = re-blit with a different y.
local scroll, doc = 0, nu.ui.block({})           -- doc.height can be >> the region
local function repaint_transcript(reg)
  reg:clear()
  reg:blit(0, -scroll, doc)                       -- [FINDING G28] does blit accept y<0?
end
nu.events.on("ui:resize", function() relayout() end)   -- G1: your region, your resize
```

Verdict: it works, save for one specification crack. `Region:blit`
*"clips to the region's bounds,"* but scrollback needs to stamp the Block
with a **negative** `y` to clip off the first rows (peeking in from
below). The doc only talks about clipping on overflow, not about negative
local coordinates — and that's the central operation of any transcript
with scroll. **[G28]**

## Scenario 29: multiline editor with a real cursor and `@` / `/` popups

```lua
local buf, cur = "", 0                            -- text and caret (byte index)
local function redraw_input(reg)
  local wrapped = nu.text.wrap(buf, reg.w)        -- Block; .height known
  if wrapped.height + 1 ~= reg.h then reg:resize(reg.w, wrapped.height + 1) end
  reg:clear(); reg:blit(0, 0, wrapped)
  local cx, cy = caret_to_cell(buf, cur, reg.w)   -- nu.text.width per grapheme
  reg:cursor(cx, cy)                               -- real terminal cursor
end

nu.ui.on_input(function(ev)
  if ev.type == "paste" then
    local ins = ev.text or ev.path                 -- [G30] image → path, like @
    buf = insert(buf, cur, ins); cur = cur + #ins
  elseif ev.key == "enter" and at_start_slash(buf) then run_slash(buf)
  elseif ev.key == "enter" then session:send(buf); buf = ""
  elseif ev.text == "@" then
    local path = fuzzy_picker("@ file")            -- scenario 5, like popup z=100
    if path then buf = insert(buf, cur, path) end
  end
  redraw_input(input_region); return true
end)
```

Verdict: it comes out whole. `nu.text.wrap` gives the height to grow the box,
`Region:cursor` places the real caret, and the `@`/`/` popups are the picker
from scenario 5 reused. The only unpleasant work is `caret_to_cell` (byte
index → cell with `nu.text.width`), but that belongs to the toolkit, not API
that's missing. Pasting an image shows up here as a **path** (G30, below).

## Scenario 30: the live "Thinking…" spinner with `esc` to interrupt

```lua
local function thinking_indicator(session)
  local t0  = nu.sys.mono_ms()
  local reg = nu.ui.region{ x = 0, y = spin_y, w = 40, h = 1 }
  local frame = 0
  local timer = nu.task.every(80, function()       -- synchronous handler, repaints
    frame = frame + 1
    local secs = math.floor((nu.sys.mono_ms() - t0) / 1000)
    local toks = providers.approx_tokens(session.usage)   -- product vocabulary
    reg:blit(0, 0, nu.ui.block({{
      { text = SPIN[frame % #SPIN + 1] .. " Thinking… ", style = { italic = true } },
      { text = secs .. "s · " .. toks .. " tok · esc to interrupt",
        style = { fg = "#808080" } },
    }}))
  end)
  nu.task.cleanup(function() timer:stop(); reg:destroy() end)   -- F1/F2: dies with the turn
end
-- esc → Session:cancel() (chat.md §3); the cleanup kills the timer and region.
```

Verdict: clean. `nu.task.every` animates, `mono_ms` counts, `cleanup`
guarantees the spinner dies with the turn even if it's aborted — it's the
F5 pattern (coalesced repaint, not delta-based).

## Scenario 31: mouse over a collapsible tool block (analysis)

```lua
-- Clicking the header of a tool block to collapse it:
nu.ui.on_input(function(ev)
  if ev.type == "mouse" then
    -- ev.x, ev.y come in SCREEN coordinates; the block lives in
    -- LOCAL coordinates of the transcript region, offset by
    -- scroll. There is no Region:contains(x,y) nor global→local translation: the
    -- plugin tracks the geometry of each region by hand (which it set) and
    -- resolves the hit-test by adding/subtracting origin and scroll.        [G29]
  end
end)
```

Verdict: expressible, but by hand. The stack model delivers the mouse in
global coordinates and regions are local; without a translation/hit-test
primitive, every clickable widget in the toolkit reimplements the same
calculation. **[G29]**

---

## Findings (round 6)

All three were resolved after discussing counterindications (recorded in
[problemas.md](problemas.md)):

**G28 — `Region:blit` with negative local coordinates (viewport/scrollback).**
Central mechanism of the transcript with scroll; the doc only specified
clipping by excess. Resolved in [api.md](api.md) §9.1: `blit` clips at
**both ends** (negatives clip the leading edge), it's **copy, not
re-render**, and virtualization belongs to the toolkit. The counterindications
that refined the resolution: pinning down the semantics of the negative,
guaranteeing it doesn't re-render, and recognizing that it doesn't solve
virtualization (the "cache the Block, move the offset" pattern in the guide
§6).

**G29 — Mouse in global coordinates with no translation to region (hit-testing).**
The temptation was `Region:hit(x,y)`, but it would only do the trivial half
(subtract the origin the plugin already set); the valuable half (which
block/line of a scrolled Block) needs the layout the plugin owns, not the
core. Resolved as a **toolkit convention** (option c), the same split as G1
(relayout) and G22 (theming) — guide §6.

**G30 — Pasting an image is not expressible; the `paste` event only carries text.**
Resolution (decided): pasting non-text content **injects a path**, not the
bytes — the core dumps the clipboard image to a session temp file and the
`paste` event carries `path`; the UI inserts it just like an `@` mention and
the agent decides whether to read it. It keeps binaries out of the text/JSON
boundaries (consistent with G11) and is distinct from P6 (rendering images
on screen, postponed). Applied to [api.md](api.md) §9.3.

Confirmations (no new API): all three areas — the multiline editor with a
real cursor, the `@`/`/` popups, the live spinner, and the tool renderers —
are built **entirely** on top of `nu.ui` + the `chat` contract. The
conclusion of the question that opened the round holds: the TUI of a coding
harness doesn't "come out of the core" — the core provides the substrate and
`chat.md` already is that harness. The only cracks (G28, G29) are matters of
**`nu.ui` ergonomics**, not missing mechanism.


---

# Round 7: per-model reasoning (`thinking`) control

An area the previous rounds didn't torture: **requesting** extended
reasoning from the model (not receiving its `thinking` blocks, already
validated — they travel with their signature in `meta`, §2.2 — but the
*request* parameter of the canonical request). The trigger is real: the
project's default model is `claude-opus-4-8`, from the family that changed
the way reasoning is requested.

## Scenario 32: enabling reasoning on two models with the canonical contract

```lua
-- A plugin (or a future agent feature) wants to enable reasoning per
-- turn. With ONLY today's canonical contract (providers.md §2.1) the only
-- way is `thinking = { budget }`.

agent.hook("request.pre", function(req, ctx)
  req.thinking = { budget = 8000 }   -- the only thing the canonical form knows how to express
  return req
end)

-- (a) "Legacy" model (extended thinking with budget): EXPRESSIBLE.
--     The anthropic adapter translates { budget = 8000 } -> the wire
--     { type = "enabled", budget_tokens = 8000 }, which those models accept.

-- (b) Opus 4.6+ (claude-opus-4-8, the DEFAULT model): the SAME code
--     produces the SAME wire { type = "enabled", budget_tokens = 8000 } -> the real
--     API responds with 400: that family REMOVED budget_tokens and expects
--     { type = "adaptive" }. The canonical contract has NO WAY to request
--     "adaptive": there's nothing `req.thinking` can carry to express it, and
--     the adapter —a faithful translator— can't invent what the canonical form leaves unsaid.
--                                                                        [G34]
```

Verdict: branch (a) is expressible; (b) is **not**. The canonical model
only knows how to request reasoning by *budget* (`budget`), a form modern
models reject, and it offers no "adaptive" *mode*. It's a crack in the
**canonical model** (not in the adapter, which follows the frozen contract to
the letter): vocabulary is missing to express the reasoning mode, and the
**data** of which form each model understands is missing. **[G34]**

> Note: the crack is **latent** today —the headless agent doesn't fill in
> `req.thinking` when assembling the turn (§2 step 2), so the 400 only shows
> up via a `request.pre` hook like the one above, or via a future reasoning
> control feature—. It was tortured and resolved **before** wiring up
> thinking so that feature is born on top of an already-correct canonical
> form.

---

## Findings (round 7)

**G34 — the canonical `thinking` model doesn't express adaptive mode.**
Resolved in [ADR-016](adr.md#adr-016--modelo-canónico-de-thinking-con-mode-y-traducción-por-modelo-en-el-adaptador)
(which **reopens and closes [P21](pospuesto.md)**, postponed until today): the
canonical parameter grows by addition to `thinking = { mode?: "off"|"adaptive"|"budget",
budget? }` (with `{budget=N}` as a compatible alias for `mode="budget"`), and
the **reasoning dialect of each model is declared as data** in
`providers.toml` (`thinking = "adaptive"|"budget"|"none"`), which the adapter
reads to translate per-model. The adapter remains a pure translator
(ADR-003/ADR-005): zero model-version tables in the code. Recorded in
[problemas.md](problemas.md#g34) (G34). It's the first crack born from
*using* the binary against the reality of a provider's API (the 400 from
Opus 4.6+), not from internal incompleteness.

---

# Round 8: a distributed mesh of agents ("kubernetes for agents")

Stress test question: if the **unit of work is the branch/worktree** and
coordination travels over git (or an external broker), can a third party
assemble, using only the public contract, a mesh of headless `nu -e` nodes
that execute **two-layer declarative specs** (reusable Role + Job instance),
support **fork-as-replication** (locally and across machines), and put the
human at the boundaries that matter (the Roles and the merges, never the
torrent of turns)? The round's hard hypothesis is **pull-only**: nu only acts
as a client — no listener, P1/P19 stay dormant. Out of scope, as in Round 5:
the non-determinism of the model's *sampling* (territory of
[providers.md](providers.md); the "replay adapter" from scenario 27 remains
the escape hatch for full reproducibility). Findings G38-G40 at the end.

## Scenario 33: mesh node with git CAS claim

The two-layer spec, as pure TOML data. The **Role** (the *who*) was
reviewed by a human and travels versioned in the repo; the **Job** (the
*what*) gets stamped out in bulk by a controller. Human attention is spent
on the layer that changes slowly.

```
roles/reviewer.toml                     jobs/J-0142.toml
-------------------                     ----------------
model = "anthropic/opus"                role   = "reviewer"
[permissions]                           base   = "9f3c1e..."   # pinned sha, NEVER
allow = ["read", "grep", "glob",        branch = "fleet/J-0142"  # a branch name
         "edit", "bash:pytest *"]       territory = ["src/parser/**"]
[budget]                                prompt = "review and fix ..."
max_turns = 40
max_cost_usd = 2.0
[[skills]]
name = "review"
hash = "b52f..."    # git hash-object: when the substrate is git, git is the hasher
```

```lua
-- plugins/fleet/node.lua — runs with `nu -e node.lua` on each machine: the
-- headless engine is free by design (agente.md §1); default deny without supervision (§5).
local agent = require("agent")

-- The claim is a distributed CAS with NO server of its own: creating a ref on
-- the remote is atomic — if another node created it first, the push is rejected.
local function claim(job_id)
  local r = nu.proc.run({ "git", "push", "origin",
                          "HEAD:refs/nu/claims/" .. job_id })          -- ⏸
  return r.code == 0                               -- you lost the race = code ≠ 0 ✓
end

-- Cross-machine liveness: the lock in sesiones.md §6 (pid + proc.alive) is
-- deliberately LOCAL — a pid means nothing on another machine. The pattern
-- here is heartbeat: re-push the claim-ref with a commit {hostname, ts}
-- using --force-with-lease (CAS again: only whoever holds the claim gets a heartbeat), and
-- re-claim by staleness with a generous threshold (different wall clocks:
-- nu.sys.now_ms() isn't synchronized across nodes). Fully expressible with
-- nu.proc; it's a PATTERN for the guide, not missing API.

local function run_job(job, role)
  local wt = nu.fs.tmpdir() .. "/" .. job.id                           -- ⏸
  nu.proc.run({ "git", "worktree", "add", wt, job.base })              -- ⏸
  nu.task.cleanup(function()
    nu.proc.run({ "git", "worktree", "remove", "--force", wt })
  end)

  local s = agent.session{                 -- spec→opts is a PURE FUNCTION: all
    model = role.model,                    -- the opts were JSON-able by contract ✓
    cwd = wt,                              -- the physical territory is the worktree
    permissions = role.permissions,        -- headless: what isn't listed is denied (§5)
    skills = skill_names(role.skills),
  }

  -- HARD budget in the driver, not on faith: send() runs the whole turn,
  -- so the cost cap is watched via events (G3 attribution) and cut
  -- with cancel — same split as max_turns, which is already opt-in.
  local sub = nu.events.on("agent:message", function(p)
    if p.session == s.id and s.usage.cost_usd > role.budget.max_cost_usd then
      s:cancel()                                   -- P22; closes as canceled (§4)
    end
  end)
  nu.task.cleanup(function() sub:cancel() end)

  local msg = s:send(job.prompt)                                       -- ⏸
  commit_and_push(wt, job.branch)          -- the BRANCH is the result; the merge is
                                           -- the human gate, outside this node
  attach_transcript(wt, s.id)              -- ...and the audit trail travels with the work
end

-- attach_transcript wants to commit the session's JSONL INSIDE the branch:
-- whoever reviews the diff will have right next to it how it was reached. sesiones.md is
-- documented as "public convention: any external tool can read
-- sessions" (§1), and the path is data_dir()/sessions/<project>/<id>.jsonl with
-- "<project> = cwd encoded as a slug" (§2). But the cwd→slug algorithm is NOT
-- written down anywhere: the promise of third-party reading can't be
-- exercised without guessing the encoding.                    [FINDING G38]
```

Verdict: the entire node — atomic claim, heartbeat, worktree, spec→session,
hard budget, result-branch — is written with `nu.proc` + `nu.fs` +
`nu.toml` + the `agent` contract. A single crack, and it's a specification
one: the transcript's path is unfindable for the third party the contract
invites to read it. **[G38]**

## Scenario 34: fork tournament (fork-as-replication, local)

"Replicating" an agent isn't cloning a process — its units aren't fungible,
unlike pods: the value is in the transcript. Replicating is **forking a
history**: K variants that share the entire prefix (context *and* prompt
cache: with P31's breakpoints, the fan-out costs marginally little in
input). Tournament as the exit: deterministic verifiers filter, a judge
ranks, the human only merges.

```lua
local root = agent.session{ model = M, cwd = repo }
root:send("study the bug in #412 and write a plan")   -- ⏸ the prefix is paid ONCE

local NUDGES = {
  "apply the plan minimizing the diff",
  "apply the plan; refactor if it simplifies",
  "discard the plan if you find a shorter path",
}

local fns = {}
for i, nudge in ipairs(NUDGES) do
  fns[i] = function()
    local v = root:fork()                  -- new session with meta.parent ✓ (sesiones §5)
    -- ...and here the tournament gets stuck: the variant needs ITS OWN worktree (the
    -- remedy from G16: physical territory per branch), but fork() doesn't accept opts
    -- — there's no way to re-house it in another cwd or trim its permissions or
    -- change its model per variant. What does it inherit from the parent? That's
    -- not written either. The natural workaround is to close and reopen with ephemeral opts (§2, G18):
    --     v:close()
    --     v = agent.session{ resume = v.id, cwd = worktree(i) }
    -- ...but `close` appears in the status note in §2 ("implemented
    -- send/spawn/set_model/close") and NOT in the contract's signature: the workaround
    -- rests on a method that officially doesn't exist.          [FINDING G39]
    local msg = v:send(nudge)                                          -- ⏸
    return { id = v.id, dir = worktree(i) }
  end
end
local variants = nu.task.all(fns)          -- ⏸ K streams in real parallel,
                                           -- results aligned with inputs ✓ (G27)

-- Tournament. First line ALWAYS deterministic: a human shouldn't see anything
-- a machine could have rejected.
local alive = {}
for i, v in ipairs(variants) do
  local t = nu.proc.run({ "pytest", "-q" }, { cwd = v.dir })           -- ⏸
  if t.code == 0 then alive[#alive + 1] = v end
end
-- Second line: a read-only LLM judge ranks the survivors.
local judge = agent.session{ model = M, permissions = { allow = { "read" } } }
local ranking = judge:send(render_diffs(alive))                        -- ⏸
-- Third line: the human merges the winner. The losers get discarded at
-- ZERO cost (worktree torn down) — the slop doesn't even get to exist as a branch.
```

The tournament's *rewind* axis (forking at an EARLIER point, not at the
head) trips on the same thing twice: `fork(at)` doesn't define what `at`
indexes (JSONL entry, message, turn? — `meta.parent = {id, entry}` suggests
entries, but it's implicit), and to *choose* the point the orchestrator has
to read the transcript and count — which again requires locating the file
(G38, second bite).

Verdict: the tournament composes entirely — fan-out from Round 5,
verifiers via `nu.proc`, a read-only judge, a human at the merge — except
that **fork doesn't re-house**: without `opts` (cwd/permissions/model per
variant) and with `at` lacking a defined unit, fork-as-replication falls one
step short. **[G39]** (Plan B — fresh subagents with the plan in the prompt
— loses exactly what made the fork valuable: the fidelity of the shared
prefix and its cache.)

## Scenario 35: distributed fork — the transcript travels in the branch

```lua
-- Node A left its transcript inside the result-branch (scenario 33). A
-- fork-job asks to continue THAT history on another machine with another nudge:
--
--   jobs/J-0177.toml
--   ----------------
--   role = "fixer"
--   parent_branch = "fleet/J-0142"
--   parent_transcript = ".nu/transcript.jsonl"
--   fork_at = 12
--   nudge = "parser tests pass; now fix the lexer ones"

-- Node B — importing someone else's session = COPYING the file to its place. This is
-- P9's promise ("the JSONL format is the API") put to a real test:
local raw  = nu.fs.read(wt .. "/" .. job.parent_transcript)            -- ⏸
local meta = nu.json.decode(first_line(raw))     -- {id, cwd, created, parent?} ✓ (§3)
nu.fs.write(sessions_dir(cwd_B) .. "/" .. meta.id .. ".jsonl", raw)    -- ⏸
--          ^^^^^^^^^^^^^^^^^^^
--          again: what's the project directory called? The slug from
--          sesiones.md §2 left unspecified, third bite.       [G38]

-- Reopen and fork. Replay (§3) doesn't flinch: meta.cwd points to a path
-- from A that doesn't exist here, but it's metadata, not state that gets re-executed ✓.
-- The lock doesn't get in the way either: A's .lock didn't travel (nobody commits it), and if the
-- directory arrived synced with someone else's lock, §6 already covers it:
-- different hostname → "can't be verified: it's asked, never assumed" ✓.
local parent = agent.session{ resume = meta.id, cwd = wt }   -- acquires the lock (§6)
local v = parent:fork(job.fork_at)               -- does fork_at count entries? [G39]
-- Friction noted without escalating it: B opens the parent AS A WRITER (resume) just to
-- fork it — a "read-only fork" doesn't exist. Here it's harmless (nobody else
-- writes that file on B), but the lock is conceptually redundant. If G39 ends up
-- in fork(at, opts), the resume-to-fork pairing deserves a line in the guide.
```

Verdict: the distributed fork **is** copying a file — P9 comes out
reinforced from its first real test: neither the replay, nor the locks, nor
the ids (timestamp+randomness, no machine state) raise any objection. The two
notches are the ones already open: locating the destination directory (G38)
and the semantics of `fork(at)` (G39). None new — a good sign for the
format.

## Scenario 36: contrast broker + denial as data

```lua
-- (a) The SAME node on the other substrate: a broker nu connects to
-- OUTBOUND (pull-only holds; P1/P19 stay dormant).
local ws = nu.ws.connect("wss://broker.example/fleet")                 -- ⏸
while true do
  local job = nu.json.decode(ws:recv())          -- ⏸ claim and liveness: from the broker
  local result = run_job(job.spec, job.role)     -- ⏸ the run_job from scenario 33,
  ws:send(nu.json.encode(result))                -- ⏸ UNTOUCHED: the Role/Job layer
end                                              --   doesn't know which substrate it's traveling on ✓
-- Honest tradeoff: plain git pays for claim+liveness with CAS+heartbeat but doesn't
-- add infrastructure; the broker gives them away for free but it's one more piece to operate.
-- That run_job is identical in both is the fact that matters: the spec is
-- substrate-agnostic.

-- (b) Asynchronous permission escalation: default deny AS A MECHANISM. The
-- model asks for `bash:npm install`; the Role doesn't list it; headless denies it with the
-- actionable error from §5 ("add allow = [\"bash:npm *\"]"). The controller
-- wants to turn that into a Role amendment that a human approves and a
-- cheap re-run (the job is idempotent: pinned sha). How does it capture WHAT got
-- denied? Let's torture the three paths:

nu.events.on("agent:permission.asked", function(p) end)
-- ✗ doesn't apply: asked is the interactive flow; a policy deny doesn't even ask

agent.hook("permission", function(p) end)
-- ✗ not this either: the pipeline is deny → allow → hooks (§5) — a deny from the list
--   cuts the chain BEFORE reaching the hooks; it's invisible to them

nu.events.on("agent:tool.end", function(p) end)
-- ✗ not even specified whether it's emitted for a denied call (its handler
--   never ran), and its payload doesn't carry the denied pattern

-- What's left is reading the transcript: the tool_result with is_error carries the
-- actionable prose — PERFECT for a human, useless for a controller, which
-- ends up parsing prose looking for the pattern. The denial needs to travel
-- as DATA.                                               [FINDING G40]
```

Verdict: the contrast confirms the declarative layer doesn't depend on the
substrate, and the deny → Role amendment → idempotent re-run loop is the
asynchronous human-in-the-loop the mesh needed — the friction of default
deny turned into the escalation mechanism, with no new transport. It's
missing just one piece of data: the denied pattern in structured form.
**[G40]**

---

## Findings (round 8)

**G38 — the project slug for `sessions/<proyecto>/` is unspecified.**
[sesiones.md](sesiones.md) §1 promises that "any external tool can read
sessions", but §2 encodes the directory as "cwd slug" without writing down
the algorithm: the promise can't be exercised. The round needed it three
times (committing the transcript in the branch, counting entries to choose
the fork point, importing someone else's session). **Resolved**: the
algorithm becomes part of the format ([sesiones.md](sesiones.md) §2, frozen
as-is with its properties — readable, lossy, a grouping key and not an
identity) and the extension exposes it as `sessions.slug/dir`; detail and
counterindications in [problemas.md](problemas.md#g38).

**G39 — `Session:fork` doesn't re-house: no `opts`, and `at` has no defined unit.**
Fork-as-replication requires its own worktree (cwd) per variant — G16's
remedy — and sometimes different permissions/model; `fork(at?)` doesn't
accept opts, doesn't document what it inherits, and the workaround (close +
`resume` with ephemeral opts) rests on a `close` the contract's signature
omits. Also, `at` doesn't define what it indexes (the unit of
`meta.parent.entry` is implicit). **Resolved**: `fork(at?, opts?)` (ephemeral
opts, permissions only trim) and `close()` enter the contract
([agente.md](agente.md) §2), `at` indexes the current message history,
inheritance is fully specified, and copying the prefix is blessed — the
self-contained child makes the transcripts travel ([sesiones.md](sesiones.md)
§5). Detail in [problemas.md](problemas.md#g39).

**G40 — permission denials aren't observable as data.**
A policy deny cuts off before the `permission` hooks, `permission.asked`
only belongs to the interactive flow, `tool.end` doesn't specify whether it's
emitted for denied calls, and the actionable error from §5 is prose. A
headless orchestrator can't turn denials into Role amendments without
parsing text. **Resolved**: every denial produces a structured object
(`{ id, tool, args?, source, pattern?, suggested? }`) with two destinations —
the `agent:permission.denied` event for live observers and the `tool_result`'s
`meta` so the denial travels with the transcript —, and `tool.end` is now
also specified for denials ([agente.md](agente.md) §4/§5). Detail in
[problemas.md](problemas.md#g40).

Confirmations (no new API): the **distributed claim** is an atomic ref
push and the heartbeat a `--force-with-lease` — CAS twice, all with
`nu.proc` (§33); the **hard budget** is watched from the driver with events
(G3) + `Session:cancel`, the same split as `max_turns` (§33); the **Role/Job
spec is substrate-agnostic** — the same `run_job` runs over plain git and
over an outbound `nu.ws` broker, and the pull-only hypothesis holds up
through the whole round without waking P1/P19 (§36); the **distributed fork
is copying a file** — P9 ("the format is the API") comes out reinforced from
its first real test (§35); and the tournament validates the **anti-slop
pyramid**: deterministic verifiers → read-only judge → human only at the
merge, with losing variants discarded at zero cost (§34).

Patterns for the guide (no API change): claim by atomic ref creation +
heartbeat with lease + re-claim by staleness with a generous threshold
(unsynchronized clocks) (§33); two-layer spec with skills pinned by hash
(`git hash-object` as the house hasher when the substrate is git) (§33); cost
cap in the driver via `agent:message` + `cancel` (§33); deterministic-first
tournament over disposable worktrees (§34); transcript committed in the
result-branch so the audit trail travels with the work (§33/§35); denial →
Role amendment → idempotent re-run as asynchronous human-in-the-loop (§36).
