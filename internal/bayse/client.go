package bayse

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"net/url"
	"time"
)

const defaultBaseURL = "https://relay.bayse.markets/v1/pm"

type Client struct {
	http        *http.Client
	baseURL     string
	publicKey   string
	maxAttempts int
	baseBackoff time.Duration
	log         *slog.Logger
}

type Option func(*Client)

func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

func WithBaseURL(u string) Option { return func(c *Client) { c.baseURL = u } }

func WithRetry(attempts int, baseBackoff time.Duration) Option {
	return func(c *Client) { c.maxAttempts, c.baseBackoff = attempts, baseBackoff }
}

func WithLogger(l *slog.Logger) Option { return func(c *Client) { c.log = l } }

func New(publicKey string, opts ...Option) *Client {
	c := &Client{
		http:        &http.Client{Timeout: 5 * time.Second},
		baseURL:     defaultBaseURL,
		publicKey:   publicKey,
		maxAttempts: 3,
		baseBackoff: 100 * time.Millisecond,
		log:         slog.Default(),
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

func (c *Client) GetEventBySlug(ctx context.Context, slug string) (Event, error) {
	var ev Event
	q := url.Values{"currency": {"USD"}}
	err := c.get(ctx, "/events/slug/"+url.PathEscape(slug), q, &ev)
	return ev, err
}

func (c *Client) Ticker(ctx context.Context, marketID, outcome string) (Ticker, error) {
	var t Ticker
	q := url.Values{"outcome": {outcome}}
	err := c.get(ctx, "/markets/"+url.PathEscape(marketID)+"/ticker", q, &t)
	return t, err
}

func (c *Client) OrderBook(ctx context.Context, outcomeID string) (OrderBook, error) {
	var books []OrderBook
	q := url.Values{"outcomeId[]": {outcomeID}, "depth": {"1"}, "currency": {"USD"}}
	if err := c.get(ctx, "/books", q, &books); err != nil {
		return OrderBook{}, err
	}
	if len(books) == 0 {
		return OrderBook{}, ErrNotFound
	}
	return books[0], nil
}

func (c *Client) PriceHistory(ctx context.Context, eventID, marketID, outcome, timePeriod string) ([]PricePoint, error) {
	var raw map[string][]PricePoint
	q := url.Values{"timePeriod": {timePeriod}}
	if outcome != "" {
		q.Set("outcome", outcome)
	}
	if marketID != "" {
		q.Set("marketId[]", marketID)
	}
	err := c.get(ctx, "/events/"+url.PathEscape(eventID)+"/price-history", q, &raw)
	if err != nil {
		return nil, err
	}
	return raw[marketID], nil
}

func (c *Client) CurrentPrice(ctx context.Context, marketID, outcome string) (int64, error) {
	t, err := c.Ticker(ctx, marketID, outcome)
	if err != nil {
		return 0, err
	}
	price := t.LastPrice
	if price <= 0 {
		price = t.MidPrice
	}
	if price <= 0 {
		return 0, fmt.Errorf("bayse: no usable price for market %s outcome %s", marketID, outcome)
	}
	return toCents(price), nil
}

func (c *Client) ReferencePrice(ctx context.Context, eventID, marketID, outcome string, window time.Duration, now time.Time) (int64, error) {
	points, err := c.PriceHistory(ctx, eventID, marketID, outcome, periodFor(window))
	if err != nil {
		return 0, err
	}
	price, ok := nearestAtOrBefore(points, outcome, now.Add(-window))
	if !ok {
		return 0, fmt.Errorf("bayse: no price history at/before window start for market %s", marketID)
	}
	return toCents(price), nil
}

func periodFor(window time.Duration) string {
	switch {
	case window <= 12*time.Hour:
		return "12H"
	case window <= 24*time.Hour:
		return "24H"
	case window <= 7*24*time.Hour:
		return "1W"
	case window <= 30*24*time.Hour:
		return "1M"
	default:
		return "1Y"
	}
}

func nearestAtOrBefore(points []PricePoint, outcome string, target time.Time) (float64, bool) {
	var best PricePoint
	found := false
	for _, p := range points {
		if outcome != "" && p.Outcome != outcome {
			continue
		}
		if p.Timestamp.After(target) {
			continue
		}
		if !found || p.Timestamp.After(best.Timestamp) {
			best, found = p, true
		}
	}
	return best.Price, found
}

func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	backoff := c.baseBackoff
	var lastErr error
	for attempt := 0; attempt < c.maxAttempts; attempt++ {
		if attempt > 0 {
			if err := sleep(ctx, jitter(backoff)); err != nil {
				return err
			}
			backoff *= 2
		}

		err := c.doOnce(ctx, u, out)
		if err == nil {
			return nil
		}
		var te *transientError
		if !errors.As(err, &te) {
			return err
		}
		lastErr = err
		c.log.Warn("bayse transient failure; will retry",
			"url", u, "attempt", attempt+1, "err", err)
	}
	return lastErr
}

func (c *Client) doOnce(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if c.publicKey != "" {
		req.Header.Set("X-Public-Key", c.publicKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return transient(err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusOK:
		if out == nil {
			return nil
		}
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("bayse: decode response: %w", err)
		}
		return nil
	case resp.StatusCode == http.StatusNotFound:
		return ErrNotFound
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		return transient(c.apiErr(resp))
	default:
		return c.apiErr(resp)
	}
}

func (c *Client) apiErr(resp *http.Response) error {
	var body errorBody
	_ = json.NewDecoder(resp.Body).Decode(&body)
	return &apiError{StatusCode: resp.StatusCode, Code: body.Error, Message: body.Message}
}

func jitter(d time.Duration) time.Duration {
	half := d / 2
	return half + time.Duration(rand.Int63n(int64(half)+1))
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
