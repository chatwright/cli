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
	"flag"
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
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, "chatwright server: missing subcommand (serve|start|stop|restart)")
		printServerUsage(stderr)
		return 2
	}
	switch args[0] {
	case "serve":
		return runServerServe(args[1:], stdout, stderr)
	case "start":
		return runServerStart(args[1:], stdout, stderr)
	case "stop":
		return runServerStop(args[1:], stdout, stderr)
	case "restart":
		return runServerRestart(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		printServerUsage(stdout)
		return 0
	default:
		_, _ = fmt.Fprintf(stderr, "chatwright server: unknown subcommand %q\n\n", args[0])
		printServerUsage(stderr)
		return 2
	}
}

func printServerUsage(w io.Writer) {
	_, _ = fmt.Fprintf(w, `Run the chatwright server companion daemon (see chatwright.dev/cli/internal/server).

Usage:
  chatwright server serve   [flags]   Run in the foreground, logging to stdout
  chatwright server start   [flags]   Start a detached background daemon
  chatwright server stop    [flags]   Stop the background daemon
  chatwright server restart [flags]   Stop (if running), then start

Flags (serve/start/restart):
  --addr string               listen address (default %s, env %s)
  --upstream string           OpenAI-compatible upstream base URL for the
                               chat-completions proxy (default %s, env %s)
  --datastate-fixtures string path to a JSON datastate fixtures file; unset
                               means POST /datastate/query always answers
                               "unsupported" (env %s)
  --ui-dir string              directory of a built Studio UI to serve at /,
                               with an SPA fallback to index.html (env %s);
                               wins over --ui when both are given
  --ui                         download, cache, integrity-verify and serve
                               the Studio UI at / so the tester can run
                               offline after the first fetch (env %s; see
                               chatwright.dev/cli/internal/server
                               ui_offline.go)
  --ui-url string               override the Studio UI release base URL
                               used by --ui (default %s, env %s)
  --allow-origin string        additional CORS origin to allow, beyond the
                               built-in https://chatwright.dev and
                               http://chatwright.localhost (repeatable; env
                               %s is comma-separated)

Flags (start/stop/restart):
  --state-dir string           directory for server.pid and server.log
                               (default ~/.chatwright, env %s)
`, server.DefaultAddr, envAddr, server.DefaultUpstreamBaseURL, envUpstream, envFixtures, envUIDir, envUI, server.DefaultUIBaseURL, envUIURL, envAllowOrigin, envStateDir)
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
type repeatedFlag []string

func (r *repeatedFlag) String() string { return strings.Join(*r, ",") }
func (r *repeatedFlag) Set(v string) error {
	*r = append(*r, v)
	return nil
}

// --- serve ---

func runServerServe(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("chatwright server serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	addr := fs.String("addr", envOrDefault(envAddr, server.DefaultAddr), "listen address (host:port)")
	upstream := fs.String("upstream", envOrDefault(envUpstream, server.DefaultUpstreamBaseURL), "OpenAI-compatible upstream base URL")
	fixtures := fs.String("datastate-fixtures", envOrDefault(envFixtures, ""), "path to a JSON datastate fixtures file")
	uiDir := fs.String("ui-dir", envOrDefault(envUIDir, ""), "directory of a built Studio UI to serve at /")
	uiEnabled := fs.Bool("ui", envBoolOrDefault(envUI, false), "download, cache, verify and serve the Studio UI at /")
	uiURL := fs.String("ui-url", envOrDefault(envUIURL, ""), "override the Studio UI release base URL used by --ui")
	var allowOrigins repeatedFlag
	fs.Var(&allowOrigins, "allow-origin", "additional CORS origin to allow (repeatable)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	origins := resolveAllowedOrigins(allowOrigins)
	logger := log.New(stdout, "", log.LstdFlags)

	// Created before the server so a --ui download can itself be
	// interrupted by Ctrl-C/SIGTERM rather than only the eventual listener.
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	resolvedUIDir, err := resolveServeUIDir(ctx, *uiDir, *uiEnabled, *uiURL, logger)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "chatwright server serve: %v\n", err)
		return 1
	}

	srv, err := server.New(server.Config{
		Version:         cliBuildInfo().Short(),
		UpstreamBaseURL: *upstream,
		FixturesPath:    *fixtures,
		Logger:          logger,
		AllowedOrigins:  origins,
		UIDir:           resolvedUIDir,
	})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "chatwright server serve: %v\n", err)
		return 1
	}

	_, _ = fmt.Fprintf(stdout, "chatwright server listening on %s (upstream %s)\n", *addr, *upstream)
	if err := srv.ListenAndServe(ctx, *addr); err != nil {
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

func parseServerStartFlags(name string, args []string, stderr io.Writer) (*serverStartFlags, int) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	f := &serverStartFlags{}
	addr := fs.String("addr", envOrDefault(envAddr, server.DefaultAddr), "listen address (host:port)")
	upstream := fs.String("upstream", envOrDefault(envUpstream, server.DefaultUpstreamBaseURL), "OpenAI-compatible upstream base URL")
	fixtures := fs.String("datastate-fixtures", envOrDefault(envFixtures, ""), "path to a JSON datastate fixtures file")
	uiDir := fs.String("ui-dir", envOrDefault(envUIDir, ""), "directory of a built Studio UI to serve at /")
	uiEnabled := fs.Bool("ui", envBoolOrDefault(envUI, false), "download, cache, verify and serve the Studio UI at /")
	uiURL := fs.String("ui-url", envOrDefault(envUIURL, ""), "override the Studio UI release base URL used by --ui")
	fs.Var(&f.allowOrigins, "allow-origin", "additional CORS origin to allow (repeatable)")
	stateDir := fs.String("state-dir", envOrDefault(envStateDir, defaultStateDir()), "directory for server.pid and server.log")
	if err := fs.Parse(args); err != nil {
		return nil, 2
	}
	f.addr, f.upstream, f.fixtures, f.uiDir, f.stateDir = *addr, *upstream, *fixtures, *uiDir, *stateDir
	f.uiEnabled, f.uiURL = *uiEnabled, *uiURL
	return f, 0
}

func (f *serverStartFlags) pidPath() string { return filepath.Join(f.stateDir, "server.pid") }
func (f *serverStartFlags) logPath() string { return filepath.Join(f.stateDir, "server.log") }

func runServerStart(args []string, stdout, stderr io.Writer) int {
	f, code := parseServerStartFlags("chatwright server start", args, stderr)
	if f == nil {
		return code
	}
	return startDaemon(f, stdout, stderr)
}

func runServerRestart(args []string, stdout, stderr io.Writer) int {
	f, code := parseServerStartFlags("chatwright server restart", args, stderr)
	if f == nil {
		return code
	}
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
	fs := flag.NewFlagSet("chatwright server stop", flag.ContinueOnError)
	fs.SetOutput(stderr)
	stateDir := fs.String("state-dir", envOrDefault(envStateDir, defaultStateDir()), "directory holding server.pid")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	pidPath := filepath.Join(*stateDir, "server.pid")

	err := server.Stop(pidPath, 0)
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
