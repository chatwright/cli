---
format: https://specscore.md/feature-specification
status: Implementing
---

# Feature: Chatwright product plugin content

> [SpecScore.**Studio**](https://specscore.studio): | [Explore](https://specscore.studio/app/github.com/chatwright/cli/spec/features/chatwright-product-plugin-content?op=explore) | [Edit](https://specscore.studio/app/github.com/chatwright/cli/spec/features/chatwright-product-plugin-content?op=edit) | [Ask question](https://specscore.studio/app/github.com/chatwright/cli/spec/features/chatwright-product-plugin-content?op=ask) | [Request change](https://specscore.studio/app/github.com/chatwright/cli/spec/features/chatwright-product-plugin-content?op=request-change) |
**Status:** Implementing
**Source Ideas:** —

## Summary

Publish Chatwright plugin 0.1.0 from one colocated canonical tree for agents
working with deterministic scenario runs, replayable artifacts, arena reports,
and the server companion.

## Problem

Chatwright has a supported Cobra CLI journey but no product-owned, portable
agent content. An agent therefore has no compact source of truth for running
the offline GreetBot example, retaining its replayable bundle, or choosing
between a fresh arena campaign, an offline report refresh, and server modes.

## Behavior

`ai/chatwright-cli` is the only content source. Its `skills/` directory holds
the prefixed `chatwright-scenario-runs` and `chatwright-arena-server` skills,
including the references and the arena starter asset they need. The Codex and
Claude manifests expose that same directory; the root Agent Plugins manifest
does the same for Cursor's supported portable-plugin format. No generated or
host-specific copy of a skill exists.

The scenario skill uses `chatwright run example` for the bundled deterministic
journey and keeps the resulting run bundle as the replayable Studio artifact.
The arena/server skill distinguishes a provider-backed arena run from the
offline `arena report` refresh, and documents foreground versus daemon server
control. `chatwright skills sync` binds this tree to the shared
`github.com/strongo/cli-helpers/skillsync` engine. Ordinary sync uses the
immutable bundle embedded in the installed CLI and stays offline; only the
explicit `--newer-compatible` mode may resolve a newer compatible release.

## Dependencies

- self-update

## Acceptance Criteria

### AC: one-canonical-plugin-tree

**Given** the colocated Chatwright plugin
**When** its Agent Plugins, Codex, and Claude manifests are inspected
**Then** every native host resolves the same `skills/` tree, all referenced
files are inside that tree, and the initial Chatwright plugin 0.1.0 identity
is consistent across manifests.

### AC: deterministic-scenario-journey

**Given** an installed Chatwright binary and an empty working directory
**When** an agent follows `chatwright-scenario-runs`
**Then** `chatwright run example --json` completes without a network or API
key, produces one valid JSON outcome and a replayable run bundle, and the
editable example path remains available through `--write`.

### AC: arena-and-server-workflow

**Given** a configured local model provider or an existing arena output
**When** an agent follows `chatwright-arena-server`
**Then** it uses the packaged arena config as a starting point, refreshes an
existing report without re-running a model, and selects foreground or daemon
server control with the documented Cobra flags.

### AC: installed-cli-matched-sync

**Given** an installed Chatwright CLI and a supported harness skills directory
**When** the user runs `chatwright skills sync`
**Then** the shared cli-helpers engine installs the digest-verified immutable
bundle embedded in that exact CLI build without reading a source checkout or
the network, a repeated sync is idempotent, and a newer compatible release is
considered only when `--newer-compatible` is explicitly supplied.

## Open Questions

None at this time.

---
*This document follows the https://specscore.md/feature-specification*
