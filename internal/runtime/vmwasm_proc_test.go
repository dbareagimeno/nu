package runtime

// Tests de M13b: enu.proc sobre wasm (§6). Paridad con proc_test.go: spawn de un
// comando + wait por su código de salida, captura de salida (read/read_line), el
// round-trip por stdin de `cat` (write/close_stdin/EOF), señal/kill a un proceso que
// duerme, enu.proc.alive (G17: existencia, no identidad) y los errores de arranque/uso
// (ENOENT/EINVAL/ETIMEOUT). Se usan utilidades POSIX reales (echo/cat/sh/sleep),
// presentes en cualquier Linux de CI. Las primitivas ⏸ corren dentro de una task y el
// driver (RunTasks) las lleva a término; un plazo acota un cuelgue accidental a un
// fallo claro. `waitDead` (proc_test.go, mismo paquete) espera a la CONDICIÓN real.

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dbareagimeno/enu/internal/vmwasm"
)

// wasmProcRun registra enu.proc sobre una Instance (con métodos extra opcionales para
// los tests, p. ej. exponer el pid), evalúa `setup` (que crea tasks) y conduce el
// bucle; devuelve la Instance para leer las globales de resultado. Espeja
// wasmWsRun/wasmHTTPRun.
func wasmProcRun(t *testing.T, rt *Runtime, extra func(p *vmwasm.Pool), setup string) *vmwasm.Instance {
	t.Helper()
	p, err := vmwasm.NewPool()
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })
	registerProcWasm(p, rt)
	if extra != nil {
		extra(p)
	}
	inst, err := p.NewInstance()
	if err != nil {
		t.Fatalf("NewInstance: %v", err)
	}
	t.Cleanup(func() { _ = inst.Close() })
	if _, lerr, err := inst.Eval(setup); err != nil || lerr != "" {
		t.Fatalf("setup: lerr=%q err=%v", lerr, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := inst.RunTasks(ctx); err != nil {
		t.Fatalf("RunTasks: %v", err)
	}
	return inst
}

// evalStr evalúa `expr` sobre la Instance y devuelve el string resultante.
func evalStr(t *testing.T, inst *vmwasm.Instance, expr string) string {
	t.Helper()
	out, lerr, err := inst.Eval(expr)
	if err != nil || lerr != "" {
		t.Fatalf("eval %q: lerr=%q err=%v", expr, lerr, err)
	}
	return out
}

// M13b.proc.1: spawn de un comando simple + captura de salida (read_line) + wait por
// su código de salida. El criterio de hecho central de spawn. El equivalente wasm de
// TestSpawnCatRoundTrip (aquí sobre echo).
func TestProcWasmSpawnWait(t *testing.T) {
	inst := wasmProcRun(t, &Runtime{}, nil, `
		enu.task.spawn(function()
			local p = enu.proc.spawn({"echo", "hola"})
			local line = p:read_line("stdout")   -- "hola\n"
			local eof = p:read_line("stdout")     -- nil: echo cerró stdout
			local code = p:wait().code            -- 0
			out = line .. "|" .. tostring(eof) .. "|" .. tostring(code)
		end)`)
	if got := evalStr(t, inst, `return tostring(out)`); got != "hola\n|nil|0" {
		t.Fatalf("spawn+read_line+wait: got %q", got)
	}
}

// M13b.proc.2: round-trip por stdin de `cat` — write alimenta stdin, read_line lee la
// misma línea; close_stdin señala EOF y el siguiente read_line devuelve nil; wait da
// code=0. El equivalente wasm de TestSpawnCatRoundTrip (write/read_line/close_stdin).
func TestProcWasmCatRoundTrip(t *testing.T) {
	inst := wasmProcRun(t, &Runtime{}, nil, `
		enu.task.spawn(function()
			local p = enu.proc.spawn({"cat"})
			p:write("uno\n")
			local l1 = p:read_line("stdout")      -- "uno\n"
			p:write("dos\n")
			local l2 = p:read_line("stdout")      -- "dos\n"
			p:close_stdin()                        -- EOF a cat
			local eof = p:read_line("stdout")     -- nil: cat cerró stdout
			local code = p:wait().code             -- 0
			out = l1 .. "|" .. l2 .. "|" .. tostring(eof) .. "|" .. tostring(code)
		end)`)
	if got := evalStr(t, inst, `return tostring(out)`); got != "uno\n|dos\n|nil|0" {
		t.Fatalf("cat round-trip: got %q", got)
	}
}

// M13b.proc.3: read crudo (sin n) lee todo hasta EOF, y una lectura posterior da nil.
// El equivalente wasm de TestSpawnReadRaw.
func TestProcWasmReadRaw(t *testing.T) {
	inst := wasmProcRun(t, &Runtime{}, nil, `
		enu.task.spawn(function()
			local p = enu.proc.spawn({"sh", "-c", "printf 'abcdef'"})
			local all = p:read("stdout")          -- "abcdef" (todo hasta EOF)
			local more = p:read("stdout")         -- nil (ya en EOF)
			local code = p:wait().code
			out = all .. "|" .. tostring(more) .. "|" .. tostring(code)
		end)`)
	if got := evalStr(t, inst, `return tostring(out)`); got != "abcdef|nil|0" {
		t.Fatalf("read raw: got %q", got)
	}
}

// M13b.proc.4: señal/kill a un proceso que DUERME. Se expone el pid del subproceso con
// un método de handle de test (_pid), se comprueba que alive(pid) es true mientras
// corre, se le manda SIGKILL, y wait() desbloquea (código != 0 por señal). Luego, en
// Go, waitDead confirma que el pid dejó de existir. Espeja
// TestSpawnKilledByCleanupOnCancel/TestProcAliveSnippetG17 sin depender del scheduler.
func TestProcWasmKillSleeping(t *testing.T) {
	extra := func(p *vmwasm.Pool) {
		// método de TEST (no es API §6): devuelve el pid del subproceso para observarlo.
		p.RegisterHandleMethod("Proc", "_pid", func(inst *vmwasm.Instance, val any, args []any) ([]any, error) {
			return []any{int64(val.(*luaProc).cmd.Process.Pid)}, nil
		})
	}
	inst := wasmProcRun(t, &Runtime{}, extra, `
		enu.task.spawn(function()
			local p = enu.proc.spawn({"sleep", "30"})
			g_pid = __hcall(p.__id, "_pid")
			g_alive = enu.proc.alive(g_pid)   -- true: el proceso está vivo
			p:kill(9)                         -- SIGKILL
			local code = p:wait().code        -- desbloquea; code != 0 (matado por señal)
			g_killed = (code ~= 0)
		end)`)

	if got := evalStr(t, inst, `return tostring(g_alive)`); got != "true" {
		t.Fatalf("alive(pid del sleep vivo): got %q, want true", got)
	}
	if got := evalStr(t, inst, `return tostring(g_killed)`); got != "true" {
		t.Fatalf("kill: el proceso matado debería tener code != 0, got g_killed=%q", got)
	}
	pidStr := evalStr(t, inst, `return tostring(g_pid)`)
	pid, err := strconv.Atoi(strings.TrimSpace(pidStr))
	if err != nil {
		t.Fatalf("pid no numérico: %q (%v)", pidStr, err)
	}
	if !waitDead(pid, 5*time.Second) {
		t.Fatalf("tras kill+wait, el subproceso (pid %d) debería estar muerto", pid)
	}
}

// Señal no numérica → EINVAL, y el proceso ni muere ni queda inmatable. `Proc:kill`
// con una señal mal tipada (un string "KILL") debe lanzar EINVAL en vez de degradarla
// a 0 (la sonda de existencia, que no mata): el proceso sigue vivo, y —lo importante—
// sigue siendo matable con una señal válida posterior (el kill fallido no fija `killed`
// ni cortocircuita los siguientes). También cubre el caso feliz (el kill(9) que sí mata).
func TestProcWasmKillNonNumericSignal(t *testing.T) {
	extra := func(p *vmwasm.Pool) {
		p.RegisterHandleMethod("Proc", "_pid", func(inst *vmwasm.Instance, val any, args []any) ([]any, error) {
			return []any{int64(val.(*luaProc).cmd.Process.Pid)}, nil
		})
	}
	inst := wasmProcRun(t, &Runtime{}, extra, `
		enu.task.spawn(function()
			local p = enu.proc.spawn({"sleep", "30"})
			g_pid = __hcall(p.__id, "_pid")
			-- señal no numérica: debe lanzar EINVAL y NO tocar el proceso
			local ok, e = pcall(function() return p:kill("KILL") end)
			g_ok = ok
			g_code = (e and e.code) or "nil"
			g_alive = enu.proc.alive(g_pid)     -- sigue vivo: el kill inválido no lo mató
			-- ...y sigue siendo matable con una señal válida
			p:kill(9)                          -- SIGKILL
			g_killed = (p:wait().code ~= 0)
		end)`)

	if got := evalStr(t, inst, `return tostring(g_ok)`); got != "false" {
		t.Fatalf("kill('KILL') debería lanzar, got ok=%q", got)
	}
	if got := evalStr(t, inst, `return tostring(g_code)`); got != "EINVAL" {
		t.Fatalf("kill('KILL'): got code %q, want EINVAL", got)
	}
	if got := evalStr(t, inst, `return tostring(g_alive)`); got != "true" {
		t.Fatalf("tras un kill inválido el proceso debería seguir vivo, got alive=%q", got)
	}
	if got := evalStr(t, inst, `return tostring(g_killed)`); got != "true" {
		t.Fatalf("tras el kill inválido, un kill(9) válido debería matarlo, got killed=%q", got)
	}
	pidStr := evalStr(t, inst, `return tostring(g_pid)`)
	pid, err := strconv.Atoi(strings.TrimSpace(pidStr))
	if err != nil {
		t.Fatalf("pid no numérico: %q (%v)", pidStr, err)
	}
	if !waitDead(pid, 5*time.Second) {
		t.Fatalf("tras kill(9)+wait, el subproceso (pid %d) debería estar muerto", pid)
	}
}

// M13b.proc.5: enu.proc.alive (G17) — existencia, no identidad. El pid 1 (init) existe
// en cualquier Unix aunque no sea nuestro → true; un pid imposible (2^30) → false. El
// equivalente wasm de TestProcAliveSnippetG17. alive NO es ⏸ (consulta inmediata):
// funciona fuera de una task.
func TestProcWasmAlive(t *testing.T) {
	inst := wasmProcRun(t, &Runtime{}, nil, `
		out = tostring(enu.proc.alive(1)) .. ":" .. tostring(enu.proc.alive(1073741824))`)
	if got := evalStr(t, inst, `return tostring(out)`); got != "true:false" {
		t.Fatalf("alive G17: got %q, want true:false", got)
	}
}

// M13b.proc.6: errores de arranque/uso. Un ejecutable inexistente → ENOENT; un argv
// vacío → EINVAL. Ambos capturables con pcall. Espeja TestRunNonexistentSnippet y la
// validación de parseProcArgs.
func TestProcWasmSpawnInvalid(t *testing.T) {
	inst := wasmProcRun(t, &Runtime{}, nil, `
		enu.task.spawn(function()
			local ok1, e1 = pcall(function() return enu.proc.spawn({"no-existe-binario-xyz-123"}) end)
			local ok2, e2 = pcall(function() return enu.proc.spawn({}) end)
			out = tostring(ok1) .. ":" .. e1.code .. ":" .. tostring(ok2) .. ":" .. e2.code
		end)`)
	if got := evalStr(t, inst, `return tostring(out)`); got != "false:ENOENT:false:EINVAL" {
		t.Fatalf("spawn inválido: got %q, want false:ENOENT:false:EINVAL", got)
	}
}

// M13b.proc.7: enu.proc.run (⏸) — la conveniencia con buffers. echo → code=0 + stdout;
// un exit != 0 es dato (no lanza); un ejecutable inexistente → ENOENT; timeout_ms
// excedido → ETIMEOUT (tras matar el proceso). Espeja TestRunSnippet + TestRunTimeoutSnippet
// + TestRunNonexistentSnippet.
func TestProcWasmRun(t *testing.T) {
	inst := wasmProcRun(t, &Runtime{}, nil, `
		enu.task.spawn(function()
			local r = enu.proc.run({"echo", "hi"})
			local r2 = enu.proc.run({"sh", "-c", "exit 7"})
			local ok3, e3 = pcall(function() return enu.proc.run({"no-existe-xyz-123"}) end)
			local ok4, e4 = pcall(function() return enu.proc.run({"sleep", "30"}, { timeout_ms = 100 }) end)
			out = tostring(r.code) .. "|" .. r.stdout .. "|" .. tostring(r2.code)
				.. "|" .. tostring(ok3) .. ":" .. e3.code .. "|" .. tostring(ok4) .. ":" .. e4.code
		end)`)
	if got := evalStr(t, inst, `return tostring(out)`); got != "0|hi\n|7|false:ENOENT|false:ETIMEOUT" {
		t.Fatalf("run: got %q, want 0|hi\\n|7|false:ENOENT|false:ETIMEOUT", got)
	}
}

// M13b.proc.8: read_line con un `which` inválido → EINVAL (capturable, validado antes
// del IO). El equivalente wasm de TestProcReadInvalidStream.
func TestProcWasmReadInvalidStream(t *testing.T) {
	inst := wasmProcRun(t, &Runtime{}, nil, `
		enu.task.spawn(function()
			local p = enu.proc.spawn({"cat"})
			local ok, e = pcall(function() return p:read_line("nope") end)
			p:kill()
			p:wait()
			out = tostring(ok) .. ":" .. e.code
		end)`)
	if got := evalStr(t, inst, `return tostring(out)`); got != "false:EINVAL" {
		t.Fatalf("read_line inválido: got %q, want false:EINVAL", got)
	}
}

// M13b.proc.9: write tras close_stdin → ECLOSED (capturable). El equivalente wasm de
// TestProcWriteAfterCloseECLOSED.
func TestProcWasmWriteAfterClose(t *testing.T) {
	inst := wasmProcRun(t, &Runtime{}, nil, `
		enu.task.spawn(function()
			local p = enu.proc.spawn({"cat"})
			p:close_stdin()
			local ok, e = pcall(function() return p:write("x") end)
			p:kill()
			p:wait()
			out = tostring(ok) .. ":" .. e.code
		end)`)
	if got := evalStr(t, inst, `return tostring(out)`); got != "false:ECLOSED" {
		t.Fatalf("write tras close_stdin: got %q, want false:ECLOSED", got)
	}
}

// Reap temprano: un proceso TERMINADO cuyos dos streams se agotaron (EOF visto
// por la ruta de lectura) se recoge sin esperar a Runtime.Close — pipes cerrados
// y fuera de los mapas del scheduler—. Antes, cada spawn anclaba 2 descriptores
// y sus entradas de rastreo durante toda la vida del runtime.
func TestProcWasmReapTemprano(t *testing.T) {
	rt := &Runtime{}
	rt.sched = newScheduler(rt, 100*time.Millisecond)
	inst := wasmProcRun(t, rt, nil, `
		enu.task.spawn(function()
			local p = enu.proc.spawn({"sh", "-c", "echo hola; echo err 1>&2"})
			p:wait()
			while p:read_line("stdout") ~= nil do end
			while p:read_line("stderr") ~= nil do end
			out = "ok"
		end)`)
	if got := evalStr(t, inst, `return tostring(out)`); got != "ok" {
		t.Fatalf("setup: got %q", got)
	}
	// El último EOF dispara el reap dentro del propio hostcall, pero se sondea con
	// margen para no acoplarse a ese detalle.
	deadline := time.Now().Add(2 * time.Second)
	for {
		rt.sched.mu.Lock()
		nProcs := len(rt.sched.procs)
		nOwned := len(rt.sched.ownerHandles["user"])
		rt.sched.mu.Unlock()
		if nProcs == 0 && nOwned == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("reap temprano no ocurrió: procs=%d, handles del dueño=%d", nProcs, nOwned)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestProcWasmEnvG65 blinda la matriz de `opts.env` por el puente wasm (G65, §6): las
// dos formas puras (tabla { K = V } y array ["K=V"]), el valor con `=`, el last-wins de
// claves repetidas, el reemplazo-con-vacío frente a la herencia, un smoke de array por
// spawn (comparte parser con run), y las formas malformadas → EINVAL. Antes de G65 NO
// existía NINGÚN test de env por este puente: la rama sólo aceptaba `map[string]any` y
// descartaba en silencio arrays y entradas mal tipadas. Se usa `sh -c 'echo $VAR'`
// (echo es builtin: no necesita PATH aunque el entorno se reemplace por vacío).
func TestProcWasmEnvG65(t *testing.T) {
	// 1) tabla { K = V } → el hijo ve FOO=bar (regresión de la forma histórica).
	t.Run("tabla {K=V}", func(t *testing.T) {
		inst := wasmProcRun(t, &Runtime{}, nil, `
			enu.task.spawn(function()
				out = enu.proc.run({"sh","-c","echo $FOO"}, { env = { FOO = "bar" } }).stdout
			end)`)
		if got := strings.TrimSpace(evalStr(t, inst, `return tostring(out)`)); got != "bar" {
			t.Fatalf("tabla {FOO=bar}: got %q, want bar", got)
		}
	})

	// 2) array ["K=V"] → el hijo ve FOO=bar (lo NUEVO de G65).
	t.Run("array {K=V}", func(t *testing.T) {
		inst := wasmProcRun(t, &Runtime{}, nil, `
			enu.task.spawn(function()
				out = enu.proc.run({"sh","-c","echo $FOO"}, { env = {"FOO=bar"} }).stdout
			end)`)
		if got := strings.TrimSpace(evalStr(t, inst, `return tostring(out)`)); got != "bar" {
			t.Fatalf("array {FOO=bar}: got %q, want bar", got)
		}
	})

	// 3) array con valor que contiene `=` → se parte por el PRIMER `=`: FOO=a=b.
	t.Run("array valor con =", func(t *testing.T) {
		inst := wasmProcRun(t, &Runtime{}, nil, `
			enu.task.spawn(function()
				out = enu.proc.run({"sh","-c","echo $FOO"}, { env = {"FOO=a=b"} }).stdout
			end)`)
		if got := strings.TrimSpace(evalStr(t, inst, `return tostring(out)`)); got != "a=b" {
			t.Fatalf("array {FOO=a=b}: got %q, want a=b", got)
		}
	})

	// 4) array con clave repetida → gana la ÚLTIMA (semántica exec.Cmd.Env; mergedEnv
	// colapsa last-wins sin dedupe manual en el parser).
	t.Run("array clave repetida last-wins", func(t *testing.T) {
		inst := wasmProcRun(t, &Runtime{}, nil, `
			enu.task.spawn(function()
				out = enu.proc.run({"sh","-c","echo $FOO"}, { env = {"FOO=x","FOO=y"} }).stdout
			end)`)
		if got := strings.TrimSpace(evalStr(t, inst, `return tostring(out)`)); got != "y" {
			t.Fatalf("array {FOO=x,FOO=y}: got %q, want y (última gana)", got)
		}
	})

	// 5) env = {} (tabla vacía → array vacío en el wire) REEMPLAZA con entorno vacío: el
	// hijo NO ve una var exportada por el padre. Sin env (nil) SÍ la ve (herencia intacta).
	t.Run("env={} reemplaza; nil hereda", func(t *testing.T) {
		t.Setenv("NU_G65_PARENT", "visible")
		inst := wasmProcRun(t, &Runtime{}, nil, `
			enu.task.spawn(function()
				g_empty   = enu.proc.run({"sh","-c","echo [$NU_G65_PARENT]"}, { env = {} }).stdout
				g_inherit = enu.proc.run({"sh","-c","echo [$NU_G65_PARENT]"}).stdout
			end)`)
		if got := strings.TrimSpace(evalStr(t, inst, `return tostring(g_empty)`)); got != "[]" {
			t.Fatalf("env={} debe reemplazar con entorno vacío: got %q, want []", got)
		}
		if got := strings.TrimSpace(evalStr(t, inst, `return tostring(g_inherit)`)); got != "[visible]" {
			t.Fatalf("sin env debe heredar el entorno del padre: got %q, want [visible]", got)
		}
	})

	// Smoke: la forma de array también funciona por spawn (comparte parseProcArgsWasm).
	t.Run("array por spawn (smoke, comparte parser)", func(t *testing.T) {
		inst := wasmProcRun(t, &Runtime{}, nil, `
			enu.task.spawn(function()
				local p = enu.proc.spawn({"sh","-c","echo $FOO"}, { env = {"FOO=viaspawn"} })
				out = p:read_line("stdout")
				p:wait()
			end)`)
		if got := strings.TrimSpace(evalStr(t, inst, `return tostring(out)`)); got != "viaspawn" {
			t.Fatalf("array por spawn: got %q, want viaspawn", got)
		}
	})

	// 6) Formas malformadas → EINVAL (capturable con pcall; §1.4). Cubre no-tabla
	// (número, string), array con entrada sin `=` (eq==-1), con `=` inicial / clave
	// vacía (eq==0) o no-string, y tabla con valor no-string, clave vacía o clave con
	// `=`. El caso "=x" muerde la guardia `eq <= 0` (no bastaría `eq < 0`).
	t.Run("EINVAL", func(t *testing.T) {
		inst := wasmProcRun(t, &Runtime{}, nil, `
			enu.task.spawn(function()
				local function code(env)
					local ok, e = pcall(function() return enu.proc.run({"echo","x"}, { env = env }) end)
					return tostring(ok) .. ":" .. ((type(e) == "table" and e.code) or tostring(e))
				end
				g_num       = code(42)
				g_str       = code("PATH=x")
				g_arr_noeq  = code({"SIN_IGUAL"})
				g_arr_keyvac= code({"=x"})
				g_arr_bool  = code({true})
				g_map_valnum= code({ FOO = 1 })
				g_map_keyvac= code({ [""] = "x" })
				g_map_keyeq = code({ ["A=B"] = "x" })
			end)`)
		for _, g := range []string{
			"g_num", "g_str", "g_arr_noeq", "g_arr_keyvac", "g_arr_bool",
			"g_map_valnum", "g_map_keyvac", "g_map_keyeq",
		} {
			if got := evalStr(t, inst, `return tostring(`+g+`)`); got != "false:EINVAL" {
				t.Fatalf("%s: got %q, want false:EINVAL", g, got)
			}
		}
	})
}

// TestProcWasmEnvG65Precedence: un `opts.env` explícito EN ARRAY (la forma nueva de G65)
// pisa el overlay de enu.sys.setenv — control total por llamada (§6/§7). Espeja el
// patrón de TestMergedEnvPrecedence (sys_test.go) pero e2e por el puente wasm y con la
// forma de array. (G65)
func TestProcWasmEnvG65Precedence(t *testing.T) {
	rt := &Runtime{sys: &sysState{}}
	inst := wasmProcRun(t, rt, func(p *vmwasm.Pool) { registerSysWasm(p, rt) }, `
		enu.task.spawn(function()
			enu.sys.setenv("NU_G65_PREC", "from_overlay")
			out = enu.proc.run({"sh","-c","echo $NU_G65_PREC"}, { env = {"NU_G65_PREC=from_env"} }).stdout
		end)`)
	if got := strings.TrimSpace(evalStr(t, inst, `return tostring(out)`)); got != "from_env" {
		t.Fatalf("un env array explícito debe pisar el overlay de setenv: got %q, want from_env", got)
	}
}

// TestProcWasmTimeoutMsCero: `timeout_ms = 0` es válido (0 = sin límite, no EINVAL) —
// la guardia de §6 solo rechaza los negativos. Mata al mutante CONDITIONALS_BOUNDARY
// `tm < 0` → `tm <= 0` que la pasada de mutación de G65 dejó vivo (hueco preexistente,
// no de la rama de env).
func TestProcWasmTimeoutMsCero(t *testing.T) {
	inst := wasmProcRun(t, &Runtime{}, nil, `
		enu.task.spawn(function()
			local ok, e = pcall(function() return enu.proc.run({"echo","x"}, { timeout_ms = 0 }) end)
			out = tostring(ok) .. ":" .. ((ok and "ok") or (type(e) == "table" and e.code) or "?")
		end)`)
	if got := evalStr(t, inst, `return tostring(out)`); got != "true:ok" {
		t.Fatalf("timeout_ms=0 debe ser válido (sin límite): got %q, want true:ok", got)
	}
}
