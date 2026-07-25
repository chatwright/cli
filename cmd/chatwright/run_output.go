// run_output.go is `chatwright run`'s own presentation layer: the
// scannable human summary block (replacing the old flat "part status=…,
// outcome=…" prose line), the --json machine-readable shape, and a
// readable rendering of a rejected scenario document's validation issues.
// runRun (run.go) owns orchestration and flag parsing; this file owns
// turning what it computed into what a terminal or a script actually
// reads.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"chatwright.dev/cli/internal/term"
	runengine "chatwright.dev/runtime/run"
	"chatwright.dev/runtime/scenario"
	"chatwright.dev/sdk"
)

// verdictWord is runOutcome's own one-word verdict, shared by the human
// summary and --json's own "verdict" field so the two presentations can
// never describe two different outcomes for the same run — only render
// the same one differently. One of "verified", "judged", "not verified"
// or "actor unavailable".
func (o runOutcome) verdictWord() string {
	switch {
	case o.actorFailed:
		return "actor unavailable"
	case o.judged:
		return "judged"
	case o.verified:
		return "verified"
	default:
		return "not verified"
	}
}

// verdictDetail is verdictWord's accompanying explanatory sentence — the
// same text runOutcome.String used to append, factored out so both the
// human summary and --json's own "detail" field read it from one place.
func (o runOutcome) verdictDetail() string {
	switch {
	case o.actorFailed:
		return o.actorFailureDetail
	case o.judged:
		return "no independent journal verification declared"
	default:
		return o.verifyDetail
	}
}

// verdictTone classifies verdictWord for the human summary's own colour
// choice: genuinely good (verified), weaker-but-not-a-failure (judged), or
// a real failure (not verified / actor unavailable).
func (o runOutcome) verdictTone() actionTone {
	switch {
	case o.actorFailed:
		return toneBad
	case o.judged:
		return toneWarn
	case o.verified:
		return toneGood
	default:
		return toneBad
	}
}

// aggregateUsage sums the token/cost/call-count usage of every ai-goal
// Part across every Run in b — for this CLI's own single-Run,
// single-ai-goal-Part-in-practice bundles that is just that one Part's own
// sdk.AggregateUsage, but summing rather than indexing [0] keeps this
// correct if a future document ever declares more than one ai-goal Part
// (see sdk.Run.Parts' own doc comment: "today's writers always produce
// exactly one … the ordered-list shape is what a future hybrid run …
// composes without any schema change").
func aggregateUsage(b sdk.Bundle) sdk.AggregateUsage {
	var total sdk.AggregateUsage
	for _, run := range b.Runs {
		for _, part := range run.Parts {
			if part.AIGoal == nil {
				continue
			}
			u := part.AIGoal.Report.Usage
			total.InputTokens += u.InputTokens
			total.OutputTokens += u.OutputTokens
			total.Cost += u.Cost
			total.CallCount += u.CallCount
		}
	}
	return total
}

// runSummaryLabelWidth is the column every "label   value" line in
// renderRunSummary aligns its value to: the widest fixed label ("duration",
// 8 runes) plus a 2-space gutter, so no label ever butts directly up
// against its own value.
const runSummaryLabelWidth = 10

// renderRunSummary is `chatwright run`'s scannable human-readable summary
// block — item 4 of the UX brief this replaces the old single flat
// "<id>: part status=<status>, outcome=<verdict>: <detail>" prose line
// with: a status line, then aligned label/value rows for the verdict,
// duration, token/cost usage and the bundle path — colourised per
// profile.Color, symbols chosen per profile.Symbols (UTF-8 or ASCII).
func renderRunSummary(profile term.Profile, outcome runOutcome, usage sdk.AggregateUsage, duration time.Duration, bundlePath string) string {
	sym := profile.Symbols()
	var b strings.Builder

	statusSymbol, statusTone := sym.Check, toneGood
	statusWord := "completed"
	if outcome.partStatus != "completed" {
		statusSymbol, statusTone = sym.Cross, toneBad
		statusWord = outcome.partStatus
		if statusWord == "" {
			statusWord = "unknown"
		}
	}

	fmt.Fprintf(&b, "RUN %s\n", profile.Bold(outcome.docID))
	fmt.Fprintf(&b, "  %s %s\n", colorTone(profile, statusSymbol, statusTone), statusWord)
	row(&b, "verdict", colorTone(profile, outcome.verdictWord(), outcome.verdictTone())+detailSuffix(profile, outcome.verdictDetail()))
	if outcome.actorCacheMiss {
		row(&b, "hint", cacheMissHint)
	}
	row(&b, "duration", term.FormatDuration(duration))
	if usage.CallCount > 0 {
		usageLine := fmt.Sprintf("%d in / %d out tokens (%d call", usage.InputTokens, usage.OutputTokens, usage.CallCount)
		if usage.CallCount != 1 {
			usageLine += "s"
		}
		usageLine += ")"
		if usage.Cost > 0 {
			usageLine += fmt.Sprintf("%scost %.4g", midDot(profile), usage.Cost)
		}
		row(&b, "usage", usageLine)
	}
	if bundlePath != "" {
		row(&b, "bundle", bundlePath+emdash(profile)+"open it in the Studio player to see what happened")
	}
	return b.String()
}

