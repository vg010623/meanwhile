# Contributing

Thanks for helping. There are two kinds of contribution: lessons and code.

## Lessons

The curriculum is plain Markdown in `content/concepts/`, one file per concept. The format is in [docs/content-format.md](docs/content-format.md).

What makes a good card:

- **One idea per card,** readable in 20 to 40 seconds (60 to 140 words).
- **First principles:** start from something a working developer already knows, and explain why the idea exists, not only what it is.
- **No math, no code** unless the idea can't be explained without it.
- **A real recap line** (`> Recap: …`). It's what readers see when they come back to the concept.
- **Honest prerequisites:** list a concept as a prerequisite only if the card actually relies on it.

Before opening a pull request:

```sh
go run ./cmd/meanwhile content check
```

CI runs the same check, so a missing prerequisite or a cycle fails the build.

To read your lessons in the real app while writing them, set `content_dir` in `~/.config/meanwhile/config.toml` to your drafts folder, then press `enter` while the app is asleep to start a lesson.

## Code

- Go 1.27+. Run `go test -race ./...`, `go vet ./...` and `gofmt -l .` before pushing.
- The core (`internal/events`, `internal/spool`, `internal/core`) knows nothing about learning or the UI. Keep it that way.
- New activities implement `internal/activity.Activity`, live in `internal/activities/<name>`, own their own tables (prefixed `<name>_`), and are registered in `cmd/meanwhile/app.go`.
- The hook (`internal/hook`) runs on every Claude event. It must stay fast, must never print to stdout, and must always exit 0.
- If you change how meanwhile talks to Claude Code, check it against a real `claude` and update `docs/claude-code-behaviour.md`.
