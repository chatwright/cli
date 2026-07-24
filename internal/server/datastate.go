// datastate.go implements POST /datastate/query: the database side-effect
// verification seam. It evaluates a request against
// chatwright.dev/runtime/datastate's own Runner/Expectation machinery
// (spec/features/chatwright/deterministic-testing/data-state-assertions in
// the chatwright/chatwright repository) — this file never reimplements row
// matching or evidence bounding itself.
//
// What is genuinely wired: the full datastate.Runner/Assertion/Expectation
// pipeline (canonical ordering, field exclusion, redaction, bounded
// preview) driving a small JSON expectation DSL that maps 1:1 onto
// datastate's own combinators (NonEmpty, Empty, ExactRowCount, MinRowCount,
// MaxRowCount, RowContains, ExactRows, and All for conjunction).
//
// What is stubbed: real DTQL parsing/execution against a live
// dalgo/DataTug database. datastate.Executor is the seam DALgo's real
// dtql/dal.DB implementation is meant to satisfy (see
// chatwright.dev/runtime/datastate's own package doc comment); that
// integration does not exist yet in this CLI. FixtureStore is a
// clearly-labeled, in-memory stand-in: it treats a Query's DTQL text as an
// opaque, exact-match lookup key into rows an operator pre-loads from a
// JSON file (see LoadFixturesFile) — it does not parse or interpret DTQL
// syntax at all. When no fixtures file is configured, POST /datastate/query
// always answers verdict "unsupported": this server never claims a query
// passed against a database it never actually reached.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"chatwright.dev/runtime/datastate"
)

// --- request/response contract ---

// datastateQueryRequest is POST /datastate/query's request body.
type datastateQueryRequest struct {
	// Holder names the registered database holder to query. Optional when
	// the configured FixtureStore has exactly one holder.
	Holder string `json:"holder,omitempty"`
	// Query is the DTQL query text. Required.
	Query string `json:"query"`
	// Params are named query parameters, carried alongside Query and
	// echoed back in evidence but not interpreted by FixtureStore (see
	// this file's package doc comment).
	Params map[string]any `json:"params,omitempty"`
	// Expectations are ANDed together (all must pass) via datastate.All.
	// An empty list always passes and simply returns the queried rows as
	// evidence — the same "nil Expect always passes" behavior
	// datastate.Assertion documents.
	Expectations []expectationSpec `json:"expectations,omitempty"`
}

// expectationSpec is one JSON-encodable expectation, mapping 1:1 onto a
// chatwright.dev/runtime/datastate combinator. See buildExpectation for the
// exact mapping and the set of supported "type" values.
type expectationSpec struct {
	Type  string            `json:"type"`
	Count int               `json:"count,omitempty"`
	Row   map[string]any    `json:"row,omitempty"`
	Rows  []map[string]any  `json:"rows,omitempty"`
	Of    []expectationSpec `json:"of,omitempty"`
}

// datastateQueryResponse is POST /datastate/query's response body.
type datastateQueryResponse struct {
	// Verdict is "pass", "fail" or "unsupported" — see this file's package
	// doc comment for exactly when each applies.
	Verdict string `json:"verdict"`
	// Rows is the bounded, redacted, normalized row preview datastate.Evidence
	// produced (datastate.Evidence.Preview) — nil when Verdict is
	// "unsupported", since no query ever actually ran.
	Rows []datastate.Row `json:"rows"`
	// Detail is a human-readable explanation: empty on a clean pass, the
	// evidence's failure message on "fail", and a fixed explanation of what
	// to configure on "unsupported".
	Detail string `json:"detail"`
}

const unsupportedDetail = "no datastate store is configured: real dalgo/DTQL execution against a live database is not wired in this version. Configure --datastate-fixtures/CHATWRIGHT_SERVER_DATASTATE_FIXTURES with a JSON fixture file to evaluate assertions against in-memory canned rows, or wire a real datastate.Executor (see this package's FixtureStore doc comment)."

func (s *Server) handleDatastateQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}

	var req datastateQueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "decoding request body: "+err.Error())
		return
	}
	if strings.TrimSpace(req.Query) == "" {
		writeJSONError(w, http.StatusBadRequest, "query is required")
		return
	}
	expectation, err := buildExpectation(req.Expectations)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	if s.store == nil {
		writeJSON(w, http.StatusOK, datastateQueryResponse{
			Verdict: "unsupported",
			Rows:    nil,
			Detail:  unsupportedDetail,
		})
		return
	}

	evidence, _ := s.runner.Run(r.Context(), datastate.AttachmentAfterMessage, datastate.Assertion{
		Name:   "datastate-query",
		Holder: req.Holder,
		Query:  datastate.Query{DTQL: req.Query, Params: req.Params},
		Expect: expectation,
	})

	verdict := "pass"
	if evidence.Outcome == datastate.OutcomeFailed {
		verdict = "fail"
	}
	writeJSON(w, http.StatusOK, datastateQueryResponse{
		Verdict: verdict,
		Rows:    evidence.Preview,
		Detail:  evidence.FailureMessage,
	})
}

