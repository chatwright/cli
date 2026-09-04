---
name: chatwright-arena-server
description: Run or refresh Chatwright arena reports and operate the Chatwright server companion with the supported Cobra command paths.
---

# Chatwright arena and server

Use this skill for the actor-model arena and the server companion. Read the
matching command help before changing flags:

```sh
chatwright arena --help
chatwright server --help
```

For a new arena campaign, copy the packaged starter config, declare the
hardware and local provider values that actually exist, then run the matrix:

```sh
cp <skill-directory>/assets/arena.example.yaml ./arena.yaml
chatwright arena run --config ./arena.yaml --out ./arena-output
```

An arena run calls the configured provider and is not an offline substitute
for `chatwright run example`. If output already exists, recompute its report
without a model call:

```sh
chatwright arena report --dir ./arena-output
```

Run the server in the foreground during development so its listener and logs
remain visible:

```sh
chatwright server serve --addr 127.0.0.1:8080 --upstream http://127.0.0.1:11434/v1
```

Use `server start`, `stop`, and `restart` only when a detached daemon is
needed; choose an explicit `--state-dir` so its PID and log are easy to find.
`--ui-dir` serves an existing Studio build and takes precedence over `--ui`.
Read [the workflow reference](references/arena-and-server.md) for the required
inputs and safe command selection.
