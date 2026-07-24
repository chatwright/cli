package server

import (
	"net/http"
	"sync"
	"time"
)

// defaultMetricsCapacity bounds the in-memory ring buffer GET /metrics
// reads from when Config.MetricsCapacity is unset. It is deliberately
// small and in-process: metrics reset on every restart, and this is
// documented, not a stand-in for a real metrics backend.
const defaultMetricsCapacity = 200

// CallMetric is one recorded /v1/chat/completions proxy call. Every field
// is best-effort: a proxied call that fails before or during forwarding
// still records what it can (Model/tokens may be empty/zero when the
// request or response body could not be parsed as JSON, e.g. a streaming
// response — see proxy.go's doc comment).
type CallMetric struct {
	Timestamp        time.Time `json:"timestamp"`
	Model            string    `json:"model,omitempty"`
	LatencyMS        int64     `json:"latencyMs"`
	PromptTokens     int       `json:"promptTokens,omitempty"`
	CompletionTokens int       `json:"completionTokens,omitempty"`
	// ResponseFormat is the request's response_format.type ("json_object",
	// "json_schema", "text", ...) when the request body declared one,
	// otherwise empty.
	ResponseFormat string `json:"responseFormat,omitempty"`
	// Streamed is true when the request declared "stream": true — the
	// proxy relays these byte-for-byte and cannot recover token counts
	// from an SSE body, so PromptTokens/CompletionTokens are always 0 for
	// a streamed call.
	Streamed bool `json:"streamed"`
	// Status is the HTTP status the upstream backend returned, or 0 when
	// the call never reached it (a network/dial failure).
	Status int `json:"status"`
	// Err is set when the call failed before a status was available.
	Err string `json:"error,omitempty"`
}

// metricsRing is a fixed-capacity, thread-safe ring buffer of the most
// recent CallMetrics.
type metricsRing struct {
	mu       sync.Mutex
	capacity int
	items    []CallMetric
}

func newMetricsRing(capacity int) *metricsRing {
	return &metricsRing{capacity: capacity, items: make([]CallMetric, 0, capacity)}
}

func (m *metricsRing) record(metric CallMetric) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items = append(m.items, metric)
	if len(m.items) > m.capacity {
		m.items = m.items[len(m.items)-m.capacity:]
	}
}

// snapshot returns a copy of the currently retained metrics, oldest first.
func (m *metricsRing) snapshot() []CallMetric {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]CallMetric, len(m.items))
	copy(out, m.items)
	return out
}

// --- GET /metrics ---

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	writeJSON(w, http.StatusOK, s.metrics.snapshot())
}
