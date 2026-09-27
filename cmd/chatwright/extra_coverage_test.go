package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	server "chatwright.dev/cli/internal/server"
	"chatwright.dev/cli/internal/term"
	"chatwright.dev/runtime/actor"
	"chatwright.dev/runtime/arena"
	"chatwright.dev/runtime/platform"
	runengine "chatwright.dev/runtime/run"
	"chatwright.dev/runtime/scenario"
	"chatwright.dev/sdk"
	"github.com/spf13/cobra"
	"github.com/strongo/cli-helpers/skillsync"
	skillscmd "github.com/strongo/cli-helpers/skillsync/cobracmd"
	"go.yaml.in/yaml/v4"
)

func TestMainAndRootCoverage(t *testing.T) {
	oldExit := exitFunc
	exitCode := -1
	exitFunc = func(code int) {
		exitCode = code
	}
	defer func() { exitFunc = oldExit }()

	oldArgs := os.Args
	os.Args = []string{"chatwright", "platforms"}
	defer func() { os.Args = oldArgs }()

	main()
	if exitCode != 0 {
		t.Errorf("main() exitCode = %d, want 0", exitCode)
	}

	// depVersion for non-existent path
	if v := depVersion("nonexistent.module/path"); v != "" {
		t.Errorf("depVersion(nonexistent) = %q, want empty", v)
	}

	// depVersion when readBuildInfo returns false
	oldReadBuildInfo := readBuildInfo
	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return nil, false
	}
	if v := depVersion(sdkModulePath); v != "" {
		t.Errorf("depVersion with no build info = %q, want empty", v)
	}
	readBuildInfo = oldReadBuildInfo

	// root command RunE
	root := newRootCommand(nil)
	if err := root.RunE(root, nil); err != nil {
		t.Errorf("root.RunE error = %v, want nil", err)
	}

	// root PreRunE without --version flag
	if err := root.PreRunE(root, nil); err != nil {
		t.Errorf("root.PreRunE error = %v, want nil", err)
	}
}

func TestCobraAndCompletionCoverage(t *testing.T) {
	// commandError Error() when err == nil
	ce := commandError{code: 1, err: nil}
	if ce.Error() != "command failed" {
		t.Errorf("commandError.Error() = %q, want 'command failed'", ce.Error())
	}

	// commandResult
	if err := commandResult(0); err != nil {
		t.Errorf("commandResult(0) = %v, want nil", err)
	}
	if err := commandResult(1); err == nil {
		t.Error("commandResult(1) = nil, want error")
	}

	// addLegacyHelpCommand
	parent := &cobra.Command{Use: "parent"}
	addLegacyHelpCommand(parent)
	var out bytes.Buffer
	parent.SetOut(&out)
	if err := executeCommand(parent, []string{"help"}, &out, &out); err != 0 {
		t.Errorf("executeCommand parent help = %d, want 0", err)
	}

	// generatedCompletion
	if s := generatedCompletion("unknown-shell"); s != "" {
		t.Errorf("generatedCompletion(unknown-shell) = %q, want empty", s)
	}
	if s := bashCompletionScript(); s == "" {
		t.Error("bashCompletionScript is empty")
	}
	if s := zshCompletionScript(); s == "" {
		t.Error("zshCompletionScript is empty")
	}
	if s := fishCompletionScript(); s == "" {
		t.Error("fishCompletionScript is empty")
	}
	if code := runCompletion([]string{"bash"}, &out, &out); code != 0 {
		t.Errorf("runCompletion bash code = %d, want 0", code)
	}
}

func TestExampleCoverage(t *testing.T) {
	// materializeExample into non-existent path
	if _, err := materializeExample("/nonexistent/directory/path/example"); err == nil {
		t.Error("materializeExample(nonexistent): want error, got nil")
	}

	// materializeExample when cassettes is blocked by a file
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "cassettes"), []byte("blocked"), 0o644)
	if _, err := materializeExample(dir); err == nil {
		t.Error("materializeExample blocked cassettes: want error, got nil")
	}

	// materializeExample when cassettePath itself is a directory
	dir2 := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir2, exampleCassetteRelPath), 0o755)
	if _, err := materializeExample(dir2); err == nil {
		t.Error("materializeExample with cassettePath as dir: want error, got nil")
	}

	// materializeExampleTemp when osMkdirTempFunc returns error
	oldMkdirTemp := osMkdirTempFunc
	osMkdirTempFunc = func(string, string) (string, error) {
		return "", errors.New("temp dir error")
	}
	if _, _, err := materializeExampleTemp(); err == nil {
		t.Error("materializeExampleTemp temp dir error: want error, got nil")
	}
	osMkdirTempFunc = oldMkdirTemp

	// materializeExampleTemp when materializeExampleInnerFunc returns error
	oldMat := materializeExampleInnerFunc
	materializeExampleInnerFunc = func(string) (string, error) {
		return "", errors.New("materialize error")
	}
	if _, _, err := materializeExampleTemp(); err == nil {
		t.Error("materializeExampleTemp inner error: want error, got nil")
	}
	materializeExampleInnerFunc = oldMat
}

