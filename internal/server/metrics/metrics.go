// Package metrics keeps the main loop's tick times (vanilla's /tick query,
// the F3 TPS chart, Paper's /tps and /mspt) and the process's CPU and memory.
// Recording a tick costs a lock and a few stores; the rest is computed on ask.
package metrics

import (
	"slices"
	"sync"
	"time"
)

// TickStatsSpan is how many tick durations vanilla's /tick query reads.
// vanilla: MinecraftServer.TICK_STATS_SPAN
const TickStatsSpan = 100

// historyLen holds 15 minutes at 20 TPS, /tps's longest window, plus slack
// for a loop that ticks early.
const historyLen = 15*60*20 + 200

// cpuEvery is the CPU sampling interval; cpuLen covers the longest (15
// minute) CPU window.
const (
	cpuEvery = time.Second
	cpuLen   = 15*60 + 2
)

// noTick marks a tick with no tick before it.
const noTick = -1

// tickTime is one tick of the main loop, in nanoseconds since the recorder's
// epoch. Paper: ca.spottedleaf.common.time.TickTime
type tickTime struct {
	prevStart, start, end int64
}

type cpuSample struct {
	wall, cpu int64
}

// Recorder keeps the tick history and CPU samples. Only the loop calls Tick;
// the queries are safe from any goroutine.
type Recorder struct {
	mu     sync.Mutex
	epoch  time.Time
	target time.Duration

	ticks     [historyLen]tickTime
	count     int64 // ticks recorded: vanilla's tickCount
	lastStart int64

	profiling    bool
	profileTicks int64
	profileStart time.Time

	perf *perfRun

	cpu      [cpuLen]cpuSample
	cpuCount int64
	nextCPU  int64
}

// NewRecorder returns a recorder for a loop that aims at one tick per target.
func NewRecorder(target time.Duration) *Recorder {
	return &Recorder{epoch: time.Now(), target: target, lastStart: noTick}
}

// Target is the time one tick is meant to take.
func (r *Recorder) Target() time.Duration { return r.target }

// Tick records one tick of the main loop that ran from start to end.
func (r *Recorder) Tick(start, end time.Time) {
	s, e := start.Sub(r.epoch).Nanoseconds(), end.Sub(r.epoch).Nanoseconds()
	r.mu.Lock()
	r.ticks[r.count%historyLen] = tickTime{prevStart: r.lastStart, start: s, end: e}
	r.count++
	r.lastStart = s
	r.perfLimit()
	if e >= r.nextCPU {
		if cpu, ok := processCPU(); ok {
			r.cpu[r.cpuCount%cpuLen] = cpuSample{wall: e, cpu: cpu}
			r.cpuCount++
		}
		r.nextCPU = e + cpuEvery.Nanoseconds()
	}
	r.mu.Unlock()
}

// Count is how many ticks have been recorded.
func (r *Recorder) Count() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.count
}

// StartProfile begins the time profiler /debug start runs, false when it is
// already running.
// vanilla: MinecraftServer.startTimeProfiler
func (r *Recorder) StartProfile(now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.profiling {
		return false
	}
	r.profiling, r.profileTicks, r.profileStart = true, r.count, now
	return true
}

// StopProfile ends the profiler, answering how long it ran and how many
// ticks the loop recorded meanwhile; false when it was not running.
// vanilla: MinecraftServer.stopTimeProfiler
func (r *Recorder) StopProfile(now time.Time) (elapsed time.Duration, ticks int64, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.profiling {
		return 0, 0, false
	}
	r.profiling = false
	return now.Sub(r.profileStart), r.count - r.profileTicks, true
}

// last returns the i-th most recent tick, 0 the newest; r.mu is held.
func (r *Recorder) last(i int64) tickTime {
	return r.ticks[(r.count-1-i)%historyLen]
}

// held is how many ticks the history still holds; r.mu is held.
func (r *Recorder) held() int64 {
	return min(r.count, historyLen)
}

// TickTimes returns the last TickStatsSpan tick durations in nanoseconds
// (zero where fewer have run) and their sum integer-divided by TickStatsSpan.
// vanilla: MinecraftServer.tickTimesNanos, getAverageTickTimeNanos
func (r *Recorder) TickTimes() (samples [TickStatsSpan]int64, average int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var sum int64
	for i := range min(r.count, TickStatsSpan) {
		t := r.last(i)
		samples[i] = t.end - t.start
		sum += samples[i]
	}
	return samples, sum / min(TickStatsSpan, max(r.count, 1))
}

// DebugSample is the F3 TPS chart sample for the cycle ending at now: full
// cycle, tick, between-tick tasks (always zero: packets are handled on their
// connections' goroutines) and idle, in nanoseconds. ok is false before the
// first tick.
// vanilla: TpsDebugDimensions, MinecraftServer.logFullTickTime
func (r *Recorder) DebugSample(now time.Time) (sample [4]int64, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.count == 0 {
		return sample, false
	}
	t := r.last(0)
	full := now.Sub(r.epoch).Nanoseconds() - t.start
	tick := t.end - t.start
	return [4]int64{full, tick, 0, max(full-tick, 0)}, true
}

// window calls f newest first for each tick that ended no more than window
// before the newest began (Paper's TickData window). r.mu is held.
func (r *Recorder) window(window time.Duration, f func(tickTime)) {
	if r.count == 0 {
		return
	}
	ref := r.last(0).start
	for i := range r.held() {
		t := r.last(i)
		if ref-t.end > window.Nanoseconds() {
			return
		}
		f(t)
	}
}

// TPS is the ticks in the window over the time from each one's predecessor's
// start to its own; an empty window reads as the target rate.
// Paper: TickData.getTPSAverage
func (r *Recorder) TPS(window time.Duration) float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	var n, total int64
	r.window(window, func(t tickTime) {
		n++
		if t.prevStart == noTick {
			total += max(r.target.Nanoseconds(), t.end-t.start)
		} else {
			total += t.start - t.prevStart
		}
	})
	if n == 0 || total <= 0 {
		return float64(time.Second) / float64(r.target)
	}
	return float64(n) / (float64(total) / float64(time.Second))
}

// Durations returns the window's tick durations in nanoseconds, ascending.
func (r *Recorder) Durations(window time.Duration) []int64 {
	r.mu.Lock()
	d := make([]int64, 0, r.held())
	r.window(window, func(t tickTime) { d = append(d, t.end-t.start) })
	r.mu.Unlock()
	slices.Sort(d)
	return d
}

// ProcessCPU is the process's share of the machine's CPU over the window (1
// is every core busy), from the oldest sample inside it to now. ok is false
// without a platform reading or before the first sample.
func (r *Recorder) ProcessCPU(window time.Duration, now time.Time) (float64, bool) {
	cpuNow, ok := processCPU()
	if !ok {
		return 0, false
	}
	wallNow := now.Sub(r.epoch).Nanoseconds()
	r.mu.Lock()
	var from cpuSample
	found := false
	for i := range min(r.cpuCount, cpuLen) {
		s := r.cpu[(r.cpuCount-1-i)%cpuLen]
		if wallNow-s.wall > window.Nanoseconds() {
			break
		}
		from, found = s, true
	}
	r.mu.Unlock()
	if !found || wallNow <= from.wall {
		return 0, false
	}
	return float64(cpuNow-from.cpu) / float64(wallNow-from.wall) / float64(numCPU()), true
}
