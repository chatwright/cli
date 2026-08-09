package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/strongo/selfupdate"
)

// --- test seams: stub selfUpdateCheckFunc/selfUpdateApplyFunc/selfUpdateDetectFunc ---

func stubSelfUpdateCheck(t *testing.T, result selfupdate.CheckResult, err error) {
	t.Helper()
	prev := selfUpdateCheckFunc
	selfUpdateCheckFunc = func(selfupdate.Config, context.Context) (selfupdate.CheckResult, error) { return result, err }
	t.Cleanup(func() { selfUpdateCheckFunc = prev })
}

func stubSelfUpdateApply(t *testing.T, capture *selfupdate.Options, outcome selfupdate.Outcome, err error) {
	t.Helper()
	prev := selfUpdateApplyFunc
	selfUpdateApplyFunc = func(_ selfupdate.Config, _ context.Context, opts selfupdate.Options) (selfupdate.Outcome, error) {
		if capture != nil {
			*capture = opts
		}
		return outcome, err
	}
	t.Cleanup(func() { selfUpdateApplyFunc = prev })
}

func stubSelfUpdateDetect(t *testing.T, detection selfupdate.Detection, err error) {
	t.Helper()
	prev := selfUpdateDetectFunc
	selfUpdateDetectFunc = func(selfupdate.Config) (selfupdate.Detection, error) { return detection, err }
	t.Cleanup(func() { selfUpdateDetectFunc = prev })
}

// --- selfUpdateConfig: chatwright's own identity (REQ: consumer-configured-identity) ---

func TestSelfUpdateConfig_Identity(t *testing.T) {
	cfg := selfUpdateConfig()

	if cfg.BinaryName != "chatwright" {
		t.Errorf("BinaryName = %q, want chatwright", cfg.BinaryName)
	}
	if cfg.Repository != "chatwright/cli" {
		t.Errorf("Repository = %q, want chatwright/cli (the vanity module path chatwright.dev/cli does not name the GitHub repo)", cfg.Repository)
	}
	if cfg.CurrentVersion != cliVersion() {
		t.Errorf("CurrentVersion = %q, want cliVersion() = %q (must reuse the CLI's own version plumbing, not a duplicate)", cfg.CurrentVersion, cliVersion())
	}
	if len(cfg.VersionProbeArgs) != 1 || cfg.VersionProbeArgs[0] != "version" {
		t.Errorf("VersionProbeArgs = %v, want [version] (chatwright version prints \"chatwright <version>\")", cfg.VersionProbeArgs)
	}
}

// UndeterminedVersions must declare every placeholder cliVersion() can
// report — including "(devel)" defensively, even though cliVersion already
// filters it out before returning — per the task's own instruction: an
// undeclared placeholder is compared as a real version and reports an
// update available FROM a version that does not exist.
func TestSelfUpdateConfig_UndeterminedVersionsCoverEveryPlaceholder(t *testing.T) {
	cfg := selfUpdateConfig()
	want := map[string]bool{fallbackVersion: false, "(devel)": false}
	for _, v := range cfg.UndeterminedVersions {
		if _, ok := want[v]; ok {
			want[v] = true
		}
	}
	for v, found := range want {
		if !found {
			t.Errorf("UndeterminedVersions %v is missing %q", cfg.UndeterminedVersions, v)
		}
	}
}

func TestSelfUpdateConfig_HomebrewCaskManager(t *testing.T) {
	cfg := selfUpdateConfig()
	if len(cfg.Managers) != 1 {
		t.Fatalf("Managers = %v, want exactly one (Homebrew)", cfg.Managers)
	}
	mgr := cfg.Managers[0]
	if mgr.Name != "Homebrew" {
		t.Errorf("Managers[0].Name = %q, want Homebrew", mgr.Name)
	}
	// --cask, not a formula: .goreleaser.yml publishes a homebrew_casks
	// manifest, never a brews formula.
	if mgr.UpgradeCommand != "brew upgrade --cask chatwright" {
		t.Errorf("UpgradeCommand = %q, want %q", mgr.UpgradeCommand, "brew upgrade --cask chatwright")
	}
}

