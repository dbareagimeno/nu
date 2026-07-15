---
title: nu.proc — subprocesses
description: Running and controlling subprocesses — run with buffers, spawn with streams, and detecting live processes.
---

`nu.proc` launches subprocesses. Available in workers **[W]**. **No implicit
shell**: `argv` is an array of strings; whoever wants a shell invokes it
explicitly (`{"sh", "-c", "..."}`).

## `nu.proc.run` ⏸ [W]

```
nu.proc.run(argv: string[], opts?) -> { code, stdout, stderr }
```

Buffered convenience: runs, waits, and returns the full output. `opts`:
`cwd`, `env`, `stdin`, `timeout_ms`.

```sh
nu -e '
nu.task.spawn(function()
  local r = nu.proc.run({ "echo", "hello" })
  nu.fs.write(nu.fs.tmpdir().."/o.txt", nu.json.encode(r))
  -- r == { code = 0, stdout = "hello\n", stderr = "" }
end)
return "ok"
'
```

With standard input and a directory:

```lua
nu.task.spawn(function()
  local r = nu.proc.run({ "grep", "TODO" }, {
    cwd = "/project",
    stdin = "line1\nTODO: something\nline3\n",
    timeout_ms = 5000,
  })
  return r.stdout
end)
```

## `nu.proc.spawn` [W]

```
nu.proc.spawn(argv, opts?) -> Proc
```

Fine-grained control with streams (for long-running or interactive
processes). Returns a `Proc`:

```
Proc:write(data) ⏸ [W]                                  -- writes to stdin
Proc:close_stdin() [W]
Proc:read_line(which: "stdout"|"stderr") -> string? ⏸ [W]  -- nil at EOF
Proc:read(which, n?) -> string? ⏸ [W]                   -- raw read
Proc:wait() -> { code } ⏸ [W]
Proc:kill(signal?) [W]                                  -- TERM by default
```

```lua
nu.task.spawn(function()
  local p = nu.proc.spawn({ "cat" })
  nu.task.cleanup(function() p:kill() end)   -- safety net

  p:write("one line\n")
  p:close_stdin()

  local line = p:read_line("stdout")         -- "one line"
  local res = p:wait()                        -- { code = 0 }
end)
```

:::caution[Process lifetime]
The rule is to kill the process explicitly via
[`nu.task.cleanup`](/nu/en/api/task/#nutaskcleanup-w) in whoever creates
it. As a safety net, a `Proc` with no references ends up killed by the GC,
but that's **non-deterministic**: don't rely on it.
:::

## `nu.proc.alive` [W]

```
nu.proc.alive(pid: integer) -> boolean
```

Is there a live process with that `pid` on this machine? It reports
**existence, not identity**: a recycled pid gives `true`. Useful for
detecting orphaned locks (combine it with
[`nu.sys.pid`](/nu/en/api/sys/) and `nu.sys.hostname`).

```lua
-- Is the lock's owner still alive?
if not nu.proc.alive(lock_pid) then
  -- orphaned lock: it can be reclaimed
end
```
