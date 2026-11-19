package metrics

import "math"

// VanillaPercentiles are /tick query's P50, P95 and P99: the sorted element
// at that fraction of the length, rounded down.
// vanilla: TickCommand.tickQuery
func VanillaPercentiles(sorted []int64) (p50, p95, p99 int64) {
	n := len(sorted)
	if n == 0 {
		return 0, 0, 0
	}
	return sorted[n/2], sorted[int(float64(n)*0.95)], sorted[int(float64(n)*0.99)]
}

// Percentile is spark's sample at p (0..1) of the sorted length, rounded up;
// zero for none.
// spark: RollingAverage.percentile
func Percentile(sorted []int64, p float64) int64 {
	if len(sorted) == 0 {
		return 0
	}
	return sorted[int(math.Ceil(p*float64(len(sorted)-1)))]
}

// Summary is Paper's /mspt mean, min and max, in nanoseconds; zero if empty.
// Paper: PaperMSPTCommand.eval, TickData.computeSegmentData
type Summary struct {
	Mean     float64
	Min, Max int64
}

// Summarize summarizes the sorted samples.
func Summarize(sorted []int64) Summary {
	if len(sorted) == 0 {
		return Summary{}
	}
	var sum int64
	for _, v := range sorted {
		sum += v
	}
	return Summary{
		Mean: float64(sum) / float64(len(sorted)),
		Min:  sorted[0],
		Max:  sorted[len(sorted)-1],
	}
}
