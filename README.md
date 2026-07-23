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

Homebrew (macOS — not yet published; the cask activates with a future
release):

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
  platforms   List built-in messaging platform emulators
  version     Print the CLI, runtime and sdk versions
  help        Show this help
```

`chatwright version` reports the CLI's own version plus the resolved
sdk/runtime module versions it was built against, and the supported
run-bundle format id.

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
