// progress.go renders chatwright.dev/runtime/run.Run.OnProgress events as
// live terminal output for `chatwright run` — this repository's own AGENTS.md
// "the CLI is deliberately thin" applies to what this file computes (it
// reads a runengine.ProgressSnapshot and formats a line; it invents no new
// runtime state) but not to whether it exists at all: before this file,
// Run.OnProgress had zero references anywhere in this repository, so a
// multi-minute ai-goal run printed nothing until it finished, even though
// the runtime already emits everything needed for live output at every
// part boundary and every actor-loop iteration.
//
// A hard limit on what this can show, worth stating plainly because the
// obvious brief ("show what it observed and proposed") oversells the API:
// runengine.ProgressSnapshot (and the actor.ProgressSnapshot it forwards
// for an ai-goal Part) is a point-in-time GAUGE — phase, position, budget
// burn, retry counts by outcome kind — never a transcript. It carries no
// message text, no button label, no rationale for that iteration; that
// detail (actor.LoopEvent) only exists after a Part finishes, in
// sdk.AIGoalSection.Events. So this renderer cannot literally say "asked
// for English" or "clicked the button labelled X" live; the closest it can
// get is actedThisStep, which diffs two consecutive RetryCounts snapshots
// to name the ActionOutcomeKind that just changed — "executed",
// "no effect", "invalid, skipped", and so on. That is a real, derived fact
// (whether the loop's last attempt actually did something), not the
// content of what it did.
package main

import (
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"chatwright.dev/cli/internal/term"
	"chatwright.dev/runtime/actor"
	runengine "chatwright.dev/runtime/run"
)

// progressReporter turns a sequence of runengine.ProgressSnapshot values
// (delivered synchronously, on the executing goroutine — see
// runengine.Run.OnProgress's own doc comment) into terminal output. Two
// rendering modes, chosen once per line by ephemeral:
//
//   - ephemeral (profile.Interactive and not verbose): redraw a single
//     status line in place with a bare carriage return — the "watch it
//     work" experience `chatwright run`'s own pitch describes. Never used
//     outside a real terminal: a bare \r sent to a pipe or a log file
//     leaves literal, ugly control bytes in it, so this is gated on
//     Interactive, never on Color (see term.Profile.Interactive's own
//     doc comment on exactly this distinction).
//   - scrolling (anything else — piped/redirected, or --verbose): one
//     line per shown event, newline-terminated, safe for a log file and
//     complete enough to grep for --verbose's own "per-turn detail".
//
// A progressReporter is single-use: construct one per `chatwright run`
// invocation, wire Handle as the built Run's OnProgress, and call Finish
// once Execute returns (or the run is interrupted — see
// interruptibleContext in interrupt.go) so a redrawn line never bleeds
// into whatever prints next.
type progressReporter struct {
	out     io.Writer
	profile term.Profile
	verbose bool

	now       func() time.Time
	startedAt time.Time

	lastLine  string // the previously redrawn line, so a shorter redraw can blank its leftover tail
	ephemeral bool   // true once at least one redrawn (non-newline-terminated) line has been written

	// prevRetryCounts is the current task's RetryCounts as of the previous
	// snapshot this reporter saw — see actedThisStep. Reset to nil at
	// every ProgressTaskStarted (a fresh task always starts a fresh count,
	// mirroring actor.Loop.RunTask's own fresh retryCounts map).
	prevRetryCounts map[actor.ActionOutcomeKind]int
}

// newProgressReporter constructs a progressReporter writing to out (always
// stderr in practice — see runRun: progress is diagnostic narration, never
// part of the machine-readable stdout contract --json exists to guarantee)
// under profile, with now supplying both the "elapsed" clock and
// startedAt — pass the same clock the executing Run itself uses
// (built.Run.Environment.Now) so a reported elapsed time can never drift
// from the run's own notion of time.
func newProgressReporter(out io.Writer, profile term.Profile, verbose bool, now func() time.Time) *progressReporter {
	return &progressReporter{out: out, profile: profile, verbose: verbose, now: now, startedAt: now()}
}

