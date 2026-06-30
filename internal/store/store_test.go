package store

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	// NOTE: replace this module path with the one in YOUR go.mod.
	"github.com/NoBoneZ/bayse-alerter/internal/rules"
)

// These are INTEGRATION tests: they need a real Postgres. They are skipped
// unless TEST_DATABASE_URL is set, e.g.:
//
//	TEST_DATABASE_URL=postgres://bayse:bayse@localhost:5432/bayse?sslmode=disable \
//	    go test ./internal/store/
//
// Each test starts from a clean schema (migrate + truncate), so re-runs are
// deterministic.
func testStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping store integration tests")
	}
	if err := Migrate(dsn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	st, err := New(context.Background(), dsn, slog.Default())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	// Clean slate so tests don't accumulate rows across runs.
	if _, err := st.pool.Exec(context.Background(),
		`TRUNCATE alerts, rule_state, rules RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	t.Cleanup(st.Close)
	return st
}

func sampleRule() rules.Rule {
	return rules.Rule{
		EventSlug: "e", EventID: "ev", MarketID: "m", Outcome: "YES",
		Type:    rules.Threshold,
		Params:  rules.Params{Direction: rules.Above, Target: 60},
		Enabled: true,
	}
}

// CreateRules should persist the rule, seed an ARMED state row, and that rule
// should come back from EnabledRulesWithState.
func TestCreateRules_PersistsAndLoads(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	ids, err := st.CreateRules(ctx, []rules.Rule{sampleRule()})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("got %d ids, want 1", len(ids))
	}

	loaded, err := st.EnabledRulesWithState(ctx)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("loaded %d rules, want 1", len(loaded))
	}
	got := loaded[0]
	if got.Rule.ID != ids[0] {
		t.Errorf("id = %s, want %s", got.Rule.ID, ids[0])
	}
	if got.State.Phase != rules.Armed {
		t.Errorf("phase = %s, want ARMED", got.State.Phase)
	}
	if got.Rule.Params.Target != 60 {
		t.Errorf("params round-trip failed: %+v", got.Rule.Params)
	}
}

// The headline persistence guarantee: 20 concurrent FireAlert calls on a single
// ARMED rule must produce exactly ONE alert and exactly ONE successful fire.
func TestFireAlert_FiresExactlyOnceUnderConcurrency(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	ids, err := st.CreateRules(ctx, []rules.Rule{sampleRule()})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	r := rules.Rule{ID: ids[0], MarketID: "m", Outcome: "YES"}
	obs := rules.Observation{Price: 65, At: time.Now()}
	dec := rules.Decision{Fire: true, TriggeredValue: 60}

	const n = 20
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		fired int
	)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := st.FireAlert(ctx, r, obs, dec)
			if err != nil {
				t.Errorf("fire: %v", err)
				return
			}
			if ok {
				mu.Lock()
				fired++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if fired != 1 {
		t.Fatalf("FireAlert reported %d successful fires, want exactly 1", fired)
	}

	var alertCount int
	if err := st.pool.QueryRow(ctx,
		`SELECT count(*) FROM alerts WHERE rule_id = $1`, ids[0],
	).Scan(&alertCount); err != nil {
		t.Fatalf("count alerts: %v", err)
	}
	if alertCount != 1 {
		t.Fatalf("alerts table has %d rows for the rule, want exactly 1", alertCount)
	}
}

// After a fire, Rearm flips the rule back to ARMED so the next crossing can fire.
func TestRearm_AllowsNextFire(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	ids, _ := st.CreateRules(ctx, []rules.Rule{sampleRule()})
	r := rules.Rule{ID: ids[0], MarketID: "m", Outcome: "YES"}
	obs := rules.Observation{Price: 65, At: time.Now()}
	dec := rules.Decision{Fire: true, TriggeredValue: 60}

	if ok, err := st.FireAlert(ctx, r, obs, dec); err != nil || !ok {
		t.Fatalf("first fire: ok=%v err=%v", ok, err)
	}
	// A second fire while still TRIGGERED must be a no-op.
	if ok, err := st.FireAlert(ctx, r, obs, dec); err != nil || ok {
		t.Fatalf("fire while triggered: ok=%v err=%v (want ok=false)", ok, err)
	}
	// Re-arm, then a fire should succeed again.
	if err := st.Rearm(ctx, ids[0]); err != nil {
		t.Fatalf("rearm: %v", err)
	}
	if ok, err := st.FireAlert(ctx, r, obs, dec); err != nil || !ok {
		t.Fatalf("fire after rearm: ok=%v err=%v (want ok=true)", ok, err)
	}

	var alertCount int
	st.pool.QueryRow(ctx, `SELECT count(*) FROM alerts WHERE rule_id=$1`, ids[0]).Scan(&alertCount)
	if alertCount != 2 {
		t.Fatalf("alerts = %d, want 2 (one per crossing)", alertCount)
	}
}
