# meanwhile

Learn AI fundamentals in the seconds and minutes you spend waiting on Claude Code.

When you give Claude Code a prompt, you wait. Most of us reach for our phone, and then come back late with the thread lost. `meanwhile` shows lessons right next to Claude: in a panel inside Claude Code's own window, in a split pane beside it, or both (you choose at setup). It stays quiet until Claude starts working, then shows a short lesson card, readable in 20 to 40 seconds, about how AI actually works: models, tokens, context windows, next-token prediction, hallucinations, and working well with agents (more lessons are on the way). When Claude finishes or needs you, it says so clearly, and you go back to work on time.

https://github.com/user-attachments/assets/7c27ddfe-a31d-4a51-af38-7c7d4c24a5c6

*The lesson panel opening beside Claude Code while it works.*

## Install

You need [Claude Code](https://claude.com/claude-code) (a recent version: the panel uses its mod API) on macOS or Linux.

Download the binary for your machine from the [releases page](https://github.com/vg010623/meanwhile/releases) (`darwin_arm64` for Apple silicon Macs, `darwin_amd64` for Intel Macs, `linux_amd64` / `linux_arm64` for Linux) and put it on your PATH. For example, on an Apple silicon Mac:

```sh
curl -fsSL https://github.com/vg010623/meanwhile/releases/download/v0.1.0-beta.3/meanwhile_0.1.0-beta.3_darwin_arm64.tar.gz | tar xz meanwhile
mkdir -p ~/.local/bin && mv meanwhile ~/.local/bin/
```

(Downloading with `curl` avoids macOS's "unidentified developer" block. If you download through a browser instead, run `xattr -d com.apple.quarantine ~/.local/bin/meanwhile` once.)

Or, with Go 1.27+:

```sh
go install github.com/vg010623/meanwhile/cmd/meanwhile@latest
```

Homebrew is coming later.

## Set up

```sh
meanwhile setup      # adds a few hooks to ~/.claude/settings.json (shows the change first)
```

`setup` keeps your existing hooks and settings, saves a backup of the old file, and shows exactly what it will add before writing anything. Then it asks where you want your lessons:

1. **Inside Claude Code**: a panel next to the chat. `setup` installs it with `claude plugin install meanwhile-panel --marketplace vg010623/meanwhile`. It opens by itself when Claude works for more than a few seconds; type `/meanwhile` in Claude Code to open it any time (for example after closing it). Don't want it popping up? `/meanwhile off` (or the **Auto-open** button in the panel) stops it opening by itself; `/meanwhile on` brings it back. It docks beside the chat on wide terminals (about 144 columns or more when it opens by itself).
2. **In a separate terminal tab**: run `meanwhile` in a split pane or a second tab.
3. **Both** (the default). Your progress is shared, so you can switch between them freely.

Run `meanwhile setup` again to change your choice (`--mode panel|tab|both` skips the question). `meanwhile doctor` checks that everything is connected.

## How it works

1. **Claude Code rings a doorbell.** At certain moments (you sent a prompt, Claude finished, Claude needs permission), Claude Code runs `meanwhile hook`. The hook gets a short note saying which session and folder it is, writes one line to a local log file, and exits. That takes about 4 ms. It prints nothing and never changes what Claude does. Your prompt text is never stored.
2. **The app watches the log.** The `meanwhile` app in your split pane reads each new line and tracks Claude's state for each session: working, waiting for you, or done.
3. **Lessons only while Claude is working.** After 4 seconds of work (so quick answers never flash a card), a lesson appears. If Claude stops to ask for permission or a question, the lesson hides and you see **⚠ Claude needs your input**, because that's your cue to go back. When Claude finishes, a banner says **✔ Claude finished**, and you can finish the card or close it.

The panel inside Claude Code gets these moments straight from Claude Code instead, including Esc, which it shows the instant you press it (**■ Claude stopped**). Its buttons (Back, Next, I know this, Pick another, Close) work with a click.

In the terminal app, pressing Esc or Ctrl+C in Claude doesn't trigger any hook, so `meanwhile` works it out a little later. It notices when you send your next prompt, or when Claude reports it has gone idle about a minute after. You can also press `x` to say Claude is done.

## Using it

| Key | What it does |
|---|---|
| `enter` | Continue where you left off, open a card, next card |
| `p` / `b` | Pick another concept / browse all concepts |
| `i` | "I know this": mark a concept done and unlock what builds on it |
| `d` | Go deeper: ask Claude to explain the card further (see below) |
| `D` | Saved deeper explanations for this concept |
| `s` | Skip this wait (no penalty, nothing recorded as missed) |
| `x` | Close the lesson, or tell meanwhile Claude is done |
| `q` | Quit |

The picker suggests up to five concepts you're ready for: concepts whose prerequisites you've done, most useful first. You can open any concept. If it builds on something you haven't done, you get a one-line note and can carry on anyway.

`meanwhile stats` shows your total wait time, concepts completed and cards read.

### Go deeper

On any card, press `d` to have Claude explain it in more depth, matched to what you've already learned. You can ask up to two follow-up questions. The first time, `meanwhile` explains what this does and asks which model to use.

- It runs **your own** installed Claude Code (`claude -p`) with your subscription or API usage. `meanwhile` never sees or stores any key.
- It runs in an empty folder with **every tool switched off**, so it can't read or change your files.
- Answers are saved locally against the concept.

## Configuration

Everything works without a config file. To change defaults, create `~/.config/meanwhile/config.toml`:

```toml
min_wait_seconds = 4          # how long Claude must work before a lesson appears
still_working_minutes = 2     # ask "still working?" after this long with no sign of Claude
bell = "needs_input"          # ring the terminal bell: "needs_input", "always" or "never"
ignore_dirs = ["~/ci"]        # never wake for Claude runs started in these folders
content_dir = "~/my-lessons"  # load concept files from here (for writing lessons)

[deeper]
model = "haiku"               # model for Go deeper; overrides the choice made on first use
```

Runs started by scripts (`claude -p`) are recognised and never wake the app.

## Privacy

Everything stays on your machine:

- **Log of Claude events:** `~/.local/state/meanwhile/spool/`. Session ID, folder and timestamps; never your prompt. Files are deleted after 7 days.
- **Progress, wait history and saved answers:** `~/.local/state/meanwhile/state.db`, with a daily backup next to it.
- **Settings:** `~/.config/meanwhile/`.

There is no telemetry. The only network traffic is the "Go deeper" call you start yourself, which goes through your own `claude`.

## Uninstall

```sh
meanwhile uninstall           # removes only meanwhile's hooks (even if you've edited the file since) and the panel
meanwhile uninstall --purge   # also deletes progress and history
```

Then remove the binary (`brew uninstall --cask meanwhile`, or delete it).

## Writing lessons

Lessons are plain Markdown files in [`content/concepts/`](content/concepts), one file per concept with 3 to 5 cards. See [docs/content-format.md](docs/content-format.md) and [CONTRIBUTING.md](CONTRIBUTING.md). The current lessons are placeholders while the real curriculum is being written.

## Development

```sh
go test -race ./...
go run ./cmd/meanwhile content check
```

Try it without touching your real setup:

```sh
export MEANWHILE_HOME=$(mktemp -d)       # keeps all meanwhile data in a temp folder
go build -o /tmp/mw ./cmd/meanwhile
/tmp/mw                                  # in one terminal
/tmp/mw simulate start                   # in another: pretend Claude started working
/tmp/mw simulate input | resume | end    # needs input, working again, finished
```

The architecture, from hooks to activities, is described in [docs/plan.md](docs/plan.md). Behaviour verified against real Claude Code is in [docs/claude-code-behaviour.md](docs/claude-code-behaviour.md).

## License

Code: [MIT](LICENSE). Lesson content: [CC BY 4.0](content/LICENSE).
