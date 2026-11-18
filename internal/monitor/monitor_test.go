package monitor

import (
	"math"
	"strings"
	"testing"
)

func sample(cpu, net string) string {
	return strings.Join([]string{
		"@@stat", "cpu  " + cpu,
		"@@mem",
		"MemTotal:        8000000 kB",
		"MemFree:          500000 kB",
		"MemAvailable:    4000000 kB",
		"SwapTotal:       2000000 kB",
		"SwapFree:        1500000 kB",
		"@@net",
		"Inter-|   Receive                                                |  Transmit",
		" face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed",
		"    lo: 99999 10 0 0 0 0 0 0 99999 10 0 0 0 0 0 0",
		"enp2s0: " + net,
		"docker0: 5000 1 0 0 0 0 0 0 5000 1 0 0 0 0 0 0",
		"vethab12: 5000 1 0 0 0 0 0 0 5000 1 0 0 0 0 0 0",
		"@@up", "16761600.12 1000.00",
		"@@load", "1.60 1.20 0.90 2/253 12345",
		"@@df",
		"Filesystem     1024-blocks      Used Available Capacity Mounted on",
		"tmpfs              800000      2000    798000       1% /run",
		"/dev/nvme0n1p2  243000000  79000000 151000000      35% /",
		"/dev/nvme0n1p1     524000      6000    518000       2% /boot/efi",
		"/dev/loop3          65000     65000         0     100% /snap/core/1",
		"overlay         243000000  79000000 151000000      35% /var/lib/docker/overlay2/x/merged",
		"/dev/sdb1      1000000000 100000000 900000000      10% /mnt/my data",
		"@@user", "kai",
		"@@host", "wgvpn",
		"",
	}, "\n")
}

func TestParseAndCompute(t *testing.T) {
	a := Parse(sample("100 0 100 800 0 0 0 0 0 0", "1000 5 0 0 0 0 0 0 2000 5 0 0 0 0 0 0"))
	b := Parse(sample("200 0 200 1400 200 0 0 0 0 0", "4000 9 0 0 0 0 0 0 8000 9 0 0 0 0 0 0"))

	if !a.HasCPU || !a.HasMem || !a.HasNet {
		t.Fatalf("sections missing: %+v", a)
	}
	first := Compute(nil, a, 0)
	if !first.Supported || first.CPU != -1 || first.RxRate != -1 {
		t.Fatalf("first sample: %+v", first)
	}
	if first.MemTotal != 8000000*1024 || first.MemUsed != 4000000*1024 || first.SwapUsed != 500000*1024 {
		t.Fatalf("mem: %+v", first)
	}
	if first.User != "kai" || first.Host != "wgvpn" || first.Load1 != 1.6 || math.Abs(first.Uptime-16761600.12) > 0.01 {
		t.Fatalf("misc: %+v", first)
	}
	if len(first.Disks) != 3 || first.Disks[0].Mount != "/" || first.Disks[1].Mount != "/boot/efi" || first.Disks[2].Mount != "/mnt/my data" {
		t.Fatalf("disks: %+v", first.Disks)
	}

	s := Compute(&a, b, 2)
	// total 1000->2000 (+1000), idle 800->1600 (+800) => 20% busy
	if math.Abs(s.CPU-20) > 0.001 {
		t.Fatalf("cpu = %v", s.CPU)
	}
	// only enp2s0 counts: rx +3000, tx +6000 over 2s
	if s.RxRate != 1500 || s.TxRate != 3000 {
		t.Fatalf("net = %v / %v", s.RxRate, s.TxRate)
	}
}

func TestCounterReset(t *testing.T) {
	a := Parse(sample("100 0 100 800 0 0 0 0", "9000 1 0 0 0 0 0 0 9000 1 0 0 0 0 0 0"))
	b := Parse(sample("200 0 200 900 0 0 0 0", "10 1 0 0 0 0 0 0 10 1 0 0 0 0 0 0"))
	s := Compute(&a, b, 1)
	if s.RxRate != 0 || s.TxRate != 0 {
		t.Fatalf("want 0 after reset, got %v / %v", s.RxRate, s.TxRate)
	}
}

func TestNotLinux(t *testing.T) {
	// e.g. a router CLI or macOS host: no /proc output at all.
	s := Compute(nil, Parse("@@stat\n@@mem\n@@net\n@@df\nFilesystem 512-blocks\n@@user\nadmin\n@@host\nsw1\n"), 0)
	if s.Supported {
		t.Fatalf("want unsupported, got %+v", s)
	}
	if s.Disks == nil {
		t.Fatal("disks must be [] not null for the UI")
	}
}

func TestCommandHasNoSingleQuotesInside(t *testing.T) {
	inner := strings.TrimSuffix(strings.TrimPrefix(Command, "exec sh -c '"), "'")
	if strings.Contains(inner, "'") {
		t.Fatal("Command body must not contain single quotes")
	}
}
