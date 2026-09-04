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
	want := cliBuildInfo().Long()
	if lines[0] != want {
		t.Fatalf("run(version) first line = %q, want %q", lines[0], want)
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

func TestRunVersionFlag(t *testing.T) {
	want := cliBuildInfo().Short()

	for _, flag := range []string{"--version", "-v"} {
		var stdout bytes.Buffer
		var stderr bytes.Buffer

		if code := run([]string{flag}, &stdout, &stderr); code != 0 {
			t.Fatalf("run(%s) exit code = %d, want 0; stderr = %q", flag, code, stderr.String())
		}
		if got := stdout.String(); got != want+"\n" {
			t.Fatalf("run(%s) stdout = %q, want %q", flag, got, want+"\n")
		}
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

func TestRunWithoutArgumentsShowsRootHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(nil, &stdout, &stderr); code != 0 {
		t.Fatalf("run() code = %d, want 0; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Available Commands:") || !strings.Contains(stdout.String(), "self-update") {
		t.Fatalf("root help = %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "chatwright run example") {
		t.Fatalf("root help = %q, want the offline first-use example", stdout.String())
	}
}

func TestLegacyBareHelpFormsUsePublicRoot(t *testing.T) {
	for _, args := range [][]string{
		{"arena", "help"},
		{"server", "help"},
		{"completion", "help"},
		{"self-update", "help"},
		{"update", "help"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run(args, &stdout, &stderr); code != 0 {
				t.Fatalf("run(%q) code=%d, want 0; stderr=%q", args, code, stderr.String())
			}
			if !strings.Contains(stdout.String(), "Usage:") {
				t.Fatalf("run(%q) stdout=%q, want Cobra help", args, stdout.String())
			}
		})
	}
}