// Handle is wired as runengine.Run.OnProgress. It never returns an error
// and never panics on an unexpected Phase (render's own default case is
// "show nothing") — a rendering bug in this file must never take down a
// run that would otherwise have completed and written its bundle.
func (r *progressReporter) Handle(snap runengine.ProgressSnapshot) {
	line, show := r.render(snap)
	if !show {
		return
	}
	if r.profile.Interactive && !r.verbose {
		r.redraw(line)
	} else {
		r.scroll(line)
	}
}

// Finish ends this reporter's output: if the last line wasn't already
// newline-terminated (redraw mode left the cursor mid-line), it writes one
// newline so whatever prints next — the run summary, an error — starts
// its own clean line. A no-op when nothing was ever redrawn (scroll mode,
// or no progress events at all, e.g. a deterministic-only run).
func (r *progressReporter) Finish() {
	if r.ephemeral {
		_, _ = fmt.Fprintln(r.out)
		r.ephemeral = false
	}
}

// render builds the line for one snapshot and whether it should be shown
// at all — see the package doc comment's rendering-mode split, and this
// method's own per-phase comments for what "shown at all" excludes: a
// piped, non-verbose run deliberately skips per-iteration lines (only
// part/task boundaries) so a CI log isn't spammed with a line per actor
// turn unless the user asked for that detail with --verbose.
func (r *progressReporter) render(snap runengine.ProgressSnapshot) (line string, show bool) {
	switch snap.Phase {
	case runengine.PartProgressStarted:
		return fmt.Sprintf("part %d/%d %q: started", snap.PartIndex, snap.PartCount, snap.PartID), true
	case runengine.PartProgressCompleted:
		return fmt.Sprintf("part %d/%d %q: finished", snap.PartIndex, snap.PartCount, snap.PartID), true
	case runengine.PartProgressTask:
		return r.renderTask(snap)
	default:
		return "", false
	}
}

// renderTask renders a PartProgressTask snapshot — see render.
func (r *progressReporter) renderTask(snap runengine.ProgressSnapshot) (string, bool) {
	t := snap.Task
	if t == nil {
		return "", false
	}
	if t.Phase == actor.ProgressIteration && !r.verbose && !r.profile.Interactive {
		// Coarse mode: a piped, non-verbose run shows task boundaries
		// (started/ended) but not every iteration in between — see the
		// package doc comment.
		return "", false
	}

	fields := []string{
		fmt.Sprintf("part %d/%d", snap.PartIndex, snap.PartCount),
		fmt.Sprintf("task %d/%d %q", t.TaskIndex, t.TaskCount, t.TaskID),
	}

	switch t.Phase {
	case actor.ProgressTaskStarted:
		fields = append(fields, "starting")
	case actor.ProgressTaskEnded:
		fields = append(fields, "task finished")
	case actor.ProgressIteration:
		fields = append(fields, "step "+stepsDisplay(t))
	}

	fields = append(fields, "elapsed "+term.FormatDuration(r.now().Sub(r.startedAt)))

	if budget := r.budgetField(t); budget != "" {
		fields = append(fields, budget)
	}
	if t.NonProgressStreak > 0 {
		fields = append(fields, r.profile.Yellow(fmt.Sprintf("non-progress %d", t.NonProgressStreak)))
	}
	if kind, acted := actedThisStep(r.prevRetryCounts, t.RetryCounts); acted {
		fields = append(fields, r.describeActed(kind))
	}
	r.prevRetryCounts = t.RetryCounts

	return strings.Join(fields, r.sep()), true
}

