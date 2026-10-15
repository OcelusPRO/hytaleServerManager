//go:build linux

package metrics

import (
	"os"
	"testing"
	"time"
)

func TestReadProcessOfSelf(t *testing.T) {
	// Burn a little CPU so that the counter moves.
	deadline := time.Now().Add(50 * time.Millisecond)
	for time.Now().Before(deadline) {
	}
	cpu, rss, ok := readProcess(os.Getpid())
	if !ok || cpu <= 0 || rss <= 0 {
		t.Fatalf("cpu=%v rss=%d ok=%v", cpu, rss, ok)
	}
	if _, _, ok := readProcess(1 << 30); ok {
		t.Fatal("read a process that does not exist")
	}
}

func TestReadContainer(t *testing.T) {
	cpu, mem, ok := readContainer()
	if !ok {
		t.Skip("no readable cgroup here")
	}
	if cpu <= 0 || mem <= 0 {
		t.Fatalf("cpu=%v mem=%d", cpu, mem)
	}
}
