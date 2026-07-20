package runtime

// Pantalla de runtime desnudo (api.md §14, G21, S33). Cuando enu arranca con un
// TTY interactivo y NINGÚN plugin activo (ni de usuario ni embebido activado por
// `enu.toml`), el kernel pinta —ANTES de correr Lua de producto— una pantalla FIJA
// hecha SOLO de sus propias capacidades: la versión y el nivel de API
// (`enu.version`), las rutas de config y de plugins (`enu.config.dir` y los
// directorios de plugins), el catálogo de extensiones embebidas DISPONIBLES
// (`embeddedNames`, embed.go) y las ACCIONES que ofrece. No es la UI de un
// producto sino la del propio runtime: las extensiones embebidas y su activación
// son capacidad del loader, así que el kernel habla de lo suyo (filosofia.md §2).
// Render FIJO (celdas/Block sobre el compositor de S29), pre-Lua, sin widgets ni
// lógica de producto. Es lo que se ve SIEMPRE que enu arranca sin nada activo, no
// un diálogo de primera vez.
//
// CONDICIÓN (§14): se muestra SSI hay superficie de UI (`rt.uiActive`: un TTY
// interactivo, o `WithForceUI` en test) Y no hay plugins activos. Sin TTY NO se
// pinta nada: el runtime arranca "desnudo" (Boot normal) y los errores por
// extensión inactiva siguen siendo accionables (nombran la línea de `enu.toml`,
// S12). Con cualquier plugin activo tampoco se pinta: el arranque sigue su curso.
//
// ACCIONES (§14): (1) activar el CONJUNTO oficial de producto → escribe `plugins.enabled`
// en `config.dir()/enu.toml` con las extensiones embebidas del conjunto de producto (todas
// menos el andamiaje `example`, ADR-015) y CONTINÚA el arranque canónico (`Boot`), SIN red
// (la activación de una embebida sale del binario, ADR-010); (2) activar extensiones SUELTAS
// (p. ej. solo `repl`) → escribe solo esas; (3) salir. La elección real con el TECLADO usa el input de S31 + el
// driver de TTY; en este entorno HEADLESS no hay TTY, así que la lógica
// "activar → escribir enu.toml → continuar Boot" se expone por una vía interna
// (`activateAndBoot`) testeable, y el render se inspecciona componiendo a un buffer
// (la rejilla del compositor / su salida ANSI).
//
// FRONTERA. La pantalla NO añade superficie Lua nueva (es del kernel, pre-Lua, §14
// ya describe G21): no toca `api.md` ni `enu.version.api`. La interacción de teclado
// visible, el streaming visible y el resize/paste visibles son el CP-7 MANUAL con
// TTY (no ejecutable en CI headless): aquí se cubre lo automatizable (el render a
// buffer, la condición TTY+sin-plugins, y activar→enu.toml→Boot).

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// BareScreenActive es la cara pública de `bareScreenActive` para `main` (el binario):
// indica si, con la config y el entorno actuales, toca pintar la pantalla de runtime
// desnudo (hay TTY y ningún plugin activo). `main` la consulta tras `New` para decidir
// entre pintar la pantalla (TTY interactivo, sin plugins) y el arranque normal (`Boot`).
func (rt *Runtime) BareScreenActive() bool { return rt.bareScreenActive() }

// RenderBareScreen es la cara pública de `renderBareScreen` para `main`: pinta la
// pantalla de runtime desnudo en el compositor. Devuelve las líneas FIJAS mostradas
// (versión, rutas, embebidas, acciones) para que `main` las pueda volcar al terminal
// mientras el driver de TTY (S33+) que lee el teclado para elegir una acción no esté
// cableado. La interacción real (elegir con el teclado, ver el efecto) es el CP-7
// MANUAL con TTY.
func (rt *Runtime) RenderBareScreen() []string {
	return rt.renderBareScreen().lines()
}

// ActivateOfficial activa el CONJUNTO oficial de producto (las embebidas menos el
// andamiaje `example`, ADR-015) y continúa el arranque (§14): es la primera acción de
// la pantalla desnuda. Sin red (las embebidas salen del binario, ADR-010). La invoca
// la elección de teclado "activar el conjunto oficial" (driver de TTY, S33+); el flag
// `enu --default-config` (sin TTY, G33) activa el MISMO conjunto vía `officialProductSet`,
// de modo que pantalla y flag enchufan lo mismo.
func (rt *Runtime) ActivateOfficial() error {
	names, err := officialProductSet()
	if err != nil {
		return err
	}
	return rt.activateAndBoot(names)
}

