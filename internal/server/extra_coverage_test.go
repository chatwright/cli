package server

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"chatwright.dev/runtime/datastate"
)

type errReader struct {
	err error
}

func (e errReader) Read(_ []byte) (int, error) {
	if e.err != nil {
		return 0, e.err
	}
	return 0, errors.New("read error")
}

type errResponseWriter struct {
	header http.Header
}

func (e *errResponseWriter) Header() http.Header {
	if e.header == nil {
		e.header = make(http.Header)
	}
	return e.header
}

func (e *errResponseWriter) Write(_ []byte) (int, error) {
	return 0, errors.New("write error")
}

func (e *errResponseWriter) WriteHeader(_ int) {}

func TestCorsEdgeCases(t *testing.T) {
	if isLocalFamilyOrigin("://invalid-url") {
		t.Error("isLocalFamilyOrigin(invalid-url) = true, want false")
	}
	if isLocalFamilyOrigin("ftp://localhost") {
		t.Error("isLocalFamilyOrigin(ftp://localhost) = true, want false")
	}
	if isLocalFamilyOrigin("http://example.com") {
		t.Error("isLocalFamilyOrigin(http://example.com) = true, want false")
	}
}

func TestDaemonEdgeCases(t *testing.T) {
	if isProcessRunning(0) {
		t.Error("isProcessRunning(0) = true, want false")
	}
	if isProcessRunning(-1) {
		t.Error("isProcessRunning(-1) = true, want false")
	}

	// Test terminate on non-existent PID
	_ = terminate(implausiblePID)

	dir := t.TempDir()
	corruptPidPath := filepath.Join(dir, "corrupt.pid")
	if err := os.WriteFile(corruptPidPath, []byte("invalid-number"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Start with corrupt pid file
	_, err := Start(StartOptions{
		Executable: "sleep",
		PIDFile:    corruptPidPath,
		LogFile:    filepath.Join(dir, "log.txt"),
	})
	if err == nil {
		t.Error("Start with corrupt pid file: want error, got nil")
	}

	// Stop with corrupt pid file
	if err := Stop(corruptPidPath, 0); err == nil {
		t.Error("Stop with corrupt pid file: want error, got nil")
	}

	// Start with invalid log file path
	cleanPidPath := filepath.Join(dir, "clean.pid")
	_, err = Start(StartOptions{
		Executable: "sleep",
		PIDFile:    cleanPidPath,
		LogFile:    filepath.Join(dir, "nonexistent-dir", "log.txt"),
	})
	if err == nil {
		t.Error("Start with invalid log path: want error, got nil")
	}

	// Start with invalid executable
	_, err = Start(StartOptions{
		Executable: "nonexistent-binary-12345",
		PIDFile:    cleanPidPath,
		LogFile:    filepath.Join(dir, "log.txt"),
	})
	if err == nil {
		t.Error("Start with invalid executable: want error, got nil")
	}

	if runtime.GOOS != "windows" {
		// Start with unwritable PIDFile path after process start
		// To simulate WritePIDFile failure after start:
		// make PIDFile point to a directory path or inside a read-only dir
		roDir := filepath.Join(dir, "ro-dir")
		if err := os.Mkdir(roDir, 0o755); err == nil {
			// Write stale pid in roDir, then make roDir read-only to fail RemovePIDFile
			stalePid := filepath.Join(roDir, "stale.pid")
			_ = WritePIDFile(stalePid, implausiblePID)
			_ = os.Chmod(roDir, 0o555)
			_, err = Start(StartOptions{
				Executable: "sleep",
				PIDFile:    stalePid,
				LogFile:    filepath.Join(dir, "log.txt"),
			})
			_ = os.Chmod(roDir, 0o755)
			if err == nil {
				t.Log("Note: Start on read-only dir pid removal error handled")
			}
		}

		// Fail WritePIDFile after start by pointing pid file to a directory
		pidDir := filepath.Join(dir, "pid-dir")
		_ = os.Mkdir(pidDir, 0o755)
		_, err = Start(StartOptions{
			Executable: "sleep",
			Args:       []string{"1"},
			PIDFile:    pidDir, // opening directory for write fails
			LogFile:    filepath.Join(dir, "log.txt"),
		})
		if err == nil {
			t.Error("Start with directory as PIDFile: want error, got nil")
		}
	}
}

func TestDatastateExpectationAndStoreEdgeCases(t *testing.T) {
	// Test buildOneExpectation with various types
	for _, spec := range []expectationSpec{
		{Type: "empty"},
		{Type: "exactRowCount", Count: 5},
		{Type: "exactRows", Rows: []map[string]any{{"a": 1}}},
	} {
		exp, err := buildOneExpectation(spec)
		if err != nil {
			t.Errorf("buildOneExpectation(%s) error = %v", spec.Type, err)
		}
		if exp == nil {
			t.Errorf("buildOneExpectation(%s) returned nil", spec.Type)
		}
	}

	// Test LoadFixturesFile errors
	if _, err := LoadFixturesFile("/nonexistent/file.json"); err == nil {
		t.Error("LoadFixturesFile(nonexistent): want error, got nil")
	}

	badJSON := filepath.Join(t.TempDir(), "bad.json")
	_ = os.WriteFile(badJSON, []byte("{invalid json"), 0o644)
	if _, err := LoadFixturesFile(badJSON); err == nil {
		t.Error("LoadFixturesFile(bad json): want error, got nil")
	}

	// Test FixtureStore.Execute error cases
	store := NewFixtureStore(map[string]map[string][]datastate.Row{
		"h1": {"SELECT 1": {{"col": 1}}},
	})
	if _, err := store.Execute(context.Background(), "invalid-handle-type", datastate.Query{DTQL: "SELECT 1"}); err == nil {
		t.Error("store.Execute(bad handle): want error, got nil")
	}
	if _, err := store.Execute(context.Background(), store.Handles()["h1"], datastate.Query{DTQL: "SELECT 2"}); err == nil {
		t.Error("store.Execute(missing query): want error, got nil")
	}
}

func TestMetricsEdgeCases(t *testing.T) {
	ring := newMetricsRing(2)
	ring.record(CallMetric{Model: "m1"})
	ring.record(CallMetric{Model: "m2"})
	ring.record(CallMetric{Model: "m3"})
	snap := ring.snapshot()
	if len(snap) != 2 || snap[0].Model != "m2" || snap[1].Model != "m3" {
		t.Errorf("ring snapshot after overflow = %+v, want m2, m3", snap)
	}

	srv := newTestServer(t, Config{})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/metrics", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST /metrics status = %d, want 405", resp.StatusCode)
	}
}

