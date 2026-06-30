package rules

import "time"

type Observation struct {
	Price     int64
	Reference int64
	At        time.Time
}

type Decision struct {
	Fire           bool
	TriggeredValue int64
}
