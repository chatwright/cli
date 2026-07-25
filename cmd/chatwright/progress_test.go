package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"chatwright.dev/cli/internal/term"
	"chatwright.dev/runtime/actor"
	"chatwright.dev/runtime/arena"
	"chatwright.dev/runtime/goal"
	runengine "chatwright.dev/runtime/run"
)

// --- pure, no-execution unit tests ---

func snapshotTask(phase actor.ProgressPhase, iteration int, budgets goal.Budgets, burn actor.BudgetBurn, retryCounts map[actor.ActionOutcomeKind]int) runengine.ProgressSnapshot {
	return runengine.ProgressSnapshot{
		PartID: "p1", PartIndex: 1, PartCount: 1, Phase: runengine.PartProgressTask,
		Task: &actor.ProgressSnapshot{
			Phase: phase, GoalID: "g", TaskID: "t1", TaskIndex: 1, TaskCount: 1,
			Iteration: iteration, Budgets: budgets,
			Burn:        burn,
			RetryCounts: retryCounts,
		},
	}
}

func TestActedThisStep(t *testing.T) {
	t.Run("nil prev, fresh curr detects the first action", func(t *testing.T) {
		curr := map[actor.ActionOutcomeKind]int{actor.ActionExecuted: 1}
		kind, ok := actedThisStep(nil, curr)
		if !ok || kind != actor.ActionExecuted {
			t.Fatalf("actedThisStep(nil, %v) = (%v, %v), want (executed, true)", curr, kind, ok)
		}
	})
	t.Run("no change between two identical snapshots is not an action", func(t *testing.T) {
		counts := map[actor.ActionOutcomeKind]int{actor.ActionExecuted: 2}
		if _, ok := actedThisStep(counts, counts); ok {
			t.Errorf("actedThisStep(same, same) = ok, want false — nothing increased")
		}
	})
	t.Run("detects which key increased among several", func(t *testing.T) {
		prev := map[actor.ActionOutcomeKind]int{actor.ActionExecuted: 3, actor.ActionExecutedNoEffect: 1}
		curr := map[actor.ActionOutcomeKind]int{actor.ActionExecuted: 3, actor.ActionExecutedNoEffect: 2}
		kind, ok := actedThisStep(prev, curr)
		if !ok || kind != actor.ActionExecutedNoEffect {
			t.Fatalf("actedThisStep = (%v, %v), want (executed-no-effect, true)", kind, ok)
		}
	})
}

func TestStepsDisplay(t *testing.T) {
	t.Run("budgeted: derives N/max from Burn.Steps", func(t *testing.T) {
		s := &actor.ProgressSnapshot{Iteration: 99, Budgets: goal.Budgets{MaxSteps: 12}, Burn: actor.BudgetBurn{Steps: 0.5}}
		if got := stepsDisplay(s); got != "6/12" {
			t.Errorf("stepsDisplay = %q, want %q", got, "6/12")
		}
	})
	t.Run("unbudgeted: bare iteration count", func(t *testing.T) {
		s := &actor.ProgressSnapshot{Iteration: 4}
		if got := stepsDisplay(s); got != "4" {
			t.Errorf("stepsDisplay = %q, want %q", got, "4")
		}
	})
}

func TestFormatCeilingHeader(t *testing.T) {
	t.Run("zero ceiling: nothing to show", func(t *testing.T) {
		if _, ok := formatCeilingHeader(term.Profile{}, runengine.RunCeiling{}); ok {
			t.Error("formatCeilingHeader(zero) ok = true, want false")
		}
	})
	t.Run("steps and duration, ASCII", func(t *testing.T) {
		line, ok := formatCeilingHeader(term.Profile{ASCII: true}, runengine.RunCeiling{MaxSteps: 12, MaxDuration: 4 * time.Minute})
		if !ok {
			t.Fatal("ok = false, want true")
		}
		if !strings.Contains(line, "steps <=12") || !strings.Contains(line, "duration <=4m00s") {
			t.Errorf("line = %q, want ASCII <= bounds for steps and duration", line)
		}
		if strings.ContainsRune(line, '≤') {
			t.Errorf("line = %q, must not contain a non-ASCII rune under ASCII profile", line)
		}
	})
	t.Run("cost, UTF-8", func(t *testing.T) {
		cost := 0.5
		line, ok := formatCeilingHeader(term.Profile{}, runengine.RunCeiling{MaxCost: &cost})
		if !ok {
			t.Fatal("ok = false, want true")
		}
		if !strings.Contains(line, "cost ≤0.5") {
			t.Errorf("line = %q, want a cost bound", line)
		}
	})
}

