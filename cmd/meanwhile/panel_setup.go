package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// The lesson panel inside Claude Code is a Claude Code plugin (mod/
// meanwhile-panel), listed in this repository's .claude-plugin/
// marketplace.json. Setup installs it with Claude Code's own CLI.
const (
	panelPlugin                  = "meanwhile-panel"
	panelMarketplace             = "meanwhile" // the "name" in marketplace.json
	panelPluginID                = panelPlugin + "@" + panelMarketplace
	defaultMarketSrc             = "vg010623/meanwhile"
	modePanel, modeTab, modeBoth = "panel", "tab", "both"
)

// runClaude is env.claude in real use.
func runClaude(interactive bool, args ...string) (string, error) {
	bin, err := exec.LookPath("claude")
	if err != nil {
		return "", fmt.Errorf("the claude command was not found on your PATH")
	}
	cmd := exec.Command(bin, args...)
	if interactive {
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		return "", cmd.Run()
	}
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err = cmd.Run()
	return out.String(), err
}

// chooseMode asks where lessons should appear, unless a flag said so.
// An empty answer (or no terminal) picks both.
func chooseMode(e env, flagValue string) (string, error) {
	switch flagValue {
	case modePanel, modeTab, modeBoth:
		return flagValue, nil
	case "":
	default:
		return "", fmt.Errorf("--mode must be panel, tab or both")
	}
	fmt.Fprint(e.stdout, `
How do you want your lessons?
  1. Inside Claude Code (a panel next to the chat)
  2. In a separate terminal tab (run `+"`meanwhile`"+`)
  3. Both
Choose 1, 2 or 3 [3]: `)
	var answer string
	fmt.Fscanln(e.stdin, &answer)
	switch strings.TrimSpace(answer) {
	case "1":
		return modePanel, nil
	case "2":
		return modeTab, nil
	default:
		return modeBoth, nil
	}
}

// setupLessons installs or removes the panel to match the mode and says how
// to start lessons.
func setupLessons(e env, mode, marketplace string) int {
	if mode == modePanel || mode == modeBoth {
		if e.claude == nil {
			fmt.Fprintln(e.stderr, "meanwhile: Claude Code isn't available, so the panel wasn't installed.")
			return 1
		}
		fmt.Fprintf(e.stdout, "\nInstalling the lesson panel into Claude Code (claude plugin install %s --marketplace %s)…\n", panelPlugin, marketplace)
		if _, err := e.claude(true, "plugin", "install", panelPlugin, "--marketplace", marketplace); err != nil {
			fmt.Fprintf(e.stderr, "meanwhile: installing the panel failed: %v\nYou can try it yourself: claude plugin install %s --marketplace %s\n", err, panelPlugin, marketplace)
			return 1
		}
		fmt.Fprintln(e.stdout, "The panel opens by itself when Claude works for more than a few seconds.")
		fmt.Fprintln(e.stdout, "Type /meanwhile in Claude Code to open it at any time.")
	} else if e.claude != nil {
		// Switching to the tab only: take the panel out if it was installed.
		if _, err := e.claude(false, "plugin", "uninstall", panelPluginID); err == nil {
			fmt.Fprintln(e.stdout, "\nRemoved the lesson panel from Claude Code.")
		}
	}
	if mode == modeTab || mode == modeBoth {
		fmt.Fprintln(e.stdout, "\nFor the terminal app: open a split pane or a new terminal tab and run `meanwhile`.")
	}
	return 0
}

// removePanel uninstalls the panel quietly; it may never have been installed.
func removePanel(e env) {
	if e.claude == nil {
		return
	}
	if _, err := e.claude(false, "plugin", "uninstall", panelPluginID); err == nil {
		fmt.Fprintln(e.stdout, "Removed the lesson panel from Claude Code.")
	}
}
