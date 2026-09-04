package main

import (
	"errors"
	"io"

	"github.com/spf13/cobra"
	"github.com/strongo/cli-helpers/selfupdate"
	"github.com/strongo/cli-helpers/selfupdate/cobracmd"
)

const selfUpdateRepository = "chatwright/cli"
const selfUpdateBinaryName = "chatwright"
const undeterminedVersion = "dev"

var selfUpdateUndeterminedVersions = []string{undeterminedVersion, "(devel)"}

// selfUpdateConfig supplies only the Chatwright release identity to the
// shared self-update implementation. Downloading, verification, replacement,
// prompting and output all remain in github.com/strongo/cli-helpers/selfupdate.
func selfUpdateConfig() selfupdate.Config {
	return selfupdate.Config{
		BinaryName:           selfUpdateBinaryName,
		Repository:           selfUpdateRepository,
		CurrentVersion:       cliBuildInfo().Short(),
		UndeterminedVersions: selfUpdateUndeterminedVersions,
		Managers: []selfupdate.Manager{
			selfupdate.Homebrew("brew upgrade --cask chatwright"),
		},
		SupportedPlatforms: []selfupdate.Platform{
			{GOOS: "linux", GOARCH: "amd64"}, {GOOS: "linux", GOARCH: "arm64"},
			{GOOS: "darwin", GOARCH: "amd64"}, {GOOS: "darwin", GOARCH: "arm64"},
			{GOOS: "windows", GOARCH: "amd64"},
		},
		VersionProbeArgs: []string{"version"},
	}
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
