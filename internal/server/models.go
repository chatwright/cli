// models.go serves GET /v1/models by proxying the upstream's model list
// (OpenAI-compatible: Ollama and LM Studio both expose /v1/models), so the
// Studio UI can offer a dropdown of the models actually available on this
// machine instead of a free-text field. Like the chat-completions proxy it
// drops the browser's Origin/fetch-metadata request headers before the
// server-to-server call, so the upstream's own CORS never rejects it.
package server

import (
	"io"
	"net/http"
)

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}

	upstreamURL := s.upstreamBaseURL + "/models"
	outReq, err := http.NewRequestWithContext(r.Context(), http.MethodGet, upstreamURL, nil)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "building upstream request: "+err.Error())
		return
	}
	copyForwardHeaders(r.Header, outReq.Header)
	for _, h := range clientOnlyRequestHeaders {
		outReq.Header.Del(h)
	}

	resp, err := s.httpClient.Do(outReq)
	if err != nil {
		s.logger.Printf("models: upstream=%s error=%v", upstreamURL, err)
		writeJSONError(w, http.StatusBadGateway, "upstream request failed: "+err.Error())
		return
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, "reading upstream response: "+err.Error())
		return
	}

	copyForwardHeaders(resp.Header, w.Header())
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(body)
}
