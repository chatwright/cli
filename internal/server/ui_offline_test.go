package server

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// buildTestUIZip returns the bytes of a minimal zip mirroring the
// studio-ui.zip packaging contract: index.html at the root, plus whatever
// extra files the caller wants alongside it.
func buildTestUIZip(t *testing.T, extra map[string]string) []byte {
	t.Helper()
	files := map[string]string{"index.html": "<html>studio ui</html>"}
	for k, v := range extra {
		files[k] = v
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zw.Create(%q): %v", name, err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("writing %q: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zw.Close(): %v", err)
	}
	return buf.Bytes()
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// newTestUIReleaseServer serves manifest+zip at the paths ResolveOfflineUI
// expects under one base URL, mirroring the Studio release layout. The
// returned zipRequests counter increments on every GET of studio-ui.zip, so
// tests can assert a warm cache skips the download entirely.
func newTestUIReleaseServer(t *testing.T, version string, zipBytes []byte, uiContract int) (*httptest.Server, *int32) {
	t.Helper()
	manifest := uiManifest{Version: version, SHA256: sha256Hex(zipBytes), UIContract: uiContract}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshaling manifest: %v", err)
	}

	var zipRequests int32
	mux := http.NewServeMux()
	mux.HandleFunc("/studio-ui.manifest.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(manifestJSON)
	})
	mux.HandleFunc("/studio-ui.zip", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&zipRequests, 1)
		_, _ = w.Write(zipBytes)
	})
	return httptest.NewServer(mux), &zipRequests
}

func TestResolveOfflineUIHappyPath(t *testing.T) {
	zipBytes := buildTestUIZip(t, map[string]string{"assets/app.js": "console.log('hi')"})
	ts, _ := newTestUIReleaseServer(t, "1.2.3", zipBytes, SupportedUIContract)
	defer ts.Close()

	cacheDir := t.TempDir()
	dir, err := ResolveOfflineUI(context.Background(), OfflineUIOptions{BaseURL: ts.URL + "/", CacheDir: cacheDir})
	if err != nil {
		t.Fatalf("ResolveOfflineUI() error = %v", err)
	}
	if want := filepath.Join(cacheDir, "1.2.3"); dir != want {
		t.Fatalf("dir = %q, want %q", dir, want)
	}

	indexBytes, err := os.ReadFile(filepath.Join(dir, "index.html"))
	if err != nil {
		t.Fatalf("reading extracted index.html: %v", err)
	}
	if string(indexBytes) != "<html>studio ui</html>" {
		t.Fatalf("index.html content = %q", indexBytes)
	}
	if _, err := os.Stat(filepath.Join(dir, "assets", "app.js")); err != nil {
		t.Fatalf("assets/app.js not extracted: %v", err)
	}
	markerBytes, err := os.ReadFile(filepath.Join(dir, shaMarkerFileName))
	if err != nil {
		t.Fatalf("reading integrity marker: %v", err)
	}
	if string(markerBytes) != sha256Hex(zipBytes) {
		t.Fatalf("marker = %q, want the manifest's sha256", markerBytes)
	}

	// Prove the resolved dir is servable end-to-end through the existing
	// --ui-dir handler, same-origin, per the packaging contract.
	srv := newTestServer(t, Config{UIDir: dir})
	uiTS := httptest.NewServer(srv.Handler())
	defer uiTS.Close()
	resp, err := http.Get(uiTS.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "studio ui") {
		t.Fatalf("GET / = %d %q, want the served index.html", resp.StatusCode, body)
	}
}

func TestResolveOfflineUIRejectsSHA256Mismatch(t *testing.T) {
	zipBytes := buildTestUIZip(t, nil)
	manifest := uiManifest{Version: "1.0.0", SHA256: strings.Repeat("0", 64), UIContract: SupportedUIContract}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/studio-ui.manifest.json", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(manifestJSON) })
	mux.HandleFunc("/studio-ui.zip", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(zipBytes) })
	ts := httptest.NewServer(mux)
	defer ts.Close()

	cacheDir := t.TempDir()
	_, err = ResolveOfflineUI(context.Background(), OfflineUIOptions{BaseURL: ts.URL + "/", CacheDir: cacheDir})
	if err == nil {
		t.Fatal("ResolveOfflineUI() error = nil, want a sha256-mismatch error")
	}
	if !strings.Contains(err.Error(), "sha256") {
		t.Fatalf("error = %v, want it to mention sha256", err)
	}
	if _, statErr := os.Stat(filepath.Join(cacheDir, "1.0.0")); statErr == nil {
		t.Fatal("version directory must not exist after a sha256 mismatch: nothing should be served")
	}
}

func TestResolveOfflineUIRejectsZipSlip(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	good, err := zw.Create("index.html")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = good.Write([]byte("<html>ok</html>"))
	evil, err := zw.Create("../evil")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = evil.Write([]byte("pwned"))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zipBytes := buf.Bytes()

	ts, _ := newTestUIReleaseServer(t, "2.0.0", zipBytes, SupportedUIContract)
	defer ts.Close()

	cacheDir := t.TempDir()
	_, err = ResolveOfflineUI(context.Background(), OfflineUIOptions{BaseURL: ts.URL + "/", CacheDir: cacheDir})
	if err == nil {
		t.Fatal("ResolveOfflineUI() error = nil, want the zip-slip entry rejected")
	}
	if !strings.Contains(err.Error(), "zip-slip") {
		t.Fatalf("error = %v, want it to mention zip-slip", err)
	}
	// "../evil" inside cacheDir/2.0.0/ resolves to cacheDir/evil — confirm
	// it was never written, anywhere.
	if _, statErr := os.Stat(filepath.Join(cacheDir, "evil")); statErr == nil {
		t.Fatal("zip-slip entry was written outside the cache dir")
	}
	if _, statErr := os.Stat(filepath.Join(cacheDir, "2.0.0")); statErr == nil {
		t.Fatal("version directory must not exist after a rejected zip-slip entry: nothing should be served")
	}
}

