---
title: "P18 — Windows nativo (sin WSL2)"
type: "pospuesto"
id: "P18"
status: "vigente"
---
# P18 · Windows nativo (sin WSL2)

**Dónde se pospuso.** [problemas.md](../findings/README.md) G9

**Por qué.** v1 = Linux/macOS nativos + WSL2 en Windows: el contrato POSIX se cumple íntegro sin especificación condicional

**Disparador de reapertura.** Demanda real de usuarios Windows sin WSL2; implica shell portable, semántica de señales dual y pruebas de terminal nativas
