package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

func BenchmarkBlockLootSequence(b *testing.B) {
	cfg := config.Default()
	cfg.LevelName, cfg.LevelType = b.TempDir(), TestFlatLevelType
	s, err := newServer(cfg, 12345)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(s.Shutdown)
	state, _ := registry.FindState("minecraft:gravel", nil)
	ctx := s.brokenBlockLootContext(s.world, 0, 64, 0, state, nil)
	for b.Loop() {
		rules.RollLoot("minecraft:blocks/gravel", &ctx, s.lootRng)
	}
}
