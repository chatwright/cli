// proxy.go is the OpenAI-compatible reverse proxy for POST
// /v1/chat/completions: it forwards the caller's request body verbatim,
// server-to-server, to Config.UpstreamBaseURL (a local Ollama/LM Studio/any
// OpenAI-compatible server) and relays the response back faithfully —
// preserving status code and body — while recording a CallMetric for
// GET /metrics.
//
// A non-streaming ("stream" absent or false) call is buffered: the full
// response is read so its JSON body's model/usage.prompt_tokens/
// usage.completion_tokens can be extracted for the metric before it is
// written back to the caller. A streaming call ("stream": true) is relayed
// chunk-by-chunk with a flush after every chunk, exactly as the upstream
// sends it — Server-Sent-Events deltas do not carry cumulative token counts
// by default, so a streamed call's CallMetric always has zero
// prompt/completion tokens (Streamed is set to true precisely so a
// GET /metrics consumer can tell "zero because streaming" apart from "zero
// because unknown").
package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

// proxyTimeout bounds a buffered (non-streaming) call end-to-end. It is
// generous because local model inference — the whole point of this
// server — is routinely much slower than a hosted API.
const proxyTimeout = 5 * time.Minute

// chatCompletionRequest is the minimal shape this proxy reads out of an
// otherwise-opaque request body for its own bookkeeping (metrics). It is
// never used to reconstruct the outbound body — the original bytes are
// forwarded unchanged so no field the caller sent, known or not, is ever
// dropped.
type chatCompletionRequest struct {
	Model          string `json:"model"`
	Stream         bool   `json:"stream"`
	ResponseFormat *struct {
		Type string `json:"type"`
	} `json:"response_format"`
}

