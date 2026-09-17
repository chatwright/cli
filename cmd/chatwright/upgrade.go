package main

import (
	"github.com/spf13/cobra"

	upgradecobracmd "github.com/strongo/cli-helpers/cliinstall/cobracmd"
	"github.com/strongo/cli-helpers/selfupdate"
)

// newUpgradeCommand returns the "upgrade" command, built from
// github.com/strongo/cli-helpers/cliinstall/cobracmd against chatwright's
// own catalog id, resolving its own release identity the normal way
// (cli-install#req:host-identity-from-catalog).
func newUpgradeCommand() *cobra.Command {
	return newUpgradeCommandWithConfig(selfUpdateConfig())
}

// newUpgradeCommandWithConfig builds the "upgrade" command against an
// explicit selfupdate.Config for cfg's HostConfig — the SAME shape
// newRootCommandWithSelfUpdateConfig already uses to inject a fixture
// server into self-update for tests
// (cli-install#req:no-network-in-tests). `chatwright upgrade` (no
// arguments) reports every installed catalog CLI plus chatwright itself;
// `chatwright upgrade --all`/`chatwright upgrade <name>...` upgrade what
// the report showed. chatwright is always upgraded last, classified and
// versioned from its OWN self-update Config (never a PATH probe of its own
// binary), so `chatwright self-update` and `chatwright upgrade chatwright`
// reach the exact same library call whenever both are built from the SAME
// cfg (cli-install#req:self-update-equals-upgrade-self,
// cli-install#req:host-target-is-running-binary). chatwright's self-update
// configures no AfterUpdate hook, so HostAfterUpdate is left nil.
func newUpgradeCommandWithConfig(cfg selfupdate.Config) *cobra.Command {
	return upgradecobracmd.NewUpgrade(upgradecobracmd.UpgradeCommandOptions{
		Short:      "Upgrade installed fleet CLIs, including this one",
		HostID:     chatwrightCatalogID,
		Errors:     installErrors{},
		HostConfig: cfg,
	})
}
