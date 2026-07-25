// run.go wires chatwright.dev/runtime/scenario into the CLI as `chatwright
// run`. It is deliberately thin per this repository's own AGENTS.md ("the
// CLI is deliberately thin ... engine or wire logic never lives here"):
// loading, validating, resolving and executing a self-contained scenario
// document (https://chatwright.dev/formats/scenario-document/v1) all live
// in chatwright.dev/runtime/scenario; this file only parses flags, calls
// into that package, assembles the resulting sdk.Bundle (the one piece
// scenario.Build deliberately leaves to its caller — a bundle is a
// bundle-only, wire-typed concept with no runtime counterpart, exactly the
// same division run.AssembleBundleRun already draws for the arena
// subcommand) and writes it to disk.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"chatwright.dev/runtime/actor"
	runengine "chatwright.dev/runtime/run"
	"chatwright.dev/runtime/scenario"
	"chatwright.dev/sdk"
)

// exitActorUnavailable is `chatwright run`'s exit code for a run whose actor
// was never able to act at all — most commonly a cassette replay cache
// miss — as opposed to exitVerificationFailed, which means the actor ran
// and the journal it produced simply didn't show what was expected. The two
// are deliberately different exit codes, not just different wording,
// mirroring the founder's own decision for AI-judged assertions (spec/
// features/chatwright/ai-driven-testing/ai-judged-assertions/README.md,
// "Open Questions … ANSWERED (founder, 2026-07-24): an undecided judgement
// exits non-zero. A broken judge is a broken test. The exit code stays
// distinct from an assertion failure so CI can tell 'the bot is broken'
// from 'the judge is unavailable'"). This command generalises the same
// reasoning to the actor side of the same seam: a harness that could not
// run is not the same exit condition as a bot that failed its checks, and
// collapsing them into one exit code would make that distinction
// unobservable from a shell script or CI job.
const exitActorUnavailable = 3

// exitVerificationFailed is the exit code for a run that executed to
// completion (or otherwise produced a journal) but whose declared
// successCriteria/verify block did not hold, or whose part status was not
// PartCompleted for a reason other than an actor failure — the pre-existing
// "outcome=not verified" case.
const exitVerificationFailed = 1

