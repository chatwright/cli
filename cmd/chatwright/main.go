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
	"github.com/spf13/cobra"
	"github.com/strongo/buildinfo"
	"github.com/strongo/cli-helpers/selfupdate"
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
	return executeCommand(newRootCommand(os.Stdin), args, stdout, stderr)
}

func newRootCommand(stdin io.Reader) *cobra.Command {
	return newRootCommandWithSelfUpdateConfig(stdin, selfUpdateConfig())
}

func newRootCommandWithSelfUpdateConfig(stdin io.Reader, updateConfig selfupdate.Config) *cobra.Command {
	root := &cobra.Command{
		Use:   "chatwright",
		Short: "Execute and inspect Chatwright scenarios",
		Long:  "Chatwright executes self-contained messaging scenarios and writes replayable run bundles.",
		Example: `  chatwright run example
  chatwright run example --json --quiet
  chatwright run example --write --out ./my-scenario`,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return commandResult(0)
		},
	}
	root.Flags().BoolP("version", "v", false, "print the Chatwright CLI version")
	root.PreRunE = func(cmd *cobra.Command, _ []string) error {
		version, err := cmd.Flags().GetBool("version")
		if err != nil || !version {
			return nil
		}
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), cliBuildInfo().Short())
		return &commandError{code: 0}
	}
	root.AddCommand(
		newPlatformsCommand(),
		newVersionCommand(),
		newRunCommand(),
		newArenaCommand(),
		newServerCommand(),
		newCompletionCommand(),
		newSelfUpdateCommandWithConfig(updateConfig, stdin),
	)
	return root
}

func newPlatformsCommand() *cobra.Command {
	return &cobra.Command{Use: "platforms", Short: "List built-in messaging platform emulators", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		for _, p := range builtinPlatforms() {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\n", p.Name(), platformSummaries[p.Name()])
		}
		return nil
	}}
}

func newVersionCommand() *cobra.Command {
	return &cobra.Command{Use: "version", Short: "Print CLI, runtime and SDK versions", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		printVersion(cmd.OutOrStdout())
		return nil
	}}
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