func TestArenaCoverage(t *testing.T) {
	// yamlDuration UnmarshalYAML with invalid node type (e.g. sequence)
	var yd yamlDuration
	var nodeSeq yaml.Node
	_ = yaml.Unmarshal([]byte("[1, 2]"), &nodeSeq)
	if err := yd.UnmarshalYAML(nodeSeq.Content[0]); err == nil {
		t.Error("yamlDuration.UnmarshalYAML(seq): want error, got nil")
	}

	// yamlDuration UnmarshalYAML with invalid duration string
	var nodeStr yaml.Node
	_ = yaml.Unmarshal([]byte(`"not-a-duration"`), &nodeStr)
	if err := yd.UnmarshalYAML(&nodeStr); err == nil {
		t.Error("yamlDuration.UnmarshalYAML(bad-str): want error, got nil")
	}

	// loadArenaConfig missing file
	if _, err := loadArenaConfig("/nonexistent/arena.yaml"); err == nil {
		t.Error("loadArenaConfig(nonexistent): want error, got nil")
	}

	// loadArenaConfig invalid YAML
	badYAML := filepath.Join(t.TempDir(), "bad.yaml")
	_ = os.WriteFile(badYAML, []byte(": bad yaml :"), 0o644)
	if _, err := loadArenaConfig(badYAML); err == nil {
		t.Error("loadArenaConfig(bad yaml): want error, got nil")
	}

	// toMatrix edge cases
	cfg := arenaConfig{Scenario: "unknown-scenario"}
	if _, _, err := cfg.toMatrix(); err == nil {
		t.Error("toMatrix(unknown scenario): want error, got nil")
	}

	cfg = arenaConfig{Repeats: 0}
	if _, _, err := cfg.toMatrix(); err == nil {
		t.Error("toMatrix(repeats 0): want error, got nil")
	}

	cfg = arenaConfig{Repeats: 1, Providers: nil}
	if _, _, err := cfg.toMatrix(); err == nil {
		t.Error("toMatrix(empty providers): want error, got nil")
	}

	cfg = arenaConfig{Repeats: 1, Providers: []arenaProviderConfig{{Kind: "invalid-kind"}}}
	if _, _, err := cfg.toMatrix(); err == nil {
		t.Error("toMatrix(invalid kind): want error, got nil")
	}

	cfg = arenaConfig{Repeats: 1, Providers: []arenaProviderConfig{{Kind: "ollama", BaseURL: ""}}}
	if _, _, err := cfg.toMatrix(); err == nil {
		t.Error("toMatrix(empty baseURL): want error, got nil")
	}

	cfg = arenaConfig{Repeats: 1, Providers: []arenaProviderConfig{{Kind: "ollama", BaseURL: "http://localhost", Model: ""}}}
	if _, _, err := cfg.toMatrix(); err == nil {
		t.Error("toMatrix(empty model): want error, got nil")
	}

	// parseProviderKind
	if _, err := parseProviderKind("unknown"); err == nil {
		t.Error("parseProviderKind(unknown): want error, got nil")
	}

	// runArena with subcommands and root
	var out, errOut bytes.Buffer
	if code := runArena([]string{}, &out, &errOut); code != 2 {
		t.Errorf("runArena with no args = %d, want 2", code)
	}

	// arenaCmd RunE missing subcommand
	arenaCmd := newArenaCommand()
	arenaCmd.SetArgs([]string{})
	if err := arenaCmd.Execute(); err == nil {
		t.Error("arenaCmd without args: want error, got nil")
	}

	// executeArenaRun failures
	out.Reset()
	errOut.Reset()
	if code := executeArenaRun("/nonexistent/arena.yaml", t.TempDir(), &out, &errOut); code != 1 {
		t.Errorf("executeArenaRun missing config = %d, want 1", code)
	}

	// executeArenaRun invalid matrix
	badMatrixConfig := filepath.Join(t.TempDir(), "bad_matrix.yaml")
	_ = os.WriteFile(badMatrixConfig, []byte("repeats: 0\n"), 0o644)
	out.Reset()
	errOut.Reset()
	if code := executeArenaRun(badMatrixConfig, t.TempDir(), &out, &errOut); code != 1 {
		t.Errorf("executeArenaRun invalid matrix = %d, want 1", code)
	}

	// executeArenaRun arena.Run failure
	validConfig := filepath.Join(t.TempDir(), "valid.yaml")
	_ = os.WriteFile(validConfig, []byte("repeats: 1\nproviders:\n  - kind: ollama\n    baseUrl: http://localhost:11434\n    model: llama3\n"), 0o644)

	oldArenaRun := arenaRun
	arenaRun = func(context.Context, arena.Matrix, arena.RunOptions) (arena.Results, error) {
		return arena.Results{}, errors.New("simulated arena run error")
	}
	out.Reset()
	errOut.Reset()
	if code := executeArenaRun(validConfig, t.TempDir(), &out, &errOut); code != 1 {
		t.Errorf("executeArenaRun with arena.Run error = %d, want 1", code)
	}
	arenaRun = oldArenaRun

	// executeArenaRun writeArenaOutputs failure
	oldWriteOutputs := arenaWriteOutputs
	arenaWriteOutputs = func(string, arena.Results) error {
		return errors.New("simulated write output error")
	}
	out.Reset()
	errOut.Reset()
	if code := executeArenaRun(validConfig, t.TempDir(), &out, &errOut); code != 1 {
		t.Errorf("executeArenaRun with write outputs error = %d, want 1", code)
	}
	arenaWriteOutputs = oldWriteOutputs

	// executeArenaReport read error
	out.Reset()
	errOut.Reset()
	if code := executeArenaReport("/nonexistent/dir", &out, &errOut); code != 1 {
		t.Errorf("executeArenaReport nonexistent dir = %d, want 1", code)
	}

	// executeArenaReport with warnings and write report error
	reportDir := t.TempDir()
	validResults := arena.Results{
		Scenario: arena.ScenarioInfo{ID: "s1"},
		Models: []arena.ModelResult{
			{
				Spec: arena.ProviderSpec{Kind: arena.KindOllama, Model: "llama3"},
				Cells: []arena.CellResult{
					{Repeat: 1, BundleName: "missing.json"},
				},
			},
		},
	}
	_ = writeArenaOutputs(reportDir, validResults)

	oldArenaWriteReport := arenaWriteReport
	arenaWriteReport = func(io.Writer, arena.Results) error {
		return errors.New("simulated write report error")
	}
	out.Reset()
	errOut.Reset()
	if code := executeArenaReport(reportDir, &out, &errOut); code != 1 {
		t.Errorf("executeArenaReport with write report error = %d, want 1", code)
	}
	arenaWriteReport = oldArenaWriteReport

	// executeArenaReport with warnings
	oldArenaReadResults := arenaReadResults
	arenaReadResults = func(string) (arena.Results, []string, error) {
		return arena.Results{}, []string{"test warning"}, nil
	}
	out.Reset()
	errOut.Reset()
	_ = executeArenaReport(t.TempDir(), &out, &errOut)
	arenaReadResults = oldArenaReadResults

	// executeArenaReport create report.md error (report.md is a dir)
	blockedReportDir := t.TempDir()
	_ = writeArenaOutputs(blockedReportDir, validResults)
	_ = os.Remove(filepath.Join(blockedReportDir, "report.md"))
	_ = os.MkdirAll(filepath.Join(blockedReportDir, "report.md"), 0o755)
	out.Reset()
	errOut.Reset()
	if code := executeArenaReport(blockedReportDir, &out, &errOut); code != 1 {
		t.Errorf("executeArenaReport with blocked report.md = %d, want 1", code)
	}
}

