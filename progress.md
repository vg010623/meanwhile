# Progress

Handoff file. A new session should be able to start from this alone. Keep it current at the end of each milestone.

## Current status

**v1 (M0–M6) and M7 are done; in beta.** `v0.1.0-beta.2` is public (repo public since 21 Nov 2026). Before **beta 3**: add the website agent's remaining 11 beta concepts (the owner reviews every card for accuracy; see "Beta curriculum" below), then tag `v0.1.0-beta.3` and delete beta 2. Beta 3 is needed because the panel (installed from `main`) now calls `meanwhile lesson deeper` and `deeper-save`, which beta 2's binary lacks. Lessons show in a panel inside Claude Code (the `meanwhile-panel` plugin, installed by `meanwhile setup`), in the terminal app, or both; progress is shared. Next: the M7 follow-ups below, then "What's left on v1" (above all, writing the real curriculum and making the repo public).

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

### Step 1 findings (types of Claude Code 2.1.295, read 15 Nov 2026; still not tested live)
- **Esc:** `turn.abort` is only a method (`$.turn.abort({ turnId })`), not an event. Instead, **`turn.complete` carries `reason: 'answer' | 'aborted' | 'refusal' | 'error'`** (and `isAborted`). So an Esc should be detected instantly, which the hooks never could.
- **Turn start:** `turn.start` (`{ text, turnId }`) fires once per user turn. A subagent's run raises no `turn.start`; its `turn.complete` has `agentId` set, so ignore those. `turn.step` is the per-model-request event.
- **Needs input:** there's no direct event. The prototype uses two signals. (1) The **`tool.check`** verdict: `decision === 'ask'` means a permission dialog, but the docs say "ask puts it to the mode's decider", so auto mode may settle it with no dialog. It needs a live check. (2) **`tool.call`** with `tool === 'AskUserQuestion'`. The block ends when that call's `next(e)` resolves, which for a permission ask is after the tool has also run.
- **`$.process.run(argv, { cwd, env, stdin, timeoutMs })`** resolves `{ exitCode, stdout, stderr }` (30 s timeout by default). `$.process.spawn` streams. This is what step 4 will use to call the Go binary.
- **Timers:** `$.clock.after(ms, fn)` returns `{ cancel }`. `$.clock.now()` resolves Unix ms.
- **Panel width:** a panel the mod opens unasked (as ours is, from a timer) seats only from **144 columns**. Narrower, it waits undrawn. If that's a problem in practice, switch to the `AbovePrompt` band.
- **Gotcha:** `claude` on the owner's PATH is an old Homebrew install (2.1.263), and its `plugin validate` rejects `tool.check`. Validate with the running build instead, e.g. `~/.local/share/claude/versions/<version> plugin validate mod/meanwhile-panel`.
- **Gotcha:** the validator requires any function that takes `$` to be declared at the top level of the module (a closure inside `register` is refused).

