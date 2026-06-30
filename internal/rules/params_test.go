package rules

import (
	"encoding/json"
	"testing"
)

func TestParseParams_Threshold_Valid(t *testing.T) {
	p, err := ParseParams(Threshold, json.RawMessage(`{"direction":"above","target":60}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Direction != Above || p.Target != 60 {
		t.Errorf("parsed params = %+v", p)
	}
}

func TestParseParams_Threshold_Invalid(t *testing.T) {
	cases := map[string]string{
		"missing direction": `{"target":60}`,
		"bad direction":     `{"direction":"sideways","target":60}`,
		"zero target":       `{"direction":"above","target":0}`,
		"negative target":   `{"direction":"below","target":-5}`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseParams(Threshold, json.RawMessage(raw)); err == nil {
				t.Errorf("expected validation error, got nil")
			}
		})
	}
}

func TestParseParams_PercentMove_Valid(t *testing.T) {
	p, err := ParseParams(PercentMove, json.RawMessage(`{"pct_bps":1000,"window_seconds":900}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.PctBps != 1000 || p.WindowSeconds != 900 {
		t.Errorf("parsed params = %+v", p)
	}
}

func TestParseParams_PercentMove_Invalid(t *testing.T) {
	cases := map[string]string{
		"zero pct":    `{"pct_bps":0,"window_seconds":900}`,
		"zero window": `{"pct_bps":1000,"window_seconds":0}`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseParams(PercentMove, json.RawMessage(raw)); err == nil {
				t.Errorf("expected validation error, got nil")
			}
		})
	}
}

func TestParseParams_UnknownType(t *testing.T) {
	if _, err := ParseParams(RuleType("nonsense"), json.RawMessage(`{}`)); err == nil {
		t.Error("expected error for unknown rule type")
	}
}

func TestParseParams_MalformedJSON(t *testing.T) {
	if _, err := ParseParams(Threshold, json.RawMessage(`{not json`)); err == nil {
		t.Error("expected error for malformed JSON")
	}
}
