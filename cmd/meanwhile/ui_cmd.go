package main

import "fmt"

// cmdUI starts the terminal app. Implemented in M3.
func cmdUI(args []string, e env) int {
	fmt.Fprintln(e.stderr, "The meanwhile app is not built yet.")
	return 1
}

// cmdStats prints wait and learning statistics. Implemented in M2.
func cmdStats(args []string, e env) int {
	fmt.Fprintln(e.stderr, "meanwhile stats is not built yet.")
	return 1
}