func TestArenaResultsCoverage(t *testing.T) {
	// writeArenaOutputs when outDir is blocked by a file
	blockedOut := filepath.Join(t.TempDir(), "blocked")
	_ = os.WriteFile(blockedOut, []byte("file"), 0o644)
	if err := writeArenaOutputs(blockedOut, arena.Results{}); err == nil {
		t.Error("writeArenaOutputs(blocked): want error, got nil")
	}

	// writeArenaOutputs with cell errors and missing bundle name
	outDir := t.TempDir()
	res := arena.Results{
		Scenario: arena.ScenarioInfo{ID: "s1"},
		Models: []arena.ModelResult{
			{
				Spec:        arena.ProviderSpec{Kind: arena.KindOllama, Model: "m1", Label: "CustomLabel"},
				ProviderErr: errors.New("provider err"),
				Warmup: &arena.WarmupResult{
					ColdStart: 500 * time.Millisecond,
					Call:      arena.CallRecord{Mode: "warmup"},
					Err:       errors.New("warmup err"),
				},
				Cells: []arena.CellResult{
					{Repeat: 1, Err: errors.New("cell err")},
					{Repeat: 2, BundleName: ""},
					{
						Repeat: 3, BundleName: "bundle3.json",
						Bundle: sdk.Bundle{Format: sdk.FormatV1},
						Latencies: []time.Duration{100 * time.Millisecond},
						Calls: []arena.CallRecord{{Index: 1, Wall: 50 * time.Millisecond}},
					},
				},
			},
		},
	}

	if err := writeArenaOutputs(outDir, res); err != nil {
		t.Fatalf("writeArenaOutputs failed: %v", err)
	}

	// readArenaResults
	readRes, warnings, err := readArenaResults(outDir)
	if err != nil {
		t.Fatalf("readArenaResults failed: %v", err)
	}
	if len(readRes.Models) != 1 {
		t.Errorf("readRes.Models = %d, want 1", len(readRes.Models))
	}
	_ = warnings

	// readArenaResults with missing file
	if _, _, err := readArenaResults("/nonexistent"); err == nil {
		t.Error("readArenaResults(nonexistent): want error, got nil")
	}

	// readArenaResults with corrupted JSON
	corruptDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(corruptDir, resultsFileName), []byte("{corrupted"), 0o644)
	if _, _, err := readArenaResults(corruptDir); err == nil {
		t.Error("readArenaResults(corrupt): want error, got nil")
	}

	// toResults with missing bundle warning
	doc := toResultsDoc(res)
	doc.Models[0].Cells[2].BundleName = "nonexistent-bundle.json"
	_, warnings2 := doc.toResults(outDir)
	if len(warnings2) == 0 {
		t.Error("toResults with missing bundle: want warnings, got none")
	}

	// providerLabel with and without label
	if l := providerLabel(arena.ProviderSpec{Label: "MyLabel"}); l != "MyLabel" {
		t.Errorf("providerLabel(with label) = %q", l)
	}
	if l := providerLabel(arena.ProviderSpec{Kind: arena.KindOllama, Model: "llama3"}); l != "ollama/llama3" {
		t.Errorf("providerLabel(without label) = %q", l)
	}

	// writeArenaOutputs when report.md is blocked
	blockedReportDir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(blockedReportDir, "report.md"), 0o755)
	if err := writeArenaOutputs(blockedReportDir, res); err == nil {
		t.Error("writeArenaOutputs with report.md dir: want error, got nil")
	}

	// writeArenaOutputs when results.json is blocked
	blockedResultsDir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(blockedResultsDir, resultsFileName), 0o755)
	if err := writeArenaOutputs(blockedResultsDir, res); err == nil {
		t.Error("writeArenaOutputs with results.json dir: want error, got nil")
	}

	// writeArenaOutputs when create bundle file fails (bundle file is a dir)
	blockedBundleDir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(blockedBundleDir, "bundles", "bundle3.json"), 0o755)
	if err := writeArenaOutputs(blockedBundleDir, res); err == nil {
		t.Error("writeArenaOutputs with blocked bundle path: want error, got nil")
	}

	// writeArenaOutputs when sdkWriteBundle fails
	oldSdkWriteBundle := sdkWriteBundle
	sdkWriteBundle = func(io.Writer, sdk.Bundle) error {
		return errors.New("simulated sdk write bundle error")
	}
	if err := writeArenaOutputs(t.TempDir(), res); err == nil {
		t.Error("writeArenaOutputs with sdkWriteBundle error: want error, got nil")
	}
	sdkWriteBundle = oldSdkWriteBundle

	// writeArenaOutputs when arenaWriteReport fails
	oldArenaWriteReport := arenaWriteReport
	arenaWriteReport = func(io.Writer, arena.Results) error {
		return errors.New("simulated arenaWriteReport error")
	}
	if err := writeArenaOutputs(t.TempDir(), res); err == nil {
		t.Error("writeArenaOutputs with arenaWriteReport error: want error, got nil")
	}
	arenaWriteReport = oldArenaWriteReport

	// writeArenaOutputs when jsonMarshalIndent fails
	oldJSONMarshal := jsonMarshalIndent
	jsonMarshalIndent = func(any, string, string) ([]byte, error) {
		return nil, errors.New("simulated json marshal error")
	}
	if err := writeArenaOutputs(t.TempDir(), res); err == nil {
		t.Error("writeArenaOutputs with jsonMarshalIndent error: want error, got nil")
	}
	jsonMarshalIndent = oldJSONMarshal
}