// runRun implements `chatwright run DOCUMENT [--out DIR]`.
func runRun(args []string, stdout, stderr io.Writer) int {
	// A bare "help", "-h" or "--help" as the first argument all mean the
	// same thing here — mirroring main.go's own "help"/"-h"/"--help"
	// dispatch and runArena's identical three-way case. Handling all three
	// explicitly, before flag parsing, matters for two different reasons:
	// "help" is not a flag at all, so without this check "chatwright run
	// help" would try to open a file literally named "help" instead of
	// showing usage (DOCUMENT is a required positional argument); and
	// "-h"/"--help", while flag.FlagSet.Parse does recognize them itself,
	// would otherwise print flag.FlagSet's own bare default usage (just the
	// registered flags, no description, no DOCUMENT usage line) rather than
	// this command's own printRunUsage — fs.Usage is set to printRunUsage
	// below as a backstop for the rarer case of "-h"/"--help" appearing
	// after another flag (e.g. "run --out DIR --help"), but args[0] is by
	// far the common case and is handled here without even constructing a
	// FlagSet.
	if len(args) > 0 && (args[0] == "help" || args[0] == "-h" || args[0] == "--help") {
		printRunUsage(stdout)
		return 0
	}

	fs := flag.NewFlagSet("chatwright run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { printRunUsage(stderr) }
	outDir := fs.String("out", ".", "output directory for the run bundle")
	write := fs.Bool("write", false, `write the built-in example's document and cassette into --out instead of running them (DOCUMENT must be "example")`)
	// flag.FlagSet.Parse stops at the first non-flag argument, so a flag
	// given after DOCUMENT (as this command's own usage line documents:
	// "chatwright run DOCUMENT [--out DIR]") would otherwise be rejected as
	// an extra positional argument — reorder --out/--write (in either
	// "--flag VALUE" or "--flag=VALUE" form, or bare for --write) to the
	// front first, wherever they appear.
	if err := fs.Parse(reorderRunFlagsFirst(args)); err != nil {
		return 2
	}
	if fs.NArg() == 0 {
		_, _ = fmt.Fprintln(stderr, "chatwright run: missing DOCUMENT")
		printRunUsage(stderr)
		return 2
	}
	if fs.NArg() > 1 {
		_, _ = fmt.Fprintf(stderr, "chatwright run: unexpected extra argument %q\n\n", fs.Arg(1))
		printRunUsage(stderr)
		return 2
	}
	docPath := fs.Arg(0)

	if *write {
		if docPath != exampleDocumentArg {
			_, _ = fmt.Fprintf(stderr, "chatwright run: --write is only valid with DOCUMENT %q\n\n", exampleDocumentArg)
			printRunUsage(stderr)
			return 2
		}
		if err := os.MkdirAll(*outDir, 0o755); err != nil {
			_, _ = fmt.Fprintf(stderr, "chatwright run: %v\n", err)
			return 1
		}
		written, err := materializeExample(*outDir)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "chatwright run: %v\n", err)
			return 1
		}
		_, _ = fmt.Fprintf(stdout, "wrote %s\n", written)
		_, _ = fmt.Fprintf(stdout, "wrote %s\n", filepath.Join(*outDir, exampleCassetteRelPath))
		_, _ = fmt.Fprintf(stdout, "edit the goal, then run: chatwright run %s\n", written)
		return 0
	}

	ctx := context.Background()
	// A real file on disk always wins over the built-in example: silently
	// shadowing a document the user actually has is a confusing failure, and
	// "example" is a legal filename. Only when nothing of that name exists
	// does the argument mean "run the embedded worked example".
	if docPath == exampleDocumentArg && !fileExists(docPath) {
		materialized, cleanup, err := materializeExampleTemp()
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "chatwright run: %v\n", err)
			return 1
		}
		defer cleanup()
		docPath = materialized
	}

	provider := scenario.FileScenarioProvider{}
	doc, report, err := provider.Load(ctx, docPath)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "chatwright run: %v\n", err)
		return 1
	}
	for _, w := range report.Warnings() {
		_, _ = fmt.Fprintf(stderr, "chatwright run: warning: %s (%s)\n", w.Message, w.Code)
	}

	built, err := scenario.Build(ctx, doc, scenario.BuildOptions{})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "chatwright run: %v\n", err)
		return 1
	}
	defer built.Close()

	result, err := built.Run.Execute(ctx)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "chatwright run: %v\n", err)
		return 1
	}

	b, outcome, err := assembleRunBundle(doc, built, result)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "chatwright run: %v\n", err)
		return 1
	}

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		_, _ = fmt.Fprintf(stderr, "chatwright run: %v\n", err)
		return 1
	}
	bundlePath := filepath.Join(*outDir, doc.ID+".chatwright.json")
	f, err := os.Create(bundlePath)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "chatwright run: %v\n", err)
		return 1
	}
	writeErr := sdk.Write(f, b)
	closeErr := f.Close()
	if writeErr != nil {
		_, _ = fmt.Fprintf(stderr, "chatwright run: write %s: %v\n", bundlePath, writeErr)
		return 1
	}
	if closeErr != nil {
		_, _ = fmt.Fprintf(stderr, "chatwright run: close %s: %v\n", bundlePath, closeErr)
		return 1
	}

	_, _ = fmt.Fprintf(stdout, "%s\n", outcome)
	_, _ = fmt.Fprintf(stdout, "wrote %s\n", bundlePath)
	switch {
	case outcome.actorFailed:
		// A harness failure (the actor was never driven at all) must never
		// exit the same way an executed-but-unverified run does — see
		// exitActorUnavailable's own doc comment.
		return exitActorUnavailable
	case !outcome.succeeded():
		return exitVerificationFailed
	}
	return 0
}

// runOutcome is `chatwright run`'s own human-readable summary of one
// executed document — never part of the written bundle (which carries the
// full evidence already); this is what stdout shows a developer without
// them having to open the bundle.
type runOutcome struct {
	docID, partStatus string
	// judged is true when the document declared no verify block — see the
	// format's undeclared-verification-is-never-reported-as-verified
	// acceptance criterion: such a run is NEVER printed as "verified",
	// whatever the actor itself claimed.
	judged       bool
	verified     bool
	verifyDetail string

	// actorFailed is true when the actor could not be driven at all for one
	// of this run's ai-goal parts — a LoopEvent carrying a non-empty
	// ProposeError (see actorFailureFromParts) — as opposed to the actor
	// having run and the journal it produced simply not showing what was
	// expected. This is deliberately its own field rather than folded into
	// "not verified": a harness that never got to try must never be
	// reported the same way as a bot that tried and failed its checks (see
	// exitActorUnavailable's own doc comment, and the ai-judged-assertions
	// feature's "a judge that fails must not look like a judge that
	// passed" principle, which this generalises to the actor side of the
	// same seam).
	actorFailed bool
	// actorFailureDetail is the ProposeError text itself when actorFailed —
	// already a complete, self-describing error (e.g. "actor: replay cache
	// miss: goal=... task=... observation#1 messages=0 history=0") — empty
	// otherwise.
	actorFailureDetail string
	// actorCacheMiss is true when actorFailed's cause was specifically
	// actor.ErrCassetteCacheMiss, so String can point the reader at the
	// actual fix (re-record the cassette, or run against a live provider)
	// instead of leaving them to guess from the bare error text.
	actorCacheMiss bool
}

