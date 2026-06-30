package rules

import (
	"time"

	"github.com/google/uuid"
)

type RuleType string

const (
	Threshold   RuleType = "threshold_cross"
	PercentMove RuleType = "percent_move"
)

type Direction string

const (
	Above Direction = "above"
	Below Direction = "below"
)

type Phase string

const (
	Armed     Phase = "ARMED"
	Triggered Phase = "TRIGGERED"
)

type Rule struct {
	ID        uuid.UUID
	EventSlug string
	EventID   string
	MarketID  string
	Outcome   string
	Type      RuleType
	Params    Params
	Enabled   bool
	CreatedAt time.Time
}

type Params struct {
	Direction Direction `json:"direction,omitempty"`
	Target    int64     `json:"target,omitempty"`

	PctBps        int64 `json:"pct_bps,omitempty"`
	WindowSeconds int   `json:"window_seconds,omitempty"`

	CooldownSeconds int `json:"cooldown_seconds,omitempty"`
}

type State struct {
	Phase       Phase
	LastFiredAt time.Time
}
