// Command nu es el binario del runtime: un kernel Lua mínimo donde todo lo demás
// son extensiones (filosofia.md). En esta sesión (S01) solo expone la evaluación
// de un chunk con `-e`; el arranque con TTY, plugins y UI llega en sesiones
// posteriores.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/dbareagimeno/nu/internal/runtime"
)

func main() {
	os.Exit(run())
}

func run() int {
	eval := flag.String("e", "", "ejecuta el código Lua dado e imprime sus valores de retorno")
	flag.Parse()

	if *eval == "" {
		// Sin `-e` no hay nada que hacer todavía: la pantalla de runtime desnudo
		// (con TTY) y el arranque de plugins llegan en sesiones posteriores
		// (S33, S11). De momento, uso y salida con código de error.
		fmt.Fprintln(os.Stderr, "uso: nu -e '<código lua>'")
		return 2
	}

	rt := runtime.New()
	defer rt.Close()

	// Arranque canónico (§14, S11): carga los plugins activados en orden
	// topológico, ejecuta el `init.lua` del usuario el último y emite `core:ready`.
	// Sin directorios de plugins configurados (el caso de `nu -e` desnudo de S11)
	// solo corre el `init.lua` del usuario si existe. Un grafo de plugins roto
	// (colisión, ciclo, dependencia ausente) es un error de arranque accionable.
	if err := rt.Boot(); err != nil {
		fmt.Fprintln(os.Stderr, "error de arranque:", err)
		return 1
	}

	results, err := rt.EvalString(*eval)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}

	// Imprime cada valor de retorno en su propia línea, a stdout. `print` (que
	// va a stderr en esta sesión) no interfiere con esta salida.
	for _, r := range results {
		fmt.Println(r)
	}
	return 0
}
