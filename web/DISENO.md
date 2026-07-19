# Diseño de la web de `enu`

Guía de diseño **viva** de la web de documentación. Es el destilado del *handoff*
de diseño original (`design_handoff_enu_web/`, retirado en la reorganización de
2026-07-19): recoge las **reglas duras** y las **decisiones de forma** que quien
edite `web/` debe respetar para que el sitio siga leyéndose como lo que es.

Concepto rector: **la web ES un terminal**. La portada replica la pantalla de
arranque de `enu`, la wiki es un pager tipo `less(1)`, la referencia es una
página por namespace `enu.*`, y toda la navegación funciona con teclado real. La
marca no es un color sino una **paleta de terminal** (theme `enu`, cian por
defecto) intercambiable por themes famosos, reflejando la hiperconfigurabilidad
del producto.

> **Fuente de verdad de los valores.** Este documento fija la *gramática* y las
> *proporciones*. Los **valores concretos vigentes** (colores de token, fuentes,
> tamaños) viven en el código y **mandan sobre las tablas de aquí** cuando
> difieran: [`src/styles/tokens.css`](src/styles/tokens.css) para los tokens y la
> tipografía. El handoff era **pre-accesibilidad**; la auditoría web
> [`docs/audits/auditoria-web-diseno-2026-07-15.md`](../docs/audits/auditoria-web-diseno-2026-07-15.md)
> lo endureció:
> - **W-02** subió el token `--dim` de cada theme para cumplir contraste WCAG AA
>   (p. ej. `enu` `#4e686e` → `#63848b`). Las tablas de más abajo conservan los
>   valores **originales** del handoff como referencia histórica; los de
>   `tokens.css` son los buenos.
> - **W-03** pasó la prosa del cuerpo de todo-mono a **IBM Plex Sans** (`--font-prose`),
>   rompiendo a propósito la regla original «una sola familia mono». El *chrome*
>   sí sigue en mono.

## Sitemap y navegación

```
/            portada (pantalla de arranque)
/docs/...    wiki-pager (un doc .md por página)
/api/...     referencia enu.* (un namespace por página)
/plugins     guía «tu primer plugin»
/404         E404
```

Nav de primer nivel en el header de **todas** las páginas internas:
`docs · api · plugins` (la activa se pinta invertida: texto en `bg` sobre fondo
`key`). La portada **no** lleva esa nav — su menú `[i][d][a][g]` es la nav.

## Gramática visual del terminal (reglas duras)

- **Wordmark**: `enu` en caja invertida (`background:bright; color:bg;
  padding:2px 9px; weight:700`). Ancla de marca constante en todos los themes.
  Sirve también de favicon/OG (que se **generan** del wordmark, no hay imágenes).
- **Citas**: prefijo `│` en `dim` + cursiva. **Nunca** `border-left` de CSS.
- **Títulos**: subrayado con caracteres `═` (h1) y `─` (h2) en `dim`.
- **Teclas**: `[x]` — corchetes en `dim`, letra en `key` weight 600.
- **Cursor**: bloque 9×17px en `key`, blink 1.1s `step-end`
  (`@keyframes`: 0–49% opacity 1, 50–100% opacity 0). Respeta
  `prefers-reduced-motion`.
- **Selección activa en listas**: fila invertida (`bg` sobre `key`) con prefijo `▸`.
- **Statusline**: una sola, abajo, en todas las páginas. Izquierda = contexto
  (`docs/core/filosofia.md · 8% · 1/15`, `api/fs · 8% · 1/12`,
  `plugins/primer-plugin · 1/3`, `404 · 0/15`); derecha = teclas disponibles.
- **Todo el chrome en minúsculas**; la prosa con capitalización normal.
- **Prohibido**: emojis, iconos SVG, `border-radius` (todo es rectangular),
  sombras, gradientes.

Extras de plataforma: `::selection` con `background:key; color:bg`; scrollbar
estilizado (thumb `border`, track `bg`); foco visible =
`box-shadow: inset 0 0 0 1.5px key` (los contenedores capturan teclado con
`tabindex`). El cambio de theme es **instantáneo, sin `transition`**.

