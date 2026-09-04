# Arena and server workflow

`chatwright arena run` requires both `--config` and `--out`. Its config names
the built-in scenario, repeat count, hardware label, budgets, and each provider
with a kind, base URL, model, and context length. The packaged
`assets/arena.example.yaml` is a starting point for local Ollama or LM Studio;
edit it for the real local endpoint and model before running it.

`chatwright arena report --dir DIR` reads `results.json` and existing bundles
from a completed arena output directory, then rewrites `report.md`. It is the
right path for an offline report refresh.

Server flags use this precedence: explicit flag, then `CHATWRIGHT_SERVER_*`
environment variable, then the command default. `serve` accepts `--addr`,
`--upstream`, `--datastate-fixtures`, `--ui-dir`, `--ui`, `--ui-url`, and
repeatable `--allow-origin`. `start` and `restart` add `--state-dir`; `stop`
uses only `--state-dir`.

Use `chatwright server stop --state-dir DIR` before removing a daemon state
directory. Do not claim a static UI is available unless a real `--ui-dir` was
provided or `--ui` completed its own download and verification.
