package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkCommandBlockChain100 walks one hundred inactive chain blocks,
// isolating the chain's state and entity lookups from command runs.
func BenchmarkCommandBlockChain100(b *testing.B) {
	s, w := newCommandBenchServer(b, 0)
	const y = int32(world.GroundLevel + 2)
	for cx := int32(1); cx <= 7; cx++ {
		pos := world.ChunkPos{X: cx, Z: 0}
		w.Acquire(pos)
		b.Cleanup(func() { w.Release(pos) })
	}
	origin, ok := registry.FindState("minecraft:command_block", map[string]string{
		"facing": "east", "conditional": "false",
	})
	chain, okChain := registry.FindState("minecraft:chain_command_block", map[string]string{
		"facing": "east", "conditional": "false",
	})
	if !ok || !okChain {
		b.Fatal("the registry has no command blocks")
	}
	if !w.SetBlock(2, y, 2, origin) {
		b.Fatal("the origin did not place")
	}
	for x := int32(3); x < 103; x++ {
		if !w.SetBlock(x, y, 2, chain) {
			b.Fatalf("chain block %d did not place", x)
		}
		be := w.EnsureBlockEntity(x, y, 2, world.KindCommandBlock)
		if be == nil {
			b.Fatalf("chain block %d has no entity", x)
		}
		be.Use(func(be *world.BlockEntity) {
			be.Automatic = false
			be.Powered = false
		})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		s.executeCommandChain(w, 2, y, 2, origin)
	}
}