## Tipografía

- **Chrome** (headers, statusline, código): **IBM Plex Mono** (400/500/600),
  fallback `ui-monospace, monospace`.
- **Prosa del cuerpo**: **IBM Plex Sans** (`--font-prose`, ~15px/1.7) desde W-03.
- Escala (referencia; los valores vigentes en `tokens.css`):
  - Slogan de portada: 28px/1.4, weight 600 (a viewport completo admite 36–44px
    manteniendo las jerarquías relativas).
  - Títulos de página (wiki/api): 20px weight 600, con línea `═` en `dim` debajo.
  - Subtítulos de sección: 13.5–14px weight 600 con línea `─`.
  - Cuerpo: line-height ~1.7–1.95, **max-width ~68ch**.
  - Chrome (headers/statusline): 10.5–11px.

## Design tokens — los 4 themes

8 tokens por theme, como CSS custom properties bajo `[data-theme="…"]` en
`<html>`; persistidos en `localStorage`; default `enu`. **Los valores vigentes
están en [`src/styles/tokens.css`](src/styles/tokens.css)** (con `--dim` subido
por W-02). La tabla siguiente es la **original del handoff** (histórica):

| token | enu (default) | dracula | gruvbox | solarized (light) |
|---|---|---|---|---|
| bg | #0a1416 | #282a36 | #282828 | #fdf6e3 |
| fg | #a9bfc4 | #f8f8f2 | #ebdbb2 | #657b83 |
| bright | #e8f4f6 | #ffffff | #fbf1c7 | #073642 |
| dim *(pre-W-02)* | #4e686e | #6272a4 | #a89984 | #7d8e91 |
| border | #16292d | #3b3d51 | #3c3836 | #eee8d5 |
| key (acento) | #4fcadb | #ff79c6 | #fe8019 | #2aa198 |
| c2 (verde sem.) | #7fc8a8 | #50fa7b | #b8bb26 | #859900 |
| c3 (cian sem.) | #4fcadb | #8be9fd | #8ec07c | #268bd2 |
| c4 (ámbar/rojo sem.) | #c9a06a | #f1fa8c | #fabd2f | #cb4b16 |

**Semántica de uso** (esto sí es contrato vivo): `key` = teclas de menú, acento
del slogan, cursor, links, elemento activo (invertido). `dim` = prompts, chrome,
separadores de texto. `bright` = títulos y texto enfatizado. `border` = líneas
divisorias **y** fondo de bloques de código. `c2/c3/c4` = semánticos (strings
Lua, rutas, errores — E404 usa `c4`).

## Dimensiones por pantalla

- **Portada `/` (11a)**: columna flex a viewport completo. Header (borde inferior
  `border`): wordmark a la izquierda; a la derecha `theme:` y `lang:` como texto
  plano clicable (activo entre corchetes, en `key` weight 600; resto en `dim`).
  Cuerpo centrado; prompt interactivo `> ▊` y una línea de feedback de altura
  fija. Footer: `apache-2.0` izquierda, `vX.Y.Z` derecha (link a releases).
- **Wiki `/docs/<slug>` (12a)**: grid 3 columnas — sidebar **240px** (borde
  derecho) + contenido (max **~68ch**) + carril derecho **180px** (`§ en esta
  página` + metadata «última edición / commit»).
- **API `/api/<namespace>` (13a)**: misma estructura, sidebar **210px**;
  **cards de función** (borde `border`, firma con nombre en `key` 600, params en
  `fg`, retorno en `dim`; sufijo `· [permiso]` si pasa por permisos) + ejemplo
  Lua etiquetado «pruébalo con `enu -e`».
- **Plugins `/plugins` (14b)**: 2 columnas — contenido + carril **230px**. Tres
  pasos numerados con código real; card de `examples/`
  (`XDG_CONFIG_HOME=examples enu`).
- **404 (14a)**: chrome completo; `E404: no es un documento del editor` en `c4`;
  sugerencia por distancia de edición contra el manifest de rutas (build-time).
