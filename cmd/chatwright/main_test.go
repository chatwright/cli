package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunVersion(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if code := run([]string{"version"}, &stdout, &stderr); code != 0 {
		t.Fatalf("run(version) exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	got := stdout.String()
	lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	if lines[0] != "chatwright "+cliVersion() {
		t.Fatalf("run(version) first line = %q, want %q", lines[0], "chatwright "+cliVersion())
	}
	// The split's contract: version reports the resolved runtime and sdk
	// module versions from build info alongside the CLI's own.
	if !strings.Contains(got, "runtime: chatwright.dev/runtime ") {
		t.Fatalf("run(version) stdout = %q, want a resolved runtime version line", got)
	}
	if !strings.Contains(got, "sdk: chatwright.dev/sdk ") {
		t.Fatalf("run(version) stdout = %q, want a resolved sdk version line", got)
	}
	if !strings.Contains(got, "run-bundle format: https://chatwright.dev/formats/run-bundle/v1") {
		t.Fatalf("run(version) stdout = %q, want the run-bundle format line", got)
	}
}

func TestRunPlatforms(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if code := run([]string{"platforms"}, &stdout, &stderr); code != 0 {
		t.Fatalf("run(platforms) exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	got := stdout.String()
	// Names come from the linked-in runtime platforms themselves — this test
	// guards that the CLI genuinely fronts chatwright.dev/runtime rather
	// than restating a hardcoded list.
	if !strings.Contains(got, "telegram\ttext, inline actions, edits\n") {
		t.Fatalf("run(platforms) stdout = %q, want telegram line", got)
	}
	if !strings.Contains(got, "whatsapp\ttext (experimental)\n") {
		t.Fatalf("run(platforms) stdout = %q, want whatsapp line", got)
	}
}

func TestRunUnknownCommand(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if code := run([]string{"unknown"}, &stdout, &stderr); code != 2 {
		t.Fatalf("run(unknown) exit code = %d, want 2", code)
	}
	if got := stderr.String(); !strings.Contains(got, "unknown command") {
		t.Fatalf("run(unknown) stderr = %q, want unknown-command message", got)
	}
}
