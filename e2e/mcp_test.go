package e2e

// Tests e2e de la extensión oficial `mcp` (S41) contra el BINARIO real. La capa
// que un test in-process (internal/runtime/mcp_test.go) NO cubre: la DECLARACIÓN
// del servidor por fichero (`mcp.toml` → `mcp.connect_configured`, nunca
// probado desde fuera), los EXIT CODES reales del proceso, el LOG en disco
// (`<data_dir>/enu.log`) como canal de degradación, y que el subproceso del
// servidor MUERE cuando el binario entero termina —todo observado desde FUERA
// del proceso (ficheros, códigos de salida, señales del SO), sin instrumentar
// ni el runtime Go ni el estado Lua—.
//
// Lo que NO duplicamos (ya blindado in-process): el ciclo tools/list→registro→
// tools/call→resultado, el mapeo de isError, y la introspección del pid vía
// handle. Aquí el valor es el arranque real y sus efectos de disco.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Fixture: un servidor MCP de prueba propio, AMPLIADO con tres efectos de disco
// observables desde fuera del proceso `enu`. Es una copia del `mcpServerSource`
// del test in-process (no importable: vive en un `_test.go` de otro paquete)
// con tres añadidos controlados por ARGUMENTOS DE LÍNEA DE COMANDOS (y, el tercero,
// por una variable de entorno):
//
//   - -pidfile <ruta>: al arrancar, escribe su propio PID a ese fichero. Deja
//     ver desde el SO que el turno lanzó el proceso y, tras terminar `enu`,
//     comprobar que murió (escenario 2).
//   - -invocations <ruta>: en cada `tools/call` a `echo`, añade una línea con
//     el texto recibido. Prueba que el servidor REAL, por stdio, ejecutó la
//     tool (no un stub): el efecto es un fichero, no el historial Lua.
//   - -envfile <ruta>: al arrancar, escribe el valor de $MCP_TEST_ENV a ese
//     fichero. Prueba que el `env` declarado en `mcp.toml` LLEGA al subproceso.
//
// pidfile e invocations viajan por ARGV (siempre funcionó y es lo más simple). El
// tercer efecto se alimenta por `env` de `mcp.toml`, que ANTES no llegaba al hijo
// (G59: la primitiva `enu.proc.spawn` solo entendía la tabla { K = V } y el array
// "K=V" de `mcp.toml` se ignoraba en silencio) y AHORA sí, porque `normalize_env`
// lo traduce en el borde TOML→spawn. Lo ejerce TestMcpE2EConfiguredEnvReachesServer.
// ---------------------------------------------------------------------------

const mcpTestServerSource = `package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"os"
	"strconv"
)

type rpc struct {
	JSONRPC string          ` + "`json:\"jsonrpc\"`" + `
	ID      json.RawMessage ` + "`json:\"id,omitempty\"`" + `
	Method  string          ` + "`json:\"method\"`" + `
	Params  json.RawMessage ` + "`json:\"params,omitempty\"`" + `
}

func respond(w *bufio.Writer, id json.RawMessage, result interface{}) {
	out := map[string]interface{}{"jsonrpc": "2.0", "id": json.RawMessage(id), "result": result}
	b, _ := json.Marshal(out)
	w.Write(b)
	w.WriteByte('\n')
	w.Flush()
}

// appendLine añade una línea a un fichero (O_APPEND|O_CREATE); best-effort.
func appendLine(path, line string) {
	if path == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	f.WriteString(line + "\n")
}

func main() {
	pidfile := flag.String("pidfile", "", "fichero donde escribir el PID al arrancar")
	invocations := flag.String("invocations", "", "fichero al que añadir cada tools/call de echo")
	envfile := flag.String("envfile", "", "fichero donde escribir el valor de $MCP_TEST_ENV al arrancar")
	flag.Parse()

	// Efecto de disco 1: al arrancar, dejamos el PID en el pidfile (si se pidió).
	if *pidfile != "" {
		os.WriteFile(*pidfile, []byte(strconv.Itoa(os.Getpid())), 0o644)
	}

	// Efecto de disco 3 (G59, parte 2): el valor de una env var declarada en mcp.toml.
	// Si el env array NO llega al hijo (la grieta), $MCP_TEST_ENV está vacío y el
	// fichero queda vacío; si llega (el arreglo por normalize_env), trae su valor. Es
	// el discriminador del fix frente a la regresión.
	if *envfile != "" {
		os.WriteFile(*envfile, []byte(os.Getenv("MCP_TEST_ENV")), 0o644)
	}

	r := bufio.NewReader(os.Stdin)
	w := bufio.NewWriter(os.Stdout)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) == 0 && err != nil {
			return
		}
		var msg rpc
		if json.Unmarshal(line, &msg) != nil {
			continue
		}
		switch msg.Method {
		case "initialize":
			respond(w, msg.ID, map[string]interface{}{
				"protocolVersion": "2025-06-18",
				"capabilities":    map[string]interface{}{"tools": map[string]interface{}{}},
				"serverInfo":      map[string]interface{}{"name": "test-mcp", "version": "0.1.0"},
			})
		case "notifications/initialized":
			// notificación: sin respuesta.
		case "tools/list":
			respond(w, msg.ID, map[string]interface{}{
				"tools": []map[string]interface{}{
					{
						"name":        "echo",
						"description": "Devuelve el texto recibido.",
						"inputSchema": map[string]interface{}{
							"type":       "object",
							"properties": map[string]interface{}{"text": map[string]interface{}{"type": "string"}},
							"required":   []string{"text"},
						},
					},
				},
			})
		case "tools/call":
			var p struct {
				Name      string                 ` + "`json:\"name\"`" + `
				Arguments map[string]interface{} ` + "`json:\"arguments\"`" + `
			}
			json.Unmarshal(msg.Params, &p)
			if p.Name == "echo" {
				txt, _ := p.Arguments["text"].(string)
				// Efecto de disco 2: cada invocación real de echo deja rastro.
				appendLine(*invocations, txt)
				respond(w, msg.ID, map[string]interface{}{
					"content": []map[string]interface{}{{"type": "text", "text": "eco: " + txt}},
				})
			} else {
				respond(w, msg.ID, map[string]interface{}{
					"content": []map[string]interface{}{{"type": "text", "text": "tool desconocida"}},
					"isError": true,
				})
			}
		default:
			if len(msg.ID) > 0 {
				out := map[string]interface{}{"jsonrpc": "2.0", "id": json.RawMessage(msg.ID),
					"error": map[string]interface{}{"code": -32601, "message": "method not found: " + msg.Method}}
				b, _ := json.Marshal(out)
				w.Write(b)
				w.WriteByte('\n')
				w.Flush()
			}
		}
	}
}
`

