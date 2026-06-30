package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/NoBoneZ/bayse-alerter/internal/rules"
)

func (s *Store) FireAlert(ctx context.Context, r rules.Rule, obs rules.Observation, d rules.Decision) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("store: begin: %w", err)
	}
	defer tx.Rollback(ctx)

	var fireSeq int64
	err = tx.QueryRow(ctx, `
		UPDATE rule_state
		   SET phase         = 'TRIGGERED',
		       fire_seq      = fire_seq + 1,
		       last_price    = $2,
		       last_fired_at = $3,
		       updated_at    = now()
		 WHERE rule_id = $1 AND phase = 'ARMED'
		RETURNING fire_seq`,
		r.ID, obs.Price, obs.At,
	).Scan(&fireSeq)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("store: claim transition: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO alerts
			(rule_id, fire_seq, market_id, outcome, observed_price, triggered_value)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		r.ID, fireSeq, r.MarketID, r.Outcome, obs.Price, d.TriggeredValue,
	); err != nil {
		return false, fmt.Errorf("store: insert alert: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("store: commit: %w", err)
	}
	return true, nil
}

func (s *Store) Rearm(ctx context.Context, ruleID uuid.UUID) error {
	if _, err := s.pool.Exec(ctx, `
		UPDATE rule_state
		   SET phase = 'ARMED', updated_at = now()
		 WHERE rule_id = $1 AND phase = 'TRIGGERED'`,
		ruleID,
	); err != nil {
		return fmt.Errorf("store: rearm: %w", err)
	}
	return nil
}
