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

type fakeStore struct {
	mu       sync.Mutex
	items    []store.RuleWithState
	fires    int
	rearms   int
	disabled int
	phase    rules.Phase
}

func (f *fakeStore) EnabledRulesWithState(context.Context) ([]store.RuleWithState, error) {
	return f.items, nil
}

func (f *fakeStore) FireAlert(_ context.Context, _ rules.Rule, _ rules.Observation, _ rules.Decision) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.phase != rules.Armed {
		return false, nil
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

func (f *fakeStore) DisableRule(_ context.Context, _ uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.disabled++
	return nil
}

type fakePrices struct {
	mu       sync.Mutex
	prices   []int64
	i        int
	ref      int64
	resolved bool
}

func (f *fakePrices) CurrentPrice(context.Context, string, string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.i >= len(f.prices) {
		return f.prices[len(f.prices)-1], nil
	}
	p := f.prices[f.i]
	f.i++
	return p, nil
}

func (f *fakePrices) ReferencePrice(context.Context, string, string, string, time.Duration, time.Time) (int64, error) {
	return f.ref, nil
}

func (f *fakePrices) MarketResolved(context.Context, string, string) (bool, error) {
	return f.resolved, nil
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

func TestPoller_FiresOncePerCrossing(t *testing.T) {
	item := thresholdRule()
	st := &fakeStore{items: []store.RuleWithState{item}, phase: rules.Armed}
	prices := &fakePrices{prices: []int64{55, 58, 61, 63, 59, 62}}
	p := New(st, prices, time.Hour, quietLogger())

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

func TestPoller_NoDoubleFireWhileTrue(t *testing.T) {
	item := thresholdRule()
	st := &fakeStore{items: []store.RuleWithState{item}, phase: rules.Armed}
	prices := &fakePrices{prices: []int64{65, 66, 67, 68}}
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
	if st.disabled != 0 {
		t.Errorf("disabled = %d, want 0 (a transient price error must not retire a rule)", st.disabled)
	}
}

func TestPoller_DisablesRuleOnResolvedMarket(t *testing.T) {
	item := thresholdRule()
	st := &fakeStore{items: []store.RuleWithState{item}, phase: rules.Armed}
	prices := &resolvedPrices{}
	p := New(st, prices, time.Hour, quietLogger())

	if err := p.tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if st.disabled != 1 {
		t.Errorf("disabled = %d, want 1 (resolved market should retire the rule)", st.disabled)
	}
	if st.fires != 0 {
		t.Errorf("fires = %d, want 0", st.fires)
	}
}

type resolvedPrices struct{}

func (resolvedPrices) CurrentPrice(context.Context, string, string) (int64, error) {
	return 0, errors.New("market does not have an active orderbook")
}
func (resolvedPrices) ReferencePrice(context.Context, string, string, string, time.Duration, time.Time) (int64, error) {
	return 0, errors.New("market does not have an active orderbook")
}
func (resolvedPrices) MarketResolved(context.Context, string, string) (bool, error) {
	return true, nil
}

type erroringPrices struct{}

func (erroringPrices) CurrentPrice(context.Context, string, string) (int64, error) {
	return 0, errors.New("upstream down")
}
func (erroringPrices) ReferencePrice(context.Context, string, string, string, time.Duration, time.Time) (int64, error) {
	return 0, errors.New("upstream down")
}
func (erroringPrices) MarketResolved(context.Context, string, string) (bool, error) {
	return false, nil
}
