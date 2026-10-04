package main

import "fmt"

// cmdUI starts the terminal app. Implemented in M3.
func cmdUI(args []string, e env) int {
	fmt.Fprintln(e.stderr, "The meanwhile app is not built yet.")
	return 1
}
