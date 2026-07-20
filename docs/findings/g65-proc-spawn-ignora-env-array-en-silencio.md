---
title: "`enu.proc.spawn`/`run` ignoran en silencio un `env` que no sea tabla (p. ej. array `[\"K=V\"]`, la forma POSIX/declarativa natural), y `api.md` §6 no documenta la forma de `env`"
type: "hallazgo"
id: "G65"
status: "abierto"
date: "2026-07-20"
origin: "resolución de G59 (parte 2): el arreglo se aplicó en la capa mcp (normalize_env), dejando viva la grieta del primitivo"
affected: ["enu.proc (api.md §6)", "internal/runtime/vmwasm_proc.go (parseProcArgsWasm)"]
---
# G65 · `enu.proc.spawn`/`run` ignoran en silencio un `env` que no sea tabla — `enu.proc` / `api.md` §6

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
