package poller

import (
	"context"
	"github.com/NoBoneZ/bayse-alerter/internal/rules"
	"github.com/NoBoneZ/bayse-alerter/internal/store"
	"time"

	"github.com/google/uuid"
)

type Store interface {
	EnabledRulesWithState(ctx context.Context) ([]store.RuleWithState, error)
	FireAlert(ctx context.Context, r rules.Rule, obs rules.Observation, d rules.Decision) (bool, error)
	Rearm(ctx context.Context, ruleID uuid.UUID) error
}

type Prices interface {
	CurrentPrice(ctx context.Context, marketID, outcome string) (int64, error)
	ReferencePrice(ctx context.Context, eventID, marketID, outcome string, window time.Duration, now time.Time) (int64, error)
}
