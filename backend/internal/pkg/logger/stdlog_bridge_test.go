package logger

import (
	"io"
	"log"
	"os"
	"strings"
	"testing"
)

func TestInferStdLogLevel(t *testing.T) {
	cases := []struct {
		msg  string
		want Level
	}{
		{msg: "Warning: queue full", want: LevelWarn},
		{msg: "Forward request failed: timeout", want: LevelError},
		{msg: "[ERROR] upstream unavailable", want: LevelError},
		{msg: "[OpenAI WS Mode] reconnect_retry account_id=22 retry=1 max_retries=5", want: LevelInfo},
		{msg: "service started", want: LevelInfo},
		{msg: "debug: cache miss", want: LevelDebug},
	}

	for _, tc := range cases {
		got := inferStdLogLevel(tc.msg)
		if got != tc.want {
			t.Fatalf("inferStdLogLevel(%q)=%v want=%v", tc.msg, got, tc.want)
		}
	}
}

func TestNormalizeStdLogMessage(t *testing.T) {
	raw := "  [TokenRefresh]  cycle complete \n total=1   failed=0 \n"
	got := normalizeStdLogMessage(raw)
	want := "[TokenRefresh] cycle complete total=1 failed=0"
	if got != want {
		t.Fatalf("normalizeStdLogMessage()=%q want=%q", got, want)
	}
}

func TestStdLogBridgeRoutesLevels(t *testing.T) {
	origStdout := os.Stdout
	origStderr := os.Stderr
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stderr pipe: %v", err)
	}
	os.Stdout = stdoutW
	os.Stderr = stderrW
	t.Cleanup(func() {
		os.Stdout = origStdout
		os.Stderr = origStderr
		_ = stdoutR.Close()
		_ = stdoutW.Close()
		_ = stderrR.Close()
		_ = stderrW.Close()
	})

	if err := Init(InitOptions{
		Level:       "debug",
		Format:      "json",
		ServiceName: "sub2api",
		Environment: "test",
		Output: OutputOptions{
			ToStdout: true,
			ToFile:   false,
		},
		Sampling: SamplingOptions{Enabled: false},
	}); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	log.Printf("service started")
	log.Printf("Warning: queue full")
	log.Printf("Forward request failed: timeout")
	// Skip Sync() — on Windows, fsync on pipes deadlocks (FlushFileBuffers).

	_ = stdoutW.Close()
	_ = stderrW.Close()
	stdoutBytes, _ := io.ReadAll(stdoutR)
	stderrBytes, _ := io.ReadAll(stderrR)
	stdoutText := string(stdoutBytes)
	stderrText := string(stderrBytes)

	if !strings.Contains(stdoutText, "service started") {
		t.Fatalf("stdout missing info log: %s", stdoutText)
	}
	if !strings.Contains(stderrText, "Warning: queue full") {
		t.Fatalf("stderr missing warn log: %s", stderrText)
	}
	if !strings.Contains(stderrText, "Forward request failed: timeout") {
		t.Fatalf("stderr missing error log: %s", stderrText)
	}
	if !strings.Contains(stderrText, "\"legacy_stdlog\":true") {
		t.Fatalf("stderr missing legacy_stdlog marker: %s", stderrText)
	}
}