// OfficialProductSet expone el conjunto oficial de producto (ADR-015, G33) a `main`
// (el binario) como FUNCIÓN DE PAQUETE —no método—: el modo EFÍMERO de
// `enu --default-config` necesita el conjunto ANTES de construir el Runtime (para
// pasarlo a `WithEnabledPlugins`), cuando aún no hay `rt`. El conjunto es estático
// (sale del `embed.FS`, sin estado de runtime), así que no requiere un Runtime. Es un
// wrapper fino de `officialProductSet`.
func OfficialProductSet() ([]string, error) { return officialProductSet() }

// WriteDefaultConfig respalda el modo PERSISTENTE de `enu --default-config` (ADR-015,
// G33): escribe el conjunto oficial de producto en `plugins.enabled` de
// `config.dir()/enu.toml` —preservando el resto del fichero, atómico, idempotente; un
// `enu.toml` mal formado NO se sobrescribe (error accionable)— Y deja config de agente
// USABLE (ADR-017, G35): plantillas ACTIVAS de `agent.toml` (con un `model` por
// defecto) y `providers.toml` (provider `anthropic` con `api_key_env`), escritas SOLO
// si no existen (nunca pisan config del usuario). Sin esas plantillas, el primer `enu`
// arrancaría el chat sin modelo y moriría (G35). Devuelve `(configDir, names,
// createdTemplates, err)` para que `main` informe qué escribió y dónde —incluida la
// lista de plantillas creadas, para no afirmar que escribió algo que ya existía—. NO
// arranca nada (a diferencia de `ActivateOfficial`, la acción TTY que escribe Y
// continúa el `Boot`): el modo persistente escribe y sale. Sin red (ADR-010).
func (rt *Runtime) WriteDefaultConfig() (configDir string, names []string, createdTemplates []string, err error) {
	names, err = officialProductSet()
	if err != nil {
		return "", nil, nil, err
	}
	if err = writeEnabledPlugins(rt.ldr.configDir, names); err != nil {
		return "", nil, nil, err
	}
	// Plantillas activas de config de agente (ADR-017, G35): solo si no existen, para
	// que el onramp deje el harness usable de un comando (con la API key en el entorno).
	for _, tpl := range []struct{ name, content string }{
		{agentTomlName, defaultAgentToml},
		{providersTomlName, defaultProvidersToml},
	} {
		created, werr := writeTemplateIfAbsent(rt.ldr.configDir, tpl.name, tpl.content)
		if werr != nil {
			return "", nil, nil, werr
		}
		if created {
			createdTemplates = append(createdTemplates, tpl.name)
		}
	}
	return rt.ldr.configDir, names, createdTemplates, nil
}

// WriteInitConfig respalda el wizard de `enu init` (ADR-026 pieza 2, S49): escribe la
// config de agente con el MODELO elegido y, si `activateOfficial`, activa el conjunto
// oficial en `enu.toml`. MISMA semántica por-fichero que WriteDefaultConfig —`agent.toml`
// y `providers.toml` solo si no existen (nunca pisan config del usuario, ADR-017);
// `enu.toml` preserva el resto—. Devuelve qué activó, qué plantillas CREÓ y cuáles
// RESPETÓ (ya existían), para el mensaje honesto del wizard. Sin red (ADR-010). El
// wizard v1 solo ofrece `anthropic` (G61): `providers.toml` es la plantilla anthropic.
func (rt *Runtime) WriteInitConfig(model string, activateOfficial bool) (configDir string, activated, created, respected []string, err error) {
	configDir = rt.ldr.configDir
	if activateOfficial {
		names, oerr := officialProductSet()
		if oerr != nil {
			return "", nil, nil, nil, oerr
		}
		if werr := writeEnabledPlugins(configDir, names); werr != nil {
			return "", nil, nil, nil, werr
		}
		activated = names
	}
	for _, tpl := range []struct{ name, content string }{
		{agentTomlName, agentTomlFor(model)},
		{providersTomlName, defaultProvidersToml},
	} {
		wasCreated, werr := writeTemplateIfAbsent(configDir, tpl.name, tpl.content)
		if werr != nil {
			return "", nil, nil, nil, werr
		}
		if wasCreated {
			created = append(created, tpl.name)
		} else {
			respected = append(respected, tpl.name)
		}
	}
	return configDir, activated, created, respected, nil
}

// bareScreenActive decide si toca pintar la pantalla de runtime desnudo (§14, G21):
// hay superficie de UI (`rt.uiActive`) Y no hay plugins activos. Es el gate que
// `Boot` consulta antes de cargar Lua de producto. Sin UI (headless) o con algún
// plugin activo, devuelve false y el arranque sigue normal.
func (rt *Runtime) bareScreenActive() bool {
	return rt.uiActive && !rt.ldr.hasActivePlugins()
}