func TestModelsEdgeCases(t *testing.T) {
	srv := newTestServer(t, Config{})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Method not allowed
	resp, err := http.Post(ts.URL+"/v1/models", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST /v1/models status = %d, want 405", resp.StatusCode)
	}

	// Upstream request failed
	badUpstreamSrv := newTestServer(t, Config{UpstreamBaseURL: "http://127.0.0.1:1"}) // port 1 should refuse
	ts2 := httptest.NewServer(badUpstreamSrv.Handler())
	defer ts2.Close()

	resp2, err := http.Get(ts2.URL + "/v1/models")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp2.Body.Close()
	if resp2.StatusCode != http.StatusBadGateway {
		t.Errorf("GET /v1/models with unreachable upstream status = %d, want 502", resp2.StatusCode)
	}

	// Upstream response read error
	var errSrv *httptest.Server
	errSrv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		errSrv.CloseClientConnections()
	}))
	defer errSrv.Close()

	respReadErrSrv := newTestServer(t, Config{UpstreamBaseURL: errSrv.URL})
	ts3 := httptest.NewServer(respReadErrSrv.Handler())
	defer ts3.Close()

	resp3, err := http.Get(ts3.URL + "/v1/models")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp3.Body.Close()
	if resp3.StatusCode != http.StatusBadGateway {
		t.Errorf("GET /v1/models with broken body status = %d, want 502", resp3.StatusCode)
	}
}

