// selfupdate.go wires github.com/strongo/selfupdate into the CLI as
// `chatwright self-update` (alias `update`). This repository has no Cobra
// dependency (AGENTS.md: "the CLI is deliberately thin") and does not take
// one on for this feature either: the flag parsing and dispatch below use
// the same hand-rolled flag.FlagSet convention every other subcommand in
// this package already follows (run.go/arena.go/server.go/completion.go),
// while every decision about WHETHER an update is safe — install-method
// detection, checksum verification, the atomic swap — belongs to
// github.com/strongo/selfupdate, reached through selfupdate.Config.Check
// and .Update exactly as that module's own README documents for a
// non-Cobra caller.
//
// The confirmation prompt, its non-interactive refusal, and the text/JSON
// writers all come from the framework-neutral
// github.com/strongo/selfupdate/cliui subpackage — the same helpers the
// module's own Cobra adapter (cobracmd) is built from — so this file
// contains no prompting, formatting, or refusal logic of its own. See
// spec/features/self-update/README.md: this repository's Feature inherits
// the shared library's behavior contract rather than restating it.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/strongo/selfupdate"
	"github.com/strongo/selfupdate/cliui"
)

// selfUpdateRepository is the GitHub repository releases are published to.
// The module's own vanity import path (chatwright.dev/cli, see go.mod)
// does NOT determine this — it resolves through a go-import meta tag, not
// GitHub — so this is spelled out explicitly rather than derived from the
// module path.
const selfUpdateRepository = "chatwright/cli"

// selfUpdateBinaryName matches .goreleaser.yml's own binary/archive name
// ("chatwright"), which is also github.com/strongo/selfupdate's default
// asset-naming convention.
const selfUpdateBinaryName = "chatwright"

// undeterminedVersion is the placeholder github.com/strongo/buildinfo's own
// Info.Version carries when neither link-time stamping nor
// debug.ReadBuildInfo() could resolve a real version (see buildinfo.Info's
// doc comment) — e.g. a plain `go build` inside this repository with no
// build-info fallback available.
const undeterminedVersion = "dev"

// selfUpdateUndeterminedVersions lists every string cliBuildInfo().Short()
// (main.go) can report that does not identify a real release.
// undeterminedVersion ("dev") is buildinfo's own placeholder, above.
// "(devel)" is declared too, defensively: it's what the Go toolchain itself
// stamps on debug.BuildInfo.Main.Version for an un-tagged source-tree build,
// and buildinfo.Get already normalizes it away before it would ever reach
// this package's Config (see buildinfo's own `!= "(devel)"` guard) — but an
// undeclared placeholder gets compared as a real version and reports an
// update available FROM a version that was never released, so both are
// declared here rather than relying on that normalization never changing.
var selfUpdateUndeterminedVersions = []string{undeterminedVersion, "(devel)"}

// selfUpdateConfig builds this CLI's own selfupdate.Config: everything
// selfupdate.Config's own doc comment calls "consumer-configured-identity"
// and nothing else. AssetName/ChecksumsName/DownloadURL are left at their
// defaults — they already match .goreleaser.yml's own archive and
// checksum name_templates (GoReleaser's stock
// "<binary>_<version>_<os>_<arch>" convention, .zip on windows, no name
// replacements configured there) — so none of those are overridden here.
func selfUpdateConfig() selfupdate.Config {
	return selfupdate.Config{
		BinaryName:           selfUpdateBinaryName,
		Repository:           selfUpdateRepository,
		CurrentVersion:       cliBuildInfo().Short(),
		UndeterminedVersions: selfUpdateUndeterminedVersions,
		// The only distribution channel .goreleaser.yml configures beyond
		// plain release archives (see its own "Homebrew cask" comment
		// block) is `brew install --cask chatwright/tap/chatwright` —
		// --cask, not a formula, since this ships prebuilt binaries.
		Managers: []selfupdate.Manager{
			selfupdate.Homebrew("brew upgrade --cask chatwright"),
		},
		// Exactly .goreleaser.yml's own builds.goos x builds.goarch
		// matrix, including its one explicit exclusion (windows/arm64 is
		// `ignore`d there, so no such asset is ever published).
		SupportedPlatforms: []selfupdate.Platform{
			{GOOS: "linux", GOARCH: "amd64"},
			{GOOS: "linux", GOARCH: "arm64"},
			{GOOS: "darwin", GOARCH: "amd64"},
			{GOOS: "darwin", GOARCH: "arm64"},
			{GOOS: "windows", GOARCH: "amd64"},
		},
		// `chatwright version` (main.go) prints buildinfo.Info.Long(),
		// "chatwright <version> (<commit>) <date>", as its first line;
		// VersionProbeArgs' output only needs to CONTAIN the target version
		// (see selfupdate's own verifyBinaryVersion), which that line
		// already does.
		VersionProbeArgs: []string{"version"},
	}
}