var (
	mcpTestServerOnce sync.Once
	mcpTestServerBin  string
	mcpTestServerOut  string // salida del build si falló (para el mensaje de error)
	mcpTestServerErr  error
)

// buildMcpTestServer compila (una vez por ejecución de la suite) el servidor MCP
// de prueba a un binario temporal y devuelve su ruta. Mismo patrón que el arnés
// usa para el binario `enu`: `go build` con CGO desactivado, sin red. Es un
// HELPER PRIVADO de este fichero: el arnés no ofrece un compilador de fixtures
// auxiliares y el `buildMCPServer` in-process vive en otro paquete (no importable).
func buildMcpTestServer(t *testing.T) string {
	t.Helper()
	mcpTestServerOnce.Do(func() {
		dir, err := os.MkdirTemp("", "enu-e2e-mcpserver-")
		if err != nil {
			mcpTestServerErr = err
			return
		}
		src := filepath.Join(dir, "main.go")
		if err := os.WriteFile(src, []byte(mcpTestServerSource), 0o644); err != nil {
			mcpTestServerErr = err
			return
		}
		bin := filepath.Join(dir, "mcpserver")
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, "go", "build", "-o", bin, src)
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			mcpTestServerErr = err
			mcpTestServerOut = string(out)
			return
		}
		mcpTestServerBin = bin
	})
	if mcpTestServerErr != nil {
		t.Fatalf("no se pudo compilar el servidor MCP de prueba: %v\n%s", mcpTestServerErr, mcpTestServerOut)
	}
	return mcpTestServerBin
}

// writeMcpToml escribe `mcp.toml` en el ConfigDir con un único servidor `srv`
// cuyo argv se pasa tal cual. Los efectos de disco del servidor de prueba
// (pidfile, invocations) viajan como argumentos DENTRO de `command`, no por
// `env` (que la primitiva ignora si es array; ver la nota del fixture). Helper
// privado: el arnés cubre enu.toml/providers/agent, pero no la config de `mcp`.
func writeMcpToml(t *testing.T, ws *Workspace, command []string) {
	t.Helper()
	var b strings.Builder
	b.WriteString("[servers.srv]\n")
	b.WriteString("command = [")
	for i, c := range command {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(tomlString(c))
	}
	b.WriteString("]\n")
	ws.WriteConfig(t, "mcp.toml", b.String())
}

// tomlString serializa `s` como cadena básica TOML, escapando `\` y `"`. Las
// rutas de t.TempDir() (bajo /var/folders o /tmp) no traen caracteres exóticos,
// pero escapamos por corrección.
func tomlString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

