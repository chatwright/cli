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
// subcommand) and writes it to disk. Everything about how the run is
// *presented* — colour, the live progress line, the JSON shape, the
// summary block — lives in run_output.go and progress.go; this file owns
// orchestration only.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"chatwright.dev/cli/internal/term"
	"chatwright.dev/runtime/actor"
	runengine "chatwright.dev/runtime/run"
	"chatwright.dev/runtime/scenario"
	"chatwright.dev/sdk"
	"github.com/spf13/cobra"
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

// exitInterrupted is the exit code for a run cut short by an interrupt
// signal (Ctrl-C) — see interruptibleContext. 130 is the conventional
// Unix "terminated by signal N" code (128 + SIGINT's own number, 2), the
// same value a shell reports for its own Ctrl-C'd child processes, chosen
// deliberately over reusing exitVerificationFailed or
// exitActorUnavailable: "the user stopped this" is a third condition, not
// a specialisation of either existing failure class, and a CI script
// checking for it should never have to also rule out a genuine
// verification failure or a broken harness to do so.
const exitInterrupted = 130

// runRun implements `chatwright run DOCUMENT [flags]`.
func runRun(args []string, stdout, stderr io.Writer) int {
	return run(append([]string{"run"}, args...), stdout, stderr)
}

type runOptions struct {
	outDir                         string
	write, jsonOut, quiet, verbose bool
}

func newRunCommand() *cobra.Command {
	opts := runOptions{outDir: "."}
	cmd := &cobra.Command{Use: "run DOCUMENT", Short: "Execute a self-contained scenario document", Long: "Load and execute a self-contained scenario document, then write a replayable run bundle. The literal DOCUMENT example runs the bundled GreetBot journey without network access or an API key.", Example: `  chatwright run example
  chatwright run example --json --quiet
  chatwright run example --write --out ./my-scenario`, Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if args[0] == "help" {
			return cmd.Help()
		}
		if opts.quiet && opts.verbose {
			return fmt.Errorf("--quiet and --verbose are mutually exclusive")
		}
		return commandResult(executeRun(args[0], opts, cmd.OutOrStdout(), cmd.ErrOrStderr()))
	}}
	cmd.Flags().StringVar(&opts.outDir, "out", ".", "output directory for the run bundle")
	cmd.Flags().BoolVar(&opts.write, "write", false, "write the built-in example into --out instead of running it")
	cmd.Flags().BoolVar(&opts.jsonOut, "json", false, "emit the run outcome as JSON")
	cmd.Flags().BoolVar(&opts.quiet, "quiet", false, "print nothing on a successful run")
	cmd.Flags().BoolVar(&opts.verbose, "verbose", false, "show every actor loop iteration")
	return cmd
}

