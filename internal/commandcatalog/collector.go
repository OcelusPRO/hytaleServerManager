package commandcatalog

import (
	"regexp"
	"strings"
	"sync"

	"hytale-server-manager/internal/gameserver"
)

// timestamped matches regular log lines ("[2026/10/13 09:00:09   INFO] …"),
// as opposed to the raw lines printed as command feedback.
var timestamped = regexp.MustCompile(`^\[\d{4}/\d{2}/\d{2} `)

const executedPrefix = "Console executed command: "

// collector hides the output of the manager's housekeeping commands from
// the console and forwards it to the builder. The server first logs
// "Console executed command: <cmd>", then prints the feedback as lines
// without timestamp; the next timestamped line ends the block.
type collector struct {
	mu        sync.Mutex
	expected  string
	capturing bool
	lines     chan string
}

func newCollector() *collector {
	return &collector{lines: make(chan string, 4096)}
}

// expect arms the collector for the next command.
func (c *collector) expect(command string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.expected = executedPrefix + command
	c.capturing = false
}

// drain discards lines left over by a previous command.
func (c *collector) drain() {
	for {
		select {
		case <-c.lines:
		default:
			return
		}
	}
}

// reset disarms the collector.
func (c *collector) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.expected, c.capturing = "", false
}

// intercept is registered on the console; it must not block.
func (c *collector) intercept(line gameserver.Line) bool {
	if line.Stream != gameserver.StreamStdout && line.Stream != gameserver.StreamStderr {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.expected == "" {
		return false
	}
	if strings.HasSuffix(line.Text, c.expected) {
		c.capturing = true
		return true
	}
	if !c.capturing {
		return false
	}
	if timestamped.MatchString(line.Text) {
		c.capturing, c.expected = false, ""
		return false
	}
	select {
	case c.lines <- line.Text:
	default:
	}
	return true
}
