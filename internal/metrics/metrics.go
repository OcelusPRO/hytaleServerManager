// Package metrics records the resource usage of the game server over time:
// memory and CPU of the Java process and of the whole container (manager
// included), connected players and the server state (running, asleep,
// stopped). Samples are taken every few seconds and aggregated per minute;
// the minutes of the last days are kept on disk so that the history
// survives restarts.
package metrics

import (
	"context"
	"math"
	"runtime"
	"sync"
	"time"

	"hytale-server-manager/internal/fsutil"
)

const (
	sampleInterval = 5 * time.Second
	bucket         = time.Minute
	// retention is the history kept (one point per minute).
	retention = 14 * 24 * time.Hour
	// saveInterval spaces out writes of the history file.
	saveInterval = 5 * time.Minute
	// maxPoints bounds the points returned for a chart.
	maxPoints = 300
)

// Server states.
const (
	StateRunning  = "running"
	StateSleeping = "sleeping"
	StateStopped  = "stopped"
)

var states = [...]string{StateRunning, StateSleeping, StateStopped}

func stateIndex(state string) int {
	switch state {
	case StateRunning:
		return 0
	case StateSleeping:
		return 1
	}
	return 2
}

// Probe is the server situation at sampling time.
type Probe struct {
	PID     int // 0 when the process is not running
	State   string
	Players int
	// Plugin carries the readings of the MetricsStream plugin, when it is
	// connected (nil otherwise).
	Plugin *PluginSample
}

// PluginSample is what the MetricsStream plugin reports at sampling time.
type PluginSample struct {
	TPS     float64 // mean over the worlds, last 10 s
	MSPT    float64 // mean milliseconds per tick
	MSPTMax float64 // slowest world, peak of the last 10 s
	HeapMB  float64 // JVM heap in use
}

// StateStat is the average usage of the container while the server was in
// one state.
type StateStat struct {
	CPU   *float64 `json:"cpu,omitempty"`
	MemMB *float64 `json:"memMb,omitempty"`
}

// Point aggregates the samples of a period (one minute on disk). CPU values
// are numbers of cores used ("docker stats" style: 1.5 = 150 %); memory is
// in MB. Nil means unknown.
type Point struct {
	Time int64 `json:"t"` // start of the period, Unix seconds
	// Java process: average CPU, average and peak resident memory.
	CPU      *float64 `json:"cpu,omitempty"`
	MemMB    *float64 `json:"memMb,omitempty"`
	MemMaxMB *float64 `json:"memMaxMb,omitempty"`
	// Whole container (manager included), as accounted by its cgroup.
	ContainerCPU      *float64 `json:"ccpu,omitempty"`
	ContainerMemMB    *float64 `json:"cmem,omitempty"`
	ContainerMemMaxMB *float64 `json:"cmemMax,omitempty"`
	Players           int      `json:"players"` // peak
	// Readings of the MetricsStream plugin (absent while it is off): ticks
	// per second (average and lowest), milliseconds per tick (average and
	// peak), JVM heap in use (average and peak).
	TPS       *float64 `json:"tps,omitempty"`
	TPSMin    *float64 `json:"tpsMin,omitempty"`
	MSPT      *float64 `json:"mspt,omitempty"`
	MSPTMax   *float64 `json:"msptMax,omitempty"`
	HeapMB    *float64 `json:"heapMb,omitempty"`
	HeapMaxMB *float64 `json:"heapMaxMb,omitempty"`
	// Seconds spent in each state during the period.
	Running  float64 `json:"running"`
	Sleeping float64 `json:"sleeping"`
	Stopped  float64 `json:"stopped"`
	// Container usage per state.
	States map[string]StateStat `json:"states,omitempty"`
}

// Summary totals a range.
type Summary struct {
	RunningSeconds  float64 `json:"runningSeconds"`
	SleepingSeconds float64 `json:"sleepingSeconds"`
	StoppedSeconds  float64 `json:"stoppedSeconds"`
	// Java process: peak memory used, average CPU while running.
	PeakMemMB *float64 `json:"peakMemMb,omitempty"`
	AvgCPU    *float64 `json:"avgCpu,omitempty"`
	// Whole container: peak memory, average CPU, and averages per state.
	ContainerPeakMemMB *float64             `json:"containerPeakMemMb,omitempty"`
	ContainerAvgCPU    *float64             `json:"containerAvgCpu,omitempty"`
	States             map[string]StateStat `json:"states,omitempty"`
	PeakPlayers        int                  `json:"peakPlayers"`
	// MetricsStream plugin: average and lowest TPS, average MSPT, peak heap.
	AvgTPS     *float64 `json:"avgTps,omitempty"`
	MinTPS     *float64 `json:"minTps,omitempty"`
	AvgMSPT    *float64 `json:"avgMspt,omitempty"`
	PeakHeapMB *float64 `json:"peakHeapMb,omitempty"`
}