// chatCompletionResponse is the minimal shape this proxy reads out of a
// buffered (non-streaming) response body to enrich a CallMetric.
type chatCompletionResponse struct {
	Model string `json:"model"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

// hopByHopHeaders are never forwarded in either direction, per RFC 7230
// §6.1 — they describe the connection to the immediate peer, not the
// resource.
var hopByHopHeaders = []string{
	"Connection", "Proxy-Connection", "Keep-Alive", "Transfer-Encoding",
	"Upgrade", "Te", "Trailer",
}

// clientOnlyRequestHeaders are browser-set request headers that must not be
// forwarded upstream to the model server. This proxy exists precisely to turn
// a browser's cross-origin call into a clean server-to-server one: an upstream
// such as Ollama enforces its OWN CORS allowlist and answers 403 to any
// request carrying an Origin it does not recognise — so the Origin the browser
// stamped on its call to us must be dropped before we call the model. Referer,
// Cookie and the Sec-Fetch-*/Access-Control-Request-* fetch-metadata headers
// are dropped for the same reason: the upstream is not the browser's peer.
// (Authorization is deliberately NOT in this list — it carries the caller's
// key for the upstream and must be forwarded.)
var clientOnlyRequestHeaders = []string{
	"Origin", "Referer", "Cookie",
	"Sec-Fetch-Dest", "Sec-Fetch-Mode", "Sec-Fetch-Site", "Sec-Fetch-User",
	"Access-Control-Request-Method", "Access-Control-Request-Headers",
}

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "reading request body: "+err.Error())
		return
	}

	var parsed chatCompletionRequest
	_ = json.Unmarshal(body, &parsed) // best-effort: an unparseable body is still forwarded verbatim

	upstreamURL := s.upstreamBaseURL + "/chat/completions"
	outReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, upstreamURL, bytes.NewReader(body))
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "building upstream request: "+err.Error())
		return
	}
	copyForwardHeaders(r.Header, outReq.Header)
	for _, h := range clientOnlyRequestHeaders {
		outReq.Header.Del(h)
	}
	if outReq.Header.Get("Content-Type") == "" {
		outReq.Header.Set("Content-Type", "application/json")
	}

	start := time.Now()
	resp, err := s.httpClient.Do(outReq)
	if err != nil {
		s.logger.Printf("proxy: model=%q upstream=%s error=%v", parsed.Model, upstreamURL, err)
		s.metrics.record(CallMetric{
			Timestamp:      start,
			Model:          parsed.Model,
			LatencyMS:      time.Since(start).Milliseconds(),
			ResponseFormat: responseFormatOf(parsed),
			Streamed:       parsed.Stream,
			Err:            err.Error(),
		})
		writeJSONError(w, http.StatusBadGateway, "upstream request failed: "+err.Error())
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if parsed.Stream || isEventStream(resp.Header.Get("Content-Type")) {
		s.relayStreaming(w, resp, start, parsed)
		return
	}
	s.relayBuffered(w, resp, start, parsed)
}

// relayBuffered reads the full upstream response before writing anything
// back, so the response JSON's model/usage fields can be extracted for the
// CallMetric — the buffering is what "faithfully" means here: the caller
// receives exactly the same status and bytes the upstream sent, just not
// streamed incrementally.
func (s *Server) relayBuffered(w http.ResponseWriter, resp *http.Response, start time.Time, req chatCompletionRequest) {
	respBody, err := io.ReadAll(resp.Body)
	latency := time.Since(start)
	if err != nil {
		s.metrics.record(CallMetric{
			Timestamp: start, Model: req.Model, LatencyMS: latency.Milliseconds(),
			ResponseFormat: responseFormatOf(req), Streamed: false, Status: resp.StatusCode, Err: err.Error(),
		})
		writeJSONError(w, http.StatusBadGateway, "reading upstream response: "+err.Error())
		return
	}

	var parsedResp chatCompletionResponse
	_ = json.Unmarshal(respBody, &parsedResp) // best-effort, mirrors the request-side parse

	model := parsedResp.Model
	if model == "" {
		model = req.Model
	}
	metric := CallMetric{
		Timestamp:      start,
		Model:          model,
		LatencyMS:      latency.Milliseconds(),
		ResponseFormat: responseFormatOf(req),
		Streamed:       false,
		Status:         resp.StatusCode,
	}
	if parsedResp.Usage != nil {
		metric.PromptTokens = parsedResp.Usage.PromptTokens
		metric.CompletionTokens = parsedResp.Usage.CompletionTokens
	}
	s.metrics.record(metric)
	s.logger.Printf("proxy: model=%q status=%d latency=%s prompt_tokens=%d completion_tokens=%d",
		metric.Model, metric.Status, latency, metric.PromptTokens, metric.CompletionTokens)

	copyForwardHeaders(resp.Header, w.Header())
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(respBody)
}

// relayStreaming copies the upstream response to the caller chunk-by-chunk,
// flushing after every chunk, so an SSE stream reaches the caller with the
// same incremental timing it left the upstream with.
func (s *Server) relayStreaming(w http.ResponseWriter, resp *http.Response, start time.Time, req chatCompletionRequest) {
	copyForwardHeaders(resp.Header, w.Header())
	w.WriteHeader(resp.StatusCode)

	flusher, canFlush := w.(http.Flusher)
	buf := make([]byte, 4096)
	var streamErr error
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				streamErr = writeErr
				break
			}
			if canFlush {
				flusher.Flush()
			}
		}
		if err != nil {
			if err != io.EOF {
				streamErr = err
			}
			break
		}
	}

	metric := CallMetric{
		Timestamp:      start,
		Model:          req.Model,
		LatencyMS:      time.Since(start).Milliseconds(),
		ResponseFormat: responseFormatOf(req),
		Streamed:       true,
		Status:         resp.StatusCode,
	}
	if streamErr != nil {
		metric.Err = streamErr.Error()
	}
	s.metrics.record(metric)
	s.logger.Printf("proxy: model=%q status=%d latency=%s streamed=true", metric.Model, metric.Status, time.Since(start))
}

func responseFormatOf(req chatCompletionRequest) string {
	if req.ResponseFormat == nil {
		return ""
	}
	return req.ResponseFormat.Type
}

func isEventStream(contentType string) bool {
	return strings.Contains(strings.ToLower(contentType), "text/event-stream")
}

// copyForwardHeaders copies every header from src to dst except hop-by-hop
// headers (RFC 7230 §6.1), which must never be forwarded in either
// direction: they describe the connection to the immediate peer, not the
// proxied resource.
func copyForwardHeaders(src, dst http.Header) {
	skip := make(map[string]struct{}, len(hopByHopHeaders))
	for _, h := range hopByHopHeaders {
		skip[http.CanonicalHeaderKey(h)] = struct{}{}
	}
	for k, values := range src {
		if _, blocked := skip[http.CanonicalHeaderKey(k)]; blocked {
			continue
		}
		for _, v := range values {
			dst.Add(k, v)
		}
	}
}