// Test seams over selfupdate.Config's own methods, mirroring the shared
// module's own cobracmd package (its checkFunc/updateFunc/detectFunc
// indirections), so this file's tests can exercise flag parsing, Options
// construction, and output formatting — this file's actual responsibility
// — without a live GitHub endpoint or a real installed binary. The
// defaults are the real thing; only tests override these.
var (
	selfUpdateCheckFunc = func(cfg selfupdate.Config, ctx context.Context) (selfupdate.CheckResult, error) {
		return cfg.Check(ctx)
	}
	selfUpdateApplyFunc = func(cfg selfupdate.Config, ctx context.Context, opts selfupdate.Options) (selfupdate.Outcome, error) {
		return cfg.Update(ctx, opts)
	}
	selfUpdateDetectFunc = func(cfg selfupdate.Config) (selfupdate.Detection, error) {
		return cfg.DetectSelf()
	}
)

// runSelfUpdate implements `chatwright self-update` / `chatwright update`
// [flags]. Unlike this package's other runX functions, which only ever
// write, stdin is threaded through explicitly because the confirmation
// prompt cliui.Confirm builds needs to read from it — main.go's dispatch
// passes os.Stdin.
func runSelfUpdate(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	// A bare "help", "-h" or "--help" as the first argument, ahead of any
	// FlagSet — mirroring main.go's own dispatch and every sibling
	// subcommand's identical three-way case.
	if len(args) > 0 && (args[0] == "help" || args[0] == "-h" || args[0] == "--help") {
		printSelfUpdateUsage(stdout)
		return 0
	}

	fs := flag.NewFlagSet("chatwright self-update", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { printSelfUpdateUsage(stderr) }
	check := fs.Bool("check", false, "report whether a newer release is available without applying it")
	yes := fs.Bool("yes", false, "skip the interactive confirmation prompt")
	fs.BoolVar(yes, "y", false, "shorthand for --yes")
	pinned := fs.String("version", "", `install a specific release tag (leading "v" optional) instead of the latest`)
	allowDowngrade := fs.Bool("allow-downgrade", false, "permit installing a --version older than the running build")
	dryRun := fs.Bool("dry-run", false, "report what would happen without downloading or writing anything")
	format := fs.String("format", "text", "output format: text|json")

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		_, _ = fmt.Fprintf(stderr, "chatwright self-update: unexpected extra argument %q\n\n", fs.Arg(0))
		printSelfUpdateUsage(stderr)
		return 2
	}
	if *format != "text" && *format != "json" {
		_, _ = fmt.Fprintf(stderr, "chatwright self-update: unknown --format %q (want text or json)\n\n", *format)
		printSelfUpdateUsage(stderr)
		return 2
	}

	cfg := selfUpdateConfig()
	ctx := context.Background()

	// Mirrors the shared module's own cobracmd.New RunE dispatch order
	// (cobracmd.go): --check is checked first and, when set, every
	// update-only flag (--yes, --version, --allow-downgrade, --dry-run) is
	// simply not consulted — that's cobracmd's own observable behavior,
	// not a chatwright-specific addition.
	if *check {
		return runSelfUpdateCheck(ctx, cfg, stdout, stderr, *format)
	}
	return runSelfUpdateApply(ctx, cfg, stdin, stdout, stderr, selfUpdateApplyOptions{
		pinnedVersion:  *pinned,
		allowDowngrade: *allowDowngrade,
		dryRun:         *dryRun,
		yes:            *yes,
		format:         *format,
	})
}

