package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"chatwright.dev/sdk"
)

// greetbotFixturePath is the same worked-example scenario document (and its
// checked-in cassette, resolved relative to it) the standard repository's
// self-contained-scenario-documents feature ships and runtime-go's own
// greetbot conformance test replays — copied here so `chatwright run`'s own
// tests exercise the real CLI end to end with zero network dependency,
// without this repository needing a cross-repository path into runtime-go.
const greetbotFixturePath = "testdata/greetbot-language-onboarding.json"

func TestRunRequiresDocument(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runRun(nil, &stdout, &stderr); code != 2 {
		t.Fatalf("runRun(nil) code = %d, want 2; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "missing DOCUMENT") {
		t.Errorf("stderr = %q, want a missing-DOCUMENT message", stderr.String())
	}
}

func TestRunRejectsUnknownDocument(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runRun([]string{filepath.Join(t.TempDir(), "does-not-exist.json")}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("runRun() code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "chatwright run:") {
		t.Errorf("stderr = %q, want a \"chatwright run:\" prefixed error", stderr.String())
	}
}

// TestRunEndToEndAgainstGreetbotFixture drives `chatwright run` against the
// real worked-example document — no manifest, no registered Go scenario,
// no network — and checks it writes a bundle that reads back with
// sdk.Read and reports the verified verdict the fixture's own cassette
// produces. The --out flag is deliberately given AFTER the document
// argument, exercising this command's documented usage
// ("chatwright run DOCUMENT [--out DIR]"), not just flag.FlagSet's own
// flags-first default order.
func TestRunEndToEndAgainstGreetbotFixture(t *testing.T) {
	outDir := t.TempDir()

	var stdout, stderr bytes.Buffer
	code := runRun([]string{greetbotFixturePath, "--out", outDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("runRun() code = %d, want 0; stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	if !strings.Contains(stdout.String(), "part status=completed") {
		t.Errorf("stdout = %q, want it to report a completed part", stdout.String())
	}
	if !strings.Contains(stdout.String(), "outcome=verified") {
		t.Errorf("stdout = %q, want it to report a verified outcome (not judged) — the document declares a verify block", stdout.String())
	}

	bundlePath := filepath.Join(outDir, "greetbot-language-onboarding.chatwright.json")
	f, err := os.Open(bundlePath)
	if err != nil {
		t.Fatalf("Open(%s) error = %v", bundlePath, err)
	}
	defer f.Close()

	b, err := sdk.Read(f)
	if err != nil {
		t.Fatalf("sdk.Read() error = %v", err)
	}
	if b.Format != sdk.FormatV1 {
		t.Errorf("Format = %q, want %q", b.Format, sdk.FormatV1)
	}
	if len(b.Runs) != 1 {
		t.Fatalf("len(Runs) = %d, want 1", len(b.Runs))
	}
	if b.Runs[0].ID != "greetbot-language-onboarding" {
		t.Errorf("Runs[0].ID = %q, want %q", b.Runs[0].ID, "greetbot-language-onboarding")
	}
	if b.Runs[0].Platform != "telegram" {
		t.Errorf("Runs[0].Platform = %q, want %q", b.Runs[0].Platform, "telegram")
	}
	if len(b.Runs[0].Parts) != 1 || b.Runs[0].Parts[0].AIGoal == nil {
		t.Fatalf("Runs[0].Parts = %+v, want exactly one ai-goal part", b.Runs[0].Parts)
	}
	if got := b.Runs[0].Parts[0].AIGoal.Report.Tasks[0].Status; got != "completed" {
		t.Errorf("task status = %q, want %q", got, "completed")
	}
}

// TestRunFlagsBeforeDocumentAlsoWorks proves the conventional flags-first
// ordering (Go's own flag.FlagSet default) still works, alongside the
// documented flags-after-DOCUMENT order TestRunEndToEndAgainstGreetbotFixture
// exercises.
func TestRunFlagsBeforeDocumentAlsoWorks(t *testing.T) {
	outDir := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := runRun([]string{"--out", outDir, greetbotFixturePath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("runRun() code = %d, want 0; stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(filepath.Join(outDir, "greetbot-language-onboarding.chatwright.json")); err != nil {
		t.Fatalf("bundle not written: %v", err)
	}
}

func TestRunRejectsExtraArguments(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runRun([]string{greetbotFixturePath, "extra-argument"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("runRun() code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "unexpected extra argument") {
		t.Errorf("stderr = %q, want an unexpected-extra-argument message", stderr.String())
	}
}
