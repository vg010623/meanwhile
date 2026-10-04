# meanwhile

Learn AI fundamentals in the seconds and minutes you spend waiting on Claude Code.

`meanwhile` runs in a split pane next to Claude Code. It stays quiet until Claude
starts working, then shows a short first-principles lesson card (20 to 40
seconds). When Claude finishes or needs you, it tells you, so you get back to
work on time.

> **Status:** early development. Only the curriculum format and validator exist
> so far. See the milestones below.

## How it works

1. `meanwhile setup` adds a few hooks to `~/.claude/settings.json`.
2. Claude Code runs `meanwhile hook` at key moments (prompt sent, finished,
   needs permission). The hook writes one line to a local log file and exits.
   It prints nothing and never changes what Claude does.
3. The `meanwhile` app in your split pane watches that log and shows lessons
   only while Claude is actually working.

Everything stays on your machine. There is no telemetry. The only network call
is the optional "Go deeper" feature, which runs your own installed `claude`.

## Writing curriculum

Concepts are Markdown files in [`content/concepts/`](content/concepts). See
[docs/content-format.md](docs/content-format.md) for the format, then check your
work:

```sh
go run ./cmd/meanwhile content check
go run ./cmd/meanwhile content check --dir path/to/drafts
```

## Development

```sh
go test ./...
```

## Milestones

- [x] M0: repo skeleton, curriculum format and validator
- [ ] M1: hook, log file, `setup` / `uninstall` / `doctor`
- [ ] M2: wait detection and storage
- [ ] M3: terminal UI shell
- [ ] M4: learning activity (picker, cards, resume, "I know this")
- [ ] M5: "Go deeper"
- [ ] M6: hardening and first release

## License

Code: [MIT](LICENSE). Curriculum: [CC BY 4.0](content/LICENSE).
