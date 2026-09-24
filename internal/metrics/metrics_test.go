package metrics

import "testing"

func TestUnescapeMountField(t *testing.T) {
	cases := map[string]string{
		`/mnt/my\040drive`: "/mnt/my drive",
		`/normal/path`:     "/normal/path",
		`/tab\011here`:     "/tab\there",
	}
	for in, want := range cases {
		if got := unescapeMountField(in); got != want {
			t.Errorf("unescapeMountField(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIOWaitPercent(t *testing.T) {
	prev := CPUStat{User: 100, Nice: 0, System: 50, Idle: 800, IOWait: 20, IRQ: 0, SoftIRQ: 0, Steal: 0}
	// One tick later: total jiffies moved by (50+20+50+10)=130, 10 of them iowait.
	cur := CPUStat{User: 150, Nice: 0, System: 70, Idle: 850, IOWait: 30, IRQ: 0, SoftIRQ: 0, Steal: 0}

	got := IOWaitPercent(prev, cur)
	want := 10.0 / 130.0 * 100 // 10 iowait jiffies / 130 total jiffies * 100
	if got != want {
		t.Errorf("IOWaitPercent = %v, want %v", got, want)
	}
}

func TestIOWaitPercent_NoMovementOrCounterReset(t *testing.T) {
	prev := CPUStat{User: 100, Idle: 800, IOWait: 20}

	if got := IOWaitPercent(prev, prev); got != 0 {
		t.Errorf("IOWaitPercent with no elapsed time = %v, want 0", got)
	}

	reset := CPUStat{User: 10, Idle: 80, IOWait: 2} // counters went backwards (reboot)
	if got := IOWaitPercent(prev, reset); got != 0 {
		t.Errorf("IOWaitPercent after counter reset = %v, want 0", got)
	}
}

// The following are smoke tests against the real /proc filesystem --
// vpswatch only runs on Linux, so there is no fake-/proc fixture here,
// just a check that parsing doesn't error and returns plausible values.

func TestLoadAvg1_Smoke(t *testing.T) {
	v, err := LoadAvg1()
	if err != nil {
		t.Fatalf("LoadAvg1: %v", err)
	}
	if v < 0 {
		t.Errorf("LoadAvg1 = %v, want >= 0", v)
	}
}

func TestReadMemInfo_Smoke(t *testing.T) {
	info, err := ReadMemInfo()
	if err != nil {
		t.Fatalf("ReadMemInfo: %v", err)
	}
	if info.MemAvailableKB == 0 {
		t.Errorf("MemAvailableKB = 0, expected a nonzero reading on a real host")
	}
}

func TestReadDiskUsage_Smoke(t *testing.T) {
	disks, err := ReadDiskUsage()
	if err != nil {
		t.Fatalf("ReadDiskUsage: %v", err)
	}
	foundRoot := false
	for _, d := range disks {
		if d.MountPoint == "/" {
			foundRoot = true
		}
		if d.UsedPct < 0 || d.UsedPct > 100 {
			t.Errorf("disk %s used pct = %v, want 0..100", d.MountPoint, d.UsedPct)
		}
	}
	if !foundRoot {
		t.Errorf("expected \"/\" to be among reported mounts, got %+v", disks)
	}
}

func TestReadCPUStat_Smoke(t *testing.T) {
	if _, err := ReadCPUStat(); err != nil {
		t.Fatalf("ReadCPUStat: %v", err)
	}
}
