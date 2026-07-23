# Changelog

All notable changes to the Chatwright CLI are documented here. The format is
based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions
follow [SemVer](https://semver.org/spec/v2.0.0.html) (pre-1.0: minor versions
may break).

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