// hasActivePlugins informa, ANTES de correr ningún `init.lua`, si el arranque
// cargaría algún plugin: o bien `plugins.enabled` de `enu.toml` nombra algo (una
// embebida activada, ADR-010), o bien algún directorio de plugins contiene un
// plugin de disco. Es deliberadamente LIGERO —no materializa embebidas ni valida el
// grafo (eso lo hace `discover`/`topoSort` en el `Boot` real)—: solo decide si la
// pantalla desnuda procede. Una config rota (`configErr`) no se trata aquí; `Boot`
// la devolverá igual antes de pintar nada.
func (l *loader) hasActivePlugins() bool {
	if len(l.enabled) > 0 {
		return true
	}
	return l.anyDiskPlugin()
}

// anyDiskPlugin devuelve true en cuanto encuentra UN subdirectorio con `plugin.toml`
// en cualquiera de los directorios de plugins configurados (`WithPluginDir` +
// `plugins.dirs`). No parsea el manifiesto ni valida nada: para decidir si hay
// plugins de disco basta su presencia. Un directorio inexistente o ilegible no
// aporta (no es fatal aquí: el `Boot` real reporta los errores de IO).
func (l *loader) anyDiskPlugin() bool {
	for _, root := range l.pluginDirs {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			manifest := filepath.Join(root, e.Name(), pluginManifestName)
			if _, err := os.Stat(manifest); err == nil {
				return true
			}
		}
	}
	return false
}

// bareScreenModel reúne lo que la pantalla desnuda muestra (§14): es el estado FIJO
// derivado solo de las capacidades del kernel. Lo construye `buildBareScreenModel`
// y lo consume `renderBareScreen` (para pintar) y los tests (para verificar el
// contenido sin depender del layout exacto).
type bareScreenModel struct {
	versionLine string   // "enu 0.1.0 · API 2"
	configDir   string   // config.dir()
	pluginDirs  []string // directorios donde se buscan plugins
	embedded    []string // catálogo de extensiones embebidas DISPONIBLES
	actions     []string // las acciones ofrecidas (activar conjunto / sueltas / salir)
}

// buildBareScreenModel arma el modelo fijo de la pantalla desnuda a partir de las
// capacidades del runtime: versión + nivel de API (§2), rutas (§14), catálogo de
// embebidas (embed.go) y acciones (§14). No toca disco salvo enumerar el catálogo
// embebido (que sale del binario, sin red). Un fallo al enumerar el catálogo deja la
// lista vacía: la pantalla se pinta igual (el catálogo es informativo).
func (rt *Runtime) buildBareScreenModel() bareScreenModel {
	embedded, _ := embeddedNames() // del binario (ADR-010); si falla, lista vacía

	m := bareScreenModel{
		versionLine: fmt.Sprintf("enu %d.%d.%d · API %d",
			VersionMajor, VersionMinor, VersionPatch, APILevel),
		configDir:  rt.ldr.configDir,
		pluginDirs: append([]string(nil), rt.ldr.pluginDirs...),
		embedded:   embedded,
		actions: []string{
			"activar el conjunto oficial",
			"activar extensiones sueltas (p. ej. solo repl)",
			"salir",
		},
	}
	return m
}

// bareScreenLines produce las líneas de texto FIJAS de la pantalla desnuda, en
// orden. Es texto plano (sin estilos): el render es del runtime, no de un producto.
// Lo usa `renderBareScreen` (para blittear un Block) y los tests (para comprobar que
// las cadenas esperadas —versión, rutas, embebidas, acciones— están presentes).
func (m bareScreenModel) lines() []string {
	var ls []string
	ls = append(ls, "enu — runtime desnudo")
	ls = append(ls, "")
	ls = append(ls, m.versionLine)
	ls = append(ls, "")
	ls = append(ls, "config: "+m.configDir)
	if len(m.pluginDirs) == 0 {
		ls = append(ls, "plugins: (ninguno configurado)")
	} else {
		for i, d := range m.pluginDirs {
			if i == 0 {
				ls = append(ls, "plugins: "+d)
			} else {
				ls = append(ls, "         "+d)
			}
		}
	}
	ls = append(ls, "")
	if len(m.embedded) == 0 {
		ls = append(ls, "extensiones embebidas: (ninguna)")
	} else {
		ls = append(ls, "extensiones embebidas disponibles:")
		for _, name := range m.embedded {
			ls = append(ls, "  - "+name)
		}
	}
	ls = append(ls, "")
	ls = append(ls, "acciones:")
	for i, a := range m.actions {
		ls = append(ls, fmt.Sprintf("  %d) %s", i+1, a))
	}
	return ls
}

