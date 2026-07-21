---
title: "`enu.proc.spawn`/`run` ignoran en silencio un `env` que no sea tabla (p. ej. array `[\"K=V\"]`, la forma POSIX/declarativa natural), y `api.md` §6 no documenta la forma de `env`"
type: "hallazgo"
id: "G65"
status: "resuelto"
date: "2026-07-20"
origin: "resolución de G59 (parte 2): el arreglo se aplicó en la capa mcp (normalize_env), dejando viva la grieta del primitivo"
resolution: "Opciones (d)+(e): enu.proc.run/spawn aceptan env como tabla {K=V} O array POSIX [\"K=V\"] (partido por el primer =; entre claves repetidas gana la última), y un env malformado lanza EINVAL en vez de ignorarse en silencio; env presente —aunque vacío— reemplaza el heredado (§6 lo fija por fin como letra del contrato). Adición a la superficie sagrada: enu.version.api 5→6, sin ADR (la permisividad rota nunca fue contrato público). La dimensión reemplazo-vs-fusión se escinde a P55 con su disparador."
affected: ["enu.proc (api.md §6, §17)", "internal/runtime/vmwasm_proc.go (parseProcArgsWasm)", "internal/runtime/enu.go (APILevel 6)", "internal/runtime/embedded/mcp/lua/mcp/init.lua (normalize_env, prosa)", "docs/postponed/p55-fusion-de-entorno-en-proc.md"]
---
# G65 · `enu.proc.spawn`/`run` ignoran en silencio un `env` que no sea tabla — `enu.proc` / `api.md` §6 — **RESUELTO**

> ✅ **RESUELTO (2026-07-21).** Decidido por el operador: opciones **(d)+(e)**
> compuestas. `enu.proc.run`/`spawn` aceptan `env` como tabla `{ K = V }` **o** array
> POSIX `["K=V"]`, y el env malformado pasa de ignorarse en silencio a **`EINVAL`**.
> Adición a la superficie sagrada: `api.md` §6 documenta por fin la forma y semántica
> de `env`, `enu.version.api` sube a **6** (§17). Pasó por `juez-filosofia`
> (APROBADA CON MATICES, los cuatro incorporados). La dimensión reemplazo-vs-fusión
> queda fuera deliberadamente → [P55](../postponed/p55-fusion-de-entorno-en-proc.md).

**Resolución** (2026-07-21).

1. **(d) Forma dual de `opts.env`.** `parseProcArgsWasm`
   (`internal/runtime/vmwasm_proc.go`, parser compartido por `run` y `spawn`) acepta
   la tabla `{ K = V }` (como siempre) y, nuevo, el array `["K=V", ...]` — la forma
   POSIX/`exec`/docker. Cada entrada se parte por el **primer** `=` (el valor puede
   contener `=`) y entre claves repetidas gana la **última** (la semántica de
   `exec.Cmd.Env`). Retrocompatible: quien pasa tabla no cambia. La justificación NO
   es el corolario de completitud (la conversión array→tabla ya se componía en Lua,
   como demostró `normalize_env`): es que el array es **vocabulario POSIX legítimo
   del kernel**, que el parser se tocaba igualmente por (e), y que el footgun del
   silent-ignore ya costó una investigación e2e (G59).
2. **(e) Fail-closed para lo malformado.** Un `env` que no sea ni tabla válida ni
   array válido —un escalar, una entrada de array no-string o sin `=` o con clave
   vacía, una tabla con clave vacía o con `=` o con valor no-string— lanza
   **`EINVAL`** con mensaje que enseña las dos formas esperadas. No necesita ADR: la
   «permisividad» rota vivía solo en el comentario de cabecera del parser, nunca en
   `api.md` — no hay rotura de firma ni de semántica contratada (ADR-025 pieza 4 solo
   exige ADR para roturas de firma).
3. **Caso borde `{}`.** La tabla Lua vacía cruza el wire como array vacío; hoy caía
   en el silent-ignore y **heredaba**, contradiciendo la semántica de reemplazo que
   los contratos ya citaban (agente.md §3, guia-plugins.md). Ahora reemplaza con
   entorno vacío, y §6 fija por fin esa letra («presente —aunque vacío— reemplaza»)
   en el propio contrato, autocontenido.
4. **mcp conserva `normalize_env`** como validación temprana: sus errores nombran el
   servidor de `mcp.toml` ofensor (un `EINVAL` genérico del primitivo no puede), el
   `pcall` por-servidor degrada solo ese servidor, y resuelve la tabla mixta que la
   frontera no distingue. Además devuelve `nil` ante un `env` declarado vacío (vacío
   = «nada que añadir» = heredar): evita que un `env = []` en `mcp.toml` pase a
   reemplazar-con-vacío y rompa el servidor por perder `PATH`.
5. **Fuera de alcance (deliberado).** La dimensión reemplazo-vs-fusión (falta
   `enu.sys.environ()` o un modo «merge»; un servidor que declara un secreto pierde
   `PATH`/`HOME`) se escinde a [P55](../postponed/p55-fusion-de-entorno-en-proc.md)
   con su disparador — este G se resuelve solo en su eje (d)+(e).

