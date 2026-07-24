// Package server implements the chatwright server companion daemon: a
// host-side HTTP proxy that lets the browser Studio (served from
// https://chatwright.dev) reach local AI model servers and verify database
// assertions — things a sandboxed HTTPS page cannot do directly (mixed
// content blocks a page loaded over HTTPS from calling http://localhost,
// and a local model server rarely sends CORS headers of its own).
//
// This package holds every byte of the server's behavior — request
// routing, the reverse proxy, the metrics ring buffer, the datastate
// evaluation seam and the daemon lifecycle primitives. cmd/chatwright's
// server.go stays a thin flag-parsing front end, per this repository's own
// AGENTS.md ("the CLI is deliberately thin ... engine or wire logic never
// lives here").
package server

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultAddr is the fixed, discoverable default listen address for
// `chatwright server serve`/`start`. It is deliberately not an ephemeral
// port: the Studio web app needs a stable address to probe via GET /health
// without any prior handshake. 4319 was chosen to avoid the obvious
// collisions — common dev-server ports (3000, 5173, 8080, ...) and Ollama's
// own default of 11434 (the server's own default upstream, so the two must
// never share a port) — while still being easy to recognize and grep for in
// logs. Override with --addr or CHATWRIGHT_SERVER_ADDR.
const DefaultAddr = "127.0.0.1:4319"

// DefaultUpstreamBaseURL is the OpenAI-compatible backend the chat-completions
// proxy forwards to when the operator does not configure one: a local Ollama
// server's own OpenAI-compatible surface. Override with --upstream or
// CHATWRIGHT_SERVER_UPSTREAM.
const DefaultUpstreamBaseURL = "http://localhost:11434/v1"

// Capabilities is the fixed capability roster GET /health reports for
// Studio feature-detection. Adding a capability here is a deliberate,
// reviewed contract change — it is never inferred from which handlers
// happen to be registered.
var Capabilities = []string{"ai-proxy", "datastate"}

// Config configures a Server. Only Version is required; every other field
// has a documented zero-value behavior.
type Config struct {
	// Version is the running CLI's own version, reported verbatim by
	// GET /health.
	Version string
	// UpstreamBaseURL is the OpenAI-compatible backend the chat-completions
	// proxy forwards to. Empty uses DefaultUpstreamBaseURL.
	UpstreamBaseURL string
	// FixturesPath, when non-empty, is a JSON file loaded into a
	// FixtureStore backing POST /datastate/query. Empty means no store is
	// configured: every /datastate/query call returns an explicit
	// "unsupported" verdict — see FixtureStore's doc comment for exactly
	// what this does and does not wire.
	FixturesPath string
	// Logger receives one line per proxied chat-completion call plus
	// server lifecycle notices. A nil Logger discards all output via
	// log.New(io.Discard, ...).
	Logger *log.Logger
	// HTTPClient is the client used to call UpstreamBaseURL. A nil value
	// builds a client with proxyTimeout. Tests inject their own client to
	// point at an httptest.Server without touching a real network.
	HTTPClient *http.Client
	// MetricsCapacity bounds the in-memory metrics ring buffer. <= 0 uses
	// defaultMetricsCapacity.
	MetricsCapacity int
	// AllowedOrigins are extra CORS origins to allow, beyond the built-in
	// defaults (https://chatwright.dev and http://chatwright.localhost —
	// see cors.go's defaultAllowedOrigins) and the generic local-dev-server
	// pattern match (any port on localhost/*.localhost/a loopback IP).
	// Blank entries are ignored.
	AllowedOrigins []string
	// UIDir, when non-empty, serves that directory's static files at "/"
	// with an SPA fallback to index.html for any unmatched non-API path —
	// see ui.go. Empty means no UI is served; unmatched paths get the
	// ServeMux's ordinary 404.
	UIDir string
}

