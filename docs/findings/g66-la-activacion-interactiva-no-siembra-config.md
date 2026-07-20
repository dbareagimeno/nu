---
title: "El onboarding interactivo de primer arranque es pasivo: la degradación con gracia del chat manda a editar los TOML a mano en vez de configurar; debería ser un setup navegable (Lua) tras activar"
type: "hallazgo"
id: "G66"
status: "abierto"
origin: "auditoría de UX del onramp de primer arranque (plan de unificación del onramp); reencuadrado tras el verificador clean-room"
affected: ["chat.md §8", "agente.md §10", "ADR-017 pieza 2", "P44"]
---
# G66 · El onboarding interactivo de primer arranque es pasivo: la degradación del chat manda a editar los TOML a mano — `chat.md` §8 / `agente.md` §10 / ADR-017 pieza 2 / P44 — **ABIERTO**

> **Nota de reencuadre (2026-07-20).** Este hallazgo se registró primero como «la
> activación interactiva "activar el conjunto oficial" no siembra config, a
> diferencia del flag: bug / paridad de ADR-015 rota / dead-end». Un `verificador`
> clean-room **refutó ese encuadre** (FALSO POSITIVO): (1) el código cumple
> `api.md` §14 al pie de la letra — §14 especifica *a propósito* que la acción
> interactiva escribe solo `plugins.enabled` y que el flag *además* siembra
> plantillas; (2) la invariante de ADR-015 («la pantalla desnuda y el flag activan
> exactamente lo mismo») se refiere al **conjunto de plugins** (`officialProductSet`),
> no a los efectos de disco (la siembra ni existía cuando se escribió ADR-015); (3)
> el «dead-end» es la **degradación con gracia** que ADR-017 pieza 2 diseñó a
> propósito, y es **salible** (`esc`/`q`/`ctrl+c`), no una trampa. Por tanto **no es
> un bug de correctitud**. Lo que sí es cierto —y es el hallazgo real— es que esa
> superficie de onboarding es **pasiva**.

**Problema.** El primer arranque interactivo, tras activar el conjunto oficial
desde la pantalla desnuda, **está diseñado** para caer en la degradación con gracia
del chat (ADR-017 pieza 2). Pero esa degradación es **pasiva**: una pantalla de
solo-texto ([chat.md](../contracts/chat.md) §8) que te **manda a editar
`agent.toml`/`providers.toml` y la variable de la API key a mano**, con un único
keymap para **salir**. Para un producto que quiere ser accesible (la tesis de
adquisición de ADR-025), mandar a un recién llegado a editar TOML a mano en el
primer arranque es una **fuga de UX/adopción**. La superficie de onboarding que el
diseño ya eligió existe, pero no **hace** el onboarding: lo delega en el usuario.

**Frontera (dictada por `juez-filosofia`).** El arreglo va en **Lua**, en esa
superficie de degradación (o una extensión `onboarding` dedicada), **alcanzada tras
activar** — **nunca** en la pantalla del kernel (que se queda hablando solo de su
vocabulario: versión, rutas, plugins) ni **renderizado en Go pre-Lua**. El
onboarding de producto (provider/modelo/clave) es vocabulario de producto → es de
una extensión (idea central 1 + corolario de completitud: `enu.ui`, `enu.sys.env`,
`enu.fs.write`, `providers.list`/`resolve` ya bastan).

**Impacto.** La primera experiencia interactiva — la superficie de adopción. No
bloquea (es salible), pero un onboarding pasivo mina justo lo que ADR-025 quiere.

**Opciones.**

- **(a) [Recomendada] Convertir la degradación pasiva en un setup NAVEGABLE en
  Lua.** Tras activar: elegir preset de provider → modelo (lo **aporta el usuario**,
  onramp provider-neutral — ver P44) → guía de la clave (detecta la variable, nunca
  la escribe; Ollama sin clave) → escribe la config (`enu.fs.write`) → transición al
  chat; camino rápido solo si la config ya resuelve del todo. **Refina ADR-017 pieza
  2**; toca `chat.md` §8 y `agente.md` §10. **Depende** de los presets
  provider-neutral (**P44 reabierto**). ADR nuevo.

- **(b) Extensión oficial `onboarding` dedicada** (en vez de enriquecer el chat).
  Alternativa; más superficie nueva. A decidir en el ADR.

- **(c) Solo un default silencioso desde el kernel** (el viejo "Track A":
  `ActivateOfficial` siembra una plantilla). **Rechazada**: no da elección navegable
  (lo que se pidió), es un cambio a diseño deliberado de todos modos, y `juez-filosofia`
  lo declaró **subsumido** por el setup Lua.

> **Estado.** **ABIERTO.** Dirección **acordada** (opción (a)); **implementación
> pendiente de priorización** (registro completo en la ficha de plan del onramp).
> **Depende de [P44](../postponed/p44-wizard-init-multi-provider.md)** (presets
> provider-neutral) y, al implementarse, **refina ADR-017 pieza 2**.
