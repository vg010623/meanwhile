package main

import (
	"flag"
	"fmt"
	"io/fs"
	"os"

	"github.com/vg010623/meanwhile/content"
	"github.com/vg010623/meanwhile/internal/curriculum"
)

// cmdContentCheck validates the embedded curriculum, or a folder on disk when
// --dir is given (useful while writing new concepts).
func cmdContentCheck(args []string, e env) int {
	fl := flag.NewFlagSet("content check", flag.ContinueOnError)
	fl.SetOutput(e.stderr)
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
		fmt.Fprintln(e.stdout, i)
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
	fmt.Fprintf(e.stdout, "%d concepts, %d cards: %d errors, %d warnings\n", len(cur.Concepts), cards, errs, warns)
	if errs > 0 {
		return 1
	}
	return 0
}