func TestRenderTaskGating(t *testing.T) {
	budgeted := goal.Budgets{MaxSteps: 10}

	t.Run("piped, non-verbose: iteration events are suppressed, boundaries are not", func(t *testing.T) {
		r := &progressReporter{profile: term.Profile{Interactive: false}, now: time.Now, startedAt: time.Now()}
		if _, show := r.render(snapshotTask(actor.ProgressIteration, 1, budgeted, actor.BudgetBurn{Steps: 0.1}, map[actor.ActionOutcomeKind]int{actor.ActionExecuted: 1})); show {
			t.Error("iteration event shown in piped, non-verbose mode — want it suppressed")
		}
		if _, show := r.render(snapshotTask(actor.ProgressTaskStarted, 0, budgeted, actor.BudgetBurn{}, nil)); !show {
			t.Error("task-started event suppressed in piped mode — want it shown (task boundary)")
		}
		if _, show := r.render(snapshotTask(actor.ProgressTaskEnded, 3, budgeted, actor.BudgetBurn{Steps: 0.3}, map[actor.ActionOutcomeKind]int{actor.ActionExecuted: 3})); !show {
			t.Error("task-ended event suppressed in piped mode — want it shown (task boundary)")
		}
	})

	t.Run("piped, verbose: iteration events are shown too", func(t *testing.T) {
		r := &progressReporter{profile: term.Profile{Interactive: false}, verbose: true, now: time.Now, startedAt: time.Now()}
		if _, show := r.render(snapshotTask(actor.ProgressIteration, 1, budgeted, actor.BudgetBurn{Steps: 0.1}, map[actor.ActionOutcomeKind]int{actor.ActionExecuted: 1})); !show {
			t.Error("iteration event suppressed with --verbose — want it shown")
		}
	})

	t.Run("interactive, non-verbose: iteration events are shown (the live redraw)", func(t *testing.T) {
		r := &progressReporter{profile: term.Profile{Interactive: true}, now: time.Now, startedAt: time.Now()}
		if _, show := r.render(snapshotTask(actor.ProgressIteration, 1, budgeted, actor.BudgetBurn{Steps: 0.1}, map[actor.ActionOutcomeKind]int{actor.ActionExecuted: 1})); !show {
			t.Error("iteration event suppressed while interactive — want it shown")
		}
	})

	t.Run("part boundaries are always shown, everywhere", func(t *testing.T) {
		for _, interactive := range []bool{false, true} {
			r := &progressReporter{profile: term.Profile{Interactive: interactive}, now: time.Now, startedAt: time.Now()}
			snap := runengine.ProgressSnapshot{Phase: runengine.PartProgressStarted, PartID: "p1", PartIndex: 1, PartCount: 2}
			if _, show := r.render(snap); !show {
				t.Errorf("part-started suppressed (interactive=%v) — want always shown", interactive)
			}
		}
	})
}

func TestRenderTaskContent(t *testing.T) {
	cost := 0.02
	snap := snapshotTask(actor.ProgressIteration, 3, goal.Budgets{MaxSteps: 12, MaxDuration: 4 * time.Minute, MaxCost: &cost},
		actor.BudgetBurn{Steps: 0.25, Cost: 0.25}, map[actor.ActionOutcomeKind]int{actor.ActionExecuted: 3})
	r := &progressReporter{profile: term.Profile{Interactive: true}, now: func() time.Time { return time.Unix(0, 0).Add(2500 * time.Millisecond) }, startedAt: time.Unix(0, 0)}
	line, show := r.render(snap)
	if !show {
		t.Fatal("show = false, want true")
	}
	for _, want := range []string{"part 1/1", `task 1/1 "t1"`, "step 3/12", "elapsed 2.5s", "duration ≤4m00s", "cost 0.005/0.02", "acted: executed"} {
		if !strings.Contains(line, want) {
			t.Errorf("line = %q, want it to contain %q", line, want)
		}
	}
}

