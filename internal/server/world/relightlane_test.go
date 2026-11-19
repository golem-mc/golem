package world

import (
	"math/rand/v2"
	"sync"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// TestQueuedRelightsLightLikeOneByOne writes the same blocks to two worlds:
// one relights each write before the next, the other queues all its relights
// from many goroutines at once, for the lane to merge. Every queued request
// is answered, and the light ends the same.
func TestQueuedRelightsLightLikeOneByOne(t *testing.T) {
	t.Parallel()
	queued := newTestWorld(t, testTerrain(31, false))
	serial := newTestWorld(t, testTerrain(31, false))
	var positions []ChunkPos
	for cx := int32(-1); cx <= 1; cx++ {
		for cz := int32(-1); cz <= 1; cz++ {
			positions = append(positions, ChunkPos{cx, cz})
			queued.Acquire(ChunkPos{cx, cz})
			serial.Acquire(ChunkPos{cx, cz})
		}
	}
	states := []uint16{stoneState, torchState, airState, registry.BlockStates["minecraft:glowstone"]}
	rng := rand.New(rand.NewPCG(31, 31))
	type write struct {
		x, y, z int32
		state   uint16
	}
	writes := make([]write, 300)
	for i := range writes {
		writes[i] = write{rng.Int32N(48) - 16, 55 + rng.Int32N(30), rng.Int32N(48) - 16, states[rng.IntN(len(states))]}
	}
	for _, wr := range writes {
		serial.SetBlock(wr.x, wr.y, wr.z, wr.state)
		serial.Relight(wr.x, wr.z)
	}

	// Nobody waits on an answer before queueing the next, so the lane finds
	// long batches to merge.
	pending := make(chan (<-chan []LightChange), len(writes))
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			for j := i; j < len(writes); j += 8 {
				wr := writes[j]
				queued.SetBlock(wr.x, wr.y, wr.z, wr.state)
				pending <- queued.RelightAsync(wr.x, wr.z)
			}
		})
	}
	wg.Wait()
	close(pending)
	for answer := range pending {
		<-answer
	}
	// The interleaved writes may land in another order than the serial
	// world's; the cells written more than once are set to the serial
	// world's last state and relit, so both worlds hold the same blocks.
	for _, wr := range writes {
		if s := serial.BlockAt(wr.x, wr.y, wr.z); queued.BlockAt(wr.x, wr.y, wr.z) != s {
			queued.SetBlock(wr.x, wr.y, wr.z, s)
			<-queued.RelightAsync(wr.x, wr.z)
		}
	}
	compareLight(t, 31, queued, serial, positions)
}