// renderBareScreen compone la pantalla desnuda en el compositor (§9.1, S29) y la
// pinta: construye un Block fijo con las líneas del modelo, lo blittea en una región
// a pantalla completa y fuerza un `paint`. Devuelve el modelo pintado (para que el
// llamante/tests sepan qué se mostró). Corre bajo el token, en el estado principal
// (lo invoca `Boot`, que lo tiene): toca el compositor como cualquier mutación de
// `enu.ui`. Requiere `rt.ui != nil` (garantizado por el gate `bareScreenActive`, que
// exige `uiActive`).
//
// El render es FIJO y pre-Lua: no hay widgets ni lógica de producto, solo celdas. El
// resultado vive en la rejilla del compositor (`back`) y en su salida ANSI
// (`encoded`), ambas inspeccionables por los tests sin un TTY real.
func (rt *Runtime) renderBareScreen() bareScreenModel {
	m := rt.buildBareScreenModel()
	if rt.ui == nil {
		return m // defensivo: el gate ya exige uiActive, pero no asumimos compositor
	}

	lines := m.lines()
	spanLines := make([][]span, len(lines))
	for i, ln := range lines {
		spanLines[i] = []span{{text: ln}}
	}
	b := newBlock(spanLines)

	// Una región a pantalla completa, sin dueño de plugin (es del runtime): el
	// owner "user" la etiqueta como cualquier handle del estado principal. Se
	// blittea el Block en su origen (0,0) y se compone+pinta de inmediato (no se
	// espera al timer de coalescing: la pantalla desnuda debe verse ya). Bajo el
	// candado de la UI (G44): el compositor se comparte con la VM.
	rt.withUILock(func() {
		comp := rt.ui.comp
		r := comp.addRegion(0, 0, comp.w, comp.h, 0, ownerUser)
		r.content.blitBlock(0, 0, b)
		comp.markDirty()
		comp.paint()
	})
	return m
}

// --- S54: elección por teclado en la pantalla de runtime desnudo (§14, G21) ---
//
// La máquina de estados de la elección vive en Go (§14: "es lógica de input del
// driver: vive en el binario"; "el estado del cursor vive en bare_screen.go/driver,
// jamás como widget de enu.ui"). El driver de TTY la conduce desde `feed`
// (pollBareAction) con el patrón flag+poll del quit: el `on_input` Lua es un
// reenviador tonto que anota la tecla; la lógica —incluida la activación, que
// re-entra la VM (activateAndBoot→Boot→Eval) y deadlockearía bajo `inst.mu` si
// corriera dentro de un HostFn síncrono— corre aquí, en Go, con `inst.mu` libre.

// bareAction es el resultado de handleKey: qué debe hacer el driver tras procesar
// una tecla. bareNone = nada (ya se re-renderizó o la tecla se ignoró); bareQuit =
// el usuario pidió salir (el driver apaga el bucle, reusando `requestQuit`).
type bareAction int

const (
	bareNone bareAction = iota
	bareQuit
)

// bareMode distingue las dos pantallas de la máquina (§14): el MENÚ raíz (las tres
// acciones) y el modo SELECCIÓN (navegar el catálogo de embebidas para activar una
// suelta). El menú↔selección es toda la máquina: cualquier enriquecimiento (filtro,
// búsqueda, descripciones, paneles) es hallazgo/ADR, no código (§14, cláusula sagrada).
type bareMode int

const (
	bareMenu bareMode = iota
	bareSelect
)

// bareScreen es el ESTADO de la elección por teclado en la pantalla de runtime
// desnudo (S54). Dos modos (MENÚ ↔ SELECCIÓN), un cursor ACOTADO al catálogo, un
// latch de RE-ENTRADA (una sola activación por vida de la pantalla: doble
// `enu.toml`/doble `Boot` es el fallo silencioso, 🔒) y el error de una activación
// fallida pintado en pantalla (ADR-017: la salida por teclado sigue viva tras el
// fallo). El render reusa UNA región (no acumula), coherente con "render pre-Lua,
// sin widgets" de §14.
type bareScreen struct {
	model     bareScreenModel // el contenido fijo (versión, rutas, catálogo, acciones)
	mode      bareMode
	cursor    int       // índice en `catalog`, acotado a [0, len(catalog)-1]
	catalog   []string  // el catálogo de embebidas navegable (activar suelta)
	activated bool      // latch: una sola activación por vida de la pantalla (re-entrada)
	done      bool      // activación con éxito: el producto se montó; deja de sondearse
	errMsg    string    // error accionable de la última activación fallida (en pantalla)
	region    *uiRegion // la ÚNICA región de la pantalla (se reusa en cada render)

	// activate es la costura de test de la activación: en producción
	// `rt.activateAndBoot` (escribe `enu.toml` y continúa el `Boot`); un test la
	// sustituye por un spy contador para blindar la re-entrada y "activa exactamente
	// X" sin montar el `Boot` real.
	activate func(names []string) error
}

