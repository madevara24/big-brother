package alert

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/madevara24/big-brother/internal/metrics"
)

// MetricState is the per-metric dedupe bookkeeping vpswatch needs to turn
// "bad every tick" into "one alert, then one recovery" -- and, for the
// warn tier, "one warning, no repeats."
type MetricState struct {
	ConsecutiveBad int  `json:"consecutive_bad"`
	Alerting       bool `json:"alerting"`
	WarnSent       bool `json:"warn_sent"`
}

// State is everything vpswatch must remember between ticks. Each tick is
// a short-lived process invoked by the systemd timer, so this file (not
// process memory) is the only place this can live.
type State struct {
	SetupMessageSent bool `json:"setup_message_sent"`
	// LastDigestAt is the interval-aligned boundary (see
	// cmd/vpswatch's nextDigestBoundary) the last digest covered up to
	// -- e.g. 14:00:00 for an hourly digest, never the wall-clock
	// moment the message was actually sent. Storing the boundary
	// rather than the send time is what makes the next boundary a
	// pure function of this value and lets digests land on the hour
	// (or minute, for testing) regardless of when a tick happens to
	// fire or how long the process was stopped in between.
	LastDigestAt time.Time              `json:"last_digest_at"`
	PrevCPUStat  *metrics.CPUStat       `json:"prev_cpu_stat,omitempty"`
	Metrics      map[string]MetricState `json:"metrics"`
}

// GetMetricState returns the stored state for key, or a zero-value
// MetricState if it's never been seen before.
func (s *State) GetMetricState(key string) MetricState {
	return s.Metrics[key]
}

// SetMetricState stores the (possibly mutated) state for key.
func (s *State) SetMetricState(key string, ms MetricState) {
	if s.Metrics == nil {
		s.Metrics = map[string]MetricState{}
	}
	s.Metrics[key] = ms
}

// LoadState reads the state file, returning a fresh zero-value State
// (not an error) if it doesn't exist yet -- that's exactly the "first
// run" case the setup message needs to detect.
func LoadState(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &State{Metrics: map[string]MetricState{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading state file: %w", err)
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parsing state file: %w", err)
	}
	if s.Metrics == nil {
		s.Metrics = map[string]MetricState{}
	}
	return &s, nil
}

// SaveState writes the state file atomically (write to a temp file, then
// rename) so a crash mid-write can't corrupt it -- the next tick would
// otherwise be unable to load its dedupe state at all.
func SaveState(path string, s *State) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling state: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("writing temp state file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("renaming temp state file: %w", err)
	}
	return nil
}
