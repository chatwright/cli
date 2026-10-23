package main

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

// commandError carries a CLI exit code after the command has already written
// its diagnostic. Cobra is deliberately kept silent so diagnostics stay on
// their established stream and are never duplicated.
type commandError struct {
	code int
	err  error
}

func (e commandError) Error() string {
	if e.err != nil {
		return e.err.Error()
	}
	return "command failed"
}

func commandResult(code int) error {
	if code == 0 {
		return nil
	}
	return &commandError{code: code}
}

// addLegacyHelpCommand keeps the long-supported "COMMAND help" spelling as
// a real Cobra child. Cobra's generated --help remains available too; this
// child exists for the public CLI vocabulary, not as argument rewriting in a
// wrapper around command execution.
func addLegacyHelpCommand(parent *cobra.Command) {
	parent.AddCommand(&cobra.Command{
		Use:   "help",
		Short: "Show help for this command",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Parent().Help()
		},
	})
}

func executeCommand(root *cobra.Command, args []string, stdout, stderr io.Writer) int {
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)
	if len(args) == 0 {
		_ = root.Help()
		return 0
	}
	if err := root.Execute(); err != nil {
		var commandErr *commandError
		if errors.As(err, &commandErr) {
			if commandErr.err != nil {
				_, _ = fmt.Fprintf(stderr, "chatwright: %v\n", commandErr.err)
			}
			return commandErr.code
		}
		_, _ = fmt.Fprintf(stderr, "chatwright: %v\n", err)
		return 2
	}
	return 0
}
