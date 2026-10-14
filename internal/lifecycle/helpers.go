package lifecycle

import (
	"sync"
	"time"
)

// sessionHolder remembers the session token of the running server.
type sessionHolder struct {
	mu    sync.Mutex
	token string
}

func (h *sessionHolder) set(token string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.token = token
}

func (h *sessionHolder) take() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	token := h.token
	h.token = ""
	return token
}

// restartBudget limits automatic restarts to max per window.
type restartBudget struct {
	max    int
	window time.Duration

	mu    sync.Mutex
	times []time.Time
}

func newRestartBudget(max int, window time.Duration) *restartBudget {
	return &restartBudget{max: max, window: window}
}

// take consumes one restart, returning false when the budget is exhausted.
func (b *restartBudget) take() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	kept := b.times[:0]
	for _, t := range b.times {
		if now.Sub(t) < b.window {
			kept = append(kept, t)
		}
	}
	b.times = kept
	if len(b.times) >= b.max {
		return false
	}
	b.times = append(b.times, now)
	return true
}