// SupportedPlatforms must be exactly .goreleaser.yml's own goos x goarch
// matrix, including its one explicit exclusion (windows/arm64 is `ignore`d
// there).
func TestSelfUpdateConfig_SupportedPlatformsMatchGoreleaser(t *testing.T) {
	cfg := selfUpdateConfig()
	want := map[selfupdate.Platform]bool{
		{GOOS: "linux", GOARCH: "amd64"}:   false,
		{GOOS: "linux", GOARCH: "arm64"}:   false,
		{GOOS: "darwin", GOARCH: "amd64"}:  false,
		{GOOS: "darwin", GOARCH: "arm64"}:  false,
		{GOOS: "windows", GOARCH: "amd64"}: false,
	}
	if len(cfg.SupportedPlatforms) != len(want) {
		t.Fatalf("SupportedPlatforms = %v, want exactly %d entries", cfg.SupportedPlatforms, len(want))
	}
	for _, p := range cfg.SupportedPlatforms {
		if _, ok := want[p]; !ok {
			t.Errorf("SupportedPlatforms contains unexpected %+v", p)
		}
		want[p] = true
	}
	for p, found := range want {
		if !found {
			t.Errorf("SupportedPlatforms is missing %+v", p)
		}
	}
	for _, p := range cfg.SupportedPlatforms {
		if p.GOOS == "windows" && p.GOARCH == "arm64" {
			t.Error("SupportedPlatforms must not include windows/arm64 (.goreleaser.yml ignores it)")
		}
	}
}

// --- dispatch and alias: main.go's `self-update`/`update` cases both reach runSelfUpdate ---

func TestRunSelfUpdateAliasDispatch(t *testing.T) {
	for _, name := range []string{"self-update", "update"} {
		t.Run(name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			// --help never touches stdin, so this is safe to drive through
			// main.go's own run() dispatch (which reads the real os.Stdin
			// for a non-help invocation) without a fake terminal.
			if code := run([]string{name, "--help"}, &stdout, &stderr); code != 0 {
				t.Fatalf("run([%q, --help]) code = %d, want 0; stderr=%q", name, code, stderr.String())
			}
			if !strings.Contains(stdout.String(), "chatwright self-update") {
				t.Errorf("run([%q, --help]) stdout = %q, want self-update usage text", name, stdout.String())
			}
		})
	}
}

func TestRunSelfUpdateHelp(t *testing.T) {
	for _, args := range [][]string{{"help"}, {"-h"}, {"--help"}} {
		var stdout, stderr bytes.Buffer
		code := runSelfUpdate(args, strings.NewReader(""), &stdout, &stderr)
		if code != 0 {
			t.Fatalf("runSelfUpdate(%v) code = %d, want 0", args, code)
		}
		for _, want := range []string{"chatwright self-update", "chatwright update", "--check", "--yes, -y", "--version TAG", "--allow-downgrade", "--dry-run", "--format text|json", "Exit codes"} {
			if !strings.Contains(stdout.String(), want) {
				t.Errorf("runSelfUpdate(%v) stdout missing %q:\n%s", args, want, stdout.String())
			}
		}
	}
}

// --- flag surface ---

func TestRunSelfUpdate_UnexpectedExtraArgument(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runSelfUpdate([]string{"extra"}, strings.NewReader(""), &stdout, &stderr)
	if code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), `unexpected extra argument "extra"`) {
		t.Errorf("stderr = %q, want an unexpected-extra-argument message", stderr.String())
	}
}

func TestRunSelfUpdate_UnknownFormat(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runSelfUpdate([]string{"--format", "xml"}, strings.NewReader(""), &stdout, &stderr)
	if code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), `unknown --format "xml"`) {
		t.Errorf("stderr = %q, want an unknown-format message", stderr.String())
	}
}

func TestRunSelfUpdate_BadFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runSelfUpdate([]string{"--not-a-flag"}, strings.NewReader(""), &stdout, &stderr)
	if code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
}

