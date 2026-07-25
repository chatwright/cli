// interrupt.go gives `chatwright run` a graceful Ctrl-C (item 8 of the UX
// brief: "Ctrl-C should not lose the run"). Before this file, interrupting
// a multi-minute ai-goal run left nothing behind — no bundle, no partial
// result, just a killed process — because nothing in this repository ever
// derived a cancellable context or listened for os.Interrupt.
//
// What SIGINT actually reaches: runengine.Run.Execute's own ai-goal loop
// (chatwright.dev/runtime/actor.Loop.RunTask) passes ctx through to
// Provider.Propose on every iteration, and a Propose call that returns a
// context-cancellation error is recorded as a LoopEvent carrying that
// error in ProposeError — exactly the same shape this repository's own
// actorFailureFromParts already reads for a cassette cache miss (see
// run.go). Run.Execute itself then reports that Part as PartFailed and
// returns the Result normally, with a nil top-level error — cancellation
// never becomes a Go error runRun has to specially unwrap; it already
// arrives as an ordinary actor-unavailable-shaped outcome. This file only
// adds the *signal*; runOutcome.actorFailed's own machinery does the rest.
// A deterministic Part is not interruptible this way: runDeterministic
// takes no context at all, so Ctrl-C during a purely deterministic run has
// no earlier interception point than the process's own default signal
// disposition — a real gap, out of this CLI's reach without a
// chatwright.dev/runtime change (see this repository's own AGENTS.md: "the
// CLI is deliberately thin").
package main

import (
	"context"
	"os"
	"os/signal"
	"sync/atomic"
)

// interruptibleContext derives a cancellable context from parent that is
// cancelled the first time this process receives an interrupt (SIGINT on
// Unix; os.Interrupt is the portable spelling Go maps to the nearest
// equivalent on every platform it supports, including Windows). Returns
// the derived context, a cleanup the caller must always run (defer it) to
// stop signal delivery and release the internal goroutine, and interrupted,
// whose Load reports (after cleanup or at any point) whether the signal
// ever actually fired.
//
// A second interrupt while the first is still being handled restores the
// platform's own default disposition via signal.Stop, rather than staying
// caught forever: a user who has already asked once to stop must always be
// able to just press Ctrl-C again and have the process actually die,
// rather than discover this CLI silently swallows every further attempt.
func interruptibleContext(parent context.Context) (ctx context.Context, cleanup func(), interrupted *atomic.Bool) {
	ctx, cancel := context.WithCancel(parent)
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	done := make(chan struct{})
	fired := &atomic.Bool{}

	go func() {
		select {
		case <-sig:
			fired.Store(true)
			signal.Stop(sig) // a second Ctrl-C now terminates immediately, as if this handler were never installed.
			cancel()
		case <-done:
		}
	}()

	cleanup = func() {
		select {
		case <-done: // already closed — a repeated cleanup call (defer plus an explicit call) must never double-close.
		default:
			close(done)
		}
		signal.Stop(sig)
		cancel()
	}
	return ctx, cleanup, fired
}
