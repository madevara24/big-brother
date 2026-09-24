// Package metrics reads host resource stats directly from /proc and via
// statfs(2) -- no third-party gopsutil-style dependency, since vpswatch is
// stdlib-only by design (see repo README).
package metrics

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
)

// LoadAvg1 returns the 1-minute load average from /proc/loadavg.
func LoadAvg1() (float64, error) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, fmt.Errorf("reading /proc/loadavg: %w", err)
	}
	fields := strings.Fields(string(data))
	if len(fields) < 1 {
		return 0, fmt.Errorf("unexpected /proc/loadavg format: %q", string(data))
	}
	v, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, fmt.Errorf("parsing /proc/loadavg field 0 (%q): %w", fields[0], err)
	}
	return v, nil
}

// MemInfo is the subset of /proc/meminfo values (in kB, as reported) that
// vpswatch cares about.
type MemInfo struct {
	MemAvailableKB uint64
	SwapTotalKB    uint64
	SwapFreeKB     uint64
}

// ReadMemInfo parses /proc/meminfo for MemAvailable/SwapTotal/SwapFree.
// Every /proc/meminfo line vpswatch reads is reported in kB, so no
// per-line unit handling is needed.
func ReadMemInfo() (MemInfo, error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return MemInfo{}, fmt.Errorf("opening /proc/meminfo: %w", err)
	}
	defer f.Close()

	var info MemInfo
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		key := strings.TrimSuffix(fields[0], ":")
		val, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch key {
		case "MemAvailable":
			info.MemAvailableKB = val
		case "SwapTotal":
			info.SwapTotalKB = val
		case "SwapFree":
			info.SwapFreeKB = val
		}
	}
	if err := scanner.Err(); err != nil {
		return MemInfo{}, fmt.Errorf("scanning /proc/meminfo: %w", err)
	}
	return info, nil
}

// MemAvailableMB is MemAvailable converted from kB to MB.
func (m MemInfo) MemAvailableMB() float64 {
	return float64(m.MemAvailableKB) / 1024.0
}

// SwapUsedMB is (SwapTotal - SwapFree) converted from kB to MB.
func (m MemInfo) SwapUsedMB() float64 {
	if m.SwapTotalKB < m.SwapFreeKB {
		return 0
	}
	return float64(m.SwapTotalKB-m.SwapFreeKB) / 1024.0
}

// DiskUsage is the used-space percentage for one mount point.
type DiskUsage struct {
	MountPoint string
	UsedPct    float64
}

// realFSTypes are the filesystems vpswatch reports disk usage for --
// pseudo/virtual filesystems (proc, sysfs, tmpfs, cgroup, overlay, ...)
// are skipped since statfs on them is meaningless or misleading for a
// "disk is filling up" alert.
var realFSTypes = map[string]bool{
	"ext2": true, "ext3": true, "ext4": true,
	"xfs": true, "btrfs": true, "zfs": true,
	"vfat": true, "exfat": true, "ntfs": true, "ntfs3": true,
	"f2fs": true, "reiserfs": true, "jfs": true,
}

// ReadDiskUsage lists real (non-pseudo) mounted filesystems from
// /proc/mounts and reports used-space percentage for each, computed the
// same way `df` does: used / (used + available-to-non-root).
// Mounts sharing the same backing device (bind mounts, duplicate
// remounts) are reported once.
func ReadDiskUsage() ([]DiskUsage, error) {
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return nil, fmt.Errorf("opening /proc/mounts: %w", err)
	}
	defer f.Close()

	seenDevices := map[string]bool{}
	var out []DiskUsage
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 3 {
			continue
		}
		device, mountPoint, fsType := fields[0], unescapeMountField(fields[1]), fields[2]
		if !realFSTypes[fsType] {
			continue
		}
		if seenDevices[device] {
			continue
		}
		seenDevices[device] = true

		var stat syscall.Statfs_t
		if err := syscall.Statfs(mountPoint, &stat); err != nil {
			continue
		}
		total := stat.Blocks * uint64(stat.Bsize)
		free := stat.Bfree * uint64(stat.Bsize)
		avail := stat.Bavail * uint64(stat.Bsize)
		if free > total {
			continue
		}
		used := total - free
		denom := used + avail
		var pct float64
		if denom > 0 {
			pct = float64(used) / float64(denom) * 100
		}
		out = append(out, DiskUsage{MountPoint: mountPoint, UsedPct: pct})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanning /proc/mounts: %w", err)
	}
	return out, nil
}

// unescapeMountField decodes the octal escapes (spaces, tabs, etc.) that
// the kernel uses in /proc/mounts fields.
func unescapeMountField(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if v, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(v))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// CPUStat is the raw jiffie counters from the first ("cpu") line of
// /proc/stat, kept across ticks (via the state file) so IOWaitPercent can
// compute a delta -- a single short-lived tick process has no in-memory
// "previous" reading of its own.
type CPUStat struct {
	User, Nice, System, Idle, IOWait, IRQ, SoftIRQ, Steal uint64
}

// ReadCPUStat parses the aggregate "cpu" line of /proc/stat.
func ReadCPUStat() (CPUStat, error) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return CPUStat{}, fmt.Errorf("opening /proc/stat: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		return CPUStat{}, fmt.Errorf("empty /proc/stat")
	}
	fields := strings.Fields(scanner.Text())
	if len(fields) < 5 || fields[0] != "cpu" {
		return CPUStat{}, fmt.Errorf("unexpected /proc/stat format: %q", scanner.Text())
	}
	vals := make([]uint64, 8)
	for i := 0; i < 8 && i+1 < len(fields); i++ {
		v, err := strconv.ParseUint(fields[i+1], 10, 64)
		if err != nil {
			return CPUStat{}, fmt.Errorf("parsing /proc/stat field %d (%q): %w", i+1, fields[i+1], err)
		}
		vals[i] = v
	}
	return CPUStat{
		User: vals[0], Nice: vals[1], System: vals[2], Idle: vals[3],
		IOWait: vals[4], IRQ: vals[5], SoftIRQ: vals[6], Steal: vals[7],
	}, nil
}

// IOWaitPercent is the percentage of total CPU time spent in iowait
// between two /proc/stat snapshots. Returns 0 if the counters didn't
// move forward (e.g. first-ever tick with a zero-value prev, or a
// reboot reset the counters).
func IOWaitPercent(prev, cur CPUStat) float64 {
	prevTotal := prev.User + prev.Nice + prev.System + prev.Idle + prev.IOWait + prev.IRQ + prev.SoftIRQ + prev.Steal
	curTotal := cur.User + cur.Nice + cur.System + cur.Idle + cur.IOWait + cur.IRQ + cur.SoftIRQ + cur.Steal
	if curTotal <= prevTotal || cur.IOWait < prev.IOWait {
		return 0
	}
	totalDelta := float64(curTotal - prevTotal)
	iowaitDelta := float64(cur.IOWait - prev.IOWait)
	return iowaitDelta / totalDelta * 100
}
