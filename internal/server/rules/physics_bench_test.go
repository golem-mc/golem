package rules

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// BenchmarkMovementBlockReads runs the per-cell block reads a body's tick
// makes over a mix of ordinary and special states.
func BenchmarkMovementBlockReads(b *testing.B) {
	var states []uint16
	for _, n := range []string{"minecraft:air", "minecraft:stone", "minecraft:water", "minecraft:ice",
		"minecraft:honey_block", "minecraft:cobweb", "minecraft:ladder", "minecraft:oak_trapdoor",
		"minecraft:red_bed", "minecraft:oak_fence", "minecraft:bubble_column"} {
		s, ok := registry.FindState(n, nil)
		if !ok {
			b.Fatalf("no state %s", n)
		}
		states = append(states, s)
	}
	b.ReportAllocs()
	var sink float64
	for b.Loop() {
		for i, s := range states {
			below := states[(i+1)%len(states)]
			sink += BlockFriction(s) + BlockSpeedFactor(s) + BlockJumpFactor(s) +
				BlockBounceRestitution(s, true)
			if IsWater(s) || IsClimbable(s, below) || IsSuffocating(s) || isPostBlock(s) || IsStuckIn(s) {
				sink++
			}
			if _, _, _, ok := StuckMultiplier(s, true, false); ok {
				sink++
			}
		}
	}
	_ = sink
}
