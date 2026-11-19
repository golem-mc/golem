package entities

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

const spawnerCart = "minecraft:spawner_minecart"

// TestASpawnerMinecartSpawnsItsMobNearAPlayerAndBreaksIntoAMinecart: the cart
// ticks its own spawner, keeps its fields in the saved compound, and leaves a
// plain minecart when broken.
// vanilla: MinecartSpawner.tick, getDropItem
func TestASpawnerMinecartSpawnsItsMobNearAPlayerAndBreaksIntoAMinecart(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance = config.MinDistance
	cfg.SimulationDistance = config.MinDistance
	srv, c := servertest.OpenWindowed(t, cfg)
	c.MoveToGround(2.5, 64, 1.5, true)
	c.Settle()
	srv.StopTick()
	servertest.CartRails(t, srv, 3, 6)
	srv.RunLine("fill -2 63 -6 12 63 8 minecraft:grass_block")
	srv.RunLine(`summon ` + spawnerCart + ` 4.5 64.0625 1.5 {SpawnData:{entity:{id:"minecraft:pig"}},Delay:0}`)
	var id int32
	for _, cart := range srv.MinecartsInDimension(world.DimensionName) {
		id = cart.ID
	}
	if id == 0 {
		t.Fatal("the summon stood no cart")
	}
	tag := cartSavedTag(t, srv, id)
	data, _ := nbt.Get[nbt.Compound](tag, "SpawnData")
	entity, _ := nbt.Get[nbt.Compound](data, "entity")
	if got, _ := nbt.Get[nbt.String](entity, "id"); got != "minecraft:pig" {
		t.Fatalf("SpawnData entity id = %q, want minecraft:pig", got)
	}
	pigs := 0
	for range 20 {
		srv.TickMinecarts(1)
		for _, m := range srv.MobsInDimension(world.DimensionName) {
			if m.Kind == "minecraft:pig" {
				pigs++
			}
		}
		if pigs > 0 {
			break
		}
	}
	if pigs == 0 {
		t.Error("the spawner cart spawned no pig")
	}
	srv.HurtMinecart(id, 100, false)
	srv.TickMinecarts(1)
	if !servertest.BlastLeftItem(srv.ItemsInDimension(world.DimensionName), "minecraft:minecart") {
		t.Error("the broken spawner cart left no minecart item")
	}
}
