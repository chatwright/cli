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
)

// version is this CLI's own release version, injected by GoReleaser via
// -ldflags "-X main.version={{.Version}}" at release-build time. When empty
// (a `go install chatwright.dev/cli/cmd/chatwright@vX.Y.Z` build, or a plain
// `go build` inside this repository), cliVersion falls back to the module
// version recorded in the binary's build info.
var version string

// fallbackVersion is used when neither the injected version nor build info
// carries a module version (e.g. a plain `go build` inside the repo).
const fallbackVersion = "devel"

// sdkModulePath and runtimeModulePath name the two Chatwright modules whose
// resolved versions `chatwright version` reports alongside the CLI's own —
// the split's contract (spec/plans/code-split-restructuring.md in the
// chatwright/chatwright repository): the CLI is thin, and which sdk/runtime
// it was built against is part of its identity.
const (
	sdkModulePath     = "chatwright.dev/sdk"
	runtimeModulePath = "chatwright.dev/runtime"
)

func cliVersion() string {
	if version != "" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return fallbackVersion
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
	case "version", "--version":
		printVersion(stdout)
		return 0
	case "platforms":
		for _, p := range builtinPlatforms() {
			_, _ = fmt.Fprintf(stdout, "%s\t%s\n", p.Name(), platformSummaries[p.Name()])
		}
		return 0
	default:
		_, _ = fmt.Fprintf(stderr, "chatwright: unknown command %q\n\n", args[0])
		printUsage(stderr)
		return 2
	}
}

// printVersion prints the CLI's own version followed by the resolved
// sdk/runtime module versions from build info — see sdkModulePath's doc
// comment. A dependency line is omitted when the version cannot be
// determined, rather than printing an empty placeholder.
func printVersion(w io.Writer) {
	_, _ = fmt.Fprintf(w, "chatwright %s\n", cliVersion())
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
  platforms   List built-in messaging platform emulators
  version     Print the CLI, runtime and sdk versions
  help        Show this help`)
}