// runSelfUpdateCheck implements --check: read-only, so a detection failure
// is reported as Ambiguous (never fatal to the check itself) exactly as
// cobracmd.runCheck does — install-method classification reads no network
// and writes nothing, so it costs the read-only guarantee nothing, but a
// failure resolving it must not take the whole check down with it. An
// available update's next step is only printed for the human text format;
// cliui.WriteCheckJSON's own shape already carries install_method/manager/
// upgrade_command for a machine caller to act on without parsing prose.
func runSelfUpdateCheck(ctx context.Context, cfg selfupdate.Config, stdout, stderr io.Writer, format string) int {
	result, err := selfUpdateCheckFunc(cfg, ctx)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "chatwright self-update: %v\n", err)
		return mapSelfUpdateExitCode(err)
	}

	detection, detectErr := selfUpdateDetectFunc(cfg)
	if detectErr != nil {
		detection = selfupdate.Detection{Method: selfupdate.Ambiguous}
	}

	if format == "json" {
		if err := cliui.WriteCheckJSON(stdout, cfg, result, detection); err != nil {
			_, _ = fmt.Fprintf(stderr, "chatwright self-update: encode result: %v\n", err)
			return 1
		}
		return 0
	}

	cliui.WriteCheck(stdout, cfg, result)
	if result.Verdict != selfupdate.UpToDate {
		cliui.WriteNextStep(stdout, cfg, detection, "chatwright self-update")
	}
	// An available (or undetermined) update is reported, not failed: exit
	// 0 either way. This CLI has no pre-existing "general findings" exit
	// code to fold this into, and reserving a brand-new one for exactly
	// this case would be inventing a code — see mapSelfUpdateExitCode's own
	// doc comment for the rest of this file's exit-code reasoning. This
	// matches the shared module's own reference CLI
	// (cmd/selfupdate/main.go, passthroughErrors.UpdateAvailable): one of
	// the two equally valid answers selfupdate's own README documents real
	// consumers giving to this exact question.
	return 0
}

// selfUpdateApplyOptions carries runSelfUpdateApply's flag values as a
// struct, not five positional parameters, purely for readability at the
// call site in runSelfUpdate.
type selfUpdateApplyOptions struct {
	pinnedVersion  string
	allowDowngrade bool
	dryRun         bool
	yes            bool
	format         string
}

// runSelfUpdateApply implements every non-check invocation: the managed
// redirect, the already-current no-op, a dry run, a declined confirmation,
// and an actual swap. The confirmation callback and every line it (and the
// outcome) print are cliui's; this function is only flag-to-Options
// translation, dispatch, and this CLI's own exit-code mapping.
func runSelfUpdateApply(ctx context.Context, cfg selfupdate.Config, stdin io.Reader, stdout, stderr io.Writer, opts selfUpdateApplyOptions) int {
	confirm := cliui.Confirm(cliui.ConfirmOptions{
		In:  stdin,
		Out: stdout,
		Yes: opts.yes,
		// Interactive: nil -> cliui.IsTerminal, a real tty ioctl via
		// golang.org/x/term — never a hand-rolled os.ModeCharDevice check
		// (/dev/null is a character device too; see cliui.IsTerminal's own
		// doc comment for the bug that already cost this library's wb
		// sibling once).
	})

	outcome, err := selfUpdateApplyFunc(cfg, ctx, selfupdate.Options{
		PinnedVersion:  opts.pinnedVersion,
		AllowDowngrade: opts.allowDowngrade,
		DryRun:         opts.dryRun,
		Confirm:        confirm,
	})
	if err != nil {
		if selfupdate.KindOf(err) == selfupdate.KindAmbiguous {
			cliui.WriteAmbiguousGuidance(stdout, cfg)
		}
		_, _ = fmt.Fprintf(stderr, "chatwright self-update: %v\n", err)
		return mapSelfUpdateExitCode(err)
	}

	if opts.format == "json" {
		if err := cliui.WriteOutcomeJSON(stdout, outcome); err != nil {
			_, _ = fmt.Fprintf(stderr, "chatwright self-update: encode result: %v\n", err)
			return 1
		}
		return 0
	}
	cliui.WriteOutcome(stdout, stderr, cfg, outcome)
	return 0
}

