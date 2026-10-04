# Plan: a learning companion for Claude Code wait time

*Name: **`meanwhile`**. Project location: **`/Users/vivek/dev/projects/meanwhile/`**, a new directory created in M0 (not `~/dev/agent-reach`). Run `git init` there, create a branch, and work there from now on.*

---

## 1. My understanding of the project

Developers lose the seconds-to-minutes that Claude Code spends working. They pick up their phone and come back late and having lost the thread. We are building a local, open-source terminal app that runs in a split pane. It stays quiet until a Claude run starts, then offers a short first-principles AI lesson card (20–40 s each). It tells the user clearly when Claude is done or needs them, so they go back promptly. The app has three layers:

- a **core**, which turns Claude Code hooks into a few generic events and knows nothing about learning
- **activities** that plug into a fixed interface (the curriculum is the first one)
- **content** kept as plain files that contributors can improve through pull requests

Everything stays local. The only network call is an opt-in "Go deeper" call to the user's own `claude -p`.

This is a greenfield plan. The project will live in a new directory, `/Users/vivek/dev/projects/meanwhile/`. Your machine has Claude Code 2.1.286, Node 26, Go and Python 3.

### Where your plan conflicts with how Claude Code actually works

I checked these against the current docs (code.claude.com/docs/en/hooks, /headless, /cli-reference) and against `claude --help` on your machine.

| # | Your assumption | Reality | Impact on the plan |
|---|---|---|---|
| C1 | We can detect when the user interrupts or cancels a run | **No hook fires when the user presses Esc or Ctrl+C.** `Stop` explicitly does not fire on interrupts. | We need heuristics to close an interrupted wait (§5, E4). This is the biggest gap. |
| C2 | "Run headless with tools disabled": the obvious flag is `--bare` | `--bare` skips hooks, CLAUDE.md and MCP, **but it also turns off OAuth/keychain login**. It only accepts `ANTHROPIC_API_KEY`, so it would **break every subscription user**. | Use `--safe-mode` instead. It turns off hooks, CLAUDE.md, MCP, plugins and skills, and login keeps working. Combine it with `--tools ""` and an empty working directory. This must be checked with one real run in milestone M5. |
| C3 | A hook can passively observe | Mostly true. But for `UserPromptSubmit` and `SessionStart`, **anything the hook prints to stdout is injected into Claude's context**, and exit code 2 blocks the prompt. | Our hook must print nothing, always exit 0, and swallow its own errors. |
| C4 | Hooks fire only for interactive sessions | Hooks **also fire for `claude -p` runs**: the user's own scripts, and our own "Go deeper" call if we're careless. | Add a recursion guard (`--safe-mode` plus an environment-variable marker). The wait detector also has to tolerate headless runs (§5, E12). |
| C5 | "Claude needs input" is one signal | It comes from several hooks. `PermissionRequest` fires right before a permission dialog, but not in auto or bypass permission modes. `Notification` with `permission_prompt` fires only after about 6 s. `AskUserQuestion` is a tool call (`PreToolUse`), and MCP elicitations have their own `Elicitation` event. | The core maps all of these to one `needs_input` event. |
| C6 | "Claude finished" equals the end of a task | `Stop` fires at the end of **every turn**, including when Claude stops to ask a question in plain text. API errors fire `StopFailure` instead of `Stop`. | That's exactly the wait boundary we want. We handle `StopFailure` as an end with reason `error`. |

---

## 2. Stack options (you decide; my recommendation is marked ★)

### 2a. Language and runtime

| Option | Pros | Cons |
|---|---|---|
| ★ **Go** | Ships as a single static binary with no runtime to install. The hook process starts in about 5 ms, which matters because it runs on every prompt and tool call. Cross-compiling is easy. | Smaller contributor pool than JS. More verbose. |
| **TypeScript on Node** | Largest contributor pool. You can install with `npx`. React-style UI through Ink. | Node takes about 50–100 ms to start for every hook call. Users need a Node runtime, and Claude Code's native installer no longer requires one, so we can't assume it's there. Shipping a `node_modules` tree adds dependency churn. |
| **Python** | Easiest to read. Textual is a polished terminal UI library. | Packaging is the weak spot (pipx, uv, venvs), and startup time is about 50 ms or more. |

**Why Go:** the hook runs inside Claude's critical path, so startup cost and "nothing to install" matter most. Contributors mostly edit Markdown content, so the code language affects fewer people than it seems. **This is a close call that depends on what you want to learn.** If you'd rather deepen your TypeScript or Python, the architecture below carries over unchanged (Q1).

