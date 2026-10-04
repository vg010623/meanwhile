// Package deeper runs the user's own Claude Code in headless mode to explain
// a lesson in more depth, streaming the answer back.
//
// Safety: the call runs in an empty folder with every tool disabled, so the
// model cannot read or change the user's files. We never see or store an API
// key; Claude Code uses whatever login the user already has.
package deeper

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/vg010623/meanwhile/internal/hook"
)

// Request is one question.
type Request struct {
	System string // replaces Claude Code's coding-agent system prompt
	Prompt string // sent on stdin
	Model  string // "" uses the user's default model
}

// EventKind says what an Event carries.
type EventKind int

const (
	Text   EventKind = iota // more answer text
	Retry                   // Claude is retrying after a temporary error
	Done                    // the answer is complete
	Failed                  // the call failed; see Err
)

// Event is one update from a running call. The channel closes after a Done
// or Failed event.
type Event struct {
	Kind        EventKind
	Text        string
	Attempt     int
	MaxAttempts int
	Err         *Error
}

// ErrKind classifies failures so the UI can say something useful.
type ErrKind int

const (
	ErrFailed ErrKind = iota
	ErrNotInstalled
	ErrNotLoggedIn
	ErrRateLimited
	ErrOffline
	ErrTimeout
	ErrOutdated
	ErrCancelled
)

// Error is a classified failure.
type Error struct {
	Kind   ErrKind
	Detail string
}

func (e *Error) Error() string { return e.Message() }

// Message is the short line shown to the user.
func (e *Error) Message() string {
	switch e.Kind {
	case ErrNotInstalled:
		return "Claude Code isn't installed (no `claude` command found)."
	case ErrNotLoggedIn:
		return "Claude Code isn't logged in. Run `claude` once and log in."
	case ErrRateLimited:
		return "You've hit your Claude usage limit. Try again later."
	case ErrOffline:
		return "Couldn't reach Claude. Check your connection."
	case ErrTimeout:
		return "Claude took too long to answer."
	case ErrOutdated:
		return "Your Claude Code is too old for this. Update it and try again."
	case ErrCancelled:
		return "Cancelled."
	default:
		if e.Detail != "" {
			return "Claude couldn't answer: " + e.Detail
		}
		return "Claude couldn't answer."
	}
}

// Runner starts headless Claude Code calls.
type Runner struct {
	Bin              string        // default "claude"
	Dir              string        // empty sandbox folder used as the working directory
	FirstByteTimeout time.Duration // default 30 s
	Timeout          time.Duration // default 120 s
}

// Args are the command-line flags for a request. Each one matters:
//   - --tools "" removes every built-in tool: no reading, writing or running.
//   - --strict-mcp-config with no --mcp-config loads no MCP servers.
//   - --safe-mode skips hooks (so this call doesn't look like a user wait),
//     CLAUDE.md, plugins and skills, but keeps the user's login. (--bare
//     would also do that, but it ignores subscription logins.)
//   - --no-session-persistence keeps these calls out of the user's /resume list.
//   - stream-json with partial messages streams text as it is generated.
//   - --system-prompt replaces the big coding-agent prompt: cheaper, on topic.
func (r Runner) Args(req Request) []string {
	args := []string{
		"-p",
		"--safe-mode",
		"--tools", "",
		"--strict-mcp-config",
		"--no-session-persistence",
		"--output-format", "stream-json",
		"--verbose",
		"--include-partial-messages",
		"--max-turns", "1",
		"--system-prompt", req.System,
	}
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	return args
}

// Ask starts a call. Cancelling ctx stops it (the process gets SIGINT, then
// is killed if it hasn't exited within two seconds).
func (r Runner) Ask(ctx context.Context, req Request) <-chan Event {
	out := make(chan Event, 16)
	go func() {
		defer close(out)
		if err := r.run(ctx, req, out); err != nil {
			out <- Event{Kind: Failed, Err: err}
		}
	}()
	return out
}

