---
format: https://specscore.md/features-index-specification
---

# Features

Feature specifications for this project.

## Index

| Feature | Status | Description |
|---------|--------|-------------|
| [Self-Update](self-update/README.md) | Implementing | `chatwright self-update` (alias `chatwright update`) brings a running `chatwright` binary to the latest release. The behavior is not specified here: chatwright binds the shared [strongo/selfupdate](https://specscore.studio/app/github.com/strongo/selfupdate/spec/features/self-update?op=explore) library, whose Feature owns install-method detection, release resolution, checksum verification, atomic replacement, and every failure rule. This Feature specifies only what is chatwright's own — the command surface, chatwright's configuration of the library, and its exit-code contract. |

## Open Questions

None at this time.

---
*This document follows the https://specscore.md/features-index-specification*