### 2b. Terminal UI framework (follows from 2a)

| Option | Notes |
|---|---|
| ★ **Bubble Tea + Lip Gloss + Glamour (Go)** | Uses the Elm architecture: state, then update, then view. That fits an event-driven app well, because hook events become just another message. It comes with a test harness (`teatest`). Lip Gloss handles colour, `NO_COLOR` and width. Glamour renders the Markdown cards. |
| tview (Go) | Widget-based and imperative. Fine for forms, but clunkier for custom layouts and testing. |
| Ink (TypeScript) or Textual (Python) | The natural pick if you choose that language. |

### 2c. Local storage

| Option | Pros | Cons |
|---|---|---|
| ★ **SQLite in WAL mode** (pure-Go driver `modernc.org/sqlite`, so no C compiler is needed) | Writes are transactional, so a crash halfway through a write can't corrupt progress. It handles two processes writing at once safely (edge case E6). Stats are simple SQL. Migrations are easy. | It's a binary file, so users can't hand-edit it. |
| JSON files | Easy for humans to read. | Two writers or a crash during a write can corrupt them. You end up re-implementing locking and atomic writes yourself. |
| Append-only JSONL log only | Simple and durable. | Every stats query becomes a full scan, and progress turns into "replay the whole history". |

Hook events are also written to a plain JSONL spool file first (see 2e), so the debugging story stays human-readable.

### 2d. Packaging, distribution and install

| Option | Notes |
|---|---|
| ★ **GoReleaser, publishing to GitHub Releases and a Homebrew tap**, plus `go install` | One tagged release builds binaries for macOS and Linux, and both CPU types. Install with `brew install <you>/tap/meanwhile`, then run `meanwhile setup`. |
| npm package that wraps the binary | Familiar to JS developers, but adds a second packaging channel to maintain. Can be added later. |
| `curl \| sh` install script | Convenient, but many developers rightly distrust it. Fine as a secondary option. |

### 2e. How hook scripts talk to the running UI (IPC)

| Option | Pros | Cons |
|---|---|---|
| ★ **Append to a JSONL spool file; the UI watches it** (with fsnotify, plus a 500 ms polling fallback) | Events are durable even when the UI isn't running (E1). The hook is dumb and fast, and never touches the database. Small appends with `O_APPEND` are effectively atomic, so concurrent sessions are safe. You can debug with `tail -f`. | The UI has to track its read position. Files need rotating. Latency is about 10–50 ms, which is irrelevant here. |
| Unix domain socket to the UI | Instant, with a direct message to the UI. | If the UI isn't listening, the event is lost unless you also log it, so you end up building the spool anyway. Stale socket files need cleanup. |
| The hook writes straight into SQLite | One store. | The hook now runs migrations and handles lock contention inside Claude's critical path. A bug there can corrupt the user's progress. |

**Why the spool:** it separates "record what happened" (the hook, which must never fail) from "interpret it" (the core, which can be restarted or replayed). The spool uses one file per day (`spool/2026-10-01.jsonl`) instead of rename-based rotation. That avoids a race where a hook appends to a file that's being rotated.

### 2f. Testing approach

★ Use a layered approach:

- **Pure unit tests**, table-driven, for:
  - mapping hook payloads to events, using recorded real payloads in `testdata/`
  - the wait state machine
  - graph validation and picker ranking
  - patching `settings.json`
- **Golden-file tests** that snapshot the UI output at a fixed size, in colour and with `NO_COLOR`, using `teatest`.
- **A fake `claude` binary** placed on `PATH` that replays scripted `stream-json` output, success and every error shape, for "Go deeper".
- **A `simulate` dev command** that writes fake events to the spool, so you can drive the UI by hand without spending Claude usage.
- **A small manual checklist** run against real Claude Code before each release.

The alternative, full end-to-end tests through tmux or expect, is slow and flaky. I'd skip it for v1.

### 2g. License

| Option | Notes |
|---|---|
| ★ **MIT for code + CC BY 4.0 for content** | MIT is the most permissive and familiar license, with the least friction for contributors. Curriculum prose is not code, so a Creative Commons license is the conventional fit and makes clear that people can reuse lessons with attribution. |
| Apache-2.0 for code | Like MIT, but adds an explicit patent grant. Slightly more ceremony. |
| GPL-3.0 or AGPL | Forces forks to stay open. That deters some contributors and companies, and isn't needed when there's no hosted service. |
| CC BY-SA 4.0 for content | Use this if you want derivative courses to stay open too. |

