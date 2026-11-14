# meanwhile: notes for Claude sessions

**Read `progress.md` first.** It says what is done, what's next, and the decisions already made. Don't re-derive them. If the user says "resume", continue from "M7: next steps" in `progress.md`.

**Current work (M7):** a Claude Code mod that shows lessons inside Claude's own window. Before touching the mod, load the `plugin-authoring` skill: it gives this build's mods folder and API types. Confirm unknowns with a prototype before building on them, and record what's verified in `docs/claude-code-behaviour.md`.

- The full approved design is in `docs/plan.md`. Section "6b. Decisions made" overrides anything earlier in that file.
- Verified Claude Code behaviour (hook payloads, flags) goes in `docs/claude-code-behaviour.md`.
- At the end of each milestone, update `progress.md`, then commit and push. The user doesn't want check-ins between milestones.
- Never edit the user's real `~/.claude/settings.json` while testing. Use `--settings <tmpfile>` and `MEANWHILE_HOME=<tmpdir>`.
- The user writes the curriculum. Only placeholder concepts live in `content/concepts/`.

## Commands

```sh
go test -race ./...          # all tests
go vet ./... && gofmt -l .   # lint
go run ./cmd/meanwhile content check
```

## Conventions

- Go standard library first. Every new dependency needs a reason in `progress.md`.
- The core (`internal/core`, `internal/events`, `internal/spool`) must never import anything about learning or the UI.
- Activities live in `internal/activities/<name>` and own their own tables, prefixed `<name>_`.
- The hook path (`internal/hook`) must print nothing to stdout and must always exit 0.
