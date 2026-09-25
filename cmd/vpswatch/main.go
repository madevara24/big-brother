// Command vpswatch is a short-lived tick: collect host resource metrics,
// heartbeat to healthchecks.io, alert to Discord on sustained bad
// metrics, and send an hourly digest. Invoked once per minute by
// systemd/vpswatch.timer -- see README.md for the full design.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"

	"github.com/madevara24/big-brother/internal/alert"
	"github.com/madevara24/big-brother/internal/config"
	"github.com/madevara24/big-brother/internal/metrics"
)

// currentRevision returns the git commit hash the running binary was
// built from, or "" if build info isn't available (e.g. `go run`).
func currentRevision() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" {
				return s.Value
			}
		}
	}
	return ""
}

func main() {
	configDir := flag.String("config-dir", defaultConfigDir(), "directory containing .env, state.json, and samples.log")
	flag.Parse()

	cfg, err := config.Load(*configDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "vpswatch: config: %v\n", err)
		os.Exit(1)
	}

	rev := currentRevision()

	statePath := filepath.Join(*configDir, "state.json")
	samplesPath := filepath.Join(*configDir, "samples.log")

	_, statErr := os.Stat(statePath)
	firstRun := os.IsNotExist(statErr)

	st, err := alert.LoadState(statePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "vpswatch: state: %v\n", err)
		os.Exit(1)
	}

	now := time.Now().UTC()

	// On a brand-new install, don't fire the hourly digest right alongside
	// the setup message with a single sample in it -- seed the digest
	// clock to the current interval boundary instead, so the first real
	// digest still lands on the next hour mark (not offset by whatever
	// minute the process first happened to start at).
	if firstRun {
		st.LastDigestAt = now.Truncate(cfg.DigestInterval)
	}

	reading, warnings := collect(st.PrevCPUStat)
	for _, w := range warnings {
		fmt.Fprintf(os.Stderr, "vpswatch: %v\n", w)
	}

	// Heartbeat is unconditional -- it must fire even if metric collection
	// above partially failed or a metric is over threshold below. "No
	// ping" must mean "the watcher is dead," never "something's degraded."
	if err := alert.PingHealthchecks(cfg.HealthchecksPingURL); err != nil {
		fmt.Fprintf(os.Stderr, "vpswatch: healthchecks ping: %v\n", err)
	}

	sample := reading.toSample(now)
	if err := alert.AppendSample(samplesPath, sample); err != nil {
		fmt.Fprintf(os.Stderr, "vpswatch: append sample: %v\n", err)
	}
	if err := alert.PruneSamples(samplesPath, now.Add(-cfg.SampleRetention)); err != nil {
		fmt.Fprintf(os.Stderr, "vpswatch: prune samples: %v\n", err)
	}

	sendSetupMessage(rev, st, cfg, sample)

	for _, c := range buildChecks(cfg, reading) {
		ms := st.GetMetricState(c.Key)
		ms, outcome := alert.EvaluateMetric(ms, c, cfg.Debounce)
		st.SetMetricState(c.Key, ms)

		switch {
		case outcome.AlertFired:
			send(cfg, alert.BuildAlertMessage(c, cfg.Debounce))
		case outcome.AlertCleared:
			send(cfg, alert.BuildRecoveryMessage(c))
		}
		if outcome.WarnFired {
			send(cfg, alert.BuildWarnMessage(c))
		}
	}

	runDigestIfDue(cfg, st, samplesPath, now)

	st.PrevCPUStat = &reading.cpuStat
	if err := alert.SaveState(statePath, st); err != nil {
		fmt.Fprintf(os.Stderr, "vpswatch: save state: %v\n", err)
		os.Exit(1)
	}
}

func send(cfg *config.Config, msg string) {
	if err := alert.SendDiscordMessage(cfg.DiscordWebhookURL, msg); err != nil {
		fmt.Fprintf(os.Stderr, "vpswatch: discord message: %v\n", err)
	}
}

// sendSetupMessage sends the setup banner whenever rev (the running
// binary's git commit hash) hasn't been announced yet -- first run, or
// a redeploy from a new commit. The stored revision is only updated on
// a successful send, so a failed POST retries next tick.
func sendSetupMessage(rev string, st *alert.State, cfg *config.Config, sample alert.Sample) {
	if st.AnnouncedRevision == rev {
		return
	}
	msg := alert.BuildSetupMessage(sample, rev)
	if err := alert.SendDiscordMessage(cfg.DiscordWebhookURL, msg); err != nil {
		fmt.Fprintf(os.Stderr, "vpswatch: setup message: %v\n", err)
		return
	}
	st.AnnouncedRevision = rev
}

// nextDigestBoundary returns the next interval-aligned instant at or
// after which a digest covering [last, that instant) is due. last is
// itself expected to already be interval-aligned -- State.LastDigestAt
// only ever holds boundaries, never wall-clock send times -- so this is
// just "one interval past the last boundary", not a fresh truncation.
func nextDigestBoundary(last time.Time, interval time.Duration) time.Time {
	return last.Add(interval)
}

// digestDue reports whether now has reached or passed the next digest
// boundary after last. A gap of several missed boundaries (box was off,
// cron stalled) still reports due exactly once here; runDigestIfDue is
// what collapses that gap into a single catch-up digest.
func digestDue(last time.Time, interval time.Duration, now time.Time) bool {
	return !now.Before(nextDigestBoundary(last, interval))
}