// newBareScreen construye el estado de la pantalla desnuda: el modelo fijo, el
// catálogo navegable (todas las embebidas disponibles; ADR-015 deja `example`/`mesh`
// activables SUELTOS) y la activación real (`rt.activateAndBoot`). El render inicial
// y el reenviador de teclado los cablea `PrepareBareScreen` (driver.go).
func newBareScreen(rt *Runtime) *bareScreen {
	m := rt.buildBareScreenModel()
	return &bareScreen{
		model:    m,
		mode:     bareMenu,
		catalog:  m.embedded,
		activate: rt.activateAndBoot,
	}
}

// handleKey conduce la máquina con una tecla YA NORMALIZADA (con prefijo "ctrl+"
// para combos; el reenviador `on_input` la anota). Devuelve `bareQuit` si el usuario
// pidió salir. Re-renderiza cuando el estado cambia (cursor, modo, error). Presupone
// el token del scheduler tomado (lo llama `pollBareAction` desde `feed`): la
// activación corre aquí, con `inst.mu` libre, no bajo un HostFn (evita el deadlock).
func (bs *bareScreen) handleKey(key string, rt *Runtime) bareAction {
	if bs.done {
		return bareNone // ya se activó con éxito: el producto gobierna la pantalla
	}
	switch bs.mode {
	case bareMenu:
		switch key {
		case "1": // activar el conjunto oficial de producto
			bs.activateOfficial(rt)
		case "2": // entrar en modo selección del catálogo
			bs.mode = bareSelect
			bs.cursor = 0
			bs.render(rt)
		case "3", "q", "ctrl+c", "esc": // salir (la acción 3 y los atajos de salida)
			return bareQuit
		}
	case bareSelect:
		switch key {
		case "up", "k":
			bs.moveCursor(-1, rt)
		case "down", "j":
			bs.moveCursor(1, rt)
		case "enter": // activar SOLO la embebida bajo el cursor
			bs.activateSelected(rt)
		case "esc": // volver al menú (esc es contextual: aquí NO sale)
			bs.mode = bareMenu
			bs.render(rt)
		case "q", "ctrl+c": // los atajos de salida duros salen desde cualquier modo
			return bareQuit
		}
	}
	return bareNone // tecla no mapeada: se ignora
}

// moveCursor mueve el cursor del catálogo por `delta`, ACOTÁNDOLO a
// [0, len(catalog)-1]. Con catálogo VACÍO no hay dónde moverse (cursor queda en 0,
// sin índice fuera de rango). Re-renderiza para reflejar el cursor.
func (bs *bareScreen) moveCursor(delta int, rt *Runtime) {
	if len(bs.catalog) == 0 {
		return
	}
	bs.cursor += delta
	if bs.cursor < 0 {
		bs.cursor = 0
	} else if bs.cursor >= len(bs.catalog) {
		bs.cursor = len(bs.catalog) - 1
	}
	bs.render(rt)
}

// activateOfficial activa el CONJUNTO OFICIAL de producto (acción 1, ADR-015):
// exactamente `officialProductSet` (`example`/`mesh` fuera). Pasa por `tryActivate`
// (guarda de re-entrada + error en pantalla).
func (bs *bareScreen) activateOfficial(rt *Runtime) {
	names, err := officialProductSet()
	if err != nil {
		bs.fail(err, rt)
		return
	}
	bs.tryActivate(names, rt)
}

// activateSelected activa SOLO la embebida bajo el cursor (acción 2 → enter). Con
// catálogo VACÍO no hay nada que activar (no-op: no escribe `enu.toml` ni indexa
// fuera de rango).
func (bs *bareScreen) activateSelected(rt *Runtime) {
	if len(bs.catalog) == 0 {
		return
	}
	bs.tryActivate([]string{bs.catalog[bs.cursor]}, rt)
}