// tomlArray serializa un []string como array TOML de cadenas básicas (`["a", "b"]`).
func tomlArray(items []string) string {
	var b strings.Builder
	b.WriteString("[")
	for i, it := range items {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(tomlString(it))
	}
	b.WriteString("]")
	return b.String()
}

// writeMcpTomlWithEnv es como writeMcpToml pero añade la línea `env = [...]` al
// servidor `srv` (array "K=V", el formato documentado de mcp.toml). Ejercita que el
// env declarado LLEGA al subproceso (G59, parte 2): `enu.proc.spawn` solo entendía la
// tabla { K = V }, y `normalize_env` traduce el array antes del spawn.
func writeMcpTomlWithEnv(t *testing.T, ws *Workspace, command []string, env []string) {
	t.Helper()
	var b strings.Builder
	b.WriteString("[servers.srv]\n")
	b.WriteString("command = " + tomlArray(command) + "\n")
	b.WriteString("env = " + tomlArray(env) + "\n")
	ws.WriteConfig(t, "mcp.toml", b.String())
}

// ---------------------------------------------------------------------------
// Escenario 1 (MÍNIMO IMPRESCINDIBLE): ciclo completo con un servidor MCP real
// DECLARADO EN `mcp.toml` e invocado por un turno de agente. Servidor real por
// stdio, leído del fichero (nunca `mcp.connect` a mano), su tool ejecutada por
// el agente, y el resultado en el texto final —todo observado desde fuera del
// proceso (dos ficheros de disco) a través del binario compilado—.
//
// Este test conduce el turno con `enu -e` en UNA task que llama a
// `mcp.connect_configured` (que lee `mcp.toml`, el criterio "declarado en disco") y,
// con la conexión aún viva en esa misma task, corre un `agent.session` contra el
// adaptador anthropic real (sobre el FakeProvider). Ejerce el camino PROGRAMÁTICO
// (`require("mcp")` + `connect_configured` + `agent.session` en una task del autor).
// El camino HEADLESS `-p` —que el enunciado original pedía y que antes estaba roto de
// fábrica (G59)— lo cubre ahora TestMcpE2EAgentInvokesConfiguredToolHeadless, desde
// que el driver del CLI conecta MCP en la task del turno. Ambos son e2e del binario:
// mcp.toml real, subproceso real, JSON-RPC/stdio real, HTTP/SSE real.
// ---------------------------------------------------------------------------

func TestMcpE2EAgentInvokesConfiguredTool(t *testing.T) {
	t.Run("dado_mcp_toml_con_servidor_cuando_el_agente_pide_la_tool_entonces_el_resultado_llega_a_disco", func(t *testing.T) {
		ws := NewWorkspace(t)
		bin := buildMcpTestServer(t)
		tmp := t.TempDir()
		invLog := filepath.Join(tmp, "invocations.log") // rastro de cada tools/call real
		replyFile := filepath.Join(tmp, "reply.txt")    // texto final del asistente

		ws.WriteEnuToml(t, "providers", "sessions", "agent", "mcp")
		fp := NewFakeProvider(t)
		ws.UseFakeProvider(t, fp)
		writeMcpToml(t, ws, []string{bin, "-invocations", invLog})

		// El modelo pide la tool MCP y, tras el tool_result, redacta el texto final.
		fp.PushToolUse("call-1", "mcp__srv__echo", map[string]any{"text": "hola MCP"})
		fp.PushText("la tool dijo: eco: hola MCP")

		// El driver Lua: connect_configured lee mcp.toml y deja las conexiones VIVAS
		// en esta task; la misma task corre la sesión, así la conexión sigue en pie
		// al invocar. El texto final del turno se vuelca a un fichero (observable
		// externo, como el `-p` lo vuelca a stdout).
		res := ws.Run(t, RunOpts{Args: []string{"-e", driveConfiguredToolLua(invLogAllow, replyFile)}})

		if res.ExitCode != 0 {
			t.Fatalf("exit: got %d, want 0 (stdout=%q, stderr=%q)", res.ExitCode, res.Stdout, res.Stderr)
		}
		if fp.RequestCount() < 2 {
			t.Fatalf("el loop de tools debía disparar >=2 requests; got %d", fp.RequestCount())
		}
		// El texto final del asistente (tras el tool_result) llegó completo.
		reply, err := os.ReadFile(replyFile)
		if err != nil {
			t.Fatalf("el turno debía haber dejado el texto final en %s: %v (stderr=%q)", replyFile, err, res.Stderr)
		}
		if !strings.Contains(string(reply), "la tool dijo: eco: hola MCP") {
			t.Fatalf("el texto final debía traer el eco de la tool; got %q", string(reply))
		}
		// Prueba de que el subproceso REAL ejecutó tools/call por stdio: el fichero de
		// invocaciones tiene exactamente una línea con el texto echoado.
		data, err := os.ReadFile(invLog)
		if err != nil {
			t.Fatalf("el servidor MCP debía haber escrito %s: %v", invLog, err)
		}
		lines := nonEmptyLines(string(data))
		if len(lines) != 1 || !strings.Contains(lines[0], "hola MCP") {
			t.Fatalf("invocations.log debía tener 1 línea con \"hola MCP\"; got %q", string(data))
		}
	})
}

