package api

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/NoBoneZ/bayse-alerter/internal/bayse"
	"github.com/NoBoneZ/bayse-alerter/internal/rules"
	"github.com/NoBoneZ/bayse-alerter/internal/store"
	"github.com/google/uuid"
)

type RuleStore interface {
	CreateRules(ctx context.Context, rs []rules.Rule) ([]uuid.UUID, error)
	ListRules(ctx context.Context) ([]store.RuleWithState, error)
	ListAlerts(ctx context.Context, limit int) ([]store.Alert, error)
	Ping(ctx context.Context) error
}

type EventResolver interface {
	GetEventBySlug(ctx context.Context, slug string) (bayse.Event, error)
}

type Server struct {
	store    RuleStore
	resolver EventResolver
	log      *slog.Logger
}

func NewServer(store RuleStore, resolver EventResolver, log *slog.Logger) *Server {
	return &Server{store: store, resolver: resolver, log: log}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /rules", s.handleCreateRules)
	mux.HandleFunc("GET /rules", s.handleListRules)
	mux.HandleFunc("GET /alerts", s.handleListAlerts)
	mux.HandleFunc("GET /healthz", s.handleHealth)
	return s.recoverPanics(mux)
}

func (s *Server) recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.log.Error("panic in handler", "err", rec, "path", r.URL.Path)
				writeError(w, http.StatusInternalServerError, "internal error", nil)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