// tryActivate ejecuta la activación con la GUARDA DE RE-ENTRADA (🔒): una vez
// iniciada, una segunda pulsación no dispara otra (doble `enu.toml`/doble `Boot`).
// Éxito → el producto se montó (`done`, deja de sondearse). Error → se pinta
// accionable y la pantalla sigue viva (la salida por teclado no se pierde, ADR-017).
func (bs *bareScreen) tryActivate(names []string, rt *Runtime) {
	if bs.activated {
		return // re-entrada: ya hay una activación en curso/hecha
	}
	bs.activated = true
	if err := bs.activate(names); err != nil {
		bs.fail(err, rt)
		return
	}
	bs.done = true
}

// fail registra el error accionable de una activación fallida y lo pinta en la
// pantalla (§14: los errores van a la pantalla, no al vacío). No apaga nada: la
// salida por teclado sigue operativa (ADR-017).
func (bs *bareScreen) fail(err error, rt *Runtime) {
	bs.errMsg = "no se pudo activar: " + err.Error()
	bs.render(rt)
}

// render (re)pinta la pantalla desnuda en su ÚNICA región, según el estado actual
// (menú o selección, con el cursor y un eventual error). RECREA la región en cada
// render soltando la anterior: así no acumula regiones (el bug de llamar `addRegion`
// sin `removeRegion`) y adapta al tamaño actual de la pantalla sin lógica de resize;
// el coste —una rejilla nueva por pulsación, a velocidad humana— es despreciable.
// Bajo el candado de la UI (§9.1, G44): el compositor se comparte con la VM.
// Token-agnóstico (solo usa `withUILock`): lo llaman `PrepareBareScreen` (sin token)
// y `pollBareAction` (con token).
func (bs *bareScreen) render(rt *Runtime) {
	if rt.ui == nil {
		return // defensivo: el gate bareScreenActive ya exige uiActive
	}
	lines := bs.lines()
	spanLines := make([][]span, len(lines))
	for i, ln := range lines {
		spanLines[i] = []span{{text: ln}}
	}
	b := newBlock(spanLines)
	rt.withUILock(func() {
		comp := rt.ui.comp
		if bs.region != nil {
			comp.removeRegion(bs.region)
		}
		bs.region = comp.addRegion(0, 0, comp.w, comp.h, 0, ownerUser)
		bs.region.content.blitBlock(0, 0, b)
		comp.markDirty()
		comp.paint()
	})
}

// teardown suelta la ÚNICA región de la pantalla desnuda del compositor: tras activar
// con éxito, el producto gobierna la pantalla y el menú desnudo (z=0, al fondo) no debe
// quedar debajo —sería una fuga de región y un bleed-through del menú bajo una UI de
// producto que no cubra toda la pantalla—. Bajo el candado de la UI (§9.1, G44). Lo
// llama el driver (`pollBareAction`) cuando la activación marcó `done`.
func (bs *bareScreen) teardown(rt *Runtime) {
	if rt.ui == nil || bs.region == nil {
		return
	}
	rt.withUILock(func() {
		rt.ui.comp.removeRegion(bs.region)
		rt.ui.comp.markDirty()
	})
	bs.region = nil
}

// lines produce las líneas de texto de la pantalla desnuda SEGÚN EL ESTADO: el menú
// raíz (idéntico al de S33, `model.lines`) o el modo selección (el catálogo con el
// cursor). Un error de activación se añade al final, accionable (§14). Texto plano
// pre-Lua, sin widgets.
func (bs *bareScreen) lines() []string {
	var ls []string
	if bs.mode == bareSelect {
		ls = bs.selectLines()
	} else {
		ls = bs.model.lines()
	}
	if bs.errMsg != "" {
		ls = append(ls, "", bs.errMsg)
	}
	return ls
}

// selectLines pinta el modo SELECCIÓN: cabecera fija (versión) y el catálogo de
// embebidas navegable, marcando con "> " la que el cursor señala. El cursor es
// estado del DRIVER (Go), no un widget: aquí solo se traduce a un marcador de texto.
func (bs *bareScreen) selectLines() []string {
	var ls []string
	ls = append(ls, "enu — runtime desnudo · activar extensión suelta")
	ls = append(ls, "")
	ls = append(ls, bs.model.versionLine)
	ls = append(ls, "")
	if len(bs.catalog) == 0 {
		ls = append(ls, "(no hay extensiones embebidas que activar)")
		ls = append(ls, "")
		ls = append(ls, "esc vuelve al menú · q sale")
		return ls
	}
	ls = append(ls, "elige una extensión (↑/↓ o j/k · enter activa · esc vuelve):")
	for i, name := range bs.catalog {
		marker := "  "
		if i == bs.cursor {
			marker = "> "
		}
		ls = append(ls, marker+name)
	}
	return ls
}