// buildExpectation combines specs into a single datastate.Expectation via
// datastate.All. An empty specs list returns a nil Expectation (always
// passes), matching datastate.Assertion.Expect's own documented zero value.
func buildExpectation(specs []expectationSpec) (datastate.Expectation, error) {
	if len(specs) == 0 {
		return nil, nil
	}
	built := make([]datastate.Expectation, 0, len(specs))
	for i, spec := range specs {
		one, err := buildOneExpectation(spec)
		if err != nil {
			return nil, fmt.Errorf("expectations[%d]: %w", i, err)
		}
		built = append(built, one)
	}
	return datastate.All(built...), nil
}

func buildOneExpectation(spec expectationSpec) (datastate.Expectation, error) {
	switch spec.Type {
	case "nonEmpty":
		return datastate.NonEmpty(), nil
	case "empty":
		return datastate.Empty(), nil
	case "exactRowCount":
		return datastate.ExactRowCount(spec.Count), nil
	case "minRowCount":
		return datastate.MinRowCount(spec.Count), nil
	case "maxRowCount":
		return datastate.MaxRowCount(spec.Count), nil
	case "rowContains":
		return datastate.RowContains(datastate.Row(spec.Row)), nil
	case "exactRows":
		rows := make([]datastate.Row, len(spec.Rows))
		for i, row := range spec.Rows {
			rows[i] = datastate.Row(row)
		}
		return datastate.ExactRows(rows), nil
	case "all":
		return buildExpectation(spec.Of)
	default:
		return nil, fmt.Errorf("unknown expectation type %q (want one of: nonEmpty, empty, exactRowCount, minRowCount, maxRowCount, rowContains, exactRows, all)", spec.Type)
	}
}

// --- FixtureStore: the clearly-labeled in-memory stand-in ---

// FixtureStore is an in-memory, exact-match stand-in for a real
// datastate.Executor. It is NOT a DTQL engine — see this file's package doc
// comment. Query.DTQL text is trimmed and used as a literal lookup key into
// canned rows an operator pre-loads per holder.
type FixtureStore struct {
	data map[string]map[string][]datastate.Row // holder -> trimmed DTQL text -> rows
}

// NewFixtureStore builds a FixtureStore directly from in-memory data,
// mainly for tests; LoadFixturesFile is the operator-facing constructor.
func NewFixtureStore(data map[string]map[string][]datastate.Row) *FixtureStore {
	return &FixtureStore{data: data}
}

// LoadFixturesFile reads a JSON file shaped as
// {"<holder>": {"<exact DTQL text>": [{...row...}, ...]}} into a
// FixtureStore.
func LoadFixturesFile(path string) (*FixtureStore, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("datastate fixtures: reading %s: %w", path, err)
	}
	var decoded map[string]map[string][]map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("datastate fixtures: parsing %s: %w", path, err)
	}
	data := make(map[string]map[string][]datastate.Row, len(decoded))
	for holder, queries := range decoded {
		byQuery := make(map[string][]datastate.Row, len(queries))
		for query, rows := range queries {
			converted := make([]datastate.Row, len(rows))
			for i, row := range rows {
				converted[i] = datastate.Row(row)
			}
			byQuery[strings.TrimSpace(query)] = converted
		}
		data[holder] = byQuery
	}
	return &FixtureStore{data: data}, nil
}

// Handles returns the datastate.Handles this store backs: one entry per
// configured holder, whose handle value is that holder's own query map —
// exactly what Execute expects to receive back via Runner's holder
// resolution.
func (f *FixtureStore) Handles() datastate.Handles {
	handles := make(datastate.Handles, len(f.data))
	for holder, queries := range f.data {
		handles[holder] = queries
	}
	return handles
}

// Execute implements datastate.Executor: an exact, trimmed-text lookup into
// the holder's pre-loaded query map. A query text with no matching fixture
// is a query execution error (matching datastate's own "must fail
// explicitly for unsupported query" contract), never a silent empty result.
func (f *FixtureStore) Execute(_ context.Context, handle any, query datastate.Query) ([]datastate.Row, error) {
	byQuery, ok := handle.(map[string][]datastate.Row)
	if !ok {
		return nil, fmt.Errorf("fixture store: unexpected handle type %T", handle)
	}
	rows, ok := byQuery[strings.TrimSpace(query.DTQL)]
	if !ok {
		return nil, fmt.Errorf("fixture store: no fixture recorded for query %q", query.DTQL)
	}
	return rows, nil
}

// datastateRunner is the narrow subset of *datastate.Runner this server
// depends on, so a fake can stand in for it in tests without needing a real
// FixtureStore.
type datastateRunner interface {
	Run(ctx context.Context, point datastate.AttachmentPoint, assertion datastate.Assertion) (datastate.Evidence, error)
}

func newFixtureRunner(store *FixtureStore) (*datastate.Runner, error) {
	return datastate.NewRunner(store, store.Handles(), datastate.Limits{})
}
