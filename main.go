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
