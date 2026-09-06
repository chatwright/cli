package main

import (
	"errors"
	"fmt"
	"io/fs"

	chatwrightplugin "chatwright.dev/cli/ai/chatwright-cli"
	"github.com/spf13/cobra"
	"github.com/strongo/cli-helpers/skillsync"
	skillscmd "github.com/strongo/cli-helpers/skillsync/cobracmd"
	"github.com/strongo/cli-helpers/skillsync/githubrelease"
)

const (
	chatwrightSkillsDevelopmentVersion = "0.0.0"
	chatwrightSkillsUnknownRevision    = "0000000000000000000000000000000000000000"
)

var (
	chatwrightSkillsCLI    = skillsync.Identity{Publisher: "chatwright", Name: "chatwright"}
	chatwrightSkillsPlugin = skillsync.PluginIdentity{Publisher: "chatwright", Name: "chatwright-cli"}
)

func newSkillsCommand() *cobra.Command {
	cfg, cfgErr := newSkillsSyncConfig()
	cmd := skillscmd.New(cfg, skillscmd.CommandOptions{
		Short:  "Install Chatwright Agent Skills into a harness skills directory",
		Errors: skillsSyncErrors{},
		Resolver: skillsync.ReleaseResolver{
			Source:         githubrelease.Source{},
			CurrentVersion: cfg.CurrentVersion,
		},
	})
	syncCmd, _, findErr := cmd.Find([]string{"sync"})
	if findErr == nil {
		addJSONShortcut(syncCmd)
	}
	cmd.Long = `Install Chatwright's immutable, CLI-matched Agent Skills.

By default, sync reads the skill bundle embedded in this installed binary and
does not need a source checkout or network access. Use --newer-compatible only
to explicitly select a newer compatible published bundle.

With no target, every present Claude, Cursor, and Codex harness is synced. Use
--harness to select a harness or --dir to select an explicit skills directory.`
	if cfgErr != nil {
		cmd.RunE = func(*cobra.Command, []string) error {
			return fmt.Errorf("prepare embedded Chatwright skills: %w", cfgErr)
		}
	}
	return cmd
}

type skillsSyncErrors struct{}

func (skillsSyncErrors) Failure(err error) error {
	var usage *skillscmd.UsageError
	if errors.As(err, &usage) {
		return &commandError{code: 2, err: err}
	}
	return &commandError{code: 1, err: fmt.Errorf("skills sync: %w", err)}
}

func (skillsSyncErrors) Conflict(report skillsync.Report) error {
	return &commandError{code: 1, err: fmt.Errorf(
		"skills sync: %d skill(s) conflict with another plugin or unmanaged directory in %s",
		len(report.Names(skillsync.Conflict)), report.Dir)}
}

func newSkillsSyncConfig() (skillsync.Config, error) {
	source, err := fs.Sub(chatwrightplugin.SkillsFS, "skills")
	if err != nil {
		return skillsync.Config{}, err
	}
	digest, err := skillsync.Digest(source)
	if err != nil {
		return skillsync.Config{}, err
	}
	build := cliBuildInfo()
	revision := build.Commit
	if len(revision) != 40 {
		revision = chatwrightSkillsUnknownRevision
	}
	version := build.Version
	if _, err := skillsync.CompareVersions(version, version); err != nil {
		version = chatwrightSkillsDevelopmentVersion
	}
	descriptor := skillsync.BundleDescriptor{
		Plugin: chatwrightSkillsPlugin,
		Source: skillsync.Source{
			Repository: "github.com/chatwright/cli",
			Path:       "ai/chatwright-cli/skills",
			Revision:   revision,
			Version:    version,
			Digest:     digest,
		},
	}
	if err := skillsync.ValidateDescriptor(descriptor); err != nil {
		return skillsync.Config{}, fmt.Errorf("validate embedded Chatwright skills descriptor: %w", err)
	}
	bundle, err := skillsync.EmbeddedBundle(descriptor, source)
	if err != nil {
		return skillsync.Config{}, fmt.Errorf("bind embedded Chatwright skills: %w", err)
	}
	return skillsync.Config{
		CLI:            chatwrightSkillsCLI,
		CurrentVersion: build.Version,
		Bundles:        []skillsync.Bundle{bundle},
	}, nil
}

func addJSONShortcut(cmd *cobra.Command) {
	var jsonOut bool
	cmd.Flags().BoolVarP(&jsonOut, "json", "j", false, "Output as JSON (shortcut for --format=json)")
	original := cmd.PreRunE
	cmd.PreRunE = func(command *cobra.Command, args []string) error {
		if jsonOut {
			if err := command.Flags().Set("format", "json"); err != nil {
				return err
			}
		}
		if original != nil {
			return original(command, args)
		}
		return nil
	}
}
