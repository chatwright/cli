// Command chatwright is the local command-line entry point for the Chatwright
// conversation execution platform.
package main

import (
	"fmt"
	"io"
	"os"
)

const version = "0.1.0-dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stdout)
		return 0
	}

	switch args[0] {
	case "help", "-h", "--help":
		printUsage(stdout)
		return 0
	case "version", "--version":
		_, _ = fmt.Fprintf(stdout, "chatwright %s\n", version)
		return 0
	case "platforms":
		_, _ = fmt.Fprintln(stdout, "telegram\ttext, inline actions, edits")
		_, _ = fmt.Fprintln(stdout, "whatsapp\ttext (experimental)")
		return 0
	default:
		_, _ = fmt.Fprintf(stderr, "chatwright: unknown command %q\n\n", args[0])
		printUsage(stderr)
		return 2
	}
}

func printUsage(w io.Writer) {
	_, _ = fmt.Fprintln(w, `Chatwright CLI

Usage:
  chatwright <command>

Commands:
  platforms   List built-in messaging platform emulators
  version     Print the CLI version
  help        Show this help`)
}
