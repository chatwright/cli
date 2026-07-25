package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"chatwright.dev/runtime/actor"
	runengine "chatwright.dev/runtime/run"
	"chatwright.dev/sdk"
)

// greetbotFixturePath is the same worked-example scenario document (and its
// checked-in cassette, resolved relative to it) the standard repository's
// self-contained-scenario-documents feature ships and runtime-go's own
// greetbot conformance test replays — copied here so `chatwright run`'s own
// tests exercise the real CLI end to end with zero network dependency,
// without this repository needing a cross-repository path into runtime-go.
// This is the very same copy example.go embeds as `chatwright run example`
// (see example_test.go) — one committed fixture serving both the shipped
// example and this file's own file-path tests.
const greetbotFixturePath = "examples/greetbot-language-onboarding.json"

func TestRunRequiresDocument(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runRun(nil, &stdout, &stderr); code != 2 {
		t.Fatalf("runRun(nil) code = %d, want 2; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "missing DOCUMENT") {
		t.Errorf("stderr = %q, want a missing-DOCUMENT message", stderr.String())
	}
}

// TestRunHelp guards a bare "help" (not "-h"/"--help", already handled by
// flag.FlagSet.Parse itself) against being swallowed as the DOCUMENT
// positional argument — found in review of #7.
func TestRunHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runRun([]string{"help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("runRun([\"help\"]) code = %d, want 0; stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "chatwright run DOCUMENT") {
		t.Errorf("stdout = %q, want the command's own usage", stdout.String())
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

	if !strings.Contains(stdout.String(), "completed") {
		t.Errorf("stdout = %q, want it to report a completed part", stdout.String())
	}
	if !strings.Contains(stdout.String(), "verdict   verified") {
		t.Errorf("stdout = %q, want it to report a verified verdict (not judged) — the document declares a verify block", stdout.String())
	}

	bundlePath := filepath.Join(outDir, "greetbot-language-onboarding.chatwright.json")
	f, err := os.Open(bundlePath)
	if err != nil {
		t.Fatalf("Open(%s) error = %v", bundlePath, err)
	}
	defer func() { _ = f.Close() }()

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

// writeMutatedGreetbotFixture writes a private copy of greetbotFixturePath
// (document + cassette, laid out exactly as its own directory does — see
// example.go's own note that a cassette path is always resolved relative to
// its document) into a fresh t.TempDir(), after applying mutate to the
// document's decoded JSON. This lets a test invalidate exactly one thing
// (the goal text that feeds the actor's cassette key, or the verify block
// that judges the resulting journal) while everything else — crucially the
// cassette itself — stays byte-identical to the shipped fixture, the same
// way a real user's `chatwright run example --write` followed by a hand
// edit does. Returns the written document's own path.
func writeMutatedGreetbotFixture(t *testing.T, mutate func(doc map[string]any)) string {
	t.Helper()

	raw, err := os.ReadFile(greetbotFixturePath)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", greetbotFixturePath, err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("Unmarshal(%s) error = %v", greetbotFixturePath, err)
	}
	mutate(doc)
	mutated, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("Marshal mutated document: %v", err)
	}

	dir := t.TempDir()
	docPath := filepath.Join(dir, "greetbot-language-onboarding.json")
	if err := os.WriteFile(docPath, mutated, 0o644); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", docPath, err)
	}

	cassetteSrc := filepath.Join(filepath.Dir(greetbotFixturePath), "cassettes", "greetbot-language-onboarding.json")
	cassetteBytes, err := os.ReadFile(cassetteSrc)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", cassetteSrc, err)
	}
	cassetteDir := filepath.Join(dir, "cassettes")
	if err := os.MkdirAll(cassetteDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s) error = %v", cassetteDir, err)
	}
	cassetteDst := filepath.Join(cassetteDir, "greetbot-language-onboarding.json")
	if err := os.WriteFile(cassetteDst, cassetteBytes, 0o644); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", cassetteDst, err)
	}

	return docPath
}

