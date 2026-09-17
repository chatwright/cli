package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	upgradecobracmd "github.com/strongo/cli-helpers/cliinstall/cobracmd"
)

// AC: cli-install#req:core-framework-neutral, cli-install#req:update-alias-
// policy — the command is named "upgrade", registers the shared upgrade
// flag surface (no --dir), and carries no "update" alias (that alias stays
// on self-update only).
func TestUpgradeCommand_Registration(t *testing.T) {
	cmd := newUpgradeCommand()
	if cmd.Name() != "upgrade" {
		t.Errorf("Name() = %q, want %q", cmd.Name(), "upgrade")
	}
	for _, name := range []string{"all", "check", "yes", "dry-run", "format"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("missing shared flag --%s", name)
		}
	}
	if cmd.Flags().Lookup("dir") != nil {
		t.Error("upgrade must not register --dir")
	}
	for _, alias := range cmd.Aliases {
		if alias == "update" {
			t.Errorf("upgrade command carries an %q alias; REQ: update-alias-policy forbids it", alias)
		}
	}
}

// cli-install#req:host-identity-from-catalog — a host id absent from the
// compiled catalog is a programming error the command constructor panics
// on.
func TestUpgradeCommand_PanicsForUnknownHostID(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected cobracmd.NewUpgrade to panic for an unregistered host id")
		}
	}()
	upgradecobracmd.NewUpgrade(upgradecobracmd.UpgradeCommandOptions{HostID: "nosuchhost-chatwright-test"})
}

// AC: cli-install#req:unknown-target-refused — `chatwright upgrade
// nosuchcli` MUST fail before any confirmation, network request or write,
// with exit code 2 (matching `install nosuchcli` exactly, via the SAME
// installErrors mapper), carrying no "self-update:" prefix.
// cli-install#req:no-network-in-tests: unknown-name validation happens
// before any target — including the host — is probed or looked up.
func TestRunUpgrade_UnknownTargetExitsTwo(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"upgrade", "nosuchcli"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("run(upgrade nosuchcli) exit code = %d, want 2; stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "nosuchcli") {
		t.Errorf("stderr = %q, does not name the unknown target", stderr.String())
	}
	if strings.Contains(stderr.String(), "self-update:") {
		t.Errorf("stderr = %q carries a self-update: prefix; upgrade errors MUST NOT", stderr.String())
	}
}

// --- end-to-end wiring: cobracmd.NewUpgrade + selfupdate.Config.Check
// --- against a fake GitHub releases endpoint, proving `self-update` and
// --- `upgrade chatwright` reach the same verdict from the SAME Config
// --- (cli-install#req:self-update-equals-upgrade-self,
// --- cli-install#req:host-target-is-running-binary,
// --- cli-install#req:no-network-in-tests). selfUpdateTestConfig and
// --- releaseServer are defined in selfupdate_test.go.

func TestUpgrade_SelfUpdateEqualsUpgradeSelf(t *testing.T) {
	server := releaseServer(t, http.StatusOK, `[{"tag_name":"v1.1.0"}]`)
	defer server.Close()
	cfg := selfUpdateTestConfig(server)

	var selfOut, selfErr bytes.Buffer
	if code := runSelfUpdateWithConfig(cfg, []string{"self-update", "--check", "--format", "json"}, &selfOut, &selfErr); code != 0 {
		t.Fatalf("self-update --check code=%d stderr=%q", code, selfErr.String())
	}
	var selfResult struct {
		Current string `json:"current"`
		Latest  string `json:"latest"`
		Verdict string `json:"verdict"`
	}
	if err := json.Unmarshal(selfOut.Bytes(), &selfResult); err != nil {
		t.Fatalf("self-update JSON = %q: %v", selfOut.String(), err)
	}
	if selfResult.Current != "1.0.0" || selfResult.Latest != "1.1.0" || selfResult.Verdict != "update_available" {
		t.Fatalf("self-update result = %+v", selfResult)
	}

	var upOut, upErr bytes.Buffer
	upCmd := newUpgradeCommandWithConfig(cfg)
	upCmd.SetOut(&upOut)
	upCmd.SetErr(&upErr)
	upCmd.SetArgs([]string{"chatwright", "--check", "--format", "json"})
	if err := upCmd.Execute(); err != nil {
		t.Fatalf("upgrade chatwright --check --format json returned error: %v", err)
	}
	if !strings.Contains(upOut.String(), selfResult.Current) || !strings.Contains(upOut.String(), selfResult.Latest) {
		t.Errorf("upgrade chatwright output %q does not report the same current/latest self-update saw (%+v)", upOut.String(), selfResult)
	}
}