// budgetField renders t's own goal.Budgets ceiling — the ai-goal task's
// own duration/cost bounds, as opposed to stepsDisplay's steps bound,
// which is folded into the "step" field itself rather than repeated here.
// Returns "" when neither is budgeted (goal.Budgets' own "zero/nil means
// unlimited" convention).
func (r *progressReporter) budgetField(t *actor.ProgressSnapshot) string {
	var parts []string
	if t.Budgets.MaxDuration > 0 {
		parts = append(parts, "duration "+r.le()+term.FormatDuration(t.Budgets.MaxDuration))
	}
	if t.Budgets.MaxCost != nil {
		spend := t.Burn.Cost * *t.Budgets.MaxCost
		parts = append(parts, fmt.Sprintf("cost %.4g/%.4g", spend, *t.Budgets.MaxCost))
	}
	if len(parts) == 0 {
		return ""
	}
	return "budget: " + strings.Join(parts, ", ")
}

// stepsDisplay renders t's step position as "N/max" when the task's own
// goal.Budgets.MaxSteps is set, or a bare "N" otherwise. N is derived from
// t.Burn.Steps (a 0..1+ fraction of MaxSteps — see actor.BudgetBurn's own
// doc comment) rather than t.Iteration directly: MaxSteps budgets
// CampaignState.RecordStep calls, which count every task in the
// containing ai-goal Part, not just the current one, whereas t.Iteration
// is scoped to the current task alone — the two coincide for the common
// single-task goal (this CLI's own bundled example among them) but not in
// general, and Burn.Steps is the field actually computed against MaxSteps.
func stepsDisplay(t *actor.ProgressSnapshot) string {
	if t.Budgets.MaxSteps > 0 {
		steps := int(math.Round(t.Burn.Steps * float64(t.Budgets.MaxSteps)))
		return fmt.Sprintf("%d/%d", steps, t.Budgets.MaxSteps)
	}
	return fmt.Sprintf("%d", t.Iteration)
}

// actedThisStep reports the single actor.ActionOutcomeKind whose count in
// curr exceeds its count in prev — see actor.Loop.RunTask, which
// increments exactly one entry of its own retryCounts map
// (retryCounts[action.Kind]++) once per iteration, immediately before
// calling emitProgress. prev nil (the state at a task's own first
// snapshot, ProgressTaskStarted) is treated as all-zero, so the very
// first iteration's own action is still detected correctly. ok is false
// when nothing increased — ProgressTaskStarted itself (curr is always
// empty there), or a ProgressTaskEnded snapshot repeating the same counts
// as the ProgressIteration snapshot immediately before it (see
// actor.Loop.RunTask: several of its endTask paths emit ProgressIteration
// and ProgressTaskEnded back to back with the same retryCounts map,
// deliberately not double-counted here).
func actedThisStep(prev, curr map[actor.ActionOutcomeKind]int) (kind actor.ActionOutcomeKind, ok bool) {
	for k, v := range curr {
		if v > prev[k] {
			return k, true
		}
	}
	return "", false
}

// describeActed renders kind for a progress line, coloured by whether it
// represents genuine forward motion (green), a recorded-but-inert attempt
// (yellow) or a real failure (red) — see actor.ActionOutcomeKind's own
// doc comments for what each value means.
func (r *progressReporter) describeActed(kind actor.ActionOutcomeKind) string {
	label, tone := actionOutcomeLabel(kind)
	text := "acted: " + label
	switch tone {
	case toneGood:
		return r.profile.Green(text)
	case toneWarn:
		return r.profile.Yellow(text)
	case toneBad:
		return r.profile.Red(text)
	default:
		return text
	}
}

// actionTone classifies an actor.ActionOutcomeKind for describeActed's own
// colour choice.
type actionTone int

const (
	toneNeutral actionTone = iota
	toneGood
	toneWarn
	toneBad
)

// actionOutcomeLabel renders kind as short present-reader-facing text plus
// its actionTone. The default case (an unrecognised kind) prints the raw
// string rather than "unknown" — a future actor.ActionOutcomeKind this
// file hasn't been updated for should degrade to plain text, not a
// misleading placeholder.
func actionOutcomeLabel(kind actor.ActionOutcomeKind) (label string, tone actionTone) {
	switch kind {
	case actor.ActionExecuted:
		return "executed", toneGood
	case actor.ActionTaskCompleted:
		return "task done", toneGood
	case actor.ActionExecutedNoEffect:
		return "no effect", toneWarn
	case actor.ActionSkippedInvalid:
		return "invalid, skipped", toneWarn
	case actor.ActionResolutionFailed:
		return "resolution failed", toneBad
	case actor.ActionTaskGivenUp:
		return "gave up", toneBad
	case actor.ActionBlockedConstraintViolation:
		return "blocked (constraint)", toneBad
	case actor.ActionOvershootProbe:
		return "overshoot probe", toneNeutral
	default:
		return string(kind), toneNeutral
	}
}