// Series is a chart-ready history.
type Series struct {
	From     int64   `json:"from"`
	To       int64   `json:"to"`
	Step     int64   `json:"step"` // seconds per point
	Points   []Point `json:"points"`
	Summary  Summary `json:"summary"`
	CPUCount int     `json:"cpuCount"`
	// ProcessMetrics is false where processes cannot be measured (outside
	// Linux): only players and states are recorded.
	ProcessMetrics bool `json:"processMetrics"`
	// ContainerMetrics is true when the container's cgroup can be read.
	ContainerMetrics bool `json:"containerMetrics"`
	// PluginMetrics is true when the MetricsStream plugin contributed data.
	PluginMetrics bool `json:"pluginMetrics"`
}

// Deps lists the collaborators of the Collector.
type Deps struct {
	Probe func() Probe
	// Path of the history file ("" keeps the history in memory only).
	Path string
	Now  func() time.Time
	// Process reads the cumulated CPU time and resident memory of a process
	// (defaults to /proc on Linux).
	Process func(pid int) (cpu time.Duration, rssBytes int64, ok bool)
	// Container reads the cumulated CPU time and memory of the whole
	// container (defaults to its cgroup on Linux).
	Container func() (cpu time.Duration, memBytes int64, ok bool)
}

// Collector samples the server and keeps its history.
type Collector struct {
	d Deps

	mu            sync.Mutex
	history       []Point // closed minutes, oldest first
	current       minute
	lastProcess   cpuReading
	lastContainer cpuReading
	containerSeen bool
}

// gauge accumulates the samples of one measure.
type gauge struct {
	sum, min, max float64
	n             int
}

func (g *gauge) add(v float64) {
	if g.n == 0 {
		g.min = v
	}
	g.sum += v
	g.min = min(g.min, v)
	g.max = max(g.max, v)
	g.n++
}

func (g gauge) avg() *float64 {
	if g.n == 0 {
		return nil
	}
	return ptr(round(g.sum / float64(g.n)))
}

func (g gauge) lowest() *float64 {
	if g.n == 0 {
		return nil
	}
	return ptr(round(g.min))
}

func (g gauge) peak() *float64 {
	if g.n == 0 {
		return nil
	}
	return ptr(round(g.max))
}

// minute accumulates the samples of the current minute.
type minute struct {
	start      time.Time
	cpu, mem   gauge // Java process
	ccpu, cmem gauge // container
	byState    [len(states)]struct {
		seconds    float64
		ccpu, cmem gauge
	}
	players int
	// MetricsStream plugin readings.
	tps, mspt, msptMax, heap gauge
}

type cpuReading struct {
	pid  int // process readings only
	at   time.Time
	used time.Duration
}

// rate turns two cumulated CPU readings into a number of cores.
func (r cpuReading) rate(next cpuReading) (float64, bool) {
	if r.at.IsZero() || r.pid != next.pid || !next.at.After(r.at) || next.used < r.used {
		return 0, false
	}
	return (next.used - r.used).Seconds() / next.at.Sub(r.at).Seconds(), true
}

// New creates a collector, loading the saved history; call Run to start it.
func New(d Deps) *Collector {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Process == nil {
		d.Process = readProcess
	}
	if d.Container == nil {
		d.Container = readContainer
	}
	c := &Collector{d: d}
	if d.Path != "" {
		var saved []Point
		if _, err := fsutil.ReadJSON(d.Path, &saved); err == nil {
			c.history = saved
		}
	}
	return c
}

// Run samples until ctx ends, then saves the history.
func (c *Collector) Run(ctx context.Context) {
	ticker := time.NewTicker(sampleInterval)
	defer ticker.Stop()
	lastSave := c.d.Now()
	for {
		select {
		case <-ctx.Done():
			c.save()
			return
		case <-ticker.C:
		}
		c.sample(sampleInterval)
		if c.d.Now().Sub(lastSave) >= saveInterval {
			c.save()
			lastSave = c.d.Now()
		}
	}
}