---

## 3. Architecture

```
Claude Code ──hook stdin JSON──▶ meanwhile hook ──append──▶ ~/.local/state/meanwhile/spool/YYYY-MM-DD.jsonl
                                (silent, exit 0)                         │
                                                                         ▼ (fsnotify / poll)
                                         ┌─────────────────── core ─────────────────────┐
                                         │ Ingestor: read spool from saved offsets      │
                                         │ → Wait state machine → events/waits in SQLite│
                                         │ → publishes CoreEvent on a channel           │
                                         └──────────────────────┬───────────────────────┘
                                                                ▼
                                         ┌───────────── shell (TUI host) ───────────────┐
                                         │ asleep/awake, grace delay, banners           │
                                         │ ("Claude's done", "needs input"), skip key,  │
                                         │ layout, small-terminal & no-colour handling  │
                                         └──────────────────────┬───────────────────────┘
                                                                ▼ Activity interface
                                         ┌──────── activities/learn (v1) ───────────────┐   ┌─ deeper ─┐
                                         │ graph, picker, cards, resume, "I know this"  │──▶│ claude -p│
                                         └──────────────────────────────────────────────┘   └──────────┘
```

**Why a shell sits between the core and the activities:** banners, the skip key and the "Claude's done" behaviour should look the same for every activity. If each activity drew them itself, the future prediction game would re-implement them, slightly differently. So the shell owns the chrome around the screen and the activity owns the content area.

### 3a. Core event contract (this is the stable boundary)

The hook writes one normalized line per relevant hook call:

```
SpoolEvent {
  v: 1                         // schema version, so the format can change later
  id: ULID                     // unique and time-sortable; makes ingestion idempotent
  ts: RFC3339Nano              // wall clock when the hook ran
  kind: wait_started | wait_ended | needs_input | input_resolved | session_started | session_ended
  session_id, cwd, worktree_root   // worktree_root: walk up from cwd to the dir containing .git (a file or a dir), no git subprocess
  source_hook: "Stop"          // the raw event name, kept for debugging
  detail: { end_reason?: completed|error|interrupted|superseded|session_ended|timeout,
            error_type?: rate_limit|…, input_kind?: permission|question|elicitation,
            transcript_path? }
}
```

The prompt text is **never stored**, for privacy and because we don't need it.

**How hooks map to events.** All hooks are installed into the user scope (`~/.claude/settings.json`) so they work in every project.

| Hook (matcher) | Core event |
|---|---|
| `UserPromptSubmit` | `wait_started` |
| `Stop` | `wait_ended(completed)` |
| `StopFailure` | `wait_ended(error, error_type)` |
| `PermissionRequest`; `PreToolUse` with matcher `AskUserQuestion`; `Elicitation`; `Notification` with `permission_prompt` or `elicitation_dialog` as a deduplicated fallback | `needs_input` |
| `PostToolUse`, `PostToolUseFailure`, `PermissionDenied` | `input_resolved` (ignored unless the session is currently waiting on the user); also serves as a "still alive" heartbeat |
| `Notification` with `idle_prompt` (about 60 s after Claude went idle) | `wait_ended(timeout)`, only if a wait is still open. This is the backstop for interrupts (C1). |
| `SessionStart` / `SessionEnd` | `session_started` / `session_ended` (closes any open wait as `session_ended`) |
| `SubagentStart` and `SubagentStop` | ignored, because the main turn's `Stop` is what matters |

**Hook command.** It uses the shell form so that it does nothing if our binary has been removed without uninstalling:

```
command -v meanwhile >/dev/null 2>&1 && meanwhile hook 2>/dev/null; exit 0
```

- `timeout: 5`. The binary enforces its own 500 ms limit internally.
- It is **synchronous, not async**. That keeps events in order, because a 1-second wait could otherwise log `Stop` before `UserPromptSubmit`, and our hook takes about 5 ms. The trade-off: if the disk hangs, Claude could stall for up to 5 s. With `async: true` it never would, but then we need a reorder buffer. I chose ordering. You can flip this.
- If the environment variable `MEANWHILE_INTERNAL=1` is set, the hook exits immediately. That is the recursion guard (C4).

### 3b. Activity interface (conceptual; the Go signatures come at implementation time)

