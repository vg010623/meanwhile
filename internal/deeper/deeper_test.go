package deeper

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeClaude writes a shell script that stands in for `claude`. It records
// its arguments, stdin and MEANWHILE_INTERNAL, then runs body.
func fakeClaude(t *testing.T, body string) (bin, dir string) {
	t.Helper()
	dir = t.TempDir()
	bin = filepath.Join(dir, "claude")
	script := "#!/bin/sh\n" +
		`printf '%s\n' "$@" > "` + dir + `/args"` + "\n" +
		`cat > "` + dir + `/stdin"` + "\n" +
		`echo "$MEANWHILE_INTERNAL" > "` + dir + `/env"` + "\n" +
		body + "\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, dir
}

func collect(t *testing.T, ch <-chan Event) (text string, last Event) {
	t.Helper()
	timeout := time.After(10 * time.Second)
	var sb strings.Builder
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return sb.String(), last
			}
			if ev.Kind == Text {
				sb.WriteString(ev.Text)
			}
			last = ev
		case <-timeout:
			t.Fatal("timed out")
		}
	}
}

func TestSuccessFromRecordedOutput(t *testing.T) {
	fixture, _ := filepath.Abs("../../testdata/fake-claude/success.jsonl")
	bin, dir := fakeClaude(t, `cat "`+fixture+`"`)
	r := Runner{Bin: bin, Dir: filepath.Join(dir, "sandbox")}
	text, last := collect(t, r.Ask(context.Background(), Request{System: "sys", Prompt: "explain tokens", Model: "haiku"}))

	if last.Kind != Done {
		t.Fatalf("last event %+v", last)
	}
	if !strings.Contains(text, "a token is a small unit of text") {
		t.Errorf("text: %q", text)
	}
	args, _ := os.ReadFile(filepath.Join(dir, "args"))
	for _, want := range []string{"--safe-mode", "--strict-mcp-config", "--no-session-persistence", "stream-json", "--include-partial-messages", "haiku", "sys"} {
		if !strings.Contains(string(args), want) {
			t.Errorf("missing arg %q in:\n%s", want, args)
		}
	}
	if !strings.Contains(string(args), "--tools\n\n") {
		t.Errorf("--tools \"\" not passed:\n%s", args)
	}
	if strings.Contains(string(args), "--bare") {
		t.Error("--bare breaks subscription logins")
	}
	stdin, _ := os.ReadFile(filepath.Join(dir, "stdin"))
	env, _ := os.ReadFile(filepath.Join(dir, "env"))
	if string(stdin) != "explain tokens" || strings.TrimSpace(string(env)) != "1" {
		t.Errorf("stdin=%q env=%q", stdin, env)
	}
}

func TestErrors(t *testing.T) {
	tests := []struct {
		name string
		body string
		want ErrKind
	}{
		{"not logged in", `echo '{"type":"result","subtype":"success","is_error":true,"result":"Not logged in · Please run /login"}'`, ErrNotLoggedIn},
		{"rate limited", `echo '{"type":"result","subtype":"success","is_error":true,"result":"Claude AI usage limit reached|1791200000"}'`, ErrRateLimited},
		{"offline", `echo 'API Error: Connection error. (getaddrinfo ENOTFOUND api.anthropic.com)' >&2; exit 1`, ErrOffline},
		{"outdated", `echo "error: unknown option '--safe-mode'" >&2; exit 1`, ErrOutdated},
		{"other", `echo 'something odd happened' >&2; exit 3`, ErrFailed},
		{"error subtype", `echo '{"type":"result","subtype":"error_during_execution","is_error":true,"result":""}'`, ErrFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bin, _ := fakeClaude(t, tt.body)
			_, last := collect(t, Runner{Bin: bin}.Ask(context.Background(), Request{}))
			if last.Kind != Failed || last.Err == nil || last.Err.Kind != tt.want {
				t.Fatalf("got %+v (err %+v)", last, last.Err)
			}
			if last.Err.Message() == "" {
				t.Error("empty message")
			}
		})
	}
}

func TestNotInstalled(t *testing.T) {
	_, last := collect(t, Runner{Bin: "/nonexistent/claude"}.Ask(context.Background(), Request{}))
	if last.Err == nil || last.Err.Kind != ErrNotInstalled {
		t.Fatalf("got %+v", last)
	}
}

func TestRetryEvent(t *testing.T) {
	bin, _ := fakeClaude(t, `
echo '{"type":"system","subtype":"api_retry","attempt":2,"max_retries":5,"retry_delay_ms":500,"error_status":529,"error":"overloaded"}'
echo '{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"ok"}}}'
echo '{"type":"result","subtype":"success","is_error":false,"result":"ok"}'`)
	var sawRetry bool
	for ev := range (Runner{Bin: bin}).Ask(context.Background(), Request{}) {
		if ev.Kind == Retry && ev.Attempt == 2 && ev.MaxAttempts == 5 {
			sawRetry = true
		}
	}
	if !sawRetry {
		t.Error("retry event missing")
	}
}

func TestAssistantMessageFallback(t *testing.T) {
	// Without partial deltas, the full assistant message is used once.
	bin, _ := fakeClaude(t, `
echo '{"type":"assistant","message":{"content":[{"type":"text","text":"whole answer"}]}}'
echo '{"type":"result","subtype":"success","is_error":false}'`)
	text, last := collect(t, Runner{Bin: bin}.Ask(context.Background(), Request{}))
	if text != "whole answer" || last.Kind != Done {
		t.Errorf("text=%q last=%+v", text, last)
	}
}

func TestCancel(t *testing.T) {
	bin, _ := fakeClaude(t, `
echo '{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"partial"}}}'
sleep 30`)
	ctx, cancel := context.WithCancel(context.Background())
	ch := Runner{Bin: bin}.Ask(ctx, Request{})
	if ev := <-ch; ev.Text != "partial" {
		t.Fatalf("first event %+v", ev)
	}
	start := time.Now()
	cancel()
	_, last := collect(t, ch)
	if last.Err == nil || last.Err.Kind != ErrCancelled {
		t.Errorf("got %+v", last)
	}
	if time.Since(start) > 5*time.Second {
		t.Error("cancel took too long")
	}
}

func TestFirstByteTimeout(t *testing.T) {
	bin, _ := fakeClaude(t, `sleep 30`)
	_, last := collect(t, Runner{Bin: bin, FirstByteTimeout: 200 * time.Millisecond}.Ask(context.Background(), Request{}))
	if last.Err == nil || last.Err.Kind != ErrOffline {
		t.Errorf("got %+v", last)
	}
}

func TestTotalTimeout(t *testing.T) {
	bin, _ := fakeClaude(t, `
echo '{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"x"}}}'
sleep 30`)
	_, last := collect(t, Runner{Bin: bin, Timeout: 300 * time.Millisecond}.Ask(context.Background(), Request{}))
	if last.Err == nil || last.Err.Kind != ErrTimeout {
		t.Errorf("got %+v", last)
	}
}
