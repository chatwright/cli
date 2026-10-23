package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	installcobracmd "github.com/strongo/cli-helpers/cliinstall/cobracmd"
	"github.com/strongo/cli-helpers/selfupdate"
)

// AC: cli-install#req:host-identity-from-catalog — the command is
// registered under chatwright's own catalog id and does not panic
// (installcobracmd.New panics when the host id is absent from the compiled
// catalog).
func TestInstallCommand_Registration(t *testing.T) {
	cmd := newInstallCommand()
	if cmd.Name() != "install" {
		t.Errorf("Name() = %q, want %q", cmd.Name(), "install")
	}
}

// cli-install#req:host-identity-from-catalog — a host id absent from the
// compiled catalog is a programming error the command constructor panics
// on. This proves the mechanism installcobracmd.New documents, using a
// bogus id rather than "chatwright" (which always exists).
func TestInstallCommand_PanicsForUnknownHostID(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected installcobracmd.New to panic for an unregistered host id")
		}
	}()
	installcobracmd.New(installcobracmd.CommandOptions{HostID: "nosuchhost-chatwright-test"})
}

// AC: cli-install#ac:direct-install-writes-only-verified-new-files,
// cli-install#req:unknown-target-refused — `chatwright install nosuchcli`
// MUST fail before any confirmation, network request or write, with exit
// code 2 and a message naming the unknown target, carrying no
// "self-update:" prefix. Plan() rejects every unknown name before probing
// anything, so this is offline-safe with no injected Env
// (cli-install#req:no-network-in-tests).
func TestRunInstall_UnknownTargetExitsTwo(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"install", "nosuchcli"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("run(install nosuchcli) exit code = %d, want 2; stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "nosuchcli") {
		t.Errorf("stderr = %q, does not name the unknown target", stderr.String())
	}
	if strings.Contains(stderr.String(), "self-update:") {
		t.Errorf("stderr = %q carries a self-update: prefix; install errors MUST NOT (cli-install#req:host-owned-exit-codes)", stderr.String())
	}
}

// AC: cli-install#req:host-owned-exit-codes — installErrors.Failure maps
// every kind explicitly, and no message carries a "self-update:" prefix.
func TestInstallErrors_FailureExitCodes(t *testing.T) {
	cases := []struct {
		name string
		kind selfupdate.FailureKind
		want int
	}{
		{"unknown_target", selfupdate.KindUnknownTarget, 2},
		{"no_install_dir", selfupdate.KindNoInstallDir, 1},
		{"destination_exists", selfupdate.KindDestinationExists, 1},
		{"non_interactive", selfupdate.KindNonInteractive, 2},
		{"downgrade", selfupdate.KindDowngrade, 2},
		{"unknown_tag", selfupdate.KindUnknownTag, 2},
		{"permission", selfupdate.KindPermission, 1},
		{"ambiguous", selfupdate.KindAmbiguous, 1},
		{"checksum", selfupdate.KindChecksum, 1},
		{"release_lookup", selfupdate.KindReleaseLookup, 1},
		{"download", selfupdate.KindDownload, 1},
		{"unsupported_platform", selfupdate.KindUnsupportedPlatform, 1},
		{"managed_version", selfupdate.KindManagedVersion, 1},
		{"managed_command", selfupdate.KindManagedCommand, 1},
		{"unexpected", selfupdate.KindUnexpected, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := installErrors{}.Failure(&selfupdate.Failure{Kind: c.kind, Err: errors.New("boom")})
			var ce *commandError
			if !errors.As(err, &ce) {
				t.Fatalf("error %T is not a *commandError", err)
			}
			if ce.code != c.want {
				t.Errorf("code = %d, want %d", ce.code, c.want)
			}
			if strings.Contains(err.Error(), "self-update:") {
				t.Errorf("message %q carries a self-update: prefix; install errors MUST NOT", err.Error())
			}
		})
	}
}

// A nil err IS a real, reachable call on the success/dry-run path — see
// installErrors.Failure's own doc comment for why cliinstall/cobracmd
// v0.20.0 calls opts.Errors.Failure(nil) on every successful run.
func TestInstallErrors_FailureNilIsNil(t *testing.T) {
	if err := (installErrors{}).Failure(nil); err != nil {
		t.Errorf("Failure(nil) = %v, want nil", err)
	}
}

// *installcobracmd.UsageError (an invalid --format, or --all combined with
// names) MUST map to 2, matching chatwright's own usage-error convention
// (executeCommand's fallback for an unmapped error, cobra.go).
func TestInstallErrors_FailureUsageError(t *testing.T) {
	err := installErrors{}.Failure(&installcobracmd.UsageError{Err: errors.New("invalid --format")})
	var ce *commandError
	if !errors.As(err, &ce) {
		t.Fatalf("error %T is not a *commandError", err)
	}
	if ce.code != 2 {
		t.Errorf("code = %d, want 2", ce.code)
	}
}

// installErrors and selfUpdateErrors MUST agree on the exit code for every
// kind self-update itself already classifies
// (cli-install#req:host-owned-exit-codes: "a host maps it as its
// self-update maps ..." — extended here to every shared failure kind), so
// `install <name>` and `self-update` give the same code for the same
// underlying failure.
func TestInstallErrors_SharedKindsMatchSelfUpdateErrors(t *testing.T) {
	shared := []selfupdate.FailureKind{
		selfupdate.KindAmbiguous, selfupdate.KindReleaseLookup, selfupdate.KindDownload,
		selfupdate.KindChecksum, selfupdate.KindPermission, selfupdate.KindNonInteractive,
		selfupdate.KindDowngrade, selfupdate.KindUnknownTag, selfupdate.KindUnsupportedPlatform,
		selfupdate.KindManagedVersion, selfupdate.KindManagedCommand, selfupdate.KindUnexpected,
	}
	for _, kind := range shared {
		t.Run(kind.String(), func(t *testing.T) {
			installErr := installErrors{}.Failure(&selfupdate.Failure{Kind: kind, Err: errors.New("boom")})
			selfUpdateErr := selfUpdateErrors{}.Failure(&selfupdate.Failure{Kind: kind, Err: errors.New("boom")})
			var installCE, selfUpdateCE *commandError
			if !errors.As(installErr, &installCE) {
				t.Fatalf("install error %T is not a *commandError", installErr)
			}
			if !errors.As(selfUpdateErr, &selfUpdateCE) {
				t.Fatalf("self-update error %T is not a *commandError", selfUpdateErr)
			}
			if installCE.code != selfUpdateCE.code {
				t.Errorf("install code = %d, self-update code = %d; want equal for shared kind %v",
					installCE.code, selfUpdateCE.code, kind)
			}
		})
	}
}
