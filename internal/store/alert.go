package store

import (
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