// mutateSuccessCriteriaText appends to the document's one task's
// successCriteria — exactly the edit the bug report describes ("edit the
// written document's successCriteria text, re-run it") — which changes the
// prompt the checked-in cassette was keyed on without touching anything
// else, so every Provider.Propose call the actor makes is a guaranteed
// actor.ErrCassetteCacheMiss.
func mutateSuccessCriteriaText(doc map[string]any) {
	task := doc["parts"].([]any)[0].(map[string]any)["goal"].(map[string]any)["tasks"].([]any)[0].(map[string]any)
	task["successCriteria"] = task["successCriteria"].(string) + " Be extra polite about it."
}

// mutateVerifyExpectationToNeverMatch edits only the verify block's
// "greeting-changed" expectation to require text the bot never sends,
// leaving the goal (and so the cassette's replay key) untouched. This
// constructs a genuine bot-behaviour-style failure: the actor runs to
// completion exactly as the unedited fixture does (same cassette, same
// hits), but the journal it produced does not show what the scenario
// declared it must — the class of failure "not verified" exists to report,
// as opposed to the actor never having been driven at all.
func mutateVerifyExpectationToNeverMatch(doc map[string]any) {
	for _, raw := range doc["verify"].(map[string]any)["journal"].([]any) {
		exp := raw.(map[string]any)
		if exp["id"] != "greeting-changed" {
			continue
		}
		all := exp["all"].([]any)
		last := all[len(all)-1].(map[string]any)
		last["value"] = "this text never appears in any greeting the bot sends"
	}
}

// TestRunReportsActorUnavailableOnCassetteCacheMiss reproduces the
// diagnosability bug this file fixes: a document written by `chatwright run
// example --write`, then edited (successCriteria, here — the goal text a
// cassette's replay key is derived from), replays against a cassette keyed
// on the *original* document, so every Propose call is a cache miss. Before
// the fix this printed "outcome=not verified: journal evidence incomplete:
// …" — indistinguishable from the bot itself having misbehaved — and
// exited 1, the same code a real verification failure uses. This test
// guards both: the actor-unavailable wording (naming the real cause and
// the fix) and its own distinct exit code.
func TestRunReportsActorUnavailableOnCassetteCacheMiss(t *testing.T) {
	docPath := writeMutatedGreetbotFixture(t, mutateSuccessCriteriaText)
	outDir := t.TempDir()

	var stdout, stderr bytes.Buffer
	code := runRun([]string{docPath, "--out", outDir}, &stdout, &stderr)
	if code != exitActorUnavailable {
		t.Fatalf("runRun() code = %d, want %d (exitActorUnavailable); stdout=%q stderr=%q", code, exitActorUnavailable, stdout.String(), stderr.String())
	}

	out := stdout.String()
	if !strings.Contains(out, "verdict   actor unavailable") {
		t.Errorf("stdout = %q, want an \"actor unavailable\" verdict", out)
	}
	if !strings.Contains(out, "replay cache miss") {
		t.Errorf("stdout = %q, want the actual actor.ErrCassetteCacheMiss text surfaced, not swallowed", out)
	}
	if !strings.Contains(out, "Re-record the cassette") {
		t.Errorf("stdout = %q, want the cache-miss fix pointer (re-record, or run against a live provider)", out)
	}
	if strings.Contains(out, "journal evidence incomplete") {
		t.Errorf("stdout = %q, must NOT present this harness failure as a verification failure", out)
	}
}