**Aplicada en:** `docs/contracts/api.md` (§6: forma y semántica de `env`; §17: nivel
6), `internal/runtime/enu.go` (`APILevel = 6` + crónica), `internal/runtime/vmwasm_proc.go`
(`parseProcArgsWasm`: ramas map/array + EINVAL), `internal/runtime/vmwasm_proc_test.go`
(matriz G65), `internal/runtime/embedded/mcp/lua/mcp/init.lua` (`normalize_env`: prosa
post-G65 + vacío→nil), `docs/postponed/p55-fusion-de-entorno-en-proc.md` (nuevo). El
texto de abajo queda como registro histórico del problema y las opciones.

---

**Problema.** `parseProcArgsWasm` (`internal/runtime/vmwasm_proc.go`) solo interpreta
`env` como `map[string]any` (tabla Lua `{ K = V }`). Un `env` como **array** de strings
`["K=V", ...]` —la forma POSIX/`exec`/docker, y la que `mcp.toml` documentó por
ergonomía— cruza la frontera como `[]any`, la aserción de tipo falla, y `env` se
**ignora en SILENCIO** (sin error, sin warning): el subproceso hereda el entorno del
padre sin las claves declaradas. Es permisividad **deliberada** (la cabecera de la
función lo declara: "un env no-tabla ... se ignora en silencio; solo un argv malo o un
timeout_ms negativo → EINVAL"), pero convierte un error de forma en un fallo mudo.
Además, **`api.md` §6 no documenta la forma de `env`** en absoluto —solo lo lista como
clave de `opts`—: la distinción tabla-vs-array vive únicamente en un comentario del
código Go.

Esta es la **raíz a nivel de primitivo** de la parte 2 de
[G59](g59-el-auto-connect-de-mcp-toml.md). G59 se resolvió **en la capa mcp**
(`normalize_env` traduce el array→tabla en el borde TOML→spawn, con error accionable si
viene mal formado), sin tocar la API sagrada, porque el umbral que el propio G59 fijó
para arreglar el primitivo era "el patrón env-array es vocabulario natural de **más de
un llamador**", y hoy **solo mcp** lo usa (el único otro llamador Lua que pasa `env`,
`internal/runtime/sys_test.go`, usa la tabla). El footgun del primitivo queda **vivo**
para el próximo autor de plugin.

**Impacto.** Cualquier futuro llamador de `enu.proc.run`/`spawn` que pase `env` como
array (la forma declarativa natural) obtiene, sin aviso, un subproceso sin ese entorno
—exactamente el fallo mudo que costó a G59 una investigación e2e—. Solo mcp está
protegido, y solo porque normaliza por su cuenta. Es una grieta latente de la superficie
pública, no un bug de mcp.

**Opciones a explorar** (no se decide aquí):

- **(d) `enu.proc.run`/`spawn` aceptan `env` como tabla `{ K = V }` O como array
  `["K=V"]`.** Edit en `parseProcArgsWasm` (rama `[]any` además de la `map[string]any`),
  cubre `run` y `spawn` (comparten parser). Es una **adición a la superficie sagrada**:
  documentar ambas formas en `api.md` §6, bump de `enu.version.api`, y pasar por
  `juez-filosofia`. Retrocompatible (quien pasa tabla no cambia). Al aplicarse, `mcp`
  puede retirar `normalize_env` (o dejarlo como validación accionable). Es el arreglo
  "correcto" el día que un **segundo** llamador quiera env-array.
- **(e) Silent-ignore de `env` mal formado → `EINVAL`** (fail-closed): un `env` que no
  sea tabla-ni-array-de-"K=V" lanza en vez de ignorarse. Hace ruidoso el error, pero
  **rompe la permisividad documentada** de `enu.proc` (un cambio de comportamiento más
  allá de una adición) y debe sopesarse contra esa coherencia de diseño. Componible con
  (d): aceptar array Y endurecer lo verdaderamente mal formado.

**Dimensión relacionada — reemplazo vs fusión.** Al resolver
[G59](g59-el-auto-connect-de-mcp-toml.md) (parte 2, opción c) afloró un segundo eje del
diseño de `env`: un `env` **presente** (no nil) **REEMPLAZA** el entorno heredado
(`api.md` §6, "control total por llamada"), no lo fusiona. Para `mcp.toml` eso es un
footgun: un servidor que declare `env = ["API_KEY=..."]` pierde `PATH`/`HOME` heredados,
así que un servidor node/npx/python (que necesita `PATH` para encontrar su intérprete o
lanzar subprocesos) **se rompe al declarar un solo secreto** —justo el caso de uso que
G59 quería habilitar—. Hoy mcp no puede fusionar en Lua: `enu.sys` expone leer **una**
variable (`enu.sys.env(name)`) y el overlay global de `enu.sys.setenv` (que NO sirve —es
proceso-global y filtraría el secreto a todo subproceso, contra G55), pero **no** hay una
`enu.sys.environ()` que enumere el entorno completo. Resolverlo limpiamente necesita, o
un lector del entorno completo, o un **modo "merge"** de `opts.env` en `enu.proc.spawn`
(fusionar lo declarado sobre lo heredado, distinto del "reemplazo" actual) —ambas,
adiciones a la superficie sagrada—. Se evalúa junto con (d)/(e).

**Disparador de reapertura.** Cuando un **segundo llamador** (más allá de mcp) quiera
pasar `env` como array —entonces (d) es corolario de completitud—, o cuando se decida
endurecer el silent-ignore de `env` mal formado, o ante cualquier edición del parseo de
`env` en `parseProcArgsWasm`. Ligado a la opción (d) de
[G59](g59-el-auto-connect-de-mcp-toml.md).