// activateAndBoot es la lógica de la ACCIÓN de la pantalla desnuda (§14): escribe
// `names` en `plugins.enabled` de `config.dir()/enu.toml` (preservando el resto del
// fichero si existía) y CONTINÚA el arranque canónico (`Boot`), SIN red. Es la vía
// INTERNA y testeable de "activar → escribir enu.toml → continuar Boot": en
// producción la dispara la elección de teclado (driver de TTY, S33+); en headless la
// invocan los tests.
//
//   - "activar el conjunto oficial" = `activateAndBoot(officialProductSet())` (las
//     embebidas del catálogo menos el andamiaje `example`, ADR-015).
//   - "activar suelta" = `activateAndBoot([]string{"repl"})` (solo esa).
//
// Tras escribir el fichero, recarga la config (`plugins.enabled`/`dirs`/watchdog) en
// el loader y arranca: así el `Boot` posterior carga las recién activadas con
// `source="builtin"`, exactamente como si el usuario hubiera editado `enu.toml` a
// mano y vuelto a arrancar (ADR-010). Devuelve el error de `Boot` (grafo inválido,
// config rota...), accionable.
func (rt *Runtime) activateAndBoot(names []string) error {
	if err := writeEnabledPlugins(rt.ldr.configDir, names); err != nil {
		return err
	}
	// Releer la config tras escribirla: el loader debe ver la nueva `plugins.enabled`
	// (y cualquier `dirs`/watchdog que ya hubiera). Reseteamos `booted` para que el
	// `Boot` que sigue cargue de verdad (la pantalla desnuda no llegó a cargar nada).
	nuCfg, _, tomlErr := loadNuToml(rt.ldr.configDir)
	rt.ldr.enabled = nuCfg.Plugins.Enabled
	rt.ldr.configErr = tomlErr
	rt.ldr.booted = false
	// `rt.Boot()` (no `ldr.Boot()`) para que, además de cargar los plugins recién
	// activados, se arme el timer de coalescing del compositor (`armPainter`,
	// idempotente): tras activar, la UI de las extensiones debe repintarse sola.
	return rt.Boot()
}

// writeEnabledPlugins escribe (o actualiza) `plugins.enabled` en
// `config.dir()/enu.toml`, PRESERVANDO el resto del fichero si existe (otras claves
// de `[plugins]`, `[watchdog]`, `[net]`, claves desconocidas...). La estrategia: leer
// el TOML existente a un mapa genérico, fijar `plugins.enabled`, y reescribir todo
// con la librería TOML pura-Go (BurntSushi, la misma del loader, S11) de forma
// ATÓMICA (escribir a un temporal y renombrar) para no dejar un `enu.toml` a medias si
// el proceso muere a mitad. Un fichero ausente se crea con solo esa clave.
//
// POR QUÉ un mapa genérico y no `runtimeConfig`: re-serializar `runtimeConfig`
// perdería las claves que el core ignora por forward-compat (config_toml.go), y la
// pantalla desnuda no debe pisar configuración del usuario que no entiende. Un mapa
// `map[string]any` round-trippea todo lo que BurntSushi parseó.
func writeEnabledPlugins(configDir string, names []string) error {
	path := filepath.Join(configDir, nuTomlName)

	// Lee el TOML existente a un mapa genérico (preserva claves desconocidas). Un
	// fichero ausente arranca de un mapa vacío; uno mal formado es un error
	// accionable (no lo sobrescribimos a ciegas: perderíamos config del usuario).
	root := map[string]any{}
	if data, err := os.ReadFile(path); err == nil {
		if _, decErr := toml.Decode(string(data), &root); decErr != nil {
			return &StructuredError{Code: CodeEINVAL,
				Message: fmt.Sprintf("%s inválido en %q: %v (no se sobrescribe para no perder tu configuración; corrígelo a mano)",
					nuTomlName, path, decErr)}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return &StructuredError{Code: CodeEIO,
			Message: fmt.Sprintf("no se pudo leer %q: %v", path, err)}
	}

	// Fija `plugins.enabled`, conservando el resto de `[plugins]` si existía.
	plugins, _ := root["plugins"].(map[string]any)
	if plugins == nil {
		plugins = map[string]any{}
	}
	enabled := make([]any, len(names))
	for i, n := range names {
		enabled[i] = n
	}
	plugins["enabled"] = enabled
	root["plugins"] = plugins

	// Serializa y escribe atómicamente (temporal + rename) bajo `config.dir()`. El
	// directorio de config debe existir; si no, se crea (primer arranque del usuario).
	// La escritura reusa `writeAtomic` (S14): temporal en el mismo directorio +
	// `rename`, para no dejar un `enu.toml` a medias si el proceso muere a mitad.
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return &StructuredError{Code: CodeEIO,
			Message: fmt.Sprintf("no se pudo crear el directorio de configuración %q: %v", configDir, err)}
	}
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(root); err != nil {
		return &StructuredError{Code: CodeEIO,
			Message: fmt.Sprintf("no se pudo serializar %s: %v", nuTomlName, err)}
	}
	if err := writeAtomic(path, buf.Bytes(), nil); err != nil {
		return &StructuredError{Code: CodeEIO,
			Message: fmt.Sprintf("no se pudo escribir %q: %v", path, err)}
	}
	return nil
}

