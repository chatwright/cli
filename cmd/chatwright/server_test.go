package main

import (
	"bytes"
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
	for _, want := range []string{"server serve", "server start", "server stop", "server restart", "--addr", "--ui-dir", "--allow-origin"} {
		if !strings.Contains(got, want) {
			t.Errorf("help output missing %q:\n%s", want, got)
		}
	}
}

func TestRunServerUnknownSubcommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runServer([]string{"bogus"}, &stdout, &stderr); code != 2 {
		t.Fatalf("runServer(bogus) code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), `unknown subcommand "bogus"`) {
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