// invLogAllow marca, en driveConfiguredToolLua, que la sesión concede la tool con
// `allow` (el caso del escenario 1). Es un simple booleano legible.
const invLogAllow = true

// driveConfiguredToolLua construye el script `-e` que conduce el turno: conecta
// los servidores de `mcp.toml` con `connect_configured` (leyéndolo de disco),
// abre un `agent.session` contra el modelo real del fake, envía un mensaje que
// el fake resuelve pidiendo la tool MCP, y vuelca el texto final del asistente a
// `replyFile`. Si `allow` es true, concede `mcp__srv__echo` (nombre EXACTO, sin
// glob —G53/ADR-023—); si es false, no la concede (queda a merced de la valla de
// confianza). Todo dentro de una task, con pcall para no colgar el drenaje.
func driveConfiguredToolLua(allow bool, replyFile string) string {
	perms := ""
	if allow {
		perms = `, permissions = { allow = { "mcp__srv__echo" } }`
	}
	return `enu.task.spawn(function()
	  local ok, e = pcall(function()
	    local mcp = require("mcp")
	    local agent = require("agent")
	    local conns = mcp.connect_configured()   -- lee mcp.toml y conecta (declaración en disco)
	    local s = agent.session{ model = "anthropic/opus", no_store = true` + perms + ` }
	    local reply = s:send("usa la tool echo del servidor mcp")
	    local txt = ""
	    for _, b in ipairs(reply and reply.content or {}) do
	      if b.type == "text" then txt = txt .. b.text end
	    end
	    enu.fs.write("` + replyFile + `", txt)
	    s:close()
	    for _, c in ipairs(conns) do c:close() end
	  end)
	  if not ok then
	    enu.fs.write("` + replyFile + `", "ERR: " .. tostring((type(e) == "table" and (e.message or e.code)) or e))
	  end
	end)
	return "ok"`
}

// ---------------------------------------------------------------------------
// G59 — el auto-connect de `mcp.toml` en headless `-p`, ahora SERVIBLE. Antes la
// task efímera del auto-connect (en el `init.lua` de mcp) se autolimpiaba durante
// `Boot`, así que las tools llegaban muertas (stubs "desconectado") al turno; ahora
// el driver del CLI (`cmd/enu/main.go`) conecta los servidores EN la task del turno,
// ANTES de `agent.session`, y sus tools entran VIVAS en el snapshot de la sesión.
// Estos tres tests ejercen el `-p` REAL (no el rodeo `-e` del escenario 1): invocación
// concedida, denegación (exit 3, antes inalcanzable) y el `env` de `mcp.toml`.
// ---------------------------------------------------------------------------

// TestMcpE2EAgentInvokesConfiguredToolHeadless (G59, parte 1): con `--auto-permissions`,
// un turno de `enu -p` invoca la tool de un servidor MCP declarado en `mcp.toml`. El
// servidor REAL ejecuta `tools/call` (rastro en invocations.log) y el turno sale 0. Es
// el escenario 1 conducido por el `-p` real, ya no por el rodeo `-e`.
func TestMcpE2EAgentInvokesConfiguredToolHeadless(t *testing.T) {
	ws := NewWorkspace(t)
	bin := buildMcpTestServer(t)
	invLog := filepath.Join(t.TempDir(), "invocations.log")

	ws.WriteEnuToml(t, "providers", "sessions", "agent", "mcp")
	fp := NewFakeProvider(t)
	ws.UseFakeProvider(t, fp)
	writeMcpToml(t, ws, []string{bin, "-invocations", invLog})

	fp.PushToolUse("call-1", "mcp__srv__echo", map[string]any{"text": "hola MCP"})
	fp.PushText("la tool dijo: eco: hola MCP")

	res := ws.Run(t, RunOpts{Args: []string{"-p", "usa echo", "--auto-permissions"}})
	if res.ExitCode != 0 {
		t.Fatalf("exit: got %d, want 0 (stdout=%q, stderr=%q)", res.ExitCode, res.Stdout, res.Stderr)
	}
	if !strings.Contains(res.Stdout, "la tool dijo: eco: hola MCP") {
		t.Fatalf("stdout debía traer el texto del 2º turno; got %q (stderr=%q)", res.Stdout, res.Stderr)
	}
	if fp.RequestCount() < 2 {
		t.Fatalf("el loop de tools debía disparar >=2 requests; got %d", fp.RequestCount())
	}
	// La tool MCP llegó VIVA al turno: el servidor real ejecutó tools/call.
	data, err := os.ReadFile(invLog)
	if err != nil {
		t.Fatalf("el servidor MCP debía haber escrito %s (¿la tool llegó viva al turno?): %v", invLog, err)
	}
	lines := nonEmptyLines(string(data))
	if len(lines) != 1 || !strings.Contains(lines[0], "hola MCP") {
		t.Fatalf("invocations.log debía tener 1 línea con \"hola MCP\"; got %q", string(data))
	}
}

