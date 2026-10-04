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
- **`--bare` disables OAuth** (`claude --help`: "Anthropic auth is strictly ANTHROPIC_API_KEY or apiKeyHelper"). Go deeper uses `--safe-mode` instead.

## From the docs, not yet verified by hand

These need an interactive session, which we can't script. Check them manually with `meanwhile simulate` turned off and a real `claude` session.

- `Stop` does **not** fire when the user presses Esc or Ctrl+C (documented). Fallbacks: the next prompt, `Notification idle_prompt` about 60 s later, `SessionEnd`, and the `x` key.
- Whether `idle_prompt` fires after an interrupt. The docs say it fires about 60 s after Claude finished and the user is idle.
- `PermissionRequest` fires before a permission dialog, but not in auto or bypass modes.
- `StopFailure` payload: documented as carrying `error_type`. We read `error_type` and fall back to `error`.
- `AskUserQuestion` arrives as `PreToolUse` with `tool_name: "AskUserQuestion"`. When the user answers, `PostToolUse` fires.
