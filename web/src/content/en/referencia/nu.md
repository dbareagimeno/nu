---
title: nu — root
description: Runtime version, API level, and capability detection with nu.has.
---

The root namespace exposes the runtime version and capability detection. It's
the first thing any plugin that wants to be portable touches.

## `nu.version` [W]

```
nu.version -> { major, minor, patch, api: integer }
```

Runtime version and **API level** of the core. `api` is the number that grows
with every addition to the sacred surface; use it to require a minimum, but
prefer [`nu.has`](#nuhas-w) to detect concrete capabilities.

```sh
nu -e 'return nu.json.encode(nu.version)'
```

```
{"api":2,"major":0,"minor":1,"patch":0}
```

```lua
-- Require a minimum API level.
assert(nu.version.api >= 2, "this plugin needs api >= 2")
```

## `nu.has` [W]

```
nu.has(cap: string) -> boolean
```

Capability detection for portable extensions. Returns whether a capability is
available in this runtime/environment. It covers both fine-grained traits
(`"ui.images"`, `"net.tcp"`) and **whole modules**: in headless mode `nu.ui`
doesn't exist, and `nu.has("ui")` is the correct way to know that — never
probe and catch the error.

```sh
nu -e 'return nu.has("ui")'
```

```
false
```

(In `nu -e` there's no TTY, so `nu.ui` doesn't exist and `nu.has("ui")` is
`false`.)

```lua
-- Degrade gracefully depending on the environment.
if nu.has("ui") then
  -- paint a region
else
  -- headless mode: text only, to stdout/log
end
```

:::tip[Capabilities, not versions]
`nu.has` is the recommended detection mechanism over comparing
`nu.version.api`. A capability can be absent because of the *environment*
(headless, terminal without image support), not just the API level.
:::
