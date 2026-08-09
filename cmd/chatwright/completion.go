// completion.go implements `chatwright completion bash|zsh|fish` — item 9
// of the UX brief ("Shell completion (bash/zsh/fish) if it can be done
// without a heavy dependency"). It genuinely can: this repository's own
// command surface is small and static (a fixed set of top-level commands,
// a fixed set of flags per subcommand), so the three scripts below are
// plain hand-written shell text with no code-generation framework behind
// them (no cobra, no urfave/cli) — the same "small internal helper is
// right, a framework is not" line the brief draws for terminal capability
// detection applies here too. Keeping the vocab lists (topLevelCommands,
// runFlags, arenaSubcommands, serverSubcommands, selfUpdateFlags) as the
// single source of truth for all three shells is this file's own
// discipline against drift between them; there is no schema forcing it,
// only TestCompletionScriptsCoverEveryCommand checking it.
package main

import (
	"fmt"
	"io"
	"strings"
)

// topLevelCommands, runFlags, arenaSubcommands, serverSubcommands and
// selfUpdateFlags are this file's single source of truth for what every
// shell's completion script offers — kept here, not duplicated per shell,
// precisely so adding a command/flag in main.go/run.go/arena.go/server.go/
// selfupdate.go later has one obvious place to also update (and one test,
// TestCompletionScriptsCoverEveryCommand, that fails loudly if it isn't).
var (
	topLevelCommands  = []string{"platforms", "run", "arena", "server", "version", "help", "completion", "self-update", "update"}
	runFlags          = []string{"--out", "--write", "--json", "--quiet", "--verbose", "--help"}
	arenaSubcommands  = []string{"run", "report", "help"}
	serverSubcommands = []string{"serve", "start", "stop", "restart", "help"}
	completionShells  = []string{"bash", "zsh", "fish"}
	// selfUpdateFlags completes both "self-update" and its "update" alias
	// (see main.go's dispatch and selfupdate.go). Only the canonical
	// "--flag" spellings are completed, matching runFlags' own precedent —
	// the short "-y" alias for --yes is not offered here either.
	selfUpdateFlags = []string{"--check", "--yes", "--version", "--allow-downgrade", "--dry-run", "--format"}
)

// runCompletion implements `chatwright completion SHELL`.
func runCompletion(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && (args[0] == "help" || args[0] == "-h" || args[0] == "--help") {
		printCompletionUsage(stdout)
		return 0
	}
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, "chatwright completion: missing SHELL (bash|zsh|fish)")
		printCompletionUsage(stderr)
		return 2
	}
	if len(args) > 1 {
		_, _ = fmt.Fprintf(stderr, "chatwright completion: unexpected extra argument %q\n\n", args[1])
		printCompletionUsage(stderr)
		return 2
	}

	switch args[0] {
	case "bash":
		_, _ = io.WriteString(stdout, bashCompletionScript())
	case "zsh":
		_, _ = io.WriteString(stdout, zshCompletionScript())
	case "fish":
		_, _ = io.WriteString(stdout, fishCompletionScript())
	default:
		_, _ = fmt.Fprintf(stderr, "chatwright completion: unknown shell %q (want bash, zsh or fish)\n\n", args[0])
		printCompletionUsage(stderr)
		return 2
	}
	return 0
}

func printCompletionUsage(w io.Writer) {
	_, _ = fmt.Fprintln(w, `Generate a shell completion script for bash, zsh or fish.

Usage:
  chatwright completion bash > /usr/local/etc/bash_completion.d/chatwright
  chatwright completion zsh  > "${fpath[1]}/_chatwright"
  chatwright completion fish > ~/.config/fish/completions/chatwright.fish

Completes: top-level commands, run's own flags (--out/--write/--json/
--quiet/--verbose/--help) and the literal "example" DOCUMENT value, the
arena/server/completion subcommand names, and self-update's own flags
(--check/--yes/--version/--allow-downgrade/--dry-run/--format) under both
"self-update" and its "update" alias. It does not complete file paths
beyond the shell's own default filename completion.`)
}