// TestMcpE2EAgentDeniesConfiguredToolHeadless (G59): RECUPERA el escenario antes
// "inalcanzable" (la tool MCP nunca llegaba viva a un `-p`). SIN `--auto-permissions`,
// la tool MCP (default "ask", tercero) se deniega en headless por AUSENCIA de UI (G20)
// → exit 3. El deny es de PERMISO —el que `--auto-permissions` concedería—, no un stub
// silencioso: el servidor real NUNCA se invoca (el pipeline de §5 deniega antes del
// handler), y el turno sobrevive (el modelo recibe el tool_result denegado y responde).
func TestMcpE2EAgentDeniesConfiguredToolHeadless(t *testing.T) {
	ws := NewWorkspace(t)
	bin := buildMcpTestServer(t)
	invLog := filepath.Join(t.TempDir(), "invocations.log")

	ws.WriteEnuToml(t, "providers", "sessions", "agent", "mcp")
	fp := NewFakeProvider(t)
	ws.UseFakeProvider(t, fp)
	writeMcpToml(t, ws, []string{bin, "-invocations", invLog})

	fp.PushToolUse("call-1", "mcp__srv__echo", map[string]any{"text": "hola MCP"})
	fp.PushText("no pude, sin permiso")

	res := ws.Run(t, RunOpts{Args: []string{"-p", "usa echo"}}) // sin --auto-permissions
	if res.ExitCode != 3 {
		t.Fatalf("exit: got %d, want 3 (stdout=%q, stderr=%q)", res.ExitCode, res.Stdout, res.Stderr)
	}
	if !strings.Contains(res.Stderr, "--auto-permissions") && !strings.Contains(res.Stderr, "allow") {
		t.Fatalf("stderr debía nombrar --auto-permissions o allow; got %q", res.Stderr)
	}
	// El turno sobrevive al deny: 2 requests (tool_use denegado → respuesta).
	if fp.RequestCount() != 2 {
		t.Fatalf("el turno debía sobrevivir al deny (2 requests); got %d", fp.RequestCount())
	}
	// El servidor real NO se invocó: el pipeline denegó ANTES del handler.
	if _, err := os.Stat(invLog); err == nil {
		data, _ := os.ReadFile(invLog)
		t.Fatalf("el deny debía impedir el tools/call; invocations.log no debía existir, got %q", string(data))
	}
}

// TestMcpE2EConfiguredEnvReachesServer (G59, parte 2): un `env` declarado en `mcp.toml`
// (array "K=V") LLEGA al subproceso del servidor. El servidor escribe $MCP_TEST_ENV a
// un fichero al arrancar; conectar el servidor durante el turno basta para lanzarlo.
// Antes el array se ignoraba en silencio (`enu.proc.spawn` solo entendía la tabla);
// ahora `normalize_env` lo traduce en el borde TOML→spawn.
func TestMcpE2EConfiguredEnvReachesServer(t *testing.T) {
	ws := NewWorkspace(t)
	bin := buildMcpTestServer(t)
	envFile := filepath.Join(t.TempDir(), "env.txt")

	ws.WriteEnuToml(t, "providers", "sessions", "agent", "mcp")
	fp := NewFakeProvider(t)
	ws.UseFakeProvider(t, fp)
	writeMcpTomlWithEnv(t, ws, []string{bin, "-envfile", envFile}, []string{"MCP_TEST_ENV=hola-env"})

	fp.PushText("listo") // un turno mínimo: conectar el servidor ya lo arranca.

	res := ws.Run(t, RunOpts{Args: []string{"-p", "saluda"}})
	if res.ExitCode != 0 {
		t.Fatalf("exit: got %d, want 0 (stderr=%q)", res.ExitCode, res.Stderr)
	}
	if !waitFile(envFile, 2*time.Second) {
		t.Fatalf("el servidor MCP debía haber escrito %s al arrancar (¿el env llegó?)", envFile)
	}
	data, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("no se pudo leer %s: %v", envFile, err)
	}
	if strings.TrimSpace(string(data)) != "hola-env" {
		t.Fatalf("el env de mcp.toml debía llegar al subproceso: envfile=%q, want \"hola-env\"", string(data))
	}
}