func TestProgressAndRunOutputCoverage(t *testing.T) {
	rep := &progressReporter{
		out:       &bytes.Buffer{},
		profile:   term.Profile{ASCII: true, Color: true},
		startedAt: time.Now(),
		now:       time.Now,
	}

	// progressReporter render default
	if line, show := rep.render(runengine.ProgressSnapshot{Phase: "unknown-phase"}); show || line != "" {
		t.Errorf("render(unknown phase) = (%q, %v), want ('', false)", line, show)
	}

	// renderTask when snap.Task == nil
	if line, show := rep.renderTask(runengine.ProgressSnapshot{Phase: runengine.PartProgressTask, Task: nil}); show || line != "" {
		t.Errorf("renderTask(nil) = (%q, %v), want ('', false)", line, show)
	}

	// renderTask with NonProgressStreak > 0
	taskSnap := &actor.ProgressSnapshot{
		TaskID:            "t1",
		Phase:             actor.ProgressIteration,
		NonProgressStreak: 2,
		Iteration:         1,
	}
	rep.verbose = true
	line, show := rep.renderTask(runengine.ProgressSnapshot{
		Phase: runengine.PartProgressTask,
		Task:  taskSnap,
	})
	if !show || !strings.Contains(line, "non-progress 2") {
		t.Errorf("renderTask with streak = (%q, %v)", line, show)
	}

	// describeActed with all tones
	for _, k := range []actor.ActionOutcomeKind{
		actor.ActionExecuted,
		actor.ActionTaskCompleted,
		actor.ActionExecutedNoEffect,
		actor.ActionSkippedInvalid,
		actor.ActionResolutionFailed,
		actor.ActionTaskGivenUp,
		actor.ActionBlockedConstraintViolation,
		actor.ActionOvershootProbe,
		actor.ActionOutcomeKind("unknown-action-kind"),
	} {
		_ = rep.describeActed(k)
	}

	// sep and le in ASCII and non-ASCII
	repASCII := &progressReporter{profile: term.Profile{ASCII: true}}
	repUTF8 := &progressReporter{profile: term.Profile{ASCII: false}}
	if repASCII.sep() != " - " || repUTF8.sep() != " · " {
		t.Error("sep() unexpected")
	}
	if repASCII.le() != "<=" || repUTF8.le() != "≤" {
		t.Error("le() unexpected")
	}

	// runOutcome verdictTone
	if tone := (runOutcome{actorFailed: true}).verdictTone(); tone != toneBad {
		t.Errorf("verdictTone(actorFailed) = %v", tone)
	}
	if tone := (runOutcome{judged: true}).verdictTone(); tone != toneWarn {
		t.Errorf("verdictTone(judged) = %v", tone)
	}
	if tone := (runOutcome{verified: true}).verdictTone(); tone != toneGood {
		t.Errorf("verdictTone(verified) = %v", tone)
	}
	if tone := (runOutcome{}).verdictTone(); tone != toneBad {
		t.Errorf("verdictTone(default) = %v", tone)
	}

	// aggregateUsage with non-AIGoal part
	b := sdk.Bundle{
		Runs: []sdk.Run{
			{
				Parts: []sdk.Part{
					{AIGoal: nil},
					{
						AIGoal: &sdk.AIGoalSection{
							Report: sdk.Report{
								Usage: sdk.AggregateUsage{InputTokens: 10, OutputTokens: 20, Cost: 0.05, CallCount: 1},
							},
						},
					},
				},
			},
		},
	}
	usage := aggregateUsage(b)
	if usage.InputTokens != 10 || usage.CallCount != 1 {
		t.Errorf("aggregateUsage = %+v", usage)
	}

	// renderRunSummary with empty statusWord and detailSuffix empty
	s := renderRunSummary(term.Profile{ASCII: true, Color: true}, runOutcome{docID: "d1", partStatus: ""}, usage, time.Second, "")
	if !strings.Contains(s, "unknown") {
		t.Errorf("renderRunSummary with empty status = %s", s)
	}

	// midDot
	if m := midDot(term.Profile{ASCII: true}); m != " - " {
		t.Errorf("midDot(ASCII) = %q", m)
	}
	if m := midDot(term.Profile{ASCII: false}); m != " · " {
		t.Errorf("midDot(UTF8) = %q", m)
	}

	// colorTone
	p := term.Profile{Color: true}
	_ = colorTone(p, "test", toneGood)
	_ = colorTone(p, "test", toneWarn)
	_ = colorTone(p, "test", toneBad)
	_ = colorTone(p, "test", toneNeutral)
}

