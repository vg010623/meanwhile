# Progress

Handoff file. A new session should be able to start from this alone. Keep it current at the end of each milestone.

## Current status

**M0–M2 done.** Next: **M3: TUI shell (`internal/shell`, `internal/activity`) + `meanwhile` UI command.**

## What the project is (30 seconds)

`meanwhile` is a Go terminal app that runs in a split pane next to Claude Code. Claude Code hooks run `meanwhile hook` at key moments (prompt sent, finished, needs permission…). The hook appends one JSON line to a daily spool file and exits silently. The UI watches the spool, keeps a per-session state machine (idle / working / needs input), and shows short lesson cards **only while Claude is working**. When Claude needs input, the lesson pauses ("⚠ Claude needs you") and that time is not counted. When Claude finishes, a banner says so.

Layers: **core** (events, spool, wait state machine: knows nothing about learning), **shell** (TUI host: banners, skip, sleep/wake), **activities** (the learning curriculum is the first one), **content** (Markdown concept files, embedded in the binary).

## Key decisions (do not re-litigate)

- Go 1.27, module `github.com/vg010623/meanwhile`. Bubble Tea and Lip Gloss for the TUI, SQLite through `modernc.org/sqlite` (pure Go), JSONL spool for hook → app communication.
- Hooks go into `~/.claude/settings.json` (or `$CLAUDE_CONFIG_DIR/settings.json`) directly. No plugin.
- Hooks are synchronous with `timeout: 5`. The binary has a 500 ms internal limit. They print nothing and always exit 0.
- Needs-input time is not teaching time: the lesson hides and the time is excluded from stats.
- Interrupts (Esc / Ctrl+C) fire no hook. The fallbacks are: the next prompt (superseded), `Notification idle_prompt` (about 60 s), `SessionEnd`, and the manual `x` key. We don't read transcript files.
- Go deeper: `claude -p --safe-mode --tools "" --strict-mcp-config --no-session-persistence --output-format stream-json --verbose --include-partial-messages --max-turns 1 --system-prompt …` in an empty sandbox folder with `MEANWHILE_INTERNAL=1`. **Not `--bare`**: it disables OAuth and breaks subscription users. On first use the user picks a model: default, Sonnet or Haiku.
- Grace delay 4 s. Bell on needs-input only. macOS and Linux. MIT for code, CC BY 4.0 for content.

## Milestones

- [x] **M0**: repo, curriculum parser and validator (`internal/curriculum`), `meanwhile content check`, three placeholder concepts, CI workflow, licenses.
- [x] **M1**: `internal/paths`, `internal/events` (hook → event mapping), `internal/spool` (daily JSONL), `internal/hook` (silent, 500 ms deadline, headless detection), `internal/install` (order-preserving settings.json patching, diff, backup, atomic write), CLI `setup` / `uninstall [--purge]` / `doctor` / `simulate` / `hook`. Real payloads recorded in `testdata/hook-payloads/` and `docs/claude-code-behaviour.md`. Verified end to end with a real `claude -p` run.
- [x] **M2**: `internal/store` (SQLite WAL, namespaced migrations, quick_check, corrupt → restore `.bak` or start fresh, `BackupIfDue` with VACUUM INTO, KV), `internal/lock` (flock), `internal/core` (pure `Machine`, `Ingestor.Sync`, `CloseManual`, `OpenWaits`, `MarkShown`, `ComputeWaitStats`, `FormatDuration`), `meanwhile stats`, database check in `doctor`.
- [ ] **M3**: `internal/shell` TUI: sleep/wake, grace delay, banners, skip, single-instance lock, compact and no-colour modes.
- [ ] **M4**: `internal/activities/learn`: picker, browse-all with the soft prerequisite note, cards, "I know this", resume with recap, learn stats.
- [ ] **M5**: `internal/deeper`: headless Claude runner, consent and model choice, streaming, cancel, follow-ups, saved answers, error mapping.
- [ ] **M6**: hardening, README, GoReleaser config, release workflow.

## How the pieces fit (code map)

- `cmd/meanwhile/`: one file per command (`setup_cmd.go`, `doctor_cmd.go`…). `env` holds stdin/stdout/now/paths so commands are testable. `meanwhile hook` short-circuits in `main()` before anything else.
- `internal/events`: `Event` (the spool line), `FromHook` (payload → event), end reasons (`completed|error|idle|superseded|session_ended|stale|manual`).
- `internal/spool`: `Append`, `ReadNew(offsets)`, `Prune`. Partial trailing lines are left for the next read.
- `internal/install`: `Specs` (hooks we register), `Command(bin)`, `Plan` / `PlanUninstall` / `Apply` / `Installed`, `Diff`. The marker is the trailing shell comment `# meanwhile hook`.
- `cmd/meanwhile/app.go`: `allMigrations()` (core plus activities, so add activity migrations here), `openDB`, `newIngestor`. `stats_cmd.go` has `statsSections` for activities to add lines.
- `cmd/meanwhile/ui_cmd.go`: `cmdUI` is a stub until M3.
- `internal/core`: wait rules. A new prompt closes an open wait as `superseded` (the interrupt case). `idle_prompt` closes it with the end dated back to max(last event, now − 60 s). Needs-input time accumulates in `NeedsInput` and is excluded from `Working()`. No event for 30 min (`StaleAfter`) closes it as `stale`. All times are stored as Unix ms. Headless waits are kept but left out of stats.
- `doctor` has an `extraDoctorChecks` hook so the store can add a database check without import cycles.

## Testing without touching real data

```sh
export MEANWHILE_HOME=$(mktemp -d)            # all meanwhile data goes here
go build -o /tmp/mw ./cmd/meanwhile
/tmp/mw setup --settings /tmp/s.json --bin /tmp/mw --yes   # never the real ~/.claude/settings.json
claude -p "..." --settings /tmp/s.json        # real hooks into the temp spool
/tmp/mw simulate start|input|resume|end|idle|error|session-end
```

## Gotchas learned so far

- A card's fallback recap (its first sentence) is truncated to 120 characters. Only explicit `> Recap:` lines get the length warning.
- `go:embed` can't reach parent folders, so `content/content.go` embeds `content/concepts/*.md` and exposes `content.FS` and `content.Dir`.
- Hook payloads don't reveal headless runs. `internal/hook/headless.go` walks ancestor processes (`ps -o ppid=,args=`) on `UserPromptSubmit` only.
- A quoted binary path splits the text "meanwhile hook", so the marker is a trailing `# meanwhile hook` comment instead.
- Settings are re-emitted with two-space indent (Claude's own format). A file in that format round-trips byte for byte; others get reformatted once, with a backup and a diff shown.
- macOS has no `timeout` command, and zsh globs fail when nothing matches; in scripts use `ls | while read`.
- Spool events are sorted by timestamp with a stable sort, so ties keep their write order. IDs have a random suffix, so sorting by ID breaks ties at random.
- `store.Open` uses `SetMaxOpenConns(1)`. Don't hold a `rows` open while running another query on the same DB, or it deadlocks.
