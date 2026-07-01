package api

import (
	"encoding/json"
	"time"

	"github.com/NoBoneZ/bayse-alerter/internal/rules"
)

type createRulesRequest struct {
	EventSlug string      `json:"event_slug"`
	Rules     []ruleInput `json:"rules"`
}

type ruleInput struct {
	MarketID string          `json:"market_id"`
	Outcome  string          `json:"outcome"`
	Type     string          `json:"type"`
	Params   json.RawMessage `json:"params"`
}

type createRulesResponse struct {
	RuleIDs []string `json:"rule_ids"`
}

type ruleView struct {
	ID          string       `json:"id"`
	EventSlug   string       `json:"event_slug"`
	EventID     string       `json:"event_id"`
	MarketID    string       `json:"market_id"`
	Outcome     string       `json:"outcome"`
	Type        string       `json:"type"`
	Params      rules.Params `json:"params"`
	Enabled     bool         `json:"enabled"`
	Phase       string       `json:"phase"`
	LastFiredAt *time.Time   `json:"last_fired_at,omitempty"`
	CreatedAt   time.Time    `json:"created_at"`
}

type alertView struct {
	ID             string    `json:"id"`
	RuleID         string    `json:"rule_id"`
	FireSeq        int64     `json:"fire_seq"`
	MarketID       string    `json:"market_id"`
	Outcome        string    `json:"outcome"`
	ObservedPrice  int64     `json:"observed_price"`
	TriggeredValue int64     `json:"triggered_value"`
	TriggeredAt    time.Time `json:"triggered_at"`
}

type errorResponse struct {
	Error   string `json:"error"`
	Details any    `json:"details,omitempty"`
}
