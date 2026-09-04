// server.go wires chatwright.dev/cli/internal/server into the CLI as
// `chatwright server serve|start|stop|restart`. It is deliberately thin per
// this repository's own AGENTS.md ("the CLI is deliberately thin ... engine
// or wire logic never lives here"): every HTTP handler, the reverse proxy,
// the metrics ring buffer, the datastate evaluation seam, and the PID-file/
// daemon primitives live in internal/server; this file only parses flags
// (falling back to environment variables, then fixed defaults), and calls
// into that package.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"chatwright.dev/cli/internal/server"
	"github.com/spf13/cobra"
)

// Environment variable names the server subcommands fall back to when the
// corresponding flag is not given, in the usual "flag overrides env
// overrides fixed default" order.
const (
	envAddr        = "CHATWRIGHT_SERVER_ADDR"
	envUpstream    = "CHATWRIGHT_SERVER_UPSTREAM"
	envFixtures    = "CHATWRIGHT_SERVER_DATASTATE_FIXTURES"
	envAllowOrigin = "CHATWRIGHT_SERVER_ALLOW_ORIGIN"
	envUIDir       = "CHATWRIGHT_SERVER_UI_DIR"
	envUI          = "CHATWRIGHT_SERVER_UI"
	envUIURL       = "CHATWRIGHT_SERVER_UI_URL"
	envStateDir    = "CHATWRIGHT_HOME"
)

func runServer(args []string, stdout, stderr io.Writer) int {
	return run(append([]string{"server"}, args...), stdout, stderr)
}

// envOrDefault returns the named environment variable's value when set and
// non-empty, otherwise def — used as a flag's own default value so "flag >
// env > fixed default" falls out of flag.FlagSet's ordinary behavior.
func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// envBoolOrDefault is envOrDefault for a boolean flag (--ui): an unset or
// unparseable environment value falls back to def rather than erroring, so
// a typo'd env var degrades to the flag's ordinary default instead of
// refusing to start the server.
func envBoolOrDefault(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	parsed, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return parsed
}

func defaultStateDir() string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".chatwright")
	}
	return ".chatwright"
}

// repeatedFlag accumulates every occurrence of a repeatable string flag
// (the standard library's flag package has no built-in repeatable string
// flag type).
type repeatedFlag = []string

// --- serve ---

func runServerServe(args []string, stdout, stderr io.Writer) int {
	return run(append([]string{"server", "serve"}, args...), stdout, stderr)
}

func executeServerServe(f serverStartFlags, stdout, stderr io.Writer) int {
	origins := resolveAllowedOrigins(f.allowOrigins)
	logger := log.New(stdout, "", log.LstdFlags)

	// Created before the server so a --ui download can itself be
	// interrupted by Ctrl-C/SIGTERM rather than only the eventual listener.
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	resolvedUIDir, err := resolveServeUIDir(ctx, f.uiDir, f.uiEnabled, f.uiURL, logger)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "chatwright server serve: %v\n", err)
		return 1
	}

	srv, err := server.New(server.Config{
		Version:         cliBuildInfo().Short(),
		UpstreamBaseURL: f.upstream,
		FixturesPath:    f.fixtures,
		Logger:          logger,
		AllowedOrigins:  origins,
		UIDir:           resolvedUIDir,
	})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "chatwright server serve: %v\n", err)
		return 1
	}

	_, _ = fmt.Fprintf(stdout, "chatwright server listening on %s (upstream %s)\n", f.addr, f.upstream)
	if err := srv.ListenAndServe(ctx, f.addr); err != nil {
		_, _ = fmt.Fprintf(stderr, "chatwright server serve: %v\n", err)
		return 1
	}
	_, _ = fmt.Fprintln(stdout, "chatwright server stopped")
	return 0
}

