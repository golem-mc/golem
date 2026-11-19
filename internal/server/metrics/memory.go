package metrics

import (
	"math"
	"runtime/metrics"
)

// Memory is the Go runtime's memory and GC view, read through runtime/metrics,
// which unlike runtime.ReadMemStats does not stop the world.
type Memory struct {
	// HeapInUse is live plus unswept heap bytes; HeapGoal the next GC's
	// target; Total every byte mapped from the OS (MemStats.Sys).
	HeapInUse, HeapGoal, Total uint64
	// GCCycles counts the collections since the process started.
	GCCycles uint64
	// GCPauseP50 and GCPauseMax are the median and longest GC pause since
	// start, in seconds, from the pause histogram's bucket bounds.
	GCPauseP50, GCPauseMax float64
	// GCCPU is the collector's share of the process's CPU time.
	GCCPU float64
	// Goroutines is how many goroutines are alive.
	Goroutines uint64
}

var memoryNames = []string{
	"/memory/classes/heap/objects:bytes",
	"/gc/heap/goal:bytes",
	"/memory/classes/total:bytes",
	"/gc/cycles/total:gc-cycles",
	"/sched/pauses/total/gc:seconds",
	"/cpu/classes/gc/total:cpu-seconds",
	"/cpu/classes/total:cpu-seconds",
	"/sched/goroutines:goroutines",
}

// ReadMemory reads the runtime's memory and collector numbers.
func ReadMemory() Memory {
	samples := make([]metrics.Sample, len(memoryNames))
	for i, name := range memoryNames {
		samples[i].Name = name
	}
	metrics.Read(samples)

	u := func(i int) uint64 {
		if samples[i].Value.Kind() != metrics.KindUint64 {
			return 0
		}
		return samples[i].Value.Uint64()
	}
	f := func(i int) float64 {
		if samples[i].Value.Kind() != metrics.KindFloat64 {
			return 0
		}
		return samples[i].Value.Float64()
	}
	m := Memory{
		HeapInUse:  u(0),
		HeapGoal:   u(1),
		Total:      u(2),
		GCCycles:   u(3),
		Goroutines: u(7),
	}
	if samples[4].Value.Kind() == metrics.KindFloat64Histogram {
		h := samples[4].Value.Float64Histogram()
		m.GCPauseP50 = histogramQuantile(h, 0.5)
		m.GCPauseMax = histogramQuantile(h, 1)
	}
	if total := f(6); total > 0 {
		m.GCCPU = f(5) / total
	}
	return m
}

// histogramQuantile is the upper bound of the q-th quantile's bucket (the
// lower bound where the upper is infinite); zero for an empty histogram.
func histogramQuantile(h *metrics.Float64Histogram, q float64) float64 {
	var total uint64
	for _, c := range h.Counts {
		total += c
	}
	if total == 0 {
		return 0
	}
	rank := uint64(math.Ceil(q * float64(total)))
	var seen uint64
	for i, c := range h.Counts {
		seen += c
		if c > 0 && seen >= rank {
			if hi := h.Buckets[i+1]; !math.IsInf(hi, 1) {
				return hi
			}
			return h.Buckets[i]
		}
	}
	return 0
}
