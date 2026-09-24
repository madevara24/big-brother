package alert

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Sample is one tick's readings, appended as one JSON line to the sample
// log. There's no time-series database here (see README) -- this file,
// pruned to the retention window, is the only history the hourly digest
// has to work with.
type Sample struct {
	Timestamp  time.Time          `json:"ts"`
	CPULoad1   float64            `json:"cpu_load1"`
	MemAvailMB float64            `json:"mem_avail_mb"`
	SwapUsedMB float64            `json:"swap_used_mb"`
	IOWaitPct  float64            `json:"iowait_pct"`
	Disks      map[string]float64 `json:"disks"`
}

// AppendSample writes one sample as a JSON line to the sample log.
func AppendSample(path string, s Sample) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("opening sample log: %w", err)
	}
	defer f.Close()

	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("marshaling sample: %w", err)
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("writing sample: %w", err)
	}
	return nil
}

// ReadSamplesSince returns every sample at or after `since`. Lines that
// fail to parse (e.g. a partial write from a crash mid-append) are
// skipped rather than failing the whole read.
func ReadSamplesSince(path string, since time.Time) ([]Sample, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("opening sample log: %w", err)
	}
	defer f.Close()

	var out []Sample
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		var s Sample
		if err := json.Unmarshal(scanner.Bytes(), &s); err != nil {
			continue
		}
		if !s.Timestamp.Before(since) {
			out = append(out, s)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanning sample log: %w", err)
	}
	return out, nil
}

// PruneSamples rewrites the sample log keeping only samples at or after
// `cutoff`, so the file never grows past the retention window. Rewritten
// atomically (temp file + rename) like the state file.
func PruneSamples(path string, cutoff time.Time) error {
	kept, err := ReadSamplesSince(path, cutoff)
	if err != nil {
		return err
	}

	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("creating temp sample log: %w", err)
	}
	w := bufio.NewWriter(f)
	for _, s := range kept {
		data, err := json.Marshal(s)
		if err != nil {
			f.Close()
			return fmt.Errorf("marshaling sample: %w", err)
		}
		if _, err := w.Write(append(data, '\n')); err != nil {
			f.Close()
			return fmt.Errorf("writing pruned sample log: %w", err)
		}
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return fmt.Errorf("flushing pruned sample log: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("closing pruned sample log: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("renaming pruned sample log: %w", err)
	}
	return nil
}
