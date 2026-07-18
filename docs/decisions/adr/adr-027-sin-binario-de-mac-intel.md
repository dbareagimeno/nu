---
title: "El release no publica binario para Mac Intel (darwin/amd64); el macOS soportado es Apple Silicon"
type: "adr"
id: "ADR-027"
status: "aceptada"
date: "2026-07-18"
---
# ADR-027 · Sin binario de Mac Intel: el macOS soportado es Apple Silicon

**Estado:** Aceptada · 2026-07-18 (**refina el alcance de plataforma de
[G9](../../findings/g09-alcance-windows-en-v1.md)**; motivada por la matriz de
smoke de [S48](../../plan/implementacion.md) y una decisión del operador)

**Contexto.** G9 fijó el alcance v1 en «Linux y macOS nativos» y `release.yml`
publica cuatro binarios: `linux/amd64`, `linux/arm64`, `darwin/amd64`,
`darwin/arm64`. Al construir la matriz de smoke en sistemas limpios (S48) los
runners macOS de GitHub resultaron escasos: el leg **Intel** (`macos-13`) se
encolaba y frenaba toda la matriz, mientras que los cuatro contenedores Linux y
el leg **ARM** (`macos-14`) pasaban en segundos. Eso destapó una pregunta de
producto: ¿vale la pena sostener Mac Intel? El hardware Intel de Apple es
legacy (última máquina 2020, transición a Apple Silicon completada), y quien
sigue en una de esas máquinas casi siempre corre Linux encima —ya cubierto por
`linux/amd64`— o puede usar WSL2 (G9) / compilar desde fuente.

**Decisión.** El release **deja de publicar el binario `darwin/amd64`**. El
macOS soportado de primera clase es **Apple Silicon (`darwin/arm64`)**. La
matriz de release baja a **tres binarios** (`linux/amd64`, `linux/arm64`,
`darwin/arm64`) → **cuatro assets** con `checksums.txt`. El instalador
(`install.sh`) **rechaza `darwin`+`amd64`** con un mensaje accionable (usa
Linux/WSL2, o compila desde fuente) en vez de intentar bajar un asset que ya no
existe. La matriz de smoke (S48) ya quedó en solo ARM.

No se retira la *capacidad* de cross-compilar: `GOOS=darwin GOARCH=amd64 go
build` sigue funcionando (el core es POSIX y `CGO_ENABLED=0`); lo que se retira
es el **soporte publicado y probado**. Quien de verdad lo necesite lo compila.

**Razonamiento.**
- **Coste/beneficio.** Sostener Intel cuesta un runner escaso en CI (que
  encolaba toda la matriz) y un asset más que firmar y verificar, para servir a
  un parque de hardware en extinción cuya vía de escape (Linux/WSL2) ya está
  cubierta. El corolario de ADR-025 (motor para desplegar como infraestructura)
  no se resiente: los entornos objetivo —contenedores, CI, air-gapped— son
  Linux, y el Mac de desarrollo moderno es ARM.
- **Coherencia con S48.** La matriz de smoke ya dejó de probar Intel; seguir
  *shipeando* un binario que no se prueba en limpio sería incoherente —o lo
  probamos, o no lo publicamos—. Se elige no publicarlo.
- **Por qué un ADR y no un retoque.** La decisión ripplea por `release.yml`,
  `install.sh`, `release.md`, `arquitectura.md` y el alcance de G9: es
  multi-documento y de producto (qué plataformas soporta enu), justo lo que un
  ADR registra.

**Consecuencias.**
- `release.yml`: se quita `{ goos: darwin, goarch: amd64 }` de la matriz.
- `install.sh`: rechazo explícito de `darwin`+`amd64` con remedio.
- `release.md`: la verificación pasa de **5 a 4 assets**
  (`checksums.txt` + `linux-amd64` + `linux-arm64` + `darwin-arm64`).
- `arquitectura.md` y `G9`: el macOS **publicado** se estrecha a `arm64`
  (Apple Silicon); el texto de G9 conserva su resolución y añade el puntero a
  este ADR (los hallazgos no se reescriben).
- **Disparador de reapertura:** demanda real y sostenida de usuarios de Mac
  Intel que no puedan usar Linux/WSL2 ni compilar desde fuente — improbable y
  decreciente con el tiempo.
