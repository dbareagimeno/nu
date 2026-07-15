---
title: nu.text / nu.re — text
description: Cell width, wrap, truncation, markdown, syntax highlighting, diff and RE2 regex.
---

`nu.text` gathers text rendering and processing operations —the
quadratic-on-screen ones, in Go— and `nu.re` the regex engine. Both available in
workers **[W]** and neither suspends.

Several functions return a **Block** (an opaque handle of styled lines)
that gets stamped with [`Region:blit`](/nu/en/api/ui/#surface). The ones
that return plain values (`width`, `truncate`) are tested directly with
`nu -e`.

## `nu.text.width` [W]

```
nu.text.width(s) -> integer
```

Width in **cells** (not bytes nor runes): correctly counts graphemes,
east-asian characters and emoji.

```sh
nu -e 'return nu.text.width("café"), nu.text.width("日本")'
```

```
4
4
```

(`café` takes 4 cells; `日本` also: two wide characters.)

## `nu.text.truncate` [W]

```
nu.text.truncate(s, width, opts?) -> string
```

Truncates to `width` cells, with an optional ellipsis.

```sh
nu -e 'return nu.text.truncate("hola mundo", 7, { ellipsis = "…" })'
```

```
hola m…
```

## `nu.text.wrap` [W]

```
nu.text.wrap(s, width, opts?) -> Block
```

Word-wrap to `width` cells. Returns a Block ready for `blit`. With
`opts.style` —a [Style](../ui/) `{ fg?, bg?, bold?, italic?, underline?, reverse? }`—
every line of the Block comes out with that default style.

```lua
local block = nu.text.wrap("un párrafo largo que no cabe en una línea", 20)
-- region:blit(0, 0, block)

-- styled: each line in the accent color, bold.
local aviso = nu.text.wrap("atención: esto es importante", 20, {
  style = { fg = "#ffcc00", bold = true },
})
```

## `nu.text.markdown` [W]

```
nu.text.markdown(s, opts) -> Block
```

**Full** markdown render at `opts.width`, themable. Accepts incomplete
input (**streaming-safe**): you can re-render on every delta of an LLM without
it breaking on half-closed markdown.

```lua
local block = nu.text.markdown("# Título\n\nUn **párrafo**.", { width = 80 })
```

## `nu.text.highlight` [W]

```
nu.text.highlight(code, lang, opts?) -> Block
```

Syntax highlighting of `code` for language `lang`.

```lua
local block = nu.text.highlight("local x = 1\nreturn x", "lua")
```

## `nu.text.diff` [W]

```
nu.text.diff(a, b, opts?) -> { hunks, block? }
```

Structured diff between `a` and `b`. With `opts.render = true` also returns the
painted Block.

```lua
local d = nu.text.diff(viejo, nuevo, { render = true })
for _, h in ipairs(d.hunks) do
  -- inspect each hunk
end
-- region:blit(0, 0, d.block)
```

## `nu.re` — RE2 regex [W]

```
nu.re.compile(pattern) -> Re
  Re:match(s) -> caps?            -- nil if it doesn't match
  Re:find_all(s) -> ranges
  Re:replace(s, repl) -> string
```

**RE2** engine (linear, no catastrophic backtracking). Compile once, reuse.

```sh
nu -e '
local re = nu.re.compile("(\\w+)@(\\w+)")
return nu.json.encode(re:match("usuario@dominio"))
'
```

```
["usuario@dominio","usuario","dominio"]
```

`match` returns the full capture followed by the groups. Replace:

```sh
nu -e 'return nu.re.compile("\\d+"):replace("a1b22c333", "#")'
```

```
a#b#c#
```

:::note[No "tokens" here]
There is no LLM token estimation in this module: "token" is product
vocabulary, and the heuristic (~4 bytes/token) is pure Lua that doesn't justify a
primitive. It lives in the providers extension (`providers.approx_tokens`).
:::
