# Claude Code behaviour: verified facts

What we've confirmed about Claude Code, as opposed to what we assume. Each fact gives the version and how it was checked. Update this file when Claude Code changes.

## Verified on Claude Code 2.1.286 (4 Oct 2026)

### Hook payloads (recorded from a real `claude -p` run)

Real payloads are in `testdata/hook-payloads/`, with the prompt text and paths scrubbed. `internal/events/fixtures_test.go` runs the mapping over them.

| Event | Fields we saw |
|---|---|
| `SessionStart` | `session_id`, `transcript_path`, `cwd`, `hook_event_name`, `source` (`"startup"`) |
| `UserPromptSubmit` | the common fields plus `prompt_id`, `permission_mode`, and **`prompt` (the full prompt text)**. We never store `prompt`. |
| `PreToolUse` | the common fields plus `prompt_id`, `permission_mode`, `tool_name`, `tool_input`, `tool_use_id` |
| `PostToolUse` | as `PreToolUse`, plus `tool_response`, `duration_ms` |
| `Stop` | the common fields plus `prompt_id`, `permission_mode`, `stop_hook_active`, `last_assistant_message`, `background_tasks`, `session_crons` |
| `SessionEnd` | the common fields plus `prompt_id`, `reason` (`"other"` for a `-p` run) |

### Other facts

- **Hooks fire in headless `claude -p` runs**, including hooks passed through `--settings <file>`, merged with the user's own.
- **No payload field says a session is headless.** `SessionStart.source` is `"startup"` either way. We detect it by walking the hook's parent processes for `claude … -p` / `--print` (`internal/hook/headless.go`), only on `UserPromptSubmit`. Verified: the recorded `wait_started` had `headless: true`.
- **Hook cost:** the real binary takes about 4 ms per call (10 calls in 0.04 s on an M-series Mac).
- **Go deeper command verified** with a real subscription login. `claude -p --safe-mode --tools "" --strict-mcp-config --no-session-persistence --output-format stream-json --verbose --include-partial-messages --max-turns 1 --system-prompt … --model haiku` exits 0 and answers. The `init` line reports `"tools":[]` and `"mcp_servers":[]`. Asked to list files, the model said it has no tools. **No hooks fired**, even with extra hooks injected through `--settings`, so `--safe-mode` disables hooks in `-p` mode. Text arrives as `stream_event` → `content_block_delta` → `text_delta`. Other line types (`thinking_tokens`, `rate_limit_event`, `status`) are ignored. The recording is in `testdata/fake-claude/success.jsonl`.
- **`--bare` disables OAuth** (`claude --help`: "Anthropic auth is strictly ANTHROPIC_API_KEY or apiKeyHelper"). Go deeper uses `--safe-mode` instead.

## Verified interactively on Claude Code 2.1.296 (11 Oct 2026)

Checked in a real interactive session with meanwhile running in a second terminal, by reading the spool and `meanwhile stats` after each step.

- **Esc fires no hook.** No `Stop`, nothing at all.
- **`idle_prompt` does not fire after an Esc interrupt** (watched for 4 min 23 s). After a *normal* `Stop`, `Notification idle_prompt` reliably fires about 60 s later. By then the wait is already closed, so the core correctly ignores it. An interrupted wait therefore closes only on the next prompt (`superseded`), on `x`, or after 30 min (`stale`). The UI asks "Still working?" after `still_working_minutes` (default 2).
- **Esc on a permission dialog fires no hook** (no `PostToolUseFailure`, nothing). The blocked screen shows a "Dealt with it? Press x." hint after `still_working_minutes`.
- **`PermissionRequest`** fires about 2–3 s after the tool call in default mode. Each permission dialog also sends `Notification` (`permission_prompt`) about 6 s later; it's a duplicate, ignored by the machine. Approving leads to `PostToolUse`; a command that then fails leads to `PostToolUseFailure`. Neither fires in auto mode.
- **`AskUserQuestion`:** `PreToolUse` (`tool_name: AskUserQuestion`) gives `needs_input` with kind `question`. A `PermissionRequest` fires at the same instant (ignored as a duplicate, so the kind stays `question`), then a `Notification` about 6 s later. Answering leads to `PostToolUse`.
- **Short replies** (under 4 s) never showed the UI. Long ones showed after the 4 s grace delay and ended with "Claude finished".

Bugs found in that session and fixed:
- A superseded (interrupted) wait used to end at the *next prompt's* time, so the idle minutes after Esc counted as Claude working. It now ends at the wait's last event.
- The "Still working?" nudge appeared only after 10 min. The default is now 2 min.
- The "needs your input" screen had no way out after a permission dialog was dismissed with Esc. It now shows the quiet-time hint, and `x` closes it.
- "meanwhile showed N of M waits" counted quick waits as shown when a card was still open from the previous wait. A wait now counts as shown only after it passes the grace delay.

Second round of bugs from the same testing, fixed:
- `x` ("Claude's done") dated the end to the key press, so idle time after Esc counted as work (151.7 s recorded instead of about 5 s). It now ends at the wait's last event. If Claude's `Stop` arrives within 5 minutes of a manual close, Claude was in fact still running: the wait is corrected to end at the `Stop`, as `completed`.
- The "Still working?" nudge measured from the wait's *start*, because tool-call heartbeats never refreshed the UI's copy of the wait. It fired on every run longer than 2 minutes. The shell now refreshes the followed wait from the database on every sync.
- Another session blocked on the user was shown as "+1 other session working". It now says "1 other session needs you".
- Pressing `x` to dismiss "Claude finished" just as a new prompt started ended the new run. For the first 10 s of a wait, `x` now only closes the card.

## Not yet verified

- `StopFailure` payload: documented as carrying `error_type`. We read `error_type` and fall back to `error`.
