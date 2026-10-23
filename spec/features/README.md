---
format: https://specscore.md/features-index-specification
---

# Features

Feature specifications for this project.

## Index

| Feature | Status | Description |
|---------|--------|-------------|
| [Self-Update](self-update/README.md) | Implementing | `chatwright self-update` (alias `chatwright update`) brings a running `chatwright` binary to the latest release. The behavior is not specified here: chatwright binds the shared [strongo/cli-helpers](https://specscore.studio/app/github.com/strongo/cli-helpers/spec/features/self-update?op=explore) library, whose Feature owns install-method detection, release resolution, checksum verification, atomic replacement, and every failure rule. This Feature specifies only what is chatwright's own — the command surface, chatwright's configuration of the library, and its exit-code contract. |
| [Install](install/README.md) | Implementing | `chatwright install` lists the other fleet CLIs relevant to chatwright (`specscore`, per the fleet catalog's relevance matrix), each with its live installed status, and `chatwright install <name>...` installs named ones consistently with how chatwright itself was installed. `chatwright upgrade` is the fleet-wide counterpart: `chatwright upgrade` (no arguments) reports every installed catalog CLI plus chatwright itself — current version, latest stable release, and verdict — without changing anything; `chatwright upgrade --all`/`chatwright upgrade <name>...` upgrade what the report showed. `chatwright self-update` is `chatwright upgrade chatwright`: both reach the exact same library call, because chatwright is always upgraded last and classified from its own self-update Config, never a `PATH` probe of its own binary. The behavior is not specified here: chatwright binds the shared [strongo/cli-helpers](https://specscore.studio/app/github.com/strongo/cli-helpers/spec/features/cli-install?op=explore) library (`github.com/strongo/cli-helpers/cliinstall`), whose Feature owns the catalog, status probing, destination policy, Homebrew-cask and direct-release install methods, and every failure guarantee. This Feature specifies only what is chatwright's own — the command wiring and chatwright's exit-code mapping. |
| [Chatwright product plugin content](chatwright-product-plugin-content/README.md) | Implementing | Publish Chatwright plugin 0.1.0 from one colocated canonical tree for agents working with deterministic scenario runs, replayable artifacts, arena reports, and the server companion. |

## Open Questions

None at this time.

---
*This document follows the https://specscore.md/features-index-specification*
