package runtime

// Tests de `DiagnosePluginGraph` (S50, soporte de `enu doctor`): el diagnóstico del
// grafo de plugins SIN `Boot()`. Viven en el paquete `runtime` —no en `package main`—
// porque el método es superficie interna del runtime; los checks `plugins.enabled` /
// `plugins.requires` de `enu doctor` (package main) lo consumen, pero su LÓGICA (reusar
// `discover()`+`topoSort()` sin arrancar Lua) se blinda aquí, en su propio paquete.
// Cierran el hueco de cobertura que la mutación de CP-12 destapó: sin estos tests, las
// ramas de error de `DiagnosePluginGraph` solo las ejercitaba el paquete `main`.

import (
	"strings"
	"testing"
)

func TestDiagnosePluginGraph(t *testing.T) {
	t.Run("dado_grafo_sano_entonces_enabled_y_requires_ok", func(t *testing.T) {
		root := t.TempDir()
		writePlugin(t, root, "a", "1.0", nil, "")
		writePlugin(t, root, "b", "1.0", []string{"a"}, "")
		rt := New(WithDataDir(t.TempDir()), WithPluginDir(root), WithConfigDir(t.TempDir()))
		t.Cleanup(rt.Close)

		d := rt.DiagnosePluginGraph()
		if !d.EnabledOK {
			t.Fatalf("grafo sano: EnabledOK debe ser true (detail=%q)", d.EnabledDetail)
		}
		if !d.RequiresRun {
			t.Fatalf("grafo sano: RequiresRun debe ser true (discover ok → se evalúa requires)")
		}
		if !d.RequiresOK {
			t.Fatalf("grafo sano: RequiresOK debe ser true (detail=%q)", d.RequiresDetail)
		}
	})

	t.Run("dado_ciclo_en_requires_entonces_requires_falla_pero_enabled_ok", func(t *testing.T) {
		root := t.TempDir()
		writePlugin(t, root, "A", "1.0", []string{"B"}, "")
		writePlugin(t, root, "B", "1.0", []string{"A"}, "")
		rt := New(WithDataDir(t.TempDir()), WithPluginDir(root), WithConfigDir(t.TempDir()))
		t.Cleanup(rt.Close)

		d := rt.DiagnosePluginGraph()
		if !d.EnabledOK {
			t.Fatalf("un ciclo NO impide el descubrimiento: EnabledOK debe seguir true")
		}
		if !d.RequiresRun {
			t.Fatalf("discover ok → RequiresRun debe ser true aunque requires falle")
		}
		if d.RequiresOK {
			t.Fatalf("un ciclo A↔B debe dejar RequiresOK en false")
		}
		if !strings.Contains(d.RequiresDetail, "A") || !strings.Contains(d.RequiresDetail, "B") {
			t.Fatalf("el detalle debe nombrar los plugins del ciclo: %q", d.RequiresDetail)
		}
	})

	t.Run("dado_dependencia_ausente_entonces_requires_falla", func(t *testing.T) {
		root := t.TempDir()
		writePlugin(t, root, "solo", "1.0", []string{"fantasma"}, "")
		rt := New(WithDataDir(t.TempDir()), WithPluginDir(root), WithConfigDir(t.TempDir()))
		t.Cleanup(rt.Close)

		d := rt.DiagnosePluginGraph()
		if !d.EnabledOK {
			t.Fatalf("una dependencia ausente NO impide el descubrimiento: EnabledOK true")
		}
		if d.RequiresOK {
			t.Fatalf("`solo` requiere `fantasma` (ausente): RequiresOK debe ser false")
		}
	})

	t.Run("dado_colision_de_nombres_entonces_enabled_falla_y_requires_no_corre", func(t *testing.T) {
		// Dos plugins con el MISMO nombre en dos dirs → `discover()` falla; sin
		// descubrimiento no se puede juzgar `requires` (RequiresRun=false).
		root1, root2 := t.TempDir(), t.TempDir()
		writePlugin(t, root1, "dup", "1.0", nil, "")
		writePlugin(t, root2, "dup", "2.0", nil, "")
		rt := New(WithDataDir(t.TempDir()), WithPluginDir(root1), WithPluginDir(root2), WithConfigDir(t.TempDir()))
		t.Cleanup(rt.Close)

		d := rt.DiagnosePluginGraph()
		if d.EnabledOK {
			t.Fatalf("una colisión de nombres debe dejar EnabledOK en false")
		}
		if d.EnabledDetail == "" {
			t.Fatalf("un fallo de discover debe traer un detalle accionable")
		}
		if d.RequiresRun {
			t.Fatalf("si discover falló, requires no se evalúa: RequiresRun debe ser false")
		}
	})
}
