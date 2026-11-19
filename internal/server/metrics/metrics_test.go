package metrics

import (
	"math"
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