// runDigestIfDue sends the digest once the tick reaches the next
// interval-aligned boundary after the last one sent, so digests land on
// the hour (or minute, for VPSWATCH_DIGEST_INTERVAL_MINUTES=1 testing)
// rather than wherever the process first happened to start. If several
// boundaries were missed (box was off, cron stalled), this sends one
// catch-up digest covering the whole gap and then snaps LastDigestAt to
// now's own boundary, so normal alignment resumes on the next tick
// instead of a backlog of digests draining out one per tick.
func runDigestIfDue(cfg *config.Config, st *alert.State, samplesPath string, now time.Time) {
	if !digestDue(st.LastDigestAt, cfg.DigestInterval, now) {
		return
	}

	since := st.LastDigestAt
	boundary := now.Truncate(cfg.DigestInterval)

	samples, err := alert.ReadSamplesSince(samplesPath, since)
	if err != nil {
		fmt.Fprintf(os.Stderr, "vpswatch: read samples for digest: %v\n", err)
		return
	}
	if len(samples) == 0 {
		st.LastDigestAt = boundary
		return
	}

	msg := alert.BuildDigestMessage(samples, since, now)
	if err := alert.SendDiscordMessage(cfg.DiscordWebhookURL, msg); err != nil {
		fmt.Fprintf(os.Stderr, "vpswatch: digest message: %v\n", err)
		return
	}
	st.LastDigestAt = boundary
}

// defaultConfigDir mirrors the sibling pmrunner tool's convention:
// $VPSWATCH_CONFIG_DIR if set, else the current working directory.
func defaultConfigDir() string {
	if d := os.Getenv("VPSWATCH_CONFIG_DIR"); d != "" {
		return d
	}
	return "."
}

// reading is this tick's raw metric collection, kept separate from
// alert.Sample (the on-disk record shape) so metrics-package types don't
// leak into the alert package.
type reading struct {
	cpuLoad1   float64
	memAvailMB float64
	swapUsedMB float64
	iowaitPct  float64
	disks      []metrics.DiskUsage
	cpuStat    metrics.CPUStat
}

func (r reading) toSample(ts time.Time) alert.Sample {
	disks := make(map[string]float64, len(r.disks))
	for _, d := range r.disks {
		disks[d.MountPoint] = d.UsedPct
	}
	return alert.Sample{
		Timestamp:  ts,
		CPULoad1:   r.cpuLoad1,
		MemAvailMB: r.memAvailMB,
		SwapUsedMB: r.swapUsedMB,
		IOWaitPct:  r.iowaitPct,
		Disks:      disks,
	}
}

// collect gathers every metric best-effort: a failure reading one metric
// doesn't stop the others, since a partial tick (with the unconditional
// heartbeat still firing) beats no tick at all. prevCPUStat is nil on the
// very first-ever tick, in which case iowaitPct is reported as 0 (no
// baseline to delta against yet).
func collect(prevCPUStat *metrics.CPUStat) (reading, []error) {
	var r reading
	var errs []error

	load1, err := metrics.LoadAvg1()
	if err != nil {
		errs = append(errs, err)
	}
	r.cpuLoad1 = load1

	memInfo, err := metrics.ReadMemInfo()
	if err != nil {
		errs = append(errs, err)
	}
	r.memAvailMB = memInfo.MemAvailableMB()
	r.swapUsedMB = memInfo.SwapUsedMB()

	disks, err := metrics.ReadDiskUsage()
	if err != nil {
		errs = append(errs, err)
	}
	r.disks = disks

	cpuStat, err := metrics.ReadCPUStat()
	if err != nil {
		errs = append(errs, err)
	}
	r.cpuStat = cpuStat
	if prevCPUStat != nil {
		r.iowaitPct = metrics.IOWaitPercent(*prevCPUStat, cpuStat)
	}

	return r, errs
}

// buildChecks turns this tick's readings into the alert.Check list to
// evaluate, applying each metric's configured thresholds -- /boot gets
// its own (usually stricter) disk thresholds, per the task spec.
func buildChecks(cfg *config.Config, r reading) []alert.Check {
	checks := []alert.Check{
		{
			Key: "cpu_load1", Label: "CPU load (1m)",
			Value: r.cpuLoad1, Unit: "", Direction: alert.HigherIsBad,
			Alert: cfg.CPULoadAlert, Warn: cfg.CPULoadWarn, WarnEnabled: cfg.WarnEnabled,
		},
		{
			Key: "mem_avail", Label: "Memory available",
			Value: r.memAvailMB, Unit: " MB", Direction: alert.LowerIsBad,
			Alert: cfg.MemAvailAlertMB, Warn: cfg.MemAvailWarnMB, WarnEnabled: cfg.WarnEnabled,
		},
		{
			Key: "swap_used", Label: "Swap used",
			Value: r.swapUsedMB, Unit: " MB", Direction: alert.HigherIsBad,
			Alert: cfg.SwapUsedAlertMB, Warn: cfg.SwapUsedWarnMB, WarnEnabled: cfg.WarnEnabled,
		},
		{
			Key: "iowait", Label: "I/O wait",
			Value: r.iowaitPct, Unit: "%", Direction: alert.HigherIsBad,
			Alert: cfg.IOWaitAlertPct, Warn: cfg.IOWaitWarnPct, WarnEnabled: cfg.WarnEnabled,
		},
	}

	for _, d := range r.disks {
		alertPct, warnPct := cfg.DiskUsedAlertPct, cfg.DiskUsedWarnPct
		if d.MountPoint == "/boot" {
			alertPct, warnPct = cfg.BootUsedAlertPct, cfg.BootUsedWarnPct
		}
		checks = append(checks, alert.Check{
			Key: "disk:" + d.MountPoint, Label: "Disk " + d.MountPoint,
			Value: d.UsedPct, Unit: "%", Direction: alert.HigherIsBad,
			Alert: alertPct, Warn: warnPct, WarnEnabled: cfg.WarnEnabled,
		})
	}

	return checks
}