// Nombres de los ficheros de config de las extensiones oficiales que el onramp
// siembra (ADR-017, G35). El core no los entiende (son de `agent`/`providers`),
// pero el binario los escribe como plantilla del primer arranque; cada extensión los
// lee desde `config.dir()` (agente.md §10, providers.md).
const (
	agentTomlName     = "agent.toml"
	providersTomlName = "providers.toml"
)

// defaultAgentToml es la plantilla ACTIVA de `agent.toml` (ADR-017, G35): trae un
// `model` por defecto para que `agent.session` no falle con `EINVAL` en el primer
// arranque (agente.md §10). Opinada a Anthropic (la identidad del producto y el modelo
// por defecto del proyecto, ADR-016). El usuario la edita; el onramp no la pisa si ya
// existe.
// agentTomlFor rinde la plantilla de `agent.toml` con el MODELO dado. El onramp
// (`--default-config`) usa el default; el wizard de `enu init` (ADR-026 pieza 2, S49)
// la reutiliza con el modelo que el usuario acepte o teclee. `agentTomlFor("anthropic/opus")`
// es, byte a byte, la plantilla por defecto (de ahí `defaultAgentToml`).
func agentTomlFor(model string) string {
	return fmt.Sprintf(`# agent.toml — configuración del agente (agente.md §10).
# Generado por 'enu --default-config' (ADR-017). Edítalo a tu gusto.

# Modelo por defecto: "proveedor/modelo", resoluble en providers.toml.
model = %q

# Tope de turnos por sesión (protección contra loops).
max_turns = 32
`, model)
}

var defaultAgentToml = agentTomlFor("anthropic/opus")

// defaultProvidersToml es la plantilla ACTIVA de `providers.toml` (ADR-017, G35):
// declara el provider `anthropic` y el modelo por defecto. La API key NUNCA va al
// fichero (providers.md §1): se lee de la variable de entorno `api_key_env`. Si esa
// variable no está, `providers.resolve` no falla (deja la clave ausente): el chat
// monta igual y el error sale al primer turno. El onramp no la pisa si ya existe.
const defaultProvidersToml = `# providers.toml — proveedores y modelos (providers.md).
# Generado por 'enu --default-config' (ADR-017).
# La API key NUNCA va aquí: se lee de la variable de entorno de api_key_env.

[providers.anthropic]
adapter     = "anthropic"
base_url    = "https://api.anthropic.com"
api_key_env = "ANTHROPIC_API_KEY"

[[providers.anthropic.models]]
id       = "claude-opus-4-8"
context  = 200000
aliases  = ["opus"]
thinking = "adaptive"
`

// writeTemplateIfAbsent escribe `content` en `configDir/name` SOLO si el fichero no
// existe: nunca pisa configuración del usuario (ADR-017, G35). Devuelve `created=true`
// si lo creó, `false` si ya existía. Atómico (temporal + rename, `writeAtomic` de
// S14); crea `config.dir()` si falta. Un error de stat distinto de "no existe" o de
// escritura es `EIO` accionable. Es la pieza del onramp que siembra `agent.toml` y
// `providers.toml` sin riesgo de sobrescribir un fichero que el usuario ya editó.
func writeTemplateIfAbsent(configDir, name, content string) (created bool, err error) {
	path := filepath.Join(configDir, name)
	if _, statErr := os.Stat(path); statErr == nil {
		return false, nil // ya existe: no lo tocamos
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return false, &StructuredError{Code: CodeEIO,
			Message: fmt.Sprintf("no se pudo comprobar %q: %v", path, statErr)}
	}
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return false, &StructuredError{Code: CodeEIO,
			Message: fmt.Sprintf("no se pudo crear el directorio de configuración %q: %v", configDir, err)}
	}
	if err := writeAtomic(path, []byte(content), nil); err != nil {
		return false, &StructuredError{Code: CodeEIO,
			Message: fmt.Sprintf("no se pudo escribir %q: %v", path, err)}
	}
	return true, nil
}
