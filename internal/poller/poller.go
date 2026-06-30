package poller

import (
	"context"
	"github.com/NoBoneZ/bayse-alerter/internal/rules"
	"github.com/NoBoneZ/bayse-alerter/internal/store"
	"log/slog"
	"time"
)

// Poller checks every enabled rule on a fixed interval.
type Poller struct {
	store    Store
	prices   Prices
	interval time.Duration
	log      *slog.Logger
	now      func() time.Time
}

func New(s Store, p Prices, interval time.Duration, log *slog.Logger) *Poller {
	return &Poller{
		store:    s,
		prices:   p,
		interval: interval,
		log:      log,
		now:      time.Now,
	}
}

func (p *Poller) Run(ctx context.Context) error {
	p.log.Info("poller started", "interval", p.interval)
	t := time.NewTicker(p.interval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			p.log.Info("poller stopping")
			return ctx.Err()
		case <-t.C:
			if err := p.tick(ctx); err != nil {
				p.log.Error("poll tick failed", "err", err)
			}
		}
	}
}

func (p *Poller) tick(ctx context.Context) error {
	items, err := p.store.EnabledRulesWithState(ctx)
	if err != nil {
		return err
	}
	for _, it := range items {
		if err := p.checkRule(ctx, it); err != nil {
			p.log.Warn("rule check failed", "rule_id", it.Rule.ID, "err", err)
		}
	}
	return nil
}

func (p *Poller) checkRule(ctx context.Context, it store.RuleWithState) error {
	price, err := p.prices.CurrentPrice(ctx, it.Rule.MarketID, it.Rule.Outcome)
	if err != nil {
		return err
	}

	obs := rules.Observation{Price: price, At: p.now()}

	if it.Rule.Type == rules.PercentMove {
		window := time.Duration(it.Rule.Params.WindowSeconds) * time.Second
		ref, err := p.prices.ReferencePrice(ctx, it.Rule.EventID, it.Rule.MarketID, it.Rule.Outcome, window, obs.At)
		if err != nil {
			return err
		}
		obs.Reference = ref
	}

	decision, next := rules.Evaluate(it.Rule, it.State, obs)

	switch {
	case decision.Fire:

		fired, err := p.store.FireAlert(ctx, it.Rule, obs, decision)
		if err != nil {
			return err
		}
		if fired {
			p.log.Info("alert fired",
				"rule_id", it.Rule.ID,
				"market_id", it.Rule.MarketID,
				"outcome", it.Rule.Outcome,
				"price", price,
				"triggered_value", decision.TriggeredValue,
			)
		}

	case it.State.Phase == rules.Triggered && next.Phase == rules.Armed:
		return p.store.Rearm(ctx, it.Rule.ID)
	}

	return nil
}