// Every apply-only flag (--version, --allow-downgrade, --dry-run, --yes)
// must reach selfupdate.Options unchanged, and a Confirm callback must
// always be supplied.
func TestRunSelfUpdate_FlagsMapToOptions(t *testing.T) {
	var captured selfupdate.Options
	stubSelfUpdateApply(t, &captured, selfupdate.Outcome{Action: selfupdate.ActionUpdated, Target: "1.2.3"}, nil)

	var stdout, stderr bytes.Buffer
	code := runSelfUpdate([]string{"--version", "1.2.3", "--allow-downgrade", "--dry-run", "--yes"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr=%q", code, stderr.String())
	}
	if captured.PinnedVersion != "1.2.3" {
		t.Errorf("PinnedVersion = %q, want 1.2.3", captured.PinnedVersion)
	}
	if !captured.AllowDowngrade {
		t.Error("AllowDowngrade = false, want true")
	}
	if !captured.DryRun {
		t.Error("DryRun = false, want true")
	}
	if captured.Confirm == nil {
		t.Fatal("Confirm is nil; runSelfUpdateApply must always supply a confirm callback")
	}
}

// -y is a shorthand for --yes (both bound to the same flag.Bool var).
func TestRunSelfUpdate_ShortYesFlag(t *testing.T) {
	var captured selfupdate.Options
	stubSelfUpdateApply(t, &captured, selfupdate.Outcome{Action: selfupdate.ActionUpdated}, nil)

	var stdout, stderr bytes.Buffer
	code := runSelfUpdate([]string{"-y"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr=%q", code, stderr.String())
	}
	proceed, err := captured.Confirm("1.0.0 → 1.1.0")
	if err != nil || !proceed {
		t.Fatalf("Confirm after -y = (%v, %v), want (true, nil)", proceed, err)
	}
}

// --check takes priority over every apply-only flag, exactly matching
// cobracmd's own RunE dispatch order: an update-only flag given alongside
// --check is simply never consulted.
func TestRunSelfUpdate_CheckTakesPriority(t *testing.T) {
	stubSelfUpdateCheck(t, selfupdate.CheckResult{Current: "1.0.0", Latest: "1.0.0", Verdict: selfupdate.UpToDate}, nil)
	stubSelfUpdateDetect(t, selfupdate.Detection{Method: selfupdate.Manual}, nil)

	applyCalled := false
	prevApply := selfUpdateApplyFunc
	selfUpdateApplyFunc = func(selfupdate.Config, context.Context, selfupdate.Options) (selfupdate.Outcome, error) {
		applyCalled = true
		return selfupdate.Outcome{}, nil
	}
	t.Cleanup(func() { selfUpdateApplyFunc = prevApply })

	var stdout, stderr bytes.Buffer
	code := runSelfUpdate([]string{"--check", "--yes", "--dry-run"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr=%q", code, stderr.String())
	}
	if applyCalled {
		t.Error("--check must not reach the apply path")
	}
}

// --- --check output and exit codes ---

func TestRunSelfUpdate_CheckUpToDate(t *testing.T) {
	stubSelfUpdateCheck(t, selfupdate.CheckResult{Current: "1.0.0", Latest: "1.0.0", Verdict: selfupdate.UpToDate}, nil)
	stubSelfUpdateDetect(t, selfupdate.Detection{Method: selfupdate.Manual}, nil)

	var stdout, stderr bytes.Buffer
	code := runSelfUpdate([]string{"--check"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "up to date") {
		t.Errorf("stdout = %q, want up-to-date report", stdout.String())
	}
	if strings.Contains(stdout.String(), "To upgrade") {
		t.Errorf("an up-to-date check must print no next step:\n%s", stdout.String())
	}
}

// An available (or undetermined) update is reported via --check, never
// failed: this CLI has no pre-existing "general findings" exit code to
// fold it into, and this file deliberately declines to invent one — see
// runSelfUpdateCheck's own doc comment.
func TestRunSelfUpdate_CheckUpdateAvailableExitsZeroAndNamesNextStep(t *testing.T) {
	stubSelfUpdateCheck(t, selfupdate.CheckResult{Current: "1.0.0", Latest: "1.1.0", Verdict: selfupdate.UpdateAvailable}, nil)
	stubSelfUpdateDetect(t, selfupdate.Detection{Method: selfupdate.Manual}, nil)

	var stdout, stderr bytes.Buffer
	code := runSelfUpdate([]string{"--check"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, want 0 (an available update is reported, not failed)", code)
	}
	if !strings.Contains(stdout.String(), "1.0.0") || !strings.Contains(stdout.String(), "1.1.0") {
		t.Errorf("stdout = %q, want both versions reported", stdout.String())
	}
	if !strings.Contains(stdout.String(), "To upgrade, run: chatwright self-update") {
		t.Errorf("stdout = %q, want the manual-install next step naming the canonical command", stdout.String())
	}
}

func TestRunSelfUpdate_CheckManagedNamesUpgradeCommand(t *testing.T) {
	mgr := selfupdate.Homebrew("brew upgrade --cask chatwright")
	stubSelfUpdateCheck(t, selfupdate.CheckResult{Current: "1.0.0", Latest: "1.1.0", Verdict: selfupdate.UpdateAvailable}, nil)
	stubSelfUpdateDetect(t, selfupdate.Detection{Method: selfupdate.Managed, Manager: &mgr}, nil)

	var stdout, stderr bytes.Buffer
	code := runSelfUpdate([]string{"--check"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "brew upgrade --cask chatwright") {
		t.Errorf("stdout = %q, want the Homebrew upgrade command", stdout.String())
	}
}

// Detection is a pure path classification with no network or write of its
// own; a failure resolving it must not fail the check, which still reports
// the version comparison (matches cobracmd.runCheck's identical fallback).
func TestRunSelfUpdate_CheckDetectionFailureStillReportsComparison(t *testing.T) {
	stubSelfUpdateCheck(t, selfupdate.CheckResult{Current: "1.0.0", Latest: "1.1.0", Verdict: selfupdate.UpdateAvailable}, nil)
	stubSelfUpdateDetect(t, selfupdate.Detection{}, errors.New("cannot resolve executable"))

	var stdout, stderr bytes.Buffer
	code := runSelfUpdate([]string{"--check"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "1.0.0") || !strings.Contains(stdout.String(), "1.1.0") {
		t.Errorf("stdout = %q, want the version comparison despite the detection failure", stdout.String())
	}
}

func TestRunSelfUpdate_CheckJSON(t *testing.T) {
	mgr := selfupdate.Homebrew("brew upgrade --cask chatwright")
	stubSelfUpdateCheck(t, selfupdate.CheckResult{Current: "1.0.0", Latest: "1.1.0", Verdict: selfupdate.UpdateAvailable}, nil)
	stubSelfUpdateDetect(t, selfupdate.Detection{Method: selfupdate.Managed, Manager: &mgr}, nil)

	var stdout, stderr bytes.Buffer
	code := runSelfUpdate([]string{"--check", "--format", "json"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr=%q", code, stderr.String())
	}
	var got map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, stdout.String())
	}
	for key, want := range map[string]string{
		"current":         "1.0.0",
		"latest":          "1.1.0",
		"verdict":         "update_available",
		"install_method":  "managed",
		"manager":         "Homebrew",
		"upgrade_command": "brew upgrade --cask chatwright",
	} {
		if got[key] != want {
			t.Errorf("json[%q] = %v, want %q\n%s", key, got[key], want, stdout.String())
		}
	}
}

func TestRunSelfUpdate_CheckFailureMapsExitCode(t *testing.T) {
	stubSelfUpdateCheck(t, selfupdate.CheckResult{}, &selfupdate.Failure{Kind: selfupdate.KindReleaseLookup, Err: errors.New("network down")})

	var stdout, stderr bytes.Buffer
	code := runSelfUpdate([]string{"--check"}, strings.NewReader(""), &stdout, &stderr)
	if code != 1 {
		t.Fatalf("code = %d, want 1 (KindReleaseLookup is a general failure)", code)
	}
	if !strings.Contains(stderr.String(), "network down") {
		t.Errorf("stderr = %q, want the underlying error", stderr.String())
	}
}

// --- apply: outcome formatting ---

func TestRunSelfUpdate_ManagedRedirectText(t *testing.T) {
	mgr := selfupdate.Homebrew("brew upgrade --cask chatwright")
	stubSelfUpdateApply(t, nil, selfupdate.Outcome{
		Action:    selfupdate.ActionRedirected,
		Detection: selfupdate.Detection{Method: selfupdate.Managed, Manager: &mgr},
	}, nil)

	var stdout, stderr bytes.Buffer
	code := runSelfUpdate(nil, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Homebrew") || !strings.Contains(stdout.String(), "brew upgrade --cask chatwright") {
		t.Errorf("stdout = %q, want the manager and its upgrade command", stdout.String())
	}
}

func TestRunSelfUpdate_AlreadyCurrentText(t *testing.T) {
	stubSelfUpdateApply(t, nil, selfupdate.Outcome{
		Action: selfupdate.ActionAlreadyCurrent,
		Result: selfupdate.CheckResult{Current: "1.0.0"},
	}, nil)

	var stdout, stderr bytes.Buffer
	code := runSelfUpdate([]string{"--yes"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "up to date") {
		t.Errorf("stdout = %q, want up-to-date report", stdout.String())
	}
}

func TestRunSelfUpdate_DryRunPlannedTextNamesAsset(t *testing.T) {
	stubSelfUpdateApply(t, nil, selfupdate.Outcome{
		Action:     selfupdate.ActionPlanned,
		Result:     selfupdate.CheckResult{Current: "1.0.0", Latest: "1.1.0"},
		Target:     "1.1.0",
		PlannedURL: "https://github.com/chatwright/cli/releases/download/v1.1.0/chatwright_1.1.0_linux_amd64.tar.gz",
	}, nil)

	var stdout, stderr bytes.Buffer
	code := runSelfUpdate([]string{"--dry-run"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "dry run") {
		t.Errorf("stdout = %q, want 'dry run'", stdout.String())
	}
	if !strings.Contains(stdout.String(), "chatwright_1.1.0_linux_amd64.tar.gz") {
		t.Errorf("stdout = %q, want the planned asset name", stdout.String())
	}
}

func TestRunSelfUpdate_UpdatedTextAndJSON(t *testing.T) {
	stubSelfUpdateApply(t, nil, selfupdate.Outcome{
		Action: selfupdate.ActionUpdated,
		Result: selfupdate.CheckResult{Current: "1.0.0", Latest: "1.1.0"},
		Target: "1.1.0",
	}, nil)

	var stdout, stderr bytes.Buffer
	code := runSelfUpdate([]string{"--yes", "--format", "json"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr=%q", code, stderr.String())
	}
	var got struct{ Action, Target string }
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, stdout.String())
	}
	if got.Action != "updated" || got.Target != "1.1.0" {
		t.Errorf("decoded = %+v", got)
	}
}

func TestRunSelfUpdate_AbortedText(t *testing.T) {
	stubSelfUpdateApply(t, nil, selfupdate.Outcome{Action: selfupdate.ActionAborted}, nil)

	var stdout, stderr bytes.Buffer
	code := runSelfUpdate(nil, strings.NewReader("n\n"), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, want 0 (a declined confirmation is a success, not a failure)", code)
	}
	if !strings.Contains(strings.ToLower(stdout.String()), "aborted") {
		t.Errorf("stdout = %q, want an aborted report", stdout.String())
	}
}

func TestRunSelfUpdate_PostSwapWarningOnStderr(t *testing.T) {
	stubSelfUpdateApply(t, nil, selfupdate.Outcome{
		Action:          selfupdate.ActionUpdated,
		Target:          "1.1.0",
		PostSwapWarning: errors.New("version probe mismatch"),
	}, nil)

	var stdout, stderr bytes.Buffer
	code := runSelfUpdate([]string{"--yes"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("a post-swap warning must not fail the command: code = %d", code)
	}
	if !strings.Contains(stderr.String(), "version probe mismatch") {
		t.Errorf("stderr = %q, want the post-swap warning", stderr.String())
	}
}

// --- apply: failures and exit-code mapping end-to-end ---

func TestRunSelfUpdate_AmbiguousPrintsGuidanceAndExitsOne(t *testing.T) {
	ambErr := &selfupdate.Failure{Kind: selfupdate.KindAmbiguous, Path: "/opt/x/chatwright", Err: errors.New("ambiguous")}
	stubSelfUpdateApply(t, nil, selfupdate.Outcome{}, ambErr)

	var stdout, stderr bytes.Buffer
	code := runSelfUpdate([]string{"--yes"}, strings.NewReader(""), &stdout, &stderr)
	if code != 1 {
		t.Fatalf("code = %d, want 1 (KindAmbiguous is not fixed by a different flag)", code)
	}
	if !strings.Contains(strings.ToLower(stdout.String()), "ambiguous") {
		t.Errorf("stdout = %q, want ambiguous-install guidance", stdout.String())
	}
	if !strings.Contains(stdout.String(), "chatwright/cli") {
		t.Errorf("stdout = %q, want the repository named for manual download", stdout.String())
	}
}

func TestRunSelfUpdate_NonAmbiguousFailureNoGuidance(t *testing.T) {
	checksumErr := &selfupdate.Failure{Kind: selfupdate.KindChecksum, Err: errors.New("mismatch")}
	stubSelfUpdateApply(t, nil, selfupdate.Outcome{}, checksumErr)

	var stdout, stderr bytes.Buffer
	code := runSelfUpdate([]string{"--yes"}, strings.NewReader(""), &stdout, &stderr)
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if strings.Contains(strings.ToLower(stdout.String()), "ambiguous") {
		t.Errorf("stdout = %q, wrongly prints ambiguous guidance for a checksum failure", stdout.String())
	}
}

func TestRunSelfUpdate_DowngradeFailureExitsTwo(t *testing.T) {
	dgErr := &selfupdate.Failure{Kind: selfupdate.KindDowngrade, Err: errors.New("refusing to downgrade")}
	stubSelfUpdateApply(t, nil, selfupdate.Outcome{}, dgErr)

	var stdout, stderr bytes.Buffer
	code := runSelfUpdate([]string{"--yes", "--version", "0.9.0"}, strings.NewReader(""), &stdout, &stderr)
	if code != 2 {
		t.Fatalf("code = %d, want 2 (fixed by --allow-downgrade)", code)
	}
}

func TestRunSelfUpdate_UnknownTagFailureExitsTwo(t *testing.T) {
	tagErr := &selfupdate.Failure{Kind: selfupdate.KindUnknownTag, Err: errors.New("no such release")}
	stubSelfUpdateApply(t, nil, selfupdate.Outcome{}, tagErr)

	var stdout, stderr bytes.Buffer
	code := runSelfUpdate([]string{"--yes", "--version", "v99.0.0"}, strings.NewReader(""), &stdout, &stderr)
	if code != 2 {
		t.Fatalf("code = %d, want 2 (fixed by a different --version value)", code)
	}
}

// Without --yes and without a terminal attached (go test's own stdin is
// never a tty), the wired Confirm callback must refuse — this is the same
// behavior `self-update </dev/null` exhibits from a real shell, exercised
// here without spawning a subprocess or touching a real binary.
func TestRunSelfUpdate_NonInteractiveRefusalWithoutYes(t *testing.T) {
	var captured selfupdate.Options
	stubSelfUpdateApply(t, &captured, selfupdate.Outcome{}, nil)

	var stdout, stderr bytes.Buffer
	code := runSelfUpdate(nil, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		// The stub itself returns a nil error; this assertion only proves
		// the CLI reached the stub, not anything about Confirm's behavior.
		t.Fatalf("code = %d, want 0 from the stub", code)
	}
	if captured.Confirm == nil {
		t.Fatal("Confirm is nil")
	}
	proceed, err := captured.Confirm("1.0.0 → 1.1.0")
	if proceed {
		t.Error("Confirm proceeded without --yes and without a terminal")
	}
	if selfupdate.KindOf(err) != selfupdate.KindNonInteractive {
		t.Errorf("KindOf(Confirm error) = %v, want KindNonInteractive", selfupdate.KindOf(err))
	}
	// Prove the wiring maps that refusal onto exit code 2, the same way a
	// real (non-stubbed) Update call returning it would.
	if got := mapSelfUpdateExitCode(err); got != 2 {
		t.Errorf("mapSelfUpdateExitCode(non-interactive refusal) = %d, want 2", got)
	}
}

// --- mapSelfUpdateExitCode: every FailureKind, exhaustively ---

func TestMapSelfUpdateExitCode(t *testing.T) {
	cases := []struct {
		kind selfupdate.FailureKind
		want int
	}{
		{selfupdate.KindAmbiguous, 1},
		{selfupdate.KindReleaseLookup, 1},
		{selfupdate.KindDownload, 1},
		{selfupdate.KindChecksum, 1},
		{selfupdate.KindPermission, 1},
		{selfupdate.KindNonInteractive, 2},
		{selfupdate.KindDowngrade, 2},
		{selfupdate.KindUnknownTag, 2},
		{selfupdate.KindUnsupportedPlatform, 1},
		{selfupdate.KindUnexpected, 1},
	}
	for _, tc := range cases {
		t.Run(tc.kind.String(), func(t *testing.T) {
			err := &selfupdate.Failure{Kind: tc.kind, Err: errors.New("x")}
			if got := mapSelfUpdateExitCode(err); got != tc.want {
				t.Errorf("mapSelfUpdateExitCode(%s) = %d, want %d", tc.kind, got, tc.want)
			}
		})
	}
}

func TestMapSelfUpdateExitCode_NonFailureError(t *testing.T) {
	// A plain error (not a *selfupdate.Failure) classifies as
	// KindUnexpected via selfupdate.KindOf, and takes the general-failure
	// code like every other unexpected error.
	if got := mapSelfUpdateExitCode(errors.New("plain")); got != 1 {
		t.Errorf("mapSelfUpdateExitCode(plain error) = %d, want 1", got)
	}
}

// --- real (unstubbed) seam bodies: no network, no real binary replaced ---

// The default selfUpdateCheckFunc body just calls cfg.Check(ctx); this
// exercises that real call against a local httptest.Server, never the
// actual GitHub API.
func TestSelfUpdateCheckFunc_RealDefaultCallsConfigCheck(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"tag_name":"v1.0.0","prerelease":false,"draft":false}]`))
	}))
	t.Cleanup(srv.Close)

	cfg := selfupdate.Config{
		BinaryName: "chatwright", Repository: "chatwright/cli", CurrentVersion: "1.0.0",
		ReleasesAPIURL: srv.URL, HTTPClient: srv.Client(),
	}
	result, err := selfUpdateCheckFunc(cfg, context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Verdict != selfupdate.UpToDate {
		t.Errorf("Verdict = %v, want UpToDate", result.Verdict)
	}
}

// The default selfUpdateApplyFunc body just calls cfg.Update(ctx, opts);
// pointed at an unreachable loopback address so it fails fast at release
// lookup, before any download or write — no real network, no binary
// touched.
func TestSelfUpdateApplyFunc_RealDefaultCallsConfigUpdate(t *testing.T) {
	cfg := selfupdate.Config{
		BinaryName: "chatwright", Repository: "chatwright/cli", CurrentVersion: "1.0.0",
		ReleasesAPIURL: "http://127.0.0.1:1", // nothing listens here
		HTTPClient:     http.DefaultClient,
	}
	_, err := selfUpdateApplyFunc(cfg, context.Background(), selfupdate.Options{})
	if err == nil {
		t.Fatal("expected an error (ambiguous test-binary install or an unreachable release endpoint), got nil")
	}
}

// The default selfUpdateDetectFunc body just calls cfg.DetectSelf(), which
// resolves the real go test binary's own path — filesystem only, no
// network, and no fake binary is substituted.
func TestSelfUpdateDetectFunc_RealDefaultCallsConfigDetectSelf(t *testing.T) {
	cfg := selfUpdateConfig()
	detection, err := selfUpdateDetectFunc(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if detection.Path == "" {
		t.Error("Detection.Path is empty, want the resolved test binary path")
	}
}
