package commandcatalog

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"hytale-server-manager/internal/gameserver"
)

const (
	dumpFile = "dumps/commands.dump.json"
	// The feedback of a command is printed at once: a short silence after
	// the last line means the block is complete.
	quietWindow  = 250 * time.Millisecond
	firstLineMax = 3 * time.Second
)

// sender writes commands to the server without echoing them.
type sender interface {
	SendSilent(command string) error
}

// Builder queries a running server for its commands.
type Builder struct {
	console *gameserver.Console
	server  sender
}

// NewBuilder creates a builder talking to server and hiding the replies
// from console.
func NewBuilder(server sender, console *gameserver.Console) *Builder {
	return &Builder{console: console, server: server}
}

// Build lists the commands of the server running in saveDir.
func (b *Builder) Build(ctx context.Context, saveDir string) ([]Entry, error) {
	c := newCollector()
	remove := b.console.Intercept(c.intercept)
	defer remove()

	if _, err := b.run(ctx, c, "commands dump"); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(saveDir, dumpFile))
	if err != nil {
		return nil, fmt.Errorf("read command dump: %w", err)
	}
	paths, err := ParseDump(data)
	if err != nil {
		return nil, fmt.Errorf("parse command dump: %w", err)
	}

	var entries []Entry
	for _, root := range Roots(paths) {
		lines, err := b.run(ctx, c, "help "+root)
		if err != nil {
			return nil, err
		}
		entries = append(entries, ParseHelp(root, lines)...)
	}
	return Merge(entries, paths), nil
}

// run sends one command and collects its feedback lines.
func (b *Builder) run(ctx context.Context, c *collector, command string) ([]string, error) {
	c.drain()
	c.expect(command)
	if err := b.server.SendSilent("/" + command); err != nil {
		return nil, err
	}
	var lines []string
	timer := time.NewTimer(firstLineMax)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case line := <-c.lines:
			lines = append(lines, line)
			timer.Reset(quietWindow)
		case <-timer.C:
			c.reset() // stop capturing whatever comes late
			if lines == nil && command == "commands dump" {
				return nil, errors.New("the server did not answer /commands dump")
			}
			return lines, nil
		}
	}
}
