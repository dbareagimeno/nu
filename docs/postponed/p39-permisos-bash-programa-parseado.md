---
title: "P39 — Emparejamiento de permisos de bash sobre el programa parseado"
type: "pospuesto"
id: "P39"
status: "vigente"
---
# P39 · Emparejamiento de permisos de `bash` sobre el **programa parseado** (parser de shell completo en Go, ¿`enu.proc.parse`?)

**Dónde se pospuso.** [agente.md](../contracts/agente.md) §5 / [problemas.md](../findings/README.md) G53 / ADR-023

**Por qué.** El matcher v1 (G53) descompone el comando por operadores con un tokenizador **cerrado** (palabras planas y strings entre comillas) y falla hacia `ask` ante todo constructo no modelable — `$( )`, redirecciones, heredocs, subshells. Un parser de bash completo (AST con sustituciones, redirecciones y agrupaciones) permitiría autorizar con precisión comandos hoy no modelables, pero es un proyecto de seguridad en sí (doctrina P17: hacerlo *casi* bien es peor que no tenerlo) y sería una primitiva de kernel nueva con un único consumidor — el mismo veto que P37 aplica a `enu.html`

**Disparador de reapertura.** Fricción real **documentada** del tokenizador (subcomandos legítimos cayendo a `ask` por constructos no modelables con frecuencia que duela), o un **segundo consumidor** de parseo de shell fuera del matcher de permisos — el calco del disparador de P37
