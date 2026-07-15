---
title: nu.fs — filesystem
description: Reading, atomic writing, stat, listing, file manipulation, and filesystem watching.
---

`nu.fs` is filesystem access. Almost everything is **⏸** (suspending: it goes
inside a task) and **[W]** (available in workers), except `nu.fs.watch`, which
belongs only to the main state.

## `nu.fs.read` ⏸ [W]

```
nu.fs.read(path) -> string
```

Reads the whole file as a string. Throws `ENOENT` if it doesn't exist.

```sh
nu -e '
nu.task.spawn(function()
  local txt = nu.fs.read("README.md")
  nu.fs.write(nu.fs.tmpdir().."/n.txt", tostring(#txt))  -- number of bytes
end)
return "ok"
'
```

## `nu.fs.write` / `nu.fs.append` ⏸ [W]

```
nu.fs.write(path, data, opts?)
nu.fs.append(path, data)
```

**Atomic** write (via temp file + rename: you never leave a half-written
file). `opts.exclusive = true` creates **only if it doesn't exist**, in a
single indivisible operation (`O_EXCL`); if it already exists it throws
`EEXIST`. It's the building block for lockfiles.

```lua
nu.task.spawn(function()
  nu.fs.write("output.txt", "content\n")
  nu.fs.append("output.txt", "another line\n")

  -- Lockfile: only one wins the creation.
  local ok = pcall(function()
    nu.fs.write("app.lock", nu.sys.pid()..":"..nu.sys.hostname(), { exclusive = true })
  end)
  if not ok then error({ code = "EEXIST", message = "a process is already running" }) end
end)
```

## `nu.fs.stat` ⏸ [W]

```
nu.fs.stat(path) -> { size, mtime_ms, is_dir, mode }?
```

File metadata, or **`nil` if it doesn't exist** (it doesn't throw `ENOENT`:
that's the idiomatic way to check existence).

```lua
nu.task.spawn(function()
  local st = nu.fs.stat("config.json")
  if st and not st.is_dir then
    -- it exists and is a file
  end
end)
```

## `nu.fs.list` ⏸ [W]

```
nu.fs.list(dir) -> { name, is_dir }[]
```

Lists the directory **non-recursively**. For recursive listing that respects
`.gitignore`, use [`nu.search.files`](/nu/en/api/search/).

```sh
nu -e '
nu.task.spawn(function()
  local entries = nu.fs.list("docs")
  nu.fs.write(nu.fs.tmpdir().."/c.txt", tostring(#entries))
end)
return "ok"
'
```

## Manipulation ⏸ [W]

```
nu.fs.mkdir(path) ⏸ [W]
nu.fs.remove(path, opts?) ⏸ [W]  -- opts.recursive=true for non-empty dirs
nu.fs.rename(from, to) ⏸ [W]
nu.fs.copy(from, to) ⏸ [W]
```

```lua
nu.task.spawn(function()
  nu.fs.mkdir("build")
  nu.fs.copy("template.txt", "build/copy.txt")
  nu.fs.rename("build/copy.txt", "build/final.txt")
  nu.fs.remove("build", { recursive = true })
end)
```

## `nu.fs.tmpdir` ⏸ [W]

```
nu.fs.tmpdir() -> string
```

Temporary directory **scoped to the session** (cleaned up along with it).

## `nu.fs.cwd` [W]

```
nu.fs.cwd() -> string
```

Working directory, **immutable** during the session (subprocesses can receive
a different one via `opts.cwd`). Note: it's not ⏸, it can be called without a
task.

```sh
nu -e 'return nu.fs.cwd() ~= nil'
```

```
true
```

## `nu.fs.watch`

```
nu.fs.watch(path, opts?, fn) -> Watcher
  Watcher:stop()
```

Watches for filesystem changes. **Main state only**. `opts`:

- `recursive?` — watches subdirectories.
- `gitignore = true` — ignores what git ignores (watching `node_modules/` is
  noise).
- `debounce_ms = 50`.

Delivers **in batches**: `fn(events[])` with `{ path, kind }` where `kind` is
`"create"`, `"modify"`, or `"remove"`. A `git checkout` that touches thousands
of files arrives as **a single batch**. The handler is synchronous.
`Watcher:stop()` stops it.

```lua
local w = nu.fs.watch("src", { recursive = true, gitignore = true }, function(events)
  for _, e in ipairs(events) do
    nu.log.info("%s: %s", e.kind, e.path)
  end
end)
-- ...
w:stop()
```
