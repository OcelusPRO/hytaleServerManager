package curseforge

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"slices"
	"sync"
	"time"
)

// Job states.
const (
	JobRunning = "running"
	JobDone    = "done"
	JobFailed  = "failed"
)

const (
	keepJobs   = 10
	jobTimeout = 3 * time.Hour
)

// Job is a long-running CurseForge operation (modpack install, updates).
type Job struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	SaveID     string    `json:"saveId"`
	State      string    `json:"state"`
	Progress   float64   `json:"progress"`
	Message    string    `json:"message,omitempty"`
	Warnings   []string  `json:"warnings,omitempty"`
	Error      string    `json:"error,omitempty"`
	StartedAt  time.Time `json:"startedAt"`
	FinishedAt time.Time `json:"finishedAt,omitzero"`
}

// jobBoard tracks the recent jobs; only one runs at a time.
type jobBoard struct {
	mu   sync.Mutex
	jobs []*Job // oldest first
	byID map[string]*Job
}

func newJobBoard() *jobBoard {
	return &jobBoard{byID: map[string]*Job{}}
}

// JobHandle lets a running operation report progress.
type JobHandle struct {
	board *jobBoard
	job   *Job
}

// Progress sets the completed fraction and the current step.
func (h *JobHandle) Progress(fraction float64, message string) {
	h.board.mu.Lock()
	defer h.board.mu.Unlock()
	h.job.Progress = min(max(fraction, 0), 1)
	h.job.Message = message
}

// Warn records a non-fatal problem.
func (h *JobHandle) Warn(message string) {
	h.board.mu.Lock()
	defer h.board.mu.Unlock()
	h.job.Warnings = append(h.job.Warnings, message)
}

// startJob runs fn in the background unless a job is already running.
func (s *Service) startJob(saveID, title string, fn func(ctx context.Context, h *JobHandle) error) (Job, error) {
	b := s.jobs
	b.mu.Lock()
	for _, j := range b.jobs {
		if j.State == JobRunning {
			b.mu.Unlock()
			return Job{}, ErrBusy
		}
	}
	raw := make([]byte, 8)
	_, _ = rand.Read(raw)
	job := &Job{ID: hex.EncodeToString(raw), Title: title, SaveID: saveID, State: JobRunning, StartedAt: time.Now().UTC()}
	b.jobs = append(b.jobs, job)
	b.byID[job.ID] = job
	if len(b.jobs) > keepJobs {
		delete(b.byID, b.jobs[0].ID)
		b.jobs = b.jobs[1:]
	}
	snapshot := snapshotOf(job)
	b.mu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), jobTimeout)
		defer cancel()
		err := fn(ctx, &JobHandle{board: b, job: job})
		b.mu.Lock()
		defer b.mu.Unlock()
		job.FinishedAt = time.Now().UTC()
		if err != nil {
			job.State, job.Error = JobFailed, err.Error()
			s.Logger.Warn("curseforge job failed", "job", job.Title, "error", err)
			return
		}
		job.State, job.Progress = JobDone, 1
	}()
	return snapshot, nil
}

// Job returns a snapshot of one job.
func (s *Service) Job(id string) (Job, bool) {
	s.jobs.mu.Lock()
	defer s.jobs.mu.Unlock()
	j, ok := s.jobs.byID[id]
	if !ok {
		return Job{}, false
	}
	return snapshotOf(j), true
}

// Jobs returns the recent jobs, newest first.
func (s *Service) Jobs() []Job {
	s.jobs.mu.Lock()
	defer s.jobs.mu.Unlock()
	out := make([]Job, 0, len(s.jobs.jobs))
	for _, j := range s.jobs.jobs {
		out = append(out, snapshotOf(j))
	}
	slices.Reverse(out)
	return out
}

func snapshotOf(j *Job) Job {
	c := *j
	c.Warnings = slices.Clone(j.Warnings)
	return c
}
