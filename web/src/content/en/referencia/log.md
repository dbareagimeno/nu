---
title: nu.log — logging
description: File logging with level and source plugin. print is an alias for nu.log.info.
---

`nu.log` is the runtime's logging. Available in workers **[W]**. Writes **to a
file** in `data_dir`, with the source plugin annotated automatically —
**never to the screen**: the UI belongs to the extensions, not the core—.

## Levels

```
nu.log.debug(fmt, ...) [W]
nu.log.info(fmt, ...) [W]
nu.log.warn(fmt, ...) [W]
nu.log.error(fmt, ...) [W]
```

`fmt` uses `string.format`'s format:

```lua
nu.log.info("procesados %d ficheros en %d ms", n, dur)
nu.log.warn("reintentando: %s", err.message)
```

## `print` is `nu.log.info`

In `nu`'s Lua baseline, `print` is **redirected to `nu.log.info`**: it goes to the
log, not stdout. This is deliberate —screen IO goes through `nu.ui` or through
`nu -e`'s return values—.

```sh
nu -e 'print("esto va al log, no aquí"); return "esto sí sale"'
```

```
esto sí sale
```

:::caution[Don't use print for user output]
If you want something to appear on the terminal: in headless, return it with `return`
(in `nu -e`) or write it to stdout via the corresponding extension; in a TUI,
paint it with [`nu.ui`](/nu/en/api/ui/). `print`/`nu.log.*` are for
diagnostics, and their destination is the log file at
[`nu.config.data_dir()`](/nu/en/api/plugin/#directories).
:::
