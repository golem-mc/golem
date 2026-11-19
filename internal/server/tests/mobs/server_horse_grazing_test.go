package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
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

// TestAFedHorseOpensItsMouthAndALlamaOrCamelDoesNot: a hurt horse fed wheat
// carries the open-mouth flag for 30 ticks; a llama fed wheat or a camel fed
// cactus never does.
// vanilla: AbstractHorse.handleEating, eating, tick; Llama.handleEating, Camel.handleEating
func TestAFedHorseOpensItsMouthAndALlamaOrCamelDoesNot(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", "Groom", "minecraft:wheat", 2)
	srv.StopTick()
	spawn := func(kind string, x, z float64) int32 {
		id := srv.SpawnMob(kind, x, 64, z)
		srv.SetMobNoAI(id)
		srv.TameHorse(id)
		srv.SetMobHealth(id, 5)
		return id
	}
	horse := spawn("minecraft:horse", 1.5, 2.5)
	llama := spawn("minecraft:llama", -1.5, 2.5)
	camel := spawn("minecraft:camel", 0.5, -2.5)
	c.Settle()

	c.UseOn(horse)
	c.UseOn(llama)
	c.Settle()
	srv.GiveSlot("Groom", player.HotbarSlot,
		protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:cactus"), Count: 1})
	c.Settle()
	c.UseOn(camel)
	c.Settle()

	for _, id := range []int32{horse, llama, camel} {
		if h, _ := srv.MobHealth(id); h <= 5 {
			t.Fatalf("mob %d has %v health after feeding, want it healed", id, h)
		}
	}
	mouth := func(id int32) bool {
		t.Helper()
		m, ok := mobWithID(srv, id)
		if !ok {
			t.Fatalf("mob %d is not in the world any more", id)
		}
		return m.HorseFlags&protocol.HorseFlagMouthOpen != 0
	}
	if !mouth(horse) {
		t.Fatal("the fed horse does not open its mouth")
	}
	for range 30 {
		for _, id := range []int32{llama, camel} {
			if mouth(id) {
				t.Fatalf("fed mob %d opens its mouth", id)
			}
		}
		srv.TickMobs(1)
	}
	if mouth(horse) {
		t.Error("the horse's mouth is still open 30 ticks after feeding")
	}
}