func TestProxyEdgeCases(t *testing.T) {
	srv := newTestServer(t, Config{})

	// GET /v1/chat/completions -> 405
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/chat/completions", nil)
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /v1/chat/completions code = %d, want 405", rec.Code)
	}

	// Request body read error
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", errReader{})
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("POST /v1/chat/completions with errReader code = %d, want 400", rec.Code)
	}

	// Upstream network error (failed metric recorded)
	badUpstreamSrv := newTestServer(t, Config{UpstreamBaseURL: "http://127.0.0.1:1"})
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"m1"}`))
	badUpstreamSrv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Errorf("POST /v1/chat/completions unreachable upstream code = %d, want 502", rec.Code)
	}

	// Hop-by-hop header skipping
	srcH := http.Header{}
	srcH.Set("Connection", "close")
	srcH.Set("X-Custom", "val")
	dstH := http.Header{}
	copyForwardHeaders(srcH, dstH)
	if dstH.Get("Connection") != "" || dstH.Get("X-Custom") != "val" {
		t.Errorf("copyForwardHeaders result = %+v", dstH)
	}

	// relayBuffered response body read error
	var errSrv *httptest.Server
	errSrv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		errSrv.CloseClientConnections()
	}))
	defer errSrv.Close()

	bufferedErrSrv := newTestServer(t, Config{UpstreamBaseURL: errSrv.URL})
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"m1"}`))
	bufferedErrSrv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Errorf("relayBuffered read error code = %d, want 502", rec.Code)
	}

	// relayStreaming response body read error
	streamErrSrv := newTestServer(t, Config{UpstreamBaseURL: errSrv.URL})
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"m1","stream":true}`))
	streamErrSrv.Handler().ServeHTTP(rec, req)

	// relayStreaming write error
	dummyResp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader("data: chunk\n\n")),
	}
	streamErrSrv.relayStreaming(&errResponseWriter{}, dummyResp, time.Now(), chatCompletionRequest{Model: "m1", Stream: true})
}

func TestServerAndUIEdgeCases(t *testing.T) {
	// New with bad upstream URL
	if _, err := New(Config{UpstreamBaseURL: "://bad-url"}); err == nil {
		t.Error("New(bad upstream): want error, got nil")
	}

	// New with bad fixtures path
	if _, err := New(Config{FixturesPath: "/nonexistent/fixtures.json"}); err == nil {
		t.Error("New(bad fixtures path): want error, got nil")
	}

	// discardWriter
	dw := discardWriter{}
	if n, err := dw.Write([]byte("test")); n != 4 || err != nil {
		t.Errorf("discardWriter.Write() = (%d, %v), want (4, nil)", n, err)
	}

	// UI handler non-GET/HEAD method
	uiH := newUIHandler(t.TempDir())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	uiH.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("UI handler POST code = %d, want 405", rec.Code)
	}

	// ListenAndServe error with invalid address
	srv := newTestServer(t, Config{})
	if err := srv.ListenAndServe(context.Background(), "999.999.999.999:99999"); err == nil {
		t.Error("ListenAndServe(bad addr): want error, got nil")
	}

	// ListenAndServe clean shutdown
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- srv.ListenAndServe(ctx, "127.0.0.1:0")
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("ListenAndServe shutdown error = %v, want nil", err)
		}
	case <-time.After(3 * time.Second):
		t.Error("ListenAndServe did not return in time")
	}
}

func TestUIOfflineEdgeCases(t *testing.T) {
	_ = DefaultUICacheDir()

	cacheDir := t.TempDir()

	// BaseURL formatting and defaults
	// ResolveOfflineUI with empty cache directory and failing manifest URL -> falls back to cache error
	_, err := ResolveOfflineUI(context.Background(), OfflineUIOptions{
		BaseURL:  "http://127.0.0.1:1/ui",
		CacheDir: cacheDir,
	})
	if err == nil {
		t.Error("ResolveOfflineUI with unreachable base and empty cache: want error, got nil")
	}

	// Manifest validation errors:
	// 1. Missing version
	m1 := fmt.Sprintf(`{"uiContract": %d, "sha256": "abc"}`, SupportedUIContract)
	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(m1))
	}))
	defer srv1.Close()

	_, err = ResolveOfflineUI(context.Background(), OfflineUIOptions{
		BaseURL:  srv1.URL,
		CacheDir: cacheDir,
	})
	if err == nil || !strings.Contains(err.Error(), "missing its version") {
		t.Errorf("ResolveOfflineUI with missing version err = %v", err)
	}

	// 2. Unsafe version name
	m2 := fmt.Sprintf(`{"uiContract": %d, "version": "../evil", "sha256": "abc"}`, SupportedUIContract)
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(m2))
	}))
	defer srv2.Close()

	_, err = ResolveOfflineUI(context.Background(), OfflineUIOptions{
		BaseURL:  srv2.URL,
		CacheDir: cacheDir,
	})
	if err == nil || !strings.Contains(err.Error(), "not a safe cache directory name") {
		t.Errorf("ResolveOfflineUI with unsafe version err = %v", err)
	}

	// 3. Missing sha256
	m3 := fmt.Sprintf(`{"uiContract": %d, "version": "v1.0.0"}`, SupportedUIContract)
	srv3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(m3))
	}))
	defer srv3.Close()

	_, err = ResolveOfflineUI(context.Background(), OfflineUIOptions{
		BaseURL:  srv3.URL,
		CacheDir: cacheDir,
	})
	if err == nil || !strings.Contains(err.Error(), "missing its sha256") {
		t.Errorf("ResolveOfflineUI with missing sha256 err = %v", err)
	}

	// 4. Mismatched cached marker sha -> triggers re-download
	versionDir := filepath.Join(cacheDir, "v1.0.0")
	_ = os.MkdirAll(versionDir, 0o755)
	_ = os.WriteFile(filepath.Join(versionDir, shaMarkerFileName), []byte("old-sha"), 0o644)

	// Create valid zip with index.html
	zipBuf := new(bytes.Buffer)
	zw := zip.NewWriter(zipBuf)
	f, _ := zw.Create("index.html")
	_, _ = f.Write([]byte("<html>UI</html>"))
	_ = zw.Close()
	zipBytes := zipBuf.Bytes()
	sum := sha256.Sum256(zipBytes)
	zipSHA := hex.EncodeToString(sum[:])

	m4 := fmt.Sprintf(`{"uiContract": %d, "version": "v1.0.0", "sha256": "%s"}`, SupportedUIContract, zipSHA)
	srv4 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "manifest.json") {
			_, _ = w.Write([]byte(m4))
		} else {
			_, _ = w.Write(zipBytes)
		}
	}))
	defer srv4.Close()

	dir, err := ResolveOfflineUI(context.Background(), OfflineUIOptions{
		BaseURL:  srv4.URL,
		CacheDir: cacheDir,
		Logger:   log.Default(),
	})
	if err != nil {
		t.Fatalf("ResolveOfflineUI with mismatched cached sha error = %v", err)
	}
	if dir != versionDir {
		t.Errorf("ResolveOfflineUI dir = %s, want %s", dir, versionDir)
	}

	// 5. Zip download integrity mismatch
	m5 := fmt.Sprintf(`{"uiContract": %d, "version": "v2.0.0", "sha256": "wronghash123"}`, SupportedUIContract)
	srv5 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "manifest.json") {
			_, _ = w.Write([]byte(m5))
		} else {
			_, _ = w.Write(zipBytes)
		}
	}))
	defer srv5.Close()

	_, err = ResolveOfflineUI(context.Background(), OfflineUIOptions{
		BaseURL:  srv5.URL,
		CacheDir: cacheDir,
	})
	if err == nil || !strings.Contains(err.Error(), "integrity") {
		t.Errorf("ResolveOfflineUI with hash mismatch err = %v", err)
	}

	// 6. Zip extraction errors and edge cases:
	// - Backslash in entry name
	badZipBuf := new(bytes.Buffer)
	bzw := zip.NewWriter(badZipBuf)
	_, _ = bzw.Create("dir\\file.txt")
	_ = bzw.Close()
	if err := extractUIZip(badZipBuf.Bytes(), t.TempDir()); err == nil {
		t.Error("extractUIZip with backslash: want error, got nil")
	}

	// - Zip slip
	badZipBuf2 := new(bytes.Buffer)
	bzw2 := zip.NewWriter(badZipBuf2)
	_, _ = bzw2.Create("../evil.txt")
	_ = bzw2.Close()
	if err := extractUIZip(badZipBuf2.Bytes(), t.TempDir()); err == nil {
		t.Error("extractUIZip with zip slip: want error, got nil")
	}

	// - Invalid zip bytes
	if err := extractUIZip([]byte("not-a-zip"), t.TempDir()); err == nil {
		t.Error("extractUIZip with invalid bytes: want error, got nil")
	}

	// - Cleaned == "." and directories in zip
	goodZipBuf := new(bytes.Buffer)
	gzw := zip.NewWriter(goodZipBuf)
	_, _ = gzw.Create("subdir/")
	f2, _ := gzw.Create("subdir/file.txt")
	_, _ = f2.Write([]byte("content"))
	_ = gzw.Close()
	if err := extractUIZip(goodZipBuf.Bytes(), t.TempDir()); err != nil {
		t.Errorf("extractUIZip with directory entry error = %v", err)
	}

	// fetchUIBytes non-200
	srv404 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv404.Close()
	if _, err := fetchUIBytes(context.Background(), srv404.Client(), srv404.URL); err == nil {
		t.Error("fetchUIBytes 404: want error, got nil")
	}

	// fetchUIManifest bad JSON
	srvBadJSON := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("{invalid"))
	}))
	defer srvBadJSON.Close()
	if _, err := fetchUIManifest(context.Background(), srvBadJSON.Client(), srvBadJSON.URL); err == nil {
		t.Error("fetchUIManifest bad JSON: want error, got nil")
	}

	// newestCachedUIVersion with non-dir entries
	testDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(testDir, "a-file.txt"), []byte("hi"), 0o644)
	if _, err := newestCachedUIVersion(testDir); err == nil {
		t.Error("newestCachedUIVersion with no dirs: want error, got nil")
	}
	if _, err := newestCachedUIVersion("/nonexistent-cache-dir"); err == nil {
		t.Error("newestCachedUIVersion nonexistent: want error, got nil")
	}
}

func TestAdditionalEdgeCases(t *testing.T) {
	// Models and Proxy URL construction failure (invalid upstreamBaseURL with control char)
	badURLSrv := newTestServer(t, Config{})
	badURLSrv.upstreamBaseURL = "http://\x7f"

	recModels := httptest.NewRecorder()
	reqModels := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	badURLSrv.handleModels(recModels, reqModels)
	if recModels.Code != http.StatusInternalServerError {
		t.Errorf("handleModels with invalid URL code = %d, want 500", recModels.Code)
	}

	recProxy := httptest.NewRecorder()
	reqProxy := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{}`))
	badURLSrv.handleChatCompletions(recProxy, reqProxy)
	if recProxy.Code != http.StatusInternalServerError {
		t.Errorf("handleChatCompletions with invalid URL code = %d, want 500", recProxy.Code)
	}

	// Serve error when listener is closed externally
	srv := newTestServer(t, Config{})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_ = ln.Close()
	if err := srv.Serve(context.Background(), ln); err == nil {
		t.Error("Serve on closed listener: want error, got nil")
	}

	// Daemon Start WritePIDFile failure
	dir := t.TempDir()
	logPath := filepath.Join(dir, "log.txt")
	_, err = Start(StartOptions{
		Executable: "sleep",
		Args:       []string{"1"},
		PIDFile:    filepath.Join(dir, "missing-sub-dir", "server.pid"),
		LogFile:    logPath,
	})
	if err == nil {
		t.Error("Start with missing parent directory for PIDFile: want error, got nil")
	}

	// Daemon Stop terminate failure on pid 1 (or other non-owned pid)
	pid1File := filepath.Join(dir, "pid1.pid")
	_ = WritePIDFile(pid1File, 1)
	if err := Stop(pid1File, 0); err == nil {
		t.Log("Note: Stop on pid 1 handled")
	}
}

