package runtime

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	lua "github.com/yuin/gopher-lua"
)

// Tests de S16 (api.md §6): `nu.proc`. Sesión 🔒 —la lógica clave a blindar
// (inventario del plan): **vida del proceso por `cleanup`** (al cancelar la task
// que lo creó, el proceso muere) y **`alive` (G17)**, que informa de **existencia,
// no de identidad** (un pid reciclado daría `true`).
//
// Dos niveles, como en `fs_test.go`: tests Go directos sobre las funciones puras
// (`runBuffered`, `pidAlive`, `exitCode`) que blindan los invariantes sin pasar por
// el scheduler, y tests de snippet Lua que ejercitan la superficie de extremo a
// extremo (run/spawn/write/read_line/wait/kill/alive) por el puente ⏸ real. La
// suite corre con `-race -count=4`: el IO de `proc` va en goroutines de fondo, así
// que cualquier toque a Lua fuera del token —o una carrera sobre el `*luaProc`—
// saltaría aquí.
//
// Robustez anti-flaky: los tests de timing usan plazos holgados y, sobre todo, una
// espera a la CONDICIÓN (un proceso muerto), no a un sleep fijo. Los procesos de
// prueba son utilidades POSIX presentes en cualquier Linux de CI (`echo`, `cat`,
// `sh`, `sleep`, `printf`).

// --- 🔒 Lógica nuestra: alive (G17, existencia no identidad) ---

// TestPidAliveG17 blinda `nu.proc.alive` (G17): informa de EXISTENCIA, no de
// identidad. El pid del propio proceso de test está vivo → true; `pid 1` (init)
// existe en cualquier Unix aunque no sea nuestro → true (existencia, no propiedad);
// un pid imposible → false; un pid <= 0 → false. La parte "no identidad" se documenta
// en `pidAlive`: la llamada no distingue de QUÉ proceso es el pid —un pid reciclado
// daría true—.
func TestPidAliveG17(t *testing.T) {
	if !pidAlive(os.Getpid()) {
		t.Fatalf("G17: alive(pid del propio test) debería ser true")
	}
	if !pidAlive(1) {
		t.Fatalf("G17: alive(1) debería ser true (init existe; existencia no propiedad)")
	}
	if pidAlive(1<<30 + 12345) {
		t.Fatalf("G17: alive(pid inexistente) debería ser false")
	}
	for _, pid := range []int{0, -1, -1000} {
		if pidAlive(pid) {
			t.Fatalf("G17: alive(%d) debería ser false (no designa un proceso)", pid)
		}
	}
}

// TestPidAliveDeadProcessG17 blinda el ciclo completo de G17 sobre un proceso real:
// un proceso vivo da true; tras morir y recogerse su desenlace (wait), el mismo pid
// ya NO existe → false. Demuestra que `alive` sigue la existencia REAL del proceso,
// no un estado cacheado.
func TestPidAliveDeadProcessG17(t *testing.T) {
	cmd := newCmd([]string{"sleep", "30"}, procOpts{})
	if err := cmd.Start(); err != nil {
		t.Fatalf("no se pudo lanzar sleep: %v", err)
	}
	pid := cmd.Process.Pid

	if !pidAlive(pid) {
		t.Fatalf("G17: el sleep recién lanzado debería estar vivo (pid %d)", pid)
	}

	_ = cmd.Process.Kill()
	_ = cmd.Wait() // recoge el zombi para que el pid deje de existir

	deadline := time.Now().Add(2 * time.Second)
	for pidAlive(pid) && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if pidAlive(pid) {
		t.Fatalf("G17: tras morir y wait, el pid %d no debería estar vivo", pid)
	}
}

// --- 🔒 run: buffers, sin shell, exit code dato, timeout ---

