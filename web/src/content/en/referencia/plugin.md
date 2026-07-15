---
title: nu.plugin — plugins and loader
description: nu's plugin system — structure, loader, identity by name, boot order, reload and config directories.
---

`nu.plugin` and the loader are how `nu` loads the code that turns it into something.
Remember: **everything** —the agent, the chat, the providers— is a plugin; the
official extensions have no privilege. Main state only (except
`nu.config.dir`/`data_dir`, which are **[W]**).

## What a plugin is

A directory with two files:

- `plugin.toml` — metadata: `name`, `version`, `requires?: string[]`.
- `init.lua` — runs on load.

The plugin's `lua/` subdirectory gets added to `require`'s paths, so
plugins can require each other (composability).

```toml
# plugin.toml
name = "mi-plugin"
version = "0.1.0"
requires = ["agent"]   # loads after 'agent'
```

```lua
-- init.lua
local agent = require("agent")
-- registers tools, commands, keymaps... using only the public API
```

## Identity by name

The **name is the identity** of the plugin, and the loader keeps it **unique**: the
user's directory *replaces* the embedded extension of the same name (they don't
coexist), and two plugins with the same name are an actionable load error.
That uniqueness is what lets event namespaces and other registries be
collision-free by simple convention (namespace = plugin name), without
the core reserving any name.

## Canonical boot order

```
core → activated plugins (topological by requires) → user's init.lua → core:ready
```

The user's `init.lua` runs **last** on purpose: as in the input
stack the most recent registration wins, the user has the last word (keymaps,
theme, overrides) by construction, without a priority system.

The embedded official extensions (`go:embed`) load first but only if
`plugins.enabled` (in `nu.toml`) names them —**inactive by default**, ADR-010—.

## API

### `nu.plugin.current`

```
nu.plugin.current() -> { name, version, dir }
```

The plugin in whose context the code runs. The core uses it to tag
handles by owner (which is what makes `reload` possible).

### `nu.plugin.list`

```
nu.plugin.list() -> { name, version, source: "builtin"|"user", enabled }[]
```

```lua
for _, p in ipairs(nu.plugin.list()) do
  nu.log.info("%s %s (%s) %s", p.name, p.version, p.source,
    p.enabled and "active" or "inactive")
end
```

### `nu.plugin.reload` ⏸

```
nu.plugin.reload(name)
```

**Development** tool, *best-effort*: releases the plugin's handles, emits
`core:plugin.unload` (extensions clean up their registrations), clears the
plugin's `require` cache and reloads its `init.lua`. A plugin with exotic
global effects may not unload cleanly: it's for **iterating, not for
production**.

## Directories

```
nu.config.dir() -> string       [W]   -- ~/.config/nu (or equivalent)
nu.config.data_dir() -> string  [W]   -- ~/.local/share/nu (or equivalent)
```

`config.dir()` is where `nu.toml`, `providers.toml` and plugin config
live; `data_dir()` is for data (logs, sessions).

```sh
nu -e 'return nu.config.dir() ~= nil, nu.config.data_dir() ~= nil'
```

```
true
true
```

:::note[Runtime configuration]
`config.dir()/nu.toml` governs the core itself: which plugins get activated, extra
plugin paths and the watchdog budget. A broken `nu.toml` or a
`plugins.enabled` naming something nonexistent is an actionable boot error
that points to the line to fix.
:::
