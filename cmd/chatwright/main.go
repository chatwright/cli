// Command chatwright is the local command-line entry point for the Chatwright
// conversation execution platform. It is deliberately thin: the heavy lifting
// lives in chatwright.dev/runtime (platform emulation + the testing runtime)
// and chatwright.dev/sdk (the run-bundle wire model); this binary only fronts
// them from a terminal.
package main

import (
	"fmt"
	"io"
	"os"
	"runtime/debug"

	"chatwright.dev/runtime/platform"
	"chatwright.dev/runtime/telegram"
	"chatwright.dev/runtime/whatsapp"
	"chatwright.dev/sdk"
	"github.com/strongo/buildinfo"
)

// sdkModulePath and runtimeModulePath name the two Chatwright modules whose
// resolved versions `chatwright version` reports alongside the CLI's own —
// the split's contract (spec/plans/code-split-restructuring.md in the
// chatwright/chatwright repository): the CLI is thin, and which sdk/runtime
// it was built against is part of its identity.
const (
	sdkModulePath     = "chatwright.dev/sdk"
	runtimeModulePath = "chatwright.dev/runtime"
)

// cliBuildInfo resolves this CLI's own build identity: the injected
// -ldflags -X values from GoReleaser (see .goreleaser.yml), falling back to
// runtime/debug.ReadBuildInfo() for a `go install`/`go build` binary. See
// github.com/strongo/buildinfo — every CLI in this fleet shares this one
// implementation instead of hand-rolling its own version plumbing.
func cliBuildInfo() buildinfo.Info {
	return buildinfo.Get("chatwright")
}

// depVersion returns the resolved version of the named module dependency from
// the running binary's build info, or "" when it cannot be determined (never
// the case for a released or go-installed binary, which always records its
// dependency graph).
func depVersion(path string) string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, dep := range bi.Deps {
		if dep.Path == path {
			return dep.Version
		}
	}
	return ""
}

// builtinPlatforms is the roster of messaging-platform emulators this binary
// links in, in the order `chatwright platforms` lists them. The names come
// from the platforms themselves (platform.Platform.Name), never restated
// here; only the one-line capability summaries are the CLI's own.
func builtinPlatforms() []platform.Platform {
	return []platform.Platform{telegram.Platform(), whatsapp.Platform()}
}

// platformSummaries maps a platform.Platform.Name to the one-line capability
// summary `chatwright platforms` prints beside it. A platform missing here
// still lists — with an empty summary — so adding a platform to
// builtinPlatforms never silently drops it from the listing.
var platformSummaries = map[string]string{
	"telegram": "text, inline actions, edits",
	"whatsapp": "text (experimental)",
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stdout)
		return 0
	}

	switch args[0] {
	case "help", "-h", "--help":
		printUsage(stdout)
		return 0
	case "--version", "-v":
		_, _ = fmt.Fprintln(stdout, cliBuildInfo().Short())
		return 0
	case "version":
		printVersion(stdout)
		return 0
	case "platforms":
		for _, p := range builtinPlatforms() {
			_, _ = fmt.Fprintf(stdout, "%s\t%s\n", p.Name(), platformSummaries[p.Name()])
		}
		return 0
	case "arena":
		return runArena(args[1:], stdout, stderr)
	case "run":
		return runRun(args[1:], stdout, stderr)
	case "server":
		return runServer(args[1:], stdout, stderr)
	case "completion":
		return runCompletion(args[1:], stdout, stderr)
	case "self-update", "update":
		return runSelfUpdate(args[1:], os.Stdin, stdout, stderr)
	default:
		_, _ = fmt.Fprintf(stderr, "chatwright: unknown command %q\n\n", args[0])
		printUsage(stderr)
		return 2
	}
}

// printVersion prints the CLI's own build identity (name, version, commit,
// date — see github.com/strongo/buildinfo) followed by the resolved
// sdk/runtime module versions from build info — see sdkModulePath's doc
// comment. A dependency line is omitted when the version cannot be
// determined, rather than printing an empty placeholder.
func printVersion(w io.Writer) {
	_, _ = fmt.Fprintln(w, cliBuildInfo().Long())
	if v := depVersion(runtimeModulePath); v != "" {
		_, _ = fmt.Fprintf(w, "runtime: %s %s\n", runtimeModulePath, v)
	}
	if v := depVersion(sdkModulePath); v != "" {
		_, _ = fmt.Fprintf(w, "sdk: %s %s\n", sdkModulePath, v)
	}
	_, _ = fmt.Fprintf(w, "run-bundle format: %s\n", sdk.FormatV1)
}

func printUsage(w io.Writer) {
	_, _ = fmt.Fprintln(w, `Chatwright CLI

Usage:
  chatwright <command>

Commands:
  platforms     List built-in messaging platform emulators
  run           Execute a self-contained scenario document (chatwright run --help)
  arena         Run and report on the actor-model arena (chatwright arena help)
  server        Run the server companion daemon (chatwright server help)
  completion    Generate a bash/zsh/fish completion script (chatwright completion help)
  self-update   Update the installed binary in place (chatwright self-update --help);
                also available as "chatwright update"
  version       Print the CLI, runtime and sdk versions
  help          Show this help

Try it now — no files, no network, no API key:
  chatwright run example`)
}
