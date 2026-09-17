package main

import (
	"errors"

	"github.com/spf13/cobra"
	installcobracmd "github.com/strongo/cli-helpers/cliinstall/cobracmd"
	"github.com/strongo/cli-helpers/selfupdate"
)

// newInstallCommand returns the "install" command, built from
// github.com/strongo/cli-helpers/cliinstall/cobracmd against chatwright's
// own catalog id (cli-install#req:host-identity-from-catalog).
// `chatwright install` lists the fleet CLIs relevant to chatwright (currently
// specscore, per the catalog's relevance matrix) with their live status, and
// `chatwright install <name>...` installs them the same way chatwright
// itself was installed. installcobracmd.New panics when "chatwright" is
// absent from the compiled catalog — a programming error TestInstall_
// Registration and the cobracmd package's own tests catch, never a runtime
// state a user sees.
func newInstallCommand() *cobra.Command {
	return installcobracmd.New(installcobracmd.CommandOptions{
		Short:  "List and install fleet CLIs relevant to chatwright",
		Errors: installErrors{},
		HostID: chatwrightCatalogID,
	})
}

// installErrors implements installcobracmd.ErrorMapper for chatwright's own
// install command, keeping the same exit-code contract self-update already
// uses (chatwright's messages carry no "self-update: " prefix to begin
// with, so the shared kinds reuse mapSelfUpdateExitCode unchanged):
//   - *installcobracmd.UsageError (an invalid --format, or --all combined
//     with names): 2, the same code chatwright's top-level command runner
//     (executeCommand, cobra.go) already returns for a malformed argument.
//   - selfupdate.KindUnknownTarget: 2 —
//     cli-install#req:unknown-target-refused's underlying error already
//     names the unknown target and lists valid catalog ids.
//   - selfupdate.KindNoInstallDir, selfupdate.KindDestinationExists: 1 —
//     the local filesystem/PATH state prevents the requested operation;
//     chatwright has no dedicated "invalid state" code, so this lands on
//     the same general-failure code every other operational error here
//     already uses.
//   - Every kind mapSelfUpdateExitCode already classifies maps to the
//     EXACT SAME code self-update returns for that kind
//     (cli-install#req:host-owned-exit-codes: "a host maps it as its
//     self-update maps ..."), via mapSelfUpdateExitCode itself rather than
//     a second copy of its switch.
type installErrors struct{}

// Failure maps every install failure per the kinds table on installErrors.
// Also serves as upgrade's own error mapper (see upgrade.go), so the SAME
// table applies to both commands' self-update-shared kinds
// (cli-install#req:host-owned-exit-codes: "The upgrade command MUST use the
// same error mapper").
//
// cliinstall/cobracmd v0.21.0's mapFailure short-circuits a nil err before
// ever calling opts.Errors.Failure (the fix for the known v0.20.0 bug this
// comment used to document), so Failure is never called with nil through
// that path anymore; the guard below stays only because it is trivially
// free and keeps this method nil-safe for any direct caller, including
// TestInstallErrors_FailureNilIsNil.
func (installErrors) Failure(err error) error {
	if err == nil {
		return nil
	}

	var usage *installcobracmd.UsageError
	if errors.As(err, &usage) {
		return &commandError{code: 2, err: err}
	}

	switch selfupdate.KindOf(err) {
	case selfupdate.KindUnknownTarget:
		return &commandError{code: 2, err: err}
	case selfupdate.KindNoInstallDir, selfupdate.KindDestinationExists:
		return &commandError{code: 1, err: err}
	default: // every kind self-update itself already classifies.
		return &commandError{code: mapSelfUpdateExitCode(err), err: err}
	}
}
