package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vg010623/meanwhile/internal/install"
	"github.com/vg010623/meanwhile/internal/paths"
)

func cmdSetup(args []string, e env) int {
	fl := flag.NewFlagSet("setup", flag.ContinueOnError)
	fl.SetOutput(e.stderr)
	dryRun := fl.Bool("dry-run", false, "show the change without writing it")
	yes := fl.Bool("yes", false, "don't ask for confirmation")
	settings := fl.String("settings", "", "Claude Code settings file (default: ~/.claude/settings.json)")
	bin := fl.String("bin", "", "path of the meanwhile binary the hooks should run (default: this binary)")
	modeFlag := fl.String("mode", "", "where lessons appear: panel (inside Claude Code), tab (terminal app) or both (default: ask)")
	marketplace := fl.String("marketplace", defaultMarketSrc, "where Claude Code installs the panel from: owner/repo, a git URL or a folder")
	if err := fl.Parse(args); err != nil {
		return 2
	}

	path, err := settingsPath(*settings)
	if err != nil {
		fmt.Fprintln(e.stderr, "meanwhile:", err)
		return 1
	}
	binPath, err := binaryPath(*bin)
	if err != nil {
		fmt.Fprintln(e.stderr, "meanwhile:", err)
		return 1
	}

	r, err := install.Plan(path, binPath)
	if err != nil {
		reportSettingsError(e, err)
		return 1
	}
	if !r.Changed {
		fmt.Fprintf(e.stdout, "meanwhile's hooks are already set up in %s.\n", path)
		if *dryRun {
			return 0
		}
		return chooseAndSetupLessons(e, *modeFlag, *marketplace)
	}

	fmt.Fprintf(e.stdout, "meanwhile will add %d hooks to %s.\nYour existing hooks and settings are kept as they are.\n\n", len(install.Specs), path)
	fmt.Fprint(e.stdout, install.Diff(r.Before, r.After, 2))
	fmt.Fprintln(e.stdout)
	if *dryRun {
		fmt.Fprintln(e.stdout, "Dry run: nothing was written.")
		return 0
	}
	if !*yes && !confirm(e, "Apply this change?") {
		fmt.Fprintln(e.stdout, "Nothing was changed.")
		return 1
	}
	backup, err := install.Apply(path, r, e.now())
	if err != nil {
		fmt.Fprintln(e.stderr, "meanwhile: could not write settings:", err)
		return 1
	}
	if backup != "" {
		fmt.Fprintf(e.stdout, "Saved a backup of your old settings to %s\n", backup)
	}
	fmt.Fprintln(e.stdout, "Done. Claude Code picks up the hooks automatically.")
	return chooseAndSetupLessons(e, *modeFlag, *marketplace)
}

// chooseAndSetupLessons is the second half of setup: the hooks are in, now
// pick where lessons appear. Running setup again switches.
func chooseAndSetupLessons(e env, modeFlag, marketplace string) int {
	mode, err := chooseMode(e, modeFlag)
	if err != nil {
		fmt.Fprintln(e.stderr, "meanwhile:", err)
		return 2
	}
	return setupLessons(e, mode, marketplace)
}

func cmdUninstall(args []string, e env) int {
	fl := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	fl.SetOutput(e.stderr)
	yes := fl.Bool("yes", false, "don't ask for confirmation")
	purge := fl.Bool("purge", false, "also delete meanwhile's data (progress, wait history, saved answers)")
	settings := fl.String("settings", "", "Claude Code settings file (default: ~/.claude/settings.json)")
	if err := fl.Parse(args); err != nil {
		return 2
	}
	path, err := settingsPath(*settings)
	if err != nil {
		fmt.Fprintln(e.stderr, "meanwhile:", err)
		return 1
	}

	r, err := install.PlanUninstall(path)
	if err != nil {
		reportSettingsError(e, err)
		return 1
	}
	if r.Changed {
		fmt.Fprintf(e.stdout, "Removing %d meanwhile hook entries from %s:\n\n", r.Removed, path)
		fmt.Fprint(e.stdout, install.Diff(r.Before, r.After, 2))
		fmt.Fprintln(e.stdout)
		if !*yes && !confirm(e, "Apply this change?") {
			fmt.Fprintln(e.stdout, "Nothing was changed.")
			return 1
		}
		backup, err := install.Apply(path, r, e.now())
		if err != nil {
			fmt.Fprintln(e.stderr, "meanwhile: could not write settings:", err)
			return 1
		}
		fmt.Fprintf(e.stdout, "Hooks removed. Backup of the previous file: %s\n", backup)
	} else {
		fmt.Fprintf(e.stdout, "No meanwhile hooks found in %s.\n", path)
	}
	removePanel(e)

	if *purge {
		fmt.Fprintf(e.stdout, "\nThis deletes all meanwhile data:\n  %s\n  %s\n", e.paths.State, e.paths.Config)
		if !*yes && !confirm(e, "Delete it?") {
			fmt.Fprintln(e.stdout, "Data kept.")
			return 0
		}
		for _, dir := range []string{e.paths.State, e.paths.Config} {
			if err := os.RemoveAll(dir); err != nil {
				fmt.Fprintln(e.stderr, "meanwhile:", err)
				return 1
			}
		}
		fmt.Fprintln(e.stdout, "Data deleted.")
	}
	return 0
}

func settingsPath(flagValue string) (string, error) {
	if flagValue != "" {
		return flagValue, nil
	}
	return paths.ClaudeSettings()
}

// binaryPath picks the binary the hooks will run. A `go run` build lives in
// a temporary folder that disappears, so we refuse to install that.
func binaryPath(flagValue string) (string, error) {
	p := flagValue
	if p == "" {
		exe, err := os.Executable()
		if err != nil {
			return "", err
		}
		p = exe
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	if strings.Contains(abs, string(filepath.Separator)+"go-build") {
		return "", errors.New("this looks like a temporary `go run` build; install meanwhile first (go install ./cmd/meanwhile) or pass --bin")
	}
	if info, err := os.Stat(abs); err != nil || info.IsDir() {
		return "", fmt.Errorf("binary not found at %s", abs)
	}
	return abs, nil
}

func reportSettingsError(e env, err error) {
	if errors.Is(err, install.ErrInvalidSettings) {
		fmt.Fprintf(e.stderr, "meanwhile: %v\n\nmeanwhile won't edit a file it can't read safely. Fix the JSON (a trailing comma is the usual cause) and run this again.\n", err)
		return
	}
	fmt.Fprintln(e.stderr, "meanwhile:", err)
}