// bashCompletionScript is a hand-written bash completion function — no
// bash-completion package helpers (_init_completion) assumed installed,
// so this works on a bare bash with the builtin `complete`/`compgen` only.
func bashCompletionScript() string {
	return fmt.Sprintf(`# chatwright bash completion
# Install: chatwright completion bash > /usr/local/etc/bash_completion.d/chatwright
# (or source it directly from your ~/.bashrc)
_chatwright() {
	local cur words cword
	cur="${COMP_WORDS[COMP_CWORD]}"
	COMPREPLY=()

	if [[ $COMP_CWORD -eq 1 ]]; then
		COMPREPLY=($(compgen -W "%s" -- "$cur"))
		return 0
	fi

	case "${COMP_WORDS[1]}" in
	run)
		COMPREPLY=($(compgen -W "%s example" -- "$cur"))
		;;
	arena)
		if [[ $COMP_CWORD -eq 2 ]]; then
			COMPREPLY=($(compgen -W "%s" -- "$cur"))
		fi
		;;
	server)
		if [[ $COMP_CWORD -eq 2 ]]; then
			COMPREPLY=($(compgen -W "%s" -- "$cur"))
		fi
		;;
	completion)
		if [[ $COMP_CWORD -eq 2 ]]; then
			COMPREPLY=($(compgen -W "%s" -- "$cur"))
		fi
		;;
	self-update|update)
		COMPREPLY=($(compgen -W "%s" -- "$cur"))
		;;
	esac
}
complete -F _chatwright chatwright
`, strings.Join(topLevelCommands, " "), strings.Join(runFlags, " "), strings.Join(arenaSubcommands, " "),
		strings.Join(serverSubcommands, " "), strings.Join(completionShells, " "), strings.Join(selfUpdateFlags, " "))
}

// zshCompletionScript wraps bashCompletionScript via bashcompinit — the
// same technique long-established Go CLIs (kubectl among them) use for
// zsh, rather than hand-authoring a parallel _arguments-based completion
// that would need to be kept in sync with the bash one by hand.
func zshCompletionScript() string {
	return "#compdef chatwright\nautoload -U +X bashcompinit && bashcompinit\n" + bashCompletionScript()
}

// fishCompletionScript is a hand-written fish completion file using
// fish's own __fish_use_subcommand/__fish_seen_subcommand_from helpers,
// fish's idiomatic way to scope completions to "no subcommand chosen yet"
// versus "already inside subcommand X" — the same two states the bash
// script's own COMP_CWORD/case logic distinguishes.
func fishCompletionScript() string {
	var b strings.Builder
	_, _ = b.WriteString("# chatwright fish completion\n")
	_, _ = b.WriteString("# Install: chatwright completion fish > ~/.config/fish/completions/chatwright.fish\n")
	for _, c := range topLevelCommands {
		_, _ = fmt.Fprintf(&b, "complete -c chatwright -f -n '__fish_use_subcommand' -a %s\n", c)
	}
	for _, f := range runFlags {
		_, _ = fmt.Fprintf(&b, "complete -c chatwright -f -n '__fish_seen_subcommand_from run' -l %s\n", strings.TrimPrefix(f, "--"))
	}
	_, _ = b.WriteString("complete -c chatwright -f -n '__fish_seen_subcommand_from run' -a example\n")
	for _, s := range arenaSubcommands {
		_, _ = fmt.Fprintf(&b, "complete -c chatwright -f -n '__fish_seen_subcommand_from arena' -a %s\n", s)
	}
	for _, s := range serverSubcommands {
		_, _ = fmt.Fprintf(&b, "complete -c chatwright -f -n '__fish_seen_subcommand_from server' -a %s\n", s)
	}
	for _, s := range completionShells {
		_, _ = fmt.Fprintf(&b, "complete -c chatwright -f -n '__fish_seen_subcommand_from completion' -a %s\n", s)
	}
	for _, f := range selfUpdateFlags {
		_, _ = fmt.Fprintf(&b, "complete -c chatwright -f -n '__fish_seen_subcommand_from self-update update' -l %s\n", strings.TrimPrefix(f, "--"))
	}
	return b.String()
}
