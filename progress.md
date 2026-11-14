# Progress

Handoff file. A new session should be able to start from this alone. Keep it current at the end of each milestone.

## Current status

**v1 (M0–M6) is done and working in a separate terminal tab. Next: M7, show the lessons inside Claude Code's own window (decided 14 Nov 2026).** If the user says "resume this", start at **"M7: next steps"** below.

## M7: lessons inside Claude Code's window ("Option A")

### Why
Running `meanwhile` in a second tab is itself a context switch. The user wants lessons to appear in the same terminal where they talk to Claude, with a ✕ to close them. We chose a **Claude Code mod** (an extension that draws its own panel inside Claude's UI) over **Option B** (the hook asking iTerm2/WezTerm/Kitty/tmux to open a split). B stays the fallback if the M7 prototype disappoints. B's drawbacks: different code for each terminal, Apple Terminal can't split, and the new pane steals focus.

### Planned shape (to confirm with the prototype)
- **The Go app stays the brain:** curriculum, progress, stats, Go deeper, setup.
- **The mod is a thin UI:** a TypeScript hooks module that draws the panel and calls the Go binary for content and progress through new JSON subcommands, e.g. `meanwhile lesson current --json`, `lesson next`, `lesson back`, `lesson known`, `lesson picker --json`. These are still to be designed in step 3. One process per action costs about 10 ms, which is fine.
- **Timing and banners come from the mod's own events:** `turn.start`, `turn.complete`, `turn.abort` (which may finally give an instant Esc signal), plus the 4 s grace delay. Keep the existing hooks and spool for wait stats until the mod proves equivalent; decide then whether to retire them.
- **The standalone TUI (`meanwhile` in a tab) stays** for terminals or setups without the mod.

### Facts read from the mod API's type definitions (Claude Code 2.1.317; not yet tested)
- To get the API and the mods folder: load the **`plugin-authoring` skill** first. It names the folder to write the mod in (per session, under `~/.claude/dev-mods/<session>/<mod-name>/`) and writes this build's type definitions file (`claude-code.d.ts`, about 14,000 lines; grep it, don't read it). A mod is `.claude-plugin/plugin.json`, `hooks/hooks.json` (`{"modules":["./register.tsx"]}`) and `hooks/register.tsx`. Check it with `claude plugin validate <dir>` and `claude plugin test <dir>`; develop with `claude --plugin-dir <dir>`. The skill's `examples/pane.tsx` is the panel template.
- **Panel:** `$.ui.open({ id, title })`, drawn by a `ui.render` hook on `{ component: 'Pane', requestId: id }`. It docks beside the transcript from 110 columns (from 144 if the user didn't ask for it). Narrower, it waits undrawn (`isPlaced: false`), unless we use the `placement: 'inline'` strip above the prompt or the `AbovePrompt` band. `$.ui.close` exists. Elements come from `$.ui.resolve(e)` (`Box`, `Text`, `Button` with `onPress`).
- **Keys:** the prompt keeps the keyboard. The panel gets keys only when focused (ctrl+x tab, a click, or `open({ focus })`). Esc always returns focus to the prompt. Buttons respond to a click, or Enter while focused.
- **Events available:** `turn.start`, `turn.complete`, `turn.abort`, `stop`, `prompt.submit`, `session.start`, `session.end`, `ui.press`, `ui.close`, `ui.focus`, `key`. There are also nouns: `$.process` (run programs), `$.fs`, `$.clock` (timers), `$.store` (persists across sessions), `$.state` (per session, needs a `types/index.d.ts` contract), `$.ui.toast`, `$.ui.status`.
- **Unknown until the prototype:** whether `turn.abort` fires on Esc; whether there is an event for permission prompts or AskUserQuestion; whether the panel draws and updates while Claude is mid-turn; how `turn.start` relates to subagent requests (the docs say it fires per model request, including subagents' `e.agentId`, so the start of a whole user turn may need `prompt.submit` instead); `$.process` call shape and speed.

### M7: next steps
1. **Load the `plugin-authoring` skill**, then grep the types for `'turn.abort'`, `'prompt.submit'`, `process: {`, `Pane: {` and any permission or needs-input event. Note the findings here.
2. **Build a prototype mod** (`meanwhile-panel`) with static text, no Go calls yet. On prompt submit, start a 4 s timer and open a panel saying "Claude is working" with a placeholder card and a ✕ button. On finish, show "✔ Claude finished". On abort, show "■ stopped" or close. ✕ closes the panel. The user must answer "Turn them on" to the engine's one-time question.
3. **Test it live with the user**, and record what's confirmed or refuted in `docs/claude-code-behaviour.md`: Esc → `turn.abort`, panel while working, focus and key feel, permission prompts, narrow windows.
4. If it holds up, **design the JSON CLI** in `cmd/meanwhile` (`lesson_cmd.go`) reusing `internal/activities/learn` logic (it currently lives in the Bubble Tea view, so split the logic out of `view.go` first, with tests). Then wire the panel to it: Continue/recap, card, next/back, I know this, picker.
5. **Packaging:** ship the mod in the repo (e.g. `mod/`), and have `meanwhile setup` offer to install it. Update the README and decide whether the hooks stay.

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

## What's left on v1 (needs the owner; independent of M7)

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
- `x` (manual close) ends a wait at its `LastEventAt`. A `Stop`/`StopFailure` within `manualCorrectionWindow` (5 min) of the key press corrects it to Claude's real end (`Machine.RecentManual`, `Corrected` change, `waits.closed_at`, core migration v3). The shell ignores `Corrected` except to refresh `m.wait`.
- The shell must refresh `m.wait` from `OpenWaits` on every sync: heartbeats change `LastEventAt` without producing a `Change`.
- For `manualDoneGuard` (10 s) after a wait starts, `x` only closes the card; it doesn't end the run.
