package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"chatwright.dev/sdk"
)

// TestRunExampleEndToEnd drives `chatwright run example` — the zero-file,
// zero-network, zero-API-key path a stranger who just installed this
// binary actually has — and checks it behaves the same as running the same
// fixture from a file path (TestRunEndToEndAgainstGreetbotFixture), then
// validates the written bundle against the committed run-bundle schema
// chatwright.dev/sdk ships, not just this repository's own structural spot
// checks.
func TestRunExampleEndToEnd(t *testing.T) {
	outDir := t.TempDir()

	var stdout, stderr bytes.Buffer
	code := runRun([]string{exampleDocumentArg, "--out", outDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("runRun() code = %d, want 0; stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "completed") {
		t.Errorf("stdout = %q, want it to report a completed part", stdout.String())
	}
	if !strings.Contains(stdout.String(), "verdict   verified") {
		t.Errorf("stdout = %q, want a verified verdict (not judged) — the embedded document declares a verify block", stdout.String())
	}

	bundlePath := filepath.Join(outDir, "greetbot-language-onboarding.chatwright.json")
	data, err := os.ReadFile(bundlePath)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", bundlePath, err)
	}

	b, err := sdk.Read(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("sdk.Read() error = %v", err)
	}
	if b.Format != sdk.FormatV1 {
		t.Errorf("Format = %q, want %q", b.Format, sdk.FormatV1)
	}
	if len(b.Runs) != 1 || b.Runs[0].ID != "greetbot-language-onboarding" {
		t.Fatalf("Runs = %+v, want exactly one run with ID greetbot-language-onboarding", b.Runs)
	}

	validateAgainstRunBundleSchema(t, data)
}

// TestRunExampleWriteThenRun proves the second half of task 1's contract:
// --write hands back the exact document (and cassette) the embedded run
// just executed — byte-identical to the embedded copy — and that written
// document is itself a fully working `chatwright run` input. A user who
// runs the example, writes it out, and re-runs it unmodified sees the same
// result, with no repository or network dependency of any kind.
func TestRunExampleWriteThenRun(t *testing.T) {
	dir := t.TempDir()

	var stdout, stderr bytes.Buffer
	code := runRun([]string{exampleDocumentArg, "--write", "--out", dir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("runRun() code = %d, want 0; stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "edit the goal, then run:") {
		t.Errorf("stdout = %q, want a hint on how to edit and re-run", stdout.String())
	}

	docPath := filepath.Join(dir, exampleDocumentFileName)
	cassettePath := filepath.Join(dir, exampleCassetteRelPath)

	gotDoc, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", docPath, err)
	}
	if !bytes.Equal(gotDoc, exampleDocumentBytes) {
		t.Errorf("written document does not byte-match the embedded example")
	}
	gotCassette, err := os.ReadFile(cassettePath)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", cassettePath, err)
	}
	if !bytes.Equal(gotCassette, exampleCassetteBytes) {
		t.Errorf("written cassette does not byte-match the embedded example")
	}

	// Now run the written file exactly as a stranger following the printed
	// hint would: `chatwright run <docPath>`.
	runOutDir := t.TempDir()
	stdout.Reset()
	stderr.Reset()
	code = runRun([]string{docPath, "--out", runOutDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("runRun(%q) code = %d, want 0; stdout=%q stderr=%q", docPath, code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "verdict   verified") {
		t.Errorf("stdout = %q, want a verified verdict", stdout.String())
	}
}

// TestRunWriteRequiresExampleDocument guards --write against a DOCUMENT
// other than "example": there is nothing else this flag could write.
func TestRunWriteRequiresExampleDocument(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runRun([]string{greetbotFixturePath, "--write"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("runRun() code = %d, want 2; stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), `--write is only valid with DOCUMENT "example"`) {
		t.Errorf("stderr = %q, want the --write-requires-example message", stderr.String())
	}
}

// TestRunHelpAliases proves "-h" and "--help" behave exactly like the bare
// "help" TestRunHelp already checks — the fix this task made: previously
// both fell through to flag.FlagSet's own bare default usage (just the
// registered flags, no description) instead of this command's own
// printRunUsage.
func TestRunHelpAliases(t *testing.T) {
	for _, alias := range []string{"-h", "--help"} {
		t.Run(alias, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := runRun([]string{alias}, &stdout, &stderr)
			if code != 0 {
				t.Fatalf("runRun([%q]) code = %d, want 0; stdout=%q stderr=%q", alias, code, stdout.String(), stderr.String())
			}
			if !strings.Contains(stdout.String(), "chatwright run DOCUMENT") {
				t.Errorf("stdout = %q, want the command's own usage", stdout.String())
			}
			if !strings.Contains(stdout.String(), "chatwright run example") {
				t.Errorf("stdout = %q, want it to mention the built-in example", stdout.String())
			}
			if strings.Contains(stdout.String(), "Usage of chatwright run:") {
				t.Errorf("stdout = %q, still shows flag.FlagSet's own bare default usage", stdout.String())
			}
		})
	}
}

// validateAgainstRunBundleSchema validates data against the committed
// run-bundle JSON schema chatwright.dev/sdk ships at
// formats/run-bundle/v1/schema.json — resolving that module's own on-disk
// directory via `go list -m`, since the schema is a plain repository file,
// not something the sdk package embeds or exposes via its own Go API.
// Skips (rather than fails) when the `go` toolchain cannot resolve it —
// e.g. a fully offline test run with no module cache — since this is an
// extra rigor check on top of, not a replacement for, sdk.Read's own
// structural validation TestRunExampleEndToEnd already relies on.
func validateAgainstRunBundleSchema(t *testing.T, data []byte) {
	t.Helper()

	cmd := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "chatwright.dev/sdk")
	cmd.Env = append(os.Environ(), "GOWORK=off")
	out, err := cmd.Output()
	if err != nil {
		t.Skipf("cannot resolve chatwright.dev/sdk module directory (%v); skipping JSON Schema validation", err)
	}
	sdkDir := strings.TrimSpace(string(out))
	schemaPath := filepath.Join(sdkDir, "formats", "run-bundle", "v1", "schema.json")

	schema, err := jsonschema.NewCompiler().Compile(schemaPath)
	if err != nil {
		t.Fatalf("compile %s: %v", schemaPath, err)
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode bundle JSON: %v", err)
	}
	if err := schema.Validate(inst); err != nil {
		t.Fatalf("bundle does not validate against %s:\n%v", schemaPath, err)
	}
}

// TestRunExampleDoesNotShadowARealFile pins the precedence rule: the literal
// DOCUMENT value "example" means the embedded worked example ONLY when no file
// of that name exists. Silently running the built-in in place of a document the
// user actually has would be a confusing failure, and "example" is a legal
// filename.
func TestRunExampleDoesNotShadowARealFile(t *testing.T) {
	dir := t.TempDir()
	shadow := filepath.Join(dir, exampleDocumentArg)
	if err := os.WriteFile(shadow, []byte(`{"nope":true}`), 0o600); err != nil {
		t.Fatalf("write shadowing file: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := runRun([]string{shadow, "--out", dir}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("runRun() code = 0, want non-zero: the user's own (invalid) %q must be loaded, not the embedded example; stdout=%q", exampleDocumentArg, stdout.String())
	}
	if !strings.Contains(stderr.String(), "unknown-member") {
		t.Errorf("stderr = %q, want the user's file to be parsed and rejected by rule id", stderr.String())
	}
	if stdout.String() != "" {
		t.Errorf("stdout = %q, want NO run at all — the embedded example must not have been substituted", stdout.String())
	}
}
