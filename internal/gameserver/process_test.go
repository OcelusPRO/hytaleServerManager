package gameserver

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// TestHelperServer is not a real test: it is executed as a child process
// that imitates the Hytale server console.
func TestHelperServer(t *testing.T) {
	if os.Getenv("HSM_HELPER") != "1" {
		return
	}
	fmt.Println("Server booted")
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		switch cmd := scanner.Text(); cmd {
		case "/stop":
			fmt.Println("Shutting down")
			os.Exit(0)
		case "/crash":
			os.Exit(3)
		default:
			fmt.Println("echo " + cmd)
		}
	}
	os.Exit(0)
}

func helperSpec() LaunchSpec {
	return LaunchSpec{
		Path:   os.Args[0],
		Args:   []string{"-test.run=^TestHelperServer$"},
		Env:    append(os.Environ(), "HSM_HELPER=1"),
		SaveID: "abc",
	}
}

func waitForLine(t *testing.T, c *Console, substr string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		for _, l := range c.Since(0) {
			if strings.Contains(l.Text, substr) {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("console never printed %q; got %v", substr, c.Since(0))
}

func TestProcessLifecycle(t *testing.T) {
	console := NewConsole(100)
	p := NewProcess(console)
	exits := make(chan ExitInfo, 1)
	p.OnExit(func(info ExitInfo) { exits <- info })

	if err := p.Start(helperSpec()); err != nil {
		t.Fatal(err)
	}
	if err := p.Start(helperSpec()); err != ErrAlreadyRunning {
		t.Errorf("second start: got %v", err)
	}
	waitForLine(t, console, "Server booted")

	if err := p.Send("say hello"); err != nil {
		t.Fatal(err)
	}
	waitForLine(t, console, "echo say hello")
	if err := p.Send("bad\ncommand"); err != ErrInvalidCommand {
		t.Errorf("multi-line command: got %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := p.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	info := <-exits
	if !info.Expected || info.ExitCode != 0 || info.SaveID != "abc" {
		t.Errorf("unexpected exit info %+v", info)
	}
	if st := p.Status(); st.State != StateStopped {
		t.Errorf("state after stop: %s", st.State)
	}
}

func TestProcessCrash(t *testing.T) {
	console := NewConsole(100)
	p := NewProcess(console)
	exits := make(chan ExitInfo, 1)
	p.OnExit(func(info ExitInfo) { exits <- info })

	if err := p.Start(helperSpec()); err != nil {
		t.Fatal(err)
	}
	waitForLine(t, console, "Server booted")
	if err := p.Send("/crash"); err != nil {
		t.Fatal(err)
	}
	info := <-exits
	if info.Expected || info.ExitCode != 3 {
		t.Errorf("unexpected exit info %+v", info)
	}
	if st := p.Status(); st.State != StateCrashed || st.ExitCode == nil || *st.ExitCode != 3 {
		t.Errorf("unexpected status %+v", st)
	}
}

func TestConsoleRingAndSanitize(t *testing.T) {
	c := NewConsole(3)
	for i := range 5 {
		c.Append(StreamStdout, fmt.Sprintf("\x1b[32mline %d\x1b[0m", i))
	}
	lines := c.Since(0)
	if len(lines) != 3 || lines[0].Text != "line 2" || lines[2].Seq != 5 {
		t.Fatalf("unexpected buffer %+v", lines)
	}
	if got := c.Since(4); len(got) != 1 || got[0].Text != "line 4" {
		t.Errorf("Since(4) = %+v", got)
	}
}
