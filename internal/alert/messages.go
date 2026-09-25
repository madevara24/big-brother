package alert

import "fmt"

// formatValue renders a Check's current reading, e.g. "0.42", "512 MB", "73.2%".
func formatValue(c Check) string {
	return formatNumber(c.Value, c.Unit)
}

func formatNumber(v float64, unit string) string {
	switch unit {
	case "%":
		return fmt.Sprintf("%.1f%%", v)
	case " MB":
		return fmt.Sprintf("%.0f MB", v)
	default:
		return fmt.Sprintf("%.2f", v)
	}
}

// BuildSetupMessage is the "Big Brother is Watching You(r VPS)" message
// sent whenever the running binary's revision hasn't been announced yet
// (first run, or a redeploy from a new commit), with a table of current
// metric readings.
func BuildSetupMessage(s Sample, revision string) string {
	t := newTable("Metric", "Current")
	t.addRow("CPU load (1m)", formatNumber(s.CPULoad1, ""))
	t.addRow("Memory available", formatNumber(s.MemAvailMB, " MB"))
	t.addRow("Swap used", formatNumber(s.SwapUsedMB, " MB"))
	t.addRow("I/O wait", formatNumber(s.IOWaitPct, "%"))
	for _, mount := range sortedDiskMounts(s.Disks) {
		t.addRow("Disk "+mount, formatNumber(s.Disks[mount], "%"))
	}
	return "**Big Brother is Watching You(r VPS)** (build " + shortHash(revision) + ")\n" + t.render()
}

// shortHash returns the first 7 characters of a git commit hash, for
// display only -- the full hash is what's stored and compared.
func shortHash(revision string) string {
	if len(revision) <= 7 {
		return revision
	}
	return revision[:7]
}

// BuildAlertMessage is sent once, the tick a metric first hits `debounce`
// consecutive bad checks.
func BuildAlertMessage(c Check, debounce int) string {
	return fmt.Sprintf("\U0001F534 **ALERT** %s is %s (threshold %s) for %d consecutive checks",
		c.Label, formatValue(c), formatNumber(c.Alert, c.Unit), debounce)
}

// BuildRecoveryMessage is sent once, the first tick a metric is no
// longer bad after having alerted.
func BuildRecoveryMessage(c Check) string {
	return fmt.Sprintf("✅ **RECOVERED** %s is back to normal: %s", c.Label, formatValue(c))
}

// BuildWarnMessage is sent once per warn incident (see EvaluateMetric).
func BuildWarnMessage(c Check) string {
	return fmt.Sprintf("\U0001F7E1 **WARN** %s is %s (warn threshold %s)",
		c.Label, formatValue(c), formatNumber(c.Warn, c.Unit))
}
