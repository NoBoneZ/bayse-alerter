// Command alerter is the entrypoint for the bayse-alerter service. It wires the
// pieces together — config, database, Bayse client, the polling loop, and the
// HTTP API — and runs the loop and the API server side by side under a single
// context so that one SIGINT/SIGTERM shuts everything down cleanly.
package main

import (
	"context"
	"errors"
	"github.com/NoBoneZ/bayse-alerter/internal/store"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"

	// NOTE: replace this module path with the one in YOUR go.mod
	// (e.g. github.com/<your-username>/bayse-alerter).
	"github.com/NoBoneZ/bayse-alerter/internal/api"
	"github.com/NoBoneZ/bayse-alerter/internal/bayse"
	"github.com/NoBoneZ/bayse-alerter/internal/config"
	"github.com/NoBoneZ/bayse-alerter/internal/poller"
)

func main() {
	// A structured JSON logger, used everywhere. slog is standard library.
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
	log.Info("stopped cleanly")
}

// run holds the real logic so every failure path can return an error instead of
// calling os.Exit directly. main() does the single exit. This keeps the startup
// sequence readable and testable.
func run(log *slog.Logger) error {
	// 1. Load configuration from the environment (secrets included).
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// 2. Apply database migrations before anything touches the schema.
	//    Idempotent: a no-op once the DB is already up to date.
	if err := store.Migrate(cfg.DatabaseURL); err != nil {
		return err
	}

	// 3. One context for the whole process lifetime. It is cancelled the moment
	//    the user hits Ctrl-C (SIGINT) or the container is asked to stop
	//    (SIGTERM). Everything below watches this context to wind down.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// 4. Open the Postgres connection pool.
	st, err := store.New(ctx, cfg.DatabaseURL, log)
	if err != nil {
		return err
	}
	defer st.Close()

	// 5. Build the read-only Bayse client.
	client := bayse.New(cfg.BaysePublicKey,
		bayse.WithBaseURL(cfg.BayseBaseURL),
		bayse.WithHTTPClient(&http.Client{Timeout: cfg.HTTPTimeout}),
		bayse.WithLogger(log),
	)

	// 6. Construct the two long-running components.
	//    - the poller: loads rules, checks prices, fires alerts on the interval
	//    - the API server: accepts new rules over HTTP
	p := poller.New(st, client, cfg.PollInterval, log)
	srv := api.NewServer(st, client, log)
	httpSrv := &http.Server{Addr: cfg.HTTPAddr, Handler: srv.Routes()}

	// 7. Run them together. errgroup gives us a shared, cancellable context: if
	//    any goroutine returns an error, gctx is cancelled and the others stop.
	g, gctx := errgroup.WithContext(ctx)

	// 7a. The polling loop. Blocks until gctx is cancelled.
	g.Go(func() error {
		return p.Run(gctx)
	})

	// 7b. The HTTP server. ListenAndServe returns ErrServerClosed on a clean
	//     Shutdown — that is success, not failure, so we swallow it.
	g.Go(func() error {
		log.Info("http server listening", "addr", cfg.HTTPAddr)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	})

	// 7c. The shutdown watcher. When the shared context ends (signal received,
	//     or a sibling goroutine errored), gracefully drain the HTTP server with
	//     a bounded deadline so in-flight requests get a chance to finish.
	g.Go(func() error {
		<-gctx.Done()
		log.Info("shutdown signal received, draining")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return httpSrv.Shutdown(shutdownCtx)
	})

	// 8. Wait for everything to finish. A clean shutdown surfaces as
	//    context.Canceled, which is not a real error — filter it out.
	if err := g.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}
