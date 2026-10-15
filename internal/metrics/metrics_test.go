package metrics

import (
	"path/filepath"
	"testing"
	"time"
)

type fakeServer struct {
	now     time.Time
	probe   Probe
	cpu     time.Duration // cumulated by the fake process
	rss     int64
	process bool
	// Whole container.
	ccpu      time.Duration
	cmem      int64
	container bool
}

func (f *fakeServer) collector(path string) *Collector {
	return New(Deps{
		Probe: func() Probe { return f.probe },
		Path:  path,
		Now:   func() time.Time { return f.now },
		Process: func(int) (time.Duration, int64, bool) {
			return f.cpu, f.rss, f.process
		},
		Container: func() (time.Duration, int64, bool) {
			return f.ccpu, f.cmem, f.container
		},
	})
}

// run samples for d, the process using cores and rss meanwhile.
func (f *fakeServer) run(c *Collector, d time.Duration, cores float64) {
	f.runBoth(c, d, cores, 0)
}

// runBoth also makes the container use containerCores.
func (f *fakeServer) runBoth(c *Collector, d time.Duration, cores, containerCores float64) {
	for elapsed := time.Duration(0); elapsed < d; elapsed += sampleInterval {
		f.now = f.now.Add(sampleInterval)
		f.cpu += time.Duration(cores * float64(sampleInterval))
		f.ccpu += time.Duration(containerCores * float64(sampleInterval))
		c.sample(sampleInterval)
	}
}

