# Scenario output and exits

`chatwright run DOCUMENT --out DIR --json` writes one JSON object to stdout.
It includes `documentId`, `partStatus`, `verdict`, `detail`, `durationMs`,
`usage`, and `bundlePath`; optional warning and interruption fields appear
when applicable. Progress and diagnostics use stderr.

The bundled `example` is a deterministic cassette-backed GreetBot journey.
It produces a run bundle, not a command-line replay session. Use the bundle
in the Studio player to inspect or replay the recorded result.

Exit codes are part of the CLI contract:

- `0`: completed with the declared outcome.
- `1`: a harness-level error or a completed run whose declared outcome failed.
- `2`: invalid flags or document arguments; the run did not start.
- `3`: the actor could not run, commonly a cassette cache miss.
- `130`: interrupted by Ctrl-C.

Run `chatwright run --help` when selecting flags; `--quiet` and `--verbose`
cannot be combined.
