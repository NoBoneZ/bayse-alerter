# bayse-alerter

A small Go service that watches Bayse prediction markets, evaluates price-alert
rules, and records an alert in Postgres whenever a rule trips. You register rules
over HTTP; a background loop polls the markets on an interval and decides when
each rule fires.

The genuinely interesting part of this isn't fetching prices. It's making each
alert fire **once**, at the moment its condition becomes true, and not again on
every poll while it stays true. Most of the design below exists to get that one
behavior right, and to keep it right even when a process crashes mid-write or two
copies of the service run at the same time.

---

## What it does

- Resolves a Bayse event by its slug, validates the rules you submit against that
  event's real markets and outcomes, and stores them.
- Polls the current price for each enabled rule on a configurable interval.
- Supports two rule types against a market outcome:
    - **Threshold cross** — the price moves above (or below) a target, e.g. YES
      crosses above 60 cents.
    - **Percent move** — the price moves by at least X% over a rolling window,
      e.g. plus or minus 10% within 15 minutes.
- Fires each rule exactly once on the transition into the triggered state, then
  re-arms when the condition clears so the next crossing can fire again.
- Writes every firing to an `alerts` table without duplicates.
- Retires a rule automatically once its market resolves, so the loop stops
  polling markets that will never trade again.

---

## Running it

You need Docker, and a Bayse **public** key (the `pk_live_...` one). Read
endpoints only need the public key, so a standard account works — no funded
balance or write access required. Create a key from the Bayse web app under
Account Settings, API Keys.

The whole thing comes up with one command:

```bash
BAYSE_PUBLIC_KEY=pk_live_yourkey docker compose up --build
```

That builds the binary, starts Postgres, waits for it to be healthy, runs the
schema migrations automatically on startup, and then starts the polling loop and
the HTTP API together. The API listens on `:8080` by default.

To check it's alive:

```bash
curl localhost:8080/healthz
# {"status":"ok"}
```

### Configuration

Everything is read from the environment. Only two values are required; the rest
have sensible defaults.

| Variable | Required | Default | Meaning |
| --- | --- | --- | --- |
| `BAYSE_PUBLIC_KEY` | yes | — | Your `pk_live_...` key, sent as `X-Public-Key`. |
| `DATABASE_URL` | yes | — | Postgres DSN. Compose sets this for you. |
| `BAYSE_BASE_URL` | no | `https://relay.bayse.markets/v1/pm` | API base; override for testing. |
| `POLL_INTERVAL` | no | `10s` | How often the loop checks every rule. |
| `HTTP_ADDR` | no | `:8080` | Address the API listens on. |
| `HTTP_TIMEOUT` | no | `5s` | Per-request timeout for calls to Bayse. |

The public key is a secret and never lives in the repo. There's a
`.env.example` with placeholders; copy it to `.env` for local runs if you like.
Note that the service only needs the **public** key — if you have a private
`sk_live_...` key lying around, this service has no use for it, and you should
keep it out of anywhere it might get committed or shared.

---

## API reference

The service exposes a small HTTP surface: one endpoint to create rules and two
read-only endpoints to inspect what it has stored and fired.

| Method | Path | Purpose |
| --- | --- | --- |
| `POST` | `/rules` | Register one or more rules against an event slug. |
| `GET` | `/rules` | List every rule with its current phase (armed/triggered). |
| `GET` | `/alerts` | List recent firings, newest first. Takes `?limit=` (default 100, max 1000). |
| `GET` | `/healthz` | Liveness plus a database ping. |

A full, runnable Postman collection with example requests and responses lives
here:

https://documenter.getpostman.com/view/26527466/2sBY4HSNbA

## Creating rules

Rules are created by POSTing an event slug and a list of rules. The service
resolves the slug against Bayse first, so a bad slug or a market/outcome that
doesn't exist on that event is rejected before anything is stored.

A threshold rule and a percent rule on the same market:

```bash
curl -s localhost:8080/rules \
  -H 'Content-Type: application/json' \
  -d '{
    "event_slug": "crypto-btc-1h-feb-24-11am",
    "rules": [
      {
        "market_id": "b2c3d4e5-f6a7-8901-bcde-f12345678901",
        "outcome": "YES",
        "type": "threshold_cross",
        "params": { "direction": "above", "target": 60 }
      },
      {
        "market_id": "b2c3d4e5-f6a7-8901-bcde-f12345678901",
        "outcome": "YES",
        "type": "percent_move",
        "params": { "pct_bps": 1000, "window_seconds": 900 }
      }
    ]
  }'
```

On success you get back the IDs of the rules that were created:

```json
{ "rule_ids": ["7c9e6679-7425-40de-944b-e07fc1f90ae7", "..."] }
```

### Choosing the outcome

Every rule targets one outcome of a market. Bayse markets are binary, and each
has two outcomes with display labels that vary from market to market — a BTC
market might label them `Up` and `Down` rather than `YES` and `NO`. You can name
the outcome either way:

- by its display label, exactly as the market lists it (`"Up"`, `"Down"`), or
- by the canonical side, `"YES"` for the first outcome or `"NO"` for the second.

Internally the service always talks to Bayse using the canonical side, because
the ticker and price-history endpoints only understand `YES`/`NO`; the display
label is kept for the stored rule and the alert row. If you name an outcome the
market doesn't have, creation fails with a 422 that lists the valid labels.

### Parameter reference

Prices are handled internally as integer cents, so `target` is given in cents
(60 means 60 cents). This matches how the price comes back once the decimal from
Bayse is converted.

**threshold_cross**
- `direction` — `"above"` or `"below"`.
- `target` — the price to cross, in cents (1 to 99).

**percent_move**
- `pct_bps` — the move that trips the rule, in basis points. 1000 is 10%, 500 is
  5%. Basis points keep the math in integers, which avoids floating-point
  rounding right at the threshold.
- `window_seconds` — the rolling window the move is measured over.

**optional, either type**
- `cooldown_seconds` — minimum gap between fires, to stop a price that's
  oscillating right at the boundary from firing repeatedly. Defaults to off.

### Errors you might get back

The status codes try to tell you *what kind* of mistake happened:

- `400` — the JSON was malformed, or a rule's params were invalid (for example a
  threshold with no direction, or a percent rule with a zero window). The body
  names the offending rule and field.
- `404` — the event slug doesn't exist on Bayse.
- `422` — the request was well-formed, but you named a market or outcome that
  isn't part of that event.
- `502` — Bayse itself couldn't be reached to validate the slug.

---

## Inspecting rules and alerts

Two read-only endpoints let you watch the service work without opening a psql
session.

`GET /rules` returns every rule the service knows about, each with the phase it
is currently in, so you can see at a glance which rules are armed and which have
already tripped:

```bash
curl -s localhost:8080/rules | jq
```

```json
{
  "rules": [
    {
      "id": "7c9e6679-7425-40de-944b-e07fc1f90ae7",
      "event_slug": "crypto-btc-1h-feb-24-11am",
      "market_id": "b2c3d4e5-f6a7-8901-bcde-f12345678901",
      "outcome": "YES",
      "type": "threshold_cross",
      "params": { "direction": "above", "target": 60 },
      "enabled": true,
      "phase": "ARMED",
      "created_at": "2026-02-17T12:00:00Z"
    }
  ]
}
```

`GET /alerts` returns the firings themselves, newest first. The `limit` query
parameter caps the page and defaults to 100:

```bash
curl -s 'localhost:8080/alerts?limit=20' | jq
```

```json
{
  "alerts": [
    {
      "id": "b1f2...",
      "rule_id": "7c9e6679-7425-40de-944b-e07fc1f90ae7",
      "fire_seq": 1,
      "market_id": "b2c3d4e5-f6a7-8901-bcde-f12345678901",
      "outcome": "YES",
      "observed_price": 61,
      "triggered_value": 60,
      "triggered_at": "2026-02-17T12:03:10Z"
    }
  ]
}
```

Prices come back in the same integer cents the rules are written in, and
`fire_seq` is the per-rule counter that backs the once-only guarantee described
below, so a rule that has fired twice will show alerts with `fire_seq` 1 and 2.

---

