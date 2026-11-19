package rules

import "math"

// Chunk batch pacing limits.
// vanilla: PlayerChunkSender
const (
	startChunksPerTick       = 9.0
	minChunksPerTick         = 0.01
	maxChunksPerTick         = 64.0
	maxUnacknowledgedBatches = 10
)

// ChunkPacer is one connection's chunk batch pacing: a quota grows each tick
// by the client's last rate, storing at most one rate's worth, and one batch
// of up to that many chunks leaves a tick. Before the first acknowledgement
// at most one batch is in flight, then ten. Zero is unstarted; the caller
// serialises access.
// vanilla: PlayerChunkSender
type ChunkPacer struct {
	rate              float32
	quota             float32
	unacknowledged    int
	maxUnacknowledged int
}

// Start gives an unstarted pacer vanilla's initial rate and cap, and leaves a
// started one alone so its rate survives a dimension change.
func (p *ChunkPacer) Start() {
	if p.maxUnacknowledged == 0 {
		p.rate = startChunksPerTick
		p.maxUnacknowledged = 1
	}
}

// Take accounts one batch against the pacing and returns how many of the
// pending chunks it may hold; zero sends nothing and charges nothing.
// vanilla: PlayerChunkSender.sendNextChunks
func (p *ChunkPacer) Take(pending int) int {
	if p.unacknowledged >= p.maxUnacknowledged {
		return 0
	}
	maxBatch := max(1.0, p.rate)
	p.quota = min(p.quota+p.rate, maxBatch)
	if p.quota < 1 || pending == 0 {
		return 0
	}
	size := min(pending, int(p.quota))
	p.unacknowledged++
	p.quota -= float32(size)
	return size
}

// Acknowledge counts an answered batch and stores the requested rate,
// clamped (NaN to the floor). With nothing outstanding the quota tops up to
// one chunk, so a prompt client resumes at once.
// vanilla: PlayerChunkSender.onChunkBatchReceivedByClient
func (p *ChunkPacer) Acknowledge(rate float32) {
	p.unacknowledged = max(0, p.unacknowledged-1)
	if math.IsNaN(float64(rate)) {
		rate = minChunksPerTick
	} else {
		rate = min(maxChunksPerTick, max(minChunksPerTick, rate))
	}
	p.rate = rate
	if p.unacknowledged == 0 {
		p.quota = 1
	}
	p.maxUnacknowledged = maxUnacknowledgedBatches
}