func TestResolveOfflineUISkipsSymlinkEntries(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	idx, err := zw.Create("index.html")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = idx.Write([]byte("<html>ok</html>"))

	fh := &zip.FileHeader{Name: "sneaky-link", Method: zip.Deflate}
	fh.SetMode(os.ModeSymlink | 0o777)
	linkWriter, err := zw.CreateHeader(fh)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = linkWriter.Write([]byte("/etc/passwd"))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zipBytes := buf.Bytes()

	ts, _ := newTestUIReleaseServer(t, "6.0.0", zipBytes, SupportedUIContract)
	defer ts.Close()

	cacheDir := t.TempDir()
	dir, err := ResolveOfflineUI(context.Background(), OfflineUIOptions{BaseURL: ts.URL + "/", CacheDir: cacheDir})
	if err != nil {
		t.Fatalf("ResolveOfflineUI() error = %v", err)
	}
	if _, statErr := os.Lstat(filepath.Join(dir, "sneaky-link")); statErr == nil {
		t.Fatal("symlink entry should have been skipped, not extracted")
	}
}

func TestResolveOfflineUIRefusesIncompatibleContract(t *testing.T) {
	zipBytes := buildTestUIZip(t, nil)
	ts, zipRequests := newTestUIReleaseServer(t, "3.0.0", zipBytes, SupportedUIContract+1)
	defer ts.Close()

	cacheDir := t.TempDir()
	_, err := ResolveOfflineUI(context.Background(), OfflineUIOptions{BaseURL: ts.URL + "/", CacheDir: cacheDir})
	if err == nil {
		t.Fatal("ResolveOfflineUI() error = nil, want a uiContract mismatch refusal")
	}
	if !strings.Contains(err.Error(), "uiContract") {
		t.Fatalf("error = %v, want it to mention uiContract", err)
	}
	if _, statErr := os.Stat(filepath.Join(cacheDir, "3.0.0")); statErr == nil {
		t.Fatal("an incompatible UI must never be extracted/cached")
	}
	if got := atomic.LoadInt32(zipRequests); got != 0 {
		t.Fatalf("zip requests = %d, want 0: the zip must never be downloaded for an incompatible manifest", got)
	}
}

func TestResolveOfflineUISecondRunUsesCacheNoRedownload(t *testing.T) {
	zipBytes := buildTestUIZip(t, nil)
	ts, zipRequests := newTestUIReleaseServer(t, "4.0.0", zipBytes, SupportedUIContract)
	defer ts.Close()

	cacheDir := t.TempDir()
	opts := OfflineUIOptions{BaseURL: ts.URL + "/", CacheDir: cacheDir}

	if _, err := ResolveOfflineUI(context.Background(), opts); err != nil {
		t.Fatalf("first ResolveOfflineUI() error = %v", err)
	}
	if got := atomic.LoadInt32(zipRequests); got != 1 {
		t.Fatalf("zip requests after first run = %d, want 1", got)
	}

	if _, err := ResolveOfflineUI(context.Background(), opts); err != nil {
		t.Fatalf("second ResolveOfflineUI() error = %v", err)
	}
	if got := atomic.LoadInt32(zipRequests); got != 1 {
		t.Fatalf("zip requests after second (warm-cache) run = %d, want still 1 (no re-download)", got)
	}
}

func TestResolveOfflineUIOfflineFallsBackToCache(t *testing.T) {
	zipBytes := buildTestUIZip(t, nil)
	ts, _ := newTestUIReleaseServer(t, "5.0.0", zipBytes, SupportedUIContract)

	cacheDir := t.TempDir()
	opts := OfflineUIOptions{BaseURL: ts.URL + "/", CacheDir: cacheDir}
	if _, err := ResolveOfflineUI(context.Background(), opts); err != nil {
		t.Fatalf("priming ResolveOfflineUI() error = %v", err)
	}
	ts.Close() // simulate offline: the base URL is now unreachable

	dir, err := ResolveOfflineUI(context.Background(), opts)
	if err != nil {
		t.Fatalf("offline ResolveOfflineUI() error = %v, want it to fall back to the warm cache", err)
	}
	if want := filepath.Join(cacheDir, "5.0.0"); dir != want {
		t.Fatalf("dir = %q, want %q", dir, want)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "index.html")); statErr != nil {
		t.Fatalf("cached index.html missing: %v", statErr)
	}
}

func TestResolveOfflineUINoNetworkNoCacheFailsClearly(t *testing.T) {
	cacheDir := t.TempDir()
	_, err := ResolveOfflineUI(context.Background(), OfflineUIOptions{
		BaseURL:  "http://127.0.0.1:1/", // reserved, nothing listens: fails fast
		CacheDir: cacheDir,
	})
	if err == nil {
		t.Fatal("ResolveOfflineUI() error = nil, want a clear failure with no network and no cache")
	}
	if !strings.Contains(err.Error(), cacheDir) {
		t.Fatalf("error = %v, want it to mention the cache dir it looked in", err)
	}
}

func TestDefaultUICacheDirEndsInUI(t *testing.T) {
	got := DefaultUICacheDir()
	if filepath.Base(got) != "ui" {
		t.Fatalf("DefaultUICacheDir() = %q, want it to end in ui", got)
	}
}
