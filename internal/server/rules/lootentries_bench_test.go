package rules

import (
	"math/rand"
	"testing"
)

func BenchmarkLootEntries(b *testing.B) {
	ctx := &LootContext{}
	rng := rand.New(rand.NewSource(1))
	for b.Loop() {
		_ = RollLoot("minecraft:chests/simple_dungeon", ctx, rng)
	}
}
