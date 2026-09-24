package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeEnv(t *testing.T, dir, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(contents), 0o600); err != nil {
		t.Fatalf("writing .env: %v", err)
	}
}

func TestLoad_Defaults(t *testing.T) {
	dir := t.TempDir()
	writeEnv(t, dir, `
VPSWATCH_HEALTHCHECKS_PING_URL=https://hc-ping.com/abc
VPSWATCH_DISCORD_WEBHOOK_URL=https://discord.example/webhook
`)

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Tick != 60*time.Second {
		t.Errorf("Tick = %v, want 60s", cfg.Tick)
	}
	if cfg.Debounce != 5 {
		t.Errorf("Debounce = %v, want 5", cfg.Debounce)
	}
	if cfg.CPULoadAlert != 6.0 {
		t.Errorf("CPULoadAlert = %v, want 6.0", cfg.CPULoadAlert)
	}
	if cfg.WarnEnabled {
		t.Errorf("WarnEnabled = true, want false by default")
	}
	if cfg.DigestInterval != time.Hour {
		t.Errorf("DigestInterval = %v, want 1h", cfg.DigestInterval)
	}
}

func TestLoad_Overrides(t *testing.T) {
	dir := t.TempDir()
	writeEnv(t, dir, `
VPSWATCH_HEALTHCHECKS_PING_URL=https://hc-ping.com/abc
VPSWATCH_DISCORD_WEBHOOK_URL=https://discord.example/webhook
VPSWATCH_TICK=30
VPSWATCH_DEBOUNCE=3
VPSWATCH_CPU_LOAD_ALERT=2.5
VPSWATCH_WARN_ENABLED=true
VPSWATCH_DIGEST_INTERVAL_MINUTES=1
`)

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Tick != 30*time.Second {
		t.Errorf("Tick = %v, want 30s", cfg.Tick)
	}
	if cfg.Debounce != 3 {
		t.Errorf("Debounce = %v, want 3", cfg.Debounce)
	}
	if cfg.CPULoadAlert != 2.5 {
		t.Errorf("CPULoadAlert = %v, want 2.5", cfg.CPULoadAlert)
	}
	if !cfg.WarnEnabled {
		t.Errorf("WarnEnabled = false, want true")
	}
	if cfg.DigestInterval != time.Minute {
		t.Errorf("DigestInterval = %v, want 1m", cfg.DigestInterval)
	}
}

func TestLoad_MissingRequiredURLs(t *testing.T) {
	dir := t.TempDir()
	writeEnv(t, dir, `VPSWATCH_TICK=60`)

	if _, err := Load(dir); err == nil {
		t.Fatalf("expected an error when the required webhook/ping URLs are missing")
	}
}

func TestLoad_QuotedValues(t *testing.T) {
	dir := t.TempDir()
	writeEnv(t, dir, `
VPSWATCH_HEALTHCHECKS_PING_URL="https://hc-ping.com/abc"
VPSWATCH_DISCORD_WEBHOOK_URL='https://discord.example/webhook'
`)

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HealthchecksPingURL != "https://hc-ping.com/abc" {
		t.Errorf("HealthchecksPingURL = %q, quotes not stripped", cfg.HealthchecksPingURL)
	}
	if cfg.DiscordWebhookURL != "https://discord.example/webhook" {
		t.Errorf("DiscordWebhookURL = %q, quotes not stripped", cfg.DiscordWebhookURL)
	}
}
