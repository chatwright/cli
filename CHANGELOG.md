# Changelog

All notable changes to the Chatwright CLI are documented here. The format is
based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions
follow [SemVer](https://semver.org/spec/v2.0.0.html) (pre-1.0: minor versions
may break).

## Unreleased

### Added

- `chatwright server serve|start|stop|restart`: a host-side companion
  daemon so the browser Studio (served from https://chatwright.dev) can
  reach local AI model servers and verify database assertions — things a
  sandboxed HTTPS page cannot do directly. Fronts a new
  `chatwright.dev/cli/internal/server` package (`serve` runs it in the
  foreground; `start` re-execs the binary as a detached `serve` child,
  writes a PID file under `~/.chatwright/`, and redirects its stdout/
  stderr to a log file; `stop` sends SIGTERM and cleans up; `restart` is
  `stop` then `start`). Listens on `127.0.0.1:4319` by default
  (`--addr`/`CHATWRIGHT_SERVER_ADDR`). Endpoints: `GET /health` (name/
  version/capabilities), `POST /v1/chat/completions` (an OpenAI-compatible
  reverse proxy to a local Ollama/LM Studio/any OpenAI-compatible backend,
  default `http://localhost:11434/v1`, buffering a non-streaming call to
  record latency/model/token metrics and relaying a streaming call
  chunk-by-chunk), `GET /metrics` (an in-memory ring buffer of recent
  proxied calls), and `POST /datastate/query` (evaluates a JSON
  expectation DSL against `chatwright.dev/runtime/datastate`'s own Runner;
  answers an explicit `"unsupported"` verdict — never a faked pass — unless
  `--datastate-fixtures` points at a JSON file of canned rows, since real
  dalgo/DTQL execution against a live database is not wired in this
  version). Every response carries CORS/Private-Network-Access headers for
  `https://chatwright.dev` and `http://chatwright.localhost` (any port) by
  default, extensible via repeatable `--allow-origin`/
  `CHATWRIGHT_SERVER_ALLOW_ORIGIN`. `--ui-dir` optionally serves a built
  Studio UI at `/` (SPA fallback to `index.html`) alongside the API routes,
  for a future offline/local-first mode — packaging that UI is out of
  scope here.
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