// TestMcpE2EConfiguredEnvEmptyInheritsG65 (G65): un `env = []` (array VACÍO) en
// `mcp.toml` NO reemplaza con entorno vacío — se trata como «nada que añadir» y el
// servidor HEREDA el entorno del proceso `enu` padre. `normalize_env` colapsa el env
// vacío a `nil` (no `{}`) para que el primitivo (post-G65: un array/tabla vacíos
// REEMPLAZAN-con-vacío) reciba herencia y el servidor no pierda PATH/HOME y muera.
//
// Discriminador del fix: exportamos MCP_TEST_ENV en el entorno del `enu` lanzado
// (RunOpts.Env) y el servidor escribe su $MCP_TEST_ENV a disco. Con herencia (env nil)
// el fichero trae el valor del padre; si `normalize_env` devolviera `{}` en vez de
// `nil`, el spawn recibiría `[]string{}` (reemplazo-con-vacío) y el fichero quedaría
// vacío. Espeja TestMcpE2EConfiguredEnvReachesServer con la mecánica de eco invertida:
// aquí el valor NO viene de mcp.toml sino del padre, y llega solo por herencia.
func TestMcpE2EConfiguredEnvEmptyInheritsG65(t *testing.T) {
	ws := NewWorkspace(t)
	bin := buildMcpTestServer(t)
	envFile := filepath.Join(t.TempDir(), "env.txt")

	ws.WriteEnuToml(t, "providers", "sessions", "agent", "mcp")
	fp := NewFakeProvider(t)
	ws.UseFakeProvider(t, fp)
	// env = [] (array vacío): «heredar», no «reemplazar con vacío».
	writeMcpTomlWithEnv(t, ws, []string{bin, "-envfile", envFile}, []string{})

	fp.PushText("listo") // un turno mínimo: conectar el servidor ya lo arranca.

	// MCP_TEST_ENV vive en el entorno del PADRE (enu), no en mcp.toml: solo llega al
	// servidor si el spawn heredó el entorno (env nil), no si lo reemplazó con vacío.
	res := ws.Run(t, RunOpts{Args: []string{"-p", "saluda"}, Env: []string{"MCP_TEST_ENV=heredado-del-padre"}})
	if res.ExitCode != 0 {
		t.Fatalf("exit: got %d, want 0 (stderr=%q)", res.ExitCode, res.Stderr)
	}
	if !waitFile(envFile, 2*time.Second) {
		t.Fatalf("el servidor MCP debía haber escrito %s al arrancar (¿arrancó?)", envFile)
	}
	data, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("no se pudo leer %s: %v", envFile, err)
	}
	if strings.TrimSpace(string(data)) != "heredado-del-padre" {
		t.Fatalf("con env=[] el servidor debía HEREDAR el entorno del padre "+
			"(normalize_env colapsa vacío→nil, no {}): envfile=%q, want \"heredado-del-padre\"", string(data))
	}
}

// ---------------------------------------------------------------------------
// Escenario 2: cleanup del subproceso al terminar el binario. El servidor deja
// su PID en un fichero; tras `enu` retornar, ese PID debe estar MUERTO. La
// garantía externa es "un `enu -p ...` que termina no deja ningún subproceso MCP
// huérfano" —lo cumplen tanto el cierre de las conexiones que hace el driver al
// terminar el turno (G59) como, red final, el `defer rt.Close()` → stopAllProcs()
// de main.go—. Sin instrumentar el runtime Go: solo señales del SO.
// ---------------------------------------------------------------------------

func TestMcpE2EServerKilledOnProcessExit(t *testing.T) {
	t.Run("dado_servidor_mcp_conectado_cuando_el_binario_enu_termina_entonces_el_subproceso_esta_muerto", func(t *testing.T) {
		ws := NewWorkspace(t)
		bin := buildMcpTestServer(t)
		pidfile := filepath.Join(t.TempDir(), "pid")

		ws.WriteEnuToml(t, "providers", "sessions", "agent", "mcp")
		fp := NewFakeProvider(t)
		ws.UseFakeProvider(t, fp)
		writeMcpToml(t, ws, []string{bin, "-pidfile", pidfile})

		// Turno sin tool: basta con que el auto-connect lance el servidor.
		fp.PushText("listo")

		res := ws.Run(t, RunOpts{Args: []string{"-p", "saluda"}})
		if res.ExitCode != 0 {
			t.Fatalf("exit: got %d, want 0 (stderr=%q)", res.ExitCode, res.Stderr)
		}

		// Run es bloqueante: al retornar, `enu` ya terminó. Leemos el PID que dejó el
		// servidor y comprobamos que el proceso ya no existe.
		if !waitFile(pidfile, 2*time.Second) {
			t.Fatalf("el servidor MCP debía haber escrito su pidfile (%s); ¿no arrancó?", pidfile)
		}
		pid := readPid(t, pidfile)

		// Contraste (descarta falsos negativos del helper): un servidor idéntico
		// lanzado FUERA de `enu` sí se detecta vivo antes de matarlo.
		assertHelperDetectsLiveProcess(t, bin)

		if !waitPidDead(pid, 5*time.Second) {
			t.Fatalf("el servidor MCP (pid %d) debía estar muerto tras terminar enu "+
				"(cleanup del auto-connect y/o stopAllProcs)", pid)
		}
	})
}