// resolveServeUIDir decides which directory (if any) the UI static handler
// should serve. An explicit --ui-dir always wins over --ui: it is a
// deliberate local override, and resolveServeUIDir never second-guesses it
// with a network call. With --ui-dir empty and --ui set, it delegates to
// server.ResolveOfflineUI's download/cache/verify pipeline. With neither
// set, it returns "" — unchanged behavior: no UI is served.
func resolveServeUIDir(ctx context.Context, uiDir string, uiEnabled bool, uiURL string, logger *log.Logger) (string, error) {
	if uiDir != "" {
		if uiEnabled {
			logger.Printf("ui: --ui-dir %q takes precedence over --ui; serving that local directory as-is", uiDir)
		}
		return uiDir, nil
	}
	if !uiEnabled {
		return "", nil
	}
	return server.ResolveOfflineUI(ctx, server.OfflineUIOptions{
		BaseURL: uiURL,
		Logger:  logger,
	})
}

// resolveAllowedOrigins merges --allow-origin occurrences with a
// comma-separated CHATWRIGHT_SERVER_ALLOW_ORIGIN, ignoring blank entries.
func resolveAllowedOrigins(flagValues repeatedFlag) []string {
	origins := append([]string(nil), flagValues...)
	if v := os.Getenv(envAllowOrigin); v != "" {
		for _, o := range strings.Split(v, ",") {
			if o = strings.TrimSpace(o); o != "" {
				origins = append(origins, o)
			}
		}
	}
	return origins
}

// --- start/restart: shared flags and start logic ---

// serverStartFlags is the flag set start and restart both need (restart
// stops using the same --state-dir before re-running the same start logic
// start itself uses, via startDaemon).
type serverStartFlags struct {
	addr         string
	upstream     string
	fixtures     string
	uiDir        string
	uiEnabled    bool
	uiURL        string
	allowOrigins repeatedFlag
	stateDir     string
}

func (f *serverStartFlags) pidPath() string { return filepath.Join(f.stateDir, "server.pid") }
func (f *serverStartFlags) logPath() string { return filepath.Join(f.stateDir, "server.log") }

func executeServerRestart(f *serverStartFlags, stdout, stderr io.Writer) int {
	if err := server.Stop(f.pidPath(), 0); err != nil && !errors.Is(err, server.ErrNotRunning) {
		_, _ = fmt.Fprintf(stderr, "chatwright server restart: stopping: %v\n", err)
		return 1
	}
	return startDaemon(f, stdout, stderr)
}

// startDaemon implements the actual "start" behavior shared by both `start`
// and `restart`: it re-execs the running binary as a detached `server
// serve` child carrying the same flags, records its PID, and redirects its
// stdout/stderr to a log file — see internal/server.Start for the
// PID-file/session-detach mechanics.
func startDaemon(f *serverStartFlags, stdout, stderr io.Writer) int {
	if err := os.MkdirAll(f.stateDir, 0o755); err != nil {
		_, _ = fmt.Fprintf(stderr, "chatwright server start: creating state dir: %v\n", err)
		return 1
	}

	exe, err := os.Executable()
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "chatwright server start: resolving own executable: %v\n", err)
		return 1
	}

	childArgs := []string{"server", "serve", "--addr", f.addr, "--upstream", f.upstream}
	if f.fixtures != "" {
		childArgs = append(childArgs, "--datastate-fixtures", f.fixtures)
	}
	if f.uiDir != "" {
		childArgs = append(childArgs, "--ui-dir", f.uiDir)
	}
	if f.uiEnabled {
		childArgs = append(childArgs, "--ui")
	}
	if f.uiURL != "" {
		childArgs = append(childArgs, "--ui-url", f.uiURL)
	}
	for _, origin := range f.allowOrigins {
		childArgs = append(childArgs, "--allow-origin", origin)
	}
	// CHATWRIGHT_SERVER_ALLOW_ORIGIN and every other env fallback above
	// need no explicit forwarding: os/exec.Cmd inherits the parent's
	// entire environment by default, and the child parses its own flags
	// with the same envOrDefault fallbacks.

	pid, err := server.Start(server.StartOptions{
		Executable: exe,
		Args:       childArgs,
		PIDFile:    f.pidPath(),
		LogFile:    f.logPath(),
	})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "chatwright server start: %v\n", err)
		return 1
	}
	_, _ = fmt.Fprintf(stdout, "chatwright server started (pid %d) at http://%s (log: %s)\n", pid, f.addr, f.logPath())
	return 0
}

// --- stop ---

func runServerStop(args []string, stdout, stderr io.Writer) int {
	return run(append([]string{"server", "stop"}, args...), stdout, stderr)
}