- **Móvil (<768px, 14d/14e)**: sin prompt (no hay teclado físico); el menú de
  portada pasa a **filas táctiles** de ancho completo (≥48px; la primaria `[i]`
  con borde `key`); en wiki el sidebar y el índice van tras un botón `[≡]`
  (drawer), el carril derecho desaparece.

## Interacción y comportamiento

**Teclado global (desktop), módulo compartido:**
- Portada: `i/d/a/g` con input vacío ejecutan directo (`i` copia el `curl` al
  portapapeles y lo notifica en la línea de feedback; `d/a/g` navegan). Otro
  texto + Enter → `enu: «X» no encontrado — prueba [i], [d], [g] o help`.
- Modo comando (portada con `>` visible; páginas internas tras pulsar `:`, que
  convierte la statusline en prompt): `help`, `theme <nombre>`, `lang <es|en>`,
  `open <doc>`, `q`, `i/d/a/g`. Comando desconocido en páginas internas →
  `E492: no es un comando del editor: X` (guiño a vim).
- `help` responde: `comandos: i · d · g · theme <enu|dracula|gruvbox|solarized>
  · lang <es|en>` + segunda línea `…y si sabes lua, ya sabes qué hacer` (el
  **único** anuncio del easter egg).
- **Easter egg**: comando `lua` → mini-REPL (el prompt cambia a `lua>` en `key`).
  Evalúa aritmética, `print("…")`, concatenación `..` y `enu.version` (la versión
  vigente, centralizada en [`src/lib/const.ts`](src/lib/const.ts)). `salir` /
  `exit` / `q` para volver. Mensaje de entrada:
  `lua 5.4 (embebido en enu) — escribe salir para volver`.
- Pager (docs/api): `j/k` scroll, `n/p` doc/namespace siguiente/anterior, `/`
  búsqueda, `q` → portada. `Backspace` con prompt vacío o `Escape` cierran el
  modo comando/búsqueda. El teclado **nunca** captura si el foco está en un
  `input`/`textarea`.

**Búsqueda (14c):** overlay sobre la página actual; la statusline izquierda se
vuelve `/término▊`. Resultados agrupados por documento; término resaltado
invertido en el resultado activo (con `◂`). Teclas `[n/p]` saltar, `[enter]`
abrir, `[esc]` cerrar. Índice build-time (pagefind).

**Estado y persistencia:**
- `theme: 'enu'|'dracula'|'gruvbox'|'solarized'` y `lang: 'es'|'en'` en
  `localStorage` (aplicar el theme inline en `<head>` antes del primer paint para
  evitar el flash).
- `commandMode: ''|'cmd'|'search'|'repl'` + buffer de texto + línea de feedback.
- Posición de lectura (% para la statusline) derivada del scroll.

**i18n:** el **chrome** entero es bilingüe es/en; el **contenido** de docs solo
existe en español por ahora (en EN, la nota «in spanish for now» en el header de
las páginas de contenido). El español es la fuente de verdad (ver
[`README.md`](README.md) §«Contenido en inglés»).

## Copy de la portada

- **ES** — slogan: «Tu agente de código.» / «Tus reglas.» · cuerpo: «Instálalo
  con una línea. Úsalo tal cual, o cámbialo entero escribiendo Lua. enu es el
  coding harness que es tuyo de verdad.»
- **EN** — slogan: «Your coding agent.» / «Your rules.» · cuerpo: «Install it
  with one line. Use it as is, or rewrite the whole thing in Lua. enu is the
  coding harness that is truly yours.» · la entrada `[d]` dice «documentation —
  the enu wiki · in spanish for now».

## Decisiones abiertas (no bloquean)

- **Dominio**: el `curl` de instalación usa un placeholder centralizado en
  [`src/lib/const.ts`](src/lib/const.ts) (`DOMAIN`), pendiente de decisión.
- Traducción EN del contenido de docs (hoy «in spanish for now»).
- El drawer `[≡]` de móvil abierto no está especificado al detalle: usar la misma
  lista del sidebar de desktop a ancho completo.
