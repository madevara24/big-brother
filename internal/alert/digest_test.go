package alert

import (
	"math"
	"testing"
)

func TestComputeStats(t *testing.T) {
	values := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	st := computeStats(values)
	if st.Min != 1 {
		t.Errorf("Min = %v, want 1", st.Min)
	}
	if st.Max != 10 {
		t.Errorf("Max = %v, want 10", st.Max)
	}
	if math.Abs(st.Avg-5.5) > 1e-9 {
		t.Errorf("Avg = %v, want 5.5", st.Avg)
	}
	// 95th percentile of 10 sorted values, ceil(0.95*10)-1 = index 9 -> 10.
	if st.P95 != 10 {
		t.Errorf("P95 = %v, want 10", st.P95)
	}
}

func TestComputeStats_Empty(t *testing.T) {
	st := computeStats(nil)
	if st != (MetricStats{}) {
		t.Errorf("expected zero-value stats for empty input, got %+v", st)
	}
}

func TestComputeStats_Single(t *testing.T) {
	st := computeStats([]float64{42})
	want := MetricStats{Min: 42, Avg: 42, Max: 42, P95: 42}
	if st != want {
		t.Errorf("got %+v, want %+v", st, want)
	}
}
