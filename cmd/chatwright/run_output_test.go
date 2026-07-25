package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"chatwright.dev/cli/internal/term"
	runengine "chatwright.dev/runtime/run"
	"chatwright.dev/runtime/scenario"
	"chatwright.dev/sdk"
)

// makeResult builds a minimal runengine.Result with nParts executed Parts
// and nSkipped never-reached ones — interruptSummary only ever reads the
// two slices' lengths, so the entries' own field values don't matter.
func makeResult(nParts, nSkipped int) runengine.Result {
	var r runengine.Result
	for i := 0; i < nParts; i++ {
		r.Parts = append(r.Parts, runengine.PartOutcome{})
	}
	for i := 0; i < nSkipped; i++ {
		r.Skipped = append(r.Skipped, runengine.SkippedPart{})
	}
	return r
}

func TestRunOutcomeVerdict(t *testing.T) {
	cases := []struct {
		name           string
		outcome        runOutcome
		wantWord       string
		wantJSON       string
		wantDetailPart string
	}{
		{"verified", runOutcome{verified: true, verifyDetail: "all good"}, "verified", "verified", "all good"},
		{"judged", runOutcome{judged: true}, "judged", "judged", "no independent journal verification declared"},
		{"not verified", runOutcome{verified: false, verifyDetail: "missing X"}, "not verified", "not-verified", "missing X"},
		{"actor unavailable", runOutcome{actorFailed: true, actorFailureDetail: "replay cache miss"}, "actor unavailable", "actor-unavailable", "replay cache miss"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.outcome.verdictWord(); got != tc.wantWord {
				t.Errorf("verdictWord() = %q, want %q", got, tc.wantWord)
			}
			if got := verdictJSONWord(tc.outcome.verdictWord()); got != tc.wantJSON {
				t.Errorf("verdictJSONWord() = %q, want %q", got, tc.wantJSON)
			}
			if got := tc.outcome.verdictDetail(); got != tc.wantDetailPart {
				t.Errorf("verdictDetail() = %q, want %q", got, tc.wantDetailPart)
			}
		})
	}
}

func TestRenderRunSummaryContent(t *testing.T) {
	outcome := runOutcome{docID: "greetbot-language-onboarding", partStatus: "completed", verified: true, verifyDetail: "all journal-verified"}
	usage := sdk.AggregateUsage{InputTokens: 48, OutputTokens: 16, CallCount: 4}
	out := renderRunSummary(term.Profile{}, outcome, usage, 1234*time.Millisecond, "out/greetbot-language-onboarding.chatwright.json")

	for _, want := range []string{
		"RUN greetbot-language-onboarding",
		"completed",
		"verdict   verified — all journal-verified",
		"duration  1.2s",
		"usage     48 in / 16 out tokens (4 calls)",
		"bundle    out/greetbot-language-onboarding.chatwright.json",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("renderRunSummary() = %q, want it to contain %q", out, want)
		}
	}
}

// TestRenderRunSummaryActorUnavailableIncludesHint guards that the
// cache-miss hint actually surfaces in the human summary, not just in
// runOutcome's own internal field.
func TestRenderRunSummaryActorUnavailableIncludesHint(t *testing.T) {
	outcome := runOutcome{docID: "d", partStatus: "failed", actorFailed: true, actorFailureDetail: "actor: replay cache miss: ...", actorCacheMiss: true}
	out := renderRunSummary(term.Profile{}, outcome, sdk.AggregateUsage{}, 0, "b.json")
	if !strings.Contains(out, "verdict   actor unavailable — actor: replay cache miss") {
		t.Errorf("out = %q, want the actor-unavailable verdict line", out)
	}
	if !strings.Contains(out, "hint      "+cacheMissHint) {
		t.Errorf("out = %q, want the cache-miss hint as its own row", out)
	}
}

// TestRenderRunSummaryOmitsUsageWhenNoCalls guards that a purely
// deterministic (or actor-unavailable-before-any-call) run's summary
// doesn't print a misleading "0 in / 0 out tokens (0 calls)" row.
func TestRenderRunSummaryOmitsUsageWhenNoCalls(t *testing.T) {
	outcome := runOutcome{docID: "d", partStatus: "failed", actorFailed: true, actorFailureDetail: "boom"}
	out := renderRunSummary(term.Profile{}, outcome, sdk.AggregateUsage{}, 0, "")
	if strings.Contains(out, "usage") {
		t.Errorf("out = %q, want no usage row when CallCount is 0", out)
	}
	if strings.Contains(out, "bundle") {
		t.Errorf("out = %q, want no bundle row when bundlePath is empty", out)
	}
}