// TestRunReportsGenuineVerificationFailureDistinctFromActorFailure is
// TestRunReportsActorUnavailableOnCassetteCacheMiss's counterpart: a
// document whose goal is untouched (so the cassette replays exactly as it
// does for the unedited fixture — the actor runs the task to completion)
// but whose verify block was edited to require something the bot never
// did. This is the failure class "not verified" exists to report, and it
// must stay reported that way — the fix must not become so eager to detect
// "the actor failed" that it starts misreporting the opposite case too.
func TestRunReportsGenuineVerificationFailureDistinctFromActorFailure(t *testing.T) {
	docPath := writeMutatedGreetbotFixture(t, mutateVerifyExpectationToNeverMatch)
	outDir := t.TempDir()

	var stdout, stderr bytes.Buffer
	code := runRun([]string{docPath, "--out", outDir}, &stdout, &stderr)
	if code != exitVerificationFailed {
		t.Fatalf("runRun() code = %d, want %d (exitVerificationFailed); stdout=%q stderr=%q", code, exitVerificationFailed, stdout.String(), stderr.String())
	}

	out := stdout.String()
	if !strings.Contains(out, "completed") {
		t.Errorf("stdout = %q, want the actor to have run to completion — this is a bot-behaviour failure, not a harness failure", out)
	}
	if !strings.Contains(out, "verdict   not verified") {
		t.Errorf("stdout = %q, want a \"not verified\" verdict", out)
	}
	if !strings.Contains(out, "journal evidence incomplete") {
		t.Errorf("stdout = %q, want the unmet-expectation detail", out)
	}
	if strings.Contains(out, "actor unavailable") || strings.Contains(out, "cache miss") {
		t.Errorf("stdout = %q, must NOT be reported as an actor/harness failure", out)
	}
}