func executeServerStop(stateDir string, stdout, stderr io.Writer) int {
	err := server.Stop(filepath.Join(stateDir, "server.pid"), 0)
	switch {
	case err == nil:
		_, _ = fmt.Fprintln(stdout, "chatwright server stopped")
		return 0
	case errors.Is(err, server.ErrNotRunning):
		_, _ = fmt.Fprintln(stdout, "chatwright server: not running")
		return 0
	default:
		_, _ = fmt.Fprintf(stderr, "chatwright server stop: %v\n", err)
		return 1
	}
}

func newServerFlags() serverStartFlags {
	return serverStartFlags{
		addr:      envOrDefault(envAddr, server.DefaultAddr),
		upstream:  envOrDefault(envUpstream, server.DefaultUpstreamBaseURL),
		fixtures:  envOrDefault(envFixtures, ""),
		uiDir:     envOrDefault(envUIDir, ""),
		uiEnabled: envBoolOrDefault(envUI, false),
		uiURL:     envOrDefault(envUIURL, ""),
		stateDir:  envOrDefault(envStateDir, defaultStateDir()),
	}
}

func bindServerFlags(cmd *cobra.Command, f *serverStartFlags, includeStateDir bool) {
	flags := cmd.Flags()
	flags.StringVar(&f.addr, "addr", f.addr, "listen address (host:port)")
	flags.StringVar(&f.upstream, "upstream", f.upstream, "OpenAI-compatible upstream base URL")
	flags.StringVar(&f.fixtures, "datastate-fixtures", f.fixtures, "path to a JSON datastate fixtures file")
	flags.StringVar(&f.uiDir, "ui-dir", f.uiDir, "directory of a built Studio UI to serve at /")
	flags.BoolVar(&f.uiEnabled, "ui", f.uiEnabled, "download, cache, verify and serve the Studio UI at /")
	flags.StringVar(&f.uiURL, "ui-url", f.uiURL, "override the Studio UI release base URL used by --ui")
	flags.StringArrayVar(&f.allowOrigins, "allow-origin", nil, "additional CORS origin to allow (repeatable)")
	if includeStateDir {
		flags.StringVar(&f.stateDir, "state-dir", f.stateDir, "directory for server.pid and server.log")
	}
}

func newServerCommand() *cobra.Command {
	serverCmd := &cobra.Command{Use: "server", Short: "Run the server companion daemon", Long: "Run Chatwright's HTTP server in the foreground or manage its detached companion daemon.", Example: `  chatwright server serve --addr 127.0.0.1:8080
  chatwright server start --state-dir ~/.chatwright
  chatwright server stop --state-dir ~/.chatwright`, Args: cobra.NoArgs, RunE: func(_ *cobra.Command, _ []string) error {
		return fmt.Errorf("missing subcommand (serve|start|stop|restart)")
	}}
	serveFlags := newServerFlags()
	serveCmd := &cobra.Command{Use: "serve", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return commandResult(executeServerServe(serveFlags, cmd.OutOrStdout(), cmd.ErrOrStderr()))
	}}
	bindServerFlags(serveCmd, &serveFlags, false)
	startFlags := newServerFlags()
	startCmd := &cobra.Command{Use: "start", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return commandResult(startDaemon(&startFlags, cmd.OutOrStdout(), cmd.ErrOrStderr()))
	}}
	bindServerFlags(startCmd, &startFlags, true)
	restartFlags := newServerFlags()
	restartCmd := &cobra.Command{Use: "restart", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return commandResult(executeServerRestart(&restartFlags, cmd.OutOrStdout(), cmd.ErrOrStderr()))
	}}
	bindServerFlags(restartCmd, &restartFlags, true)
	stopFlags := newServerFlags()
	stopCmd := &cobra.Command{Use: "stop", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return commandResult(executeServerStop(stopFlags.stateDir, cmd.OutOrStdout(), cmd.ErrOrStderr()))
	}}
	stopCmd.Flags().StringVar(&stopFlags.stateDir, "state-dir", stopFlags.stateDir, "directory holding server.pid")
	serverCmd.AddCommand(serveCmd, startCmd, stopCmd, restartCmd)
	addLegacyHelpCommand(serverCmd)
	return serverCmd
}
