# Progress

Handoff file. A new session should be able to start from this alone. Keep it current at the end of each milestone.

## Current status

**M0 done.** Next: **M1: hooks, spool, setup/uninstall/doctor.**

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
- [ ] **M1**: `internal/events`, `internal/spool`, `internal/hook`, `internal/install` (settings.json patching), `setup` / `uninstall` / `doctor` / `simulate` commands; record real hook payloads in `docs/claude-code-behaviour.md`.
- [ ] **M2**: `internal/store` (SQLite, migrations, integrity check, backup), `internal/core` (ingestor and wait state machine), `meanwhile stats` (wait part).
- [ ] **M3**: `internal/shell` TUI: sleep/wake, grace delay, banners, skip, single-instance lock, compact and no-colour modes.
- [ ] **M4**: `internal/activities/learn`: picker, browse-all with the soft prerequisite note, cards, "I know this", resume with recap, learn stats.
- [ ] **M5**: `internal/deeper`: headless Claude runner, consent and model choice, streaming, cancel, follow-ups, saved answers, error mapping.
- [ ] **M6**: hardening, README, GoReleaser config, release workflow.

## Gotchas learned so far

- A card's fallback recap (its first sentence) is truncated to 120 characters. Only explicit `> Recap:` lines get the length warning.
- `go:embed` can't reach parent folders, so `content/content.go` embeds `content/concepts/*.md` and exposes `content.FS` and `content.Dir`.
