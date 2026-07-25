// Package term is a small, dependency-free terminal-capability helper for
// the Chatwright CLI. It answers exactly three questions a runtime-output
// renderer needs — is this a real terminal (so a line can be redrawn in
// place), may ANSI colour be used, and is a UTF-8 symbol (✓/✗/⚠) safe to
// print or must an ASCII fallback be used — and packages the answers as a
// Profile, computed once per output stream.
//
// This package deliberately adds no third-party dependency (no
// golang.org/x/term, no colour library): the CLI ships as a single static
// binary, and TTY/colour detection is a handful of lines once NO_COLOR,
// CLICOLOR and CLICOLOR_FORCE are accounted for. See Profile and NewProfile.
package term

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// Profile is everything a renderer needs to decide how much terminal
// capability it may use for one output stream — computed once (via
// NewProfile) and threaded through rather than re-detected per line.
type Profile struct {
	// Interactive is true when the underlying stream is a real terminal
	// (see IsTerminal) — never influenced by NO_COLOR/CLICOLOR/
	// CLICOLOR_FORCE, which only ever narrow or force *colour*, not
	// interactivity. A renderer uses Interactive (not Color) to decide
	// whether it may redraw a line in place with a bare carriage return: an
	// in-place redraw sent to a pipe or a log file corrupts it regardless
	// of colour, so CLICOLOR_FORCE must never turn that on, and NO_COLOR
	// must never turn it off.
	Interactive bool
	// Color is true when ANSI SGR colour escapes may be written — see
	// ColorEnabled for the precedence NO_COLOR/CLICOLOR/CLICOLOR_FORCE are
	// resolved in.
	Color bool
	// ASCII is true when the stream cannot be trusted to render UTF-8 (see
	// ASCIIOnly) — a renderer must use an ASCII fallback for any symbol it
	// would otherwise print as ✓/✗/⚠.
	ASCII bool
}

// NewProfile computes a Profile for out from whether out is a real
// terminal (IsTerminal) and the process environment (via getenv — pass
// os.Getenv; a function, not the map, so tests can supply a fake one
// without mutating real process environment variables).
func NewProfile(out *os.File, getenv func(string) string) Profile {
	interactive := IsTerminal(out)
	return Profile{
		Interactive: interactive,
		Color:       ColorEnabled(interactive, getenv),
		ASCII:       ASCIIOnly(getenv),
	}
}

// IsTerminal reports whether f is connected to a real terminal rather than
// a pipe, a redirected file, or /dev/null — the same "is this a character
// device" heuristic most dependency-free Go CLIs use in place of a
// platform-specific ioctl (golang.org/x/term's TIOCGETA/GetConsoleMode
// underneath is more precise about *which* fd is a console on Windows, but
// this package's brief is "dependency-light," and a wrong answer here only
// ever costs cosmetic degradation — plain text instead of colour/redraw —
// never a functional one).
func IsTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// ColorEnabled decides whether ANSI colour escapes may be written, given
// whether the stream is interactive and the process environment, in this
// precedence (highest first):
//
//  1. NO_COLOR set to any non-empty value (https://no-color.org) — colour
//     is always off. This is checked first and unconditionally, including
//     ahead of CLICOLOR_FORCE: an explicit opt-out (commonly set org-wide,
//     e.g. by a CI image) must never be silently overridden by a force
//     flag a different tool or shell profile happens to also export.
//  2. CLICOLOR_FORCE set to a non-empty value other than "0" — colour is
//     always on, even when the stream is not a terminal (the convention's
//     own "no matter what").
//  3. CLICOLOR set to exactly "0" — colour is off.
//  4. Otherwise — colour is on exactly when interactive is true (the
//     BSD/CLICOLOR default: colour when attached to a terminal, plain text
//     into a pipe).
func ColorEnabled(interactive bool, getenv func(string) string) bool {
	if getenv("NO_COLOR") != "" {
		return false
	}
	if v := getenv("CLICOLOR_FORCE"); v != "" && v != "0" {
		return true
	}
	if getenv("CLICOLOR") == "0" {
		return false
	}
	return interactive
}

