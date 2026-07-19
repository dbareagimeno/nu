---
title: "Los checks de producto de `enu doctor` presuponen consultar a las extensiones sin efectos y una API de herramientas externas — ninguna de las dos existe"
type: "hallazgo"
id: "G62"
status: "resuelto"
date: "2026-07-18"
origin: "escenarista BDD de la sesión S50 (enu doctor)"
affected: ["adr-026 §pieza 3", "doctor.md", "S50"]
---
# G62 · Los checks de producto de `enu doctor` presuponen introspección de extensiones que no existe — ADR-026 pieza 3 / doctor.md

**Problema.** El catálogo de `doctor.v1` ([doctor.md](../ops/doctor.md)) y
[ADR-026](../decisions/adr/adr-026-subcomandos-de-gestion-del-binario.md) pieza 3
mandan que los checks de **producto** consulten a las extensiones «por la API
pública, nunca re-implementando su semántica en Go». El escenarista BDD de S50
destapó que **cuatro checks** no tienen ruta de invocación posible hoy:

- **`provider.model`** (¿el modelo por defecto resuelve contra `providers.toml`?)
  y **`provider.key`** (¿está la variable `api_key_env`?): la API pública para
  ambos existe (`providers.resolve`, y `providers.secret_env_vars()` de G55),
  **pero la única forma de invocar Lua de una extensión hoy es `Boot()`**, que
  ejecuta el `init.lua` de **todos** los plugins activados y emite `core:ready`
  (`internal/runtime/runtime.go`). Un doctor «de solo lectura, sin efectos»
  arrancaría de hecho el runtime entero. Ni doctor.md ni ADR-026 definen un
  **modo de consulta sin efectos** (arranque parcial que cargue solo la
  extensión a consultar, o un contrato de que los `init.lua` oficiales son
  libres de efectos y por tanto un `Boot()` completo es aceptable para doctor).
- **`tools.external`** (¿las herramientas externas que declaran las extensiones
  activas están en `PATH`?): peor — el **dato no existe en ninguna parte**. No
  hay mecanismo, ni en Lua ni en Go, por el que una extensión declare de forma
  consultable «uso el binario `git`» o «uso `rg`»: las tools del agente son
  código Lua fijo, sin metadatos de dependencias externas. Este check exige una
  **API de introspección nueva** (p. ej. campo en `plugin.toml` o función
  pública que agregue lo que cada plugin declara).
- **`provider.reach`**: hereda el mismo hueco de consulta (más el opt-in de red,
  que sí está especificado).

Es el mismo patrón que [G61](g61-el-wizard-de-init-ofrece-providers-sin-plantilla.md):
la espec presupone algo que no existe. Además, dos incoherencias editoriales
menores del propio doctor.md: `binary.version` menciona «que `--version`
responde» (flag inexistente: se quitó en S48/ADR-027) y `config.parse` usa un
solo `id` para tres ficheros sin decir cómo se reporta un fallo parcial.

> ✅ **RESUELTO (2026-07-18) — opción (a): S50 v1 implementa solo los checks
> kernel; los de producto salen como `skip` honesto.** Elegida por el operador.
> `enu doctor` v1 implementa los **siete checks kernel** —`binary.version`,
> `config.dir`, `config.parse`, `sessions.perms`, `tty.caps`,
> `plugins.enabled`, `plugins.requires`— (los dos últimos exponiendo un método
> del Runtime que envuelve `discover()`+`topoSort()` sin `Boot()`: reusa el
> loader, no re-implementa nada). Los **cuatro checks de producto**
> (`provider.model`, `provider.key`, `tools.external`, `provider.reach`) salen
> con `status: "skip"` y la pista en `detail` apuntando a este hallazgo
> (`remedy: null`, la regla del esquema: `remedy` solo en `fail`): `doctor.v1`
> **nunca miente con un `ok` fabricado**. El diseño de la introspección que
> necesitan se difiere como [P45](../postponed/pospuesto.md).
>
> **Aplicación:** nota de estrechamiento en ADR-026 pieza 3 (puntero a este
> hallazgo, sin reescribir la decisión); doctor.md marca los cuatro checks como
> «v1: skip (no implementado, G62)», corrige `binary.version` (reporta versión
> desde los símbolos del binario, sin `--version`) y aclara `config.parse` (un
> `id`, `detail` lista los tres ficheros, `remedy` nombra el roto); P45
> registrado; la fila de S50 acota a los siete checks kernel.
>
> Que los cuatro checks estén **en el catálogo** desde v1 con `skip` es
> deliberado: sus `id` quedan reservados y estables, y activarlos cuando P45 se
> resuelva es adición legítima (pasar de `skip` a `ok`/`fail`), no un cambio de
> esquema.

**Disparador de reapertura.** — (resuelto). Los cuatro checks de producto
reviven con P45, cuando exista el diseño del mecanismo de consulta a extensiones
sin efectos y la API de declaración de herramientas externas.
