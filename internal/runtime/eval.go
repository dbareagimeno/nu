package runtime

import (
	lua "github.com/yuin/gopher-lua"
)

// EvalString compila y ejecuta `code` como un chunk Lua y devuelve sus valores
// de retorno convertidos a string (vía `tostring`), en orden. Es lo que respalda
// `nu -e`: el chunk `return nu.version.api` produce `["2"]` (G32 lo subió de 1).
//
// Si el chunk lanza un error estructurado del core (§1.4), se devuelve como
// `*StructuredError` con su `code`/`message` intactos: el puente no traga ni
// reescribe el error al cruzar la frontera Lua→Go (invariante 🔒 de S02). Un
// error de sintaxis o un `error("string")` cualquiera se devuelve tal cual.
//
// El chunk de `nu -e` corre en el estado principal, **no es una task**: puede
// lanzar tasks con `nu.task.spawn` pero no usar funciones ⏸ (que exigen estar en
// una task, §1.3). Corre con el token Lua tomado; al soltarlo, las tasks que
// lanzó progresan, y `waitIdle` espera a que todas terminen antes de leer los
// valores de retorno del chunk (que viven en la pila del estado principal, que
// las tasks —en sus propios threads— nunca tocan).
func (rt *Runtime) EvalString(code string) ([]string, error) {
	L := rt.L
	s := rt.sched

	s.acquire()
	fn, err := L.LoadString(code)
	if err != nil {
		s.release()
		return nil, err
	}

	base := L.GetTop()
	L.Push(fn)
	perr := L.PCall(0, lua.MultRet, nil)
	s.release()

	// Espera a que las tasks lanzadas por el chunk corran a término (sus efectos,
	// sus liberaciones en S08) antes de devolver el control.
	s.waitIdle()

	s.acquire()
	defer s.release()

	if perr != nil {
		if se, ok := structuredFromError(perr); ok {
			return nil, se
		}
		return nil, perr
	}

	n := L.GetTop() - base
	results := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		v := L.Get(base + i)
		results = append(results, L.ToStringMeta(v).String())
	}
	L.SetTop(base)
	return results, nil
}

// EvalTaskString compila `code` y lo ejecuta **como una task** (§3), no como el
// chunk principal: a diferencia de `EvalString` (que corre en el estado principal
// y por eso NO puede usar funciones ⏸), aquí el chunk corre sobre su propio thread
// con el puente de suspensión disponible, de modo que puede llamar directamente a
// `nu.fs.read`, `nu.http.stream`, `Session:send` del agente, etc. Espera a que la
// task —y cualquier otra que ella lance— termine, y devuelve sus valores de
// retorno convertidos a string (vía `tostring`), en orden.
//
// Es el **ejecutor headless** del binario: respalda los modos del CLI que orquestan
// extensiones suspendientes sin TTY (un turno de agente headless, `--continue`), la
// contraparte ⏸ de `nu -e`. NO es superficie Lua sagrada (igual que `EvalString` o
// `RenderBareScreen`): es la interfaz Go del ejecutable, fuera de api.md. El core
// sigue sin saber lo que es un agente (ADR-003): aquí solo corre un chunk Lua a
// término; la lógica de agente vive en la extensión `agent` y en el driver Lua que
// el CLI le pasa (main.go).
//
// Errores: si el chunk (o lo que orquesta) lanza un error estructurado del core o
// de una extensión (§1.4), se devuelve como `*StructuredError` con su `code`/
// `message` intactos (el puente no traga ni reescribe el code, invariante 🔒 de
// S02), exactamente como `EvalString`. Un error no estructurado (sintaxis,
// `error("texto")`) se rinde a texto. Una cancelación/abort de la task se reporta
// como `ECANCELED`/`EBUDGET` (la task no entrega valor; §1.3).
func (rt *Runtime) EvalTaskString(code string) ([]string, error) {
	L := rt.L
	s := rt.sched

	s.acquire()
	fn, err := L.LoadString(code)
	if err != nil {
		s.release()
		return nil, err
	}
	s.release()

	// Lanza el chunk como una task (su propio thread) y espera a que el primer
	// plano —la task y cuanto encole— se quiesca. `spawn` arranca la goroutine;
	// `runTask` toma el token por su cuenta, así que aquí el token NO debe estar
	// tomado (lo soltamos arriba tras compilar).
	t := s.spawn(fn, nil)
	s.waitIdle()

	s.acquire()
	defer s.release()

	// La task fue abortada (cancelación o watchdog): no entrega valor (§1.3). Se
	// reporta como el error estructurado correspondiente para que el CLI lo mapee a
	// un código de salida coherente.
	if t.canceled {
		if t.reason == abortBudget {
			return nil, &StructuredError{Code: CodeEBUDGET,
				Message: "la task del CLI excedió el presupuesto de slice del watchdog", Detail: lua.LNil}
		}
		return nil, &StructuredError{Code: CodeECANCELED,
			Message: "la task del CLI fue cancelada", Detail: lua.LNil}
	}

	if t.errValue != nil {
		if se, ok := structuredFromValue(t.errValue); ok {
			return nil, se
		}
		return nil, &luaRuntimeError{value: t.errValue}
	}

	results := make([]string, 0, len(t.results))
	for _, v := range t.results {
		results = append(results, L.ToStringMeta(v).String())
	}
	return results, nil
}

// luaRuntimeError envuelve un error de task que NO es la tabla estructurada del
// contrato §1.4 (un `error("texto")`, un error nativo de Lua): conserva el valor
// para rendirlo a texto. Lo usa `EvalTaskString` para que el CLI tenga siempre un
// `error` Go que mapear a un código de salida, aunque el fallo no fuera estructurado.
type luaRuntimeError struct {
	value lua.LValue
}

func (e *luaRuntimeError) Error() string { return errString(e.value) }

// SetStringGlobal fija un global Lua de tipo string desde Go. Es la vía por la que
// el BINARIO (main.go) pasa sus argumentos de línea de comandos —el prompt del
// agente, el modelo, los flags— al **driver Lua** del CLI SIN interpolarlos en el
// código (lo que abriría una inyección a través de un prompt con comillas o saltos
// de línea). Igual que `EvalTaskString`/`RenderBareScreen`, es interfaz Go del
// ejecutable, NO superficie Lua sagrada (fuera de api.md): el core no acuña aquí
// ningún nombre de producto; el contrato del nombre del global lo fija el CLI con
// su driver. Toma el token para tocar el estado Lua de forma segura.
func (rt *Runtime) SetStringGlobal(name, value string) {
	rt.sched.acquire()
	defer rt.sched.release()
	rt.L.SetGlobal(name, lua.LString(value))
}