func (r Runner) run(parent context.Context, req Request, out chan<- Event) *Error {
	bin := r.Bin
	if bin == "" {
		bin = "claude"
	}
	path, err := exec.LookPath(bin)
	if err != nil {
		return &Error{Kind: ErrNotInstalled}
	}
	if r.Dir != "" {
		if err := os.MkdirAll(r.Dir, 0o700); err != nil {
			return &Error{Detail: err.Error()}
		}
	}
	firstByte, total := r.FirstByteTimeout, r.Timeout
	if firstByte == 0 {
		firstByte = 30 * time.Second
	}
	if total == 0 {
		total = 120 * time.Second
	}

	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	var (
		mu     sync.Mutex
		reason *Error // why we cancelled, if we did
	)
	stop := func(e *Error) {
		mu.Lock()
		if reason == nil {
			reason = e
		}
		mu.Unlock()
		cancel()
	}
	firstTimer := time.AfterFunc(firstByte, func() { stop(&Error{Kind: ErrOffline}) })
	totalTimer := time.AfterFunc(total, func() { stop(&Error{Kind: ErrTimeout}) })
	defer firstTimer.Stop()
	defer totalTimer.Stop()

	cmd := exec.CommandContext(ctx, path, r.Args(req)...)
	cmd.Dir = r.Dir
	cmd.Env = append(os.Environ(), hook.InternalEnv+"=1")
	cmd.Stdin = strings.NewReader(req.Prompt)
	var stderr bytes.Buffer
	cmd.Stderr = &limitWriter{w: &stderr, n: 8 << 10}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // own process group, so we can stop it cleanly
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGINT) }
	cmd.WaitDelay = 2 * time.Second
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return &Error{Detail: err.Error()}
	}
	if err := cmd.Start(); err != nil {
		return &Error{Detail: err.Error()}
	}

	var (
		sawDelta  bool
		result    *Error
		gotResult bool
	)
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		firstTimer.Stop()
		ev, isResult, resErr := parseLine(sc.Bytes(), &sawDelta)
		if isResult {
			gotResult, result = true, resErr
			continue
		}
		if ev != nil {
			select {
			case out <- *ev:
			case <-ctx.Done():
			}
		}
	}
	waitErr := cmd.Wait()

	mu.Lock()
	why := reason
	mu.Unlock()
	switch {
	case parent.Err() != nil:
		return &Error{Kind: ErrCancelled}
	case why != nil:
		return why
	case gotResult && result != nil:
		return result
	case gotResult:
		out <- Event{Kind: Done}
		return nil
	case waitErr != nil:
		return classify(stderr.String() + " " + waitErr.Error())
	default:
		return classify(stderr.String())
	}
}

// streamLine holds the fields we read from Claude Code's stream-json output.
type streamLine struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	Event   *struct {
		Type  string `json:"type"`
		Delta *struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"delta"`
	} `json:"event"`
	Message *struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"message"`
	IsError    bool            `json:"is_error"`
	Result     string          `json:"result"`
	Attempt    int             `json:"attempt"`
	MaxRetries int             `json:"max_retries"`
	Error      json.RawMessage `json:"error"`
	Errors     json.RawMessage `json:"errors"`
}

// parseLine turns one stream-json line into an event. Text arrives as
// partial "stream_event" deltas; the full "assistant" message is used only
// if no deltas came (older versions, or partial messages unsupported).
func parseLine(b []byte, sawDelta *bool) (ev *Event, isResult bool, resErr *Error) {
	var l streamLine
	if json.Unmarshal(b, &l) != nil {
		return nil, false, nil
	}
	switch l.Type {
	case "stream_event":
		if l.Event != nil && l.Event.Type == "content_block_delta" && l.Event.Delta != nil && l.Event.Delta.Type == "text_delta" {
			*sawDelta = true
			return &Event{Kind: Text, Text: l.Event.Delta.Text}, false, nil
		}
	case "assistant":
		if !*sawDelta && l.Message != nil {
			var sb strings.Builder
			for _, c := range l.Message.Content {
				if c.Type == "text" {
					sb.WriteString(c.Text)
				}
			}
			if sb.Len() > 0 {
				return &Event{Kind: Text, Text: sb.String()}, false, nil
			}
		}
	case "system":
		if l.Subtype == "api_retry" {
			return &Event{Kind: Retry, Attempt: l.Attempt, MaxAttempts: l.MaxRetries}, false, nil
		}
	case "result":
		if l.IsError || strings.HasPrefix(l.Subtype, "error") {
			return nil, true, classify(l.Result + " " + string(l.Error) + " " + string(l.Errors) + " " + l.Subtype)
		}
		return nil, true, nil
	}
	return nil, false, nil
}

// classify maps error text from Claude Code to an error kind.
func classify(text string) *Error {
	t := strings.ToLower(text)
	has := func(words ...string) bool {
		for _, w := range words {
			if strings.Contains(t, w) {
				return true
			}
		}
		return false
	}
	switch {
	case has("unknown option", "unknown argument", "unrecognized option"):
		return &Error{Kind: ErrOutdated}
	case has("not logged in", "/login", "please log in", "authentication", "unauthorized", "invalid api key", "oauth", "401"):
		return &Error{Kind: ErrNotLoggedIn}
	case has("rate limit", "rate_limit", "usage limit", "limit reached", "429", "quota"):
		return &Error{Kind: ErrRateLimited}
	case has("enotfound", "econnrefused", "econnreset", "etimedout", "getaddrinfo", "fetch failed", "network", "connection error", "offline"):
		return &Error{Kind: ErrOffline}
	}
	detail := strings.TrimSpace(text)
	if i := strings.IndexByte(detail, '\n'); i >= 0 {
		detail = detail[:i]
	}
	if len(detail) > 160 {
		detail = detail[:160] + "…"
	}
	return &Error{Kind: ErrFailed, Detail: detail}
}

// limitWriter keeps only the first n bytes written.
type limitWriter struct {
	w interface{ Write([]byte) (int, error) }
	n int
}

func (l *limitWriter) Write(p []byte) (int, error) {
	if l.n > 0 {
		k := min(len(p), l.n)
		l.w.Write(p[:k])
		l.n -= k
	}
	return len(p), nil
}

// ErrIsKind reports whether err is an *Error of kind k.
func ErrIsKind(err error, k ErrKind) bool {
	var e *Error
	return errors.As(err, &e) && e.Kind == k
}