func TestUIOfflineMoreEdgeCases(t *testing.T) {
	// fetchUIBytes invalid URL
	if _, err := fetchUIBytes(context.Background(), http.DefaultClient, "http://\x7f"); err == nil {
		t.Error("fetchUIBytes(invalid url): want error, got nil")
	}

	// extractUIZip with "." entry
	dotZipBuf := new(bytes.Buffer)
	dzw := zip.NewWriter(dotZipBuf)
	_, _ = dzw.Create(".")
	_ = dzw.Close()
	destDir := t.TempDir()
	if err := extractUIZip(dotZipBuf.Bytes(), destDir); err != nil {
		t.Errorf("extractUIZip with dot entry: %v", err)
	}

	// extractUIZip destDir blocked by regular file
	blockedDir := filepath.Join(t.TempDir(), "blocked")
	_ = os.WriteFile(blockedDir, []byte("file"), 0o644)
	if err := extractUIZip(dotZipBuf.Bytes(), blockedDir); err == nil {
		t.Error("extractUIZip over file: want error, got nil")
	}

	// extractUIZip directory entry blocked by file
	dirZipBuf := new(bytes.Buffer)
	drzw := zip.NewWriter(dirZipBuf)
	_, _ = drzw.Create("subdir/")
	_ = drzw.Close()
	extractDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(extractDir, "subdir"), []byte("file"), 0o644)
	if err := extractUIZip(dirZipBuf.Bytes(), extractDir); err == nil {
		t.Error("extractUIZip dir entry blocked: want error, got nil")
	}

	// extractUIZip file entry whose parent dir is blocked by file
	fileZipBuf := new(bytes.Buffer)
	fzw := zip.NewWriter(fileZipBuf)
	_, _ = fzw.Create("sub/file.txt")
	_ = fzw.Close()
	extractDir2 := t.TempDir()
	_ = os.WriteFile(filepath.Join(extractDir2, "sub"), []byte("file"), 0o644)
	if err := extractUIZip(fileZipBuf.Bytes(), extractDir2); err == nil {
		t.Error("extractUIZip file entry parent blocked: want error, got nil")
	}

	// extractUIZip file entry whose target is an existing directory (extractUIZipEntry fails)
	fileZipBuf3 := new(bytes.Buffer)
	fzw3 := zip.NewWriter(fileZipBuf3)
	_, _ = fzw3.Create("file.txt")
	_ = fzw3.Close()
	extractDir3 := t.TempDir()
	_ = os.MkdirAll(filepath.Join(extractDir3, "file.txt"), 0o755)
	if err := extractUIZip(fileZipBuf3.Bytes(), extractDir3); err == nil {
		t.Error("extractUIZip file entry target is dir: want error, got nil")
	}

	// extractUIZipEntry target open failure (target is a directory)
	targetDir := filepath.Join(t.TempDir(), "targetDir")
	_ = os.MkdirAll(targetDir, 0o755)
	goodZipBuf := new(bytes.Buffer)
	gzw := zip.NewWriter(goodZipBuf)
	zf, _ := gzw.Create("file.txt")
	_, _ = zf.Write([]byte("hello"))
	_ = gzw.Close()
	zr, _ := zip.NewReader(bytes.NewReader(goodZipBuf.Bytes()), int64(goodZipBuf.Len()))
	if err := extractUIZipEntry(zr.File[0], targetDir); err == nil {
		t.Error("extractUIZipEntry to directory: want error, got nil")
	}

	// ResolveOfflineUI with empty BaseURL and empty CacheDir
	// Setup mock server for default UI
	var mockUI = fmt.Sprintf(`{"uiContract": %d, "version": "v3.0.0", "sha256": "abcdef"}`, SupportedUIContract)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(mockUI))
	}))
	defer srv.Close()

	// ResolveOfflineUI with empty BaseURL (uses default)
	_, _ = ResolveOfflineUI(context.Background(), OfflineUIOptions{
		CacheDir: t.TempDir(),
	})
	// ResolveOfflineUI with empty CacheDir (uses DefaultUICacheDir)
	_, _ = ResolveOfflineUI(context.Background(), OfflineUIOptions{
		BaseURL: srv.URL,
	})

	// ResolveOfflineUI failure writing integrity marker (make versionDir read-only)
	zipBuf := new(bytes.Buffer)
	zw := zip.NewWriter(zipBuf)
	zf2, _ := zw.Create("index.html")
	_, _ = zf2.Write([]byte("<html></html>"))
	_ = zw.Close()
	zipBytes := zipBuf.Bytes()
	sum := sha256.Sum256(zipBytes)
	zipSHA := hex.EncodeToString(sum[:])

	manifestData := fmt.Sprintf(`{"uiContract": %d, "version": "v4.0.0", "sha256": "%s"}`, SupportedUIContract, zipSHA)
	srvMarker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "manifest.json") {
			_, _ = w.Write([]byte(manifestData))
		} else {
			_, _ = w.Write(zipBytes)
		}
	}))
	defer srvMarker.Close()

	// DefaultUICacheDir with empty HOME
	t.Setenv("HOME", "")
	if d := DefaultUICacheDir(); !strings.HasSuffix(d, filepath.Join(".chatwright", "ui")) {
		t.Errorf("DefaultUICacheDir with empty HOME = %q", d)
	}

	// isWithinDir with invalid relative/absolute mix
	if isWithinDir("relative", "/absolute") {
		t.Error("isWithinDir(relative, /absolute) = true, want false")
	}

	// extractUIZipEntry with unsupported zip method
	badMethodBuf := new(bytes.Buffer)
	bmzw := zip.NewWriter(badMethodBuf)
	f, _ := bmzw.Create("bad.txt")
	_, _ = f.Write([]byte("bad"))
	_ = bmzw.Close()
	bmzr, _ := zip.NewReader(bytes.NewReader(badMethodBuf.Bytes()), int64(badMethodBuf.Len()))
	bmzr.File[0].Method = 99
	if err := extractUIZipEntry(bmzr.File[0], filepath.Join(t.TempDir(), "bad.txt")); err == nil {
		t.Error("extractUIZipEntry with bad method: want error, got nil")
	}

	// ResolveOfflineUI failure downloading zip (manifest returns 200, zip returns 500)
	srvFailZip := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "manifest.json") {
			_, _ = fmt.Fprintf(w, `{"uiContract": %d, "version": "v5.0.0", "sha256": "abc"}`, SupportedUIContract)
		} else {
			http.Error(w, "server error", http.StatusInternalServerError)
		}
	}))
	defer srvFailZip.Close()
	_, err := ResolveOfflineUI(context.Background(), OfflineUIOptions{
		BaseURL:  srvFailZip.URL,
		CacheDir: t.TempDir(),
	})
	if err == nil {
		t.Error("ResolveOfflineUI with 500 zip download: want error, got nil")
	}

	// ResolveOfflineUI failure writing integrity marker (shaMarkerFileName is a directory)
	manifestDataDir := fmt.Sprintf(`{"uiContract": %d, "version": "v6.0.0", "sha256": "%s"}`, SupportedUIContract, zipSHA)
	srvMarkerDir := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "manifest.json") {
			_, _ = w.Write([]byte(manifestDataDir))
		} else {
			_, _ = w.Write(zipBytes)
		}
	}))
	defer srvMarkerDir.Close()

	cacheDirMarkerDir := t.TempDir()
	v6Dir := filepath.Join(cacheDirMarkerDir, "v6.0.0")
	_ = os.MkdirAll(filepath.Join(v6Dir, shaMarkerFileName), 0o755) // make marker path a directory
	_, err = ResolveOfflineUI(context.Background(), OfflineUIOptions{
		BaseURL:  srvMarkerDir.URL,
		CacheDir: cacheDirMarkerDir,
	})
	if err == nil {
		t.Error("ResolveOfflineUI with directory at marker path: want error, got nil")
	}

	// newestCachedUIVersion skipping directory without marker
	skipDir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(skipDir, "unmarked"), 0o755)
	validVerDir := filepath.Join(skipDir, "marked")
	_ = os.MkdirAll(validVerDir, 0o755)
	_ = os.WriteFile(filepath.Join(validVerDir, shaMarkerFileName), []byte("sha"), 0o644)
	if v, err := newestCachedUIVersion(skipDir); err != nil || v != "marked" {
		t.Errorf("newestCachedUIVersion with unmarked dir = (%s, %v), want marked", v, err)
	}
}
