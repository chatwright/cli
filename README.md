# Chatwright CLI

The command-line entry point for [Chatwright](https://chatwright.dev) —
deterministic and AI-driven testing for conversational applications.

Module `chatwright.dev/cli`, binary `chatwright`. The CLI is deliberately
thin: platform emulation and the testing runtime live in
[`chatwright.dev/runtime`](https://github.com/chatwright/runtime-go), and the
run-bundle wire model in
[`chatwright.dev/sdk`](https://github.com/chatwright/sdk-go); this binary
fronts them from a terminal.

## Install

Canonical (macOS/Linux):

```sh
curl -fsSL https://chatwright.dev/install.sh | sh
```

Windows (PowerShell):

```powershell
irm https://chatwright.dev/install.ps1 | iex
```

Homebrew (macOS):

```sh
brew install --cask chatwright/tap/chatwright
```

Go-native:

```sh
go install chatwright.dev/cli/cmd/chatwright@latest
```

## Usage

```text
chatwright <command>

Commands:
  platforms     List built-in messaging platform emulators
  run           Execute a self-contained scenario document (chatwright run --help)
  arena         Run and report on the actor-model arena (chatwright arena help)
  server        Run the server companion daemon (chatwright server help)
  completion    Generate a bash/zsh/fish completion script (chatwright completion help)
  self-update   Update the installed binary in place (chatwright self-update --help);
                also available as "chatwright update"
  version       Print the CLI, runtime and sdk versions
  help          Show this help

Try it now — no files, no network, no API key:
  chatwright run example
```

`chatwright version` reports the CLI's own version plus the resolved
sdk/runtime module versions it was built against, and the supported
run-bundle format id.

### `chatwright run`

Runs a self-contained [scenario document](https://chatwright.dev/formats/scenario-document/v1)
and writes the resulting run bundle — live progress on stderr while it runs,
a scannable summary (or `--json`) once it's done:

```sh
chatwright run example                 # the built-in worked example — try this first
chatwright run my-scenario.json --out ./runs
chatwright run my-scenario.json --json --quiet   # CI-friendly: one JSON object, nothing else
chatwright run my-scenario.json --verbose        # every actor turn, not just task boundaries
```

Colour and the live progress line both respect a real terminal, `NO_COLOR`
and the `CLICOLOR`/`CLICOLOR_FORCE` conventions, and degrade to plain,
newline-terminated lines once piped or redirected. See `chatwright run --help`
for the full flag reference, the `--json` shape, and this command's exit
codes (0 verified/judged, 1 not verified, 2 usage error, 3 actor
unavailable, 130 interrupted).

### `chatwright self-update`

Updates the installed `chatwright` binary in place, or reports whether a
newer release is available — also available as `chatwright update`:

```sh
chatwright self-update --check                # report availability only; never modifies
chatwright self-update --check --format json  # machine-readable verdict
chatwright self-update --yes                  # skip the confirmation prompt (for scripts/CI/agents)
chatwright self-update --dry-run              # print the exact asset URL a real run would fetch
chatwright self-update --version v0.4.0       # install a specific release (manual installs only)
chatwright update                             # alias for self-update
```

Every safety decision — whether this install may be replaced at all,
checksum verification before extraction, the atomic swap — comes from
[`github.com/strongo/selfupdate`](https://github.com/strongo/selfupdate); see
[spec/features/self-update](spec/features/self-update/README.md) for what is
chatwright's own configuration versus the shared library's behavior. A
Homebrew-installed binary is redirected to `brew upgrade --cask chatwright`
and is never overwritten directly; a manual install (the install script, or
`go install`) is what actually gets replaced. Without `--yes` and without an
interactive terminal attached, self-update refuses rather than blocking on
input. See `chatwright self-update --help` for the full flag reference and
this command's exit codes (0 success — including a completed `--check`
whatever its verdict; 1 a runtime failure no flag fixes; 2 a usage error,
including a confirmation that was needed but neither `--yes` nor a terminal
was available).

### Shell completion

```sh
chatwright completion bash > /usr/local/etc/bash_completion.d/chatwright
chatwright completion zsh  > "${fpath[1]}/_chatwright"
chatwright completion fish > ~/.config/fish/completions/chatwright.fish
```

### Actor-model arena

Compares actor models (Ollama, LM Studio, any OpenAI-compatible endpoint)
on the same Chatwright scenario — see
[`chatwright.dev/runtime/arena`](https://github.com/chatwright/runtime-go)
and [spec/ideas/actor-model-arena.md](https://github.com/chatwright/chatwright/blob/main/spec/ideas/actor-model-arena.md)
in the standard repository:

```sh
chatwright arena run --config arena.yaml --out ./arena-run
chatwright arena report --dir ./arena-run   # recompute report.md later, no re-run
```

`arena run` writes `bundles/` (one replayable run-bundle per cell),
`report.md` (the comparison table) and `results.json` (machine-readable) into
`--out`. See [`arena.example.yaml`](arena.example.yaml) for a documented
starting config.

## The Chatwright repositories

| Repository | What it holds |
|---|---|
| [chatwright/chatwright](https://github.com/chatwright/chatwright) | The standard: specs, formats, docs |
| [chatwright/sdk-go](https://github.com/chatwright/sdk-go) | `chatwright.dev/sdk` — the run-bundle wire model |
| [chatwright/runtime-go](https://github.com/chatwright/runtime-go) | `chatwright.dev/runtime` — the engine |
| chatwright/cli (this repo) | `chatwright.dev/cli` — this CLI |
| [chatwright/studio](https://github.com/chatwright/studio) | Chatwright Studio and the chatwright.dev site |

## Licence

Apache-2.0 — see [LICENSE](LICENSE) and [NOTICE](NOTICE).

## Spec-first

Chatwright is developed spec-first with [SpecScore](https://specscore.md/) —
product specs live in the [standard repository](https://github.com/chatwright/chatwright);
this repository's own specs live under [`spec/`](spec/README.md).