func TestBuildAndWriteRunJSONResult(t *testing.T) {
	outcome := runOutcome{docID: "greetbot-language-onboarding", partStatus: "completed", verified: true, verifyDetail: "all good"}
	usage := sdk.AggregateUsage{InputTokens: 10, OutputTokens: 5, CallCount: 2}
	warnings := []scenario.Issue{{Code: "no-run-ceiling", Pointer: "/ceiling", Message: "no run-level ceiling is declared", Severity: scenario.SeverityWarning}}

	res := buildRunJSONResult(outcome, false, 250*time.Millisecond, usage, "out/x.chatwright.json", warnings)
	if res.Verdict != "verified" {
		t.Errorf("Verdict = %q, want %q", res.Verdict, "verified")
	}
	if res.DurationMS != 250 {
		t.Errorf("DurationMS = %d, want 250", res.DurationMS)
	}
	if res.Interrupted {
		t.Error("Interrupted = true, want false")
	}

	var buf bytes.Buffer
	if err := writeRunJSONResult(&buf, res); err != nil {
		t.Fatalf("writeRunJSONResult() error = %v", err)
	}

	// Round-trip through encoding/json directly (not this package's own
	// struct) so the test also pins the wire field NAMES, not just that
	// Go's encoder round-trips its own struct successfully.
	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	for _, field := range []string{"documentId", "partStatus", "verdict", "durationMs", "usage", "bundlePath", "warnings"} {
		if _, ok := decoded[field]; !ok {
			t.Errorf("decoded JSON missing field %q: %v", field, decoded)
		}
	}
	if decoded["documentId"] != "greetbot-language-onboarding" {
		t.Errorf("documentId = %v, want the document's own id", decoded["documentId"])
	}
	if _, ok := decoded["interrupted"]; ok {
		t.Errorf("decoded JSON has an \"interrupted\" key = %v, want it omitted (omitempty, false)", decoded["interrupted"])
	}
}

func TestFormatRejectionErrorNeverEchoesAnyExtraContent(t *testing.T) {
	rej := &scenario.RejectionError{Report: scenario.Report{Issues: []scenario.Issue{
		{Code: "inline-secret", Pointer: "/cast/0/provider/apiKey", Message: "a secret value must never be written inline; use $ref", Severity: scenario.SeverityError},
		{Code: "invalid-shape", Pointer: "/chats", Message: "at least one chat is required", Severity: scenario.SeverityError},
	}}}

	out := formatRejectionError(term.Profile{}, rej)
	if !strings.Contains(out, "2 problems found") {
		t.Errorf("out = %q, want a problem count", out)
	}
	for _, want := range []string{"/cast/0/provider/apiKey", "inline-secret", "a secret value must never be written inline; use $ref", "/chats", "invalid-shape", "at least one chat is required"} {
		if !strings.Contains(out, want) {
			t.Errorf("out = %q, want it to contain %q", out, want)
		}
	}
	// This function only ever reads Pointer/Code/Message — see its own doc
	// comment. There is no secret VALUE anywhere in this test's Issues to
	// begin with (scenario.Issue.Message is constructed by scenario's own
	// code, never from a document-supplied value — see that type's doc
	// comment), so this assertion is necessarily about form, not content:
	// a single-issue document renders as a numbered list, not a wall of
	// unstructured text.
	if !strings.Contains(out, "\n  1. ") || !strings.Contains(out, "\n  2. ") {
		t.Errorf("out = %q, want a numbered list (1. / 2.)", out)
	}
}

func TestFormatRejectionErrorSingularWording(t *testing.T) {
	rej := &scenario.RejectionError{Report: scenario.Report{Issues: []scenario.Issue{
		{Code: "invalid-shape", Pointer: "/id", Message: "id is required", Severity: scenario.SeverityError},
	}}}
	out := formatRejectionError(term.Profile{}, rej)
	if !strings.Contains(out, "1 problem found") {
		t.Errorf("out = %q, want singular \"1 problem found\"", out)
	}
	if strings.Contains(out, "1 problems") {
		t.Errorf("out = %q, want no \"1 problems\" (plural) wording", out)
	}
}

// TestRenderRunSummaryASCIIFallback guards the ASCII-mode gap found while
// manually verifying this change against a real terminal: the em dash
// (verdict detail, bundle hint) and middle dot (cost separator) this
// file's own formatting introduces must degrade under an ASCII profile
// exactly like progress.go's own separators already do — not just the
// ✓/✗/⚠ status symbol.
func TestRenderRunSummaryASCIIFallback(t *testing.T) {
	outcome := runOutcome{docID: "d", partStatus: "completed", verified: true, verifyDetail: "ok"}
	usage := sdk.AggregateUsage{InputTokens: 1, OutputTokens: 1, CallCount: 1, Cost: 0.01}
	out := renderRunSummary(term.Profile{ASCII: true}, outcome, usage, time.Second, "b.json")

	for _, want := range []string{"[OK]", "verdict   verified -- ok", "cost 0.01", "b.json -- open it"} {
		if !strings.Contains(out, want) {
			t.Errorf("ASCII summary = %q, want it to contain %q", out, want)
		}
	}
	for _, forbidden := range []rune{'—', '·', '✓'} {
		if strings.ContainsRune(out, forbidden) {
			t.Errorf("ASCII summary = %q, must not contain non-ASCII rune %q", out, forbidden)
		}
	}
}

func TestInterruptSummary(t *testing.T) {
	// A minimal hand-built Result — see runengine.Result's own shape.
	// interruptSummary only ever reads len(Parts)/len(Skipped), so this
	// stays independent of any real execution.
	t.Run("singular", func(t *testing.T) {
		got := interruptSummary(makeResult(1, 0))
		if got != "1 of 1 part finished" {
			t.Errorf("interruptSummary = %q, want %q", got, "1 of 1 part finished")
		}
	})
	t.Run("plural, some skipped", func(t *testing.T) {
		got := interruptSummary(makeResult(1, 2))
		if got != "1 of 3 parts finished" {
			t.Errorf("interruptSummary = %q, want %q", got, "1 of 3 parts finished")
		}
	})
}