// Server is the chatwright server companion daemon's HTTP surface. Build
// one with New and either call Handler to embed it (tests do this via
// httptest.NewServer) or ListenAndServe/Serve to run it standalone.
type Server struct {
	version string
	logger  *log.Logger

	upstreamBaseURL string
	httpClient      *http.Client

	metrics *metricsRing

	store  *FixtureStore // nil => POST /datastate/query always answers "unsupported"
	runner datastateRunner

	uiDir string
	mux   http.Handler
}

// New builds a Server from cfg. It never opens a network listener itself —
// see Serve/ListenAndServe for that.
func New(cfg Config) (*Server, error) {
	logger := cfg.Logger
	if logger == nil {
		logger = log.New(discardWriter{}, "", 0)
	}

	upstream := cfg.UpstreamBaseURL
	if upstream == "" {
		upstream = DefaultUpstreamBaseURL
	}
	upstream = strings.TrimRight(upstream, "/")
	if _, err := url.Parse(upstream); err != nil {
		return nil, errors.New("server: invalid upstream base URL: " + err.Error())
	}

	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: proxyTimeout}
	}

	capacity := cfg.MetricsCapacity
	if capacity <= 0 {
		capacity = defaultMetricsCapacity
	}

	var store *FixtureStore
	if cfg.FixturesPath != "" {
		loaded, err := LoadFixturesFile(cfg.FixturesPath)
		if err != nil {
			return nil, err
		}
		store = loaded
	}

	s := &Server{
		version:         cfg.Version,
		logger:          logger,
		upstreamBaseURL: upstream,
		httpClient:      client,
		metrics:         newMetricsRing(capacity),
		store:           store,
		uiDir:           cfg.UIDir,
	}
	if store != nil {
		runner, err := newFixtureRunner(store)
		if err != nil {
			return nil, err
		}
		s.runner = runner
	}
	allowlist := newOriginAllowlist(cfg.AllowedOrigins)
	s.mux = withCORS(allowlist, s.buildMux())
	return s, nil
}

// Handler returns the Server's full HTTP handler (routing + CORS/PNA
// headers on every response), suitable for embedding in an httptest.Server
// or any other http.Server.
func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) buildMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/metrics", s.handleMetrics)
	mux.HandleFunc("/v1/chat/completions", s.handleChatCompletions)
	mux.HandleFunc("/v1/models", s.handleModels)
	mux.HandleFunc("/datastate/query", s.handleDatastateQuery)
	if s.uiDir != "" {
		// Registered last, on the "/" subtree pattern: ServeMux always
		// prefers the exact-match API patterns above for their own literal
		// paths, so this can never shadow an API route — see ui.go.
		mux.Handle("/", newUIHandler(s.uiDir))
	}
	return mux
}

// Serve runs the Server on the given listener until ctx is canceled, then
// gracefully shuts it down (http.Server.Shutdown) and returns. A nil error
// means either a clean shutdown or the listener closing on its own accord
// (http.ErrServerClosed); any other error from either the listener or an
// unclean shutdown is returned.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	httpSrv := &http.Server{Handler: s.mux}

	serveErr := make(chan error, 1)
	go func() { serveErr <- httpSrv.Serve(ln) }()

	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		shutdownErr := httpSrv.Shutdown(shutdownCtx)
		<-serveErr // wait for Serve to actually return before this method does
		return shutdownErr
	}
}

// ListenAndServe binds addr and calls Serve. It is the convenience entry
// point `chatwright server serve` uses; Serve itself (taking a caller-owned
// net.Listener) is what tests use to run against an ephemeral port.
func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return s.Serve(ctx, ln)
}

// shutdownTimeout bounds how long Serve waits for in-flight requests to
// finish during a graceful shutdown before giving up.
const shutdownTimeout = 10 * time.Second

// --- GET /health ---

type healthResponse struct {
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	Capabilities []string `json:"capabilities"`
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	writeJSON(w, http.StatusOK, healthResponse{
		Name:         "chatwright-server",
		Version:      s.version,
		Capabilities: Capabilities,
	})
}

// --- shared helpers ---

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func methodNotAllowed(w http.ResponseWriter, allow ...string) {
	w.Header().Set("Allow", strings.Join(allow, ", "))
	writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }
