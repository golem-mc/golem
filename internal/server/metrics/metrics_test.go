package metrics

import (
	"math"
	rtmetrics "runtime/metrics"
	"testing"
	"time"
)

const tick = 50 * time.Millisecond

// run records n ticks of the given length, one every interval, starting at
// the recorder's epoch plus from, and returns where the next would start.
func run(r *Recorder, from time.Duration, n int, interval, length time.Duration) time.Duration {
	for range n {
		start := r.epoch.Add(from)
		r.Tick(start, start.Add(length))
		from += interval
	}
	return from
}

func TestTickTimesDividesByTheTicksRunUntilTheSpanIsFull(t *testing.T) {
	r := NewRecorder(tick)
	if _, avg := r.TickTimes(); avg != 0 {
		t.Fatalf("average before any tick = %d, want 0", avg)
	}
	run(r, 0, 10, tick, 4*time.Millisecond)
	samples, avg := r.TickTimes()
	if avg != (4 * time.Millisecond).Nanoseconds() {
		t.Errorf("average of 10 ticks = %d, want 4ms", avg)
	}
	nonZero := 0
	for _, s := range samples {
		if s != 0 {
			nonZero++
		}
	}
	if nonZero != 10 {
		t.Errorf("%d samples set, want the 10 ticks run", nonZero)
	}

	// 150 more ticks of 2ms push the 4ms ones out of the span.
	run(r, time.Second, 150, tick, 2*time.Millisecond)
	if _, avg := r.TickTimes(); avg != (2 * time.Millisecond).Nanoseconds() {
		t.Errorf("average after 160 ticks = %d, want 2ms", avg)
	}
}

func TestVanillaPercentilesIndexTheSortedSamples(t *testing.T) {
	sorted := make([]int64, TickStatsSpan)
	for i := range sorted {
		sorted[i] = int64(i + 1)
	}
	p50, p95, p99 := VanillaPercentiles(sorted)
	if p50 != 51 || p95 != 96 || p99 != 100 {
		t.Errorf("percentiles of 1..100 = %d %d %d, want 51 96 100", p50, p95, p99)
	}
}

func TestPercentileRoundsTheRankUp(t *testing.T) {
	sorted := []int64{1, 2, 3, 4, 5}
	for _, c := range []struct {
		p    float64
		want int64
	}{{0, 1}, {0.25, 2}, {0.5, 3}, {0.95, 5}, {1, 5}} {
		if got := Percentile(sorted, c.p); got != c.want {
			t.Errorf("Percentile(%v) = %d, want %d", c.p, got, c.want)
		}
	}
	if got := Percentile(nil, 0.5); got != 0 {
		t.Errorf("Percentile of nothing = %d, want 0", got)
	}
}

func TestTPSCountsTicksOverTheTimeBetweenTheirStarts(t *testing.T) {
	r := NewRecorder(tick)
	if got := r.TPS(time.Minute); got != 20 {
		t.Errorf("TPS before any tick = %v, want the target 20", got)
	}
	next := run(r, 0, 1200, tick, time.Millisecond)
	if got := r.TPS(time.Minute); math.Abs(got-20) > 1e-9 {
		t.Errorf("TPS at one tick per 50ms = %v, want 20", got)
	}
	// Half a minute at 10 a second: the minute holds both rates, the five
	// minutes as well, weighted by the time each took.
	run(r, next, 300, 2*tick, time.Millisecond)
	if got := r.TPS(30 * time.Second); math.Abs(got-10) > 0.05 {
		t.Errorf("TPS over the slow half minute = %v, want 10", got)
	}
	if got := r.TPS(time.Minute); math.Abs(got-15) > 0.05 {
		t.Errorf("TPS over the minute = %v, want 15", got)
	}
}

func TestDurationsKeepToTheWindow(t *testing.T) {
	r := NewRecorder(tick)
	next := run(r, 0, 200, tick, 30*time.Millisecond) // 10s of slow ticks
	run(r, next, 100, tick, 10*time.Millisecond)      // then 5s of fast ones
	d := r.Durations(5 * time.Second)
	s := Summarize(d)
	if s.Max != (30*time.Millisecond).Nanoseconds() || s.Min != (10*time.Millisecond).Nanoseconds() {
		// The tick that ended right at the window's edge is kept, as
		// Paper keeps it.
		t.Errorf("5s window min/max = %d/%d, want 10ms/30ms", s.Min, s.Max)
	}
	if len(d) != 101 {
		t.Errorf("5s window holds %d ticks, want 101", len(d))
	}
	if got := Summarize(r.Durations(time.Minute)); got.Mean < 23e6 || got.Mean > 24e6 {
		t.Errorf("minute mean = %v, want about 23.3ms", got.Mean)
	}
	if got := Summarize(nil); got != (Summary{}) {
		t.Errorf("empty summary = %+v", got)
	}
}

func TestDebugSampleSplitsTheCycle(t *testing.T) {
	r := NewRecorder(tick)
	if _, ok := r.DebugSample(time.Now()); ok {
		t.Fatal("a sample before any tick")
	}
	start := r.epoch.Add(time.Second)
	r.Tick(start, start.Add(12*time.Millisecond))
	got, ok := r.DebugSample(start.Add(tick))
	want := [4]int64{50e6, 12e6, 0, 38e6}
	if !ok || got != want {
		t.Errorf("sample = %v, want %v", got, want)
	}
}

func TestTickDoesNotAllocate(t *testing.T) {
	r := NewRecorder(tick)
	start := time.Now()
	if n := testing.AllocsPerRun(1000, func() {
		r.Tick(start, start.Add(time.Millisecond))
		r.nextCPU = 0 // sample the CPU every time
		start = start.Add(tick)
	}); n != 0 {
		t.Errorf("Tick allocates %v times", n)
	}
}

func TestProcessCPUMeasuresFromTheWindowsOldestSample(t *testing.T) {
	r := NewRecorder(tick)
	if _, ok := processCPU(); !ok {
		t.Skip("no process CPU time on this platform")
	}
	start := time.Now()
	r.Tick(start, start)
	for time.Since(start) < 20*time.Millisecond {
		// Burn some CPU for the sample to see.
	}
	got, ok := r.ProcessCPU(time.Minute, time.Now())
	if !ok || got <= 0 || got > 1.01 {
		t.Errorf("process CPU = %v (ok %v), want a share in (0, 1]", got, ok)
	}
}

func TestHistogramQuantileReadsTheBucketBounds(t *testing.T) {
	h := &rtmetrics.Float64Histogram{
		Counts:  []uint64{2, 0, 1, 1},
		Buckets: []float64{0, 1, 2, 3, math.Inf(1)},
	}
	if got := histogramQuantile(h, 0.5); got != 1 {
		t.Errorf("median = %v, want 1", got)
	}
	if got := histogramQuantile(h, 1); got != 3 {
		t.Errorf("max = %v, want the open bucket's lower bound 3", got)
	}
	if got := histogramQuantile(&rtmetrics.Float64Histogram{Counts: []uint64{0}, Buckets: []float64{0, 1}}, 1); got != 0 {
		t.Errorf("empty = %v, want 0", got)
	}
}

func TestReadMemory(t *testing.T) {
	m := ReadMemory()
	if m.HeapInUse == 0 || m.Total < m.HeapInUse || m.Goroutines == 0 {
		t.Errorf("memory = %+v", m)
	}
}
