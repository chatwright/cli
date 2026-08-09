---
format: https://specscore.md/feature-specification
status: Draft
---

# Feature: Self-Update

> [SpecScore.**Studio**](https://specscore.studio): | [Explore](https://specscore.studio/app/github.com/chatwright/cli/spec/features/self-update?op=explore) | [Edit](https://specscore.studio/app/github.com/chatwright/cli/spec/features/self-update?op=edit) | [Ask question](https://specscore.studio/app/github.com/chatwright/cli/spec/features/self-update?op=ask) | [Request change](https://specscore.studio/app/github.com/chatwright/cli/spec/features/self-update?op=request-change) |
**Status:** Draft
**Source Ideas:** —

## Summary

`chatwright self-update` (alias `chatwright update`) brings a running
`chatwright` binary to the latest release. The behavior is not specified
here: chatwright binds the shared
[strongo/selfupdate](https://specscore.studio/app/github.com/strongo/selfupdate/spec/features/self-update?op=explore)
library, whose Feature owns install-method detection, release resolution,
checksum verification, atomic replacement, and every failure rule. This
Feature specifies only what is chatwright's own — the command surface,
chatwright's configuration of the library, and its exit-code contract.

## Synopsis

```
chatwright self-update                       # detect, then self-replace (manual) or redirect (Homebrew)
chatwright self-update --check                # report availability only; never modifies
chatwright self-update --check --format json  # machine-readable verdict
chatwright self-update --yes                  # skip the confirmation prompt (non-interactive)
chatwright self-update --dry-run              # report the exact asset URL a real run would fetch
chatwright self-update --version v0.4.0       # install a specific release (manual installs)
chatwright self-update --version 0.3.0 --allow-downgrade   # roll back
chatwright update                             # alias for `self-update`
```

## Problem

chatwright ships through the canonical `chatwright.dev/install.sh` script, a
Homebrew cask (`brew install --cask chatwright/tap/chatwright`), and
`go install chatwright.dev/cli/cmd/chatwright@latest` for anyone building
from source. None of those channels gives a running binary a first-class way
to reach the current release, and an agent driving `chatwright run` in CI has
no way to notice it is on a stale build.

The safety rules that make self-update non-trivial — whether a swap is even
allowed, checksum verification before extraction, an atomic replace — are the
same for every CLI, which is why they live in the shared library rather than
here. What is genuinely chatwright's own is small: chatwright is one of the
few consumers that publishes a Windows build alongside macOS and Linux,
chatwright's version placeholder differs from its sibling consumers', and
chatwright's exit-code contract, chosen from chatwright's own pre-existing
two-code convention rather than copied from either sibling.

## Behavior

### Command surface

#### REQ: command-and-alias

chatwright MUST expose the command as `chatwright self-update`, and MUST
accept `chatwright update` as an alias resolving to identical behavior.

#### REQ: library-provided-behavior

The command MUST obtain its behavior from `github.com/strongo/selfupdate`
rather than reimplementing it. Install-method detection, stable-release
resolution, version comparison, pinned targets and the downgrade guard,
asset download, sha256 verification before extraction, atomic replacement,
the post-swap version check, the non-interactive refusal, and the guarantee
that every failure leaves a working binary are inherited from that library's
Feature and MUST NOT be restated or reinterpreted here. A behavior change
belongs upstream, in the library, not in a chatwright-local fork.

#### REQ: flag-surface-without-cobra

chatwright MUST expose the identical flag surface
`github.com/strongo/selfupdate/cobracmd` would register for a Cobra-based
consumer — `--check`, `--yes`/`-y`, `--version <tag>`, `--allow-downgrade`,
`--dry-run`, and `--format text|json` — reached through the framework-neutral
`github.com/strongo/selfupdate/cliui` subpackage instead, because this
repository has no Cobra dependency (see AGENTS.md: "the CLI is deliberately
thin") and does not take one on for this feature. `--version` here is
`self-update`-local and MUST NOT collide with the root `chatwright version`
command, which reports build identity.

### chatwright's configuration of the library

#### REQ: chatwright-release-identity

chatwright MUST configure the library with its own release identity: the
GitHub repository `chatwright/cli` — distinct from the module's vanity import
path `chatwright.dev/cli`, which does not resolve to a GitHub repository at
all — and the binary name `chatwright`. Release-asset and checksums naming
are left at the library's own GoReleaser-shaped defaults, which already match
this project's `.goreleaser.yml` archive and checksum `name_template`s
exactly, so no override is configured.

#### REQ: chatwright-homebrew-cask

chatwright MUST configure Homebrew as its managing package manager, with the
upgrade command `brew upgrade --cask chatwright`. chatwright ships as a cask,
not a formula, so the printed command MUST carry `--cask`.

#### REQ: chatwright-platform-matrix

chatwright MUST configure exactly the platforms its `.goreleaser.yml` build
matrix publishes: `linux`, `darwin`, and `windows` on `amd64`, plus `arm64`
for `linux` and `darwin` — five platforms in total, explicitly excluding
`windows/arm64`, which `.goreleaser.yml` itself ignores. Unlike some sibling
consumers of this library, chatwright publishes a Windows build, so
`windows/amd64` MUST be included rather than omitted by default.

#### REQ: chatwright-version-identity

chatwright MUST supply the version `chatwright version` reports — the
link-time `-ldflags` stamp of a release build, otherwise the module version
Go's build info records, otherwise chatwright's own `"devel"` fallback — and
MUST declare as undetermined every string that version can take when it
identifies no release: `"devel"`, chatwright's own final fallback, and
`"(devel)"`, what the Go toolchain stamps for a binary built from an
unresolved source tree. An undeclared placeholder is compared as if it were a
real version, which reports an update available *from* a version that does
not exist. The post-swap version probe MUST use `version`, the argument that
makes `chatwright version` print a line containing the installed version.

### Exit codes

#### REQ: exit-code-mapping

The command MUST report through chatwright's own pre-existing two-code
failure convention (`1` for a runtime failure, `2` for a usage error the
caller can fix by typing something different — the same split
`run`/`arena`/`server`/`completion` already use) and MUST NOT introduce a
third. Three of the library's failure kinds are, in that same sense, usage
errors and MUST map to `2`: a non-interactive refusal (fixed by `--yes`), a
refused downgrade (fixed by `--allow-downgrade`), and an unknown `--version`
tag (fixed by a different value). Every other failure kind — ambiguous
detection, release-lookup, download, checksum, or permission failure, and an
unsupported platform — MUST map to `1`. `--check` reporting an update
available, or an undetermined current version, MUST exit `0`: it is a
read-only report, not a failure, and chatwright has no existing "general
findings" exit code to fold it into — inventing one here would give exit `1`
a different meaning for this one subcommand than it already has for every
other. This differs deliberately from both a sibling that folds "update
available" into its general failure code and one that reserves a dedicated
code for it — chatwright's own exit-code convention already had no room for
either without conflict.

## Interaction with Other Features

| Feature | Interaction |
|---|---|
| [strongo/selfupdate: Self-Update Library](https://specscore.studio/app/github.com/strongo/selfupdate/spec/features/self-update?op=explore) | Owns the behavior contract this Feature binds. chatwright is a consumer; behavior changes belong there. |

## Acceptance Criteria

### AC: canonical-and-alias

**Requirements:** self-update#req:command-and-alias, self-update#req:flag-surface-without-cobra

**Given** an installed chatwright binary
**When** the user runs `chatwright self-update --check` and, separately, `chatwright update --check`
**Then** both invocations execute the same command and produce identical output and exit code, and the full flag surface (`--check`, `--yes`/`-y`, `--version`, `--allow-downgrade`, `--dry-run`, `--format`) is accepted by both without a Cobra dependency anywhere in the CLI.

### AC: behavior-comes-from-the-library

**Requirements:** self-update#req:library-provided-behavior, self-update#req:chatwright-release-identity, self-update#req:chatwright-version-identity

**Given** the chatwright/cli source tree
**When** the self-update command is built
**Then** detection, release resolution, verification, and replacement come from `github.com/strongo/selfupdate`, chatwright supplies only its release identity, version, and undetermined placeholders, and no copy of that logic exists in chatwright's own tree.

### AC: homebrew-is-redirected-never-overwritten

**Requirements:** self-update#req:chatwright-homebrew-cask

**Given** a chatwright binary whose resolved path is inside a Homebrew Caskroom or Cellar
**When** the user runs `chatwright self-update`, including with `--yes` and with `--version <tag>`
**Then** chatwright prints `brew upgrade --cask chatwright`, exits `0`, and performs no download, no write, and no replacement.

### AC: windows-is-a-supported-platform

**Requirements:** self-update#req:chatwright-platform-matrix

**Given** a manual chatwright install on `windows/amd64`
**When** the user runs `chatwright self-update --dry-run`
**Then** the reported planned asset names a `windows/amd64` archive rather than being refused as an unsupported platform, while a hypothetical `windows/arm64` host is refused, matching `.goreleaser.yml`'s own exclusion.

### AC: chatwright-exit-codes

**Requirements:** self-update#req:exit-code-mapping

**Given** an up-to-date binary, a binary with a newer release available, a non-interactive refusal, a refused downgrade, an unknown `--version` tag, and a checksum failure
**When** the user runs `chatwright self-update` (or `--check`) in each case
**Then** the up-to-date and update-available cases both exit `0`, the non-interactive refusal, the refused downgrade, and the unknown tag each exit `2`, the checksum failure exits `1`, and no exit code outside `{0, 1, 2}` is ever returned.

## Open Questions

None at this time.

---
*This document follows the https://specscore.md/feature-specification*
