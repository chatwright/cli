package main

import (
	"bytes"
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

// runCompletion remains a compatibility seam for package tests. Parsing and
// completion generation are owned by the Cobra command below.
func runCompletion(args []string, stdout, stderr io.Writer) int {
	return run(append([]string{"completion"}, args...), stdout, stderr)
}

func newCompletionCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion [bash|zsh|fish]",
		Short: "Generate a shell completion script",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if args[0] == "help" {
				return cmd.Help()
			}
			return writeCompletion(cmd.Root(), args[0], cmd.OutOrStdout())
		},
	}
	return cmd
}

func writeCompletion(root *cobra.Command, shell string, out io.Writer) error {
	switch shell {
	case "bash":
		return root.GenBashCompletion(out)
	case "zsh":
		return root.GenZshCompletion(out)
	case "fish":
		return root.GenFishCompletion(out, true)
	default:
		return fmt.Errorf("unknown shell %q (want bash, zsh or fish)", shell)
	}
}

func bashCompletionScript() string { return generatedCompletion("bash") }
func zshCompletionScript() string  { return generatedCompletion("zsh") }
func fishCompletionScript() string { return generatedCompletion("fish") }

func generatedCompletion(shell string) string {
	var b bytes.Buffer
	if err := writeCompletion(newRootCommand(nil), shell, &b); err != nil {
		return ""
	}
	return b.String()
}
