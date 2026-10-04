// Command meanwhile is a terminal companion that teaches AI concepts while
// Claude Code is working.
package main

import (
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"

	"github.com/vg010623/meanwhile/content"
	"github.com/vg010623/meanwhile/internal/curriculum"
)

// version is set at release time with -ldflags "-X main.version=...".
var version = "dev"

const usage = `meanwhile: learn AI fundamentals while Claude Code works.

Usage:
  meanwhile content check [--dir DIR]   validate curriculum files
  meanwhile version                     print the version

More commands (setup, uninstall, hook, stats, doctor) arrive in later milestones.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "version", "--version":
		fmt.Fprintln(stdout, version)
		return 0
	case "content":
		if len(args) < 2 || args[1] != "check" {
			fmt.Fprint(stderr, usage)
			return 2
		}
		return contentCheck(args[2:], stdout, stderr)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}

// contentCheck validates the embedded curriculum, or a folder on disk when
// --dir is given (useful while writing new concepts).
func contentCheck(args []string, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("content check", flag.ContinueOnError)
	fl.SetOutput(stderr)
	dir := fl.String("dir", "", "folder of concept .md files (default: the content built into meanwhile)")
	if err := fl.Parse(args); err != nil {
		return 2
	}

	var (
		fsys fs.FS = content.FS
		root       = content.Dir
	)
	if *dir != "" {
		fsys, root = os.DirFS(*dir), "."
	}

	cur, issues := curriculum.Load(fsys, root)
	errs, warns := 0, 0
	for _, i := range issues {
		fmt.Fprintln(stdout, i)
		if i.Severity == curriculum.Error {
			errs++
		} else {
			warns++
		}
	}
	cards := 0
	for _, c := range cur.Concepts {
		cards += len(c.Cards)
	}
	fmt.Fprintf(stdout, "%d concepts, %d cards: %d errors, %d warnings\n", len(cur.Concepts), cards, errs, warns)
	if errs > 0 {
		return 1
	}
	return 0
}
