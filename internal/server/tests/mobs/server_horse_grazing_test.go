package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestHorsesGrazeOnGrassAndLlamasDoNot: a horse on grass lowers its head on
// 1 tick in 300 for about 50 ticks; a llama never does.
// vanilla: AbstractHorse.aiStep, Llama.canEatGrass
func TestHorsesGrazeOnGrassAndLlamasDoNot(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	srv.StopTick()
	connect().Join("Rancher")
	grass := registry.BlockGrassBlock.DefaultState()
	kindOf := map[int32]string{}
	for i := range int32(10) {
		for row, kind := range []string{"minecraft:horse", "minecraft:llama"} {
			x, z := 2*i, int32(4*row)
			srv.SetBlock(x, 63, z, grass)
			id := srv.SpawnMob(kind, float64(x)+0.5, 64, float64(z)+0.5)
			// NoAI keeps each on its block; vanilla's aiStep grazes without AI.
			srv.SetMobNoAI(id)
			kindOf[id] = kind
		}
	}
	grazed := map[int32]bool{}
	ended := false
	for range 600 {
		srv.TickMobs(1)
		for _, m := range srv.Mobs() {
			kind, ok := kindOf[m.ID]
			if !ok {
				continue
			}
			eating := m.HorseFlags&protocol.HorseFlagEating != 0
			if eating && kind == "minecraft:llama" {
				t.Fatalf("llama %d grazes", m.ID)
			}
			if !eating && grazed[m.ID] {
				ended = true
			}
			if eating {
				grazed[m.ID] = true
			}
		}
	}
	if len(grazed) == 0 {
		t.Fatal("no horse of ten grazed in 600 ticks")
	}
	if !ended {
		t.Error("no graze ended within 600 ticks")
	}
}
