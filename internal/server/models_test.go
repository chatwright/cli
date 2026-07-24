package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestModelsProxiesUpstreamListAndStripsOrigin(t *testing.T) {
	var gotPath, gotOrigin string
	fakeUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotOrigin = r.Header.Get("Origin")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"qwen3.6:latest"},{"id":"llama3.2:3b"}]}`))
	}))
	defer fakeUpstream.Close()

	srv := newProxyTestServer(t, fakeUpstream)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/v1/models", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "http://chatwright.localhost:4200")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if gotPath != "/models" {
		t.Fatalf("upstream path = %q, want /models", gotPath)
	}
	if gotOrigin != "" {
		t.Fatalf("upstream saw Origin = %q, want it stripped", gotOrigin)
	}
	// The CORS layer allowed the browser origin on the response.
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "http://chatwright.localhost:4200" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want the echoed origin", got)
	}

	body, _ := io.ReadAll(resp.Body)
	var decoded struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Data) != 2 || decoded.Data[0].ID != "qwen3.6:latest" {
		t.Fatalf("model list = %+v, want the two upstream models faithfully relayed", decoded.Data)
	}
}

func TestModelsRejectsNonGET(t *testing.T) {
	srv := newTestServer(t, Config{})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/v1/models", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", resp.StatusCode)
	}
}