```
Activity
  Meta() → {id, name, description}
  Migrations() → []Migration            // the activity owns its tables, namespaced (e.g. learn_*), so the core schema never changes
  Init(ctx: {store (scoped), content, config, clock, logger}) → error
  OnWaitStart(wait: WaitInfo) → View    // WaitInfo = {wait_id, session_id, cwd, worktree_root, started_at, is_resume_of_quick_succession}
  OnNeedsInput(wait)                    // informational; the shell already shows the banner
  OnWaitEnd(wait, reason)               // e.g. learn: "let the user finish the current card"
  Save() → error                        // called on every meaningful state change and on shutdown
  Close()

View = the shell's UI component (Bubble Tea model): Update(msg), View(width, height) → string, KeyHelp()
```

Activities register in a small registry (`activities.Register(learn.New())`). The prediction game would be a new package plus one registration line, with no changes to the core.

The interface is deliberately shaped so the prediction game fits:

- it gets `cwd` and `worktree_root` at wait start, so it can snapshot `git status`
- `OnWaitEnd` lets it compute the diff and score the prediction

### 3c. Data model (SQLite, `~/.local/state/meanwhile/state.db`)

The core owns these tables:

- `ingest_offsets(file, byte_offset)`
- `events(id PK, ts, kind, session_id, cwd, worktree_root, source_hook, detail_json)`
- `sessions(session_id PK, cwd, worktree_root, first_seen, last_seen, ended_at)`
- `waits(id PK, session_id, cwd, worktree_root, started_at, ended_at, end_reason, needs_input_ms, shown BOOL, activity_id)`. `shown` records whether the UI actually appeared for this wait.
- `kv(namespace, key, value_json)`: settings such as Go deeper consent and schema version.

The `learn` activity owns these tables:

- `learn_concept_progress(concept_id PK, status: in_progress|completed|known, current_card_id, current_card_index, updated_at, completed_at)`
- `learn_card_reads(concept_id, card_id, read_at, wait_id NULL)`. Every row is one card actually read, which is what stats count.
- `learn_deep_dives(id, concept_id, card_id, parent_id NULL, question, answer, status: complete|cancelled|error, model, created_at)`

**Why `session_id` and `worktree_root` are on every row from day one:** multi-session v2 is then a UI change (group waits by session) rather than a data migration.

**User config** lives in `~/.config/meanwhile/config.toml` (XDG paths, and the same on macOS for simplicity). It holds things like `min_wait_seconds`, `bell`, `deeper.model`, `content_dir` and `colors`. Config files are for preferences a human edits; the database is for state the app owns.

### 3d. Curriculum content format

There is one Markdown file per concept, `content/concepts/<concept-id>.md`. It has front matter, and each `## ` heading starts a card.

```markdown
---
id: tokens
title: Tokens
summary: How models chop text into pieces before they ever "read" it.
prerequisites: [what-is-a-model]
track: foundations          # optional grouping for the "browse all" view
order: 20                   # optional tiebreak in recommendations
---

## Text becomes numbers {#text-becomes-numbers}
Card body…

> Recap: Models never see letters, only token IDs.

## Why not whole words? {#why-not-words}
…
```

Why this format:

- **One file per concept** means a reviewer sees the whole lesson in one PR diff, and authors never touch code.
- **Explicit `{#card-id}` anchors** (falling back to a slug of the heading) mean saved progress survives an author inserting or reordering cards. Progress is stored by card ID, with the index kept only as a fallback.
- **The `> Recap:` line** is the one-line resume summary. If it's missing, the first sentence of the card is used.
- **The content is embedded in the binary** (`go:embed`), so it can never go missing.
- **`content_dir` override:** setting `content_dir` (or `--content-dir`) loads files from disk and live-reloads them. That's for you while you write.

**Validation** runs as `meanwhile content check`, and the same code runs as a Go test in CI on every PR.

- Errors (these fail CI):
  - bad front matter
  - duplicate concept or card IDs
  - a prerequisite that doesn't exist
  - **cycles**, found with a depth-first search that prints the cycle path, e.g. `attention → transformers → attention`
  - a concept with fewer than 3 or more than 5 cards
  - an empty card
- Warnings:
  - a card outside 60–140 words, which is roughly 20–40 s at 200 words per minute
  - a recap longer than 120 characters
  - concepts that no other concept builds on and that have no track

The two or three **placeholder concepts** (`what-is-a-model` → `tokens` → `context-window`) will contain lorem-style dummy text clearly marked `PLACEHOLDER`.

### 3e. Folder structure