// sample records the situation over the last elapsed period.
func (c *Collector) sample(elapsed time.Duration) {
	now := c.d.Now()
	probe := c.d.Probe()
	var procCores, procMem, contCores, contMem float64
	var procCPU, procMemOK, contCPU, contMemOK bool
	if probe.PID > 0 {
		if used, rss, ok := c.d.Process(probe.PID); ok {
			procMem, procMemOK = float64(rss)/(1<<20), true
			reading := cpuReading{pid: probe.PID, at: now, used: used}
			procCores, procCPU = c.lastProcess.rate(reading)
			c.lastProcess = reading
		}
	}
	if used, mem, ok := c.d.Container(); ok {
		contMem, contMemOK = float64(mem)/(1<<20), true
		reading := cpuReading{at: now, used: used}
		contCores, contCPU = c.lastContainer.rate(reading)
		c.lastContainer = reading
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	start := now.Truncate(bucket)
	if !c.current.start.Equal(start) {
		c.closeMinuteLocked()
		c.current = minute{start: start}
	}
	m := &c.current
	st := &m.byState[stateIndex(probe.State)]
	st.seconds += elapsed.Seconds()
	m.players = max(m.players, probe.Players)
	if pl := probe.Plugin; pl != nil {
		m.tps.add(pl.TPS)
		m.mspt.add(pl.MSPT)
		m.msptMax.add(pl.MSPTMax)
		m.heap.add(pl.HeapMB)
	}
	if procMemOK {
		m.mem.add(procMem)
	}
	if procCPU {
		m.cpu.add(procCores)
	}
	if contMemOK {
		c.containerSeen = true
		m.cmem.add(contMem)
		st.cmem.add(contMem)
	}
	if contCPU {
		m.ccpu.add(contCores)
		st.ccpu.add(contCores)
	}
}

func (c *Collector) closeMinuteLocked() {
	if c.current.start.IsZero() {
		return
	}
	c.history = append(c.history, c.current.point())
	cutoff := c.d.Now().Add(-retention).Unix()
	drop := 0
	for drop < len(c.history) && c.history[drop].Time < cutoff {
		drop++
	}
	c.history = c.history[drop:]
}

// point summarises the minute.
func (m minute) point() Point {
	p := Point{
		Time: m.start.Unix(), Players: m.players,
		Running: m.byState[0].seconds, Sleeping: m.byState[1].seconds, Stopped: m.byState[2].seconds,
		CPU: m.cpu.avg(), MemMB: m.mem.avg(), MemMaxMB: m.mem.peak(),
		ContainerCPU: m.ccpu.avg(), ContainerMemMB: m.cmem.avg(), ContainerMemMaxMB: m.cmem.peak(),
		TPS: m.tps.avg(), TPSMin: m.tps.lowest(), MSPT: m.mspt.avg(), MSPTMax: m.msptMax.peak(),
		HeapMB: m.heap.avg(), HeapMaxMB: m.heap.peak(),
	}
	for i, s := range m.byState {
		if s.ccpu.n > 0 || s.cmem.n > 0 {
			if p.States == nil {
				p.States = make(map[string]StateStat, len(states))
			}
			p.States[states[i]] = StateStat{CPU: s.ccpu.avg(), MemMB: s.cmem.avg()}
		}
	}
	return p
}

func (c *Collector) save() {
	if c.d.Path == "" {
		return
	}
	c.mu.Lock()
	snapshot := append([]Point(nil), c.history...)
	c.mu.Unlock()
	_ = fsutil.WriteJSONAtomic(c.d.Path, snapshot, 0o600) // best effort: metrics only
}

// Series returns the history of the last span, merged into at most
// maxPoints points.
func (c *Collector) Series(span time.Duration) Series {
	now := c.d.Now()
	from := now.Add(-span).Truncate(bucket)
	stepMinutes := max(1, int64(math.Ceil(span.Minutes()/maxPoints)))
	step := time.Duration(stepMinutes) * bucket

	c.mu.Lock()
	minutes := make([]Point, 0, len(c.history)+1)
	for _, p := range c.history {
		if p.Time >= from.Unix() {
			minutes = append(minutes, p)
		}
	}
	if !c.current.start.IsZero() {
		minutes = append(minutes, c.current.point()) // minute in progress
	}
	containerSeen := c.containerSeen
	c.mu.Unlock()

	s := Series{
		From: from.Unix(), To: now.Unix(), Step: int64(step.Seconds()),
		CPUCount: runtime.NumCPU(), ProcessMetrics: processMetricsAvailable,
		ContainerMetrics: containerSeen,
	}
	var group []Point
	flush := func() {
		if len(group) > 0 {
			s.Points = append(s.Points, merge(group))
			group = group[:0]
		}
	}
	for _, p := range minutes {
		if len(group) > 0 && (p.Time-group[0].Time) >= int64(step.Seconds()) {
			flush()
		}
		if len(group) == 0 {
			// Align groups on the step so that points stay put between refreshes.
			p.Time -= (p.Time - from.Unix()) % int64(step.Seconds())
		}
		group = append(group, p)
	}
	flush()
	s.Summary = summarize(minutes)
	for _, p := range minutes {
		if p.TPS != nil {
			s.PluginMetrics = true
			break
		}
	}
	if s.Summary.ContainerAvgCPU != nil || s.Summary.ContainerPeakMemMB != nil {
		s.ContainerMetrics = true
	}
	return s
}

// weighted averages values, each counting for its weight (seconds).
type weighted struct{ sum, weight float64 }

func (w *weighted) add(v *float64, weight float64) {
	if v != nil && weight > 0 {
		w.sum += *v * weight
		w.weight += weight
	}
}

func (w weighted) avg() *float64 {
	if w.weight == 0 {
		return nil
	}
	return ptr(round(w.sum / w.weight))
}

func maxOf(a, b *float64) *float64 {
	if b == nil || (a != nil && *a >= *b) {
		return a
	}
	return ptr(*b)
}

// combine merges points; what is averaged weighs by the seconds it covers.
func combine(points []Point) (out Point, cpu, mem, ccpu, cmem weighted, byState [len(states)]struct{ cpu, mem weighted }) {
	for _, p := range points {
		total := p.Running + p.Sleeping + p.Stopped
		out.Running += p.Running
		out.Sleeping += p.Sleeping
		out.Stopped += p.Stopped
		out.Players = max(out.Players, p.Players)
		cpu.add(p.CPU, p.Running)
		mem.add(p.MemMB, p.Running)
		ccpu.add(p.ContainerCPU, total)
		cmem.add(p.ContainerMemMB, total)
		out.MemMaxMB = maxOf(out.MemMaxMB, p.MemMaxMB)
		out.ContainerMemMaxMB = maxOf(out.ContainerMemMaxMB, p.ContainerMemMaxMB)
		seconds := [len(states)]float64{p.Running, p.Sleeping, p.Stopped}
		for i, name := range states {
			if st, ok := p.States[name]; ok {
				byState[i].cpu.add(st.CPU, seconds[i])
				byState[i].mem.add(st.MemMB, seconds[i])
			}
		}
	}
	return
}

func stateStats(byState [len(states)]struct{ cpu, mem weighted }) map[string]StateStat {
	var out map[string]StateStat
	for i, name := range states {
		st := StateStat{CPU: byState[i].cpu.avg(), MemMB: byState[i].mem.avg()}
		if st.CPU != nil || st.MemMB != nil {
			if out == nil {
				out = make(map[string]StateStat, len(states))
			}
			out[name] = st
		}
	}
	return out
}

// merge combines consecutive minutes into one point.
func merge(group []Point) Point {
	out, cpu, mem, ccpu, cmem, byState := combine(group)
	out.Time = group[0].Time
	out.CPU, out.MemMB = cpu.avg(), mem.avg()
	out.ContainerCPU, out.ContainerMemMB = ccpu.avg(), cmem.avg()
	out.States = stateStats(byState)
	mergePlugin(&out, group)
	return out
}

// mergePlugin aggregates the plugin readings of points into out: averages
// weigh by the seconds the server ran, extremes are kept.
func mergePlugin(out *Point, points []Point) {
	var tps, mspt, heap weighted
	for _, p := range points {
		tps.add(p.TPS, p.Running)
		mspt.add(p.MSPT, p.Running)
		heap.add(p.HeapMB, p.Running)
		out.TPSMin = minOf(out.TPSMin, p.TPSMin)
		out.MSPTMax = maxOf(out.MSPTMax, p.MSPTMax)
		out.HeapMaxMB = maxOf(out.HeapMaxMB, p.HeapMaxMB)
	}
	out.TPS, out.MSPT, out.HeapMB = tps.avg(), mspt.avg(), heap.avg()
}

func minOf(a, b *float64) *float64 {
	if b == nil || (a != nil && *a <= *b) {
		return a
	}
	return ptr(*b)
}

func summarize(points []Point) Summary {
	p, cpu, _, ccpu, _, byState := combine(points)
	var plugin Point
	mergePlugin(&plugin, points)
	return Summary{
		AvgTPS: plugin.TPS, MinTPS: plugin.TPSMin, AvgMSPT: plugin.MSPT, PeakHeapMB: plugin.HeapMaxMB,
		RunningSeconds:     p.Running,
		SleepingSeconds:    p.Sleeping,
		StoppedSeconds:     p.Stopped,
		PeakMemMB:          p.MemMaxMB,
		AvgCPU:             cpu.avg(),
		ContainerPeakMemMB: p.ContainerMemMaxMB,
		ContainerAvgCPU:    ccpu.avg(),
		States:             stateStats(byState),
		PeakPlayers:        p.Players,
	}
}

func ptr(v float64) *float64 { return &v }

func round(v float64) float64 { return math.Round(v*100) / 100 }
