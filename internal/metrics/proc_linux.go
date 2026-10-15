//go:build linux

package metrics

import (
	"os"
	"strconv"
	"strings"
	"time"
)

const processMetricsAvailable = true

// clockTicks is USER_HZ, the unit of the CPU times in /proc (100 on every
// mainstream Linux architecture).
const clockTicks = 100

// readProcess reads the CPU time used so far and the resident memory of
// pid from /proc.
func readProcess(pid int) (time.Duration, int64, bool) {
	dir := "/proc/" + strconv.Itoa(pid) + "/"
	stat, err := os.ReadFile(dir + "stat")
	if err != nil {
		return 0, 0, false
	}
	// "pid (comm) state ppid ...": comm may contain spaces and parentheses.
	end := strings.LastIndexByte(string(stat), ')')
	if end < 0 {
		return 0, 0, false
	}
	fields := strings.Fields(string(stat[end+1:]))
	if len(fields) < 13 {
		return 0, 0, false
	}
	// utime and stime are fields 14 and 15 of the line (11 and 12 here).
	utime, err1 := strconv.ParseInt(fields[11], 10, 64)
	stime, err2 := strconv.ParseInt(fields[12], 10, 64)
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	cpu := time.Duration(utime+stime) * time.Second / clockTicks

	statm, err := os.ReadFile(dir + "statm")
	if err != nil {
		return 0, 0, false
	}
	parts := strings.Fields(string(statm))
	if len(parts) < 2 {
		return 0, 0, false
	}
	pages, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, 0, false
	}
	return cpu, pages * int64(os.Getpagesize()), true
}

// cgroupRoot is where the container's own cgroup is mounted.
const cgroupRoot = "/sys/fs/cgroup/"

// readContainer reads the CPU time and memory of the whole container from
// its cgroup (v2, or v1 as a fallback). Memory excludes the inactive file
// cache, like "docker stats".
func readContainer() (time.Duration, int64, bool) {
	if usage, ok := statValue(cgroupRoot+"cpu.stat", "usage_usec"); ok {
		mem, ok := readInt(cgroupRoot + "memory.current")
		if !ok {
			return 0, 0, false
		}
		inactive, _ := statValue(cgroupRoot+"memory.stat", "inactive_file")
		return time.Duration(usage) * time.Microsecond, max(0, mem-inactive), true
	}
	usage, ok1 := readInt(cgroupRoot + "cpuacct/cpuacct.usage") // nanoseconds
	mem, ok2 := readInt(cgroupRoot + "memory/memory.usage_in_bytes")
	if !ok1 || !ok2 {
		return 0, 0, false
	}
	inactive, _ := statValue(cgroupRoot+"memory/memory.stat", "total_inactive_file")
	return time.Duration(usage), max(0, mem-inactive), true
}

func readInt(path string) (int64, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	v, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	return v, err == nil
}

// statValue reads "key value" from a cgroup stat file.
func statValue(path, key string) (int64, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	for line := range strings.Lines(string(data)) {
		name, value, ok := strings.Cut(strings.TrimSpace(line), " ")
		if ok && name == key {
			v, err := strconv.ParseInt(value, 10, 64)
			return v, err == nil
		}
	}
	return 0, false
}