// row writes one aligned "  label  value" line to b — renderRunSummary's
// own small layout primitive.
func row(b *strings.Builder, label, value string) {
	fmt.Fprintf(b, "  %-*s%s\n", runSummaryLabelWidth, label, value)
}

// detailSuffix renders " <emdash> detail" when detail is non-empty, ""
// otherwise — renderRunSummary's own convention for attaching a verdict's
// explanatory sentence, mirroring the old runOutcome.String's ": detail"
// convention closely enough that a reader familiar with the old format
// isn't surprised, without literally keeping its punctuation.
func detailSuffix(profile term.Profile, detail string) string {
	if detail == "" {
		return ""
	}
	return emdash(profile) + detail
}

// emdash and midDot are renderRunSummary's own ASCII-aware punctuation —
// the same concern, and the same " -- "/" · " choice, as progress.go's
// sep/le: a terminal that cannot render UTF-8 cannot render an em dash or
// a middle dot correctly either, not just the ✓/✗/⚠ status symbols the UX
// brief names explicitly.
func emdash(profile term.Profile) string {
	if profile.ASCII {
		return " -- "
	}
	return " — "
}

func midDot(profile term.Profile) string {
	if profile.ASCII {
		return " - "
	}
	return " · "
}

// colorTone applies profile's colour for tone to s — the one place
// renderRunSummary and progress.go's describeActed agree on what each
// actionTone means visually (toneGood green, toneWarn yellow, toneBad
// red, toneNeutral unstyled).
func colorTone(profile term.Profile, s string, tone actionTone) string {
	switch tone {
	case toneGood:
		return profile.Green(s)
	case toneWarn:
		return profile.Yellow(s)
	case toneBad:
		return profile.Red(s)
	default:
		return s
	}
}

// runJSONResult is the machine-readable shape `chatwright run --json`
// writes as the ONLY content on stdout in place of renderRunSummary — see
// printRunUsage's own documentation of this exact shape, which is this
// format's one and only spec: there is no separate schema file, and a
// breaking change to this struct's JSON tags is a breaking change to that
// documentation and must update both together.
type runJSONResult struct {
	// DocumentID is the scenario document's own declared id.
	DocumentID string `json:"documentId"`
	// PartStatus is the last executed Part's own status string (e.g.
	// "completed", "failed") — runengine.PartStatus's wire values verbatim.
	PartStatus string `json:"partStatus"`
	// Verdict is one of "verified", "judged", "not-verified" or
	// "actor-unavailable" — see runOutcome.verdictWord (hyphenated here,
	// unlike that method's own space-separated human text, so a consumer
	// can treat it as a single JSON-friendly token/enum value).
	Verdict string `json:"verdict"`
	// Detail is Verdict's accompanying explanation — the matched
	// expectation's metDetail/unmetDetail text, the actor's own
	// ProposeError, or "no independent journal verification declared" for
	// Verdict "judged". Never a secret: this is always runOutcome's own
	// derived text, never anything read back out of the scenario
	// document's declared fields.
	Detail string `json:"detail,omitempty"`
	// ActorCacheMiss is true when Verdict is "actor-unavailable" and the
	// specific cause was a cassette replay cache miss — see
	// runOutcome.actorCacheMiss and cacheMissHint.
	ActorCacheMiss bool `json:"actorCacheMiss,omitempty"`
	// Interrupted is true when this run was cut short by an interrupt
	// signal (Ctrl-C) rather than running to its own natural conclusion —
	// see interruptibleContext. PartStatus/Verdict still describe
	// whatever the run actually reached before being interrupted; a
	// consumer that wants to distinguish "the bot failed" from "the user
	// stopped the run" must check this field, not just Verdict.
	Interrupted bool `json:"interrupted,omitempty"`
	// DurationMS is how long Execute actually ran, in whole milliseconds,
	// per the same clock the run itself used
	// (built.Run.Environment.Now) — never wall-clock time.Now, so this
	// is reproducible replaying the same document against the same
	// cassette.
	DurationMS int64 `json:"durationMs"`
	// Usage sums every ai-goal Part's own token/cost/call-count spend —
	// see aggregateUsage. The zero value (an all-zero AggregateUsage)
	// means either a purely deterministic run or an actor-unavailable run
	// that never got as far as its first Propose call — never omitted,
	// so a consumer can always read Usage.CallCount without a presence
	// check.
	Usage sdk.AggregateUsage `json:"usage"`
	// BundlePath is where the run bundle was written, relative to the
	// working directory `chatwright run` was invoked from — empty only
	// when the run never reached the point of writing one (a harness
	// error before assembleRunBundle; --json's own top-level "error"
	// field, not this struct, covers that case — see runRun).
	BundlePath string `json:"bundlePath,omitempty"`
	// Warnings is every SeverityWarning scenario.Issue the document's own
	// validation reported (e.g. "no-run-ceiling") — always populated
	// (never affected by --quiet, which only suppresses the human-text
	// stderr rendering of the same warnings), so a CI consumer can act on
	// them without re-parsing stderr.
	Warnings []scenario.Issue `json:"warnings,omitempty"`
}