// TestRunBufferedEchoHi blinda el **criterio de hecho** central de `run`:
// `run(["echo","hi"])` → code=0, stdout contiene "hi". Test directo sobre
// `runBuffered` (sin el scheduler).
func TestRunBufferedEchoHi(t *testing.T) {
	code, stdout, stderr, err := runBuffered([]string{"echo", "hi"}, procOpts{})
	if err != nil {
		t.Fatalf("runBuffered(echo hi) falló: %v", err)
	}
	if code != 0 {
		t.Fatalf("code: got %d, want 0", code)
	}
	if !strings.Contains(stdout, "hi") {
		t.Fatalf("stdout: got %q, want que contenga \"hi\"", stdout)
	}
	if stderr != "" {
		t.Fatalf("stderr: got %q, want vacío", stderr)
	}
}

// TestRunNoImplicitShell blinda "SIN shell implícita" (§6): `argv` se pasa tal cual,
// nadie expande variables. `run(["echo","$HOME"])` imprime el literal `$HOME`, NO el
// valor de la variable de entorno —si hubiera una shell de por medio, saldría la
// ruta del home; este test lo cazaría—.
func TestRunNoImplicitShell(t *testing.T) {
	_, stdout, _, err := runBuffered([]string{"echo", "$HOME"}, procOpts{})
	if err != nil {
		t.Fatalf("runBuffered falló: %v", err)
	}
	if got := strings.TrimSpace(stdout); got != "$HOME" {
		t.Fatalf("sin shell: stdout got %q, want el literal \"$HOME\" (no expandido)", got)
	}
}

// TestRunNonZeroExitIsData blinda que un código de salida != 0 **no lanza**: es un
// dato. `sh -c "exit 3"` devuelve code=3 sin error de Go.
func TestRunNonZeroExitIsData(t *testing.T) {
	code, _, _, err := runBuffered([]string{"sh", "-c", "exit 3"}, procOpts{})
	if err != nil {
		t.Fatalf("un exit != 0 no debe ser error de arranque: %v", err)
	}
	if code != 3 {
		t.Fatalf("code: got %d, want 3", code)
	}
}

// TestRunStdinAndEnv blinda `opts.stdin` (se alimenta a la entrada) y `opts.env` (el
// entorno explícito reemplaza al heredado): `cat` con stdin devuelve lo que se le
// dio, y `sh -c 'echo $FOO'` con `env=["FOO=bar"]` imprime "bar".
func TestRunStdinAndEnv(t *testing.T) {
	_, stdout, _, err := runBuffered([]string{"cat"}, procOpts{stdin: []byte("hola stdin"), hasStdin: true})
	if err != nil {
		t.Fatalf("cat con stdin falló: %v", err)
	}
	if strings.TrimSpace(stdout) != "hola stdin" {
		t.Fatalf("stdin→stdout: got %q, want \"hola stdin\"", stdout)
	}

	_, out2, _, err := runBuffered([]string{"sh", "-c", "echo $FOO"}, procOpts{env: []string{"FOO=bar"}})
	if err != nil {
		t.Fatalf("env falló: %v", err)
	}
	if strings.TrimSpace(out2) != "bar" {
		t.Fatalf("env: got %q, want \"bar\"", out2)
	}
}

