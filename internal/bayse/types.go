package bayse

import (
	"math"
	"time"
)

type Event struct {
	ID      string   `json:"id"`
	Slug    string   `json:"slug"`
	Title   string   `json:"title"`
	Status  string   `json:"status"`
	Markets []Market `json:"markets"`
}

type Market struct {
	ID            string  `json:"id"`
	Title         string  `json:"title"`
	Status        string  `json:"status"`
	Outcome1ID    string  `json:"outcome1Id"`
	Outcome1Label string  `json:"outcome1Label"`
	Outcome1Price float64 `json:"outcome1Price"`
	Outcome2ID    string  `json:"outcome2Id"`
	Outcome2Label string  `json:"outcome2Label"`
	Outcome2Price float64 `json:"outcome2Price"`
}

type Ticker struct {
	MarketID  string  `json:"marketId"`
	Outcome   string  `json:"outcome"`
	LastPrice float64 `json:"lastPrice"`
	BestBid   float64 `json:"bestBid"`
	BestAsk   float64 `json:"bestAsk"`
	MidPrice  float64 `json:"midPrice"`
}

type OrderBook struct {
	MarketID        string       `json:"marketId"`
	OutcomeID       string       `json:"outcomeId"`
	Bids            []OrderLevel `json:"bids"`
	Asks            []OrderLevel `json:"asks"`
	LastTradedPrice float64      `json:"lastTradedPrice"`
}

type OrderLevel struct {
	Price    float64 `json:"price"`
	Quantity float64 `json:"quantity"`
	Total    float64 `json:"total"`
}

type PricePoint struct {
	Outcome   string    `json:"outcome"`
	Price     float64   `json:"price"`
	Timestamp time.Time `json:"timestamp"`
}

func (e Event) Market(marketID string) (Market, bool) {
	for _, m := range e.Markets {
		if m.ID == marketID {
			return m, true
		}
	}
	return Market{}, false
}

func (m Market) OutcomeID(label string) (string, bool) {
	switch label {
	case m.Outcome1Label:
		return m.Outcome1ID, true
	case m.Outcome2Label:
		return m.Outcome2ID, true
	default:
		return "", false
	}
}

func (m Market) HasOutcome(label string) bool {
	_, ok := m.OutcomeID(label)
	return ok
}

func toCents(price float64) int64 {
	return int64(math.Round(price * 100))
}
