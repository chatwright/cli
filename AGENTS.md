# chatwright/cli — instructions for AI agents and humans

This repository holds the Chatwright CLI: module `chatwright.dev/cli`, binary
at `cmd/chatwright`. The conventions of the standard repository apply — read
[chatwright/chatwright's AGENTS.md](https://github.com/chatwright/chatwright/blob/main/AGENTS.md)
first.

- The CLI is deliberately thin: it fronts `chatwright.dev/runtime` and
  `chatwright.dev/sdk`; engine or wire logic never lives here.
- `chatwright version` reports the CLI's own version plus the resolved
  sdk/runtime versions from build info — keep that contract.
- Go: `gofmt` clean, `go vet ./...`, `go test -race ./...` before pushing.
- Docs use British English; Go code/comments may use American English; never
  mixed within a file.
- Releases are deliberate: an annotated `vX.Y.Z` tag pushed by a maintainer
  triggers GoReleaser via the shared strongo/cicd release workflow (see
  `.github/workflows/release.yml` and `.goreleaser.yml`). The canonical
  install is the chatwright.dev install script; the Homebrew cask and
  `go install chatwright.dev/cli/cmd/chatwright@latest` are alternatives.
