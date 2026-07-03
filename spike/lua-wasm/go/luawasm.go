package spike

// Cargador de lua.wasm sobre wazero: el lado Go del trampolín (spike_unwind.h)
// y el protocolo de buffer compartido (spike_buf).
//
// v2 del trampolín: usa la API EXPERIMENTAL de snapshots de wazero
// (experimental.Snapshotter) — un setjmp/longjmp de verdad para el motor:
// LUAI_TRY toma un Snapshot; LUAI_THROW hace Restore(1), que desenrolla el
// stack wasm hasta el punto del snapshot y hace que ESE host_try retorne 1
// (el "retorno por segunda vez" de setjmp). La v1 usaba pánicos Go: correcta,
// pero cada throw pagaba la construcción de la stack trace de wazero (~30ms);
// con snapshots el throw es barato. El __stack_pointer (shadow stack de C) es
// un global del módulo que el snapshot no cubre: lo salvamos/restauramos
// nosotros en paralelo (mismo par LIFO).

import (
	"context"
	"fmt"
	"os"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

type tryFrame struct {
	snap experimental.Snapshot
	sp   uint64
}

type LuaWasm struct {
	rt  wazero.Runtime
	mod api.Module
	ctx context.Context // con el snapshotter habilitado

	tries []tryFrame // pila LIFO de trys activos (espejo de L->errorJmp)

	evalFn    api.Function
	coSpawn   api.Function
	coResume  api.Function
	resultLen api.Function
	bufPtr    uint32

	// contadores de los benchs (host_note / host_render)
	Notes   int
	Renders int
}

// NewLuaWasmCompiled permite compartir un módulo COMPILADO entre instancias
// (el coste de compilar lua.wasm se paga una vez por proceso).
var sharedCompiled wazero.CompiledModule
var sharedRuntime wazero.Runtime

func NewLuaWasm(path string) (*LuaWasm, error) {
	baseCtx := experimental.WithSnapshotter(context.Background())
	lw := &LuaWasm{ctx: baseCtx}

	if sharedRuntime == nil {
		sharedRuntime = wazero.NewRuntime(baseCtx)
		wasi_snapshot_preview1.MustInstantiate(baseCtx, sharedRuntime)
		if _, err := sharedRuntime.NewHostModuleBuilder("spike").
			NewFunctionBuilder().
			WithFunc(hostTry).Export("host_try").
			NewFunctionBuilder().
			WithFunc(hostThrow).Export("host_throw").
			NewFunctionBuilder().
			WithFunc(hostNote).Export("host_note").
			NewFunctionBuilder().
			WithFunc(hostRender).Export("host_render").
			Instantiate(baseCtx); err != nil {
			return nil, err
		}
		wasm, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if sharedCompiled, err = sharedRuntime.CompileModule(baseCtx, wasm); err != nil {
			return nil, err
		}
	}
	lw.rt = sharedRuntime

	var err error
	lw.mod, err = lw.rt.InstantiateModule(
		// el módulo va al contexto para que los host functions encuentren SU instancia
		baseCtx,
		sharedCompiled,
		wazero.NewModuleConfig().WithName(""),
	)
	if err != nil {
		return nil, err
	}
	lw.ctx = context.WithValue(baseCtx, ctxKey{}, lw)

	lw.evalFn = lw.mod.ExportedFunction("spike_eval")
	lw.coSpawn = lw.mod.ExportedFunction("spike_co_spawn")
	lw.coResume = lw.mod.ExportedFunction("spike_co_resume")
	lw.resultLen = lw.mod.ExportedFunction("spike_result_len")

	r, err := lw.mod.ExportedFunction("spike_buf").Call(lw.ctx)
	if err != nil {
		return nil, err
	}
	lw.bufPtr = uint32(r[0])

	if r, err = lw.mod.ExportedFunction("spike_init").Call(lw.ctx); err != nil || r[0] != 0 {
		return nil, fmt.Errorf("spike_init: r=%v err=%w", r, err)
	}
	return lw, nil
}

type ctxKey struct{}

func fromCtx(ctx context.Context) *LuaWasm { return ctx.Value(ctxKey{}).(*LuaWasm) }

// hostTry: LUAI_TRY — snapshot + correr el cuerpo re-entrando en wasm.
// Devuelve 0 (éxito) o, vía Restore desde hostThrow, 1 (hubo throw).
func hostTry(ctx context.Context, L, f, ud int32) int32 {
	lw := fromCtx(ctx)
	sp := lw.mod.ExportedGlobal("__stack_pointer")
	lw.tries = append(lw.tries, tryFrame{
		snap: experimental.GetSnapshotter(ctx).Snapshot(),
		sp:   sp.Get(),
	})
	depth := len(lw.tries)
	// función FRESCA por llamada: api.Function no es reentrante (estado interno);
	// reusar un objeto cacheado corrompe el frame del nivel exterior.
	_, callErr := lw.mod.ExportedFunction("spike_call_pfunc").
		Call(ctx, uint64(uint32(L)), uint64(uint32(f)), uint64(uint32(ud)))
	if callErr != nil {
		panic(callErr) // trap REAL del wasm (los throws ya no pasan por aquí)
	}
	if len(lw.tries) != depth {
		panic(fmt.Sprintf("trampolín desbalanceado: %d != %d", len(lw.tries), depth))
	}
	lw.tries = lw.tries[:depth-1]
	return 0
}

// hostThrow: LUAI_THROW — restaura el shadow-stack y "longjmp-ea" al try
// más interno. No retorna: Restore reescribe el estado de ejecución.
func hostThrow(ctx context.Context) {
	lw := fromCtx(ctx)
	if len(lw.tries) == 0 {
		panic("LUAI_THROW sin LUAI_TRY activo (abort de Lua fuera de protección)")
	}
	top := lw.tries[len(lw.tries)-1]
	lw.tries = lw.tries[:len(lw.tries)-1]
	lw.mod.ExportedGlobal("__stack_pointer").(api.MutableGlobal).Set(top.sp)
	top.snap.Restore([]uint64{1}) // el host_try del snapshot retorna 1
}

func hostNote(ctx context.Context, n int32) int32 {
	lw := fromCtx(ctx)
	lw.Notes++
	return n + 1
}

func hostRender(ctx context.Context, p, n int32) int32 {
	lw := fromCtx(ctx)
	b, _ := lw.mod.Memory().Read(uint32(p), uint32(n))
	out := make([]byte, 0, len(b)+2)
	out = append(out, '<')
	out = append(out, b...)
	out = append(out, '>')
	lw.mod.Memory().Write(uint32(p), out)
	lw.Renders++
	return int32(len(out))
}

func (lw *LuaWasm) Close() { lw.mod.Close(context.Background()) }

func (lw *LuaWasm) result() string {
	r, err := lw.resultLen.Call(lw.ctx)
	if err != nil {
		return "<error leyendo result_len>"
	}
	if r[0] == 0 {
		return ""
	}
	b, _ := lw.mod.Memory().Read(lw.bufPtr, uint32(r[0]))
	return string(b)
}

// Eval corre un chunk protegido. (resultado, error-de-lua?, error-duro)
func (lw *LuaWasm) Eval(chunk string) (string, string, error) {
	lw.mod.Memory().Write(lw.bufPtr, []byte(chunk))
	r, err := lw.evalFn.Call(lw.ctx, uint64(len(chunk)))
	if err != nil {
		return "", "", err
	}
	if r[0] != 0 {
		return "", lw.result(), nil
	}
	return lw.result(), "", nil
}

// CoSpawn crea una corrutina; CoResume la reanuda (arg nil ⇒ sin argumento).
func (lw *LuaWasm) CoSpawn(chunk string) (int32, error) {
	lw.mod.Memory().Write(lw.bufPtr, []byte(chunk))
	r, err := lw.coSpawn.Call(lw.ctx, uint64(len(chunk)))
	if err != nil {
		return 0, err
	}
	if int32(r[0]) < 0 {
		return 0, fmt.Errorf("no compila: %s", lw.result())
	}
	return int32(r[0]), nil
}

type CoStatus int

const (
	CoDone CoStatus = iota
	CoYield
	CoError
)

func (lw *LuaWasm) CoResume(ref int32, arg *string) (CoStatus, string, error) {
	alen := -1
	if arg != nil {
		lw.mod.Memory().Write(lw.bufPtr, []byte(*arg))
		alen = len(*arg)
	}
	r, err := lw.coResume.Call(lw.ctx, uint64(uint32(ref)), uint64(uint32(int32(alen))))
	if err != nil {
		return CoError, "", err
	}
	return CoStatus(r[0]), lw.result(), nil
}

var lwDebugHook func(enter bool)
var lwDebugTry func(L, f, ud int32)