// verdictJSONWord maps runOutcome.verdictWord's space-separated human text
// to --json's own hyphenated token vocabulary.
func verdictJSONWord(word string) string {
	switch word {
	case "not verified":
		return "not-verified"
	case "actor unavailable":
		return "actor-unavailable"
	default:
		return word // "verified", "judged"
	}
}

// buildRunJSONResult assembles runJSONResult from everything runRun
// computed for one executed run — see runJSONResult's own field
// comments for what each argument becomes.
func buildRunJSONResult(outcome runOutcome, interrupted bool, duration time.Duration, usage sdk.AggregateUsage, bundlePath string, warnings []scenario.Issue) runJSONResult {
	return runJSONResult{
		DocumentID:     outcome.docID,
		PartStatus:     outcome.partStatus,
		Verdict:        verdictJSONWord(outcome.verdictWord()),
		Detail:         outcome.verdictDetail(),
		ActorCacheMiss: outcome.actorCacheMiss,
		Interrupted:    interrupted,
		DurationMS:     duration.Milliseconds(),
		Usage:          usage,
		BundlePath:     bundlePath,
		Warnings:       warnings,
	}
}

// writeRunJSONResult encodes res to out as indented JSON (2-space, the
// same convention `chatwright run --json` documents in printRunUsage) —
// deliberately pretty-printed rather than a single compact line: it is
// still perfectly valid input to `jq` or any JSON parser, and printed
// stand-alone (never interleaved with any other stdout content — see
// runRun, which suppresses the human summary entirely under --json) it
// costs nothing to also be readable directly in a terminal.
func writeRunJSONResult(out io.Writer, res runJSONResult) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(res)
}

// formatRejectionError renders a *scenario.RejectionError as a readable,
// numbered list — item 7 of the UX brief ("a JSON pointer plus a rule id,
// and can report several problems at once … present them as a readable
// list, not a wall"). It reads only Issue.Pointer/Code/Message — the same
// three fields RejectionError.Error's own plain-text rendering uses —
// never anything else off the (possibly still-invalid) Document, so the
// format's own "never echo a secret value" guarantee (already enforced by
// construction in scenario.Issue.Message — see that type's own doc
// comment) is preserved here by construction too, not by this function
// remembering to scrub anything.
func formatRejectionError(profile term.Profile, rej *scenario.RejectionError) string {
	issues := rej.Report.Errors()
	noun := "problem"
	if len(issues) != 1 {
		noun += "s"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "document rejected: %d %s found\n", len(issues), noun)
	for i, issue := range issues {
		pointer := issue.Pointer
		if pointer == "" {
			pointer = "(document)"
		}
		fmt.Fprintf(&b, "\n  %d. %s\n     %s\n", i+1, profile.Bold(pointer+"  "+issue.Code), issue.Message)
	}
	return b.String()
}

// interruptSummary renders how much of result a Ctrl-C-interrupted run
// actually reached — runRun's own stderr narration for
// interruptibleContext firing, and part of the point of handling SIGINT
// at all (item 8: "tell the user what was kept").
func interruptSummary(result runengine.Result) string {
	total := len(result.Parts) + len(result.Skipped)
	noun := "part"
	if total != 1 {
		noun += "s"
	}
	return fmt.Sprintf("%d of %d %s finished", len(result.Parts), total, noun)
}
