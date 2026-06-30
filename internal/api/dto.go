package api

import "encoding/json"

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

type errorResponse struct {
	Error   string `json:"error"`
	Details any    `json:"details,omitempty"`
}