// mapSelfUpdateExitCode maps selfupdate.KindOf(err) onto this CLI's own,
// pre-existing two-code failure contract: 2 for a usage error the caller
// can fix by typing something different, 1 for every other runtime failure
// — the same split main.go/run.go/arena.go/server.go/completion.go already
// use throughout this package (a bad or missing/extra argument returns 2;
// everything else that fails at runtime returns 1). This file does not
// invent a third code: three of selfupdate's ten FailureKinds are, in
// exactly that sense, usage errors —
//
//   - KindNonInteractive: no terminal was attached and --yes was not
//     given; the fix is to pass --yes, not to retry.
//   - KindDowngrade: --version named a release older than the running
//     build and --allow-downgrade was not given; the fix is that flag.
//   - KindUnknownTag: --version named a tag with no matching release (or
//     no asset for this platform); the fix is a different --version
//     value.
//
// Every other kind — a release-lookup, download, checksum or permission
// failure, an ambiguous install, an unsupported platform, or anything
// KindUnexpected — is not something a flag fixes, so it takes the same
// general-failure code every other runtime error in this CLI already does.
func mapSelfUpdateExitCode(err error) int {
	switch selfupdate.KindOf(err) {
	case selfupdate.KindNonInteractive, selfupdate.KindDowngrade, selfupdate.KindUnknownTag:
		return 2
	default:
		return 1
	}
}

func printSelfUpdateUsage(w io.Writer) {
	_, _ = fmt.Fprintln(w, `Update the installed chatwright binary in place, or report whether a newer
release is available. Safety decisions — whether this install may be
replaced at all, checksum verification, the atomic swap — belong to
github.com/strongo/selfupdate; see spec/features/self-update.

A Homebrew-installed binary is redirected to "brew upgrade --cask
chatwright" and is never overwritten directly.

Usage:
  chatwright self-update [flags]
  chatwright update [flags]   (alias)

Flags:
  --check             report whether a newer release is available without
                       applying it; prints the next step (a manager's
                       upgrade command, or this command itself) when one
                       exists
  --yes, -y           skip the interactive confirmation prompt
  --version TAG       install a specific release tag (leading "v" optional)
                       instead of the latest
  --allow-downgrade   permit installing a --version older than the running
                       build
  --dry-run           report what would happen — including the exact asset
                       URL that would be fetched — without downloading or
                       writing anything
  --format text|json  output format (default text)

Without --yes and without an interactive terminal attached, self-update
refuses rather than blocking on input or silently doing nothing — pass
--yes for scripts, CI, and agents.

Exit codes:
  0  success: up to date, redirected to the owning package manager, updated,
     a declined confirmation, a completed --check (whatever its verdict),
     or a completed --dry-run
  1  a runtime failure not fixed by a different flag: a release-lookup,
     download, checksum, or permission failure; an ambiguous install; an
     unsupported platform
  2  usage error: a bad flag, an unexpected extra argument, a --version tag
     that matched no release, or a confirmation that was needed but
     neither --yes nor a terminal was available`)
}