// succeeded reports whether this run should be treated as a success for
// exit-code purposes. All three of the following must hold:
//   - the actor was never reported unavailable (see actorFailed's own doc
//     comment) — a harness failure is never a success;
//   - the part completed (partStatus == "completed");
//   - either the document declared no verify block (judged: a judged run
//     has nothing deterministic to fail) or its verify block held
//     (verified).
//
// The third condition fixes a real, adjacent bug found while verifying this
// change: this method used to check only partStatus, so a document whose
// actor ran to completion but whose verify block did NOT hold (a genuine
// bot-behaviour failure — "the bot didn't do what the scenario expected")
// exited 0, indistinguishable from a real pass. That is the same class of
// defect as the one this file's other changes fix — a failure looking like
// a success — just on the opposite side of the actor/verification line, so
// it is fixed alongside it rather than left in place.
func (o runOutcome) succeeded() bool {
	if o.actorFailed || o.partStatus != "completed" {
		return false
	}
	return o.judged || o.verified
}

// cacheMissHint is appended to runOutcome.String when actorCacheMiss is
// true — see the CLI issue this fixes (a scenario document written by
// `chatwright run example --write`, then edited, replays against a cassette
// keyed on the *unedited* document's goal text, so every Propose call is a
// guaranteed cache miss). This is a predictable first-run experience, not an
// edge case, so the message names the fix rather than just the symptom.
const cacheMissHint = `likely cause: the checked-in cassette only replays this document unedited — editing the goal (or anything else that feeds the actor's prompt) changes its replay key. Re-record the cassette against a live provider (cast member provider.mode: "record") or point provider.mode at "live" to run this edited document.`

func (o runOutcome) String() string {
	if o.actorFailed {
		line := fmt.Sprintf("%s: part status=%s, outcome=actor unavailable: %s", o.docID, o.partStatus, o.actorFailureDetail)
		if o.actorCacheMiss {
			line += "\n" + cacheMissHint
		}
		return line
	}
	verdict := "not verified"
	switch {
	case o.judged:
		verdict = "judged (no independent journal verification declared)"
	case o.verified:
		verdict = "verified"
	}
	line := fmt.Sprintf("%s: part status=%s, outcome=%s", o.docID, o.partStatus, verdict)
	if o.verifyDetail != "" {
		line += ": " + o.verifyDetail
	}
	return line
}

// actorFailureFromParts scans result's executed Parts (in execution order)
// for an ai-goal Part whose loop could not even attempt to act: a LoopEvent
// carrying a non-empty ProposeError, meaning actor.Loop.RunTask's call to
// Provider.Propose itself returned an error (see actor.LoopEvent.
// ProposeError's own doc comment — RunTask always appends that event before
// returning the error, so it is retained evidence, not a vanished Go error).
// At most one Part can ever carry such an event: RunTask returns immediately
// on the first Propose error, which fails that Part and — because a Part
// failure always aborts or coverage-gaps every remaining Part (run.Execute) —
// no later Part ever runs far enough to add one of its own.
//
// This is deliberately narrower than "the Part failed"
// (run.PartOutcome.Status == run.PartFailed also covers, say, a RecordStep
// or RecordCost error): those are genuine runtime errors about a task the
// actor *did* attempt, not evidence that the actor was never driven at all,
// and treating them the same would blur exactly the distinction this
// function exists to draw — see runOutcome.actorFailed's own doc comment.
//
// detail is the ProposeError text itself, already a complete, self-
// describing error. cacheMiss reports whether the underlying cause was
// specifically actor.ErrCassetteCacheMiss, read from the same Part's own
// PartOutcome.Err (which wraps that same error — a bare string can't be
// compared with errors.Is, so this is why both are consulted rather than
// just the LoopEvent's own string field).
func actorFailureFromParts(parts []runengine.PartOutcome) (detail string, cacheMiss bool, found bool) {
	for _, p := range parts {
		if p.AIGoal == nil {
			continue
		}
		for i := len(p.AIGoal.Events) - 1; i >= 0; i-- {
			if pe := p.AIGoal.Events[i].ProposeError; pe != "" {
				return pe, p.Err != nil && errors.Is(p.Err, actor.ErrCassetteCacheMiss), true
			}
		}
	}
	return "", false, false
}

