package rules

import (
	"testing"
	"time"
)

var epoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func runSequence(r Rule, obs []Observation) []bool {
	fires := make([]bool, len(obs))
	st := State{Phase: Armed}
	for i, o := range obs {
		d, next := Evaluate(r, st, o)
		fires[i] = d.Fire
		st = next
	}
	return fires
}

func priceSeq(prices []int64) []Observation {
	obs := make([]Observation, len(prices))
	for i, p := range prices {
		obs[i] = Observation{Price: p, At: epoch.Add(time.Duration(i) * time.Second)}
	}
	return obs
}

func percentSeq(prices []int64, ref int64) []Observation {
	obs := priceSeq(prices)
	for i := range obs {
		obs[i].Reference = ref
	}
	return obs
}

func assertFires(t *testing.T, prices []int64, got, want []bool) {
	t.Helper()
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("tick %d (price %d): fire=%v, want %v", i, prices[i], got[i], want[i])
		}
	}
}

func TestThresholdAbove_FiresOncePerCrossing(t *testing.T) {
	r := Rule{Type: Threshold, Params: Params{Direction: Above, Target: 60}}
	prices := []int64{55, 58, 61, 63, 59, 62}
	want := []bool{false, false, true, false, false, true}
	assertFires(t, prices, runSequence(r, priceSeq(prices)), want)
}

func TestThresholdBelow_FiresOncePerCrossing(t *testing.T) {
	r := Rule{Type: Threshold, Params: Params{Direction: Below, Target: 40}}
	prices := []int64{45, 42, 39, 37, 41, 38}
	want := []bool{false, false, true, false, false, true}
	assertFires(t, prices, runSequence(r, priceSeq(prices)), want)
}

func TestThreshold_NoDoubleFireWhileTrue(t *testing.T) {
	r := Rule{Type: Threshold, Params: Params{Direction: Above, Target: 50}}
	prices := []int64{55, 56, 57, 58, 59}
	want := []bool{true, false, false, false, false}
	assertFires(t, prices, runSequence(r, priceSeq(prices)), want)
}

func TestPercentMoveUp_FiresOncePerCrossing(t *testing.T) {
	r := Rule{Type: PercentMove, Params: Params{PctBps: 1000}}
	prices := []int64{52, 54, 55, 58, 52, 56}
	want := []bool{false, false, true, false, false, true}
	assertFires(t, prices, runSequence(r, percentSeq(prices, 50)), want)
}

func TestPercentMoveDown_FiresAndRecordsNegative(t *testing.T) {
	r := Rule{Type: PercentMove, Params: Params{PctBps: 1000}}
	obs := percentSeq([]int64{48, 45}, 50)

	st := State{Phase: Armed}
	d0, st := Evaluate(r, st, obs[0])
	if d0.Fire {
		t.Fatalf("tick 0 (-4%%): unexpected fire")
	}
	d1, _ := Evaluate(r, st, obs[1])
	if !d1.Fire {
		t.Fatalf("tick 1 (-10%%): expected fire")
	}
	if d1.TriggeredValue != -1000 {
		t.Errorf("triggered value = %d bps, want -1000 (direction must be preserved)", d1.TriggeredValue)
	}
}

func TestPercentMove_NoReferenceNeverFires(t *testing.T) {
	r := Rule{Type: PercentMove, Params: Params{PctBps: 100}}
	obs := []Observation{
		{Price: 80, Reference: 0, At: epoch},
		{Price: 95, Reference: 0, At: epoch.Add(time.Second)},
	}
	for i, fired := range runSequence(r, obs) {
		if fired {
			t.Errorf("tick %d: fired with no reference; should never fire", i)
		}
	}
}

func TestCooldown_SuppressesRefireWithinWindow(t *testing.T) {
	r := Rule{Type: Threshold, Params: Params{Direction: Above, Target: 60, CooldownSeconds: 10}}
	obs := []Observation{
		{Price: 65, At: epoch},
		{Price: 55, At: epoch.Add(2 * time.Second)},
		{Price: 65, At: epoch.Add(4 * time.Second)},
		{Price: 65, At: epoch.Add(15 * time.Second)},
	}
	want := []bool{true, false, false, true}
	got := runSequence(r, obs)
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("tick %d (t=%s): fire=%v, want %v", i, obs[i].At.Sub(epoch), got[i], want[i])
		}
	}
}
