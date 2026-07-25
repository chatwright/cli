# Changelog

All notable changes to the Chatwright CLI are documented here. The format is
based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions
follow [SemVer](https://semver.org/spec/v2.0.0.html) (pre-1.0: minor versions
may break).

## Unreleased

### Added

- `chatwright run DOCUMENT [--out DIR]`: execute a self-contained scenario
  document (`https://chatwright.dev/formats/scenario-document/v1`,
  `chatwright.dev/runtime/scenario`) with no manifest and no registered Go
  scenario — load, validate, resolve (secrets, an example bot or an HTTP
  bot transport, a cassette provider), run, and write the resulting
  `.chatwright.json` run bundle to `--out` (default `.`). Prints a
  one-line outcome: the executed part's status and, when the document
  declares a `verify` block, its journal-verified verdict and detail
  string — never printed as "verified" when the document declares no
  `verify` block, only "judged" (the format's own judged-versus-verified
  rule). Requires the `scenario` package from
  [chatwright/runtime-go#12](https://github.com/chatwright/runtime-go/pull/12),
  intended to release as `chatwright.dev/runtime` v0.4.0 — until that PR is
  reviewed, merged and tagged, `go.mod` pins a pseudo-version of its branch
  HEAD rather than v0.4.0 itself (see this repository's own PR description
  for the exact commit).

## 0.2.0 — 2026-07-25

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
  scope here (see `--ui` below, which closes that gap).
- `GET /v1/models` on `chatwright server`: proxies the upstream's
  OpenAI-compatible model list (Ollama and LM Studio both expose it), so the
  Studio's **Local AI** mode can offer a dropdown of the models actually
  present on the machine instead of a free-text field. Strips the browser
  `Origin` and fetch-metadata headers before the upstream call — the same
  treatment `/v1/chat/completions` already applies — so the upstream's own
  CORS does not reject it with a 403.
- `chatwright server --ui`: downloads, verifies, caches and serves the Studio
  UI, for a fully offline tester (local UI, local model, local database). The
  release manifest supplies a version and a SHA-256; the archive is verified
  against that digest **before** extraction, extracted with path-traversal
  and absolute-path entries rejected, and cached under
  `~/.chatwright/ui/<version>` so subsequent starts need no network. A cached
  UI is reused when the download cannot be reached; with neither, startup
  fails with an explicit error rather than serving nothing. `--ui-dir` still
  takes precedence and makes no network call. Override the source with
  `--ui-url`.

### Known limitations

- `chatwright server start --ui` and `restart --ui` report success as soon as
  the child process forks, before its UI download can succeed or fail. A
  failed download therefore surfaces only in `server.log`, not in the exit
  code. Foreground `serve --ui` fails visibly and synchronously, so prefer it
  when first setting up. Tracked as
  [cli#6](https://github.com/chatwright/cli/issues/6).
- `POST /datastate/query` still answers `"unsupported"` for real database
  execution — declared, never a faked pass. Canned rows via
  `--datastate-fixtures` are the only verified path in this version.

## 0.1.2 — 2026-07-24

### Changed

- Documentation only: SpecScore initialisation and a spec-first README
  section. Cut to publish the Homebrew cask, which became an available
  install path with this release once the GoReleaser token was in place.

## 0.1.1 — 2026-07-23

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
### Changed

- Depends on `chatwright.dev/runtime` v0.2.0 (arena package plus actor loop
  fixes) and `chatwright.dev/sdk` v0.1.1.

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
