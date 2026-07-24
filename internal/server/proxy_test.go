package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newProxyTestServer builds a Server whose upstream is fakeUpstream, wired
// via Config.HTTPClient so no real network call is ever made.
func newProxyTestServer(t *testing.T, fakeUpstream *httptest.Server) *Server {
	t.Helper()
	srv, err := New(Config{
		Version:         "test",
		UpstreamBaseURL: fakeUpstream.URL,
		HTTPClient:      fakeUpstream.Client(),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return srv
}

func TestProxyForwardsRequestAndBuffersResponse(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	var gotAuth string

	fakeUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"model": "llama3.2",
			"choices": [{"message": {"role": "assistant", "content": "hi"}}],
			"usage": {"prompt_tokens": 12, "completion_tokens": 7}
		}`))
	}))
	defer fakeUpstream.Close()

	srv := newProxyTestServer(t, fakeUpstream)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqBody := `{"model":"llama3.2","messages":[{"role":"user","content":"hello"}],"response_format":{"type":"json_object"}}`
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/v1/chat/completions", bytes.NewBufferString(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-key")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	// The upstream received exactly what the caller sent.
	if gotPath != "/chat/completions" {
		t.Fatalf("upstream path = %q, want /chat/completions", gotPath)
	}
	if gotAuth != "Bearer test-key" {
		t.Fatalf("upstream Authorization = %q, want forwarded", gotAuth)
	}
	if gotBody["model"] != "llama3.2" {
		t.Fatalf("upstream body model = %v, want llama3.2", gotBody["model"])
	}

	// The caller received exactly what the upstream sent back.
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	respBody, _ := io.ReadAll(resp.Body)
	var decoded map[string]any
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["model"] != "llama3.2" {
		t.Fatalf("response model = %v, want llama3.2 (faithful passthrough)", decoded["model"])
	}

	// A metric was recorded with the expected fields.
	metricsResp, err := http.Get(ts.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = metricsResp.Body.Close() }()
	var metrics []CallMetric
	if err := json.NewDecoder(metricsResp.Body).Decode(&metrics); err != nil {
		t.Fatal(err)
	}
	if len(metrics) != 1 {
		t.Fatalf("metrics count = %d, want 1: %+v", len(metrics), metrics)
	}
	m := metrics[0]
	if m.Model != "llama3.2" {
		t.Errorf("metric.Model = %q, want llama3.2", m.Model)
	}
	if m.PromptTokens != 12 || m.CompletionTokens != 7 {
		t.Errorf("metric tokens = %d/%d, want 12/7", m.PromptTokens, m.CompletionTokens)
	}
	if m.ResponseFormat != "json_object" {
		t.Errorf("metric.ResponseFormat = %q, want json_object", m.ResponseFormat)
	}
	if m.Status != http.StatusOK {
		t.Errorf("metric.Status = %d, want 200", m.Status)
	}
	if m.Streamed {
		t.Error("metric.Streamed = true, want false for a buffered call")
	}
	if m.LatencyMS < 0 {
		t.Errorf("metric.LatencyMS = %d, want >= 0", m.LatencyMS)
	}
}

func TestProxyPreservesUpstreamErrorStatus(t *testing.T) {
	fakeUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error": {"message": "rate limited"}}`))
	}))
	defer fakeUpstream.Close()

	srv := newProxyTestServer(t, fakeUpstream)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/v1/chat/completions", "application/json", bytes.NewBufferString(`{"model":"m"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 (preserved from upstream)", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != `{"error": {"message": "rate limited"}}` {
		t.Fatalf("body = %q, want the upstream's own error body preserved verbatim", body)
	}
}

func TestProxyStreamingRelaysChunksAndSkipsTokenMetrics(t *testing.T) {
	fakeUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		for _, chunk := range []string{
			"data: {\"choices\":[{\"delta\":{\"content\":\"hel\"}}]}\n\n",
			"data: {\"choices\":[{\"delta\":{\"content\":\"lo\"}}]}\n\n",
			"data: [DONE]\n\n",
		} {
			_, _ = w.Write([]byte(chunk))
			flusher.Flush()
		}
	}))
	defer fakeUpstream.Close()

	srv := newProxyTestServer(t, fakeUpstream)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqBody := `{"model":"llama3.2","stream":true,"messages":[{"role":"user","content":"hi"}]}`
	resp, err := http.Post(ts.URL+"/v1/chat/completions", "application/json", bytes.NewBufferString(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	if !bytes.Contains(body, []byte("hel")) || !bytes.Contains(body, []byte("[DONE]")) {
		t.Fatalf("streamed body = %q, want all chunks relayed", body)
	}

	metricsResp, err := http.Get(ts.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = metricsResp.Body.Close() }()
	var metrics []CallMetric
	if err := json.NewDecoder(metricsResp.Body).Decode(&metrics); err != nil {
		t.Fatal(err)
	}
	if len(metrics) != 1 {
		t.Fatalf("metrics count = %d, want 1", len(metrics))
	}
	if !metrics[0].Streamed {
		t.Error("metric.Streamed = false, want true")
	}
	if metrics[0].PromptTokens != 0 || metrics[0].CompletionTokens != 0 {
		t.Errorf("streamed metric tokens = %d/%d, want 0/0 (not discernible from SSE)", metrics[0].PromptTokens, metrics[0].CompletionTokens)
	}
}

func TestProxyUpstreamUnreachableReturns502(t *testing.T) {
	srv, err := New(Config{
		Version:         "test",
		UpstreamBaseURL: "http://127.0.0.1:1", // reserved, nothing listens here
		HTTPClient:      &http.Client{},
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/v1/chat/completions", "application/json", bytes.NewBufferString(`{"model":"m"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", resp.StatusCode)
	}
}

func TestProxyRejectsNonPOST(t *testing.T) {
	srv := newTestServer(t, Config{})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/v1/chat/completions")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", resp.StatusCode)
	}
}
