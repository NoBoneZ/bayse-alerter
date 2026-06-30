package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/NoBoneZ/bayse-alerter/internal/rules"
	"net/http"

	"github.com/NoBoneZ/bayse-alerter/internal/bayse"
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
		if !market.HasOutcome(in.Outcome) {
			writeError(w, http.StatusUnprocessableEntity,
				fmt.Sprintf("rules[%d]: outcome %q not found in market", i, in.Outcome), nil)
			return
		}
		params, err := rules.ParseParams(rules.RuleType(in.Type), in.Params)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("rules[%d]: %v", i, err), nil)
			return
		}
		built = append(built, rules.Rule{
			EventSlug: event.Slug,
			EventID:   event.ID,
			MarketID:  in.MarketID,
			Outcome:   in.Outcome,
			Type:      rules.RuleType(in.Type),
			Params:    params,
			Enabled:   true,
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

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Ping(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "database unavailable", nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
