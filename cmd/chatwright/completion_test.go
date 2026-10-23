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
	if !strings.Contains(stderr.String(), "accepts 1 arg") {
		t.Errorf("stderr = %q, want Cobra argument diagnostic", stderr.String())
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
	if !strings.Contains(stderr.String(), "accepts 1 arg") {
		t.Errorf("stderr = %q, want Cobra argument diagnostic", stderr.String())
	}
}

func TestRunCompletionHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runCompletion([]string{"help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "chatwright completion") {
		t.Errorf("stdout = %q, want Cobra usage text", stdout.String())
	}
}

func TestRunCompletionEachShell(t *testing.T) {
	cases := []struct {
		shell string
		want  string // a shell-specific marker proving the right generator ran.
	}{
		{"bash", "__start_chatwright"},
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

func TestCobraCompletionIncludesCommandTree(t *testing.T) {
	scripts := map[string]string{
		"bash": bashCompletionScript(),
		"zsh":  zshCompletionScript(),
		"fish": fishCompletionScript(),
	}
	for shellName, script := range scripts {
		if script == "" {
			t.Errorf("%s Cobra completion is empty", shellName)
		}
	}
}