### The prototype (`mod/meanwhile-panel/`)
`hooks/register.tsx` with a `$.state` contract in `types/index.d.ts` (phase: idle / working / blocked / finished / stopped / error). `turn.start` sets "working" and starts a 4 s `clock.after`; when it fires and Claude is still working, `$.ui.open` opens the panel. `turn.complete` sets finished, stopped (aborted) or error, or resets silently if the panel never opened. `tool.check` ask and `tool.call` AskUserQuestion set "blocked"; the end of that call sets "working" again. The panel shows a header, a placeholder card (or "Switch to Claude…" while blocked) and a `[ ✕ Close ]` button (hotkey `x` when focused). There are no tests yet (`claude plugin test`; see the skill's reference for `claude-code/testing`). To try it: `claude --plugin-dir ~/dev/projects/meanwhile/mod/meanwhile-panel`, with the terminal at least 144 columns wide.

### M7: next steps
1. ~~Load the `plugin-authoring` skill and grep the types.~~ Done; see "Step 1 findings".
2. ~~Build a prototype mod.~~ Done; see "The prototype".
3. ~~Test it live with the user.~~ Done on 2.1.325. One open item: keyboard focus (ctrl+x tab) didn't work, but clicks do. Progress is only in `$.state` for now (per session); step 4 moves it into the Go app's database.
4. ~~JSON CLI and real lessons in the panel.~~ Done and checked live (21 Nov 2026). `meanwhile lesson current|next|back|known|pick|start ID` (`cmd/meanwhile/lesson_cmd.go`) prints a `learn.Lesson` as JSON (errors are `{"error": …}` with exit 1). The logic is in `internal/activities/learn/lesson.go` and built on the existing store and graph functions; the Bubble Tea view was left untouched. The KV key `learn/panel_concept` remembers the concept the panel opened, so re-reading a finished concept keeps its place. With nothing ready, `all` lists every concept to re-read. A call takes about 6 ms. The mod runs it with `$.process.run(['/bin/sh','-c', …])`, adding `~/go/bin`, `/opt/homebrew/bin` and `/usr/local/bin` to PATH (`MEANWHILE_BIN` overrides), with no permission prompt.
   - Go deeper in the panel (21 Nov 2026): a one-time consent screen says it uses the person's own Claude account (plan limits or API credits) and offers Haiku or Sonnet, remembered in `$.store` (`deeperModel`). Then `$.model.complete` (the session's own client, no tools) uses the same tutor prompt as the terminal app and shows the tokens used. It has Stop and Change model. The happy path was checked by the owner; edge cases (in and out repeatedly, Stop, errors) are not yet tested. Since 21 Nov 2026, answers are **saved** in `learn_deep_dives` through `meanwhile lesson deeper CONCEPT CARD` and `deeper-save` (JSON on stdin), shared with the terminal app. A card with a saved explanation shows "📖 Your explanation" (`card.has_deeper`) and opens it with no model call; "Ask again" starts a new thread. There are **3 follow-ups** per explanation (the terminal app's `maxFollowUps` went from 2 to 3 to match), as three one-click suggestions plus an `Input` box.
   - Narrow windows (21 Nov 2026): when the side panel can't seat, a strip above the prompt offers the lesson (Read / Later); the inline box uses a compact layout. Verified by the owner.
   - "Needs your input" now comes from `classic.PermissionRequest` (real dialogs only), not `tool.check`.
   - Follow-ups: the mod hard-codes the 4 s delay and ignores `min_wait_seconds` and `ignore_dirs` from `config.toml`. Card Markdown is flattened to plain text. There are no `claude plugin test` tests for the mod yet. Keyboard focus (ctrl+x tab) didn't work.
5. ~~**Packaging**~~ Done and verified on 21 Nov 2026. The owner ran `meanwhile setup --marketplace ~/dev/projects/meanwhile`, chose "both", and a fresh `claude` session (no `--plugin-dir`) opened the panel on its own and reopened it with `/meanwhile`. What it does: `.claude-plugin/marketplace.json` at the repo root (marketplace `meanwhile`, plugin `meanwhile-panel`, source `./mod/meanwhile-panel`). `meanwhile setup` installs the hooks as before, then asks "panel / tab / both" (default both; `--mode` skips it). For panel or both it runs `claude plugin install meanwhile-panel --marketplace <src>` (default `vg010623/meanwhile`; `--marketplace <folder>` for a local checkout). Tab only uninstalls `meanwhile-panel@meanwhile`. `uninstall` removes the panel too. The hooks always stay, because they feed stats and the tab app. `/meanwhile` (a mod slash command) reopens the panel at any time and seats it at any width.
   - The repo is **private**, so others can't install from GitHub yet ("What's left on v1" step 4: make it public).
   - Gotcha: `~/go/bin` is not on the owner's PATH, so run `~/go/bin/meanwhile …` (or add it to PATH).
   - Setup fixes found along the way: up-to-date hooks are left alone wherever other tools' hooks sit, and declining the hooks change still offers the lesson choice.

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

## Beta curriculum (agreed 21 Nov 2026)

Drafts come from an AI agent; the owner reviews every card before it ships. Format: `docs/content-format.md`.
- **Done (4):** what-is-a-model, tokens, context-window, context-engineering.
- **For beta 3 (11):** next-token-prediction [tokens], hallucinations [next-token-prediction] (drop pretraining-vs-posttraining from its prerequisites for now), statelessness-and-chat [context-window], system-prompts [context-window], tool-use [tokens], the-agent-loop [tool-use, statelessness-and-chat], prompt-caching [statelessness-and-chat], compaction [statelessness-and-chat], verification-loops [tool-use, hallucinations], subagents-and-isolation [context-engineering], when-to-reset [context-engineering].
- **Later (5):** sampling-and-temperature, attention, lost-in-the-middle (careful: an older-model finding), pretraining-vs-posttraining, reasoning-and-thinking.
- Tracks and orders: foundations 10–30, how-models-work 40–100, from-model-to-agent 110–150, using-agents-well 160–200.

## Near-term direction (owner, 21 Nov 2026): more than static lessons

Lessons about AI (plus Go deeper) are too narrow on their own. Plan **other things that genuinely help someone while Claude Code runs in their session**. This is the near future, not someday; design it with the owner before building. Being Claude Code-only is accepted on purpose. Rule for every feature that calls a model (Go deeper included): **say up front that it uses the person's own Claude account and usage** (subscription limits or API credits).

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