// sep is the field separator render joins a line's parts with: a middle
// dot on a UTF-8-capable stream, a plain hyphen otherwise — see
// term.Profile.ASCII's own doc comment on why even a purely decorative
// character has to respect it.
func (r *progressReporter) sep() string {
	if r.profile.ASCII {
		return " - "
	}
	return " · "
}

// le renders "at most" for a budget bound: "<=" in ASCII mode, "≤"
// otherwise.
func (r *progressReporter) le() string {
	if r.profile.ASCII {
		return "<="
	}
	return "≤"
}

// redraw overwrites the previously written line in place with a bare
// carriage return — never an ANSI cursor-control escape: a plain \r plus
// trailing-space padding needs no terminal capability beyond "supports a
// carriage return," which is as close to universal as a terminal feature
// gets, and keeps this package's own "no escape sequences, only what
// term.Profile already gates" promise honest even for the one thing here
// that isn't SGR colour.
func (r *progressReporter) redraw(line string) {
	pad := len([]rune(r.lastLine)) - len([]rune(line))
	if pad < 0 {
		pad = 0
	}
	_, _ = fmt.Fprintf(r.out, "\r%s%s\r", line, strings.Repeat(" ", pad))
	r.lastLine = line
	r.ephemeral = true
}

// scroll appends line as its own newline-terminated line — used whenever
// redraw is not (see Handle) — and clears the ephemeral flag: a run that
// starts out redrawing (interactive, non-verbose) can never reach scroll
// mode mid-run (verbose and Interactive are both fixed for a whole
// progressReporter's lifetime), so this is purely defensive.
func (r *progressReporter) scroll(line string) {
	_, _ = fmt.Fprintln(r.out, line)
	r.ephemeral = false
}

// formatCeilingHeader renders a Run's own declared runengine.RunCeiling —
// the run-level aggregate budget hybrid-runs.md describes, distinct from
// any individual ai-goal Part's own goal.Budgets — as a single line printed
// once before a run starts, so a ceiling that is going to bind is visible
// up front rather than only inferable from where a run eventually stopped.
// ok is false (render nothing) when ceiling is the zero value — no
// run-level ceiling declared, already separately reported as the
// "no-run-ceiling" warning (see runRun's own warning-printing loop).
func formatCeilingHeader(profile term.Profile, ceiling runengine.RunCeiling) (line string, ok bool) {
	var parts []string
	if ceiling.MaxSteps > 0 {
		parts = append(parts, fmt.Sprintf("steps %s%d", leSymbol(profile), ceiling.MaxSteps))
	}
	if ceiling.MaxDuration > 0 {
		parts = append(parts, fmt.Sprintf("duration %s%s", leSymbol(profile), term.FormatDuration(ceiling.MaxDuration)))
	}
	if ceiling.MaxCost != nil {
		parts = append(parts, fmt.Sprintf("cost %s%.4g", leSymbol(profile), *ceiling.MaxCost))
	}
	if len(parts) == 0 {
		return "", false
	}
	return "run ceiling: " + strings.Join(parts, ", "), true
}

// leSymbol is formatCeilingHeader's own ASCII/UTF-8 "at most" choice — see
// progressReporter.le, duplicated as a free function because
// formatCeilingHeader runs before any progressReporter exists (the
// ceiling header is printed once, up front, not through the reporter's
// own per-line rendering).
func leSymbol(profile term.Profile) string {
	if profile.ASCII {
		return "<="
	}
	return "≤"
}
