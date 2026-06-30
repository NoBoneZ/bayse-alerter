CREATE TABLE rules (
                       id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
                       event_slug  text        NOT NULL,
                       event_id    text        NOT NULL,
                       market_id   text        NOT NULL,
                       outcome     text        NOT NULL,
                       rule_type   text        NOT NULL CHECK (rule_type IN ('threshold_cross','percent_move')),
                       params      jsonb       NOT NULL,
                       enabled     boolean     NOT NULL DEFAULT true,
                       created_at  timestamptz NOT NULL DEFAULT now()
);


CREATE TABLE rule_state (
                            rule_id       uuid        PRIMARY KEY REFERENCES rules(id) ON DELETE CASCADE,
                            phase         text        NOT NULL DEFAULT 'ARMED' CHECK (phase IN ('ARMED','TRIGGERED')),
                            fire_seq      bigint      NOT NULL DEFAULT 0,  -- monotonic; the DB owns this number
                            last_price    bigint,                          -- last observed price, cents (nullable)
                            last_fired_at timestamptz,                     -- null until the first fire
                            updated_at    timestamptz NOT NULL DEFAULT now()
);


CREATE TABLE alerts (
                        id              uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
                        rule_id         uuid        NOT NULL REFERENCES rules(id),
                        fire_seq        bigint      NOT NULL,
                        market_id       text        NOT NULL,
                        outcome         text        NOT NULL,
                        observed_price  bigint      NOT NULL,
                        triggered_value bigint      NOT NULL,
                        triggered_at    timestamptz NOT NULL DEFAULT now(),

                        CONSTRAINT uniq_rule_fire UNIQUE (rule_id, fire_seq)
);

CREATE INDEX idx_rules_enabled ON rules (enabled) WHERE enabled;