package runtime

import (
	"fmt"
	"os"

	lua "github.com/yuin/gopher-lua"
)

// safeLibs son las librerías estándar de Lua que el baseline (§1.2) permite
// abrir tal cual: puro cómputo, sin IO bloqueante que congele el event loop.
// `io` y `package` (require) se omiten deliberadamente: `io` está prohibido y
// `require` queda reservado al loader de plugins (§1.1, llega en S11).
var safeLibs = []struct {
	name string
	open lua.LGFunction
}{
	{lua.BaseLibName, lua.OpenBase},
	{lua.TabLibName, lua.OpenTable},
	{lua.StringLibName, lua.OpenString},
	{lua.MathLibName, lua.OpenMath},
	{lua.CoroutineLibName, lua.OpenCoroutine},
	{lua.OsLibName, lua.OpenOs},
}

// bannedOsFuncs son las funciones de `os` que el baseline retira: todo lo que
// haga IO, mute el proceso o lea el entorno por fuera de las primitivas del
// core. §1.2 enumera execute/exit/remove/rename/getenv; añadimos `setenv` y
// `tmpname` por el mismo motivo declarado ("todo IO debe pasar por las
// primitivas async del core"): mutar el entorno es competencia de
// `nu.sys.setenv` (S17) y los temporales, de `nu.fs.tmpdir` (S14). Quedan
// `clock`, `date`, `time`, `difftime` y `setlocale`: cómputo puro sobre el
// reloj, sin efectos.
var bannedOsFuncs = []string{
	"execute", "exit", "remove", "rename", "getenv", "setenv", "tmpname",
}

// applySandbox abre solo las librerías permitidas y recorta la superficie
// peligrosa que el baseline del entorno Lua (§1.2) prohíbe.
func applySandbox(L *lua.LState) {
	for _, lib := range safeLibs {
		L.Push(L.NewFunction(lib.open))
		L.Push(lua.LString(lib.name))
		L.Call(1, 0)
	}

	// Recorta `os`: deja el cómputo de reloj, quita el IO/efectos.
	if osMod, ok := L.GetGlobal("os").(*lua.LTable); ok {
		for _, name := range bannedOsFuncs {
			osMod.RawSetString(name, lua.LNil)
		}
	}

	// `io` nunca se abre (no está en safeLibs), pero lo dejamos explícitamente
	// en nil por si una librería futura lo tocara.
	L.SetGlobal("io", lua.LNil)

	// `dofile`/`loadfile` cargan ficheros del disco saltándose el loader; el
	// baseline los prohíbe fuera de él (§1.2). El loader (S11) los usará por su
	// cuenta sin reexponerlos como globales.
	L.SetGlobal("dofile", lua.LNil)
	L.SetGlobal("loadfile", lua.LNil)

	// `print` se redirige (§1.2). En esta sesión aún no existe `nu.log.info`
	// (llega en S03): de momento va a stderr, nunca a stdout, para no
	// contaminar la salida de `nu -e` (que es el valor de retorno del chunk).
	L.SetGlobal("print", L.NewFunction(sandboxPrint))
}

// sandboxPrint es el `print` provisional del baseline: escribe a stderr con el
// mismo formato que el `print` de Lua (argumentos separados por tabulador,
// terminados en nueva línea). S03 lo reemplaza por un alias de `nu.log.info`.
func sandboxPrint(L *lua.LState) int {
	top := L.GetTop()
	for i := 1; i <= top; i++ {
		if i > 1 {
			fmt.Fprint(os.Stderr, "\t")
		}
		fmt.Fprint(os.Stderr, L.ToStringMeta(L.Get(i)).String())
	}
	fmt.Fprintln(os.Stderr)
	return 0
}
