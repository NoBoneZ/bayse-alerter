package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Alert struct {
	ID             uuid.UUID
	RuleID         uuid.UUID
	FireSeq        int64
	MarketID       string
	Outcome        string
	ObservedPrice  int64
	TriggeredValue int64
	TriggeredAt    time.Time
}

// ListAlerts returns the most recent alerts, newest first, capped at limit.
func (s *Store) ListAlerts(ctx context.Context, limit int) ([]Alert, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, rule_id, fire_seq, market_id, outcome,
		       observed_price, triggered_value, triggered_at
		FROM alerts
		ORDER BY triggered_at DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("store: query alerts: %w", err)
	}
	defer rows.Close()

	var out []Alert
	for rows.Next() {
		var a Alert
		if err := rows.Scan(
			&a.ID, &a.RuleID, &a.FireSeq, &a.MarketID, &a.Outcome,
			&a.ObservedPrice, &a.TriggeredValue, &a.TriggeredAt,
		); err != nil {
			return nil, fmt.Errorf("store: scan alert: %w", err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate alerts: %w", err)
	}
	return out, nil
}
