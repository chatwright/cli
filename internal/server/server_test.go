package server

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestServer(t *testing.T, cfg Config) *Server {
	t.Helper()
	if cfg.Version == "" {
		cfg.Version = "test-version"
	}
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return srv
}

func TestHealthShape(t *testing.T) {
	srv := newTestServer(t, Config{Version: "1.2.3"})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q", ct)
	}

	var body healthResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Name != "chatwright-server" {
		t.Fatalf("Name = %q, want chatwright-server", body.Name)
	}
	if body.Version != "1.2.3" {
		t.Fatalf("Version = %q, want 1.2.3", body.Version)
	}
	wantCaps := []string{"ai-proxy", "datastate"}
	if len(body.Capabilities) != len(wantCaps) {
		t.Fatalf("Capabilities = %v, want %v", body.Capabilities, wantCaps)
	}
	for i, c := range wantCaps {
		if body.Capabilities[i] != c {
			t.Fatalf("Capabilities[%d] = %q, want %q", i, body.Capabilities[i], c)
		}
	}
}

func TestHealthRejectsNonGET(t *testing.T) {
	srv := newTestServer(t, Config{})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/health", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", resp.StatusCode)
	}
}

// --- CORS / Private-Network-Access ---

func TestCORSHeadersOnAllowedOrigins(t *testing.T) {
	srv := newTestServer(t, Config{})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	cases := []string{
		"https://chatwright.dev",
		"http://chatwright.localhost",
		"http://chatwright.localhost:5173",
		"http://localhost:3000",
		"http://127.0.0.1:9999",
	}
	for _, origin := range cases {
		req, err := http.NewRequest(http.MethodGet, ts.URL+"/health", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Origin", origin)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		got := resp.Header.Get("Access-Control-Allow-Origin")
		_ = resp.Body.Close()
		if got != origin {
			t.Errorf("origin %q: Access-Control-Allow-Origin = %q, want %q", origin, got, origin)
		}
	}
}

func TestCORSExtraAllowedOrigin(t *testing.T) {
	srv := newTestServer(t, Config{AllowedOrigins: []string{"https://example.test"}})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/health", nil)
	req.Header.Set("Origin", "https://example.test")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "https://example.test" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want https://example.test", got)
	}
}

func TestCORSRejectsUnknownOrigin(t *testing.T) {
	srv := newTestServer(t, Config{})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/health", nil)
	req.Header.Set("Origin", "https://evil.example")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want empty for a disallowed origin", got)
	}
}

func TestOPTIONSPreflight(t *testing.T) {
	srv := newTestServer(t, Config{})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	req, err := http.NewRequest(http.MethodOptions, ts.URL+"/v1/chat/completions", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "https://chatwright.dev")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "Content-Type, X-Custom")
	req.Header.Set("Access-Control-Request-Private-Network", "true")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "https://chatwright.dev" {
		t.Fatalf("Access-Control-Allow-Origin = %q", got)
	}
	if got := resp.Header.Get("Access-Control-Allow-Private-Network"); got != "true" {
		t.Fatalf("Access-Control-Allow-Private-Network = %q, want true", got)
	}
	if got := resp.Header.Get("Access-Control-Allow-Methods"); got == "" {
		t.Fatal("Access-Control-Allow-Methods is empty")
	}
	if got := resp.Header.Get("Access-Control-Allow-Headers"); got != "Content-Type, X-Custom" {
		t.Fatalf("Access-Control-Allow-Headers = %q, want echoed request headers", got)
	}

	body, _ := io.ReadAll(resp.Body)
	if len(body) != 0 {
		t.Fatalf("preflight body = %q, want empty", body)
	}
}

// --- UI static serving (--ui-dir) ---

func TestUIDirServesStaticFilesAndSPAFallback(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>index</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.js"), []byte("console.log('hi')"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := newTestServer(t, Config{UIDir: dir})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// A real static file is served as itself.
	resp, err := http.Get(ts.URL + "/app.js")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "console.log('hi')" {
		t.Fatalf("GET /app.js = %d %q", resp.StatusCode, body)
	}

	// An unknown client-side route falls back to index.html.
	resp, err = http.Get(ts.URL + "/runs/42")
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "<html>index</html>" {
		t.Fatalf("GET /runs/42 = %d %q, want SPA fallback to index.html", resp.StatusCode, body)
	}
}

func TestUIDirNeverShadowsAPIRoutes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>index</html>"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := newTestServer(t, Config{UIDir: dir, Version: "9.9.9"})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var body healthResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Name != "chatwright-server" {
		t.Fatalf("GET /health with --ui-dir set returned %+v, want the health handler, not the UI fallback", body)
	}
}

func TestNoUIDirMeansOrdinary404(t *testing.T) {
	srv := newTestServer(t, Config{})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/nope")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 when no --ui-dir is configured", resp.StatusCode)
	}
}

// --- graceful shutdown ---

func TestServeGracefulShutdownOnContextCancel(t *testing.T) {
	srv := newTestServer(t, Config{})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx, ln) }()

	// Give the listener a moment to start accepting, then confirm it
	// actually answers before asking it to shut down.
	addr := ln.Addr().String()
	waitForHealth(t, addr)

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve() returned %v after context cancel, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve() did not return within 5s of context cancellation")
	}
}

func waitForHealth(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://" + addr + "/health")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("server at %s never became healthy", addr)
}
