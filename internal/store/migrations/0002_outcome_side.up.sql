-- The Bayse ticker and price-history endpoints key an outcome by its canonical
-- side (YES = outcome1, NO = outcome2), not by the market's display label
-- (which can be anything, e.g. "Up"/"Down"). We keep the human label in
-- `outcome` for display and store the canonical side here for API calls.
ALTER TABLE rules
    ADD COLUMN outcome_side text NOT NULL DEFAULT 'YES'
        CHECK (outcome_side IN ('YES', 'NO'));
