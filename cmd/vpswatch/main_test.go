package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/madevara24/big-brother/internal/alert"
	"github.com/madevara24/big-brother/internal/config"
)

func TestNextDigestBoundary(t *testing.T) {
	tests := []struct {
		name     string
		last     time.Time
		interval time.Duration
		want     time.Time
	}{
		{
			name:     "hourly interval",
			last:     mustTime("2026-01-01T13:00:00Z"),
			interval: time.Hour,
			want:     mustTime("2026-01-01T14:00:00Z"),
		},
		{
			name:     "sub-hour interval",
			last:     mustTime("2026-01-01T14:05:00Z"),
			interval: time.Minute,
			want:     mustTime("2026-01-01T14:06:00Z"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := nextDigestBoundary(tt.last, tt.interval)
			if !got.Equal(tt.want) {
				t.Errorf("nextDigestBoundary(%v, %v) = %v, want %v", tt.last, tt.interval, got, tt.want)
			}
		})
	}
}

func TestDigestDue(t *testing.T) {
	tests := []struct {
		name     string
		last     time.Time
		interval time.Duration
		now      time.Time
		want     bool
	}{
		{
			name:     "just before the boundary",
			last:     mustTime("2026-01-01T13:00:00Z"),
			interval: time.Hour,
			now:      mustTime("2026-01-01T13:59:59.999Z"),
			want:     false,
		},
		{
			name:     "exactly at the boundary",
			last:     mustTime("2026-01-01T13:00:00Z"),
			interval: time.Hour,
			now:      mustTime("2026-01-01T14:00:00Z"),
			want:     true,
		},
		{
			name:     "already sent this boundary",
			last:     mustTime("2026-01-01T14:00:00Z"),
			interval: time.Hour,
			now:      mustTime("2026-01-01T14:30:00Z"),
			want:     false,
		},
		{
			name:     "multi-boundary gap collapses to due",
			last:     mustTime("2026-01-01T10:00:00Z"),
			interval: time.Hour,
			now:      mustTime("2026-01-01T14:00:03Z"),
			want:     true,
		},
		{
			name:     "sub-hour interval not yet due",
			last:     mustTime("2026-01-01T14:05:00Z"),
			interval: time.Minute,
			now:      mustTime("2026-01-01T14:05:59Z"),
			want:     false,
		},
		{
			name:     "sub-hour interval due",
			last:     mustTime("2026-01-01T14:05:00Z"),
			interval: time.Minute,
			now:      mustTime("2026-01-01T14:06:00.5Z"),
			want:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := digestDue(tt.last, tt.interval, tt.now)
			if got != tt.want {
				t.Errorf("digestDue(%v, %v, %v) = %v, want %v", tt.last, tt.interval, tt.now, got, tt.want)
			}
		})
	}
}

func TestSendSetupMessage(t *testing.T) {
	const revA = "7f620bceec435e70704d488217a9c72ad12e4588"
	const revB = "8d581ef1234567890abcdef1234567890abcdef1"

	t.Run("no stored revision: sends once, stores full hash", func(t *testing.T) {
		var calls int
		var body string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			b, _ := io.ReadAll(r.Body)
			body = string(b)
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		st := &alert.State{}
		cfg := &config.Config{DiscordWebhookURL: srv.URL}

		sendSetupMessage(revA, st, cfg, alert.Sample{})

		if calls != 1 {
			t.Fatalf("expected exactly one discord call, got %d", calls)
		}
		if !strings.Contains(body, "build 7f620bc") {
			t.Errorf("expected body to contain %q, got %q", "build 7f620bc", body)
		}
		if st.AnnouncedRevision != revA {
			t.Errorf("AnnouncedRevision = %q, want %q", st.AnnouncedRevision, revA)
		}
	})

	t.Run("stored equals current: no send", func(t *testing.T) {
		var calls int
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		st := &alert.State{AnnouncedRevision: revA}
		cfg := &config.Config{DiscordWebhookURL: srv.URL}

		sendSetupMessage(revA, st, cfg, alert.Sample{})

		if calls != 0 {
			t.Errorf("expected no discord call, got %d", calls)
		}
		if st.AnnouncedRevision != revA {
			t.Errorf("AnnouncedRevision = %q, want unchanged %q", st.AnnouncedRevision, revA)
		}
	})

	t.Run("stored differs from current: sends once, updates to new hash", func(t *testing.T) {
		var calls int
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		st := &alert.State{AnnouncedRevision: revA}
		cfg := &config.Config{DiscordWebhookURL: srv.URL}

		sendSetupMessage(revB, st, cfg, alert.Sample{})

		if calls != 1 {
			t.Fatalf("expected exactly one discord call, got %d", calls)
		}
		if st.AnnouncedRevision != revB {
			t.Errorf("AnnouncedRevision = %q, want %q", st.AnnouncedRevision, revB)
		}
	})

	t.Run("webhook failure: stored revision unchanged, retries next tick", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		st := &alert.State{}
		cfg := &config.Config{DiscordWebhookURL: srv.URL}

		sendSetupMessage(revA, st, cfg, alert.Sample{})

		if st.AnnouncedRevision != "" {
			t.Errorf("AnnouncedRevision = %q, want unchanged empty string after send failure", st.AnnouncedRevision)
		}
	})
}

func mustTime(s string) time.Time {
	tm, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		panic(err)
	}
	return tm
}

