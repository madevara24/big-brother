package alert

import (
	"fmt"
	"math"
	"sort"
	"time"
)

// MetricStats is the min/avg/max/p95 summary of one metric's samples
// over a digest period.
type MetricStats struct {
	Min, Avg, Max, P95 float64
}

// computeStats returns the zero value if values is empty.
func computeStats(values []float64) MetricStats {
	if len(values) == 0 {
		return MetricStats{}
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)

	sum := 0.0
	for _, v := range sorted {
		sum += v
	}

	// Nearest-rank method: rank = ceil(0.95 * n), 1-indexed.
	idx := int(math.Ceil(0.95*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}

	return MetricStats{
		Min: sorted[0],
		Avg: sum / float64(len(sorted)),
		Max: sorted[len(sorted)-1],
		P95: sorted[idx],
	}
}

// BuildDigestMessage summarizes every sample in the period [since, until)
// as a min/avg/max/p95 table, one row per metric.
func BuildDigestMessage(samples []Sample, since, until time.Time) string {
	var cpu, mem, swap, iowait []float64
	diskValues := map[string][]float64{}
	for _, s := range samples {
		cpu = append(cpu, s.CPULoad1)
		mem = append(mem, s.MemAvailMB)
		swap = append(swap, s.SwapUsedMB)
		iowait = append(iowait, s.IOWaitPct)
		for mount, v := range s.Disks {
			diskValues[mount] = append(diskValues[mount], v)
		}
	}

	t := newTable("Metric", "Min", "Avg", "Max", "P95")
	addStatRow := func(label string, values []float64, unit string) {
		st := computeStats(values)
		t.addRow(label,
			formatNumber(st.Min, unit),
			formatNumber(st.Avg, unit),
			formatNumber(st.Max, unit),
			formatNumber(st.P95, unit),
		)
	}
	addStatRow("CPU load (1m)", cpu, "")
	addStatRow("Memory available", mem, " MB")
	addStatRow("Swap used", swap, " MB")
	addStatRow("I/O wait", iowait, "%")
	for _, mount := range sortedDiskMountKeys(diskValues) {
		addStatRow("Disk "+mount, diskValues[mount], "%")
	}

	header := fmt.Sprintf("**Hourly digest** — %s to %s UTC (%d samples)",
		since.UTC().Format("15:04"), until.UTC().Format("15:04"), len(samples))
	return header + "\n" + t.render()
}

// sortedDiskMountKeys sorts the keys of a mount->values map, "/boot" last.
func sortedDiskMountKeys(m map[string][]float64) []string {
	single := map[string]float64{}
	for k := range m {
		single[k] = 0
	}
	return sortedDiskMounts(single)
}
