// Package config loads vpswatch's .env-file tunables.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Config holds every vpswatch tunable, defaulted per the values documented
// in .env.example.
type Config struct {
	Tick     time.Duration
	Debounce int

	HealthchecksPingURL string
	DiscordWebhookURL   string

	CPULoadAlert float64
	CPULoadWarn  float64

	MemAvailAlertMB float64
	MemAvailWarnMB  float64

	SwapUsedAlertMB float64
	SwapUsedWarnMB  float64

	DiskUsedAlertPct float64
	DiskUsedWarnPct  float64

	BootUsedAlertPct float64
	BootUsedWarnPct  float64

	IOWaitAlertPct float64
	IOWaitWarnPct  float64

	WarnEnabled bool

	// DigestInterval and SampleRetention aren't in the proposed .env key
	// list, but both need to be shrinkable to test the hourly digest
	// without waiting an hour (see README "How to test").
	DigestInterval  time.Duration
	SampleRetention time.Duration
}

// Load reads configDir/.env (if present) and layers process environment
// variables and hardcoded defaults underneath it. A missing .env file is
// not an error by itself -- the values may already be in the process
// environment (e.g. systemd EnvironmentFile) -- but the two secret URLs
// are required one way or another, since a watcher that can't ping or
// alert isn't doing its job.
func Load(configDir string) (*Config, error) {
	fileVals, err := parseEnvFile(filepath.Join(configDir, ".env"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	get := func(key, def string) string {
		if v, ok := fileVals[key]; ok && v != "" {
			return v
		}
		if v := os.Getenv(key); v != "" {
			return v
		}
		return def
	}

	var errs []string
	getFloat := func(key, def string) float64 {
		v, err := strconv.ParseFloat(get(key, def), 64)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", key, err))
		}
		return v
	}
	getInt := func(key, def string) int {
		v, err := strconv.Atoi(get(key, def))
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", key, err))
		}
		return v
	}
	getBool := func(key, def string) bool {
		v, err := strconv.ParseBool(get(key, def))
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", key, err))
		}
		return v
	}

	cfg := &Config{}
	cfg.Tick = time.Duration(getInt("VPSWATCH_TICK", "60")) * time.Second
	cfg.Debounce = getInt("VPSWATCH_DEBOUNCE", "5")

	cfg.HealthchecksPingURL = get("VPSWATCH_HEALTHCHECKS_PING_URL", "")
	cfg.DiscordWebhookURL = get("VPSWATCH_DISCORD_WEBHOOK_URL", "")

	cfg.CPULoadAlert = getFloat("VPSWATCH_CPU_LOAD_ALERT", "6.0")
	cfg.CPULoadWarn = getFloat("VPSWATCH_CPU_LOAD_WARN", "4.0")

	cfg.MemAvailAlertMB = getFloat("VPSWATCH_MEM_AVAIL_ALERT_MB", "1024")
	cfg.MemAvailWarnMB = getFloat("VPSWATCH_MEM_AVAIL_WARN_MB", "2048")

	cfg.SwapUsedAlertMB = getFloat("VPSWATCH_SWAP_USED_ALERT_MB", "1024")
	cfg.SwapUsedWarnMB = getFloat("VPSWATCH_SWAP_USED_WARN_MB", "512")

	cfg.DiskUsedAlertPct = getFloat("VPSWATCH_DISK_USED_ALERT_PCT", "90")
	cfg.DiskUsedWarnPct = getFloat("VPSWATCH_DISK_USED_WARN_PCT", "80")

	cfg.BootUsedAlertPct = getFloat("VPSWATCH_BOOT_USED_ALERT_PCT", "80")
	cfg.BootUsedWarnPct = getFloat("VPSWATCH_BOOT_USED_WARN_PCT", "70")

	cfg.IOWaitAlertPct = getFloat("VPSWATCH_IOWAIT_ALERT_PCT", "25")
	cfg.IOWaitWarnPct = getFloat("VPSWATCH_IOWAIT_WARN_PCT", "15")

	cfg.WarnEnabled = getBool("VPSWATCH_WARN_ENABLED", "false")

	cfg.DigestInterval = time.Duration(getInt("VPSWATCH_DIGEST_INTERVAL_MINUTES", "60")) * time.Minute
	cfg.SampleRetention = time.Duration(getInt("VPSWATCH_SAMPLE_RETENTION_MINUTES", "60")) * time.Minute

	if cfg.HealthchecksPingURL == "" {
		errs = append(errs, "VPSWATCH_HEALTHCHECKS_PING_URL is required")
	}
	if cfg.DiscordWebhookURL == "" {
		errs = append(errs, "VPSWATCH_DISCORD_WEBHOOK_URL is required")
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("invalid config: %s", strings.Join(errs, "; "))
	}

	return cfg, nil
}

// parseEnvFile reads plain KEY=VALUE lines (comments and blank lines
// ignored, matched surrounding quotes stripped) -- same minimal format
// used across this project's sibling tools.
func parseEnvFile(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, "=")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
				val = val[1 : len(val)-1]
			}
		}
		out[key] = val
	}
	return out, nil
}
