---
title: nu.json / toml / yaml — codecs
description: Encoding and decoding of JSON, TOML and YAML, with the NULL sentinel and strict UTF-8 handling.
---

`nu.json`, `nu.toml` and `nu.yaml` are the codecs. All available in workers
**[W]** and none suspends: they work on in-memory strings.

## `nu.json` [W]

```
nu.json.encode(v, opts?) -> string   -- opts.pretty
nu.json.decode(s) -> v
```

```sh
nu -e 'return nu.json.encode({ a = 1, b = { 2, 3 } })'
```

```
{"a":1,"b":[2,3]}
```

With formatting:

```sh
nu -e 'return nu.json.encode({ a = 1 }, { pretty = true })'
```

```
{
  "a": 1
}
```

Decoding:

```sh
nu -e 'local v = nu.json.decode("[10,20,30]"); return v[1] + v[2] + v[3]'
```

```
60
```

### `null` and the `NULL` sentinel

JSON `null` ↔ `nu.json.NULL` (a sentinel), so as **not to lose keys**: if
it mapped to `nil`, the key would disappear from the Lua table.

```lua
local v = nu.json.decode('{"x": null}')
-- v.x == nu.json.NULL  (the key "x" exists; it wasn't lost)
if v.x == nu.json.NULL then -- ...
```

### Strict about UTF-8

`encode` throws `EINVAL` on invalid bytes: sanitizing is a **visible** decision
of whoever has the context (the tool), never something the codec does behind your back.

```lua
local ok, err = pcall(function() return nu.json.encode({ s = bytes_crudos }) end)
if not ok and err.code == "EINVAL" then
  -- you decide how to sanitize; the codec doesn't do it on its own
end
```

## `nu.toml` [W]

```
nu.toml.encode(v) -> string
nu.toml.decode(s) -> v
```

```sh
nu -e 'local v = nu.toml.decode("nombre = \"nu\"\nversion = 2"); return v.nombre, v.version'
```

```
nu
2
```

TOML is `nu`'s configuration format (`nu.toml`, `providers.toml`,
`plugin.toml`), so this codec is the one plugins use to read their own
config.

## `nu.yaml` [W]

```
nu.yaml.encode(v) -> string
nu.yaml.decode(s) -> v
```

Needed for metadata from the existing ecosystem (skills frontmatter): YAML
is too treacherous to parse in pure Lua.

```sh
nu -e 'local v = nu.yaml.decode("nombre: nu\ntags:\n  - cli\n  - lua"); return v.nombre, #v.tags'
```

```
nu
2
```
