package main

import (
	"bytes"
	"context"
	"io"
	"log"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunServerMissingSubcommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runServer(nil, &stdout, &stderr); code != 2 {
		t.Fatalf("runServer(nil) code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "missing subcommand") {
		t.Fatalf("stderr = %q, want a missing-subcommand message", stderr.String())
	}
}

func TestRunServerHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runServer([]string{"help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("runServer(help) code = %d, want 0", code)
	}
	got := stdout.String()
	for _, want := range []string{"serve", "start", "stop", "restart"} {
		if !strings.Contains(got, want) {
			t.Errorf("help output missing %q:\n%s", want, got)
		}
	}
	stdout.Reset()
	if code := runServerServe([]string{"--help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("serve help code=%d", code)
	}
	for _, want := range []string{"--addr", "--ui-dir", "--ui", "--ui-url", "--allow-origin"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("serve help missing %q:\n%s", want, stdout.String())
		}
	}
}

// --- resolveServeUIDir: --ui-dir/--ui precedence ---

func TestResolveServeUIDirPrefersExplicitUIDirOverUI(t *testing.T) {
	dir := t.TempDir()
	logger := log.New(io.Discard, "", 0)
	// A --ui-url that nothing listens on: if resolveServeUIDir tried to use
	// it, this would fail (or hang). It must not be attempted at all when
	// --ui-dir is set, per the "--ui-dir wins" precedence rule.
	got, err := resolveServeUIDir(context.Background(), dir, true, "http://127.0.0.1:1/", logger)
	if err != nil {
		t.Fatalf("resolveServeUIDir() error = %v, want --ui-dir to win without attempting any network call", err)
	}
	if got != dir {
		t.Fatalf("resolveServeUIDir() = %q, want %q", got, dir)
	}
}

func TestResolveServeUIDirNeitherFlagMeansNoUI(t *testing.T) {
	got, err := resolveServeUIDir(context.Background(), "", false, "", log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("resolveServeUIDir() error = %v", err)
	}
	if got != "" {
		t.Fatalf("resolveServeUIDir() = %q, want empty when neither --ui-dir nor --ui is set", got)
	}
}

func TestRunServerUnknownSubcommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runServer([]string{"bogus"}, &stdout, &stderr); code != 2 {
		t.Fatalf("runServer(bogus) code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), `unknown command "bogus"`) {
		t.Fatalf("stderr = %q, want an unknown-subcommand message", stderr.String())
	}
}

func TestRunServerServeRejectsUnknownFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runServerServe([]string{"--nope"}, &stdout, &stderr); code != 2 {
		t.Fatalf("runServerServe(--nope) code = %d, want 2", code)
	}
}

func TestRunServerStopWhenNotRunning(t *testing.T) {
	var stdout, stderr bytes.Buffer
	stateDir := t.TempDir()
	code := runServerStop([]string{"--state-dir", stateDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("runServerStop() on an empty state dir code = %d, want 0; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "not running") {
		t.Fatalf("stdout = %q, want a not-running message", stdout.String())
	}
}

// Note: runServerStart/runServerRestart are not exercised here with a real
// spawn — startDaemon re-execs os.Executable(), which under `go test` is
// this very test binary, and handing it plain CLI-style args ("server",
// "serve", ...) would make the testing package's own generated main treat
// them as unrecognized positional arguments and fall through to running
// the whole test suite again as an untracked background process. The
// daemon lifecycle primitives Start/Stop actually use (spawn, PID file,
// stale detection, SIGTERM, cleanup) are covered directly and safely in
// internal/server/daemon_test.go, using "sleep" as the stand-in child
// process instead of re-execing the test binary itself.

func TestEnvOrDefault(t *testing.T) {
	t.Setenv("CHATWRIGHT_TEST_ENV_OR_DEFAULT", "")
	if got := envOrDefault("CHATWRIGHT_TEST_ENV_OR_DEFAULT", "fallback"); got != "fallback" {
		t.Fatalf("envOrDefault() = %q, want fallback for an unset/empty env var", got)
	}
	t.Setenv("CHATWRIGHT_TEST_ENV_OR_DEFAULT", "from-env")
	if got := envOrDefault("CHATWRIGHT_TEST_ENV_OR_DEFAULT", "fallback"); got != "from-env" {
		t.Fatalf("envOrDefault() = %q, want from-env", got)
	}
}

func TestDefaultStateDirEndsInDotChatwright(t *testing.T) {
	got := defaultStateDir()
	if filepath.Base(got) != ".chatwright" {
		t.Fatalf("defaultStateDir() = %q, want it to end in .chatwright", got)
	}
}

func TestResolveAllowedOriginsMergesFlagAndEnv(t *testing.T) {
	t.Setenv("CHATWRIGHT_SERVER_ALLOW_ORIGIN", "https://a.test, https://b.test")
	got := resolveAllowedOrigins(repeatedFlag{"https://flag.test"})
	want := map[string]bool{"https://flag.test": true, "https://a.test": true, "https://b.test": true}
	if len(got) != len(want) {
		t.Fatalf("resolveAllowedOrigins() = %v, want %v", got, want)
	}
	for _, o := range got {
		if !want[o] {
			t.Errorf("unexpected origin %q in %v", o, got)
		}
	}
}
