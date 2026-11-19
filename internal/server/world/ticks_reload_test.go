package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestReloadedChunkRebooksBeforePhysicsTick(t *testing.T) {
	for _, kind := range []string{"block", "fluid", "booked", "event", "finalize", "fall"} {
		t.Run(kind, func(t *testing.T) {
			w, p := newOneChunkPhysics(t)
			key := blockKey{8, 70, 8}
			state := registry.BlockStone.DefaultState()
			if kind == "booked" {
				state = registry.BlockOakLeaves.DefaultState()
			}
			if kind == "fluid" {
				state = registry.BlockWater.DefaultState()
			}
			if kind == "fall" {
				state = registry.MustState("minecraft:pointed_dripstone", map[string]string{
					"vertical_direction": "down", "thickness": "tip",
				})
			}
			w.SetBlock(key.x, key.y, key.z, state)
			p.eventDelay = 1
			book := func(due int64) {
				p.mu.Lock()
				defer p.mu.Unlock()
				switch kind {
				case "block", "fluid":
					p.schedule(key.x, key.y, key.z, due)
				case "booked":
					p.scheduleLeaf(key.x, key.y, key.z, due)
				case "event":
					p.schedulePistonEvent(key.x, key.y, key.z, dirUp, 0)
				case "finalize":
					p.schedulePistonFinalize(key.x, key.y, key.z, due)
				case "fall":
					p.adoptBlockTick(key.x, key.y, key.z, state, "minecraft:pointed_dripstone", due, tickPriorityNormal)
				}
			}
			book(20)
			oldSeq := p.seq
			w.Release(ChunkPos{})
			w.FlushSaves()
			if w.ChunkLoaded(ChunkPos{}) {
				t.Fatal("setup: chunk did not unload")
			}
			w.Acquire(ChunkPos{})
			book(40)
			if len(p.queue) != 1 || p.queue[0].seq <= oldSeq {
				t.Fatalf("new booking kept the old tick: %+v", p.queue)
			}
			seq := p.queue[0].seq
			if kind == "fall" && !p.fallBooked[key] {
				t.Fatal("new fall lost its booking")
			}
			p.Tick(0)
			if len(p.queue) != 1 || p.queue[0].seq != seq {
				t.Fatalf("adoption discarded or replaced the new booking: %+v", p.queue)
			}
		})
	}
}
