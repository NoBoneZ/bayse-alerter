package rules

import "time"

func Evaluate(r Rule, st State, obs Observation) (Decision, State) {
	met, triggeredValue := conditionMet(r, obs)
	next := st

	switch {

	case met && st.Phase == Armed:
		if inCooldown(r, st, obs.At) {
			return Decision{Fire: false}, next
		}
		next.Phase = Triggered
		next.LastFiredAt = obs.At
		return Decision{Fire: true, TriggeredValue: triggeredValue}, next

	case !met && st.Phase == Triggered:
		next.Phase = Armed
		return Decision{Fire: false}, next

	default:
		return Decision{Fire: false}, next
	}
}

func inCooldown(r Rule, st State, now time.Time) bool {
	if r.Params.CooldownSeconds <= 0 || st.LastFiredAt.IsZero() {
		return false
	}
	return now.Sub(st.LastFiredAt) < time.Duration(r.Params.CooldownSeconds)*time.Second
}
