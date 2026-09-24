package alert

import "testing"

func cpuCheck(value float64) Check {
	return Check{
		Key: "cpu_load1", Label: "CPU load (1m)",
		Value: value, Unit: "", Direction: HigherIsBad,
		Alert: 6.0, Warn: 4.0, WarnEnabled: false,
	}
}

func TestEvaluateMetric_DebounceBeforeAlert(t *testing.T) {
	var ms MetricState
	const debounce = 5
	for i := 1; i <= debounce-1; i++ {
		var out Outcome
		ms, out = EvaluateMetric(ms, cpuCheck(10), debounce)
		if out.AlertFired {
			t.Fatalf("tick %d: alert fired before debounce reached", i)
		}
	}
	ms, out := EvaluateMetric(ms, cpuCheck(10), debounce)
	if !out.AlertFired {
		t.Fatalf("alert did not fire on the %dth consecutive bad tick", debounce)
	}
	if !ms.Alerting {
		t.Fatalf("state not marked alerting after alert fired")
	}
}

func TestEvaluateMetric_NoRepeatWhileAlerting(t *testing.T) {
	var ms MetricState
	const debounce = 5
	for i := 0; i < debounce; i++ {
		ms, _ = EvaluateMetric(ms, cpuCheck(10), debounce)
	}
	ms, out := EvaluateMetric(ms, cpuCheck(10), debounce)
	if out.AlertFired {
		t.Fatalf("alert fired again on a tick after it already fired")
	}
	if !ms.Alerting {
		t.Fatalf("expected to remain in alerting state")
	}
}

func TestEvaluateMetric_RecoveryFiresOnce(t *testing.T) {
	var ms MetricState
	const debounce = 5
	for i := 0; i < debounce; i++ {
		ms, _ = EvaluateMetric(ms, cpuCheck(10), debounce)
	}
	ms, out := EvaluateMetric(ms, cpuCheck(1), debounce)
	if !out.AlertCleared {
		t.Fatalf("expected recovery on first good tick after alerting")
	}
	if ms.Alerting {
		t.Fatalf("expected alerting to clear")
	}
	ms, out = EvaluateMetric(ms, cpuCheck(1), debounce)
	if out.AlertCleared {
		t.Fatalf("recovery message fired twice")
	}
}

func TestEvaluateMetric_IntermittentBadResetsCounter(t *testing.T) {
	var ms MetricState
	const debounce = 5
	ms, _ = EvaluateMetric(ms, cpuCheck(10), debounce)
	ms, _ = EvaluateMetric(ms, cpuCheck(10), debounce)
	ms, _ = EvaluateMetric(ms, cpuCheck(1), debounce) // one good tick resets the streak
	if ms.ConsecutiveBad != 0 {
		t.Fatalf("expected consecutive-bad counter to reset, got %d", ms.ConsecutiveBad)
	}
	for i := 0; i < debounce-1; i++ {
		var out Outcome
		ms, out = EvaluateMetric(ms, cpuCheck(10), debounce)
		if out.AlertFired {
			t.Fatalf("alert fired before a fresh full debounce streak")
		}
	}
}

func TestEvaluateMetric_WarnFiresOnceAndResets(t *testing.T) {
	warnCheck := func(value float64) Check {
		c := cpuCheck(value)
		c.WarnEnabled = true
		return c
	}
	var ms MetricState
	ms, out := EvaluateMetric(ms, warnCheck(5), 5) // above warn (4.0), below alert (6.0)
	if !out.WarnFired {
		t.Fatalf("expected warn to fire when crossing the warn threshold")
	}

	ms, out = EvaluateMetric(ms, warnCheck(5), 5)
	if out.WarnFired {
		t.Fatalf("warn fired a second time for the same sustained incident")
	}

	ms, _ = EvaluateMetric(ms, warnCheck(1), 5) // back to normal
	if ms.WarnSent {
		t.Fatalf("expected WarnSent to reset once the metric is healthy again")
	}

	_, out = EvaluateMetric(ms, warnCheck(5), 5) // a fresh, separate incident
	if !out.WarnFired {
		t.Fatalf("expected warn to fire again on a new incident after resetting")
	}
}

// Warn isn't gated behind the alert debounce -- it's a heads-up that
// fires the moment the warn threshold is crossed, once, even while the
// alert's own consecutive-bad count is still climbing toward its
// debounce. Once alerting, EvaluateMetric stops re-evaluating warn at
// all (it's moot: the alert already fired).
func TestEvaluateMetric_WarnFiresOnceThenAlertTakesOver(t *testing.T) {
	warnCheck := func(value float64) Check {
		c := cpuCheck(value)
		c.WarnEnabled = true
		return c
	}
	var ms MetricState
	for i := 0; i < 5; i++ {
		var out Outcome
		ms, out = EvaluateMetric(ms, warnCheck(10), 5) // over both warn and alert thresholds
		wantWarn := i == 0
		if out.WarnFired != wantWarn {
			t.Fatalf("tick %d: WarnFired = %v, want %v", i, out.WarnFired, wantWarn)
		}
	}
	if !ms.Alerting {
		t.Fatalf("expected to be alerting after 5 consecutive bad ticks")
	}
}

func TestEvaluateMetric_LowerIsBad(t *testing.T) {
	memCheck := func(value float64) Check {
		return Check{
			Key: "mem_avail", Label: "Memory available",
			Value: value, Unit: " MB", Direction: LowerIsBad,
			Alert: 1024, Warn: 2048,
		}
	}
	var ms MetricState
	for i := 0; i < 5; i++ {
		ms, _ = EvaluateMetric(ms, memCheck(512), 5) // below alert threshold: bad
	}
	if !ms.Alerting {
		t.Fatalf("expected low available memory to alert")
	}
	_, out := EvaluateMetric(ms, memCheck(4096), 5) // plenty available: recovers
	if !out.AlertCleared {
		t.Fatalf("expected recovery once memory is plentiful again")
	}
}
