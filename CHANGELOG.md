# Changelog

All notable changes to the Chatwright CLI are documented here. The format is
based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions
follow [SemVer](https://semver.org/spec/v2.0.0.html) (pre-1.0: minor versions
may break).

## Unreleased

### Added

- `chatwright arena run --config arena.yaml --out DIR` and
  `chatwright arena report --dir DIR`, fronting
  [`chatwright.dev/runtime/arena`](https://github.com/chatwright/runtime-go)
  per [spec/ideas/actor-model-arena.md](https://github.com/chatwright/chatwright/blob/main/spec/ideas/actor-model-arena.md):
  `run` executes an `arena.Matrix` built from a YAML config
  (providers/models/repeats/budgets) and writes `bundles/`, `report.md` and
  a machine-readable `results.json` into `DIR`; `report` recomputes
  `report.md` from a prior run's `results.json` (warning, never failing,
  about any bundle it can no longer find) without re-running any model.
  `arena.example.yaml` documents the founder's own line-up (Ollama
  qwen3.6; LM Studio gemma-4-e4b, gemma-4-26b-a4b-qat, qwen3.6-27b) with
  right-sized context lengths.
- **Depends on an unreleased `chatwright.dev/runtime` tag** — this PR is a
  draft: gates ran locally against a `replace chatwright.dev/runtime =>
  ../rtg-arena` pointing at
  [runtime-go#7](https://github.com/chatwright/runtime-go/pull/7), which
  is not yet merged/tagged. The committed `go.mod` carries no replace
  directive; `go.mod`/`go.sum` need a `chatwright.dev/runtime` bump once
  that PR merges and a runtime tag exists, before this can leave draft.

## 0.1.0 — 2026-07-23

### Added

- Initial release as its own repository, extracted with history from
  `github.com/chatwright/chatwright` as part of the code-split restructuring
  (module `chatwright.dev/cli`, binary at `cmd/chatwright`).
- `chatwright version` now reports the resolved `chatwright.dev/runtime` and
  `chatwright.dev/sdk` module versions from build info alongside the CLI's
  own version, plus the supported run-bundle format id.
- `chatwright platforms` derives platform names from the linked-in runtime
  emulators rather than a hardcoded list.
- GoReleaser release flow via the shared strongo/cicd workflow: prebuilt
  binaries for Linux/macOS (amd64/arm64) and Windows (amd64), a Homebrew
  cask published to `chatwright/homebrew-tap`, and the canonical
  chatwright.dev install scripts.
