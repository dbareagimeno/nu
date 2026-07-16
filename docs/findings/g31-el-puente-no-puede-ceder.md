# G31 · El puente ⏸ no puede ceder a través de `pcall`/tail call en gopher-lua — `api.md` §1.3/§1.4 — **RESUELTO**

**Resolución** (decisión en [adr.md](adr.md) ADR-011; sin cambios en
[api.md](api.md): la API era correcta, fallaba la técnica de realización).
El scheduler se realiza **sin yields de corrutina**: una goroutine por task
+ un único token de ejecución Lua. Una primitiva ⏸ suelta el token, hace el
trabajo bloqueante en una goroutine de fondo y al volver lo recupera; como no
hay yield, `pcall`, las tail calls y el desenrollado de errores son los
nativos de gopher-lua y sobreviven a la suspensión. Implementado en S04
(`internal/runtime/scheduler.go`), validado con `-race`.
