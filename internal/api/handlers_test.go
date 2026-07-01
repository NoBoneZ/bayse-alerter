package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	// NOTE: replace this module path with the one in YOUR go.mod.
	"github.com/NoBoneZ/bayse-alerter/internal/bayse"
	"github.com/NoBoneZ/bayse-alerter/internal/rules"
)

// --- fakes -----------------------------------------------------------------

// fakeStore implements RuleStore. It records what it was asked to create and can
// be told to fail, so we can exercise both the happy path and the 500 path.
type fakeStore struct {
	created   []rules.Rule
	createErr error
	pingErr   error
}

func (f *fakeStore) CreateRules(_ context.Context, rs []rules.Rule) ([]uuid.UUID, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	f.created = append(f.created, rs...)
	ids := make([]uuid.UUID, len(rs))
	for i := range rs {
		ids[i] = uuid.New()
	}
	return ids, nil
}

func (f *fakeStore) Ping(context.Context) error { return f.pingErr }

// fakeResolver implements EventResolver. It returns a canned event, or a chosen
// error (e.g. bayse.ErrNotFound), without touching the network.
type fakeResolver struct {
	event bayse.Event
	err   error
}

func (f *fakeResolver) GetEventBySlug(context.Context, string) (bayse.Event, error) {
	return f.event, f.err
}

// sampleEvent is a minimal valid event: one market with YES/NO outcomes.
func sampleEvent() bayse.Event {
	return bayse.Event{
		ID:   "ev1",
		Slug: "my-slug",
		Markets: []bayse.Market{{
			ID:            "m1",
			Outcome1ID:    "o1",
			Outcome1Label: "YES",
			Outcome2ID:    "o2",
			Outcome2Label: "NO",
		}},
	}
}

// newTestServer wires a Server with the given fakes and returns it.
func newTestServer(store RuleStore, resolver EventResolver) *Server {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewServer(store, resolver, log)
}

// do sends a POST /rules with the given JSON body and returns the recorder.
func do(t *testing.T, srv *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/rules", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	return rec
}

// --- tests -----------------------------------------------------------------

func TestCreateRules_Success(t *testing.T) {
	store := &fakeStore{}
	srv := newTestServer(store, &fakeResolver{event: sampleEvent()})

	rec := do(t, srv, `{
		"event_slug":"my-slug",
		"rules":[{"market_id":"m1","outcome":"YES","type":"threshold_cross",
		          "params":{"direction":"above","target":60}}]
	}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}

	var resp createRulesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.RuleIDs) != 1 {
		t.Fatalf("got %d rule ids, want 1", len(resp.RuleIDs))
	}

	// The rule reached the store, stamped with the event id and ARMED-by-default fields.
	if len(store.created) != 1 {
		t.Fatalf("store received %d rules, want 1", len(store.created))
	}
	got := store.created[0]
	if got.EventID != "ev1" || !got.Enabled || got.Type != rules.Threshold {
		t.Errorf("persisted rule = %+v", got)
	}
}

func TestCreateRules_MalformedJSON(t *testing.T) {
	srv := newTestServer(&fakeStore{}, &fakeResolver{event: sampleEvent()})
	rec := do(t, srv, `{not valid json`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestCreateRules_UnknownSlug(t *testing.T) {
	srv := newTestServer(&fakeStore{}, &fakeResolver{err: bayse.ErrNotFound})
	rec := do(t, srv, `{"event_slug":"nope","rules":[
		{"market_id":"m1","outcome":"YES","type":"threshold_cross",
		 "params":{"direction":"above","target":60}}]}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestCreateRules_UpstreamDown(t *testing.T) {
	srv := newTestServer(&fakeStore{}, &fakeResolver{err: io.ErrUnexpectedEOF})
	rec := do(t, srv, `{"event_slug":"x","rules":[
		{"market_id":"m1","outcome":"YES","type":"threshold_cross",
		 "params":{"direction":"above","target":60}}]}`)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
}

func TestCreateRules_UnknownMarket(t *testing.T) {
	srv := newTestServer(&fakeStore{}, &fakeResolver{event: sampleEvent()})
	rec := do(t, srv, `{"event_slug":"my-slug","rules":[
		{"market_id":"does-not-exist","outcome":"YES","type":"threshold_cross",
		 "params":{"direction":"above","target":60}}]}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
}

func TestCreateRules_UnknownOutcome(t *testing.T) {
	srv := newTestServer(&fakeStore{}, &fakeResolver{event: sampleEvent()})
	rec := do(t, srv, `{"event_slug":"my-slug","rules":[
		{"market_id":"m1","outcome":"MAYBE","type":"threshold_cross",
		 "params":{"direction":"above","target":60}}]}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
}

func TestCreateRules_InvalidParams(t *testing.T) {
	srv := newTestServer(&fakeStore{}, &fakeResolver{event: sampleEvent()})
	// threshold rule missing a direction -> params validation fails -> 400
	rec := do(t, srv, `{"event_slug":"my-slug","rules":[
		{"market_id":"m1","outcome":"YES","type":"threshold_cross",
		 "params":{"target":60}}]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestCreateRules_EmptyRules(t *testing.T) {
	srv := newTestServer(&fakeStore{}, &fakeResolver{event: sampleEvent()})
	rec := do(t, srv, `{"event_slug":"my-slug","rules":[]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestCreateRules_StoreFailure(t *testing.T) {
	srv := newTestServer(&fakeStore{createErr: io.ErrClosedPipe}, &fakeResolver{event: sampleEvent()})
	rec := do(t, srv, `{"event_slug":"my-slug","rules":[
		{"market_id":"m1","outcome":"YES","type":"threshold_cross",
		 "params":{"direction":"above","target":60}}]}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestHealth(t *testing.T) {
	srv := newTestServer(&fakeStore{}, &fakeResolver{})
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	// And a 503 when the database is unreachable.
	down := newTestServer(&fakeStore{pingErr: io.ErrClosedPipe}, &fakeResolver{})
	rec2 := httptest.NewRecorder()
	down.Routes().ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec2.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec2.Code)
	}
}

func TestCreateRules_WrongMethod(t *testing.T) {
	srv := newTestServer(&fakeStore{}, &fakeResolver{event: sampleEvent()})
	req := httptest.NewRequest(http.MethodGet, "/rules", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	// Go 1.22 method routing returns 405 for the wrong verb on a known path.
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}
