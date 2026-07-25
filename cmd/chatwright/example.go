// example.go embeds this repository's own copy of the standard
// self-contained-scenario-document worked example — GreetBot's language-
// onboarding fixture, copied verbatim from chatwright.dev/runtime's own
// scenario/testdata/ (never re-authored; see run_test.go's own
// greetbotFixturePath) — so `chatwright run example` has something real to
// execute the moment a user installs this binary: no files of their own, no
// network call, no API key. The document's "bot" is exampleBot:greetbot (a
// bot compiled into chatwright.dev/runtime, no URL) and its one cast member
// is a cassette-replay provider (a canned recording, not a live model), so
// nothing about running it ever leaves the process.
//
// KEEPING THE EMBEDDED CASSETTE IN SYNC: the cassette's entries are keyed by
// a hash of the whole actor Prompt, and Prompt.History embeds run-bundle wire
// types — so ANY change to those types (a renamed field, an added one)
// silently invalidates every key in this copy, and `chatwright run example`
// starts failing with a replay cache miss. It is not a copy that only drifts
// when someone edits it. When bumping chatwright.dev/runtime, re-copy
// scenario/testdata/cassettes/greetbot-language-onboarding.json from that
// module (it regenerates its own via a build-tag-guarded recorder) and re-run
// the tests here — TestRunExampleEndToEnd catches it, loudly, which is how
// the sdk Verdict->Freshness rename was caught on 2026-07-25.
//
// chatwright.dev/runtime/scenario has no seam for supplying a Document or a
// cassette as in-memory bytes — scenario.ScenarioProvider.Load and, deeper,
// scenario.Build's own cassette loading (actor.LoadCassette) are both
// hard-wired to a filesystem path (see their own doc comments in that
// package: "Loading a cassette file is I/O this package only ever performs
// here, in Build"). So the embedded bytes are, whichever of the two
// materialize functions below is used, always written back out as real
// files before scenario.FileScenarioProvider ever sees them — a temporary,
// self-cleaning directory for a plain `chatwright run example`, or a
// directory the caller chose for `chatwright run example --write`. This is
// not a workaround bent around the runtime's API; it is that API, used
// exactly as every other document already is.
package main

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

// exampleDocumentArg is the DOCUMENT value runRun recognises in place of a
// file path: run the built-in worked example instead of reading one from
// disk. Chosen over, say, a leading "@" or "built-in:" sigil because a
// scenario document's own id is never literally "example" (ids are
// kebab-case slugs — see the format's own "id" field), so this can never
// collide with a real document's Document.ID, only, in principle, with a
// file a user happens to have named exactly "example" in their working
// directory — documented plainly in printRunUsage rather than hidden.
const exampleDocumentArg = "example"

// exampleDocumentFileName and exampleCassetteRelPath are the embedded
// example's two files, named and laid out relative to one another exactly
// as examples/greetbot-language-onboarding.json's own "cassette" field
// declares ("cassettes/greetbot-language-onboarding.json") — so a document
// materialize writes (to a temp dir or, via --write, a caller-chosen one)
// resolves its cassette exactly as the embedded copy does, with no path
// rewriting.
const (
	exampleDocumentFileName = "greetbot-language-onboarding.json"
	exampleCassetteRelPath  = "cassettes/greetbot-language-onboarding.json"
)

//go:embed examples/greetbot-language-onboarding.json
var exampleDocumentBytes []byte

//go:embed examples/cassettes/greetbot-language-onboarding.json
var exampleCassetteBytes []byte

// materializeExample writes the embedded example document and its cassette
// into dir (creating dir/cassettes as needed), returning the document's own
// path. The bytes written are byte-identical to
// examples/greetbot-language-onboarding.json and
// examples/cassettes/greetbot-language-onboarding.json in this repository —
// materialize never edits or regenerates them.
func materializeExample(dir string) (docPath string, err error) {
	docPath = filepath.Join(dir, exampleDocumentFileName)
	if err := os.WriteFile(docPath, exampleDocumentBytes, 0o644); err != nil { //nolint:gosec // scenario documents are not sensitive
		return "", fmt.Errorf("write %s: %w", docPath, err)
	}
	cassettePath := filepath.Join(dir, exampleCassetteRelPath)
	if err := os.MkdirAll(filepath.Dir(cassettePath), 0o755); err != nil {
		return "", fmt.Errorf("write %s: %w", cassettePath, err)
	}
	if err := os.WriteFile(cassettePath, exampleCassetteBytes, 0o644); err != nil { //nolint:gosec // cassette files are not sensitive
		return "", fmt.Errorf("write %s: %w", cassettePath, err)
	}
	return docPath, nil
}

// materializeExampleTemp writes the embedded example into a fresh temporary
// directory, invisible to the caller, and returns the document's own path
// plus a cleanup removing that directory. This is what a plain
// `chatwright run example` (no --write) uses: execution still goes through
// the exact same file-backed scenario.FileScenarioProvider path as any
// other document, but the user never has to create, name or clean up
// anything themselves.
func materializeExampleTemp() (docPath string, cleanup func(), err error) {
	dir, err := os.MkdirTemp("", "chatwright-example-*")
	if err != nil {
		return "", nil, fmt.Errorf("create temp dir: %w", err)
	}
	cleanup = func() { _ = os.RemoveAll(dir) }
	docPath, err = materializeExample(dir)
	if err != nil {
		cleanup()
		return "", nil, err
	}
	return docPath, cleanup, nil
}

// fileExists reports whether name is an existing regular file, so
// `chatwright run example` prefers a document the user actually has over the
// embedded worked example (see runRun).
func fileExists(name string) bool {
	info, err := os.Stat(name)
	return err == nil && info.Mode().IsRegular()
}