// assembleRunBundle converts a completed scenario.Built + run.Result into
// an sdk.Bundle — the one thing scenario.Build deliberately leaves for its
// caller — and this command's own runOutcome summary.
func assembleRunBundle(doc *scenario.Document, built *scenario.Built, result runengine.Result) (sdk.Bundle, runOutcome, error) {
	chats := make([]sdk.ChatJournal, 0, len(doc.Chats))
	for _, c := range doc.Chats {
		entries, err := built.Run.Environment.Emulator.Journal(c.PlatformChatID)
		if err != nil {
			return sdk.Bundle{}, runOutcome{}, fmt.Errorf("read journal for chat %q: %w", c.ID, err)
		}
		chats = append(chats, runengine.WireJournal(c.PlatformChatID, entries))
	}

	bundleRun := runengine.AssembleBundleRun(runengine.AssembleBundleRunInput{
		RunID: doc.ID, Platform: doc.Platform, EndpointProfile: built.Fidelity.EndpointProfile,
		Actors: built.Actors, Chats: chats, Result: result,
	})

	b := sdk.Bundle{
		Format: sdk.FormatV1,
		Metadata: sdk.Metadata{
			CreatedAt:         time.Now().UTC(),
			ChatwrightVersion: sdk.ModuleVersion(),
			Author:            &sdk.Author{Name: "chatwright-cli"},
		},
		Runs: []sdk.Run{bundleRun},
	}

	outcome := runOutcome{docID: doc.ID}
	if len(result.Parts) > 0 {
		outcome.partStatus = string(result.Parts[len(result.Parts)-1].Status)
	}

	if detail, cacheMiss, found := actorFailureFromParts(result.Parts); found {
		// The actor was never driven at all, so the journal it "produced"
		// (if any) is not evidence of anything the bot did or didn't do —
		// evaluating VerifySpec against it and reporting the result as
		// "not verified" would misreport a harness failure as a failed
		// bot. See actorFailureFromParts and runOutcome.actorFailed's own
		// doc comments.
		outcome.actorFailed = true
		outcome.actorFailureDetail = detail
		outcome.actorCacheMiss = cacheMiss
		return b, outcome, nil
	}

	if built.VerifySpec == nil {
		outcome.judged = true
	} else {
		platformChatID, ok := built.ChatIDs[built.VerifySpec.ChatDocID()]
		if !ok {
			return sdk.Bundle{}, runOutcome{}, fmt.Errorf("verify.chat %q does not resolve to a declared chat", built.VerifySpec.ChatDocID())
		}
		entries, err := built.Run.Environment.Emulator.Journal(platformChatID)
		if err != nil {
			return sdk.Bundle{}, runOutcome{}, fmt.Errorf("read journal for verify: %w", err)
		}
		vr := built.VerifySpec.Evaluate(entries)
		outcome.verified, outcome.verifyDetail = vr.Verified, vr.Detail
	}

	return b, outcome, nil
}

// reorderRunFlagsFirst moves "--out"/"-out" (and its value, in either
// "--out DIR" or "--out=DIR" form) and "--write"/"-write" (bare, or
// "--write=BOOL") to the front of args, leaving every other argument —
// including DOCUMENT — in its original relative order. See runRun's own
// comment on why this command needs it and the arena subcommand (all
// flags, no positional argument) does not.
func reorderRunFlagsFirst(args []string) []string {
	var flagArgs, rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--out" || a == "-out":
			flagArgs = append(flagArgs, a)
			if i+1 < len(args) {
				i++
				flagArgs = append(flagArgs, args[i])
			}
		case strings.HasPrefix(a, "--out=") || strings.HasPrefix(a, "-out="):
			flagArgs = append(flagArgs, a)
		case a == "--write" || a == "-write":
			flagArgs = append(flagArgs, a)
		case strings.HasPrefix(a, "--write=") || strings.HasPrefix(a, "-write="):
			flagArgs = append(flagArgs, a)
		default:
			rest = append(rest, a)
		}
	}
	return append(flagArgs, rest...)
}

func printRunUsage(w io.Writer) {
	_, _ = fmt.Fprintln(w, `Load, validate, resolve and execute a self-contained scenario document
(https://chatwright.dev/formats/scenario-document/v1) — no manifest, no
registered Go scenario needed — and write the resulting run bundle, ready
to replay in the Studio player.

DOCUMENT is a path to a scenario document, or the literal word "example" to
run this CLI's own built-in worked example (GreetBot's language-onboarding
scenario) instead — no files of your own, no network call and no API key
required.

Usage:
  chatwright run DOCUMENT [--out DIR]
  chatwright run example [--out DIR]
  chatwright run example --write [--out DIR]

Flags:
  --out DIR   output directory for the run bundle, or (with --write) for
              the written example (default ".")
  --write     write the built-in example's document and cassette into
              --out instead of running them, so you can read the format,
              change the goal, and re-run it (DOCUMENT must be "example")

Examples:
  chatwright run example
      Run the built-in example straight away: no files, no network, no
      API key. Writes greetbot-language-onboarding.chatwright.json into
      "." — open it in the Studio player to see what happened.

  chatwright run example --write
      Write greetbot-language-onboarding.json (and its cassette) into "."
      instead of running it. Edit the goal, then run:
        chatwright run greetbot-language-onboarding.json`)
}
