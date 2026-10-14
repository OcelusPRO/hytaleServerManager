// Package gameserver runs the Hytale server process and exposes its console.
package gameserver

import (
	"regexp"
	"strings"
	"sync"
	"time"
)

// Stream identifies where a console line comes from.
type Stream string

// Console streams.
const (
	StreamStdout Stream = "out"
	StreamStderr Stream = "err"
	StreamInput  Stream = "in"
	StreamSystem Stream = "sys"
)

const maxLineLength = 4096

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

// Line is one console line.
type Line struct {
	Seq    uint64    `json:"seq"`
	Time   time.Time `json:"time"`
	Stream Stream    `json:"stream"`
	Text   string    `json:"text"`
}

// Interceptor may consume a line before it is recorded; it returns true to
// hide the line. It runs under the console lock and must not block.
type Interceptor func(Line) bool

// Console keeps the latest lines in a ring buffer and fans them out to
// subscribers. Slow subscribers drop lines rather than block the server.
type Console struct {
	capacity int

	mu           sync.Mutex
	lines        []Line
	next         uint64
	subs         map[chan Line]struct{}
	interceptors map[int]Interceptor
	nextID       int
}

// NewConsole keeps up to capacity lines of history.
func NewConsole(capacity int) *Console {
	return &Console{capacity: capacity, next: 1, subs: make(map[chan Line]struct{}), interceptors: make(map[int]Interceptor)}
}

// Append records a line and notifies subscribers.
func (c *Console) Append(stream Stream, text string) {
	text = sanitize(text)
	c.mu.Lock()
	defer c.mu.Unlock()
	line := Line{Seq: c.next, Time: time.Now().UTC(), Stream: stream, Text: text}
	for _, intercept := range c.interceptors {
		if intercept(line) {
			return
		}
	}
	c.next++
	c.lines = append(c.lines, line)
	if over := len(c.lines) - c.capacity; over > 0 {
		c.lines = append(c.lines[:0:0], c.lines[over:]...)
	}
	for ch := range c.subs {
		select {
		case ch <- line:
		default:
		}
	}
}

// Since returns the buffered lines with a sequence number greater than seq.
func (c *Console) Since(seq uint64) []Line {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := []Line{}
	for _, l := range c.lines {
		if l.Seq > seq {
			out = append(out, l)
		}
	}
	return out
}

// Subscribe returns a channel receiving new lines and a cancel function.
func (c *Console) Subscribe() (<-chan Line, func()) {
	ch := make(chan Line, 512)
	c.mu.Lock()
	c.subs[ch] = struct{}{}
	c.mu.Unlock()
	return ch, func() {
		c.mu.Lock()
		delete(c.subs, ch)
		c.mu.Unlock()
	}
}

// Intercept registers fn and returns a function removing it.
func (c *Console) Intercept(fn Interceptor) func() {
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.nextID
	c.nextID++
	c.interceptors[id] = fn
	return func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		delete(c.interceptors, id)
	}
}

// System appends a message emitted by the manager itself.
func (c *Console) System(text string) { c.Append(StreamSystem, text) }

func sanitize(text string) string {
	text = ansiPattern.ReplaceAllString(text, "")
	text = strings.TrimRight(text, "\r\n")
	if len(text) > maxLineLength {
		text = text[:maxLineLength] + "…"
	}
	return text
}