func executeRun(docPath string, opts runOptions, stdout, stderr io.Writer) int {

	if opts.write {
		if docPath != exampleDocumentArg {
			_, _ = fmt.Fprintf(stderr, "chatwright run: --write is only valid with DOCUMENT %q\n\n", exampleDocumentArg)
			printRunUsage(stderr)
			return 2
		}
		if err := os.MkdirAll(opts.outDir, 0o755); err != nil {
			_, _ = fmt.Fprintf(stderr, "chatwright run: %v\n", err)
			return 1
		}
		written, err := materializeExample(opts.outDir)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "chatwright run: %v\n", err)
			return 1
		}
		_, _ = fmt.Fprintf(stdout, "wrote %s\n", written)
		_, _ = fmt.Fprintf(stdout, "wrote %s\n", filepath.Join(opts.outDir, exampleCassetteRelPath))
		_, _ = fmt.Fprintf(stdout, "edit the goal, then run: chatwright run %s\n", written)
		return 0
	}

	profileOut := detectProfile(stdout, os.Getenv)
	profileErr := detectProfile(stderr, os.Getenv)

	// See interrupt.go's own package doc comment for exactly what pressing
	// Ctrl-C during Execute below actually does, and what it deliberately
	// does not (a purely deterministic Part has no ctx-aware interception
	// point at all).
	ctx, cancelInterrupt, interrupted := interruptibleContext(context.Background())
	defer cancelInterrupt()

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
		var rej *scenario.RejectionError
		if errors.As(err, &rej) {
			_, _ = fmt.Fprintf(stderr, "chatwright run: %s", formatRejectionError(profileErr, rej))
		} else {
			_, _ = fmt.Fprintf(stderr, "chatwright run: %v\n", err)
		}
		return 1
	}
	if !opts.quiet {
		for _, w := range report.Warnings() {
			_, _ = fmt.Fprintf(stderr, "chatwright run: warning: %s (%s)\n", w.Message, w.Code)
		}
	}

	built, err := scenario.Build(ctx, doc, scenario.BuildOptions{})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "chatwright run: %v\n", err)
		return 1
	}
	defer built.Close()

	var reporter *progressReporter
	if !opts.quiet {
		if line, ok := formatCeilingHeader(profileErr, built.Run.Ceiling); ok {
			_, _ = fmt.Fprintln(stderr, line)
		}
		reporter = newProgressReporter(stderr, profileErr, opts.verbose, built.Run.Environment.Now)
		built.Run.OnProgress = reporter.Handle
	}

	// duration is real wall-clock time, deliberately never
	// built.Run.Environment.Now (the clock Execute itself uses for budget/
	// ceiling accounting): scenario.Build freezes that clock for a
	// cassette-only document — see scenario.defaultClock — precisely so a
	// replayed run's timestamps are reproducible, which would make this
	// summary's own "duration" a permanently fixed 0ms for the very
	// documents (the shipped example among them) most users try first. The
	// live progress reporter's own "elapsed" gauge intentionally stays on
	// built.Run.Environment.Now (see newProgressReporter's own doc
	// comment) — it exists to track budget burn consistently with how
	// Execute itself measures it, a different concern from "how long did
	// this actually take on my machine."
	wallStart := time.Now()
	result, err := built.Run.Execute(ctx)
	duration := time.Since(wallStart)
	if reporter != nil {
		// Finish before anything else touches stdout or stderr: a redrawn
		// progress line left mid-line (no trailing newline yet) would
		// otherwise have the next thing printed — an error, the summary —
		// appended straight onto its tail on a real terminal, since stdout
		// and stderr share the same physical screen even though they are
		// different streams.
		reporter.Finish()
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "chatwright run: %v\n", err)
		return 1
	}

	b, outcome, err := assembleRunBundle(doc, built, result)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "chatwright run: %v\n", err)
		return 1
	}

	if err := os.MkdirAll(opts.outDir, 0o755); err != nil {
		_, _ = fmt.Fprintf(stderr, "chatwright run: %v\n", err)
		return 1
	}
	bundlePath := filepath.Join(opts.outDir, doc.ID+".chatwright.json")
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

	wasInterrupted := interrupted.Load()
	if wasInterrupted {
		_, _ = fmt.Fprintf(stderr, "chatwright run: interrupted — %s; kept what was captured\n", interruptSummary(result))
	}

	usage := aggregateUsage(b)
	switch {
	case opts.jsonOut:
		// --json's own contract (documented in printRunUsage): exactly one
		// JSON object on stdout, never suppressed by --quiet — the human
		// summary below is skipped entirely instead, never both.
		if err := writeRunJSONResult(stdout, buildRunJSONResult(outcome, wasInterrupted, duration, usage, bundlePath, report.Warnings())); err != nil {
			_, _ = fmt.Fprintf(stderr, "chatwright run: encode result: %v\n", err)
			return 1
		}
	case !opts.quiet || !outcome.succeeded() || wasInterrupted:
		// --quiet's own contract: silence on an unremarkable success: a
		// failure (verification failed, actor unavailable) or an
		// interrupted run is still reported, exactly as if --quiet had not
		// been given — "errors only" including the interruption itself.
		_, _ = fmt.Fprint(stdout, renderRunSummary(profileOut, outcome, usage, duration, bundlePath))
	}

	return runExitCode(wasInterrupted, outcome)
}

