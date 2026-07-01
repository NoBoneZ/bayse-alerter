package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/NoBoneZ/bayse-alerter/internal/bayse"
	"github.com/NoBoneZ/bayse-alerter/internal/rules"
)

const maxBodyBytes = 1 << 20 // 1 MiB

func (s *Server) handleCreateRules(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)

	var req createRulesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body", err.Error())
		return
	}
	if req.EventSlug == "" {
		writeError(w, http.StatusBadRequest, "event_slug is required", nil)
		return
	}
	if len(req.Rules) == 0 {
		writeError(w, http.StatusBadRequest, "at least one rule is required", nil)
		return
	}

	event, err := s.resolver.GetEventBySlug(ctx, req.EventSlug)
	switch {
	case errors.Is(err, bayse.ErrNotFound):
		writeError(w, http.StatusNotFound, "unknown event slug", req.EventSlug)
		return
	case err != nil:
		s.log.Error("resolve slug failed", "slug", req.EventSlug, "err", err)
		writeError(w, http.StatusBadGateway, "could not reach Bayse to validate slug", nil)
		return
	}

	built := make([]rules.Rule, 0, len(req.Rules))
	for i, in := range req.Rules {
		market, ok := event.Market(in.MarketID)
		if !ok {
			writeError(w, http.StatusUnprocessableEntity,
				fmt.Sprintf("rules[%d]: market_id %q not found in event", i, in.MarketID), nil)
			return
		}
		label, side, ok := market.ResolveOutcome(in.Outcome)
		if !ok {
			writeError(w, http.StatusUnprocessableEntity,
				fmt.Sprintf("rules[%d]: outcome %q not found in market (expected %q, %q, YES or NO)",
					i, in.Outcome, market.Outcome1Label, market.Outcome2Label), nil)
			return
		}
		params, err := rules.ParseParams(rules.RuleType(in.Type), in.Params)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("rules[%d]: %v", i, err), nil)
			return
		}
		built = append(built, rules.Rule{
			EventSlug:   event.Slug,
			EventID:     event.ID,
			MarketID:    in.MarketID,
			Outcome:     label,
			OutcomeSide: side,
			Type:        rules.RuleType(in.Type),
			Params:      params,
			Enabled:     true,
		})
	}

	ids, err := s.store.CreateRules(ctx, built)
	if err != nil {
		s.log.Error("create rules failed", "err", err)
		writeError(w, http.StatusInternalServerError, "could not persist rules", nil)
		return
	}

	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.String()
	}
	writeJSON(w, http.StatusCreated, createRulesResponse{RuleIDs: out})
}

func (s *Server) handleListRules(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListRules(r.Context())
	if err != nil {
		s.log.Error("list rules failed", "err", err)
		writeError(w, http.StatusInternalServerError, "could not load rules", nil)
		return
	}

	out := make([]ruleView, 0, len(items))
	for _, it := range items {
		v := ruleView{
			ID:        it.Rule.ID.String(),
			EventSlug: it.Rule.EventSlug,
			EventID:   it.Rule.EventID,
			MarketID:  it.Rule.MarketID,
			Outcome:   it.Rule.Outcome,
			Type:      string(it.Rule.Type),
			Params:    it.Rule.Params,
			Enabled:   it.Rule.Enabled,
			Phase:     string(it.State.Phase),
			CreatedAt: it.Rule.CreatedAt,
		}
		if !it.State.LastFiredAt.IsZero() {
			t := it.State.LastFiredAt
			v.LastFiredAt = &t
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, map[string]any{"rules": out})
}

// defaultAlertLimit caps an unbounded GET /alerts; maxAlertLimit stops a caller
// from asking for an arbitrarily large page.
const (
	defaultAlertLimit = 100
	maxAlertLimit     = 1000
)

func (s *Server) handleListAlerts(w http.ResponseWriter, r *http.Request) {
	limit := defaultAlertLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			writeError(w, http.StatusBadRequest, "limit must be a positive integer", raw)
			return
		}
		if n > maxAlertLimit {
			n = maxAlertLimit
		}
		limit = n
	}

	alerts, err := s.store.ListAlerts(r.Context(), limit)
	if err != nil {
		s.log.Error("list alerts failed", "err", err)
		writeError(w, http.StatusInternalServerError, "could not load alerts", nil)
		return
	}

	out := make([]alertView, 0, len(alerts))
	for _, a := range alerts {
		out = append(out, alertView{
			ID:             a.ID.String(),
			RuleID:         a.RuleID.String(),
			FireSeq:        a.FireSeq,
			MarketID:       a.MarketID,
			Outcome:        a.Outcome,
			ObservedPrice:  a.ObservedPrice,
			TriggeredValue: a.TriggeredValue,
			TriggeredAt:    a.TriggeredAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"alerts": out})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Ping(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "database unavailable", nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