func TestRedrawAndScroll(t *testing.T) {
	t.Run("redraw uses a bare carriage return, never a newline, and pads a shorter line", func(t *testing.T) {
		var buf bytes.Buffer
		r := &progressReporter{out: &buf, profile: term.Profile{Interactive: true}, now: time.Now, startedAt: time.Now()}
		r.Handle(runengine.ProgressSnapshot{Phase: runengine.PartProgressStarted, PartID: "part-one", PartIndex: 1, PartCount: 1})
		r.Handle(runengine.ProgressSnapshot{Phase: runengine.PartProgressCompleted, PartID: "p", PartIndex: 1, PartCount: 1})
		out := buf.String()
		if strings.Contains(out, "\n") {
			t.Errorf("redraw output contains a newline: %q — want none until Finish", out)
		}
		if !strings.Contains(out, "\r") {
			t.Errorf("redraw output = %q, want at least one carriage return", out)
		}
		r.Finish()
		if !strings.HasSuffix(buf.String(), "\n") {
			t.Errorf("after Finish, output = %q, want it to end with a newline", buf.String())
		}
	})

	t.Run("scroll (non-interactive) never emits a carriage return", func(t *testing.T) {
		var buf bytes.Buffer
		r := &progressReporter{out: &buf, profile: term.Profile{Interactive: false}, now: time.Now, startedAt: time.Now()}
		r.Handle(runengine.ProgressSnapshot{Phase: runengine.PartProgressStarted, PartID: "p", PartIndex: 1, PartCount: 1})
		r.Handle(runengine.ProgressSnapshot{Phase: runengine.PartProgressCompleted, PartID: "p", PartIndex: 1, PartCount: 1})
		out := buf.String()
		if strings.Contains(out, "\r") {
			t.Errorf("scroll output contains a carriage return: %q — want none", out)
		}
		if strings.Count(out, "\n") != 2 {
			t.Errorf("scroll output = %q, want exactly 2 newline-terminated lines", out)
		}
		r.Finish() // must be a no-op: nothing was ever redrawn.
		if buf.String() != out {
			t.Errorf("Finish() changed scroll-mode output: before=%q after=%q", out, buf.String())
		}
	})
}

// --- synthetic-slow integration test: a real actor.Loop, a real (loopback,
// in-process) telegram emulator and bot, but a hand-written Provider that
// sleeps before every reply — never a live model, never a network call
// beyond localhost. See this file's own package doc comment on why the
// brief's "construct a synthetic/slow test fixture" cannot mean a real
// scenario document here: `chatwright run`'s own document format can only
// declare a cassette/openai/anthropic provider, never an in-process Go
// value, so this exercises run.Run + progressReporter directly rather than
// through runRun's own document-loading path. ---

// slowProvider is a synthetic actor.Provider: it sends a harmless filler
// text message on every step but the last, sleeping delay before each
// reply so a caller (a human watching, or a test counting distinct
// progress frames) can observe one step at a time, then declares the task
// done. It respects ctx cancellation exactly the way a real network-backed
// provider would (a live HTTP client's request also fails once its ctx is
// cancelled), which is what makes it useful for testing
// interruptibleContext too, never just progress rendering.
type slowProvider struct {
	delay time.Duration
	steps int
	calls int
}

func (p *slowProvider) Propose(ctx context.Context, _ actor.Prompt) (actor.Proposal, actor.Usage, error) {
	select {
	case <-time.After(p.delay):
	case <-ctx.Done():
		return actor.Proposal{}, actor.Usage{}, ctx.Err()
	}
	p.calls++
	usage := actor.Usage{Model: "synthetic-demo", InputTokens: 10, OutputTokens: 2}
	if p.calls >= p.steps {
		return actor.Proposal{Kind: actor.ProposeTaskDone, Rationale: "synthetic demo: declared done"}, usage, nil
	}
	return actor.Proposal{Kind: actor.ProposeSendText, Text: fmt.Sprintf("synthetic step %d", p.calls), Rationale: "synthetic demo: keep going"}, usage, nil
}

