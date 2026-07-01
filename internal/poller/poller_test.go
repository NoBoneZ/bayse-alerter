package poller

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NoBoneZ/bayse-alerter/internal/rules"
	"github.com/NoBoneZ/bayse-alerter/internal/store"
)

// --- fakes -----------------------------------------------------------------

// fakeStore implements the poller's Store interface. It serves a fixed set of
// rules and records every FireAlert / Rearm call. FireAlert enforces the same
// ARMED-only rule the real store does, so the fake can't fire a triggered rule.
type fakeStore struct {
	mu     sync.Mutex
	items  []store.RuleWithState
	fires  int
	rearms int
	phase  rules.Phase // tracks the single rule's phase across calls
}

func (f *fakeStore) EnabledRulesWithState(context.Context) ([]store.RuleWithState, error) {
	return f.items, nil
}

func (f *fakeStore) FireAlert(_ context.Context, _ rules.Rule, _ rules.Observation, _ rules.Decision) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.phase != rules.Armed {
		return false, nil // not armed -> no-op, mirrors the real conditional update
	}
	f.phase = rules.Triggered
	f.fires++
	return true, nil
}

func (f *fakeStore) Rearm(_ context.Context, _ uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.phase = rules.Armed
	f.rearms++
	return nil
}

// fakePrices implements the poller's Prices interface, returning a scripted
// sequence of prices — one per call — so we can drive a rule across crossings.
type fakePrices struct {
	mu     sync.Mutex
	prices []int64
	i      int
	ref    int64
}

func (f *fakePrices) CurrentPrice(context.Context, string, string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.i >= len(f.prices) {
		return f.prices[len(f.prices)-1], nil // hold last value if over-polled
	}
	p := f.prices[f.i]
	f.i++
	return p, nil
}

func (f *fakePrices) ReferencePrice(context.Context, string, string, string, time.Duration, time.Time) (int64, error) {
	return f.ref, nil
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func thresholdRule() store.RuleWithState {
	return store.RuleWithState{
		Rule: rules.Rule{
			ID:       uuid.New(),
			MarketID: "m1",
			Outcome:  "YES",
			Type:     rules.Threshold,
			Params:   rules.Params{Direction: rules.Above, Target: 60},
		},
		State: rules.State{Phase: rules.Armed},
	}
}

// --- tests -----------------------------------------------------------------

// The headline test, through the orchestration: driving the canonical price
// sequence one tick at a time, the rule must fire exactly twice — once per
// crossing — and re-arm once on the dip.
func TestPoller_FiresOncePerCrossing(t *testing.T) {
	item := thresholdRule()
	st := &fakeStore{items: []store.RuleWithState{item}, phase: rules.Armed}
	prices := &fakePrices{prices: []int64{55, 58, 61, 63, 59, 62}}
	//                                       -    -    fire -    rearm fire
	p := New(st, prices, time.Hour, quietLogger())

	// Run one tick per price. We must reload the rule's phase from the fake each
	// tick, exactly as the real poller reloads state from the database.
	ctx := context.Background()
	for range prices.prices {
		st.items[0].State.Phase = st.phase
		if err := p.tick(ctx); err != nil {
			t.Fatalf("tick: %v", err)
		}
	}

	if st.fires != 2 {
		t.Errorf("fires = %d, want 2 (one per crossing)", st.fires)
	}
	if st.rearms != 1 {
		t.Errorf("rearms = %d, want 1 (on the dip to 59)", st.rearms)
	}
}

// While the condition stays true, the rule must fire once and never again.
func TestPoller_NoDoubleFireWhileTrue(t *testing.T) {
	item := thresholdRule()
	st := &fakeStore{items: []store.RuleWithState{item}, phase: rules.Armed}
	prices := &fakePrices{prices: []int64{65, 66, 67, 68}} // all above, never dips
	p := New(st, prices, time.Hour, quietLogger())

	ctx := context.Background()
	for range prices.prices {
		st.items[0].State.Phase = st.phase
		if err := p.tick(ctx); err != nil {
			t.Fatalf("tick: %v", err)
		}
	}

	if st.fires != 1 {
		t.Errorf("fires = %d, want exactly 1", st.fires)
	}
}

// A failed price fetch must skip the rule for that tick without firing, and must
// not abort the loop.
func TestPoller_PriceFetchErrorSkipsWithoutFiring(t *testing.T) {
	item := thresholdRule()
	st := &fakeStore{items: []store.RuleWithState{item}, phase: rules.Armed}
	p := New(st, &erroringPrices{}, time.Hour, quietLogger())

	if err := p.tick(context.Background()); err != nil {
		t.Fatalf("tick should swallow per-rule errors, got %v", err)
	}
	if st.fires != 0 {
		t.Errorf("fires = %d, want 0 (never evaluate on a failed fetch)", st.fires)
	}
}

type erroringPrices struct{}

func (erroringPrices) CurrentPrice(context.Context, string, string) (int64, error) {
	return 0, errors.New("upstream down")
}
func (erroringPrices) ReferencePrice(context.Context, string, string, string, time.Duration, time.Time) (int64, error) {
	return 0, errors.New("upstream down")
}
