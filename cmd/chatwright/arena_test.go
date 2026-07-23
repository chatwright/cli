package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// exampleConfigYAML is a small, valid arena.yaml — enough to exercise
// config parsing/validation without touching a real network or CLI tool
// (arena run in these tests always targets an unreachable BaseURL, so it
// exercises the provider-construction/degrade paths, never a live call).
const exampleConfigYAML = `
repeats: 2
hardware: "Test Rig"
budgets:
  maxSteps: 6
  maxDuration: 30s
providers:
  - kind: openai-compat
    label: fake/model
    baseUrl: http://127.0.0.1:1
    model: fake-model
    contextLength: 4096
`

func TestRunArenaHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runArena([]string{"help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("runArena(help) code = %d, want 0; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "arena run") {
		t.Errorf("help output = %q, want it to mention 'arena run'", stdout.String())
	}
}

func TestRunArenaUnknownSubcommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runArena([]string{"bogus"}, &stdout, &stderr); code != 2 {
		t.Fatalf("runArena(bogus) code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "unknown subcommand") {
		t.Errorf("stderr = %q, want an unknown-subcommand message", stderr.String())
	}
}

func TestRunArenaRunRequiresFlags(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runArenaRun(nil, &stdout, &stderr); code != 2 {
		t.Fatalf("runArenaRun(nil) code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "--config and --out are both required") {
		t.Errorf("stderr = %q, want the required-flags message", stderr.String())
	}
}

func TestRunArenaReportRequiresDir(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runArenaReport(nil, &stdout, &stderr); code != 2 {
		t.Fatalf("runArenaReport(nil) code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "--dir is required") {
		t.Errorf("stderr = %q, want the required-flag message", stderr.String())
	}
}

// TestArenaRunThenReportRoundTrip drives `chatwright arena run` end to
// end against the built-in greetbot scenario with a deliberately
// unreachable provider BaseURL (127.0.0.1:1 — nothing listens there), so
// the whole matrix executes with zero network dependency and produces a
// deterministic "every call failed" outcome: warm-up errors, the cell's
// campaign runs to a stop with transport errors recorded, and — critically
// — Run still returns a complete Results and never a top-level error (the
// spec's exclusion policy: a failing provider is a recorded data point).
// It then proves `arena report` recomputes an identical report.md from
// the directory `arena run` wrote, without touching the network again.
func TestArenaRunThenReportRoundTrip(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "arena.yaml")
	if err := os.WriteFile(configPath, []byte(exampleConfigYAML), 0o644); err != nil {
		t.Fatalf("WriteFile(config) error = %v", err)
	}
	outDir := filepath.Join(dir, "out")

	var runStdout, runStderr bytes.Buffer
	code := runArenaRun([]string{"--config", configPath, "--out", outDir}, &runStdout, &runStderr)
	if code != 0 {
		t.Fatalf("runArenaRun() code = %d, want 0; stdout=%q stderr=%q", code, runStdout.String(), runStderr.String())
	}

	reportPath := filepath.Join(outDir, "report.md")
	firstReport, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("ReadFile(report.md) error = %v", err)
	}
	if !strings.Contains(string(firstReport), "fake/model") {
		t.Errorf("report.md does not mention the configured provider label:\n%s", firstReport)
	}
	if !strings.Contains(string(firstReport), "Test Rig") {
		t.Errorf("report.md does not carry the configured Hardware label:\n%s", firstReport)
	}

	resultsPath := filepath.Join(outDir, resultsFileName)
	if _, err := os.Stat(resultsPath); err != nil {
		t.Fatalf("results.json was not written: %v", err)
	}

	// Now recompute the report from results.json alone, via `arena
	// report`, and prove it renders the same content — never re-running
	// any model (there is nothing listening on 127.0.0.1:1; a second
	// network attempt would hang/fail this test if `arena report`
	// mistakenly re-ran the matrix instead of just reading results.json).
	if err := os.Remove(reportPath); err != nil {
		t.Fatalf("Remove(report.md) error = %v", err)
	}
	var reportStdout, reportStderr bytes.Buffer
	code = runArenaReport([]string{"--dir", outDir}, &reportStdout, &reportStderr)
	if code != 0 {
		t.Fatalf("runArenaReport() code = %d, want 0; stdout=%q stderr=%q", code, reportStdout.String(), reportStderr.String())
	}

	secondReport, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("ReadFile(report.md) after 'arena report' error = %v", err)
	}
	if string(firstReport) != string(secondReport) {
		t.Fatalf("'arena report' produced a different report.md than 'arena run' did:\nfirst:\n%s\nsecond:\n%s", firstReport, secondReport)
	}
}

func TestLoadArenaConfigRejectsUnknownProviderKind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "arena.yaml")
	if err := os.WriteFile(path, []byte("repeats: 1\nproviders:\n  - kind: bogus\n    baseUrl: http://x\n    model: m\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	cfg, err := loadArenaConfig(path)
	if err != nil {
		t.Fatalf("loadArenaConfig() error = %v", err)
	}
	if _, _, err := cfg.toMatrix(); err == nil {
		t.Fatal("toMatrix() error = nil, want an unknown-provider-kind error")
	} else if !strings.Contains(err.Error(), "unknown provider kind") {
		t.Errorf("toMatrix() error = %v, want it to mention the unknown kind", err)
	}
}

// TestExampleConfigParsesAndBuildsAMatrix guards arena.example.yaml — the
// example documenting the founder's own line-up (spec/ideas/
// actor-model-arena.md's MVP scope) — against silently drifting out of
// sync with arenaConfig's own shape.
func TestExampleConfigParsesAndBuildsAMatrix(t *testing.T) {
	cfg, err := loadArenaConfig(filepath.Join("..", "..", "arena.example.yaml"))
	if err != nil {
		t.Fatalf("loadArenaConfig(arena.example.yaml) error = %v", err)
	}
	matrix, opts, err := cfg.toMatrix()
	if err != nil {
		t.Fatalf("toMatrix() error = %v", err)
	}
	if matrix.Repeats != 3 {
		t.Errorf("Repeats = %d, want 3", matrix.Repeats)
	}
	if len(matrix.Providers) != 4 {
		t.Fatalf("len(Providers) = %d, want 4 (ollama qwen3.6 + 3 LM Studio models)", len(matrix.Providers))
	}
	for _, p := range matrix.Providers {
		if p.ContextLength <= 0 {
			t.Errorf("provider %s has no right-sized ContextLength", p.Label)
		}
	}
	if opts.Hardware == "" {
		t.Error("RunOptions.Hardware is empty, want the example's declared hardware label")
	}
}

func TestLoadArenaConfigRejectsUnknownScenario(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "arena.yaml")
	if err := os.WriteFile(path, []byte("scenario: not-a-real-scenario\nrepeats: 1\nproviders:\n  - kind: ollama\n    baseUrl: http://x\n    model: m\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	cfg, err := loadArenaConfig(path)
	if err != nil {
		t.Fatalf("loadArenaConfig() error = %v", err)
	}
	if _, _, err := cfg.toMatrix(); err == nil {
		t.Fatal("toMatrix() error = nil, want an unknown-scenario error")
	} else if !strings.Contains(err.Error(), "unknown scenario") {
		t.Errorf("toMatrix() error = %v, want it to mention the unknown scenario", err)
	}
}
