package alert

// Direction says which side of a threshold counts as "bad" for a metric.
type Direction int

const (
	// HigherIsBad: value >= threshold is bad (load, disk %, iowait %, swap MB).
	HigherIsBad Direction = iota
	// LowerIsBad: value <= threshold is bad (available memory).
	LowerIsBad
)

// Check is one metric's reading plus its thresholds, evaluated once per
// tick.
type Check struct {
	Key         string // stable dedupe-state key, e.g. "cpu_load1" or "disk:/data"
	Label       string // human label for messages, e.g. "CPU load (1m)"
	Value       float64
	Unit        string // formatting suffix, e.g. "", " MB", "%"
	Direction   Direction
	Alert       float64
	Warn        float64
	WarnEnabled bool
}

func (c Check) isBad(threshold float64) bool {
	if c.Direction == HigherIsBad {
		return c.Value >= threshold
	}
	return c.Value <= threshold
}

// Outcome records which dedupe transitions happened this tick for one
// metric, so the caller knows which messages (if any) to send.
type Outcome struct {
	Check        Check
	AlertFired   bool
	AlertCleared bool
	WarnFired    bool
}

// EvaluateMetric applies one tick's reading to a metric's dedupe state
// and returns the updated state plus what happened. Semantics:
//
//   - Alert fires once bad for `debounce` consecutive ticks, and clears
//     with a single recovery message the first tick it's no longer bad.
//   - Warn (only checked while not alerting, and only if the check's
//     WarnEnabled is set) fires once when crossing the warn threshold and
//     resets silently once the metric is back to normal, so a later,
//     separate incident can warn again.
func EvaluateMetric(ms MetricState, c Check, debounce int) (MetricState, Outcome) {
	out := Outcome{Check: c}
	bad := c.isBad(c.Alert)

	if bad {
		ms.ConsecutiveBad++
	} else {
		ms.ConsecutiveBad = 0
	}

	switch {
	case bad && !ms.Alerting && ms.ConsecutiveBad >= debounce:
		ms.Alerting = true
		out.AlertFired = true
	case !bad && ms.Alerting:
		ms.Alerting = false
		out.AlertCleared = true
	}

	if c.WarnEnabled && !ms.Alerting {
		warnBad := c.isBad(c.Warn)
		if warnBad && !ms.WarnSent {
			ms.WarnSent = true
			out.WarnFired = true
		} else if !warnBad {
			ms.WarnSent = false
		}
	}

	return ms, out
}
