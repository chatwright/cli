package term

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeEnv builds a getenv func from a plain map, returning "" for any name
// not present — exactly os.Getenv's own "unset" contract — so tests never
// depend on (or mutate) the real process environment.
func fakeEnv(vars map[string]string) func(string) string {
	return func(name string) string { return vars[name] }
}

func TestColorEnabled(t *testing.T) {
	cases := []struct {
		name        string
		interactive bool
		env         map[string]string
		want        bool
	}{
		{"no env, interactive: on", true, nil, true},
		{"no env, piped: off", false, nil, false},
		{"NO_COLOR set, interactive: off", true, map[string]string{"NO_COLOR": "1"}, false},
		{"NO_COLOR set to any value still counts, interactive: off", true, map[string]string{"NO_COLOR": "0"}, false},
		{"NO_COLOR beats CLICOLOR_FORCE", true, map[string]string{"NO_COLOR": "1", "CLICOLOR_FORCE": "1"}, false},
		{"NO_COLOR empty string does not count (unset)", true, map[string]string{"NO_COLOR": ""}, true},
		{"CLICOLOR_FORCE=1, piped: on anyway", false, map[string]string{"CLICOLOR_FORCE": "1"}, true},
		{"CLICOLOR_FORCE=0 does not force on", false, map[string]string{"CLICOLOR_FORCE": "0"}, false},
		{"CLICOLOR=0, interactive: off", true, map[string]string{"CLICOLOR": "0"}, false},
		{"CLICOLOR=1, interactive: on (no different from default)", true, map[string]string{"CLICOLOR": "1"}, true},
		{"CLICOLOR=0, piped: still off", false, map[string]string{"CLICOLOR": "0"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ColorEnabled(tc.interactive, fakeEnv(tc.env)); got != tc.want {
				t.Errorf("ColorEnabled(%v, %v) = %v, want %v", tc.interactive, tc.env, got, tc.want)
			}
		})
	}
}

func TestASCIIOnly(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want bool
	}{
		{"no locale vars at all: ASCII (cannot confirm UTF-8)", nil, true},
		{"LANG=en_US.UTF-8: not ASCII", map[string]string{"LANG": "en_US.UTF-8"}, false},
		{"LANG=C: ASCII", map[string]string{"LANG": "C"}, true},
		{"LANG=POSIX: ASCII", map[string]string{"LANG": "POSIX"}, true},
		{"lowercase utf8 still recognised", map[string]string{"LANG": "en_US.utf8"}, false},
		{"LC_ALL overrides LANG", map[string]string{"LC_ALL": "C", "LANG": "en_US.UTF-8"}, true},
		{"LC_CTYPE overrides LANG when LC_ALL unset", map[string]string{"LC_CTYPE": "en_US.UTF-8", "LANG": "C"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ASCIIOnly(fakeEnv(tc.env)); got != tc.want {
				t.Errorf("ASCIIOnly(%v) = %v, want %v", tc.env, got, tc.want)
			}
		})
	}
}

func TestIsTerminal(t *testing.T) {
	t.Run("nil is never a terminal", func(t *testing.T) {
		if IsTerminal(nil) {
			t.Error("IsTerminal(nil) = true, want false")
		}
	})
	t.Run("a regular file is never a terminal", func(t *testing.T) {
		f, err := os.Open(filepath.Join(t.TempDir(), "..")) // any openable path; content unused
		if err != nil {
			t.Skipf("could not open a probe file: %v", err)
		}
		defer func() { _ = f.Close() }()
		if IsTerminal(f) {
			t.Error("IsTerminal(regular file) = true, want false")
		}
	})
}

// TestSymbolsFallback proves Profile.Symbols actually switches alphabets —
// non-vacuous by construction, since it asserts both branches produce
// different, specific strings rather than just "non-empty".
func TestSymbolsFallback(t *testing.T) {
	utf8 := Profile{ASCII: false}.Symbols()
	if utf8.Check != "✓" || utf8.Cross != "✗" || utf8.Warn != "⚠" {
		t.Errorf("UTF-8 symbols = %+v, want ✓/✗/⚠", utf8)
	}
	ascii := Profile{ASCII: true}.Symbols()
	if ascii.Check == utf8.Check || ascii.Cross == utf8.Cross || ascii.Warn == utf8.Warn {
		t.Errorf("ASCII symbols = %+v, want a fallback distinct from the UTF-8 set %+v", ascii, utf8)
	}
	for _, s := range []string{ascii.Check, ascii.Cross, ascii.Warn} {
		for _, r := range s {
			if r > 127 {
				t.Errorf("ASCII symbol %q contains a non-ASCII rune %q", s, r)
			}
		}
	}
}

// TestColorGating proves every colour helper is a true no-op (byte-for-byte
// the input, not just "looks similar") when Color is false — the property
// the piped-to-a-file requirement in the CLI's own tests depends on — and
// that it actually adds bytes when Color is true, so this test cannot pass
// by both branches accidentally doing nothing.
func TestColorGating(t *testing.T) {
	off := Profile{Color: false}
	on := Profile{Color: true}
	helpers := []struct {
		name string
		fn   func(Profile, string) string
	}{
		{"Bold", Profile.Bold},
		{"Dim", Profile.Dim},
		{"Red", Profile.Red},
		{"Green", Profile.Green},
		{"Yellow", Profile.Yellow},
		{"Cyan", Profile.Cyan},
	}
	for _, h := range helpers {
		t.Run(h.name, func(t *testing.T) {
			if got := h.fn(off, "text"); got != "text" {
				t.Errorf("%s with Color=false = %q, want %q (no escape bytes at all)", h.name, got, "text")
			}
			if got := h.fn(on, "text"); got == "text" {
				t.Errorf("%s with Color=true = %q, want it to differ from the plain input", h.name, got)
			}
		})
	}
}

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "0ms"},
		{-5 * time.Second, "0ms"},
		{850 * time.Millisecond, "850ms"},
		{999 * time.Millisecond, "999ms"},
		{1200 * time.Millisecond, "1.2s"},
		{59900 * time.Millisecond, "59.9s"},
		{60 * time.Second, "1m00s"},
		{123 * time.Second, "2m03s"},
		{3661 * time.Second, "61m01s"},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			if got := FormatDuration(tc.d); got != tc.want {
				t.Errorf("FormatDuration(%v) = %q, want %q", tc.d, got, tc.want)
			}
		})
	}
}
