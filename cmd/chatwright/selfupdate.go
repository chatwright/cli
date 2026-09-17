package main

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/strongo/cli-helpers/cliinstall"
	"github.com/strongo/cli-helpers/selfupdate"
	"github.com/strongo/cli-helpers/selfupdate/cobracmd"
)

// selfUpdateRepository and selfUpdateBinaryName restate chatwright's release
// identity for tests that build a selfupdate.Config directly against a
// fixture server (selfUpdateTestConfig); the real Config below no longer
// hardcodes them — it resolves them from the compiled-in catalog entry.
const selfUpdateRepository = "chatwright/cli"
const selfUpdateBinaryName = "chatwright"

// chatwrightCatalogID is this binary's own id in
// github.com/strongo/cli-helpers/cliinstall — the fleet-wide compiled-in
// registry of installable CLIs and their release identities
// (cli-install#req:host-identity-from-catalog). Both selfUpdateConfig and
// newInstallCommand resolve the SAME entry, so `chatwright self-update` and
// every other fleet CLI's `install chatwright` agree on how chatwright is
// released, by construction rather than by two copies staying in sync.
const chatwrightCatalogID = "chatwright"

// catalogEntryByID is a test seam over cliinstall.ByID so the defensive
// panic below (a host id absent from the compiled catalog, which never
// happens in production — chatwright's own catalog entry always exists) is
// exercisable.
var catalogEntryByID = cliinstall.ByID

// selfUpdateConfig supplies only the Chatwright release identity to the
// shared self-update implementation, resolved from chatwright's own
// compiled-in catalog entry (cliinstall/catalog_chatwright.go in
// strongo/cli-helpers) rather than a hand-maintained duplicate
// (cli-install#req:catalog-identity-single-source). The catalog entry keeps
// chatwright's self-update redirect-only for Homebrew (no
// WithExecutableUpgrade), matching its pre-migration behavior exactly.
// Downloading, verification, replacement, prompting and output all remain
// in github.com/strongo/cli-helpers/selfupdate.
func selfUpdateConfig() selfupdate.Config {
	entry, ok := catalogEntryByID(chatwrightCatalogID)
	if !ok {
		// A host id absent from the compiled catalog is a programming error
		// caught by this package's own tests, never a runtime state a user
		// can trigger (cli-install#req:host-identity-from-catalog).
		panic(fmt.Sprintf("cliinstall: no catalog entry for %q", chatwrightCatalogID))
	}
	return entry.Config(cliBuildInfo().Short())
}

type selfUpdateErrors struct{}

func (selfUpdateErrors) Failure(err error) error {
	return &commandError{code: mapSelfUpdateExitCode(err), err: err}
}

func (selfUpdateErrors) UpdateAvailable(selfupdate.CheckResult) error { return nil }

func mapSelfUpdateExitCode(err error) int {
	var usage *cobracmd.UsageError
	if errors.As(err, &usage) {
		return 2
	}
	switch selfupdate.KindOf(err) {
	case selfupdate.KindNonInteractive, selfupdate.KindDowngrade, selfupdate.KindUnknownTag:
		return 2
	default:
		return 1
	}
}

func newSelfUpdateCommand(stdin io.Reader) *cobra.Command {
	return newSelfUpdateCommandWithConfig(selfUpdateConfig(), stdin)
}

func newSelfUpdateCommandWithConfig(cfg selfupdate.Config, stdin io.Reader) *cobra.Command {
	// cobracmd owns the complete flag surface and execution path. Chatwright
	// supplies its release identity, alias and process-specific exit mapping.
	cmd := cobracmd.New(cfg, cobracmd.CommandOptions{
		Use:        "self-update",
		Short:      "Update the installed binary in place",
		Aliases:    []string{"update"},
		Errors:     selfUpdateErrors{},
		JSONFormat: true,
	})
	cmd.SetIn(stdin)
	addLegacyHelpCommand(cmd)
	return cmd
}