func TestLegacyPrintfRoutesLevels(t *testing.T) {
	origStdout := os.Stdout
	origStderr := os.Stderr
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stderr pipe: %v", err)
	}
	os.Stdout = stdoutW
	os.Stderr = stderrW
	t.Cleanup(func() {
		os.Stdout = origStdout
		os.Stderr = origStderr
		_ = stdoutR.Close()
		_ = stdoutW.Close()
		_ = stderrR.Close()
		_ = stderrW.Close()
	})

	if err := Init(InitOptions{
		Level:       "debug",
		Format:      "json",
		ServiceName: "sub2api",
		Environment: "test",
		Output: OutputOptions{
			ToStdout: true,
			ToFile:   false,
		},
		Sampling: SamplingOptions{Enabled: false},
	}); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	LegacyPrintf("service.test", "request started")
	LegacyPrintf("service.test", "Warning: queue full")
	LegacyPrintf("service.test", "forward failed: timeout")
	// Skip Sync() — on Windows, fsync on pipes deadlocks (FlushFileBuffers).

	_ = stdoutW.Close()
	_ = stderrW.Close()
	stdoutBytes, _ := io.ReadAll(stdoutR)
	stderrBytes, _ := io.ReadAll(stderrR)
	stdoutText := string(stdoutBytes)
	stderrText := string(stderrBytes)

	if !strings.Contains(stdoutText, "request started") {
		t.Fatalf("stdout missing info log: %s", stdoutText)
	}
	if !strings.Contains(stderrText, "Warning: queue full") {
		t.Fatalf("stderr missing warn log: %s", stderrText)
	}
	if !strings.Contains(stderrText, "forward failed: timeout") {
		t.Fatalf("stderr missing error log: %s", stderrText)
	}
	if !strings.Contains(stderrText, "\"legacy_printf\":true") {
		t.Fatalf("stderr missing legacy_printf marker: %s", stderrText)
	}
	if !strings.Contains(stderrText, "\"component\":\"service.test\"") {
		t.Fatalf("stderr missing component field: %s", stderrText)
	}
}

func TestNormalizeStdLogMessageStripsControlAndANSI(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "ansi color sequence removed",
			raw:  "user=\x1b[31mmallory\x1b[0m logged in",
			want: "user=mallory logged in",
		},
		{
			name: "carriage return cannot forge a new line",
			raw:  "request ok\r[ERROR] fake entry injected",
			want: "request ok [ERROR] fake entry injected",
		},
		{
			name: "crlf and tab fold to single spaces",
			raw:  "a\r\nb\tc",
			want: "a b c",
		},
		{
			name: "nul and other C0 controls dropped",
			raw:  "a\x00b\x07c\x1fd",
			want: "abcd",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeStdLogMessage(tc.raw); got != tc.want {
				t.Fatalf("normalizeStdLogMessage(%q)=%q want=%q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestSanitizeLogField(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "plain passthrough", in: "hello world", want: "hello world"},
		{name: "utf8 passthrough", in: "模型 gpt-4o ✓", want: "模型 gpt-4o ✓"},
		{name: "csi color", in: "\x1b[31mred\x1b[0m", want: "red"},
		{name: "csi with params", in: "x\x1b[1;32;40my", want: "xy"},
		{name: "csi cursor movement", in: "\x1b[2J\x1b[Hcleared", want: "cleared"},
		{name: "osc title with BEL terminator", in: "\x1b]0;owned\x07after", want: "after"},
		{name: "osc with ST terminator", in: "\x1b]8;;http://x\x1b\\link", want: "link"},
		{name: "two byte escape", in: "\x1b(Btext", want: "text"},
		{name: "unterminated csi is dropped", in: "safe\x1b[31", want: "safe"},
		{name: "lone esc at end", in: "tail\x1b", want: "tail"},
		{name: "cr and lf become spaces", in: "a\rb\nc", want: "a b c"},
		{name: "tab becomes space", in: "k\tv", want: "k v"},
		{name: "vt and ff become spaces", in: "a\vb\fc", want: "a b c"},
		{name: "nul dropped", in: "a\x00b", want: "ab"},
		{name: "del dropped", in: "a\x7fb", want: "ab"},
		{name: "c1 control (8-bit CSI) dropped", in: "a\u009bb", want: "ab"},
		{name: "latin1 letters above c1 kept", in: "café", want: "café"},
		{name: "empty", in: "", want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SanitizeLogField(tc.in)
			if got != tc.want {
				t.Fatalf("SanitizeLogField(%q)=%q want=%q", tc.in, got, tc.want)
			}
			for _, r := range got {
				if r == 0x1b || (r < 0x20 && r != ' ') || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
					t.Fatalf("SanitizeLogField(%q) left control rune %U in %q", tc.in, r, got)
				}
			}
		})
	}
}
