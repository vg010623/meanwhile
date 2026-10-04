package hook

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// Hook payloads don't say whether Claude is running interactively or as
// `claude -p` from a script. So, only when a prompt is submitted, we look at
// our ancestor processes: Claude Code runs the hook through a shell, so the
// claude process is usually our parent or grandparent.

// IsHeadless is replaceable in tests.
var IsHeadless = func() bool { return headlessAncestor(os.Getppid(), psLookup, 4) }

// psLookup returns a process's parent PID and full command line.
func psLookup(pid int) (ppid int, args string, ok bool) {
	out, err := exec.Command("ps", "-o", "ppid=,args=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0, "", false
	}
	fields := strings.Fields(strings.TrimSpace(string(out)))
	if len(fields) < 2 {
		return 0, "", false
	}
	ppid, err = strconv.Atoi(fields[0])
	if err != nil {
		return 0, "", false
	}
	return ppid, strings.Join(fields[1:], " "), true
}

// headlessAncestor walks up to depth ancestors looking for a claude process
// started with -p or --print.
func headlessAncestor(pid int, lookup func(int) (int, string, bool), depth int) bool {
	for i := 0; i < depth && pid > 1; i++ {
		ppid, args, ok := lookup(pid)
		if !ok {
			return false
		}
		if isClaudePrint(args) {
			return true
		}
		pid = ppid
	}
	return false
}

func isClaudePrint(args string) bool {
	fields := strings.Fields(args)
	if len(fields) == 0 {
		return false
	}
	isClaude := false
	for _, f := range fields[:min(2, len(fields))] { // "claude …" or "node /path/claude …"
		base := f[strings.LastIndex(f, "/")+1:]
		if base == "claude" || strings.HasPrefix(base, "claude-") {
			isClaude = true
		}
	}
	if !isClaude {
		return false
	}
	for _, f := range fields[1:] {
		if f == "-p" || f == "--print" {
			return true
		}
	}
	return false
}