```
cmd/meanwhile/main.go          # CLI: setup, uninstall, ui (default), hook, stats, doctor, content check, simulate
internal/
  events/      # SpoolEvent types: the core contract
  hook/        # stdin payload → SpoolEvent → spool append (silent, never fails)
  spool/       # daily JSONL files, append, read-from-offset, prune
  core/        # ingestor + wait state machine (pure, heavily tested)
  store/       # SQLite open, migrations, integrity check, daily backup
  install/     # settings.json patching (install, uninstall, dry-run diff)
  shell/       # Bubble Tea host: sleep/wake, banners, skip, layout
  activity/    # Activity interface + registry
  activities/learn/   # graph, picker, cards, progress
  deeper/      # headless claude runner, stream parser, error mapping
  content/     # loader, parser, validator
  config/
content/concepts/*.md            # curriculum (CC BY 4.0)
docs/                            # architecture, content-authoring guide, claude-code-behaviour.md (verified hook findings)
testdata/hook-payloads/          # real recorded payloads
testdata/fake-claude/            # scripted stream-json fixtures
```

---

## 4. How each v1 feature works

1. **Setup and uninstall.**
   - `meanwhile setup` reads `~/.claude/settings.json`. If the file isn't valid JSON, it **refuses** and tells the user what to fix; it never "repairs" their file.
   - It writes a timestamped backup, then shows a diff and asks for confirmation (`--yes` skips this and `--dry-run` only shows it).
   - It adds **its own separate matcher group** per event. Other hooks are never merged into or reordered.
   - Edits are made as path-level patches (`tidwall/sjson`), so the user's key order and formatting are preserved. Decoding into a Go map and re-encoding would scramble key order.
   - The file is written atomically: write a temporary file, then rename it over the original.
   - Running `setup` again is idempotent.
   - `uninstall` removes **every hook entry whose command contains `meanwhile hook`**, in any event and any group, wherever it ended up. It then removes groups and arrays that are left empty and prints what it removed.
   - `uninstall --purge` also deletes our data directory.
   - Claude Code picks up settings changes live, through its file watcher.
2. **Wait detection.** This is the hook mapping in §3a plus a state machine in `core`, one per session:

   ```
   idle → working (wait_started)
   working ⇄ needs_input
   working|needs_input → idle (wait_ended | superseded | session_ended | timeout)
   ```

   Each closed wait gets a row in `waits` with its duration and `end_reason`.
3. **Terminal UI.**
   - Running `meanwhile` with no arguments starts the UI.
   - Asleep, it shows one dim line: `meanwhile · waiting for Claude · q quit`.
   - It wakes after a **grace delay** (default 4 s, `min_wait_seconds`), so short waits never flash.
   - It goes back to sleep after the user finishes or skips once the wait has ended.
   - A lock file (`flock`) ensures there is one live UI. A second instance says "already running in another pane".
4. **Curriculum graph.**
   - The content loader builds a directed acyclic graph of concepts.
   - Each concept is in one of three states: done (`completed` or `known`), `in_progress`, or ready (all prerequisites done).
5. **Picker.**
   - It lists up to five *ready* concepts that aren't done yet, ranked by:
     1. how many concepts they unlock, transitively ("most useful next")
     2. then `order`
     3. then title
   - `/` or `b` opens "browse all", grouped by track, with a ✓ or ◐ marker on each concept.
   - Picking a concept with unmet prerequisites shows **one line**: `Builds on: Tokens, Embeddings (not done yet). Enter to start anyway, Esc to go back.`
6. **"I know this"** (`k`). This works from the picker or from inside a concept. It sets `status = known`, which counts as done for unlocking. Stats count `known` separately from `completed`, so they stay honest. It can be undone from browse-all.
7. **Resume.**
   - Progress is saved write-through on every card advance.
   - At wait start, the default (highlighted, Enter) is `Continue: Tokens (card 3 of 4)` followed by `Last time: <recap of card 2>`.
   - Below that, smaller: `p pick something else`.
   - If nothing is in progress, the picker shows directly.
8. **"Claude's done" banner.**
   - The shell shows a full-width inverse banner, `✔ Claude finished · <dir name> · 1m 42s`, and an optional terminal bell (`bell = true`). The current card stays readable.
   - Pressing Enter on the last card, or `x`, closes it.
   - Progress for the current card is saved the moment the card is shown, so closing early is fine.
   - `needs_input` shows a distinct, more urgent banner, `⚠ Claude needs your input (permission)`, because Claude is **blocked**. That matters more than "done".