// demoDelay is slowProvider's own per-step delay: fast by default (so the
// automated test suite — and -race — stays quick), but overridable via
// CHATWRIGHT_DEMO_DELAY for a human to actually watch the live redraw
// happen (e.g. `CHATWRIGHT_DEMO_DELAY=400ms go test -run
// TestProgressReporterAgainstSyntheticSlowRun -v ./cmd/chatwright`).
func demoDelay() time.Duration {
	if v := os.Getenv("CHATWRIGHT_DEMO_DELAY"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return 5 * time.Millisecond
}

// buildSyntheticSlowRun assembles a real run.Run (a real telegram emulator
// and greetbot instance, both loopback-only — see arena.GreetbotScenario's
// own setupGreetbot) driven by a *slowProvider instead of any real model.
// NonProgressLimit is set generously (steps+5) so the loop is guaranteed to
// run for the full step count this test asks for, regardless of whether
// greetbot's own scripted replies happen to look like "progress" to the
// non-progress detector — this test's only interest is in the loop
// producing steps steps of progress events, not in greetbot's own
// behaviour.
func buildSyntheticSlowRun(t *testing.T, steps int, delay time.Duration) (r runengine.Run, cleanup func()) {
	t.Helper()
	scenario := arena.GreetbotScenario()
	session, err := scenario.Setup()
	if err != nil {
		t.Fatalf("scenario.Setup() error = %v", err)
	}

	g := scenario.Goal
	g.Budgets = goal.Budgets{MaxSteps: steps + 5, MaxDuration: time.Hour}

	part := runengine.NewAIGoalPart(scenario.ID, scenario.Title, "", runengine.AIGoalPartInput{
		ActorID: "ai-agent", Goal: g, Provider: &slowProvider{delay: delay, steps: steps},
		Config: actor.Config{ChatID: session.ChatID, User: session.User, NonProgressLimit: steps + 5},
	})

	r = runengine.Run{
		ID:          "synthetic-slow-demo",
		Environment: runengine.Environment{Emulator: session.Emulator, ChatIDs: []int64{session.ChatID}, Now: time.Now},
		Parts:       []runengine.Part{part},
	}
	return r, session.Close
}

// TestProgressReporterAgainstSyntheticSlowRun proves progress output
// actually works on something slow enough to see: a real run.Run.Execute,
// wired to a progressReporter forced into interactive (redraw) mode,
// driven by slowProvider. Non-vacuous by construction: it asserts a
// specific minimum number of DISTINCT redrawn frames (not just "some
// output"), which fails immediately if OnProgress were disconnected or if
// the reporter collapsed every event into one line.
func TestProgressReporterAgainstSyntheticSlowRun(t *testing.T) {
	const steps = 4
	r, cleanup := buildSyntheticSlowRun(t, steps, demoDelay())
	defer cleanup()

	var buf bytes.Buffer
	reporter := newProgressReporter(&buf, term.Profile{Interactive: true}, false, time.Now)
	r.OnProgress = reporter.Handle

	result, err := r.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(result.Parts) != 1 || result.Parts[0].Status != runengine.PartCompleted {
		t.Fatalf("Parts = %+v, want exactly one PartCompleted", result.Parts)
	}

	preFinish := buf.String()
	if strings.Contains(preFinish, "\n") {
		t.Errorf("output before Finish = %q, want no newline yet — every event redrew in place", preFinish)
	}
	reporter.Finish()

	out := buf.String()
	frames := strings.Split(strings.TrimSuffix(out, "\n"), "\r")
	// frames[0] is always "" (output starts with \r) — drop it; every
	// remaining element is one distinct redrawn frame.
	nonEmpty := 0
	for _, f := range frames {
		if f != "" {
			nonEmpty++
		}
	}
	if nonEmpty < steps {
		t.Fatalf("redrew %d non-empty frames, want at least %d (one per synthetic step); output=%q", nonEmpty, steps, out)
	}
	if !strings.HasSuffix(out, "\n") {
		t.Errorf("output = %q, want Finish to leave a trailing newline", out)
	}
	if !strings.Contains(out, "acted: executed") && !strings.Contains(out, "acted: no effect") {
		t.Errorf("output = %q, want at least one derived \"acted\" indicator", out)
	}
	if !strings.Contains(out, "acted: task done") {
		t.Errorf("output = %q, want the final task-done step reported", out)
	}
}
