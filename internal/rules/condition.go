package rules

func conditionMet(r Rule, obs Observation) (met bool, triggeredValue int64) {
	switch r.Type {
	case Threshold:
		return thresholdMet(r, obs)
	case PercentMove:
		return percentMet(r, obs)
	default:
		return false, 0
	}
}

func thresholdMet(r Rule, obs Observation) (bool, int64) {
	switch r.Params.Direction {
	case Above:
		return obs.Price > r.Params.Target, r.Params.Target
	case Below:
		return obs.Price < r.Params.Target, r.Params.Target
	default:
		return false, 0
	}
}

func percentMet(r Rule, obs Observation) (bool, int64) {
	if obs.Reference <= 0 {
		return false, 0
	}

	deltaBps := (obs.Price - obs.Reference) * 10000 / obs.Reference

	abs := deltaBps
	if abs < 0 {
		abs = -abs
	}

	return abs >= r.Params.PctBps, deltaBps
}