9. **Skip** (`s` or Esc at the top level). The UI goes to sleep for this wait. Nothing is recorded as missed, and the next prompt starts with no guilt messaging.
10. **Go deeper** (`d` on any card).
    - **One-time consent screen:** "This runs your installed Claude Code (`claude -p`) using your own subscription or API usage. It runs in an empty folder with every tool disabled, so it cannot read or change your files. [y] enable · [n] not now." The answer is stored in `kv`.
    - **Command.** The process runs with its working directory set to `~/.local/state/meanwhile/sandbox/` (an empty directory) and the environment variable `MEANWHILE_INTERNAL=1`. The prompt goes in on **stdin**.

      ```
      claude -p --safe-mode --tools "" --strict-mcp-config --no-session-persistence
             --output-format stream-json --verbose --include-partial-messages
             --max-turns 1 --system-prompt <our tutor prompt> [--model <deeper.model>]
      ```

      Why each part:
      - `--tools ""` removes every built-in tool.
      - `--strict-mcp-config` with no `--mcp-config` means no MCP tools.
      - `--safe-mode` drops hooks (no recursion), CLAUDE.md, plugins and skills, but keeps the user's login. This avoids C2.
      - The empty working directory is defence in depth.
      - `--system-prompt` replaces the large default coding-agent prompt, which also makes each call cheaper.
    - **Prompt contents:**
      - the tutor role: first principles, plain language, no math, under about 250 words, check understanding with an analogy
      - the current concept's title and summary, plus the full current card
      - the titles of completed concepts ("the learner already understands: …") and those not yet done
      - the user's question; the default is "Explain this card more deeply"
    - **Follow-ups (up to 2).** These are **stateless**: we re-send the earlier exchange in the next prompt rather than using `--resume`. The trade-off is a few hundred extra tokens per call. In return, nothing is left behind in the user's Claude session history and there is no session state to manage. `--no-session-persistence` keeps these calls out of the user's `/resume` list.
    - **Streaming.** We parse `stream_event` text deltas from the stream-json output and append them to a scrollable viewport.
    - **Cancel** with Esc. The process runs in its own process group, gets SIGINT, then SIGKILL after 2 s. The partial answer is saved with status `cancelled`.
    - **Saved answers** go in `learn_deep_dives`. `D` on a concept lists past answers.
    - **Error mapping.** Every error leads to a short message and "Back to cards".

      | Failure | Message |
      |---|---|
      | `claude` not on `PATH` | "Claude Code not found" |
      | `result.is_error` with authentication, or exit 1 with an auth message | "Not logged in. Run `claude` once." |
      | `rate_limit` | "Usage limit reached. Try later." |
      | `system/api_retry` events | Live "retrying (2/5)…" |
      | Network or no first byte within 30 s | "Offline or Claude unreachable" |
      | Hard timeout | 120 s |

11. **Stats** (`meanwhile stats`) shows:
    - total and median wait time
    - the number of waits, and how many the UI appeared for
    - concepts completed and concepts marked known
    - cards read
    - deep dives
    - a "last 7 days" line

    It runs the ingestor first, so it's accurate even if the UI never ran.
12. **Local-only.**
    - No telemetry code exists at all.
    - The only process that touches the network is the user-triggered `claude` child process.
    - A `doctor` command prints exactly which files we read and write.
    - The README documents the data locations.

**Extra commands:**

- `meanwhile doctor` checks:
  - that the binary is on `PATH`
  - that our hooks are present in user settings, and whether `disableAllHooks` is set or a managed policy might block user hooks
  - when the last event arrived ("no events in 24 h, are hooks firing?")
  - that `claude` is found
  - that content validates and the database is intact
- `meanwhile simulate start|input|end` is a development aid.

---

## 5. Edge cases

