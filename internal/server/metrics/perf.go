package metrics

import (
	"bytes"
	"runtime/pprof"
	"time"
)

// PerfTicks is how many ticks a /perf run lasts before it stops itself.
// vanilla: PerfCommand.PROFILING_RUN_SECONDS (10 s)
const PerfTicks = 200

// perfRun is the CPU profile and tick window /perf start opened.
type perfRun struct {
	cpu        bytes.Buffer
	startTicks int64
	start      time.Time
	onLimit    func()
}

// PerfReport is what a finished /perf run measured.
type PerfReport struct {
	Elapsed time.Duration
	Ticks   int64
	// CPUProfile is the pprof CPU profile of the window.
	CPUProfile []byte
	// Samples are the durations of the window's ticks in nanoseconds,
	// oldest first, as far as the history still holds them.
	Samples []int64
}

// StartPerf begins a /perf run. started is false when one is already
// running or the process's CPU profiler is taken; onLimit runs on its own
// goroutine once the run has lasted PerfTicks ticks.
// vanilla: PerfCommand.startProfilingRun
func (r *Recorder) StartPerf(now time.Time, onLimit func()) (started, busy bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.perf != nil {
		return false, false
	}
	run := &perfRun{startTicks: r.count, start: now, onLimit: onLimit}
	if err := pprof.StartCPUProfile(&run.cpu); err != nil {
		return false, true
	}
	r.perf = run
	return true, false
}

// StopPerf ends the /perf run; ok is false when none was running.
// vanilla: PerfCommand.stopProfilingRun
func (r *Recorder) StopPerf(now time.Time) (PerfReport, bool) {
	r.mu.Lock()
	run := r.perf
	if run == nil {
		r.mu.Unlock()
		return PerfReport{}, false
	}
	r.perf = nil
	rep := PerfReport{Elapsed: now.Sub(run.start), Ticks: r.count - run.startTicks}
	for i := min(rep.Ticks, r.held()) - 1; i >= 0; i-- {
		t := r.last(i)
		rep.Samples = append(rep.Samples, t.end-t.start)
	}
	r.mu.Unlock()
	pprof.StopCPUProfile()
	rep.CPUProfile = run.cpu.Bytes()
	return rep, true
}

// perfLimit fires the limit callback of a /perf run that has covered
// PerfTicks ticks; r.mu is held.
func (r *Recorder) perfLimit() {
	if run := r.perf; run != nil && run.onLimit != nil && r.count-run.startTicks >= PerfTicks {
		go run.onLimit()
		run.onLimit = nil
	}
}
