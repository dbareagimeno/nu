---
title: Quick start
description: From zero to your first agent turn in three steps — activate the official set, declare a model, and launch a headless turn or the chat.
---

Freshly installed, `nu` is a **bare runtime**: the official extensions ship
embedded but **inactive by default** ([ADR-010](adr.md)) — the harness is a
choice you make, not a foregone conclusion. From zero to your first agent
turn in three steps:

```sh
# 1. Activate the official product set (agent, chat, providers, sessions,
#    mcp, toolkit, repl). Writes `plugins.enabled` to ~/.config/nu/nu.toml.
nu --default-config

# 2. Declare a model and export its key (see "Models and keys" below).
cat > ~/.config/nu/providers.toml <<'TOML'
[providers.anthropic]
adapter     = "anthropic"
base_url    = "https://api.anthropic.com"
api_key_env = "ANTHROPIC_API_KEY"

[[providers.anthropic.models]]
id      = "claude-opus-4-8"
context = 200000
aliases = ["opus"]
TOML
export ANTHROPIC_API_KEY="sk-..."

# 3. Launch a headless turn...
nu -p 'Summarize what this repository does' --model anthropic/opus

#    ...or open the interactive chat (in a terminal with a TTY):
nu
```

Without step 1, `nu` starts up at the **bare runtime screen** (with a TTY) or
fails with an actionable error naming the exact line of `nu.toml` that's
missing (without a TTY). Nothing happens by magic: every step is explicit
and reversible.
