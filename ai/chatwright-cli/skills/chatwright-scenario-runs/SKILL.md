---
name: chatwright-scenario-runs
description: Run Chatwright's deterministic bundled scenario, retain its replayable run bundle, or write an editable example without a network or API key.
---

# Chatwright scenario runs

Use this skill for `chatwright run` work. Start from an installed `chatwright`
binary and create a temporary working directory; do not depend on a source
checkout.

Run the bundled deterministic GreetBot journey and keep machine output clean:

```sh
mkdir -p chatwright-run && cd chatwright-run
chatwright run example --out ./bundle --json 2>run.stderr
```

The JSON object on stdout identifies the document, verdict, usage, and
`bundlePath`. A successful bundled run is offline and needs no API key. Keep
`bundle/` as the replayable artifact for the Studio player; inspect the
outcome before treating a non-zero exit as a scenario failure.

Use `--write` when the next step is changing the scenario rather than running
the bundled one:

```sh
chatwright run example --write --out ./editable
chatwright run ./editable/greetbot-language-onboarding.json --out ./changed-run
```

`--quiet` suppresses successful human output. `--json` remains the one JSON
outcome on stdout even with `--quiet`; reserve stderr for progress and
diagnostics. Read [the output and exit reference](references/output-and-exits.md)
before scripting a result check.
