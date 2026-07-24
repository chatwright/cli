package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"chatwright.dev/runtime/datastate"
)

func TestDatastateQueryUnsupportedWhenNoStoreConfigured(t *testing.T) {
	srv := newTestServer(t, Config{}) // no FixturesPath
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqBody := `{"query": "select from anything", "expectations": [{"type": "nonEmpty"}]}`
	resp, err := http.Post(ts.URL+"/datastate/query", "application/json", bytes.NewBufferString(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var got datastateQueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Verdict != "unsupported" {
		t.Fatalf("Verdict = %q, want unsupported", got.Verdict)
	}
	if got.Rows != nil {
		t.Fatalf("Rows = %v, want nil (no query ever ran)", got.Rows)
	}
	if got.Detail == "" {
		t.Fatal("Detail is empty, want an explanation of what to configure")
	}
}

func fixturesFile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fixtures.json")
	contents := `{
		"listus": {
			"select from lists where key = buy!groceries": [
				{"title": "milk", "active": true},
				{"title": "bread", "active": false}
			],
			"select from lists where key = empty!list": []
		}
	}`
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDatastateQueryPassWhenExpectationsHold(t *testing.T) {
	srv := newTestServer(t, Config{FixturesPath: fixturesFile(t)})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqBody := `{
		"holder": "listus",
		"query": "select from lists where key = buy!groceries",
		"expectations": [
			{"type": "nonEmpty"},
			{"type": "rowContains", "row": {"title": "milk", "active": true}}
		]
	}`
	resp, err := http.Post(ts.URL+"/datastate/query", "application/json", bytes.NewBufferString(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	var got datastateQueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Verdict != "pass" {
		t.Fatalf("Verdict = %q, want pass; detail = %q", got.Verdict, got.Detail)
	}
	if len(got.Rows) != 2 {
		t.Fatalf("Rows = %v, want 2 rows", got.Rows)
	}
	if got.Detail != "" {
		t.Fatalf("Detail = %q, want empty on pass", got.Detail)
	}
}

func TestDatastateQueryFailWhenExpectationMismatches(t *testing.T) {
	srv := newTestServer(t, Config{FixturesPath: fixturesFile(t)})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqBody := `{
		"holder": "listus",
		"query": "select from lists where key = empty!list",
		"expectations": [{"type": "nonEmpty"}]
	}`
	resp, err := http.Post(ts.URL+"/datastate/query", "application/json", bytes.NewBufferString(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	var got datastateQueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Verdict != "fail" {
		t.Fatalf("Verdict = %q, want fail", got.Verdict)
	}
	if got.Detail == "" {
		t.Fatal("Detail is empty, want the assertion failure message")
	}
}

func TestDatastateQueryFailWhenQueryHasNoFixture(t *testing.T) {
	srv := newTestServer(t, Config{FixturesPath: fixturesFile(t)})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqBody := `{"holder": "listus", "query": "select something never recorded"}`
	resp, err := http.Post(ts.URL+"/datastate/query", "application/json", bytes.NewBufferString(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	var got datastateQueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Verdict != "fail" {
		t.Fatalf("Verdict = %q, want fail (query execution error, not silently unsupported)", got.Verdict)
	}
}

func TestDatastateQueryRequiresQuery(t *testing.T) {
	srv := newTestServer(t, Config{})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/datastate/query", "application/json", bytes.NewBufferString(`{"expectations": []}`))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestDatastateQueryRejectsUnknownExpectationType(t *testing.T) {
	srv := newTestServer(t, Config{FixturesPath: fixturesFile(t)})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqBody := `{"holder": "listus", "query": "select from lists where key = buy!groceries", "expectations": [{"type": "bogus"}]}`
	resp, err := http.Post(ts.URL+"/datastate/query", "application/json", bytes.NewBufferString(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for an unknown expectation type", resp.StatusCode)
	}
}

func TestDatastateQueryRejectsMalformedJSON(t *testing.T) {
	srv := newTestServer(t, Config{})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/datastate/query", "application/json", bytes.NewBufferString(`{not json`))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestDatastateQueryRejectsNonPOST(t *testing.T) {
	srv := newTestServer(t, Config{})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/datastate/query")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", resp.StatusCode)
	}
}

// --- unit tests for the expectation DSL and FixtureStore directly ---

func TestBuildExpectationAllCombinesConjunctively(t *testing.T) {
	spec := []expectationSpec{
		{Type: "all", Of: []expectationSpec{
			{Type: "minRowCount", Count: 1},
			{Type: "maxRowCount", Count: 5},
		}},
	}
	expect, err := buildExpectation(spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := expect([]datastate.Row{{"a": 1}, {"a": 2}}); err != nil {
		t.Fatalf("expect() = %v, want pass", err)
	}
	if err := expect(nil); err == nil {
		t.Fatal("expect(nil) = nil, want failure (minRowCount 1 unmet)")
	}
}

func TestFixtureStoreExecuteUnknownHandleType(t *testing.T) {
	store := NewFixtureStore(map[string]map[string][]datastate.Row{
		"h": {"q": {{"ok": true}}},
	})
	_, err := store.Execute(context.Background(), "not-the-right-handle-type", datastate.Query{DTQL: "q"})
	if err == nil {
		t.Fatal("Execute() with wrong handle type = nil error, want an error")
	}
}