## How an alert fires exactly once

This is the heart of the service, so it's worth explaining properly.

Each rule remembers a small piece of state: whether it is **ARMED** (the
condition is currently false, ready to fire) or **TRIGGERED** (the condition is
currently true, already fired). On every poll the loop checks the condition and
looks at this state:

- condition true, rule ARMED — this is the moment it crossed. Fire, and move the
  rule to TRIGGERED.
- condition true, rule TRIGGERED — it was already true last time, so do nothing.
  This is what stops the same crossing firing on every poll.
- condition false, rule TRIGGERED — the price has moved back; re-arm the rule so
  the next crossing can fire.
- condition false, rule ARMED — nothing happening.

So a price that climbs across 60, sits at 63 for five minutes, drops to 59, then
crosses 60 again fires exactly twice — once per genuine crossing — not thirty
times because it happened to be above the line on thirty polls.

That state machine is correct, but it isn't enough on its own. If the process
crashed between "write the alert" and "save the new state," or if two copies of
the service polled the same rule at once, you could still get a double fire. So
the transition is made atomic and idempotent in the database itself.

When a rule needs to fire, the service runs a single conditional update inside a
transaction:

```sql
UPDATE rule_state
   SET phase = 'TRIGGERED', fire_seq = fire_seq + 1, last_fired_at = now()
 WHERE rule_id = $1 AND phase = 'ARMED'
RETURNING fire_seq;
```

Because of the `WHERE phase = 'ARMED'`, only one caller can ever win this update
for a given crossing — everyone else matches zero rows and quietly does nothing.
The `fire_seq` it returns is then written onto the alert row, and the `alerts`
table carries a `UNIQUE (rule_id, fire_seq)` constraint. So even if something
truly unexpected let two writes through, the second one is physically rejected by
the database.

There are three layers here, and they're deliberately independent: the in-memory
state machine makes the common case correct and easy to test; the conditional
update makes the transition safe under concurrency; and the unique constraint is
the last line of defense that turns "should never happen" into "cannot happen."

---

## How the code is organized

The packages are split so that the part that actually matters — the firing logic
— has no dependencies and can be tested without a database or the network.

- `internal/rules` — the pure evaluation engine. No I/O at all. `Evaluate` is a
  plain function: given a rule, its last state, and a price observation, it
  returns whether to fire and the next state. The ARMED/TRIGGERED state machine
  lives here, and so does all the threshold/percent math. This is where the
  bulk of the tests point.
- `internal/store` — Postgres. Owns the connection pool, the embedded
  migrations, and all the SQL. The atomic `FireAlert` and the unique constraint
  described above live here.
- `internal/bayse` — a small read-only client for the Bayse API. Sets the
  `X-Public-Key` header, retries transient failures with backoff, and converts
  the decimal prices from the API into integer cents at the boundary so the rest
  of the code never touches floats.
- `internal/poller` — the loop. It loads enabled rules, fetches prices, calls the
  pure engine, and persists whatever transition the engine decides. It contains
  no decision logic itself; it's pure orchestration.
- `internal/api` — the HTTP layer. One real endpoint (`POST /rules`) plus a
  health check.
- `internal/config` — environment to typed config.
- `cmd/alerter` — wires it all together and handles graceful shutdown.

The `poller` and `api` packages depend on the store and the client through small
interfaces they declare themselves, which is what lets them be tested with fakes.

### Data model

Three tables. `rules` is the durable definition of what you asked for.
`rule_state` is the hot, per-rule ARMED/TRIGGERED memory, kept separate because
it changes on almost every poll while the rule definition basically never does.
`alerts` is one row per firing, with the unique constraint that guarantees no
duplicates.

---

## Tests

```bash
go test ./...
```

The engine tests are the ones to look at first. `internal/rules` has a test that
feeds a sequence of prices through `Evaluate`, carrying state forward exactly as
the loop does, and asserts the precise ticks on which a rule fires — that it
fires on the crossing, not on the higher tick after, that it re-arms on the dip,
and that it fires again on the next crossing. There are matching cases for
below-thresholds, percent moves up and down, the cooldown, and the percent rule
refusing to fire before it has a reference price. None of these need a database.

