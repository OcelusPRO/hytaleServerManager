package gameserver

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"
)

// State is the lifecycle state of the server process.
type State string

// Process states.
const (
	StateStopped  State = "stopped"
	StateRunning  State = "running"
	StateStopping State = "stopping"
	StateCrashed  State = "crashed"
)

// Process errors.
var (
	ErrAlreadyRunning = errors.New("le serveur est déjà démarré")
	ErrNotRunning     = errors.New("le serveur n'est pas démarré")
	ErrInvalidCommand = errors.New("commande invalide")
)

const (
	maxCommandLength = 1024
	// stopCommand asks the server to save and shut down gracefully.
	stopCommand = "/stop"
)

// LaunchSpec fully describes how to start the server.
type LaunchSpec struct {
	Path   string
	Args   []string
	Dir    string
	Env    []string
	SaveID string
}

// ExitInfo describes how the process ended.
type ExitInfo struct {
	SaveID   string
	ExitCode int
	Expected bool // stop was requested by the operator
}

// Status is a snapshot of the process state.
type Status struct {
	State     State     `json:"state"`
	PID       int       `json:"pid,omitempty"`
	SaveID    string    `json:"saveId,omitempty"`
	StartedAt time.Time `json:"startedAt,omitzero"`
	ExitCode  *int      `json:"exitCode,omitempty"`
}

// Process supervises a single server process.
type Process struct {
	console *Console

	mu        sync.Mutex
	state     State
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	done      chan struct{}
	saveID    string
	startedAt time.Time
	exitCode  *int
	onExit    []func(ExitInfo)
}

// NewProcess creates a stopped process writing its output to console.
func NewProcess(console *Console) *Process {
	return &Process{console: console, state: StateStopped}
}

// OnExit registers a callback invoked after each process exit.
func (p *Process) OnExit(fn func(ExitInfo)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onExit = append(p.onExit, fn)
}

// Start launches the process described by spec.
func (p *Process) Start(spec LaunchSpec) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state == StateRunning || p.state == StateStopping {
		return ErrAlreadyRunning
	}

	cmd := exec.Command(spec.Path, spec.Args...)
	cmd.Dir = spec.Dir
	cmd.Env = spec.Env
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start server: %w", err)
	}

	p.cmd, p.stdin = cmd, stdin
	p.state, p.saveID = StateRunning, spec.SaveID
	p.startedAt, p.exitCode = time.Now().UTC(), nil
	p.done = make(chan struct{})

	var pumps sync.WaitGroup
	pumps.Add(2)
	go p.pump(stdout, StreamStdout, &pumps)
	go p.pump(stderr, StreamStderr, &pumps)
	go p.wait(cmd, p.done, &pumps)
	return nil
}

// Send writes a command to the server console and echoes it.
func (p *Process) Send(command string) error {
	command = strings.TrimSpace(command)
	if err := p.write(command); err != nil {
		return err
	}
	p.console.Append(StreamInput, "> "+command)
	return nil
}

// SendSilent writes a command without echoing it (manager housekeeping).
func (p *Process) SendSilent(command string) error {
	return p.write(strings.TrimSpace(command))
}

func (p *Process) write(command string) error {
	if command == "" || len(command) > maxCommandLength || strings.ContainsFunc(command, unicode.IsControl) {
		return ErrInvalidCommand
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state != StateRunning && p.state != StateStopping {
		return ErrNotRunning
	}
	if _, err := io.WriteString(p.stdin, command+"\n"); err != nil {
		return fmt.Errorf("write to server: %w", err)
	}
	return nil
}

// Stop shuts the server down gracefully: it sends the stop command, then
// SIGTERM after half of the timeout, and finally kills the process.
func (p *Process) Stop(ctx context.Context) error {
	p.mu.Lock()
	if p.state != StateRunning {
		p.mu.Unlock()
		if p.state == StateStopping {
			return p.waitDone(ctx)
		}
		return ErrNotRunning
	}
	p.state = StateStopping
	done, cmd := p.done, p.cmd
	p.mu.Unlock()

	p.console.System("Arrêt du serveur demandé…")
	_ = p.sendRaw(stopCommand)

	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(time.Minute)
	}
	grace := time.Until(deadline) / 2
	select {
	case <-done:
		return nil
	case <-time.After(grace):
	case <-ctx.Done():
	}

	p.console.System("Le serveur ne répond pas, envoi de SIGTERM…")
	_ = cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-done:
		return nil
	case <-ctx.Done():
	}
	p.console.System("Arrêt forcé du serveur.")
	_ = cmd.Process.Kill()
	<-done
	return nil
}

// Kill terminates the process immediately.
func (p *Process) Kill() error {
	p.mu.Lock()
	if p.state != StateRunning && p.state != StateStopping {
		p.mu.Unlock()
		return ErrNotRunning
	}
	p.state = StateStopping
	cmd, done := p.cmd, p.done
	p.mu.Unlock()

	p.console.System("Arrêt forcé du serveur (kill).")
	if err := cmd.Process.Kill(); err != nil {
		return err
	}
	<-done
	return nil
}

// Status returns the current process state.
func (p *Process) Status() Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	st := Status{State: p.state, SaveID: p.saveID, ExitCode: p.exitCode}
	if p.state == StateRunning || p.state == StateStopping {
		st.PID = p.cmd.Process.Pid
		st.StartedAt = p.startedAt
	}
	return st
}

// Active reports whether the process is running or stopping.
func (p *Process) Active() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state == StateRunning || p.state == StateStopping
}

// waitDone blocks until the current process exits or ctx ends.
func (p *Process) waitDone(ctx context.Context) error {
	p.mu.Lock()
	done := p.done
	p.mu.Unlock()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *Process) sendRaw(command string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stdin == nil {
		return ErrNotRunning
	}
	_, err := io.WriteString(p.stdin, command+"\n")
	return err
}

func (p *Process) pump(r io.Reader, stream Stream, wg *sync.WaitGroup) {
	defer wg.Done()
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		p.console.Append(stream, scanner.Text())
	}
	_, _ = io.Copy(io.Discard, r)
}

func (p *Process) wait(cmd *exec.Cmd, done chan struct{}, pumps *sync.WaitGroup) {
	pumps.Wait()
	_ = cmd.Wait()
	code := cmd.ProcessState.ExitCode()

	p.mu.Lock()
	expected := p.state == StateStopping
	if expected || code == 0 {
		p.state = StateStopped
	} else {
		p.state = StateCrashed
	}
	p.exitCode = &code
	p.stdin = nil
	info := ExitInfo{SaveID: p.saveID, ExitCode: code, Expected: expected}
	hooks := append([]func(ExitInfo){}, p.onExit...)
	close(done)
	p.mu.Unlock()

	p.console.System(fmt.Sprintf("Le serveur s'est arrêté (code %d).", code))
	for _, hook := range hooks {
		hook(info)
	}
}
