---
title: nu.events — event bus
description: nu's generic event bus — on, once, emit, and synchronous dispatch semantics.
---

`nu.events` is a generic event bus. The core doesn't know what an agent is:
this bus is where extensions define their own hooks. It's only available in
the **main state** (not in workers).

## Naming convention

Names are `"namespace:event"`, in **two levels**. The core reserves only its
own — `core:` and `ui:`; every other namespace belongs to a plugin by
convention (namespace = plugin name). Since the loader guarantees a plugin's
name is unique, two extensions never collide. Official ones have no
privilege: `agent:` is the namespace of the `agent` plugin, just like
`my-plugin:` is yours.

## `nu.events.on`

```
nu.events.on(name, fn) -> Sub
  Sub:cancel()
```

Subscribes `fn` to the `name` event. Handlers are **synchronous**, run in
registration order, and each one runs under `pcall` (a handler that throws
doesn't bring down the others). Returns a `Sub` with `Sub:cancel()`.

```lua
local sub = nu.events.on("my-plugin:saved", function(payload)
  -- react; synchronous, so for IO: nu.task.spawn(...)
  nu.log.info("saved: %s", payload.path)
end)
-- ...
sub:cancel()
```

## `nu.events.once`

```
nu.events.once(name, fn) -> Sub
```

Like `on`, but fires **only once** and cancels itself.

```lua
nu.events.once("core:ready", function()
  nu.log.info("runtime ready")
end)
```

## `nu.events.emit`

```
nu.events.emit(name, payload?)
```

Dispatches the event **synchronously** on the main state. `payload` is
optional (any table).

```sh
nu -e '
local seen
nu.events.on("demo:hello", function(p) seen = p.who end)
nu.events.emit("demo:hello", { who = "nu" })
return seen
'
```

```
nu
```

## Dispatch semantics

Fine-grained rules, important when a handler modifies subscriptions:

- Each `emit` runs over the **snapshot** of subscribers taken at emit time.
- **Canceling** takes effect immediately: if it hasn't reached you yet, you
  no longer run.
- Subscriptions **made during** a dispatch only see future events.
- **Nested** `emit`s are **queued** and dispatched once the current one
  finishes (breadth, not depth): no recursion, no overflow. An infinite
  ping-pong between plugins turns into a flat loop that the watchdog cuts
  off.

## Events the core emits

`core:ready`, `core:shutdown`, `core:plugin.loaded`, `core:plugin.unload`,
`core:plugin.error`, `core:plugin.misbehaved`, `ui:resize`, `ui:focus`,
`ui:suspend`/`ui:resume`.

:::note[For product hooks, look at the extension]
Events like `agent:tool.start` or `agent:message` aren't from the core: they
are emitted by the `agent` extension. Its catalog lives in the agent's
contract, not here.
:::
