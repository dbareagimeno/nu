---
title: Your first agent
description: Activate the official extensions, configure an LLM provider, and run a headless agent turn with nu -p.
---

The coding harness is `nu`'s *killer app*, but —true to the principle that
the core doesn't know what an agent is— the agent is an **extension**. This
page takes you from a bare runtime to a working agent turn.

:::note[Requires network access and an API key]
Unlike the rest of the manual, this flow talks to a real LLM: it needs a
connection and an API key. The commands are correct, but their output
depends on your provider.
:::

## 1. Activate the official extensions

The official extensions ship embedded in the binary but **inactive by
default**. The agent needs three: `providers`, `sessions`, and `agent`.
Activate them in `nu.toml`, inside `nu.config.dir()` (normally
`~/.config/nu/`):

```toml
# ~/.config/nu/nu.toml
[plugins]
enabled = ["providers", "sessions", "agent"]
```

If you launch the agent without activating them, the error is actionable: it
names exactly this line of `nu.toml`.

## 2. Declare a provider

LLM providers are declared as **data** (TOML), not code. Edit
`providers.toml` in the same config directory:

```toml
# ~/.config/nu/providers.toml
[providers.anthropic]
adapter     = "anthropic"
base_url    = "https://api.anthropic.com"
api_key_env = "ANTHROPIC_API_KEY"        # never the key itself in the file

[[providers.anthropic.models]]
id      = "claude-opus-4-8"
context = 200000
aliases = ["opus"]
```

The key **never** goes in the file: it's read from the environment
(`api_key_env`).

```sh
export ANTHROPIC_API_KEY="sk-..."
```

A model is named `"provider/id-or-alias"`: `"anthropic/opus"`.

## 3. A headless turn with `nu -p`

`nu -p '<prompt>'` runs **a single headless agent turn** and writes the
assistant's final text to stdout. It's the scripting/CI mode: the agent
engine is headless by design, so it doesn't need an interactive terminal.

```sh
nu -p 'summarize the README of this project in three lines'
```

Select the model with `--model` (overrides the one in `agent.toml`):

```sh
nu -p 'what does this repo do?' --model anthropic/opus
```

### Permissions in headless mode

Sensitive tools (writing files, running commands, network access) **are
denied in headless mode** unless you authorize them: there's no UI to ask.
To grant them in a non-interactive run, use `--auto-permissions` (the risk
is chosen, not inherited):

```sh
nu -p 'create an initial CHANGELOG.md file' --auto-permissions
```

If a tool is denied for lack of permission, `nu` exits with **code 3**
(distinct from the 1 for a runtime error) so a script can distinguish "the
model couldn't act due to permissions" from a real failure.

### Continuing the last session

`--continue` (or `-c`) resumes the project's most recent session (the cwd)
before sending the prompt:

```sh
nu -p 'now add tests' --continue
```

## 4. The same thing from Lua

`nu -p` is sugar over the `agent` extension's public API. This is,
essentially, what it does under the hood —and what you'd write yourself in
an `init.lua` or a script—:

```lua
local agent = require("agent")

nu.task.spawn(function()
  local s = agent.session{ model = "anthropic/opus", cwd = nu.fs.cwd() }
  local final = s:send("summarize the README in three lines")  -- ⏸ runs the turn
  s:close()

  -- The final Message concatenates its text blocks.
  local text = ""
  for _, b in ipairs(final.content) do
    if b.type == "text" then text = text .. b.text end
  end
  nu.fs.write(nu.fs.tmpdir() .. "/respuesta.txt", text)
end)
```

That the CLI uses exactly the same public API you'd use is the principle in
action: the official UI has no privileged access. If something about the
agent couldn't be built with the public API, it would be the API that's
incomplete.

## Next step

You now have the harness working. From here, the [API
reference](/nu/en/api/convenciones/) documents every core primitive that
all of this is built on.
