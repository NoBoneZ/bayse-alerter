package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/NoBoneZ/bayse-alerter/internal/rules"
)

type RuleWithState struct {
	Rule  rules.Rule
	State rules.State
}

func (s *Store) CreateRules(ctx context.Context, rs []rules.Rule) ([]uuid.UUID, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: begin: %w", err)
	}
	defer tx.Rollback(ctx) // no-op after Commit; unwinds on any early return

	ids := make([]uuid.UUID, 0, len(rs))
	for _, r := range rs {
		params, err := json.Marshal(r.Params)
		if err != nil {
			return nil, fmt.Errorf("store: marshal params: %w", err)
		}

		var id uuid.UUID
		err = tx.QueryRow(ctx, `
			INSERT INTO rules (event_slug, event_id, market_id, outcome, outcome_side, rule_type, params, enabled)
			VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8)
			RETURNING id`,
			r.EventSlug, r.EventID, r.MarketID, r.Outcome, r.OutcomeSide, string(r.Type), string(params), r.Enabled,
		).Scan(&id)
		if err != nil {
			return nil, fmt.Errorf("store: insert rule: %w", err)
		}

		if _, err := tx.Exec(ctx,
			`INSERT INTO rule_state (rule_id) VALUES ($1)`, id,
		); err != nil {
			return nil, fmt.Errorf("store: seed state: %w", err)
		}

		ids = append(ids, id)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("store: commit: %w", err)
	}
	return ids, nil
}

// EnabledRulesWithState returns the rules the poller acts on each tick.
func (s *Store) EnabledRulesWithState(ctx context.Context) ([]RuleWithState, error) {
	return s.rulesWhere(ctx, "WHERE r.enabled")
}

// ListRules returns every rule, enabled or not, newest first. It backs the
// read-only GET /rules endpoint so the service can be inspected without psql.
func (s *Store) ListRules(ctx context.Context) ([]RuleWithState, error) {
	return s.rulesWhere(ctx, "ORDER BY r.created_at DESC")
}

func (s *Store) rulesWhere(ctx context.Context, clause string) ([]RuleWithState, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT r.id, r.event_slug, r.event_id, r.market_id, r.outcome, r.outcome_side,
		       r.rule_type, r.params, r.enabled, r.created_at,
		       st.phase, st.last_fired_at
		FROM rules r
		JOIN rule_state st ON st.rule_id = r.id
		`+clause)
	if err != nil {
		return nil, fmt.Errorf("store: query rules: %w", err)
	}
	defer rows.Close()

	var out []RuleWithState
	for rows.Next() {
		var (
			r         rules.Rule
			ruleType  string
			params    []byte
			phase     string
			lastFired *time.Time
		)
		if err := rows.Scan(
			&r.ID, &r.EventSlug, &r.EventID, &r.MarketID, &r.Outcome, &r.OutcomeSide,
			&ruleType, &params, &r.Enabled, &r.CreatedAt,
			&phase, &lastFired,
		); err != nil {
			return nil, fmt.Errorf("store: scan rule: %w", err)
		}

		r.Type = rules.RuleType(ruleType)
		if err := json.Unmarshal(params, &r.Params); err != nil {
			return nil, fmt.Errorf("store: unmarshal params for %s: %w", r.ID, err)
		}

		state := rules.State{Phase: rules.Phase(phase)}
		if lastFired != nil {
			state.LastFiredAt = *lastFired
		}

		out = append(out, RuleWithState{Rule: r, State: state})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate rules: %w", err)
	}
	return out, nil
}
