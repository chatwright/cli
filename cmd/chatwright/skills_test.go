package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/strongo/cli-helpers/skillsync"
)

func TestNewSkillsSyncConfigBindsEmbeddedChatwrightPlugin(t *testing.T) {
	cfg, err := newSkillsSyncConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CLI != chatwrightSkillsCLI {
		t.Errorf("CLI = %+v, want %+v", cfg.CLI, chatwrightSkillsCLI)
	}
	if len(cfg.Bundles) != 1 {
		t.Fatalf("bundles = %d, want one", len(cfg.Bundles))
	}
	bundle := cfg.Bundles[0]
	if bundle.Plugin != chatwrightSkillsPlugin {
		t.Errorf("plugin = %+v, want %+v", bundle.Plugin, chatwrightSkillsPlugin)
	}
	if bundle.Source.Repository != "github.com/chatwright/cli" ||
		bundle.Source.Path != "ai/chatwright-cli/skills" {
		t.Errorf("source = %+v", bundle.Source)
	}
	if bundle.Source.Digest == "" {
		t.Error("embedded bundle digest is empty")
	}
}

func TestSkillsSyncUsesJSONShortcutAndIsIdempotent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "skills")
	first := newSkillsCommand()
	first.SetArgs([]string{"sync", "--dir", dir, "--json"})
	var output bytes.Buffer
	first.SetOut(&output)
	if err := first.Execute(); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Changes []skillsync.Change `json:"changes"`
	}
	if err := json.Unmarshal(output.Bytes(), &payload); err != nil {
		t.Fatalf("--json output is not valid JSON: %v\n%s", err, output.String())
	}
	if len(payload.Changes) == 0 {
		t.Fatal("first sync reported no changes")
	}
	if _, err := os.Stat(filepath.Join(dir, "chatwright-arena-server", "SKILL.md")); err != nil {
		t.Fatalf("installed skill missing: %v", err)
	}

	second := newSkillsCommand()
	second.SetArgs([]string{"sync", "--dir", dir, "--format", "json"})
	output.Reset()
	second.SetOut(&output)
	if err := second.Execute(); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(output.Bytes(), &payload); err != nil {
		t.Fatalf("--format=json output is not valid JSON: %v\n%s", err, output.String())
	}
	for _, change := range payload.Changes {
		if change.Action != skillsync.Unchanged {
			t.Errorf("second sync action = %q, want unchanged", change.Action)
		}
	}
}

func TestSkillsSyncExposesExplicitNewerCompatibleMode(t *testing.T) {
	cmd := newSkillsCommand()
	syncCmd, _, err := cmd.Find([]string{"sync"})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"format", "json", "newer-compatible"} {
		if syncCmd.Flags().Lookup(name) == nil {
			t.Errorf("sync flag --%s is missing", name)
		}
	}
}
