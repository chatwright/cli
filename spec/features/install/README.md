---
format: https://specscore.md/feature-specification
status: Implementing
---

# Feature: Install

> [SpecScore.**Studio**](https://specscore.studio): | [Explore](https://specscore.studio/app/github.com/chatwright/cli/spec/features/install?op=explore) | [Edit](https://specscore.studio/app/github.com/chatwright/cli/spec/features/install?op=edit) | [Ask question](https://specscore.studio/app/github.com/chatwright/cli/spec/features/install?op=ask) | [Request change](https://specscore.studio/app/github.com/chatwright/cli/spec/features/install?op=request-change) |

**Status:** Implementing
**Source Ideas:** —

## Summary

`chatwright install` lists the other fleet CLIs relevant to chatwright
(`specscore`, per the fleet catalog's relevance matrix), each with its live
installed status, and `chatwright install <name>...` installs named ones
consistently with how chatwright itself was installed. The behavior is not
specified here: chatwright binds the shared
[strongo/cli-helpers](https://specscore.studio/app/github.com/strongo/cli-helpers/spec/features/cli-install?op=explore)
library (`github.com/strongo/cli-helpers/cliinstall`), whose Feature owns the
catalog, status probing, destination policy, Homebrew-cask and direct-release
install methods, and every failure guarantee. This Feature specifies only
what is chatwright's own — the command wiring and chatwright's exit-code
mapping.

## Synopsis

```
chatwright install                              # list fleet CLIs relevant to chatwright, with live status
chatwright install --all                        # list every catalog CLI, not just the ones relevant to chatwright
chatwright install specscore                     # show details/relevance/plan for specscore, confirm once, install it
chatwright install specscore --yes                # install it, skipping the confirmation prompt
chatwright install specscore --dry-run            # report the plan without installing anything
chatwright install nosuchcli                     # refused before any confirmation, network request, or write
chatwright install --format json                 # machine-readable listing/result
```

## Problem

chatwright is used alongside sibling fleet CLIs — specscore lints and
scaffolds the SpecScore features and plans chatwright itself is specified
against, and Chatwright scenarios verify conversational behavior alongside
`specscore spec lint` — but chatwright had no way to tell a user this or help
them install a sibling CLI consistently with how they installed chatwright
itself.

This is the same problem `self-update` already solved for chatwright's own
binary: detect the install method, resolve a release, verify it, and place it
safely. Installing a *different* CLI needs the identical machinery plus a
catalog of identities and a destination policy, which now live once in
`github.com/strongo/cli-helpers/cliinstall` rather than being rederived by
every consumer.

## Behavior

### Command surface

#### REQ: command-name

The CLI MUST expose the command as `chatwright install`, taking zero or more
target-name positional arguments.

#### REQ: library-provided-behavior

The command MUST obtain its behavior from
`github.com/strongo/cli-helpers/cliinstall`'s `cobracmd.New` adapter rather
than reimplementing it. The catalog, relevance matrix, status probing,
destination policy, Homebrew-cask and direct-release install methods,
verification, confirmation gating, dry run, and batch reporting are inherited
from that library's Feature and MUST NOT be restated or reinterpreted here.
chatwright MUST NOT hand-roll its own catalog entries, process execution, or
package-manager invocation code.

#### REQ: flag-surface

The command MUST expose `--all`, `--yes` (short `-y`), `--dry-run`, `--dir`,
and `--format text|json`, bound to the library's corresponding options.

### chatwright's configuration of the library

#### REQ: chatwright-host-identity

chatwright MUST identify itself to the library by its catalog id,
`"chatwright"` (`cli-install#req:host-identity-from-catalog`) — the SAME
compiled-in catalog entry [self-update](../self-update/README.md) resolves
its own release identity from. A host id absent from the compiled catalog is
a programming error the command constructor panics on, caught by this
repository's own tests, never a runtime state a user sees.

### Exit codes

#### REQ: exit-code-contract

The command MUST map the library's typed failure kinds and its own usage
error onto chatwright's own pre-existing two-code failure convention (`1` for
a runtime failure, `2` for a usage error the caller can fix by typing
something different — the same split
[self-update](../self-update/README.md#req-exit-code-mapping) already uses):

| Outcome | Exit code |
|---|---|
| Success (including a no-op batch, e.g. every target already installed) | `0` |
| Invalid command input (`--all` combined with names, an invalid `--format`), or a name that is not a catalog id | `2` |
| No usable destination directory (the per-user bin directory is not on `PATH` and no fallback applies), or the destination is already occupied by a file the library will not overwrite | `1` |
| Every failure kind [self-update](../self-update/README.md) already maps | The same code `self-update` returns for that kind |

No message from this command carries a `self-update:` prefix, so a script
that greps for one to distinguish the two commands cannot mistake one for the
other.

## Interaction with Other Features

| Feature | Interaction |
|---|---|
| [strongo/cli-helpers: CLI Install Command Library](https://specscore.studio/app/github.com/strongo/cli-helpers/spec/features/cli-install?op=explore) | Owns the behavior contract this Feature binds. chatwright is a consumer; behavior changes belong there. |
| [Self-Update](../self-update/README.md) | Sibling command built on the same fleet catalog entry (`cliinstall.ByID("chatwright")`); `chatwright install chatwright` is reported as already installed with a `chatwright self-update` pointer rather than reinstalling. |

## Acceptance Criteria

### AC: lists-relevant-fleet-clis

**Requirements:** install#req:command-name, install#req:library-provided-behavior

**Given** an installed `chatwright` binary
**When** the user runs `chatwright install` with no arguments
**Then** the command lists the fleet CLIs relevant to chatwright (currently `specscore`), each with its live status, without any network request or filesystem write.

### AC: unknown-target-refused

**Requirements:** install#req:exit-code-contract

**Given** an installed `chatwright` binary
**When** the user runs `chatwright install nosuchcli`
**Then** the command fails before any confirmation, network request, or write, exits `2`, and the message names the unknown target and lists valid catalog ids, carrying no `self-update:` prefix.

### AC: shared-failures-map-to-selfupdate-codes

**Requirements:** install#req:exit-code-contract

**Given** a release lookup that fails for an install target
**When** the user runs `chatwright install <name> --yes`
**Then** the command exits with the same code `chatwright self-update` would return for that same underlying failure kind, and the message carries no `self-update:` prefix.

## Open Questions

- Should `chatwright install` gain the `upgrade` command's own wiring once
  `strongo/cli-helpers` ships its Cobra adapter for it? Tracked by the
  fleet-wide [cli-install Plan](https://specscore.studio/app/github.com/strongo/cli-helpers/spec/plans/cli-install?op=explore)'s
  task-22; out of scope for this round.

---
*This document follows the https://specscore.md/feature-specification*
