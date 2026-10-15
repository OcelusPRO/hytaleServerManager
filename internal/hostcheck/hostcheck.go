// Package hostcheck spots, in the game server output, problems that only the
// host's administrator can fix, so that the panel can explain them.
package hostcheck

import (
	"regexp"
	"slices"
	"strconv"
	"sync"
	"time"
)

// Warning kinds.
const (
	// UDPReceiveBuffer: the kernel caps the UDP receive buffer below what the
	// QUIC transport wants (net.core.rmem_max): bursts of packets may be
	// dropped, which players feel as lag.
	UDPReceiveBuffer = "udpReceiveBuffer"
)

var (
	// [QUICTransport] UDP receive buffer is 425984 bytes (effective 212992),
	// below the 25165824-byte autotune window. Raise net.core.rmem_max ...
	udpBufferPattern = regexp.MustCompile(`UDP receive buffer is (\d+) bytes \(effective (\d+)\), below the (\d+)-byte autotune window`)
	// A new server process: warnings are detected again from scratch.
	bootPattern = regexp.MustCompile(`Booting up HytaleServer`)
)

// Warning is a host problem reported by the server. Values hold the numbers
// the interface needs to explain it (the text itself is translated there).
type Warning struct {
	Kind   string           `json:"kind"`
	Values map[string]int64 `json:"values,omitempty"`
	SeenAt time.Time        `json:"seenAt"`
}

// Monitor collects warnings from console lines.
type Monitor struct {
	mu       sync.Mutex
	warnings map[string]Warning
}

// NewMonitor returns an empty monitor.
func NewMonitor() *Monitor {
	return &Monitor{warnings: make(map[string]Warning)}
}

// Observe inspects one console line of the game server.
func (m *Monitor) Observe(line string) {
	switch {
	case bootPattern.MatchString(line):
		m.mu.Lock()
		clear(m.warnings)
		m.mu.Unlock()
	default:
		if g := udpBufferPattern.FindStringSubmatch(line); g != nil {
			m.set(Warning{Kind: UDPReceiveBuffer, Values: map[string]int64{
				"requested": atoi(g[1]),
				"effective": atoi(g[2]),
				"wanted":    atoi(g[3]),
			}})
		}
	}
}

func (m *Monitor) set(w Warning) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, seen := m.warnings[w.Kind]; seen {
		return // the server repeats it for every socket
	}
	w.SeenAt = time.Now().UTC()
	m.warnings[w.Kind] = w
}

// Warnings lists the current warnings, oldest first.
func (m *Monitor) Warnings() []Warning {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Warning, 0, len(m.warnings))
	for _, w := range m.warnings {
		out = append(out, w)
	}
	slices.SortFunc(out, func(a, b Warning) int { return a.SeenAt.Compare(b.SeenAt) })
	return out
}

func atoi(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}