func TestRunDigestIfDue(t *testing.T) {
	t.Run("not due: no send, state untouched", func(t *testing.T) {
		var calls int
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		dir := t.TempDir()
		samplesPath := filepath.Join(dir, "samples.log")
		last := mustTime("2026-01-01T13:00:00Z")
		st := &alert.State{LastDigestAt: last}
		cfg := &config.Config{DigestInterval: time.Hour, DiscordWebhookURL: srv.URL}
		now := mustTime("2026-01-01T13:59:59Z")

		runDigestIfDue(cfg, st, samplesPath, now)

		if calls != 0 {
			t.Errorf("expected no discord call, got %d", calls)
		}
		if !st.LastDigestAt.Equal(last) {
			t.Errorf("LastDigestAt = %v, want unchanged %v", st.LastDigestAt, last)
		}
	})

	t.Run("due: sends one digest, window tiles, clock snaps to boundary", func(t *testing.T) {
		var calls int
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		dir := t.TempDir()
		samplesPath := filepath.Join(dir, "samples.log")
		last := mustTime("2026-01-01T13:00:00Z")
		if err := alert.AppendSample(samplesPath, alert.Sample{Timestamp: mustTime("2026-01-01T13:30:00Z"), CPULoad1: 1}); err != nil {
			t.Fatal(err)
		}
		st := &alert.State{LastDigestAt: last}
		cfg := &config.Config{DigestInterval: time.Hour, DiscordWebhookURL: srv.URL}
		now := mustTime("2026-01-01T14:00:03Z")

		runDigestIfDue(cfg, st, samplesPath, now)

		if calls != 1 {
			t.Fatalf("expected exactly one discord call, got %d", calls)
		}
		wantBoundary := mustTime("2026-01-01T14:00:00Z")
		if !st.LastDigestAt.Equal(wantBoundary) {
			t.Errorf("LastDigestAt = %v, want boundary %v", st.LastDigestAt, wantBoundary)
		}

		// Next tick, well before the following boundary, must not fire again.
		runDigestIfDue(cfg, st, samplesPath, now.Add(time.Second))
		if calls != 1 {
			t.Errorf("expected still exactly one discord call after a same-boundary tick, got %d", calls)
		}
	})

	t.Run("multi-boundary gap collapses into a single catch-up digest", func(t *testing.T) {
		var calls int
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		dir := t.TempDir()
		samplesPath := filepath.Join(dir, "samples.log")
		last := mustTime("2026-01-01T10:00:00Z")
		if err := alert.AppendSample(samplesPath, alert.Sample{Timestamp: mustTime("2026-01-01T13:00:00Z"), CPULoad1: 1}); err != nil {
			t.Fatal(err)
		}
		st := &alert.State{LastDigestAt: last}
		cfg := &config.Config{DigestInterval: time.Hour, DiscordWebhookURL: srv.URL}
		now := mustTime("2026-01-01T14:00:03Z")

		runDigestIfDue(cfg, st, samplesPath, now)

		if calls != 1 {
			t.Fatalf("expected exactly one catch-up digest, got %d", calls)
		}
		wantBoundary := mustTime("2026-01-01T14:00:00Z")
		if !st.LastDigestAt.Equal(wantBoundary) {
			t.Errorf("LastDigestAt = %v, want boundary %v (alignment resumed)", st.LastDigestAt, wantBoundary)
		}

		// Alignment has resumed: the following boundary is 15:00, not another gap catch-up.
		if due := digestDue(st.LastDigestAt, cfg.DigestInterval, mustTime("2026-01-01T14:30:00Z")); due {
			t.Error("expected not due mid-hour after catch-up")
		}
	})

	t.Run("zero samples: clock advances to boundary without sending", func(t *testing.T) {
		var calls int
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		dir := t.TempDir()
		samplesPath := filepath.Join(dir, "samples.log")
		last := mustTime("2026-01-01T13:00:00Z")
		st := &alert.State{LastDigestAt: last}
		cfg := &config.Config{DigestInterval: time.Hour, DiscordWebhookURL: srv.URL}
		now := mustTime("2026-01-01T14:00:00Z")

		runDigestIfDue(cfg, st, samplesPath, now)

		if calls != 0 {
			t.Errorf("expected no discord call for zero samples, got %d", calls)
		}
		wantBoundary := mustTime("2026-01-01T14:00:00Z")
		if !st.LastDigestAt.Equal(wantBoundary) {
			t.Errorf("LastDigestAt = %v, want boundary %v", st.LastDigestAt, wantBoundary)
		}
	})

	t.Run("discord failure: clock does not advance, next tick retries", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		dir := t.TempDir()
		samplesPath := filepath.Join(dir, "samples.log")
		last := mustTime("2026-01-01T13:00:00Z")
		if err := alert.AppendSample(samplesPath, alert.Sample{Timestamp: mustTime("2026-01-01T13:30:00Z"), CPULoad1: 1}); err != nil {
			t.Fatal(err)
		}
		st := &alert.State{LastDigestAt: last}
		cfg := &config.Config{DigestInterval: time.Hour, DiscordWebhookURL: srv.URL}
		now := mustTime("2026-01-01T14:00:00Z")

		runDigestIfDue(cfg, st, samplesPath, now)

		if !st.LastDigestAt.Equal(last) {
			t.Errorf("LastDigestAt = %v, want unchanged %v after send failure", st.LastDigestAt, last)
		}
	})
}
