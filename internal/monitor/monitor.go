// Package monitor samples a remote Linux host's resource usage (CPU, memory,
// network, disks, uptime) over an existing SSH connection.
//
// Each sample runs one short, read-only shell command that prints /proc files
// and df output between section markers. CPU usage and network rates are
// deltas between two consecutive samples, so the first sample only has
// absolute values.
package monitor

import (
	"bufio"
	"sort"
	"strconv"
	"strings"
)

// Command is run on the remote host. It goes through `sh -c` so it works
// whatever the login shell is (bash, zsh, fish...), and contains no single
// quotes. Everything is read-only.
const Command = `exec sh -c '
LC_ALL=C; export LC_ALL
echo @@stat; head -n 1 /proc/stat 2>/dev/null
echo @@mem; cat /proc/meminfo 2>/dev/null
echo @@net; cat /proc/net/dev 2>/dev/null
echo @@up; cat /proc/uptime 2>/dev/null
echo @@load; cat /proc/loadavg 2>/dev/null
echo @@df; df -kP 2>/dev/null
echo @@user; id -un 2>/dev/null || whoami 2>/dev/null
echo @@host; hostname 2>/dev/null || uname -n 2>/dev/null
'`

// Raw is one parsed sample.
type Raw struct {
	HasCPU            bool
	CPUTotal, CPUIdle uint64

	HasMem              bool
	MemTotal, MemAvail  uint64 // bytes
	SwapTotal, SwapFree uint64 // bytes

	HasNet           bool
	RxBytes, TxBytes uint64

	Uptime float64 // seconds
	Load1  float64
	Disks  []Disk
	User   string
	Host   string
}

// Disk is one mounted filesystem.
type Disk struct {
	Mount string `json:"mount"`
	Used  uint64 `json:"used"`  // bytes
	Total uint64 `json:"total"` // bytes
}

// Stats is what the UI shows.
type Stats struct {
	Supported bool    `json:"supported"` // false when the host has no /proc (not Linux)
	User      string  `json:"user"`
	Host      string  `json:"host"`
	CPU       float64 `json:"cpu"` // percent; -1 until two samples exist
	Load1     float64 `json:"load1"`
	MemUsed   uint64  `json:"memUsed"`
	MemTotal  uint64  `json:"memTotal"`
	SwapUsed  uint64  `json:"swapUsed"`
	SwapTotal uint64  `json:"swapTotal"`
	RxRate    float64 `json:"rxRate"` // bytes/s; -1 until two samples exist
	TxRate    float64 `json:"txRate"` // bytes/s; -1 until two samples exist
	Uptime    float64 `json:"uptime"` // seconds
	Disks     []Disk  `json:"disks"`
}

// Parse splits the command output into sections and parses each.
func Parse(out string) Raw {
	var r Raw
	sec := map[string][]string{}
	cur := ""
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if strings.HasPrefix(line, "@@") {
			cur = line[2:]
			continue
		}
		if cur != "" {
			sec[cur] = append(sec[cur], line)
		}
	}
	parseStat(&r, sec["stat"])
	parseMem(&r, sec["mem"])
	parseNet(&r, sec["net"])
	if f := fields(sec["up"]); len(f) > 0 {
		r.Uptime, _ = strconv.ParseFloat(f[0], 64)
	}
	if f := fields(sec["load"]); len(f) > 0 {
		r.Load1, _ = strconv.ParseFloat(f[0], 64)
	}
	r.Disks = parseDF(sec["df"])
	r.User = first(sec["user"])
	r.Host = first(sec["host"])
	return r
}

func first(lines []string) string {
	for _, l := range lines {
		if s := strings.TrimSpace(l); s != "" {
			return s
		}
	}
	return ""
}

func fields(lines []string) []string { return strings.Fields(first(lines)) }

func parseStat(r *Raw, lines []string) {
	f := fields(lines)
	if len(f) < 5 || f[0] != "cpu" {
		return
	}
	// user nice system idle iowait irq softirq steal (guest is already in user)
	var vals []uint64
	for i := 1; i < len(f) && i <= 8; i++ {
		v, err := strconv.ParseUint(f[i], 10, 64)
		if err != nil {
			return
		}
		vals = append(vals, v)
	}
	for _, v := range vals {
		r.CPUTotal += v
	}
	r.CPUIdle = vals[3]
	if len(vals) > 4 {
		r.CPUIdle += vals[4] // iowait
	}
	r.HasCPU = true
}