// runExitCode is `chatwright run`'s own exit-code decision, factored out
// of runRun as a pure function so its priority ordering (interrupted
// outranks actor-unavailable outranks not-verified outranks success) is
// directly unit-testable without an actual run — see printRunUsage's own
// "Exit codes" section, which this function's four return values are the
// single source of truth for.
func runExitCode(wasInterrupted bool, outcome runOutcome) int {
	switch {
	case wasInterrupted:
		return exitInterrupted
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

// detectProfile computes a term.Profile for w: a real *os.File (os.Stdout
// or os.Stderr, as main always passes) is checked for both terminal-ness
// and, via getenv, the NO_COLOR/CLICOLOR/CLICOLOR_FORCE/locale
// conventions; anything else (a bytes.Buffer, as every test in this
// package passes) is never a terminal — the correct, deterministic answer
// for a test, and also the conservative, plain-text-only default a real
// but unusual destination (a socket, say) should get too.
func detectProfile(w io.Writer, getenv func(string) string) term.Profile {
	f, _ := w.(*os.File)
	return term.NewProfile(f, getenv)
}

// runOutcome is `chatwright run`'s own human-readable summary of one
// executed document — never part of the written bundle (which carries the
// full evidence already); this is what stdout shows a developer without
// them having to open the bundle. See run_output.go for how it is
// rendered, both as the human summary block and as --json's own shape.
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
	// actor.ErrCassetteCacheMiss, so the summary can point the reader at the
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

// cacheMissHint is appended to the human summary (and available via
// runOutcome.actorCacheMiss to --json's own consumer) when actorCacheMiss
// is true — see the CLI issue this fixes (a scenario document written by
// `chatwright run example --write`, then edited, replays against a
// cassette keyed on the *unedited* document's goal text, so every Propose
// call is a guaranteed cache miss). This is a predictable first-run
// experience, not an edge case, so the message names the fix rather than
// just the symptom.
const cacheMissHint = `likely cause: the checked-in cassette only replays this document unedited — editing the goal (or anything else that feeds the actor's prompt) changes its replay key. Re-record the cassette against a live provider (cast member provider.mode: "record") or point provider.mode at "live" to run this edited document.`

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
// A cancelled context (see interrupt.go) surfaces exactly the same way —
// Provider.Propose returning a context-cancellation error is just another
// ProposeError as far as this function is concerned, which is why an
// interrupted run's PartStatus/Verdict still make sense on their own, even
// though runRun's own exitInterrupted always takes priority over
// exitActorUnavailable for the actual exit code.
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
  chatwright run DOCUMENT [--out DIR] [--json | --quiet | --verbose]
  chatwright run example [--out DIR]
  chatwright run example --write [--out DIR]

Flags:
  --out DIR   output directory for the run bundle, or (with --write) for
              the written example (default ".")
  --write     write the built-in example's document and cassette into
              --out instead of running them, so you can read the format,
              change the goal, and re-run it (DOCUMENT must be "example")
  --json      emit the run outcome as a single JSON object on stdout in
              place of the human summary — never suppressed by --quiet.
              Shape (fields omitted when zero-valued):
                documentId      string   the document's own "id"
                partStatus      string   e.g. "completed", "failed"
                verdict         string   "verified" | "judged" |
                                         "not-verified" | "actor-unavailable"
                detail          string   the verdict's own explanation
                actorCacheMiss  bool     verdict actor-unavailable was a
                                         cassette replay cache miss
                interrupted     bool     the run was cut short by Ctrl-C
                durationMs      int      Execute's own wall-clock duration
                usage           object   inputTokens/outputTokens/cost/
                                         callCount, summed across every
                                         ai-goal part (always present, may
                                         be all zero)
                bundlePath      string   where the run bundle was written
                warnings        array    every validation warning
                                         (code/pointer/message/severity),
                                         same shape regardless of --quiet
  --quiet     print nothing on a successful run; a failure (verification
              failed, actor unavailable) or an interrupted run is still
              reported — errors only. Never suppresses --json.
  --verbose   show every actor loop iteration on stderr as it happens
              (task boundaries only otherwise, once a run is piped rather
              than watched live). Mutually exclusive with --quiet.

Exit codes:
  0    the run completed and its declared outcome held (verified, or
       judged when the document declares no verify block)
  1    the run executed but its declared outcome did not hold (a genuine
       bot-behaviour failure — "not verified"), or a harness-level error
       (a bad document path, a write failure) unrelated to the actor
  2    usage error: a bad flag or a missing/extra argument — the run
       never started
  3    the actor could not be driven at all (most commonly a cassette
       replay cache miss) — a harness failure, never conflated with a
       verification failure (see exitActorUnavailable's own doc comment)
  130  the run was interrupted (Ctrl-C) before reaching its own
       conclusion — the conventional Unix "terminated by signal 2" code

Examples:
  chatwright run example
      Run the built-in example straight away: no files, no network, no
      API key. Writes greetbot-language-onboarding.chatwright.json into
      "." — open it in the Studio player to see what happened.

  chatwright run example --write
      Write greetbot-language-onboarding.json (and its cassette) into "."
      instead of running it. Edit the goal, then run:
        chatwright run greetbot-language-onboarding.json

  chatwright run example --json --quiet
      Run the example with zero human-oriented output: no progress line,
      no warnings — just the JSON result on stdout, for a CI job to parse.`)
}
