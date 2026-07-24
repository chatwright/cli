// ui_offline.go implements `chatwright server serve --ui`'s
// download/cache/verify pipeline for the Studio web UI, so the whole tester
// can run with no network after the first successful fetch. It hands its
// result to ui.go's existing --ui-dir handler, unchanged: this file's only
// job is getting a verified UI onto local disk and returning that
// directory, never serving HTTP itself.
//
// The packaging contract this file consumes is produced by the Studio
// release process, not by this repository, and must match exactly:
//
//   - studio-ui.zip — a zip whose root contains the built SPA (index.html
//     at the root, assets alongside).
//   - studio-ui.manifest.json — {"version": "<string>", "sha256": "<lowercase
//     hex sha256 of studio-ui.zip>", "uiContract": <int>}. uiContract is the
//     UI<->server compatibility integer this file gates on.
//   - Both are published to stable URLs under one release base, by default
//     the Studio repository's GitHub "latest release" alias
//     (DefaultUIBaseURL); --ui-url overrides the base for self-hosting or
//     tests.
//
// The pipeline, per ResolveOfflineUI:
//
//  1. GET {base}/studio-ui.manifest.json.
//  2. Compatibility gate: manifest.uiContract must equal SupportedUIContract.
//     A mismatch is refused outright — this CLI build never serves a UI it
//     cannot vouch for, rather than degrading silently ("fidelity is
//     declared").
//  3. Cache path is {cacheDir}/{manifest.version}/. If that directory
//     already holds a ".sha256" marker matching manifest.sha256, the cache
//     is used as-is — no re-download.
//  4. Otherwise: GET {base}/studio-ui.zip, verify its sha256 against the
//     manifest (reject on mismatch, before extracting anything), extract it
//     into the cache path with a zip-slip guard (any entry whose cleaned
//     destination would land outside the cache directory is rejected,
//     before any file is written) and symlink entries skipped outright,
//     then write the ".sha256" marker on success.
//  5. If the manifest GET itself fails (no network) but a previously
//     extracted version is already cached, that cached version is served
//     with a logged note instead of failing — this is what makes offline
//     re-runs work. With no cache to fall back to, the fetch error is
//     returned, wrapped with enough context to act on.
package server

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// SupportedUIContract is the UI<->server compatibility integer this CLI
// build understands. ResolveOfflineUI refuses to serve any Studio UI
// release whose manifest declares a different uiContract: a testing tool
// that quietly drifts from what it claims to test is worse than one that
// refuses to run.
const SupportedUIContract = 1

// DefaultUIBaseURL is the stable download location for the Studio web UI:
// the studio repository's own GitHub Releases "latest" alias. Override via
// OfflineUIOptions.BaseURL (wired to --ui-url) to self-host a mirror or
// point tests at an httptest.Server.
const DefaultUIBaseURL = "https://github.com/chatwright/studio/releases/latest/download/"

// manifestFileName, zipFileName and shaMarkerFileName name the three files
// this pipeline reads or writes: the first two are the Studio release
// artifacts (fetched, never written by this file); the third is this file's
// own on-disk integrity record, written once extraction of a version
// succeeds.
const (
	manifestFileName  = "studio-ui.manifest.json"
	zipFileName       = "studio-ui.zip"
	shaMarkerFileName = ".sha256"
)

// uiDownloadTimeout bounds the default HTTP client ResolveOfflineUI builds
// when the caller supplies none. The Studio UI zip is a small SPA bundle,
// not a model download, so this stays modest.
const uiDownloadTimeout = 2 * time.Minute

// uiManifest is studio-ui.manifest.json's shape — see this file's package
// doc comment for the full packaging contract.
type uiManifest struct {
	Version    string `json:"version"`
	SHA256     string `json:"sha256"`
	UIContract int    `json:"uiContract"`
}

// OfflineUIOptions configures ResolveOfflineUI.
type OfflineUIOptions struct {
	// BaseURL is the release base to fetch studio-ui.manifest.json/.zip
	// from. Empty uses DefaultUIBaseURL.
	BaseURL string
	// CacheDir is the root directory extracted UI versions are cached
	// under, as CacheDir/<version>/. Empty uses DefaultUICacheDir().
	CacheDir string
	// HTTPClient issues the manifest/zip GETs. A nil value builds a client
	// with uiDownloadTimeout. Tests inject their own client pointed at an
	// httptest.Server without touching a real network.
	HTTPClient *http.Client
	// Logger receives progress and fallback notices (download start, cache
	// hit, offline fallback). A nil Logger discards all output.
	Logger *log.Logger
}