// assertHelperDetectsLiveProcess lanza el servidor de prueba con os/exec (fuera
// de `enu`) y confirma que pidAlive lo ve vivo, para descartar que waitPidDead dé
// un falso "muerto" por un bug del propio helper. Lo mata al terminar.
func assertHelperDetectsLiveProcess(t *testing.T, bin string) {
	t.Helper()
	cmd := exec.Command(bin)
	// Sin stdin conectado el servidor lee EOF y sale; le damos una tubería viva.
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("contraste: StdinPipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("contraste: no se pudo lanzar el servidor de control: %v", err)
	}
	defer func() {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()
	if !pidAlive(cmd.Process.Pid) {
		t.Fatalf("contraste: el helper pidAlive no detecta vivo un proceso recién lanzado (pid %d)", cmd.Process.Pid)
	}
}

// ---------------------------------------------------------------------------
// Escenario 3: `mcp.toml` mal formado no rompe el arranque. El turno se ejecuta
// con normalidad y la degradación se registra en `<data_dir>/enu.log` (WARN),
// nunca en stderr (por diseño de enu.log). Aquí el auto-connect de `-p` SÍ es
// suficiente: solo observamos que el fallo degrada en el log, no que la tool
// llegue a un turno.
// ---------------------------------------------------------------------------

func TestMcpE2ETomlMalformedDoesNotBlockBoot(t *testing.T) {
	t.Run("dado_mcp_toml_invalido_cuando_arranca_headless_entonces_boot_ok_y_warning_en_el_log", func(t *testing.T) {
		ws := NewWorkspace(t)

		ws.WriteEnuToml(t, "providers", "sessions", "agent", "mcp")
		fp := NewFakeProvider(t)
		ws.UseFakeProvider(t, fp)
		// TOML roto: cabecera de tabla sin cerrar. `enu.toml.decode` lo rechaza.
		ws.WriteConfig(t, "mcp.toml", "[servers.srv\ncommand = [\"x\"]\n")

		fp.PushText("hola sin mcp")

		res := ws.Run(t, RunOpts{Args: []string{"-p", "saluda"}})

		if res.ExitCode != 0 {
			t.Fatalf("exit: got %d, want 0 (stderr=%q)", res.ExitCode, res.Stderr)
		}
		if !strings.Contains(res.Stdout, "hola sin mcp") {
			t.Fatalf("el turno debía ejecutarse pese al mcp.toml roto; stdout=%q", res.Stdout)
		}
		// El fallo del auto-connect degrada en el LOG, no en pantalla.
		logText := readLog(t, ws)
		if !strings.Contains(logText, "WARN") || !strings.Contains(logText, "mal formado") {
			t.Fatalf("enu.log debía traer un WARN de mcp.toml mal formado; got:\n%s", logText)
		}
		if strings.Contains(res.Stderr, "mcp") || strings.Contains(res.Stderr, "mal formado") {
			t.Fatalf("el fallo de MCP no debía asomar a stderr; got %q", res.Stderr)
		}
	})
}

// ---------------------------------------------------------------------------
// Escenario 4: comando de servidor inexistente (ENOENT) degrada solo ese
// servidor. TOML válido pero `command` a una ruta que no existe: el fallo lo
// atrapa el pcall POR SERVIDOR de connect_configured (mensaje distinto al del
// escenario 3: nombra el servidor, no "mal formado").
// ---------------------------------------------------------------------------

func TestMcpE2EServerCommandNotFoundDegradesGracefully(t *testing.T) {
	t.Run("dado_command_inexistente_cuando_arranca_headless_entonces_boot_ok_y_solo_ese_servidor_degrada", func(t *testing.T) {
		ws := NewWorkspace(t)

		ws.WriteEnuToml(t, "providers", "sessions", "agent", "mcp")
		fp := NewFakeProvider(t)
		ws.UseFakeProvider(t, fp)
		// TOML VÁLIDO, pero el binario del servidor no existe: falla al hacer spawn.
		missing := filepath.Join(t.TempDir(), "no", "existe", "mcpserver-fake")
		writeMcpToml(t, ws, []string{missing})

		fp.PushText("hola sin mcp")

		res := ws.Run(t, RunOpts{Args: []string{"-p", "saluda"}})

		if res.ExitCode != 0 {
			t.Fatalf("exit: got %d, want 0 (stderr=%q)", res.ExitCode, res.Stderr)
		}
		if !strings.Contains(res.Stdout, "hola sin mcp") {
			t.Fatalf("el turno debía ejecutarse pese al servidor caído; stdout=%q", res.Stdout)
		}
		logText := readLog(t, ws)
		// El pcall por servidor loguea "no se pudo conectar el servidor \"srv\"".
		if !strings.Contains(logText, "WARN") || !strings.Contains(logText, "no se pudo conectar") || !strings.Contains(logText, "srv") {
			t.Fatalf("enu.log debía traer un WARN nombrando el servidor caído; got:\n%s", logText)
		}
		// A diferencia del escenario 3, aquí el TOML NO estaba mal formado.
		if strings.Contains(logText, "mal formado") {
			t.Fatalf("un command inexistente no es un mcp.toml mal formado; log:\n%s", logText)
		}
	})
}

// ---------------------------------------------------------------------------
// Utilidades locales.
// ---------------------------------------------------------------------------

// nonEmptyLines parte un texto en líneas descartando las vacías (para contar
// líneas reales de un fichero append-only).
func nonEmptyLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

// readPid lee y parsea el PID que el servidor de prueba dejó en su pidfile.
func readPid(t *testing.T, pidfile string) int {
	t.Helper()
	data, err := os.ReadFile(pidfile)
	if err != nil {
		t.Fatalf("no se pudo leer el pidfile %s: %v", pidfile, err)
	}
	pid := 0
	for _, c := range strings.TrimSpace(string(data)) {
		if c < '0' || c > '9' {
			t.Fatalf("pidfile con contenido no numérico: %q", string(data))
		}
		pid = pid*10 + int(c-'0')
	}
	if pid <= 0 {
		t.Fatalf("pidfile con pid inválido: %q", string(data))
	}
	return pid
}

// readLog lee `<data_dir>/enu.log` del workspace. El log se abre perezosamente
// en la primera escritura, así que ausente = nadie logueó (fallo del test que lo
// espera).
func readLog(t *testing.T, ws *Workspace) string {
	t.Helper()
	path := filepath.Join(ws.DataDir, "enu.log")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("no se pudo leer el log %s (¿nadie logueó?): %v", path, err)
	}
	return string(data)
}

