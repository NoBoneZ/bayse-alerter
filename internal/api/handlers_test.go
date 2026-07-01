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

	"github.com/NoBoneZ/bayse-alerter/internal/bayse"
	"github.com/NoBoneZ/bayse-alerter/internal/rules"
	"github.com/NoBoneZ/bayse-alerter/internal/store"
)

type fakeStore struct {
	created   []rules.Rule
	createErr error
	pingErr   error
	listRules []store.RuleWithState
	listAlrts []store.Alert
	listErr   error
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

func (f *fakeStore) ListRules(context.Context) ([]store.RuleWithState, error) {
	return f.listRules, f.listErr
}

func (f *fakeStore) ListAlerts(_ context.Context, limit int) ([]store.Alert, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	if limit < len(f.listAlrts) {
		return f.listAlrts[:limit], nil
	}
	return f.listAlrts, nil
}

func (f *fakeStore) Ping(context.Context) error { return f.pingErr }

type fakeResolver struct {
	event bayse.Event
	err   error
}

func (f *fakeResolver) GetEventBySlug(context.Context, string) (bayse.Event, error) {
	return f.event, f.err
}

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

func newTestServer(store RuleStore, resolver EventResolver) *Server {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewServer(store, resolver, log)
}

func do(t *testing.T, srv *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/rules", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	return rec
}

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

	down := newTestServer(&fakeStore{pingErr: io.ErrClosedPipe}, &fakeResolver{})
	rec2 := httptest.NewRecorder()
	down.Routes().ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec2.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec2.Code)
	}
}

func TestCreateRules_WrongMethod(t *testing.T) {
	srv := newTestServer(&fakeStore{}, &fakeResolver{event: sampleEvent()})
	req := httptest.NewRequest(http.MethodDelete, "/rules", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestListRules(t *testing.T) {
	st := &fakeStore{listRules: []store.RuleWithState{{
		Rule: rules.Rule{
			ID: uuid.New(), EventSlug: "my-slug", MarketID: "m1", Outcome: "YES",
			Type: rules.Threshold, Params: rules.Params{Direction: rules.Above, Target: 60},
			Enabled: true,
		},
		State: rules.State{Phase: rules.Armed},
	}}}
	srv := newTestServer(st, &fakeResolver{})

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/rules", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Rules []ruleView `json:"rules"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Rules) != 1 || body.Rules[0].Phase != "ARMED" || body.Rules[0].LastFiredAt != nil {
		t.Fatalf("unexpected rules payload: %+v", body.Rules)
	}
}

func TestListAlerts_RespectsLimit(t *testing.T) {
	st := &fakeStore{listAlrts: []store.Alert{
		{ID: uuid.New(), RuleID: uuid.New(), FireSeq: 1},
		{ID: uuid.New(), RuleID: uuid.New(), FireSeq: 2},
	}}
	srv := newTestServer(st, &fakeResolver{})

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/alerts?limit=1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Alerts []alertView `json:"alerts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Alerts) != 1 {
		t.Fatalf("got %d alerts, want 1 (limit honored)", len(body.Alerts))
	}
}

func TestListAlerts_BadLimit(t *testing.T) {
	srv := newTestServer(&fakeStore{}, &fakeResolver{})
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/alerts?limit=nope", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
