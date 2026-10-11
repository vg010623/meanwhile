# Progress

Handoff file. A new session should be able to start from this alone. Keep it current at the end of each milestone.

## Current status

**All milestones M0–M6 done. v1 is feature-complete.** The remaining steps need the owner (see "What's left" below).

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
- [x] **M3**: `internal/config` (TOML, defaults, `Ignored`), `internal/activity` (Activity/View interface, `DoneMsg`), `internal/shell` (Bubble Tea model: asleep/pending/awake × idle/working/blocked/finished, grace wake, needs-input screen hides the lesson, banners, `s` skip, `x` close or manual done, `enter` lesson on demand when asleep, other-session count, still-working prompt, compact mode, fsnotify plus 1 s tick sync). Not yet wired into `cmdUI`; the single-instance lock goes there.
- [x] **M4**: `internal/activities/learn`: `progress.go` (learn_* tables, write-through progress), `graph.go` (ready / recommend by transitive unlocks, then order, then title / unmet / byTrack / cardIndex by ID with index fallback), `view.go` (screens: home Continue + recap, picker, browse with ✓◐○·, soft note, card with scroll, complete with unlocks), `text.go` (small Markdown renderer, 72-column cap), `Stats()`. `cmdUI` is wired: UI lock, config, DB, daily backup, activity Init, `shell.Run`.
- [x] **M5**: `internal/deeper` (`Runner.Ask`, stream-json parser, error classification, first-byte and total timeouts, process-group cancel). `learn/deeper_view.go`: one-time consent screen with model choice (default / Sonnet / Haiku, stored in kv; config `[deeper] model` overrides), streaming, Esc cancel keeps the partial answer as `cancelled`, 2 stateless follow-ups, saved threads (`D` history, read-only), errors fall back to the cards. Verified against real Claude (see `docs/claude-code-behaviour.md`).
- [x] **M6**: skip or quit during Go deeper keeps the partial answer; raw events pruned after 30 days (core migration v2 adds an index on `events.ts`); `.goreleaser.yaml` (darwin and linux, amd64 and arm64, CGO off, `homebrew_casks` to vg010623/homebrew-tap, skipped without `HOMEBREW_TAP_GITHUB_TOKEN`); `.github/workflows/release.yml` on `v*` tags; full README; CONTRIBUTING. Verified: `goreleaser check` passes, snapshot build works, CI is green on macOS and Linux, real-pty smoke tests pass (full flow, single-instance lock, NO_COLOR, compact).

## What's left (needs the owner)

1. **Write the curriculum.** Replace the three PLACEHOLDER concepts in `content/concepts/`. Set `content_dir` in config to preview drafts live.
2. **Install for real:** `go install ./cmd/meanwhile && meanwhile setup`. This edits the real `~/.claude/settings.json`, so the owner should run it.
3. ~~Manually verify interactive behaviours~~: **done 11 Oct 2026 on Claude Code 2.1.296**; see `docs/claude-code-behaviour.md`. Four bugs found and fixed: superseded-wait end time, the 2 min still-working default, the blocked-screen hint, and shown-counting during quick succession. Only `StopFailure` is still unverified.
4. **Go public:** `gh repo edit vg010623/meanwhile --visibility public --accept-visibility-change-consequences`.
5. **Homebrew:** create the repo `vg010623/homebrew-tap` and add a repo secret `HOMEBREW_TAP_GITHUB_TOKEN` (a token with contents:write on the tap).
6. **Release:** `git tag v0.1.0 && git push --tags`.

## Ideas for after v1 (from the plan)

- The "predict which files Claude will change" activity: a new package in `internal/activities/`, registered in `cmd/meanwhile/app.go`. `WaitInfo.WorktreeRoot` is already passed in.
- A multi-session view: data already carries `session_id` and `worktree_root`, so this is UI work only.
- Code-signing the macOS binary, so the cask's quarantine workaround can go.

## How the pieces fit (code map)