// DefaultUICacheDir is where ResolveOfflineUI caches downloaded Studio UI
// builds, keyed by version (DefaultUICacheDir()/<version>/). Override via
// OfflineUIOptions.CacheDir.
func DefaultUICacheDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".chatwright", "ui")
	}
	return filepath.Join(home, ".chatwright", "ui")
}

// ResolveOfflineUI runs the download/cache/verify pipeline documented in
// this file's package doc comment and returns the local directory holding
// the resolved Studio UI's static files — ready to pass straight to
// Config.UIDir. It never opens an HTTP listener itself.
func ResolveOfflineUI(ctx context.Context, opts OfflineUIOptions) (string, error) {
	base := opts.BaseURL
	if base == "" {
		base = DefaultUIBaseURL
	}
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	cacheDir := opts.CacheDir
	if cacheDir == "" {
		cacheDir = DefaultUICacheDir()
	}
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: uiDownloadTimeout}
	}
	logger := opts.Logger
	if logger == nil {
		logger = log.New(discardWriter{}, "", 0)
	}

	manifestURL := base + manifestFileName
	manifest, err := fetchUIManifest(ctx, client, manifestURL)
	if err != nil {
		version, cacheErr := newestCachedUIVersion(cacheDir)
		if cacheErr != nil {
			return "", fmt.Errorf("chatwright: fetching Studio UI manifest from %s: %w (and no cached UI is available in %s to fall back to)", manifestURL, err, cacheDir)
		}
		dir := filepath.Join(cacheDir, version)
		logger.Printf("ui: could not reach %s (%v); serving cached Studio UI %s from %s", manifestURL, err, version, dir)
		return dir, nil
	}

	if manifest.UIContract != SupportedUIContract {
		return "", fmt.Errorf("chatwright: Studio UI %s declares uiContract=%d but this CLI build only supports uiContract=%d; refusing to serve a UI it cannot guarantee fidelity with (upgrade the chatwright CLI, or pin a UI release built for uiContract=%d)",
			manifest.Version, manifest.UIContract, SupportedUIContract, SupportedUIContract)
	}
	if manifest.Version == "" {
		return "", errors.New("chatwright: Studio UI manifest is missing its version field")
	}
	if strings.ContainsAny(manifest.Version, `/\`) || strings.Contains(manifest.Version, "..") {
		return "", fmt.Errorf("chatwright: Studio UI manifest version %q is not a safe cache directory name", manifest.Version)
	}
	if manifest.SHA256 == "" {
		return "", errors.New("chatwright: Studio UI manifest is missing its sha256 field")
	}

	versionDir := filepath.Join(cacheDir, manifest.Version)
	if cachedSHA, err := os.ReadFile(filepath.Join(versionDir, shaMarkerFileName)); err == nil {
		if strings.EqualFold(strings.TrimSpace(string(cachedSHA)), manifest.SHA256) {
			logger.Printf("ui: using cached Studio UI %s from %s", manifest.Version, versionDir)
			return versionDir, nil
		}
		logger.Printf("ui: cached Studio UI %s at %s does not match the current manifest's sha256; re-downloading", manifest.Version, versionDir)
	}

	zipURL := base + zipFileName
	logger.Printf("ui: downloading Studio UI %s from %s", manifest.Version, zipURL)
	zipBytes, err := fetchUIBytes(ctx, client, zipURL)
	if err != nil {
		return "", fmt.Errorf("chatwright: downloading Studio UI zip from %s: %w", zipURL, err)
	}

	sum := sha256.Sum256(zipBytes)
	gotSHA := hex.EncodeToString(sum[:])
	if !strings.EqualFold(gotSHA, manifest.SHA256) {
		return "", fmt.Errorf("chatwright: Studio UI zip from %s has sha256 %s, manifest declares %s; refusing to serve a download that does not match its declared integrity",
			zipURL, gotSHA, manifest.SHA256)
	}

	if err := extractUIZip(zipBytes, versionDir); err != nil {
		return "", fmt.Errorf("chatwright: extracting Studio UI zip into %s: %w", versionDir, err)
	}
	if err := os.WriteFile(filepath.Join(versionDir, shaMarkerFileName), []byte(manifest.SHA256), 0o644); err != nil {
		return "", fmt.Errorf("chatwright: writing Studio UI integrity marker in %s: %w", versionDir, err)
	}

	logger.Printf("ui: cached Studio UI %s at %s", manifest.Version, versionDir)
	return versionDir, nil
}

// fetchUIManifest GETs url and decodes it as a uiManifest.
func fetchUIManifest(ctx context.Context, client *http.Client, url string) (*uiManifest, error) {
	body, err := fetchUIBytes(ctx, client, url)
	if err != nil {
		return nil, err
	}
	var m uiManifest
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("parsing manifest JSON: %w", err)
	}
	return &m, nil
}

// fetchUIBytes GETs url and returns its full body, treating any non-200
// status as an error (there is no partial-success case worth modeling here:
// either the release asset is there or it isn't).
func fetchUIBytes(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: unexpected status %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// newestCachedUIVersion returns the name of the most-recently-extracted
// version directory under cacheDir — "most recent" meaning its integrity
// marker (written last, at the end of a successful extraction) has the
// latest mtime. Only directories carrying a marker count as "cached": one
// left behind by an interrupted or rejected extraction has no marker and is
// never offered as an offline fallback.
func newestCachedUIVersion(cacheDir string) (string, error) {
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return "", err
	}
	var (
		best     string
		bestTime time.Time
	)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		info, err := os.Stat(filepath.Join(cacheDir, e.Name(), shaMarkerFileName))
		if err != nil {
			continue // no marker => not a completed extraction, skip
		}
		if best == "" || info.ModTime().After(bestTime) {
			best, bestTime = e.Name(), info.ModTime()
		}
	}
	if best == "" {
		return "", fmt.Errorf("no cached Studio UI version found in %s", cacheDir)
	}
	return best, nil
}

// extractUIZip extracts zipBytes into destDir. It validates every entry's
// destination before writing anything: any entry whose cleaned path would
// land outside destDir is rejected (zip-slip guard), and any entry is
// rejected outright, with nothing written, so a hostile or corrupted
// archive never leaves a partially-extracted, half-trusted cache directory
// behind. Symlink entries are skipped rather than extracted — this pipeline
// only ever needs to place plain files and directories, and a symlink from
// an untrusted archive is never something that needs following.
func extractUIZip(zipBytes []byte, destDir string) error {
	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return fmt.Errorf("reading zip: %w", err)
	}

	type plannedEntry struct {
		file   *zip.File
		target string
	}
	var planned []plannedEntry
	for _, f := range zr.File {
		if f.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if strings.Contains(f.Name, "\\") {
			// The zip spec mandates "/" as the entry separator; a literal
			// backslash is either a filename character (harmless on Unix)
			// or, on Windows, a disguised path-traversal component. Reject
			// outright rather than trying to tell those apart.
			return fmt.Errorf("zip entry %q contains a backslash, which is never a valid zip path separator", f.Name)
		}
		cleaned := path.Clean(f.Name)
		if cleaned == "." {
			continue
		}
		target := filepath.Join(destDir, filepath.FromSlash(cleaned))
		if !isWithinDir(destDir, target) {
			return fmt.Errorf("zip entry %q would extract outside the cache directory (zip-slip)", f.Name)
		}
		planned = append(planned, plannedEntry{file: f, target: target})
	}

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", destDir, err)
	}
	for _, e := range planned {
		if e.file.FileInfo().IsDir() {
			if err := os.MkdirAll(e.target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(e.target), 0o755); err != nil {
			return err
		}
		if err := extractUIZipEntry(e.file, e.target); err != nil {
			return fmt.Errorf("extracting %q: %w", e.file.Name, err)
		}
	}
	return nil
}

// isWithinDir reports whether target is dir itself or a descendant of it.
// A zip entry name is attacker-controlled data, and archive/zip does
// nothing on its own to stop a ".."-laden entry name from resolving outside
// the extraction root (confirmed: zip.Writer.Create happily writes an entry
// literally named "../evil") — this check is what actually stops it.
func isWithinDir(dir, target string) bool {
	rel, err := filepath.Rel(dir, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// extractUIZipEntry writes one zip entry's content to target.
func extractUIZipEntry(f *zip.File, target string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()

	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	_, err = io.Copy(out, rc)
	return err
}