| # | Case | Handling |
|---|---|---|
| E1 | UI not running when hooks fire | Events wait in the spool. When the UI starts, it ingests the backlog. Open waits younger than 10 min are shown as live; older ones are closed as `timeout`. |
| E1b | UI crashes mid-wait | Progress is write-through per card, so nothing is lost beyond the card on screen. On restart the ingest offset continues. A leftover "Go deeper" child process is tracked by a PID file and killed on the next start. |
| E2 | Very short waits | The UI stays asleep unless the wait is still open after `min_wait_seconds` (default 4). The wait is still logged with `shown = false`. |
| E3 | Permission or input needed mid-run | `needs_input` banner plus bell. The current card stays put. `input_resolved` (the next tool event) clears the banner. The time spent blocked goes into `needs_input_ms`, which is excluded from "useful wait time" in stats. |
| E4 | User interrupts (no hook; C1) | Three backstops, in order: (a) the next `UserPromptSubmit` in the same session closes the old wait as `superseded`; (b) `Notification(idle_prompt)` about 60 s later closes it as `timeout`; (c) after 10 min with no events, the banner says "Still working? `x` mark done". **Optional:** tail the session's `transcript_path` for the interrupt marker. This is faster but depends on an undocumented file format (Q4). |
| E5 | Several waits in quick succession | If a wait starts within 30 s of the previous one ending, and the user is still mid-card, the view isn't reset. The banner just switches from "done" to "working". The picker never pops up again mid-card. |
| E6 | Two Claude sessions at once | Data stays correct: each event carries `session_id`, the state machine runs per session, the spool appends are atomic, and SQLite in WAL mode takes the writes. One UI instance does all ingestion, and the CLI ingests under the same lock. The UI **follows the most recently started session**. Others appear as one status line ("+1 other session working"), and their banners show the directory name. |
| E7 | Uninstall after the user edited the hook config | Removal matches the marker in the command string, not saved positions. Groups the user changed are handled by removing only our command entries. If the file has become invalid JSON, it refuses with a message. `doctor` warns if our marker shows up in project settings, which happens when the user copied it there. |
| E8 | Corrupted or missing database | On open we run `PRAGMA quick_check`. On failure the file is moved to `state.db.corrupt-<ts>`, a fresh one is created, the user sees a one-line notice, and the database restores from the daily `VACUUM INTO` backup if one exists. A missing file is simply created. |
| E8b | Corrupted or missing content | The embedded content can't go missing. An invalid `content_dir` override shows validation errors and falls back to the embedded content. Progress pointing at a deleted concept or card is kept but ignored; card IDs fall back to the card index. |
| E9 | Small or no-colour terminals | Text wraps to the pane width and caps at 72 columns. Below 40×10 the UI switches to compact mode: banner plus card title plus "enlarge pane to read". `NO_COLOR` and `TERM=dumb` are respected. Banners rely on symbols and words, never colour alone. |
| E10 | "Go deeper" still running when a wait starts or ends | It keeps streaming, because it belongs to the user, not to the wait. The banners appear above it. If the user skips or quits the UI, it is cancelled and the partial answer saved. Only one "Go deeper" call runs at a time. |
| E11 | Hook binary removed without uninstall | The shell-form command does nothing, so Claude never sees an error. Homebrew's `caveats` text tells users to run `meanwhile uninstall` first. |
| E12 | Headless `claude -p` runs from the user's scripts or CI | They fire our hooks too. M1 will check whether the payloads reveal that a session is headless. If they do, these waits are logged but the UI doesn't wake. If not, there's a config deny-list of directories. Our own calls are always excluded (C4). |
| E13 | Clock or timestamp oddities | Durations are computed from hook timestamps, never from when the UI happened to ingest them. Negative or more-than-24-hour durations are clamped and flagged. |
| E14 | Spool growth | Daily files. Fully ingested files older than 7 days are deleted. |
| E15 | The user has `disableAllHooks` set, or a managed policy blocks user hooks | `doctor` detects and explains it. The UI's asleep line shows "no Claude events seen yet. Run `meanwhile doctor`." |

---

## 6. Milestones (each one can be finished and tested on its own)

- **M0. Skeleton and content pipeline.**
  - Repo, license files, CI (lint plus tests).
  - Content parser, validator and `content check`.
  - Three placeholder concepts.
  - Tests for cycles, missing prerequisites and card-count rules.
  - *Done when* CI catches a deliberately introduced cycle.
- **M1. Hooks and spool, then verify Claude Code behaviour.**
  - `hook` command, spool writer, `setup`, `uninstall` (with dry-run diff) and `doctor`.
  - Unit tests on `settings.json` fixtures: empty file, existing user hooks, edited-after-install, invalid JSON.
  - **Manual verification with real Claude**, recorded in `docs/claude-code-behaviour.md`:
    - real payloads
    - whether `idle_prompt` fires after an interrupt
    - what `PermissionRequest` looks like across permission modes
    - what headless sessions look like (E12)
  - The recorded payloads become test fixtures.
- **M2. Core ingestion and wait state machine.**
  - Spool to SQLite, migrations, per-session state machine, `stats` (wait parts only).
  - Table-driven tests for E2–E6 and E13.
- **M3. TUI shell with a dummy activity.**
  - Sleep and wake, grace delay, both banners, skip, single-instance lock, compact and no-colour modes.
  - `simulate` command and golden tests.
  - *Done when* you can run Claude in one pane and see the shell wake and report correctly in the other.