func newFake() *fakeServer {
	return &fakeServer{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
}

func TestSeriesAggregatesStatesAndResources(t *testing.T) {
	f := newFake()
	c := f.collector("")
	f.probe = Probe{PID: 42, State: StateRunning, Players: 3}
	f.process, f.rss = true, 2048<<20
	f.run(c, 10*time.Minute, 1.5)
	f.probe = Probe{State: StateSleeping}
	f.process = false
	f.run(c, 20*time.Minute, 0)

	s := c.Series(time.Hour)
	if got := s.Summary.RunningSeconds; got < 9*60 || got > 10*60 {
		t.Fatalf("running %v s", got)
	}
	if got := s.Summary.SleepingSeconds; got != 20*60 {
		t.Fatalf("sleeping %v s", got)
	}
	if s.Summary.PeakPlayers != 3 || s.Summary.PeakMemMB == nil || *s.Summary.PeakMemMB != 2048 {
		t.Fatalf("summary %+v", s.Summary)
	}
	if s.Summary.AvgCPU == nil || *s.Summary.AvgCPU != 1.5 {
		t.Fatalf("avg cpu %v", s.Summary.AvgCPU)
	}
	if len(s.Points) == 0 || len(s.Points) > maxPoints {
		t.Fatalf("%d points", len(s.Points))
	}
}

func TestSeriesIsDownsampled(t *testing.T) {
	f := newFake()
	c := f.collector("")
	f.probe = Probe{State: StateStopped}
	f.run(c, 24*time.Hour, 0)
	s := c.Series(24 * time.Hour)
	if len(s.Points) > maxPoints+1 || s.Step != 300 {
		t.Fatalf("%d points, step %d", len(s.Points), s.Step)
	}
	if s.Summary.StoppedSeconds < 23*3600 {
		t.Fatalf("stopped %v s", s.Summary.StoppedSeconds)
	}
}

func TestHistorySurvivesRestartAndExpires(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metrics.json")
	f := newFake()
	c := f.collector(path)
	f.probe = Probe{State: StateSleeping}
	f.run(c, 10*time.Minute, 0)
	c.save()

	c = f.collector(path)
	if s := c.Series(time.Hour); s.Summary.SleepingSeconds < 9*60 {
		t.Fatalf("history lost: %+v", s.Summary)
	}
	f.now = f.now.Add(retention + time.Hour)
	f.run(c, 2*time.Minute, 0)
	c.mu.Lock()
	n := len(c.history)
	c.mu.Unlock()
	if n > 2 {
		t.Fatalf("%d minutes kept beyond retention", n)
	}
}

func TestNoCPURateOnFirstReading(t *testing.T) {
	f := newFake()
	c := f.collector("")
	f.probe = Probe{PID: 1, State: StateRunning}
	f.process, f.cpu = true, time.Hour // process started long ago
	c.sample(sampleInterval)
	c.mu.Lock()
	samples := c.current.cpu.n
	c.mu.Unlock()
	if samples != 0 {
		t.Fatal("a rate was computed without a previous reading")
	}
}

func TestContainerUsagePerState(t *testing.T) {
	f := newFake()
	c := f.collector("")
	f.container = true
	f.probe = Probe{PID: 7, State: StateRunning}
	f.process, f.rss, f.cmem = true, 2048<<20, 2304<<20
	f.runBoth(c, 30*time.Minute, 1.5, 1.6)
	f.probe, f.process, f.cmem = Probe{State: StateSleeping}, false, 20<<20
	f.runBoth(c, 30*time.Minute, 0, 0.01)

	s := c.Series(time.Hour)
	if !s.ContainerMetrics {
		t.Fatal("container metrics not reported")
	}
	running, sleeping := s.Summary.States[StateRunning], s.Summary.States[StateSleeping]
	if running.CPU == nil || *running.CPU != 1.6 || running.MemMB == nil || *running.MemMB != 2304 {
		t.Fatalf("running %+v", running)
	}
	if sleeping.CPU == nil || *sleeping.CPU != 0.01 || sleeping.MemMB == nil || *sleeping.MemMB != 20 {
		t.Fatalf("sleeping %+v", sleeping)
	}
	if _, ok := s.Summary.States[StateStopped]; ok {
		t.Fatal("stopped state without samples")
	}
	if p := s.Summary.ContainerPeakMemMB; p == nil || *p != 2304 {
		t.Fatalf("container peak %v", p)
	}
	if a := s.Summary.ContainerAvgCPU; a == nil || *a < 0.75 || *a > 0.85 {
		t.Fatalf("container average %v", *a)
	}
	// Java figures only cover the running time.
	if a := s.Summary.AvgCPU; a == nil || *a != 1.5 {
		t.Fatalf("java average %v", a)
	}
}

func TestSeriesAggregatesPluginReadings(t *testing.T) {
	f := newFake()
	c := f.collector("")
	f.probe = Probe{State: StateRunning, Players: 2, Plugin: &PluginSample{TPS: 20, MSPT: 10, MSPTMax: 12, HeapMB: 1000}}
	f.run(c, 5*time.Minute, 0)
	f.probe.Plugin = &PluginSample{TPS: 10, MSPT: 30, MSPTMax: 80, HeapMB: 2000}
	f.run(c, 5*time.Minute, 0)

	s := c.Series(time.Hour)
	if !s.PluginMetrics {
		t.Fatal("plugin metrics not reported")
	}
	sum := s.Summary
	if sum.AvgTPS == nil || *sum.AvgTPS < 14 || *sum.AvgTPS > 16 {
		t.Errorf("avg TPS = %v", sum.AvgTPS)
	}
	if sum.MinTPS == nil || *sum.MinTPS != 10 || sum.PeakHeapMB == nil || *sum.PeakHeapMB != 2000 {
		t.Errorf("min TPS = %v, peak heap = %v", sum.MinTPS, sum.PeakHeapMB)
	}
	last := s.Points[len(s.Points)-1]
	if last.MSPTMax == nil || *last.MSPTMax != 80 {
		t.Errorf("last point MSPT max = %v", last.MSPTMax)
	}

	// Without the plugin nothing is invented.
	f2 := newFake()
	c2 := f2.collector("")
	f2.probe = Probe{State: StateRunning}
	f2.run(c2, 2*time.Minute, 0)
	if s2 := c2.Series(time.Hour); s2.PluginMetrics || s2.Summary.AvgTPS != nil {
		t.Error("plugin data reported while the plugin is off")
	}
}
