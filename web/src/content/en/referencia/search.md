---
title: nu.search — search
description: Recursive listing, repo-scale parallel grep and fuzzy matching for pickers.
---

`nu.search` is repository-scale search: the embodiment of "Lua
decides, Go executes" —every function is a Go primitive, parallel inside—.
Available in workers **[W]**.

## `nu.search.files` ⏸ [W]

```
nu.search.files(root, opts?) -> string[]
```

**Recursive** listing respecting `.gitignore`. `opts`: `glob`, `hidden`, `max`.

```sh
nu -e '
nu.task.spawn(function()
  local md = nu.search.files(".", { glob = "*.md", max = 3 })
  nu.fs.write(nu.fs.tmpdir().."/f.txt", nu.json.encode(md))
end)
return "ok"
'
```

```lua
nu.task.spawn(function()
  -- all Lua files in the repo, including hidden ones
  local luas = nu.search.files(".", { glob = "**/*.lua", hidden = true })
  return #luas
end)
```

## `nu.search.grep` ⏸ [W]

```
nu.search.grep(pattern, opts) -> iterator
```

Content search, **parallel inside**. Returns an **iterator** that
emits `{ path, line_no, line, ranges }` as results arrive (doesn't wait to
have them all). `opts`: `root`, `glob`, `case`, `max`.

```lua
nu.task.spawn(function()
  local n = 0
  for hit in nu.search.grep("TODO", { root = ".", glob = "*.go" }) do
    n = n + 1
    nu.log.info("%s:%d  %s", hit.path, hit.line_no, hit.line)
    -- hit.ranges marks where it matched within the line
  end
  return n
end)
```

## `nu.search.fuzzy` [W]

```
nu.search.fuzzy(query, candidates: string[], opts?) -> { index, score }[]
```

**Fuzzy** ordered matching, for pickers. **Synchronous and bounded** (it's the
picker's hot primitive, called on every keystroke): it's not ⏸. Returns the
indices (1-based, over `candidates`) with their score, best to worst.

```sh
nu -e 'return nu.json.encode(nu.search.fuzzy("ab", { "axb", "ba", "cab" }))'
```

```
[{"index":1,"score":10},{"index":3,"score":6}]
```

The candidate `"ba"` doesn't match "ab" in order, so it's discarded; `"axb"` (index
1) scores higher than `"cab"` (index 3).

```lua
-- Incremental picker: re-filter on every keystroke.
local function filtrar(query, items)
  local res = {}
  for _, m in ipairs(nu.search.fuzzy(query, items)) do
    res[#res + 1] = items[m.index]
  end
  return res
end
```
