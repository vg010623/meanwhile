// Command meanwhile is a terminal companion that teaches AI concepts while
// Claude Code is working.
package main

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/vg010623/meanwhile/internal/paths"
)

// version is set at release time with -ldflags "-X main.version=...".
var version = "dev"

const usage = `meanwhile: learn AI fundamentals while Claude Code works.

Usage:
  meanwhile                      start the app (run it in a split pane)
  meanwhile setup                add meanwhile's hooks to Claude Code
  meanwhile uninstall            remove them (--purge also deletes meanwhile's data)
  meanwhile stats                wait time and learning progress
  meanwhile lesson ACTION        lesson screen as JSON, for the Claude Code panel
  meanwhile doctor               check that everything is wired up
  meanwhile content check        validate curriculum files (--dir DIR for drafts)
  meanwhile simulate EVENT       write a fake Claude event (for development)
  meanwhile version              print the version

meanwhile hook is what Claude Code runs; you never need to call it yourself.
`

// env bundles everything a command touches, so tests can swap it out.
type env struct {
	stdin          io.Reader
	stdout, stderr io.Writer
	now            func() time.Time
	paths          paths.Paths
	// claude runs the Claude Code CLI (to install the panel). interactive
	// passes the terminal through; otherwise the output is returned. nil
	// means Claude Code isn't available.
	claude func(interactive bool, args ...string) (string, error)
}

func main() {
	// The hook path runs on every Claude event: keep it minimal and silent.
	if len(os.Args) > 1 && os.Args[1] == "hook" {
		runHook()
		os.Exit(0)
	}
	p, err := paths.Default()
	if err != nil {
		fmt.Fprintln(os.Stderr, "meanwhile:", err)
		os.Exit(1)
	}
	e := env{stdin: os.Stdin, stdout: os.Stdout, stderr: os.Stderr, now: time.Now, paths: p, claude: runClaude}
	os.Exit(run(os.Args[1:], e))
}

func run(args []string, e env) int {
	if len(args) == 0 {
		return cmdUI(nil, e)
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "ui":
		return cmdUI(rest, e)
	case "setup":
		return cmdSetup(rest, e)
	case "uninstall":
		return cmdUninstall(rest, e)
	case "stats":
		return cmdStats(rest, e)
	case "lesson":
		return cmdLesson(rest, e)
	case "doctor":
		return cmdDoctor(rest, e)
	case "simulate":
		return cmdSimulate(rest, e)
	case "content":
		if len(rest) < 1 || rest[0] != "check" {
			fmt.Fprint(e.stderr, usage)
			return 2
		}
		return cmdContentCheck(rest[1:], e)
	case "version", "--version":
		fmt.Fprintln(e.stdout, version)
		return 0
	case "help", "-h", "--help":
		fmt.Fprint(e.stdout, usage)
		return 0
	default:
		fmt.Fprintf(e.stderr, "unknown command %q\n\n%s", cmd, usage)
		return 2
	}
}

// confirm asks a yes/no question on stdin. Anything but y/yes is "no".
func confirm(e env, question string) bool {
	fmt.Fprintf(e.stdout, "%s [y/N] ", question)
	var answer string
	fmt.Fscanln(e.stdin, &answer)
	return answer == "y" || answer == "Y" || answer == "yes"
}