// ---------------------------------------------------------------------------
// HALLAZGOS que esta suite destapó.
//
//   1 y 2. RESUELTOS (G59; ver docs/findings/g59-el-auto-connect-de-mcp-toml.md).
//      (1) El auto-connect de `mcp.toml` era inservible en headless `-p`: la task
//      efímera del auto-connect (antes en embedded/mcp/init.lua:35) se autolimpiaba
//      DURANTE `Boot`, así que sus tools llegaban muertas (stubs de "desconectado") al
//      turno. (2) El `env` array de `mcp.toml` no llegaba al subproceso porque
//      `enu.proc.spawn` solo interpreta `env` como tabla { K = V } y el array `[]any`
//      se ignoraba en silencio. RESOLUCIÓN: el driver del CLI (cmd/enu/main.go) conecta
//      MCP en la task del TURNO, ANTES de `agent.session` —así las tools entran vivas en
//      el snapshot de la sesión— y las cierra al terminar el turno; y `normalize_env`
//      (mcp/lua/mcp/init.lua) traduce el `env` array→tabla en el borde TOML→spawn. Los
//      tres tests TestMcpE2EAgent{Invokes,Denies}ConfiguredToolHeadless y
//      TestMcpE2EConfiguredEnvReachesServer ejercen ambas resoluciones en `-p` REAL
//      (el escenario deny → exit 3, antes "inalcanzable", ya es alcanzable). PENDIENTE:
//      el auto-connect INTERACTIVO sigue roto (necesita una task de fondo o rediseñar el
//      snapshot del chat) → G64; el silent-ignore de un `env` no-tabla en la propia
//      `enu.proc.spawn`, como grieta del primitivo → G65.
//
//   3. (Menor, ya anotado por el escenarista) El comentario de
//      embedded/mcp/lua/mcp/init.lua:24 sugiere `allow = {"mcp__<srv>__*"}` (glob),
//      pero agente.md §5 (G53/ADR-023) prohíbe el glob sobre nombres: el patrón
//      casa por nombre EXACTO. Un autor de config que copie ese comentario
//      escribiría un `allow` que no concede nada. Los escenarios usan el nombre
//      exacto (`mcp__srv__echo`).
// ---------------------------------------------------------------------------