type errEmulator struct {
	platform.Emulator
}

func (e errEmulator) Journal(int64) ([]platform.JournalEntry, error) {
	return nil, errors.New("simulated emulator journal error")
}

func TestRunExecuteCoverage(t *testing.T) {
	// executeRun --write when outDir creation fails (outDir is a file)
	blockedOut := filepath.Join(t.TempDir(), "blocked")
	_ = os.WriteFile(blockedOut, []byte("file"), 0o644)
	var out, errOut bytes.Buffer
	if code := executeRun("example", runOptions{write: true, outDir: blockedOut}, &out, &errOut); code != 1 {
		t.Errorf("executeRun write blocked outDir code = %d, want 1", code)
	}

	// executeRun --write when materializeExample fails (outDir has cassettes file)
	blockedCassettesDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(blockedCassettesDir, "cassettes"), []byte("file"), 0o644)
	out.Reset()
	errOut.Reset()
	if code := executeRun("example", runOptions{write: true, outDir: blockedCassettesDir}, &out, &errOut); code != 1 {
		t.Errorf("executeRun write blocked cassettes code = %d, want 1", code)
	}

	// Run against document without verify block (judged)
	judgedDocPath := writeMutatedGreetbotFixture(t, func(doc map[string]any) {
		delete(doc, "verify")
	})
	out.Reset()
	errOut.Reset()
	if code := runRun([]string{judgedDocPath, "--out", t.TempDir()}, &out, &errOut); code != 0 {
		t.Errorf("runRun on judged doc code = %d, want 0; stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "verdict   judged") {
		t.Errorf("stdout = %s, want verdict judged", out.String())
	}

	// executeRun on "example" when materializeExampleTemp fails
	oldMatTemp := materializeExampleTempFunc
	materializeExampleTempFunc = func() (string, func(), error) {
		return "", nil, errors.New("simulated materialize temp error")
	}
	out.Reset()
	errOut.Reset()
	if code := executeRun("example", runOptions{outDir: t.TempDir()}, &out, &errOut); code != 1 {
		t.Errorf("executeRun example with materialize error code = %d, want 1", code)
	}
	materializeExampleTempFunc = oldMatTemp

	// executeRun when scenarioBuild fails
	oldScenarioBuild := scenarioBuild
	scenarioBuild = func(context.Context, *scenario.Document, scenario.BuildOptions) (*scenario.Built, error) {
		return nil, errors.New("simulated scenario build error")
	}
	out.Reset()
	errOut.Reset()
	if code := executeRun("example", runOptions{outDir: t.TempDir()}, &out, &errOut); code != 1 {
		t.Errorf("executeRun with scenario build error code = %d, want 1", code)
	}
	scenarioBuild = oldScenarioBuild

	// executeRun when built.Run.Execute fails
	oldScenarioBuildExecuteErr := scenarioBuild
	scenarioBuild = func(ctx context.Context, doc *scenario.Document, opts scenario.BuildOptions) (*scenario.Built, error) {
		built, err := oldScenarioBuildExecuteErr(ctx, doc, opts)
		if err != nil {
			return nil, err
		}
		built.Run.Environment.Emulator = nil
		return built, nil
	}
	out.Reset()
	errOut.Reset()
	if code := executeRun("example", runOptions{outDir: t.TempDir()}, &out, &errOut); code != 1 {
		t.Errorf("executeRun with Execute error code = %d, want 1", code)
	}
	scenarioBuild = oldScenarioBuildExecuteErr

	// executeRun when assembleRunBundleFunc fails
	oldAssembleBundle := assembleRunBundleFunc
	assembleRunBundleFunc = func(*scenario.Document, *scenario.Built, runengine.Result) (sdk.Bundle, runOutcome, error) {
		return sdk.Bundle{}, runOutcome{}, errors.New("simulated assemble error")
	}
	out.Reset()
	errOut.Reset()
	if code := executeRun("example", runOptions{outDir: t.TempDir()}, &out, &errOut); code != 1 {
		t.Errorf("executeRun with assemble bundle error code = %d, want 1", code)
	}
	assembleRunBundleFunc = oldAssembleBundle

	// executeRun when interrupted
	oldInterruptContext := interruptibleContextFunc
	interruptibleContextFunc = func(parent context.Context) (context.Context, func(), *atomic.Bool) {
		ctx, cancel := context.WithCancel(parent)
		fired := &atomic.Bool{}
		fired.Store(true)
		return ctx, cancel, fired
	}
	out.Reset()
	errOut.Reset()
	if code := executeRun("example", runOptions{outDir: t.TempDir()}, &out, &errOut); code != exitInterrupted {
		t.Errorf("executeRun with interrupt code = %d, want %d", code, exitInterrupted)
	}
	interruptibleContextFunc = oldInterruptContext

	// executeRun when writeRunJSONResultFunc fails
	oldWriteJSON := writeRunJSONResultFunc
	writeRunJSONResultFunc = func(io.Writer, runJSONResult) error {
		return errors.New("simulated json encode error")
	}
	out.Reset()
	errOut.Reset()
	if code := executeRun("example", runOptions{outDir: t.TempDir(), jsonOut: true}, &out, &errOut); code != 1 {
		t.Errorf("executeRun with json encode error code = %d, want 1", code)
	}
	writeRunJSONResultFunc = oldWriteJSON

	// executeRun when outDir creation for bundle fails (outDir is a file)
	blockedBundleOut := filepath.Join(t.TempDir(), "blocked")
	_ = os.WriteFile(blockedBundleOut, []byte("file"), 0o644)
	out.Reset()
	errOut.Reset()
	if code := executeRun("example", runOptions{outDir: blockedBundleOut}, &out, &errOut); code != 1 {
		t.Errorf("executeRun with blocked bundle outDir code = %d, want 1", code)
	}

	// executeRun when os.Create for bundle fails (bundlePath is a directory)
	bundlePathDir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(bundlePathDir, "greetbot-language-onboarding.chatwright.json"), 0o755)
	out.Reset()
	errOut.Reset()
	if code := executeRun("example", runOptions{outDir: bundlePathDir}, &out, &errOut); code != 1 {
		t.Errorf("executeRun with bundlePath as dir code = %d, want 1", code)
	}

	// executeRun when sdkWrite fails
	oldSdkWrite := sdkWrite
	sdkWrite = func(io.Writer, sdk.Bundle) error {
		return errors.New("simulated sdk write error")
	}
	out.Reset()
	errOut.Reset()
	if code := executeRun("example", runOptions{outDir: t.TempDir()}, &out, &errOut); code != 1 {
		t.Errorf("executeRun with sdkWrite error code = %d, want 1", code)
	}
	sdkWrite = oldSdkWrite

	// assembleRunBundle tests
	p := scenario.FileScenarioProvider{}
	greetbotDoc, _, err := p.Load(context.Background(), greetbotFixturePath)
	if err != nil {
		t.Fatalf("Load greetbot fixture failed: %v", err)
	}
	builtGreetbot, err := scenario.Build(context.Background(), greetbotDoc, scenario.BuildOptions{})
	if err != nil {
		t.Fatalf("Build greetbot fixture failed: %v", err)
	}
	defer builtGreetbot.Close()

	// assembleRunBundle with missing chat ID in built.ChatIDs
	builtDummy := &scenario.Built{
		ChatIDs:    map[string]int64{},
		VerifySpec: builtGreetbot.VerifySpec,
		Run:        builtGreetbot.Run,
	}
	if _, _, err := assembleRunBundle(greetbotDoc, builtDummy, runengine.Result{}); err == nil {
		t.Error("assembleRunBundle with missing chatDocID: want error, got nil")
	}

	// assembleRunBundle with Emulator.Journal error for chat
	docWithChats := &scenario.Document{
		ID:    "doc1",
		Chats: []scenario.Chat{{ID: "c1", PlatformChatID: 123}},
	}
	builtWithErrEmul := &scenario.Built{
		Run: runengine.Run{
			Environment: runengine.Environment{
				Emulator: errEmulator{},
			},
		},
	}
	if _, _, err := assembleRunBundle(docWithChats, builtWithErrEmul, runengine.Result{}); err == nil {
		t.Error("assembleRunBundle with Emulator.Journal error: want error, got nil")
	}

	// assembleRunBundle with Emulator.Journal error for verify
	docWithoutChats := &scenario.Document{ID: "doc1"}
	builtWithVerifyErrEmul := &scenario.Built{
		ChatIDs:    map[string]int64{builtGreetbot.VerifySpec.ChatDocID(): 123},
		VerifySpec: builtGreetbot.VerifySpec,
		Run: runengine.Run{
			Environment: runengine.Environment{
				Emulator: errEmulator{},
			},
		},
	}
	if _, _, err := assembleRunBundle(docWithoutChats, builtWithVerifyErrEmul, runengine.Result{}); err == nil {
		t.Error("assembleRunBundle with verify Emulator.Journal error: want error, got nil")
	}
}

