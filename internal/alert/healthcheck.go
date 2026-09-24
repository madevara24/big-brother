package alert

import (
	"fmt"
	"net/http"
	"time"
)

// PingHealthchecks fires the healthchecks.io heartbeat. This must happen
// every tick unconditionally, even when a metric is over threshold --
// "no ping" is healthchecks.io's own signal that the watcher process
// itself is dead, and must never be confused with "a metric is degraded."
func PingHealthchecks(pingURL string) error {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(pingURL)
	if err != nil {
		return fmt.Errorf("pinging healthchecks: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("healthchecks ping returned status %d", resp.StatusCode)
	}
	return nil
}