func parseMem(r *Raw, lines []string) {
	kv := map[string]uint64{}
	for _, l := range lines {
		name, rest, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		f := strings.Fields(rest)
		if len(f) == 0 {
			continue
		}
		v, err := strconv.ParseUint(f[0], 10, 64)
		if err != nil {
			continue
		}
		kv[name] = v * 1024 // /proc/meminfo is in kB
	}
	total, ok := kv["MemTotal"]
	if !ok || total == 0 {
		return
	}
	avail, ok := kv["MemAvailable"]
	if !ok { // kernels before 3.14
		avail = kv["MemFree"] + kv["Buffers"] + kv["Cached"]
	}
	r.HasMem = true
	r.MemTotal, r.MemAvail = total, min(avail, total)
	r.SwapTotal, r.SwapFree = kv["SwapTotal"], min(kv["SwapFree"], kv["SwapTotal"])
}

// virtualIface reports interfaces whose traffic is also counted on a physical
// one (or is local only), so summing them would double count.
func virtualIface(name string) bool {
	if name == "lo" {
		return true
	}
	for _, p := range []string{"veth", "docker", "br-", "virbr", "cni", "flannel", "cali", "vxlan", "kube-"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

func parseNet(r *Raw, lines []string) {
	for _, l := range lines {
		name, rest, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		f := strings.Fields(rest)
		if len(f) < 9 || name == "" || strings.Contains(name, "|") {
			continue // header lines
		}
		rx, err1 := strconv.ParseUint(f[0], 10, 64)
		tx, err2 := strconv.ParseUint(f[8], 10, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		r.HasNet = true
		if virtualIface(name) {
			continue
		}
		r.RxBytes += rx
		r.TxBytes += tx
	}
}

var skipFS = map[string]bool{
	"tmpfs": true, "devtmpfs": true, "udev": true, "overlay": true, "squashfs": true,
	"none": true, "shm": true, "efivarfs": true, "cgroup": true, "cgroup2": true, "proc": true, "sysfs": true,
}

var skipMountPrefix = []string{"/snap", "/run", "/sys", "/proc", "/dev", "/var/lib/docker", "/var/snap"}

// maxDisks is how many filesystems the bar shows.
const maxDisks = 4

func parseDF(lines []string) []Disk {
	var out []Disk
	seen := map[string]bool{}
	for _, l := range lines {
		f := strings.Fields(l)
		// Filesystem 1024-blocks Used Available Capacity Mounted on
		if len(f) < 6 || f[0] == "Filesystem" {
			continue
		}
		fs, mount := f[0], strings.Join(f[5:], " ")
		if skipFS[fs] || strings.HasPrefix(fs, "/dev/loop") || seen[fs] {
			continue
		}
		skip := false
		for _, p := range skipMountPrefix {
			if mount == p || strings.HasPrefix(mount, p+"/") {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		total, err1 := strconv.ParseUint(f[1], 10, 64)
		used, err2 := strconv.ParseUint(f[2], 10, 64)
		if err1 != nil || err2 != nil || total == 0 {
			continue
		}
		seen[fs] = true
		out = append(out, Disk{Mount: mount, Used: used * 1024, Total: total * 1024})
	}
	// "/" first, then by mount path.
	sort.SliceStable(out, func(i, j int) bool {
		if (out[i].Mount == "/") != (out[j].Mount == "/") {
			return out[i].Mount == "/"
		}
		return out[i].Mount < out[j].Mount
	})
	if len(out) > maxDisks {
		out = out[:maxDisks]
	}
	return out
}

// Compute turns a sample (and the previous one, if any, taken dt seconds
// earlier) into Stats.
func Compute(prev *Raw, cur Raw, dt float64) Stats {
	s := Stats{
		Supported: cur.HasCPU || cur.HasMem,
		User:      cur.User,
		Host:      cur.Host,
		CPU:       -1,
		Load1:     cur.Load1,
		MemTotal:  cur.MemTotal,
		MemUsed:   cur.MemTotal - cur.MemAvail,
		SwapTotal: cur.SwapTotal,
		SwapUsed:  cur.SwapTotal - cur.SwapFree,
		RxRate:    -1,
		TxRate:    -1,
		Uptime:    cur.Uptime,
		Disks:     cur.Disks,
	}
	if s.Disks == nil {
		s.Disks = []Disk{}
	}
	if prev == nil || dt <= 0 {
		return s
	}
	if prev.HasCPU && cur.HasCPU && cur.CPUTotal > prev.CPUTotal && cur.CPUIdle >= prev.CPUIdle {
		dTotal := float64(cur.CPUTotal - prev.CPUTotal)
		dIdle := float64(cur.CPUIdle - prev.CPUIdle)
		s.CPU = max(0, min(100, 100*(dTotal-dIdle)/dTotal))
	}
	if prev.HasNet && cur.HasNet {
		// Counters can reset (interface re-created); show 0 rather than a huge spike.
		s.RxRate, s.TxRate = 0, 0
		if cur.RxBytes >= prev.RxBytes {
			s.RxRate = float64(cur.RxBytes-prev.RxBytes) / dt
		}
		if cur.TxBytes >= prev.TxBytes {
			s.TxRate = float64(cur.TxBytes-prev.TxBytes) / dt
		}
	}
	return s
}