func TestServerCmdCoverage(t *testing.T) {
	// envBoolOrDefault with true
	t.Setenv("TEST_BOOL_TRUE", "true")
	if b := envBoolOrDefault("TEST_BOOL_TRUE", false); !b {
		t.Errorf("envBoolOrDefault(true) = %v, want true", b)
	}

	// envBoolOrDefault with invalid
	t.Setenv("TEST_BOOL_INVALID", "invalid-bool")
	if b := envBoolOrDefault("TEST_BOOL_INVALID", true); !b {
		t.Errorf("envBoolOrDefault(invalid, true) = %v, want true", b)
	}

	// defaultStateDir with empty HOME
	t.Setenv("HOME", "")
	if d := defaultStateDir(); d != ".chatwright" {
		t.Errorf("defaultStateDir with empty HOME = %q, want .chatwright", d)
	}

	// resolveAllowedOrigins with comma separated CHATWRIGHT_SERVER_ALLOW_ORIGIN
	t.Setenv(envAllowOrigin, "http://a.com, http://b.com , ")
	origins := resolveAllowedOrigins([]string{"http://flag.com"})
	if len(origins) != 3 || origins[0] != "http://flag.com" || origins[1] != "http://a.com" || origins[2] != "http://b.com" {
		t.Errorf("resolveAllowedOrigins = %+v", origins)
	}

	// runServer
	var out, errOut bytes.Buffer
	if code := runServer([]string{}, &out, &errOut); code != 2 {
		t.Errorf("runServer with no args code = %d, want 2", code)
	}

	// executeServerStop with corrupt pid file in stateDir
	stateDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(stateDir, "server.pid"), []byte("corrupt-pid"), 0o644)
	out.Reset()
	errOut.Reset()
	if code := executeServerStop(stateDir, &out, &errOut); code != 1 {
		t.Errorf("executeServerStop with corrupt pid = %d, want 1", code)
	}

	// executeServerStop with successful stop (serverStop returns nil)
	oldServerStop := serverStop
	serverStop = func(string, time.Duration) error { return nil }
	out.Reset()
	errOut.Reset()
	if code := executeServerStop(stateDir, &out, &errOut); code != 0 {
		t.Errorf("executeServerStop success code = %d, want 0", code)
	}
	serverStop = oldServerStop

	// newServerCommand missing subcommand error
	serverCmd := newServerCommand()
	serverCmd.SetArgs([]string{})
	if err := serverCmd.Execute(); err == nil {
		t.Error("serverCmd.Execute with no args: want error, got nil")
	}

	// startDaemon failure creating stateDir (stateDir is blocked by file)
	blockedStateDir := filepath.Join(t.TempDir(), "blocked")
	_ = os.WriteFile(blockedStateDir, []byte("file"), 0o644)
	flags := &serverStartFlags{stateDir: blockedStateDir}
	out.Reset()
	errOut.Reset()
	if code := startDaemon(flags, &out, &errOut); code != 1 {
		t.Errorf("startDaemon with blocked stateDir = %d, want 1", code)
	}

	// startDaemon failure resolving own executable
	oldExecutable := osExecutable
	osExecutable = func() (string, error) { return "", errors.New("simulated executable error") }
	out.Reset()
	errOut.Reset()
	if code := startDaemon(&serverStartFlags{stateDir: t.TempDir()}, &out, &errOut); code != 1 {
		t.Errorf("startDaemon with executable error code = %d, want 1", code)
	}
	osExecutable = oldExecutable

	// startDaemon with full flags and serverStart returning error
	oldServerStart := serverStart
	serverStart = func(server.StartOptions) (int, error) {
		return 0, errors.New("simulated server start error")
	}
	allFlags := &serverStartFlags{
		stateDir:     t.TempDir(),
		addr:         "127.0.0.1:8080",
		upstream:     "http://localhost:11434",
		fixtures:     "fixtures.json",
		uiDir:        "ui_dir",
		uiEnabled:    true,
		uiURL:        "http://ui.com",
		allowOrigins: []string{"http://origin.com"},
	}
	out.Reset()
	errOut.Reset()
	if code := startDaemon(allFlags, &out, &errOut); code != 1 {
		t.Errorf("startDaemon with server start error code = %d, want 1", code)
	}
	serverStart = oldServerStart

	// executeServerRestart with stopping error
	serverStop = func(string, time.Duration) error { return errors.New("simulated stop error") }
	out.Reset()
	errOut.Reset()
	if code := executeServerRestart(allFlags, &out, &errOut); code != 1 {
		t.Errorf("executeServerRestart with stop error code = %d, want 1", code)
	}
	serverStop = oldServerStop

	// executeServerServe with canceled context (clean start and shutdown)
	oldNotifyCtx := notifyContextFunc
	notifyContextFunc = func(parent context.Context, _ ...os.Signal) (context.Context, context.CancelFunc) {
		ctx, cancel := context.WithCancel(parent)
		cancel()
		return ctx, cancel
	}
	serveFlagsOK := serverStartFlags{
		addr:     "127.0.0.1:0",
		upstream: "http://127.0.0.1:11434",
	}
	out.Reset()
	errOut.Reset()
	if code := executeServerServe(serveFlagsOK, &out, &errOut); code != 0 {
		t.Errorf("executeServerServe clean code = %d, want 0; stderr=%s", code, errOut.String())
	}
	notifyContextFunc = oldNotifyCtx

	// executeServerServe with invalid listen address
	serveFlagsBadAddr := serverStartFlags{
		addr:     "invalid:::address",
		upstream: "http://127.0.0.1:11434",
	}
	out.Reset()
	errOut.Reset()
	if code := executeServerServe(serveFlagsBadAddr, &out, &errOut); code != 1 {
		t.Errorf("executeServerServe with bad addr code = %d, want 1", code)
	}

	// executeServerServe with invalid upstream URL
	serveFlagsBad := serverStartFlags{
		addr:     "127.0.0.1:0",
		upstream: "://bad-url",
	}
	out.Reset()
	errOut.Reset()
	if code := executeServerServe(serveFlagsBad, &out, &errOut); code != 1 {
		t.Errorf("executeServerServe with bad upstream code = %d, want 1", code)
	}

	// executeServerServe with invalid UI dir option
	serveFlagsBadUI := serverStartFlags{
		addr:      "127.0.0.1:0",
		upstream:  "http://127.0.0.1:11434",
		uiEnabled: true,
		uiURL:     "http://127.0.0.1:1/nonexistent",
	}
	out.Reset()
	errOut.Reset()
	if code := executeServerServe(serveFlagsBadUI, &out, &errOut); code != 1 {
		t.Errorf("executeServerServe with bad UI code = %d, want 1", code)
	}

	// Run serve, start, restart, stop via root command
	serverStart = func(server.StartOptions) (int, error) { return 42, nil }
	notifyContextFunc = func(parent context.Context, _ ...os.Signal) (context.Context, context.CancelFunc) {
		ctx, cancel := context.WithCancel(parent)
		cancel()
		return ctx, cancel
	}

	out.Reset()
	errOut.Reset()
	if code := runServerServe([]string{"--addr", "127.0.0.1:0"}, &out, &errOut); code != 0 {
		t.Errorf("runServerServe code = %d, want 0; stderr=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	if code := run([]string{"server", "start", "--state-dir", t.TempDir()}, &out, &errOut); code != 0 {
		t.Errorf("run server start code = %d, want 0; stderr=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	if code := run([]string{"server", "restart", "--state-dir", t.TempDir()}, &out, &errOut); code != 0 {
		t.Errorf("run server restart code = %d, want 0; stderr=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	if code := runServerStop([]string{"--state-dir", t.TempDir()}, &out, &errOut); code != 0 {
		t.Errorf("runServerStop code = %d, want 0; stderr=%s", code, errOut.String())
	}

	serverStart = oldServerStart
	notifyContextFunc = oldNotifyCtx
}

func TestSkillsCoverage(t *testing.T) {
	// skillsSyncErrors Failure with UsageError
	var se skillsSyncErrors
	usageErr := &skillscmd.UsageError{Err: errors.New("usage error")}
	if err := se.Failure(usageErr); err == nil {
		t.Error("se.Failure(usageErr) = nil, want error")
	} else {
		var ce *commandError
		if !errors.As(err, &ce) || ce.code != 2 {
			t.Errorf("se.Failure(usageErr) code = %v, want 2", ce)
		}
	}

	// skillsSyncErrors Failure with normal error
	normErr := errors.New("normal error")
	if err := se.Failure(normErr); err == nil {
		t.Error("se.Failure(normErr) = nil, want error")
	} else {
		var ce *commandError
		if !errors.As(err, &ce) || ce.code != 1 {
			t.Errorf("se.Failure(normErr) code = %v, want 1", ce)
		}
	}

	// skillsSyncErrors Conflict
	conflictReport := skillsync.Report{
		Dir: "/test/dir",
		Changes: []skillsync.Change{
			{Action: skillsync.Conflict, Name: "skill1"},
		},
	}
	if err := se.Conflict(conflictReport); err == nil {
		t.Error("se.Conflict = nil, want error")
	} else {
		var ce *commandError
		if !errors.As(err, &ce) || ce.code != 1 {
			t.Errorf("se.Conflict code = %v, want 1", ce)
		}
	}

	// addJSONShortcut
	testCmd := &cobra.Command{
		Use: "test",
		Run: func(_ *cobra.Command, _ []string) {},
	}
	testCmd.Flags().String("format", "text", "format")
	originalCalled := false
	testCmd.PreRunE = func(_ *cobra.Command, _ []string) error {
		originalCalled = true
		return nil
	}
	addJSONShortcut(testCmd)
	testCmd.SetArgs([]string{"--json"})
	_ = testCmd.Execute()
	if !originalCalled {
		t.Error("original PreRunE was not called")
	}
	if fmtVal, _ := testCmd.Flags().GetString("format"); fmtVal != "json" {
		t.Errorf("format flag = %q, want json", fmtVal)
	}

	// addJSONShortcut error when setting format fails (no format flag)
	testCmdNoFormat := &cobra.Command{
		Use: "test-no-format",
		Run: func(_ *cobra.Command, _ []string) {},
	}
	addJSONShortcut(testCmdNoFormat)
	testCmdNoFormat.SetArgs([]string{"--json"})
	if err := testCmdNoFormat.Execute(); err == nil {
		t.Error("testCmdNoFormat.Execute() with --json: want error, got nil")
	}

	// newSkillsSyncConfig panics
	assertPanic := func(fn func()) {
		t.Helper()
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic, got none")
			}
		}()
		fn()
	}

	oldFSSub := skillsFSSub
	skillsFSSub = func(fs.FS, string) (fs.FS, error) {
		return nil, errors.New("sub error")
	}
	assertPanic(func() { newSkillsSyncConfig() })
	skillsFSSub = oldFSSub

	oldDigest := skillsDigest
	skillsDigest = func(fs.FS) (string, error) {
		return "", errors.New("digest error")
	}
	assertPanic(func() { newSkillsSyncConfig() })
	skillsDigest = oldDigest

	oldValidate := skillsValidateDescriptor
	skillsValidateDescriptor = func(skillsync.BundleDescriptor) error {
		return errors.New("validate error")
	}
	assertPanic(func() { newSkillsSyncConfig() })
	skillsValidateDescriptor = oldValidate

	oldEmbed := skillsEmbeddedBundle
	skillsEmbeddedBundle = func(skillsync.BundleDescriptor, fs.FS) (skillsync.Bundle, error) {
		return skillsync.Bundle{}, errors.New("embed error")
	}
	assertPanic(func() { newSkillsSyncConfig() })
	skillsEmbeddedBundle = oldEmbed
}
