---
title: nu.sys — environment and clock
description: Platform, environment variables, wall and monotonic clocks, hostname and pid.
---

`nu.sys` exposes the process environment and clocks. Everything available in workers
**[W]** and nothing suspends: they are local queries.

## `nu.sys.platform` [W]

```
nu.sys.platform() -> "linux" | "darwin" | "windows"
```

```sh
nu -e 'return nu.sys.platform()'
```

```
linux
```

## `nu.sys.env` / `nu.sys.setenv` [W]

```
nu.sys.env(name) -> string?
nu.sys.setenv(name, value)
```

Reads and sets environment variables. `setenv` affects **only future subprocesses**
(it doesn't rewrite the environment of the `nu` process already running).

```lua
local home = nu.sys.env("HOME")
nu.sys.setenv("MI_FLAG", "1")   -- later nu.proc.run calls will see it
```

## `nu.sys.now_ms` / `nu.sys.mono_ms` [W]

```
nu.sys.now_ms() -> number   -- wall clock (epoch ms)
nu.sys.mono_ms() -> number  -- monotonic clock
```

Use `now_ms` for timestamps; use `mono_ms` to **measure durations** (it doesn't jump
with clock adjustments).

```sh
nu -e '
local t0 = nu.sys.mono_ms()
local s = 0; for i=1,1000 do s = s + i end
return (nu.sys.mono_ms() - t0) >= 0
'
```

```
true
```

## `nu.sys.hostname` [W]

```
nu.sys.hostname() -> string
```

Machine name. Together with `pid` it forms the **writer identity** for the
session locks.

## `nu.sys.pid` [W]

```
nu.sys.pid() -> integer
```

Pid of the `nu` process **itself**. Don't confuse it with
[`nu.proc.alive(pid)`](/nu/en/api/proc/#nuprocalive-w), which validates *other*
pids: `pid()` is your own.

```sh
nu -e 'return nu.sys.pid() > 0'
```

```
true
```

```lua
-- Writer identity for a lockfile.
local quien = nu.sys.hostname() .. ":" .. nu.sys.pid()
```

:::note[API level]
`nu.sys.pid()` was the first addition to the frozen API (it bumped `nu.version.api`
from 1 to 2). A good reminder that the surface **grows only by addition**.
:::