- `cmd/meanwhile/`: one file per command (`setup_cmd.go`, `doctor_cmd.go`…). `env` holds stdin/stdout/now/paths so commands are testable. `meanwhile hook` short-circuits in `main()` before anything else.
- `internal/events`: `Event` (the spool line), `FromHook` (payload → event), end reasons (`completed|error|idle|superseded|session_ended|stale|manual`).
- `internal/spool`: `Append`, `ReadNew(offsets)`, `Prune`. Partial trailing lines are left for the next read.
- `internal/install`: `Specs` (hooks we register), `Command(bin)`, `Plan` / `PlanUninstall` / `Apply` / `Installed`, `Diff`. The marker is the trailing shell comment `# meanwhile hook`.
- `cmd/meanwhile/app.go`: `allMigrations()` (core plus activities, so add activity migrations here), `openDB`, `newIngestor`. `stats_cmd.go` has `statsSections` for activities to add lines.
- `cmd/meanwhile/app.go`: `activities()` is the registry; to add an activity, append it here. Its migrations join automatically.
- Learn keys: picker/browse `↑↓/jk` `enter` `i` (I know this) `b` (browse) `esc`; card `enter/→` next, `←` back, `i`, `d` (go deeper), `D` (saved answers), `j/k` scroll. Shell keys: `s` skip, `x` close / Claude's done, `q` quit, `enter` lesson on demand while asleep.
- `internal/shell`: activity views get keys only when awake and not blocked. `Capturing()` views get every key except ctrl+c. Tests drive the model with real spool writes plus `m.syncCmd(true)()` and `tickMsg`, using `fakeActivity`.
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

## End-to-end smoke test (real TUI in a pty)

tmux isn't installed; use `expect`. A session that passed: sleep → `simulate start` → "Claude is working" → grace wake → "Ready for you" → enter → card → `simulate input` → "needs your input" → `simulate resume` → `simulate end` → "Claude finished" → `x` → next start shows "Continue: …". Spawn with `sh -c "stty rows 30 cols 90; exec mw"` so Bubble Tea gets a size.

## Gotchas learned so far

- A card's fallback recap (its first sentence) is truncated to 120 characters. Only explicit `> Recap:` lines get the length warning.
- `go:embed` can't reach parent folders, so `content/content.go` embeds `content/concepts/*.md` and exposes `content.FS` and `content.Dir`.
- Hook payloads don't reveal headless runs. `internal/hook/headless.go` walks ancestor processes (`ps -o ppid=,args=`) on `UserPromptSubmit` only.
- A quoted binary path splits the text "meanwhile hook", so the marker is a trailing `# meanwhile hook` comment instead.
- Settings are re-emitted with two-space indent (Claude's own format). A file in that format round-trips byte for byte; others get reformatted once, with a backup and a diff shown.
- macOS has no `timeout` command, and zsh globs fail when nothing matches; in scripts use `ls | while read`.
- Spool events are sorted by timestamp with a stable sort, so ties keep their write order. IDs have a random suffix, so sorting by ID breaks ties at random.
- `store.Open` uses `SetMaxOpenConns(1)`. Don't hold a `rows` open while running another query on the same DB, or it deadlocks.
- Homebrew casks are macOS-only; Linux users install from the tarball or with `go install`. GoReleaser has deprecated `brews` in favour of `homebrew_casks`.
- Under NO_COLOR, `ESC[30;H` in output is cursor positioning, not colour. Don't mistake it for a NO_COLOR bug.
- **Esc in Claude fires no hook at all, and no `idle_prompt` follows it** (verified on 2.1.296). Interrupted waits are closed by the next prompt (ended at their last event), by `x`, or as stale after 30 min. The UI nudges after `still_working_minutes` (default 2), both on the working header and on the blocked screen.
- A wait is marked `shown` only in `checkGrace`, once it has passed `min_wait_seconds`, even when a card stayed open from the previous wait (`Model.shownID` stops double writes).
