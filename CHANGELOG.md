# Changelog

All notable changes to the Chatwright CLI are documented here. The format is
based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions
follow [SemVer](https://semver.org/spec/v2.0.0.html) (pre-1.0: minor versions
may break).

## 0.7.0 — 2026-07-25

CLI UX overhaul: `chatwright run` had genuinely good `--help` text but a
bare runtime experience — no colour, no live progress, a flat prose summary
line, and its own bundled example tripped its own validation warning. This
release consumes `runengine.Run.OnProgress` for the first time anywhere in
this repository.

### Added

- **Live progress.** `chatwright run` now shows what the actor is doing as
  it happens — part/task position, step count against its budget, elapsed
  time, budget burn, and a derived "acted: executed/no effect/…" indicator
  per turn — on stderr, throttled to task boundaries only when piped
  (`--verbose` for every turn). A real terminal gets a single redrawn
  status line; a pipe or log file gets plain, newline-terminated lines,
  never a stray carriage return.
- `--json`: emit the run outcome as one JSON object on stdout (documented
  shape in `chatwright run --help`), human output suppressed. Never
  suppressed by `--quiet`.
- `--quiet`: silence on a successful run; a failure or an interrupted run
  is still reported in full — "errors only".
- `--verbose`: every actor-loop iteration on stderr, not just task/part
  boundaries. Mutually exclusive with `--quiet` (a usage error, exit 2).
- Ctrl-C now finalises the run rather than losing it: an interrupted
  ai-goal part is captured as an ordinary actor-unavailable-shaped result,
  the run bundle is still written, and `chatwright run` reports what was
  kept and exits `130` (the conventional "terminated by SIGINT" code). A
  second Ctrl-C terminates immediately, as if no handler were installed.
  Only ai-goal parts are interruptible this way — a purely deterministic
  part has no earlier interception point without a `chatwright.dev/runtime`
  change.
- `chatwright completion bash|zsh|fish`: hand-written completion scripts
  for all three shells, no framework dependency.
- Colour and symbols: TTY detection, the `NO_COLOR` and
  `CLICOLOR`/`CLICOLOR_FORCE` conventions, and a UTF-8-vs-ASCII fallback
  for the ✓/✗/⚠ status symbols — all in a new dependency-free
  `internal/term` package.
- `chatwright run --help` now documents this command's own exit codes (0,
  1, 2, 3, 130).

### Changed

- The flat `<id>: part status=<status>, outcome=<verdict>: <detail>`
  prose line is replaced by a scannable, aligned summary block (status
  symbol, verdict, human-readable duration, token/cost usage, and the
  bundle path with what to do with it next).
- A rejected scenario document's validation errors are now presented as a
  numbered, readable list (pointer + rule id + message per problem) rather
  than a wall of text — still built only from `Issue.Pointer`/`Code`/
  `Message`, so the format's own "never echo a secret value" guarantee is
  preserved.

### Fixed

- The CLI's own bundled `chatwright run example` document now declares a
  run-level `ceiling`, so a first-time user's very first `chatwright run
  example` no longer prints the CLI's own `no-run-ceiling` validation
  warning about its own demo.

## 0.6.0 — 2026-07-25

### Changed

- Depends on `chatwright.dev/sdk` v0.3.0 and `chatwright.dev/runtime` v0.5.0,
  which rename the observe-side click-validity type `Verdict` to `Freshness`
  (wire tag `verdict` → `freshness`; values `fresh`/`stale` unchanged). Run
  bundles this CLI writes now carry `freshness`. "Verdict" is reserved for the
  AI-judged-assertion outcome. The Studio player accepts both, so recordings
  made before this release still replay.
- The embedded `chatwright run example` cassette was re-copied from
  `chatwright.dev/runtime`'s regenerated fixture. Its entries are keyed by a
  hash of the whole actor prompt, which embeds run-bundle wire types, so the
  rename above invalidated every key in the previous copy — `run example`
  would have failed with a replay cache miss. Caught by this repository's own
  end-to-end tests before release.

## 0.5.0 — 2026-07-25

### Fixed

- `chatwright run`: a document whose actor could not act at all — most
  commonly a cassette replay cache miss, e.g. after `chatwright run example
  --write` followed by an edit to the goal — was reported as
  `outcome=not verified: journal evidence incomplete: …`, indistinguishable
  from the bot itself having misbehaved, and exited `1`, the same code a
  real verification failure uses. The underlying cause (a loop event's own
  `proposeError`, already carried in the run bundle) was silently dropped
  on the floor. It is now reported distinctly —
  `outcome=actor unavailable: actor: replay cache miss: …` — names the
  likely cause and the fix (re-record the cassette against a live provider,
  or run against a live provider directly), and exits a new, distinct code
  (`3`) rather than `1`, mirroring the founder's decision for AI-judged
  assertions that a broken harness must never look like a broken bot, and
  must never share an exit code with one.
- `chatwright run`: a document whose actor ran to completion but whose
  `verify` block did **not** hold (a genuine bot-behaviour failure) used to
  exit `0` — indistinguishable from a real pass — because the exit-code
  decision checked only the part's status, never `verified`. It now exits
  `1`, as `outcome=not verified` always should have.

## 0.4.0 — 2026-07-25

### Added

- `chatwright run example`: run this CLI's own built-in worked example
  (GreetBot's language-onboarding scenario, embedded via `go:embed` —
  copied verbatim from `chatwright.dev/runtime`'s own
  `scenario/testdata/`) with no files, no network call and no API key —
  the `exampleBot:greetbot` bot and its cast member's cassette-replay
  provider are both entirely self-contained. Previously a freshly
  installed CLI had nothing for `chatwright run` to run.
- `chatwright run example --write [--out DIR]`: write the example's
  document and cassette into `DIR` (default `.`) instead of running them,
  byte-identical to the copy `chatwright run example` executes, so a user
  can read the format, change the goal, and re-run it with
  `chatwright run DOCUMENT`.

### Fixed

- `chatwright run --help`/`-h` now print this command's own description,
  `Usage:`, `Flags:` and worked examples (matching `chatwright arena
  help`'s house style) instead of falling through to `flag.FlagSet`'s bare
  default usage (just the registered `-out` flag, no description). A bare
  `chatwright run help` already worked correctly before this change.
- `chatwright --help` now points new users at `chatwright run example`
  directly, alongside the existing command list.
- `chatwright run example` no longer shadows a real file: the literal
  DOCUMENT value `example` selects the built-in worked example only when no
  regular file of that name exists, so a document a user actually has is
  always loaded in preference to it.

### Known limitations

- Editing the goal text of the document written by
  `chatwright run example --write` and re-running it will usually fail with
  a cassette cache miss. The bundled cassette is a fixed recording keyed by
  a hash that includes the goal's own text, so changing the goal invalidates
  it. Running the written document *unchanged* works. Editing the goal for
  real needs a live model provider; the underlying key over-specification is
  the subject of `spec/ideas/exploration-to-regression.md` in the standard
  repository.

## 0.3.0 — 2026-07-25

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
  rule).

### Changed

- Depends on `chatwright.dev/runtime` v0.4.0, which adds the
  `chatwright.dev/runtime/scenario` package this command is built on.

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