// utf8LocaleEnvVars is the standard POSIX precedence for which locale
// category governs character-set behaviour: LC_ALL overrides every LC_*
// variable, LC_CTYPE specifically governs character classification and
// encoding, and LANG is the fallback default. The first one set (even to a
// non-UTF-8 value) decides; an unset variable is skipped, never treated as
// "no UTF-8."
var utf8LocaleEnvVars = []string{"LC_ALL", "LC_CTYPE", "LANG"}

// ASCIIOnly reports whether output should stick to ASCII rather than UTF-8
// symbols (✓/✗/⚠), read from the same POSIX locale variables a shell
// itself consults (see utf8LocaleEnvVars). No locale variable set at all —
// the common case on a freshly-provisioned CI runner, and always the case
// on Windows, which has no LANG/LC_* convention — is treated as "cannot
// confirm UTF-8" and so, conservatively, ASCII: a missing checkmark is a
// cosmetic downgrade, a mis-rendered one (a UTF-8 sequence a terminal can't
// decode showing as replacement-character boxes) is a worse first
// impression than plain ASCII would have been.
func ASCIIOnly(getenv func(string) string) bool {
	for _, name := range utf8LocaleEnvVars {
		v := getenv(name)
		if v == "" {
			continue
		}
		upper := strings.ToUpper(v)
		return !strings.Contains(upper, "UTF-8") && !strings.Contains(upper, "UTF8")
	}
	return true
}

// Symbols is the three glyphs `chatwright run` uses for a completed,
// failed and warned condition, in whichever alphabet p.Symbols selects.
type Symbols struct {
	Check string
	Cross string
	Warn  string
}

// Symbols returns p's glyph set: UTF-8 (✓/✗/⚠) normally, a plain-ASCII
// fallback ([OK]/[FAIL]/[WARN]) when p.ASCII.
func (p Profile) Symbols() Symbols {
	if p.ASCII {
		return Symbols{Check: "[OK]", Cross: "[FAIL]", Warn: "[WARN]"}
	}
	return Symbols{Check: "✓", Cross: "✗", Warn: "⚠"}
}

// ANSI SGR codes this package's colour helpers use. Never exported: a
// caller always goes through Profile's own methods, which gate every one
// of these behind p.Color, so an escape code can never leak onto a stream
// this Profile decided colour is off for (NO_COLOR, a non-terminal
// destination, CLICOLOR=0).
const (
	ansiReset  = "\x1b[0m"
	ansiBold   = "\x1b[1m"
	ansiDim    = "\x1b[2m"
	ansiRed    = "\x1b[31m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
	ansiCyan   = "\x1b[36m"
)

// colorize wraps s in code/ansiReset when p.Color, and returns s verbatim
// (no escape bytes at all, not even a no-op reset) otherwise — the single
// choke point every exported colour helper below goes through.
func (p Profile) colorize(code, s string) string {
	if !p.Color || s == "" {
		return s
	}
	return code + s + ansiReset
}

// Bold, Dim, Red, Green, Yellow and Cyan each wrap s in the named SGR
// colour/style when p.Color, or return s unchanged otherwise — see
// colorize.
func (p Profile) Bold(s string) string   { return p.colorize(ansiBold, s) }
func (p Profile) Dim(s string) string    { return p.colorize(ansiDim, s) }
func (p Profile) Red(s string) string    { return p.colorize(ansiRed, s) }
func (p Profile) Green(s string) string  { return p.colorize(ansiGreen, s) }
func (p Profile) Yellow(s string) string { return p.colorize(ansiYellow, s) }
func (p Profile) Cyan(s string) string   { return p.colorize(ansiCyan, s) }

// FormatDuration renders d for a human reader, the way `chatwright run`'s
// summary and progress lines both need (never a bare nanosecond count, and
// never time.Duration's own String, whose "1h2m3.456789s" full-precision
// tail is noise for a CLI's own progress/summary output):
//
//   - under one second: whole milliseconds, e.g. "850ms";
//   - under one minute: one decimal place of seconds, e.g. "1.2s";
//   - one minute or more: minutes and whole seconds, e.g. "2m03s".
//
// A negative d is treated as zero (clock skew between two injected times
// should never render as a negative duration a user has to puzzle over).
func FormatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%.1fs", d.Seconds())
	default:
		m := int(d / time.Minute)
		s := int((d - time.Duration(m)*time.Minute) / time.Second)
		return fmt.Sprintf("%dm%02ds", m, s)
	}
}