// TestRunOutcomeSucceededSeparatesFailureClasses is a fast, no-I/O
// companion to the two end-to-end tests above: it exercises runOutcome.
// succeeded (and so runRun's exit-code choice) directly against hand-built
// values, including the "somehow both" case that would be impossible to
// provoke through a real run but must still resolve unambiguously in
// actorFailed's favour — a harness failure is never a success, whatever
// partStatus or verified happen to say.
func TestRunOutcomeSucceededSeparatesFailureClasses(t *testing.T) {
	cases := []struct {
		name    string
		outcome runOutcome
		want    bool
	}{
		{"verified pass", runOutcome{partStatus: "completed", verified: true}, true},
		{"judged pass (document declares no verify block)", runOutcome{partStatus: "completed", judged: true}, true},
		{"not verified is a real failure, not a pass", runOutcome{partStatus: "completed", verified: false}, false},
		{"actor unavailable is a real failure, not a pass", runOutcome{partStatus: "failed", actorFailed: true}, false},
		{"actor unavailable outranks a completed/verified part", runOutcome{partStatus: "completed", verified: true, actorFailed: true}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.outcome.succeeded(); got != tc.want {
				t.Errorf("succeeded() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestActorFailureFromParts unit-tests the detection this fix hangs
// everything else off, isolated from scenario execution: a ProposeError-
// bearing LoopEvent is the signal (see actorFailureFromParts's own doc
// comment for why that is narrower, and more correct, than "the Part
// failed"), and errors.Is against actor.ErrCassetteCacheMiss on the Part's
// own Err is what decides whether the cache-miss-specific hint applies.
func TestActorFailureFromParts(t *testing.T) {
	t.Run("no parts", func(t *testing.T) {
		if _, _, found := actorFailureFromParts(nil); found {
			t.Errorf("found = true, want false for no parts at all")
		}
	})

	t.Run("ai-goal part that proposed normally is not an actor failure", func(t *testing.T) {
		parts := []runengine.PartOutcome{{
			Kind:   sdk.PartKindAIGoal,
			Status: runengine.PartCompleted,
			AIGoal: &sdk.AIGoalSection{Events: []sdk.LoopEvent{
				{Index: 0, ProposeError: ""},
				{Index: 1, ProposeError: ""},
			}},
		}}
		if _, _, found := actorFailureFromParts(parts); found {
			t.Errorf("found = true, want false — every event proposed successfully")
		}
	})

	t.Run("a deterministic part (no AIGoal at all) is never an actor failure", func(t *testing.T) {
		parts := []runengine.PartOutcome{{Kind: sdk.PartKindDeterministic, Status: runengine.PartFailed}}
		if _, _, found := actorFailureFromParts(parts); found {
			t.Errorf("found = true, want false — a deterministic part has no actor loop to fail")
		}
	})

	t.Run("cassette cache miss is detected and classified as such", func(t *testing.T) {
		wrapped := fmt.Errorf("actor: propose: %w: goal=g task=t observation#1 messages=0 history=0", actor.ErrCassetteCacheMiss)
		parts := []runengine.PartOutcome{{
			Kind:   sdk.PartKindAIGoal,
			Status: runengine.PartFailed,
			Err:    wrapped,
			AIGoal: &sdk.AIGoalSection{Events: []sdk.LoopEvent{
				{Index: 0, ProposeError: wrapped.Error()},
			}},
		}}
		detail, cacheMiss, found := actorFailureFromParts(parts)
		if !found {
			t.Fatalf("found = false, want true")
		}
		if !cacheMiss {
			t.Errorf("cacheMiss = false, want true — Err wraps actor.ErrCassetteCacheMiss")
		}
		if detail != wrapped.Error() {
			t.Errorf("detail = %q, want %q", detail, wrapped.Error())
		}
	})

	t.Run("a different actor failure is still detected but not classified as a cache miss", func(t *testing.T) {
		other := fmt.Errorf("actor: propose: some other provider error")
		parts := []runengine.PartOutcome{{
			Kind:   sdk.PartKindAIGoal,
			Status: runengine.PartFailed,
			Err:    other,
			AIGoal: &sdk.AIGoalSection{Events: []sdk.LoopEvent{
				{Index: 0, ProposeError: other.Error()},
			}},
		}}
		_, cacheMiss, found := actorFailureFromParts(parts)
		if !found {
			t.Fatalf("found = false, want true — a ProposeError was recorded, regardless of its cause")
		}
		if cacheMiss {
			t.Errorf("cacheMiss = true, want false — this was a different actor error, not actor.ErrCassetteCacheMiss")
		}
	})
}

// --- UX-overhaul tests: --json, --quiet, --verbose, the "no-run-ceiling"
// warning fix, and exit-code priority. ---

// TestRunJSONShape drives the real greetbot fixture end to end with
// --json and decodes stdout as JSON — proving --json's own documented
// shape holds for a real, successful, verified run, and that stdout
// carries NOTHING but that one JSON object (no progress line, no human
// summary bleeding in before or after it).
func TestRunJSONShape(t *testing.T) {
	outDir := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := runRun([]string{greetbotFixturePath, "--out", outDir, "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("runRun() code = %d, want 0; stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	var res runJSONResult
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		t.Fatalf("json.Unmarshal(stdout) error = %v; stdout = %q", err, stdout.String())
	}
	if res.DocumentID != "greetbot-language-onboarding" {
		t.Errorf("DocumentID = %q, want %q", res.DocumentID, "greetbot-language-onboarding")
	}
	if res.Verdict != "verified" {
		t.Errorf("Verdict = %q, want %q", res.Verdict, "verified")
	}
	if res.PartStatus != "completed" {
		t.Errorf("PartStatus = %q, want %q", res.PartStatus, "completed")
	}
	if res.Interrupted {
		t.Error("Interrupted = true, want false")
	}
	if res.BundlePath == "" {
		t.Error("BundlePath is empty, want the written bundle's path")
	}
	if res.Usage.CallCount == 0 {
		t.Error("Usage.CallCount = 0, want the real cassette-recorded call count")
	}

	// stdout must be exactly the JSON object (plus its own trailing
	// newline from json.Encoder) — no human summary line mixed in.
	if strings.Contains(stdout.String(), "RUN greetbot") {
		t.Errorf("stdout = %q, want NO human summary text alongside --json's own output", stdout.String())
	}
}

// TestRunJSONActorUnavailableShape drives the same cassette-cache-miss
// reproduction TestRunReportsActorUnavailableOnCassetteCacheMiss uses, but
// with --json, checking the failure path's own JSON fields — in
// particular actorCacheMiss and the non-zero exit code, which a CI
// consumer would branch on without ever parsing human text.
func TestRunJSONActorUnavailableShape(t *testing.T) {
	docPath := writeMutatedGreetbotFixture(t, mutateSuccessCriteriaText)
	outDir := t.TempDir()

	var stdout, stderr bytes.Buffer
	code := runRun([]string{docPath, "--out", outDir, "--json"}, &stdout, &stderr)
	if code != exitActorUnavailable {
		t.Fatalf("runRun() code = %d, want %d", code, exitActorUnavailable)
	}

	var res runJSONResult
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		t.Fatalf("json.Unmarshal(stdout) error = %v; stdout = %q", err, stdout.String())
	}
	if res.Verdict != "actor-unavailable" {
		t.Errorf("Verdict = %q, want %q", res.Verdict, "actor-unavailable")
	}
	if !res.ActorCacheMiss {
		t.Error("ActorCacheMiss = false, want true")
	}
	if !strings.Contains(res.Detail, "replay cache miss") {
		t.Errorf("Detail = %q, want it to mention the replay cache miss", res.Detail)
	}
}

// TestRunQuietSuppressesSuccessButNotFailure is quiet's own contract test:
// silence (empty stdout AND stderr) on an unremarkable success, but the
// same output as without --quiet on a genuine failure — "errors only".
func TestRunQuietSuppressesSuccessButNotFailure(t *testing.T) {
	t.Run("success: both streams empty", func(t *testing.T) {
		outDir := t.TempDir()
		var stdout, stderr bytes.Buffer
		code := runRun([]string{greetbotFixturePath, "--out", outDir, "--quiet"}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("runRun() code = %d, want 0; stderr=%q", code, stderr.String())
		}
		if stdout.String() != "" {
			t.Errorf("stdout = %q, want empty on a quiet successful run", stdout.String())
		}
		if stderr.String() != "" {
			t.Errorf("stderr = %q, want empty (no progress, no warnings) on a quiet successful run", stderr.String())
		}
		// The bundle must still have been written — --quiet silences
		// reporting, never the actual work.
		if _, err := os.Stat(filepath.Join(outDir, "greetbot-language-onboarding.chatwright.json")); err != nil {
			t.Errorf("bundle not written under --quiet: %v", err)
		}
	})

	t.Run("failure: still reported despite --quiet", func(t *testing.T) {
		docPath := writeMutatedGreetbotFixture(t, mutateVerifyExpectationToNeverMatch)
		outDir := t.TempDir()
		var stdout, stderr bytes.Buffer
		code := runRun([]string{docPath, "--out", outDir, "--quiet"}, &stdout, &stderr)
		if code != exitVerificationFailed {
			t.Fatalf("runRun() code = %d, want %d", code, exitVerificationFailed)
		}
		if !strings.Contains(stdout.String(), "verdict   not verified") {
			t.Errorf("stdout = %q, want the failure still reported despite --quiet", stdout.String())
		}
	})
}

// TestRunQuietNeverSuppressesJSON proves --json always wins: even a
// perfectly successful run with --quiet --json still emits the JSON
// object on stdout, since that IS the machine-readable answer, never
// "chatter" --quiet is meant to silence.
func TestRunQuietNeverSuppressesJSON(t *testing.T) {
	outDir := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := runRun([]string{greetbotFixturePath, "--out", outDir, "--quiet", "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("runRun() code = %d, want 0; stderr=%q", code, stderr.String())
	}
	var res runJSONResult
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		t.Fatalf("--quiet --json: stdout = %q did not decode as JSON: %v", stdout.String(), err)
	}
	if res.Verdict != "verified" {
		t.Errorf("Verdict = %q, want %q", res.Verdict, "verified")
	}
	if stderr.String() != "" {
		t.Errorf("stderr = %q, want empty — --quiet still silences stderr progress/warnings", stderr.String())
	}
}

// TestRunVerboseShowsPerIterationProgress proves --verbose surfaces
// per-turn detail on stderr — the individual actor-loop iterations a
// non-verbose piped run deliberately coarsens away to task boundaries only
// (see progressReporter.renderTask).
func TestRunVerboseShowsPerIterationProgress(t *testing.T) {
	outDir := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := runRun([]string{greetbotFixturePath, "--out", outDir, "--verbose"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("runRun() code = %d, want 0; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "step ") {
		t.Errorf("stderr = %q, want per-iteration \"step N\" lines under --verbose", stderr.String())
	}
	if !strings.Contains(stderr.String(), "acted:") {
		t.Errorf("stderr = %q, want a derived \"acted:\" indicator under --verbose", stderr.String())
	}
}

// TestRunQuietAndVerboseAreMutuallyExclusive guards the explicit usage
// error runRun returns for a nonsensical combination, rather than letting
// one flag silently win over the other.
func TestRunQuietAndVerboseAreMutuallyExclusive(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runRun([]string{greetbotFixturePath, "--quiet", "--verbose"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("runRun() code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "mutually exclusive") {
		t.Errorf("stderr = %q, want a mutually-exclusive usage error", stderr.String())
	}
}

// TestRunExampleDeclaresItsOwnCeiling guards item 6 of the UX brief: the
// CLI's own bundled example must never emit its own "no-run-ceiling"
// warning — a first-time user's first `chatwright run example` must not
// see a warning about the CLI's own demo.
func TestRunExampleDeclaresItsOwnCeiling(t *testing.T) {
	outDir := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := runRun([]string{exampleDocumentArg, "--out", outDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("runRun() code = %d, want 0; stderr=%q", code, stderr.String())
	}
	if strings.Contains(stderr.String(), "no-run-ceiling") {
		t.Errorf("stderr = %q, want NO no-run-ceiling warning from the CLI's own bundled example", stderr.String())
	}
	if !strings.Contains(stderr.String(), "run ceiling:") {
		t.Errorf("stderr = %q, want the run-ceiling header line, since the example now declares one", stderr.String())
	}
}

// TestRunExitCodePriority is runExitCode's own non-vacuous truth table:
// interrupted outranks actor-unavailable outranks not-verified outranks
// success, including the "somehow more than one applies" cases that would
// be impossible to provoke through a real run.
func TestRunExitCodePriority(t *testing.T) {
	cases := []struct {
		name        string
		interrupted bool
		outcome     runOutcome
		want        int
	}{
		{"success", false, runOutcome{partStatus: "completed", verified: true}, 0},
		{"judged success", false, runOutcome{partStatus: "completed", judged: true}, 0},
		{"not verified", false, runOutcome{partStatus: "completed", verified: false}, exitVerificationFailed},
		{"actor unavailable", false, runOutcome{actorFailed: true}, exitActorUnavailable},
		{"interrupted outranks actor unavailable", true, runOutcome{actorFailed: true}, exitInterrupted},
		{"interrupted outranks a real success", true, runOutcome{partStatus: "completed", verified: true}, exitInterrupted},
		{"actor unavailable outranks not-verified", false, runOutcome{actorFailed: true, partStatus: "completed", verified: false}, exitActorUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := runExitCode(tc.interrupted, tc.outcome); got != tc.want {
				t.Errorf("runExitCode(%v, %+v) = %d, want %d", tc.interrupted, tc.outcome, got, tc.want)
			}
		})
	}
}

// TestDetectProfile proves detectProfile correctly treats a non-*os.File
// writer (every test in this package, and any real destination that isn't
// a terminal file — a pipe, a network connection) as non-interactive, and
// so never colourised — the property TestRunEndToEndAgainstGreetbotFixture
// and friends implicitly depend on for their plain-substring assertions to
// be meaningful regardless of ambient NO_COLOR/CLICOLOR_FORCE state on the
// machine running the tests.
func TestDetectProfile(t *testing.T) {
	var buf bytes.Buffer
	p := detectProfile(&buf, func(string) string { return "" })
	if p.Interactive {
		t.Error("Interactive = true for a bytes.Buffer, want false")
	}
	if p.Color {
		t.Error("Color = true for a non-interactive stream with no env override, want false")
	}
}