- **M4. Learn activity.**
  - Graph, picker with ranking, browse-all with the soft prerequisite note, cards, "I know this", resume with recap, progress persistence, full `stats`.
- **M5. Go deeper.**
  - Consent, runner with the fake-`claude` tests (stream, cancel, all error shapes), follow-ups, saved answers.
  - **One real call to confirm C2**: `--safe-mode` keeps subscription login working, fires none of our hooks, and the model has no tools.
- **M6. Hardening and release.**
  - Corruption recovery and daily backup, a full edge-case pass using the manual checklist.
  - README, CONTRIBUTING with the content-authoring guide, GoReleaser and a Homebrew tap, first tagged release.

---

## 6b. Decisions made (4 Oct 2026). These override anything above.

- **Language:** Go, with Bubble Tea, SQLite, the spool file, GoReleaser + Homebrew, and the layered test approach.
- **Name and location:** `meanwhile`, at `~/dev/projects/meanwhile`.
- **Install:** edit the hooks in `~/.claude/settings.json` directly (no plugin).
- **Needs-input time is not teaching time.** When Claude asks for permission or asks a question:
  - the lesson pauses and hides, and the pane shows only `⚠ Claude needs you — switch to Claude`
  - the time is excluded from the wait time in stats
  - when Claude starts working again (the next tool event), the pane comes back on the same card
  - the shell does this for every activity; activities only get `OnPause` and `OnResume` calls
  - stats keep only `working_ms`; blocked time is stored as `needs_input_ms` for diagnostics only
- **Interrupts (Esc / Ctrl+C):** handled with fallback signals only; we don't read transcript files. The signals: the next prompt, Claude's idle notification about 60 s later, the session ending, and a manual `x` key ("Claude's done").
- **Go deeper model:** on the one-time opt-in screen, ask the user to pick: their Claude Code default, Sonnet, or Haiku. It can be changed later in config.
- **Bell and delay:** grace delay 4 s. The bell is on for needs-input only.
- **Platforms:** macOS and Linux; Windows only through WSL.
- **Licenses:** MIT for code, CC BY 4.0 for content.
- **Hook timing:** synchronous, with an internal 500 ms limit.

## 7. Open questions for you

- **Q1. Language.** Go (my pick), or do you want to learn TypeScript or Python instead? This decides 2b, 2d and part of 2f.
- **Q2. Name.** Decided: `meanwhile`. It's used for the binary, the config and state paths (`~/.config/meanwhile`, `~/.local/state/meanwhile`), and the hook marker `meanwhile hook`.
- **Q3. Install method.** I recommend editing `~/.claude/settings.json` directly, as described in feature 1: it's transparent, works offline and has no dependency on the plugin system. The alternative is to ship as a **Claude Code plugin** with a `hooks/hooks.json` file, installed and removed via `claude plugin install` and `claude plugin uninstall`. That's cleaner to uninstall and never touches `settings.json`. The costs are that it needs a marketplace entry, and the hook still has to find our binary. Which do you prefer?
- **Q4. Interrupt detection.** Are heuristics (a)–(c) good enough for v1, or should we also tail the transcript file, accepting that its format is undocumented?
- **Q5. "Go deeper" model.** Use the user's default model, or default to a cheaper or faster one such as Sonnet or Haiku to save their usage? The setting stays configurable either way.
- **Q6. Defaults.** Grace delay of 4 s? Terminal bell on done and on needs-input: on or off by default?
- **Q7. Platforms.** macOS and Linux for v1, with Windows only through WSL. OK?
- **Q8. Content license.** CC BY 4.0, or CC BY-SA 4.0 so derivative courses stay open?
- **Q9. Hook timing.** Synchronous (in-order events, worst case about a 5 s stall if the disk hangs), or `async: true` (never stalls Claude, but needs a reorder buffer)?

Implementation starts only after you've chosen the stack options above and approved the plan.

## Verification (end to end, once built)

- `go test ./...` passes; `meanwhile content check` passes on the placeholder content.
- `meanwhile setup --dry-run` shows only additions. After `setup` and then `uninstall`, `settings.json` is byte-for-byte identical to the backup, including with pre-existing user hooks.
- Real run: Claude in pane A and `meanwhile` in pane B. A short prompt doesn't wake the UI. A long prompt wakes it, shows "Continue…", then the done banner. A permission prompt shows the needs-input banner. Esc in Claude ends the wait within about 60 s. `stats` matches.
- "Go deeper" works on a subscription login. During the call, our spool shows no new events. Asking it to "list files in this directory" shows it has no tools.