The `internal/bayse` tests use `httptest` to assert the auth header is sent, that
a 404 maps to a not-found error and is not retried, that a 503 is retried, and
that the decimal prices decode into the right number of cents.

There's also an integration test in `internal/store` that proves the once-only
guarantee against a real Postgres: it fires twenty goroutines at a single armed
rule at once and asserts that exactly one of them succeeds and exactly one alert
row exists. It's skipped unless you point it at a database:

```bash
TEST_DATABASE_URL=postgres://bayse:bayse@localhost:5432/bayse?sslmode=disable \
  go test ./internal/store/
```

---

## Design decisions and trade-offs

A few choices worth calling out, along with what I gave up for them.

**Prices as integer cents, percentages as basis points.** Money in floats breaks
exactly where this service does its most important work — comparisons right at a
threshold. Integers remove that whole class of bug and make tests deterministic.
The cost is one conversion at the client boundary, which is a single rounding
line.

**A pure evaluation engine.** Keeping `Evaluate` free of I/O means the firing
logic — the thing the whole task is about — is a function of plain data and can
be checked exhaustively with a table of inputs and expected outputs, no mocks.
The poller becomes a thin shell around it. The trade-off is a little more
plumbing to move data into and out of the engine, which I think is well worth it.

**Two tables instead of one.** Splitting the immutable rule from its churning
state keeps the conditional update operating on a tiny, lock-friendly row and
keeps the rule definition clean. It's one more table to reason about.

**`params` as jsonb rather than typed columns.** This lets one column hold either
rule type's config and means a third rule type wouldn't need a migration. In
exchange, the database can't enforce per-type field rules — that validation lives
in Go instead, in one place that every rule passes through at creation.

**Hand-rolled migrations instead of a library.** The migration runner is about
fifty lines that embed the SQL into the binary and apply it on startup. I went
this way mainly to avoid pulling in a second Postgres driver, since a popular
migration library uses `database/sql` and `lib/pq` while everything else here is
on pgx. The trade-off is no built-in down-migration tooling, which I don't need
for a forward-only schema yet.

**Standard library where it's enough.** The router is the standard library mux
(the Go 1.22 version handles method-based routing), config is a small env loader,
and retries are about thirty lines. The only real dependencies are the Postgres
driver and a UUID type. This is partly taste and partly that fewer dependencies
is one fewer thing to explain.

---

## Assumptions

- A threshold "crosses above 60" means strictly greater than 60. The crossing
  itself is handled by the state machine; the comparison just defines "currently
  past the line." Either convention is defensible, so I picked one and stuck to
  it.
- `target` is provided in cents, matching the internal price unit.
- The poller runs as a single instance. The database guarantees make multiple
  instances *safe* (they won't double-fire), but I haven't added anything to stop
  two instances both polling everything and doubling the API load.
- The current price comes from the ticker's last traded price, falling back to
  its midpoint when nothing has traded yet.

---

## Known limitations and what I'd do with more time

I want to be straight about the weakest part. The **percent-move reference
price** currently comes from Bayse's price-history endpoint, and that endpoint
only offers coarse, fixed windows (12 hours, 24 hours, a week, and so on). For a
short window like "10% in 15 minutes," the nearest historical sample it can give
might be considerably older than fifteen minutes, so the window isn't exact. The
right fix, and the first thing I'd do next, is to keep a small rolling buffer of
the prices the poller already fetches each tick — in memory, or in a
`price_observations` table — and measure the move against the oldest sample still
inside the window. That makes the window exact and arbitrary, and removes an API
call per percent rule per poll at the same time.

After that, in rough priority order:

- Idempotency on rule creation, so a retried POST doesn't create duplicate rules.
- Authentication on the management endpoint.
- Metrics and structured visibility into fire counts, poll latency, and upstream
  error rates.
- Leader election or rule partitioning, so the service can scale to multiple
  instances without each one redundantly polling every rule.

The spine — accept rules, poll reliably, fire each one exactly once, persist
without duplicates — is what I focused on, and I'd rather it be solid and narrow
than broad and occasionally wrong.