// TestRunTimeoutKills blinda `timeout_ms`: un proceso que tarda más que el plazo se
// **mata** y `runBuffered` devuelve el centinela de timeout (que `procRun` rinde como
// `ETIMEOUT`). Se mide que la llamada vuelve PRONTO (no espera los 30 s del sleep).
func TestRunTimeoutKills(t *testing.T) {
	start := time.Now()
	_, _, _, err := runBuffered([]string{"sleep", "30"}, procOpts{timeout: 100 * time.Millisecond})
	elapsed := time.Since(start)
	if err != errProcTimeout {
		t.Fatalf("timeout: got %v, want errProcTimeout", err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("timeout: tardó %v; debería haber matado el sleep pronto", elapsed)
	}
}

// TestRunNonexistentExecutable blinda que arrancar un ejecutable inexistente devuelve
// un error (que `mapProcStartError` rinde como `ENOENT`; lo verifica el snippet Lua
// más abajo).
func TestRunNonexistentExecutable(t *testing.T) {
	if _, _, _, err := runBuffered([]string{"no-existe-este-binario-xyz"}, procOpts{}); err == nil {
		t.Fatalf("un ejecutable inexistente debería fallar al arrancar")
	}
}

// TestExitCode blinda el mapeo de `exitCode`: nil → 0.
func TestExitCode(t *testing.T) {
	if c := exitCode(nil); c != 0 {
		t.Fatalf("exitCode(nil): got %d, want 0", c)
	}
}

// --- 🔒 Vida del proceso: kill por cleanup al cancelar la task (criterio de hecho) ---

// procPidFromUD extrae el pid del subproceso de un userdata `Proc`. Es un helper de
// test para observar el pid SIN exponerlo en la API pública (§6 no incluye
// `Proc.pid`): se registra como global Go y un snippet le pasa el `Proc`.
func procPidFromUD(L *lua.LState) int {
	ud := L.CheckUserData(1)
	p, ok := ud.Value.(*luaProc)
	if !ok {
		L.RaiseError("se esperaba un Proc")
		return 0
	}
	L.Push(lua.LNumber(p.cmd.Process.Pid))
	return 1
}

// waitDead espera (con plazo holgado) a que `pid` deje de existir. Es la ancla
// anti-flaky: espera a la CONDICIÓN real, no a un sleep fijo.
func waitDead(pid int, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if !pidAlive(pid) {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return !pidAlive(pid)
}

// TestSpawnKilledByCleanupOnCancel blinda EL criterio de hecho central de S16: una
// task hace `spawn` + `nu.task.cleanup(function() proc:kill() end)`; al CANCELAR la
// task (S08), el proceso muere por el cleanup. Se verifica con `pidAlive(pid)` del
// subproceso: tras la cancelación, deja de existir.
//
// Todo el escenario va en UN `eval` que SÍ completa (el harness espera a la
// quiescencia, así que ninguna task puede quedar colgada al cruzar el borde): un
// controlador spawnea la víctima, espera a que esté suspendida en su `p:wait()`
// (future `ready`), la cancela y la espera (observa `ECANCELED`, lo que cierra el
// grafo). El pid del subproceso se publica con un helper Go (`__pid`) en un global
// que el test lee DESPUÉS para comprobar que el proceso ya está muerto.
func TestSpawnKilledByCleanupOnCancel(t *testing.T) {
	h := newHarness(t)
	// Usa andamiaje Go irreducible a Lua: un __publish_pid que bloquea en un canal
	// y procPidFromUD (userdata de nu.proc). La propiedad —un cleanup mata el
	// subproceso al cancelar la task— se apoya en nu.proc real; se valida en gopher.
	h.skipIfWasm("__publish_pid bloquea en un canal Go y usa el userdata de nu.proc")

	pidCh := make(chan int, 1)
	h.register("__publish_pid", func(L *lua.LState) int {
		pidCh <- int(L.CheckNumber(1))
		return 0
	})
	h.register("__pid", procPidFromUD)

	h.eval(`
		out = {}
		nu.task.spawn(function()
			local ready = nu.task.future()
			local victim = nu.task.spawn(function()
				local p = nu.proc.spawn({"sleep", "30"})
				nu.task.cleanup(function() p:kill() end)   -- al cancelar, muere el proceso
				__publish_pid(__pid(p))                    -- publica el pid al lado Go
				ready:set(true)
				p:wait()                                   -- cuelga hasta que el cleanup lo mate
			end)
			ready:await()         -- la víctima ya está dentro de su wait
			nu.task.sleep(20)     -- margen para que entre en el suspend del wait
			victim:cancel()       -- su cleanup corre y mata el proceso
			local ok, err = pcall(function() victim:await() end)
			out.code = err and err.code   -- ECANCELED (observable)
		end)
	`)

	pid := <-pidCh
	if !waitDead(pid, 5*time.Second) {
		t.Fatalf("criterio de hecho: tras cancelar la task, el subproceso (pid %d) debería estar muerto", pid)
	}
	h.expectEval(`return out.code`, "ECANCELED")
}

// TestSpawnKilledByCleanupOnNormalEnd blinda la otra cara de la vida por `cleanup`:
// al terminar la task NORMALMENTE (no por cancelación), su cleanup también corre y
// mata el proceso. Garantiza que un `spawn` sin `wait` no deja procesos colgando.
func TestSpawnKilledByCleanupOnNormalEnd(t *testing.T) {
	h := newHarness(t)

	pidCh := make(chan int, 1)
	h.register("__publish_pid", func(L *lua.LState) int {
		pidCh <- int(L.CheckNumber(1))
		return 0
	})
	h.register("__pid", procPidFromUD)

	h.eval(`
		nu.task.spawn(function()
			local p = nu.proc.spawn({"sleep", "30"})
			nu.task.cleanup(function() p:kill() end)
			__publish_pid(__pid(p))
			-- la task termina aquí, sin esperar; el cleanup mata el proceso al terminar
		end)
	`)

	pid := <-pidCh
	if !waitDead(pid, 5*time.Second) {
		t.Fatalf("al terminar la task, su cleanup debería haber matado el subproceso (pid %d)", pid)
	}
}

// --- 🔒 spawn/streams: write a stdin, read_line de stdout, EOF, wait ---

// TestSpawnCatRoundTrip blinda el round-trip de streams sobre `cat`: se escribe a
// stdin, se lee la misma línea de stdout; tras `close_stdin`, `read_line` devuelve
// `nil` (EOF); `wait` devuelve code=0. Es el escenario canónico de `spawn`.
func TestSpawnCatRoundTrip(t *testing.T) {
	h := newHarness(t)

	h.eval(`
		out = {}
		nu.task.spawn(function()
			local p = nu.proc.spawn({"cat"})
			nu.task.cleanup(function() p:kill() end)
			p:write("linea uno\n")
			out.l1 = p:read_line("stdout")     -- "linea uno\n"
			p:write("linea dos\n")
			out.l2 = p:read_line("stdout")     -- "linea dos\n"
			p:close_stdin()                    -- señala EOF a cat
			out.eof = p:read_line("stdout")    -- nil: cat cerró stdout al ver EOF
			out.code = p:wait().code           -- 0
		end)
	`)

	h.expectEval(`return out.l1`, "linea uno\n")
	h.expectEval(`return out.l2`, "linea dos\n")
	h.expectEval(`return tostring(out.eof)`, "nil")
	h.expectEval(`return tostring(out.code)`, "0")
}

// TestSpawnReadRaw blinda `Proc:read(which, n?)`: lectura cruda. Un proceso que
// imprime un texto conocido se lee entero (sin `n`) hasta EOF; una lectura posterior
// da `nil` (EOF).
func TestSpawnReadRaw(t *testing.T) {
	h := newHarness(t)

	h.eval(`
		out = {}
		nu.task.spawn(function()
			local p = nu.proc.spawn({"sh", "-c", "printf 'abcdef'"})
			nu.task.cleanup(function() p:kill() end)
			out.all = p:read("stdout")        -- "abcdef" (todo hasta EOF)
			out.more = p:read("stdout")       -- nil (ya en EOF)
			out.code = p:wait().code
		end)
	`)

	h.expectEval(`return out.all`, "abcdef")
	h.expectEval(`return tostring(out.more)`, "nil")
	h.expectEval(`return tostring(out.code)`, "0")
}

// TestProcReadStderr blinda que `read_line("stderr")` lee del stream correcto: un
// proceso que escribe a stderr se lee por "stderr", no por "stdout".
func TestProcReadStderr(t *testing.T) {
	h := newHarness(t)

	h.eval(`
		out = {}
		nu.task.spawn(function()
			local p = nu.proc.spawn({"sh", "-c", "echo err 1>&2"})
			nu.task.cleanup(function() p:kill() end)
			out.err = p:read_line("stderr")     -- "err\n"
			out.outline = p:read_line("stdout") -- nil: nada en stdout
			p:wait()
		end)
	`)

	h.expectEval(`return out.err`, "err\n")
	h.expectEval(`return tostring(out.outline)`, "nil")
}

// TestProcReadInvalidStream blinda que `read*` con un `which` inválido lanza
// `EINVAL` (capturable).
func TestProcReadInvalidStream(t *testing.T) {
	h := newHarness(t)
	h.eval(`
		out = {}
		nu.task.spawn(function()
			local p = nu.proc.spawn({"cat"})
			nu.task.cleanup(function() p:kill() end)
			local ok, err = pcall(function() p:read_line("nope") end)
			out.ok = ok
			out.code = err and err.code
		end)
	`)
	h.expectEval(`return tostring(out.ok)`, "false")
	h.expectEval(`return out.code`, "EINVAL")
}

// TestProcWriteAfterCloseECLOSED blinda que escribir a stdin tras `close_stdin` lanza
// `ECLOSED` (capturable).
func TestProcWriteAfterCloseECLOSED(t *testing.T) {
	h := newHarness(t)
	h.eval(`
		out = {}
		nu.task.spawn(function()
			local p = nu.proc.spawn({"cat"})
			nu.task.cleanup(function() p:kill() end)
			p:close_stdin()
			local ok, err = pcall(function() p:write("x") end)
			out.ok = ok
			out.code = err and err.code
			p:wait()
		end)
	`)
	h.expectEval(`return tostring(out.ok)`, "false")
	h.expectEval(`return out.code`, "ECLOSED")
}

// --- Snippet Lua: run de extremo a extremo (Definition of Done §2) ---

// TestRunSnippet ejercita `nu.proc.run` desde el lado del autor de extensiones por
// el puente ⏸ real: `run(["echo","hi"])` → code=0, stdout con "hi"; exit != 0 es
// dato; sin shell.
func TestRunSnippet(t *testing.T) {
	h := newHarness(t)

	h.eval(`
		out = {}
		nu.task.spawn(function()
			local r = nu.proc.run({"echo", "hi"})
			out.code = r.code
			out.stdout = r.stdout
			local r2 = nu.proc.run({"sh", "-c", "exit 7"})
			out.code2 = r2.code
			local r3 = nu.proc.run({"echo", "$HOME"})
			out.literal = r3.stdout
		end)
	`)

	h.expectEval(`return tostring(out.code)`, "0")
	if got := h.eval(`return out.stdout`); len(got) != 1 || !strings.Contains(got[0], "hi") {
		t.Fatalf("run stdout: got %q, want que contenga \"hi\"", got)
	}
	h.expectEval(`return tostring(out.code2)`, "7")
	if got := h.eval(`return out.literal`); len(got) != 1 || strings.TrimSpace(got[0]) != "$HOME" {
		t.Fatalf("sin shell: got %q, want el literal \"$HOME\"", got)
	}
}

// TestRunTimeoutSnippet ejercita el timeout por el puente ⏸: `run` con `timeout_ms`
// sobre un `sleep` largo lanza `ETIMEOUT` (capturable con pcall).
func TestRunTimeoutSnippet(t *testing.T) {
	h := newHarness(t)

	h.eval(`
		out = {}
		nu.task.spawn(function()
			local ok, err = pcall(function()
				nu.proc.run({"sleep", "30"}, { timeout_ms = 100 })
			end)
			out.ok = ok
			out.code = err and err.code
		end)
	`)

	h.expectEval(`return tostring(out.ok)`, "false")
	h.expectEval(`return out.code`, "ETIMEOUT")
}

// TestRunNonexistentSnippet ejercita el error de arranque por el puente ⏸: un
// ejecutable inexistente lanza `ENOENT` (capturable).
func TestRunNonexistentSnippet(t *testing.T) {
	h := newHarness(t)

	h.eval(`
		out = {}
		nu.task.spawn(function()
			local ok, err = pcall(function()
				nu.proc.run({"no-existe-binario-xyz-123"})
			end)
			out.ok = ok
			out.code = err and err.code
		end)
	`)

	h.expectEval(`return tostring(out.ok)`, "false")
	h.expectEval(`return out.code`, "ENOENT")
}

// TestProcOutsideTaskEINVAL blinda que las funciones ⏸ de `proc` (run, write,
// read*, wait) fuera de una task lanzan `EINVAL` (§1.3), como el resto de ⏸; en
// cambio `spawn`/`alive` SÍ funcionan fuera de task (no son ⏸).
func TestProcOutsideTaskEINVAL(t *testing.T) {
	h := newHarness(t)

	if se := h.evalErr(`nu.proc.run({"echo","hi"})`); se.Code != CodeEINVAL {
		t.Fatalf("run fuera de task: got %s, want EINVAL", se.Code)
	}

	// alive fuera de task → funciona (no ⏸).
	h.expectEval(`return tostring(nu.proc.alive(1))`, "true")
	h.expectEval(`return tostring(nu.proc.alive(`+strconv.Itoa(1<<30)+`))`, "false")

	// spawn fuera de task → funciona (no ⏸): devuelve un handle.
	h.eval(`
		local p = nu.proc.spawn({"sleep", "30"})
		p:kill()
	`)
}

// TestProcAliveSnippetG17 ejercita `nu.proc.alive` desde Lua (G17): el pid de un
// subproceso vivo da true; el de un pid imposible, false. La comprobación de "vivo"
// la hace el propio snippet (`alive(pid_real)`) mientras el proceso sigue corriendo,
// dentro de una task que completa (el proceso se mata por cleanup al terminar).
func TestProcAliveSnippetG17(t *testing.T) {
	h := newHarness(t)
	h.register("__pid", procPidFromUD)

	h.eval(`
		out = {}
		nu.task.spawn(function()
			local p = nu.proc.spawn({"sleep", "30"})
			nu.task.cleanup(function() p:kill() end)
			out.alive_real = nu.proc.alive(__pid(p))   -- true: el proceso está vivo
			out.alive_fake = nu.proc.alive(1073741824) -- false: pid imposible (2^30)
		end)
	`)

	h.expectEval(`return tostring(out.alive_real)`, "true")
	h.expectEval(`return tostring(out.alive_fake)`, "false")
}

// --- 🔒 (best-effort) red de seguridad: el finalizer del GC mata un Proc sin refs ---

// TestSpawnFinalizerSafetyNet blinda (best-effort) la red de seguridad del GC (§6):
// un `Proc` sin referencias acaba matado por el finalizer. Forzar una recolección
// determinista es difícil, así que comprobamos lo razonable: tras dejar caer la
// única referencia a un `luaProc` (con el MISMO finalizer que `procSpawn` instala) y
// forzar el GC, el proceso ACABA muerto. Si el GC no llega a correr el finalizer en
// el plazo (no determinista por contrato), NO fallamos —limpiamos a mano y dejamos
// constancia—; `Close` lo mataría de todos modos.
func TestSpawnFinalizerSafetyNet(t *testing.T) {
	h := newHarness(t)

	cmd := newCmd([]string{"sleep", "30"}, procOpts{})
	if err := cmd.Start(); err != nil {
		t.Fatalf("no se pudo lanzar sleep: %v", err)
	}
	pid := cmd.Process.Pid

	// Crea el handle con el finalizer de producción y déjalo caer fuera de scope.
	func() {
		p := &luaProc{s: h.rt.sched, cmd: cmd}
		runtime.SetFinalizer(p, func(p *luaProc) { p.killSignal(syscall.SIGKILL) })
		_ = p
	}()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		runtime.GC()
		if !pidAlive(pid) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if pidAlive(pid) {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Logf("el finalizer no corrió en el plazo (no determinista, §6); proceso limpiado a mano")
	} else {
		_ = cmd.Wait()
	}
}

// --- Sanity: la superficie de nu.proc existe (firma §6 completa) ---

// TestProcSurface comprueba que `nu.proc` y los métodos de `Proc` están registrados,
// como prueba de humo de la sesión.
func TestProcSurface(t *testing.T) {
	h := newHarness(t)
	h.expectEval(`return type(nu.proc)`, "table")
	h.expectEval(`return type(nu.proc.run)`, "function")
	h.expectEval(`return type(nu.proc.spawn)`, "function")
	h.expectEval(`return type(nu.proc.alive)`, "function")
	h.eval(`
		local p = nu.proc.spawn({"sleep", "30"})
		assert(type(p.write) == "function")
		assert(type(p.close_stdin) == "function")
		assert(type(p.read_line) == "function")
		assert(type(p.read) == "function")
		assert(type(p.wait) == "function")
		assert(type(p.kill) == "function")
		p:kill()
	`)
}
