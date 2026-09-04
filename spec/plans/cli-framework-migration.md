---
format: https://specscore.md/plan-specification
status: Implemented
---
# Plan: Migrate Chatwright CLI to Cobra

**Status:** Implemented
**Reconciled:** 2026-09-04
**Source Feature:** self-update
**Date:** 2026-09-04
**Owner:** alex
**Supersedes:** —
**Parent:** backstage:cli-skills-distribution

## Summary

Replace the handwritten root switch and every `flag.NewFlagSet` parser with
one Cobra command tree. Preserve the existing Chatwright CLI journey: a user
runs the offline `run example`, inspects or writes its bundle, operates arena
and server commands, generates completion, and sees the same version and
self-update behaviour with the same stream and exit-code contracts.

## Approach

Register the complete command tree first, then pass typed flag values to the
existing thin runtime/server handlers. Cobra owns parsing, help, aliases and
completion generation; Chatwright retains execution, rendering, cancellation
and exit mapping. Compatibility tests exercise the offline example and every
leaf command from the public root entry point.

## Tasks

### Task 1: Define the Cobra command tree

**Id:** task-1
**Verifies:** self-update#ac:canonical-and-alias
**Depends-On:** —
**Status:** complete

Add the Cobra root, version/platform commands, every nested arena and server
leaf, and the self-update alias. Preserve stdout/stderr ownership and map
usage, runtime, run-result and interruption exits without duplicate errors.

### Task 2: Move flags and completion ownership to Cobra

**Id:** task-2
**Verifies:** self-update#ac:canonical-and-alias, self-update#ac:behavior-comes-from-the-library, self-update#ac:homebrew-is-redirected-never-overwritten, self-update#ac:windows-is-a-supported-platform, self-update#ac:chatwright-exit-codes
**Depends-On:** 1
**Status:** complete

Replace all handwritten flag parsing with typed Cobra options. Bind
`github.com/strongo/cli-helpers/selfupdate/cobracmd` with Chatwright's release configuration and
exit mapper. Generate bash, zsh and fish scripts through Cobra, then prove
the offline example, accepted flag order, self-update output, and generated
completion commands.

## Open Questions

None at this time.

---

## Resolution

**Reconciled Approved → Implemented outside the tracked `change-status` flow** (2 task(s) marked complete; this did not walk the legal-transition matrix).

Implemented the complete Cobra cutover with the published cli-helpers provider; final race, vet, and compatibility checks passed before the consumer PR.

Evidence: cmd/chatwright/cobra.go, cmd/chatwright/selfupdate.go, cmd/chatwright/selfupdate_test.go
*This document follows the https://specscore.md/plan-specification*
