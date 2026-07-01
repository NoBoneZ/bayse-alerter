package main

import (
	"context"
	"errors"
	"github.com/NoBoneZ/bayse-alerter/internal/store"
	"github.com/joho/godotenv"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/NoBoneZ/bayse-alerter/internal/api"
	"github.com/NoBoneZ/bayse-alerter/internal/bayse"
	"github.com/NoBoneZ/bayse-alerter/internal/config"
	"github.com/NoBoneZ/bayse-alerter/internal/poller"
)

func main() {
	_ = godotenv.Load()

	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
	log.Info("stopped cleanly")
}

func run(log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	if err := store.Migrate(cfg.DatabaseURL); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	st, err := store.New(ctx, cfg.DatabaseURL, log)
	if err != nil {
		return err
	}
	defer st.Close()

	client := bayse.New(cfg.BaysePublicKey,
		bayse.WithBaseURL(cfg.BayseBaseURL),
		bayse.WithHTTPClient(&http.Client{Timeout: cfg.HTTPTimeout}),
		bayse.WithLogger(log),
	)

	p := poller.New(st, client, cfg.PollInterval, log)
	srv := api.NewServer(st, client, log)
	httpSrv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		return p.Run(gctx)
	})

	g.Go(func() error {
		log.Info("http server listening", "addr", cfg.HTTPAddr)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	})

	g.Go(func() error {
		<-gctx.Done()
		log.Info("shutdown signal received, draining")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return httpSrv.Shutdown(shutdownCtx)
	})

	if err := g.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}
