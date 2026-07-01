package bayse

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func testClient(t *testing.T, baseURL string, opts ...Option) *Client {
	t.Helper()
	base := []Option{
		WithBaseURL(baseURL),
		WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
		WithRetry(3, time.Millisecond),
	}
	return New("pk_test_key", append(base, opts...)...)
}

func TestGetEventBySlug_SetsAuthHeaderAndDecodes(t *testing.T) {
	var gotKey, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-Public-Key")
		gotPath = r.URL.Path
		_, _ = io.WriteString(w, `{
			"id":"ev1","slug":"my-slug","title":"T","status":"open",
			"markets":[{"id":"m1","title":"M","status":"open",
				"outcome1Id":"o1","outcome1Label":"YES","outcome1Price":0.55,
				"outcome2Id":"o2","outcome2Label":"NO","outcome2Price":0.45}]
		}`)
	}))
	defer srv.Close()

	c := testClient(t, srv.URL)
	ev, err := c.GetEventBySlug(context.Background(), "my-slug")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotKey != "pk_test_key" {
		t.Errorf("X-Public-Key header = %q, want pk_test_key", gotKey)
	}
	if gotPath != "/events/slug/my-slug" {
		t.Errorf("request path = %q, want /events/slug/my-slug", gotPath)
	}
	if ev.ID != "ev1" || len(ev.Markets) != 1 {
		t.Fatalf("decoded event = %+v", ev)
	}

	m, ok := ev.Market("m1")
	if !ok {
		t.Fatal("Market(m1) not found")
	}
	if id, ok := m.OutcomeID("YES"); !ok || id != "o1" {
		t.Errorf("OutcomeID(YES) = %q, %v; want o1, true", id, ok)
	}
	if m.HasOutcome("MAYBE") {
		t.Error("HasOutcome(MAYBE) = true, want false")
	}
}

func TestGetEventBySlug_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":"not_found","message":"Event not found","statusCode":404}`)
	}))
	defer srv.Close()

	c := testClient(t, srv.URL)
	_, err := c.GetEventBySlug(context.Background(), "nope")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestCurrentPrice_ConvertsDecimalToCents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"marketId":"m1","outcome":"YES","lastPrice":0.72,"midPrice":0.71}`)
	}))
	defer srv.Close()

	c := testClient(t, srv.URL)
	cents, err := c.CurrentPrice(context.Background(), "m1", "YES")
	if err != nil {
		t.Fatal(err)
	}
	if cents != 72 {
		t.Errorf("CurrentPrice = %d cents, want 72 (from 0.72)", cents)
	}
}

func TestCurrentPrice_FallsBackToMidWhenNoLastTrade(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"marketId":"m1","outcome":"YES","lastPrice":0,"midPrice":0.71}`)
	}))
	defer srv.Close()

	c := testClient(t, srv.URL)
	cents, err := c.CurrentPrice(context.Background(), "m1", "YES")
	if err != nil {
		t.Fatal(err)
	}
	if cents != 71 {
		t.Errorf("CurrentPrice = %d cents, want 71 (midpoint fallback)", cents)
	}
}

func TestGet_RetriesTransientThenSucceeds(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, `{"marketId":"m1","outcome":"YES","lastPrice":0.50}`)
	}))
	defer srv.Close()

	c := testClient(t, srv.URL)
	cents, err := c.CurrentPrice(context.Background(), "m1", "YES")
	if err != nil {
		t.Fatalf("expected success after retries, got %v", err)
	}
	if cents != 50 {
		t.Errorf("cents = %d, want 50", cents)
	}
	if n := atomic.LoadInt32(&calls); n != 3 {
		t.Errorf("server called %d times, want 3 (two failures then success)", n)
	}
}

func TestGet_DoesNotRetryNotFound(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := testClient(t, srv.URL)
	_, err := c.GetEventBySlug(context.Background(), "x")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Errorf("server called %d times, want 1 (404 must not retry)", n)
	}
}

func TestGet_ExhaustsRetriesOnPersistentTransient(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	c := testClient(t, srv.URL)
	_, err := c.GetEventBySlug(context.Background(), "x")
	if err == nil {
		t.Fatal("expected an error after exhausting retries")
	}
	if errors.Is(err, ErrNotFound) {
		t.Fatal("a 502 must not surface as ErrNotFound")
	}
	if n := atomic.LoadInt32(&calls); n != 3 {
		t.Errorf("server called %d times, want 3 (all attempts used)", n)
	}
}

func TestReferencePrice_PicksSampleAtOrBeforeWindowStart(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"m1":[
			{"outcome":"YES","price":0.40,"timestamp":"2026-01-01T11:00:00Z"},
			{"outcome":"YES","price":0.50,"timestamp":"2026-01-01T11:30:00Z"},
			{"outcome":"YES","price":0.60,"timestamp":"2026-01-01T12:00:00Z"}
		]}`)
	}))
	defer srv.Close()

	c := testClient(t, srv.URL)
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	cents, err := c.ReferencePrice(context.Background(), "ev1", "m1", "YES", 45*time.Minute, now)
	if err != nil {
		t.Fatal(err)
	}
	if cents != 40 {
		t.Errorf("ReferencePrice = %d cents, want 40", cents)
	}
}

func TestResolveOutcome_LabelAndCanonicalSide(t *testing.T) {
	m := Market{
		Outcome1ID: "o1", Outcome1Label: "Up",
		Outcome2ID: "o2", Outcome2Label: "Down",
	}
	cases := []struct {
		input     string
		wantLabel string
		wantSide  string
		wantOK    bool
	}{
		{"Up", "Up", "YES", true},
		{"Down", "Down", "NO", true},
		{"YES", "Up", "YES", true},
		{"no", "Down", "NO", true},
		{"Sideways", "", "", false},
	}
	for _, c := range cases {
		label, side, ok := m.ResolveOutcome(c.input)
		if ok != c.wantOK || label != c.wantLabel || side != c.wantSide {
			t.Errorf("ResolveOutcome(%q) = (%q, %q, %v), want (%q, %q, %v)",
				c.input, label, side, ok, c.wantLabel, c.wantSide, c.wantOK)
		}
	}
}
