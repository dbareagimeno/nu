package spike

// La COMPUERTA del spike (ver shim/gate.c). Hallazgo previo: wazero RECUPERA los
// pánicos de un host function en su propia frontera y los entrega como error del
// Call interno — así que el trampolín no necesita pánicos que crucen frames Go:
// host_throw marca un flag y detiene la ejecución; host_try detecta el error del
// Call + flag ⇒ throw de Lua (cualquier otro error es un trap real y se propaga).

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

var errThrow = errors.New("spike-lua-throw")

type gateEnv struct {
	mod     api.Module
	pending bool
}

func buildGate(t *testing.T) *gateEnv {
	t.Helper()
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	t.Cleanup(func() { rt.Close(ctx) })

	env := &gateEnv{}

	_, err := rt.NewHostModuleBuilder("spike").
		NewFunctionBuilder().
		WithFunc(func(ctx context.Context, depth int32) int32 {
			sp := env.mod.ExportedGlobal("__stack_pointer")
			saved := sp.Get()
			_, callErr := env.mod.ExportedFunction("t_body").Call(ctx, uint64(depth))
			if callErr != nil {
				if env.pending {
					env.pending = false
					sp.(api.MutableGlobal).Set(saved) // restaura el shadow-stack
					return 1                          // "hubo throw", como el status de setjmp
				}
				panic(callErr) // trap REAL: propaga
			}
			return 0
		}).
		Export("host_try").
		NewFunctionBuilder().
		WithFunc(func(ctx context.Context) {
			env.pending = true
			panic(errThrow) // wazero lo recupera y lo entrega como error del Call
		}).
		Export("host_throw").
		Instantiate(ctx)
	if err != nil {
		t.Fatal(err)
	}

	wasm, err := os.ReadFile("../gate.wasm")
	if err != nil {
		t.Fatal(err)
	}
	env.mod, err = rt.InstantiateWithConfig(ctx, wasm, wazero.NewModuleConfig().WithName("gate"))
	if err != nil {
		t.Fatal(err)
	}
	return env
}

func TestGateTrampolin(t *testing.T) {
	env := buildGate(t)
	ctx := context.Background()
	outer := env.mod.ExportedFunction("t_outer")

	// 1. Camino normal: status 0 (+1 del acc).
	if r, err := outer.Call(ctx, 0); err != nil || r[0] != 1 {
		t.Fatalf("normal: r=%v err=%v", r, err)
	}
	// 2. Throw simple recuperado: status 1 → 101; módulo VIVO.
	if r, err := outer.Call(ctx, 1); err != nil || r[0] != 101 {
		t.Fatalf("throw: r=%v err=%v", r, err)
	}
	// 3. Try ANIDADO: el throw interno se recupera dentro; el outer ve 0.
	if r, err := outer.Call(ctx, 2); err != nil || r[0] != 1 {
		t.Fatalf("anidado: r=%v err=%v", r, err)
	}
	// 4. Reusabilidad: 500 throws seguidos sin degradación.
	for i := 0; i < 500; i++ {
		if r, err := outer.Call(ctx, 1); err != nil || r[0] != 101 {
			t.Fatalf("iteración %d: r=%v err=%v", i, r, err)
		}
	}
	t.Log("compuerta ABIERTA: throw recuperable, try anidado, módulo reusable, shadow-stack sano")
}
