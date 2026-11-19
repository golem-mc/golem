package world

import (
	"sync"
	"testing"
)

// TestPistonRideCreateDoesNotRaceTheChunkPacket loops the chunk packet's
// encode of the moving piston's block entity against the ride writes a
// piston's push makes, for the race detector: ChunkPacket reads the ride's
// fields under the entity's lock, and a ride write that skips that lock is
// the race this catches.
func TestPistonRideCreateDoesNotRaceTheChunkPacket(t *testing.T) {
	w, p, blocks := pistonRow(t, false)
	clock := pistonSetup(t, w, p, blocks, false, map[int32]uint16{11: blocks["dirt"]}, nil)
	chunk, _ := w.Acquire(ChunkPos{X: 0, Z: 0})

	const turns = 64
	moves := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		defer close(moves)
		for range turns {
			// The power block beside the piston comes and goes, the
			// way pistonPowerOn and pistonPowerOff drive the row, less
			// the t calls a test goroutine may not make.
			old := w.BlockAt(10, 64, 8)
			w.SetBlock(10, 64, 8, blocks["power"])
			p.NotifyChanged(10, 64, 8, old)
			clock++
			p.Tick(clock)

			old = w.BlockAt(10, 64, 8)
			w.SetBlock(10, 64, 8, airState)
			p.NotifyChanged(10, 64, 8, old)
			clock++
			p.Tick(clock)
		}
	}()
	go func() {
		defer wg.Done()
		for {
			select {
			case <-moves:
				return
			default:
				w.ChunkPacket(chunk)
			}
		}
	}()
	wg.Wait()
}
