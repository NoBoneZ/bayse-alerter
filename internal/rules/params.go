package rules

import (
	"encoding/json"
	"fmt"
)

func ParseParams(t RuleType, raw json.RawMessage) (Params, error) {
	var p Params
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &p); err != nil {
			return Params{}, fmt.Errorf("params: invalid JSON: %w", err)
		}
	}
	if err := p.Validate(t); err != nil {
		return Params{}, err
	}
	return p, nil
}

func (p Params) Validate(t RuleType) error {
	if p.CooldownSeconds < 0 {
		return fmt.Errorf("cooldown_seconds must be >= 0")
	}
	switch t {
	case Threshold:
		if p.Direction != Above && p.Direction != Below {
			return fmt.Errorf("direction must be %q or %q", Above, Below)
		}
		if p.Target <= 0 {
			return fmt.Errorf("target must be > 0 (price in cents)")
		}
		return nil
	case PercentMove:
		if p.PctBps <= 0 {
			return fmt.Errorf("pct_bps must be > 0 (basis points; 1000 = 10%%)")
		}
		if p.WindowSeconds <= 0 {
			return fmt.Errorf("window_seconds must be > 0")
		}
		return nil
	default:
		return fmt.Errorf("unknown rule type %q", t)
	}
}
