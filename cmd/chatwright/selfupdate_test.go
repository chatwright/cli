package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/strongo/cli-helpers/selfupdate"
)

func TestSelfUpdateConfigIdentity(t *testing.T) {
	cfg := selfUpdateConfig()
	if cfg.BinaryName != "chatwright" || cfg.Repository != "chatwright/cli" {
		t.Fatalf("identity = %q/%q", cfg.BinaryName, cfg.Repository)
	}
	if cfg.CurrentVersion != cliBuildInfo().Short() {
		t.Fatalf("CurrentVersion = %q", cfg.CurrentVersion)
	}
	if len(cfg.VersionProbeArgs) != 1 || cfg.VersionProbeArgs[0] != "version" {
		t.Fatalf("VersionProbeArgs = %v", cfg.VersionProbeArgs)
	}
	if len(cfg.Managers) != 1 || cfg.Managers[0].UpgradeCommand != "brew upgrade --cask chatwright" {
		t.Fatalf("Managers = %+v", cfg.Managers)
	}
}

func TestSelfUpdateCommandUsesSharedCobraAdapter(t *testing.T) {
	cmd := newSelfUpdateCommand(strings.NewReader(""))
	if !strings.Contains(strings.Join(cmd.Aliases, ","), "update") {
		t.Fatal("missing update alias")
	}
	for _, name := range []string{"check", "yes", "version", "allow-downgrade", "dry-run", "format"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("missing shared flag --%s", name)
		}
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"self-update", "--help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("help code=%d stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Aliases:") || !strings.Contains(stdout.String(), "update") {
		t.Fatalf("help = %q", stdout.String())
	}
}

func TestSelfUpdateCheckJSONAndAliasUseSharedAdapter(t *testing.T) {
	server := releaseServer(t, http.StatusOK, `[{"tag_name":"v1.1.0"}]`)
	defer server.Close()
	cfg := selfUpdateTestConfig(server)

	var directOut, directErr bytes.Buffer
	if code := runSelfUpdateWithConfig(cfg, []string{"self-update", "--check", "--format", "json"}, &directOut, &directErr); code != 0 {
		t.Fatalf("self-update --check code=%d stderr=%q", code, directErr.String())
	}
	var direct struct {
		Current string `json:"current"`
		Latest  string `json:"latest"`
		Verdict string `json:"verdict"`
	}
	if err := json.Unmarshal(directOut.Bytes(), &direct); err != nil {
		t.Fatalf("self-update JSON = %q: %v", directOut.String(), err)
	}
	if direct.Current != "1.0.0" || direct.Latest != "1.1.0" || direct.Verdict != "update_available" {
		t.Fatalf("self-update result = %+v", direct)
	}

	var aliasOut, aliasErr bytes.Buffer
	if code := runSelfUpdateWithConfig(cfg, []string{"update", "--check", "--format", "json"}, &aliasOut, &aliasErr); code != 0 {
		t.Fatalf("update --check code=%d stderr=%q", code, aliasErr.String())
	}
	if aliasOut.String() != directOut.String() || aliasErr.Len() != 0 {
		t.Fatalf("alias output=%q stderr=%q; direct output=%q", aliasOut.String(), aliasErr.String(), directOut.String())
	}
}

func TestSelfUpdateReleaseLookupFailureExitsOne(t *testing.T) {
	server := releaseServer(t, http.StatusInternalServerError, "fixture unavailable")
	defer server.Close()
	var stdout, stderr bytes.Buffer
	if code := runSelfUpdateWithConfig(selfUpdateTestConfig(server), []string{"self-update", "--check"}, &stdout, &stderr); code != 1 {
		t.Fatalf("release lookup code=%d, want 1; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "github releases request failed") {
		t.Fatalf("stderr=%q, want release lookup diagnostic", stderr.String())
	}
}

func TestSelfUpdateUsageFailuresExitTwo(t *testing.T) {
	server := releaseServer(t, http.StatusOK, `[{"tag_name":"v1.1.0"}]`)
	defer server.Close()
	for _, args := range [][]string{
		{"self-update", "--check", "--format", "yaml"},
		{"self-update", "--check", "extra"},
	} {
		var stdout, stderr bytes.Buffer
		if code := runSelfUpdateWithConfig(selfUpdateTestConfig(server), args, &stdout, &stderr); code != 2 {
			t.Fatalf("%v code=%d, want 2; stderr=%q", args, code, stderr.String())
		}
	}
}

func TestSelfUpdateWriterFailureExitsOne(t *testing.T) {
	server := releaseServer(t, http.StatusOK, `[{"tag_name":"v1.1.0"}]`)
	defer server.Close()
	var stderr bytes.Buffer
	if code := runSelfUpdateWithConfig(selfUpdateTestConfig(server), []string{"self-update", "--check", "--format", "json"}, failingWriter{}, &stderr); code != 1 {
		t.Fatalf("writer failure code=%d, want 1; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "write failed") {
		t.Fatalf("stderr=%q, want writer error", stderr.String())
	}
}

func TestSelfUpdateExitMapping(t *testing.T) {
	for _, tc := range []struct {
		kind selfupdate.FailureKind
		want int
	}{
		{selfupdate.KindNonInteractive, 2}, {selfupdate.KindDowngrade, 2}, {selfupdate.KindUnknownTag, 2},
		{selfupdate.KindPermission, 1}, {selfupdate.KindChecksum, 1}, {selfupdate.KindReleaseLookup, 1},
	} {
		err := &selfupdate.Failure{Kind: tc.kind, Err: errors.New("fixture")}
		if got := mapSelfUpdateExitCode(err); got != tc.want {
			t.Errorf("%v => %d, want %d", tc.kind, got, tc.want)
		}
	}
}

func selfUpdateTestConfig(server *httptest.Server) selfupdate.Config {
	return selfupdate.Config{
		BinaryName:     selfUpdateBinaryName,
		Repository:     selfUpdateRepository,
		CurrentVersion: "1.0.0",
		ReleasesAPIURL: server.URL,
		HTTPClient:     server.Client(),
	}
}

func releaseServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method=%s, want GET", r.Method)
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
}

func runSelfUpdateWithConfig(cfg selfupdate.Config, args []string, stdout, stderr io.Writer) int {
	return executeCommand(newRootCommandWithSelfUpdateConfig(strings.NewReader(""), cfg), args, stdout, stderr)
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }
