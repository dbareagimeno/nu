---
title: "P35 — enu.plugin.reload best-effort ante colisión de nombres de módulo entre plugins (G2)"
type: "pospuesto"
id: "P35"
status: "vigente"
---
# P35 · `enu.plugin.reload` best-effort ante colisión de nombres de módulo entre plugins (G2)

**Dónde se pospuso.** `vmwasm/loader.go` / decisión de S13 (`decisiones-implementacion.md`); auditoría 2026-07-12, A-41

**Por qué.** El espacio de nombres de `require` es global y la purga de `package.loaded` del reload enumera solo los `.lua` del plugin recargado: dos plugins con un módulo `utils` y el reload de uno puede dejar cargado el del otro. La resolución registrada en S13 fue explícitamente «no resolver»: prefijar por plugin rompería el `require` relativo dentro del propio plugin y el caso real (dos plugins instalados con módulos homónimos, y además recargándose en caliente) es hoy hipotético. La garantía debilitada queda registrada aquí para que [guia-plugins.md](../contracts/guia-plugins.md) no venda el reload como infalible

**Disparador de reapertura.** El primer choque real de nombres entre plugins instalados (síntoma: tras `reload`, un módulo sirve la versión de otro plugin); o un package manager (P4→ADR-025: ya decidido, Fase 2; su construcción hace plausible instalar plugins de terceros no coordinados — al diseñar `enu plugin add`, revisar esta entrada)
