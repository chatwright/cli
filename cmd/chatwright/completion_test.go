package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunCompletionMissingShell(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runCompletion(nil, &stdout, &stderr); code != 2 {
		t.Fatalf("runCompletion(nil) code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "missing SHELL") {
		t.Errorf("stderr = %q, want a missing-SHELL message", stderr.String())
	}
}

func TestRunCompletionUnknownShell(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runCompletion([]string{"powershell"}, &stdout, &stderr); code != 2 {
		t.Fatalf("runCompletion([powershell]) code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), `unknown shell "powershell"`) {
		t.Errorf("stderr = %q, want an unknown-shell message", stderr.String())
	}
}

func TestRunCompletionExtraArgument(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runCompletion([]string{"bash", "extra"}, &stdout, &stderr); code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "unexpected extra argument") {
		t.Errorf("stderr = %q, want an unexpected-extra-argument message", stderr.String())
	}
}

func TestRunCompletionHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runCompletion([]string{"help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "chatwright completion bash") {
		t.Errorf("stdout = %q, want usage text", stdout.String())
	}
}

func TestRunCompletionEachShell(t *testing.T) {
	cases := []struct {
		shell string
		want  string // a shell-specific marker proving the right generator ran.
	}{
		{"bash", "complete -F _chatwright chatwright"},
		{"zsh", "#compdef chatwright"},
		{"fish", "complete -c chatwright"},
	}
	for _, tc := range cases {
		t.Run(tc.shell, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := runCompletion([]string{tc.shell}, &stdout, &stderr)
			if code != 0 {
				t.Fatalf("runCompletion([%q]) code = %d, want 0; stderr=%q", tc.shell, code, stderr.String())
			}
			if stderr.String() != "" {
				t.Errorf("stderr = %q, want empty", stderr.String())
			}
			if !strings.Contains(stdout.String(), tc.want) {
				t.Errorf("stdout for %s = %q, want it to contain %q", tc.shell, stdout.String(), tc.want)
			}
		})
	}
}

// TestCompletionScriptsCoverEveryCommand guards against exactly the kind
// of drift this file's own package doc comment warns about: a command or
// flag added to one shell's generator (or to main.go/run.go/arena.go/
// server.go's own dispatch) but forgotten in another. Non-vacuous: it
// fails the moment any vocabulary entry is missing from any one script,
// not just "the script is non-empty".
func TestCompletionScriptsCoverEveryCommand(t *testing.T) {
	scripts := map[string]string{
		"bash": bashCompletionScript(),
		"zsh":  zshCompletionScript(),
		"fish": fishCompletionScript(),
	}
	vocab := map[string][]string{
		"topLevelCommands":  topLevelCommands,
		"arenaSubcommands":  arenaSubcommands,
		"serverSubcommands": serverSubcommands,
		"completionShells":  completionShells,
	}
	for shellName, script := range scripts {
		for listName, entries := range vocab {
			for _, entry := range entries {
				if !strings.Contains(script, entry) {
					t.Errorf("%s script is missing %s entry %q", shellName, listName, entry)
				}
			}
		}
		for _, flag := range runFlags {
			bare := strings.TrimPrefix(flag, "--")
			if !strings.Contains(script, bare) {
				t.Errorf("%s script is missing run flag %q", shellName, flag)
			}
		}
		for _, flag := range selfUpdateFlags {
			bare := strings.TrimPrefix(flag, "--")
			if !strings.Contains(script, bare) {
				t.Errorf("%s script is missing self-update flag %q", shellName, flag)
			}
		}
	}
}
