package worldgen

import (
	"reflect"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// TestMineshaftCartWakesWithAnimalsAndKeepsLoot generates seed 3's cart and
// surface herd in the same chunk, then reloads the pending loot before opening it.
func TestMineshaftCartWakesWithAnimalsAndKeepsLoot(t *testing.T) {
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelType = "normal"
	cfg.LevelSeed = "3"
	cfg.LevelName = t.TempDir()
	cfg.GameMode = "creative"
	cfg.ViewDistance = 2
	cfg.SimulationDistance = 2
	const x, y, z = -473.5, -12.5, 344.5
	const lootSeed = int64(1160385377290633670)
	pos := world.ChunkPos{X: -30, Z: 21}
	var cartUUID nbt.Tag
	for pass := range 2 {
		srv, c := servertest.Join(t, cfg, "Miner", func(s *server.Server) {
			s.SetSpawning(false)
			s.SetWorldSpawn(world.SpawnPos{X: -474, Y: -12, Z: 346})
			servertest.FreezeUntilHeld(t, s)
		})
		servertest.HoldTicks(t, srv)
		servertest.RunTicks(t, srv, 1)
		var id int32
		matches := 0
		for _, cart := range srv.MinecartsInDimension(world.DimensionName) {
			tag, _ := srv.MinecartSaveTag(cart.ID)
			p, _ := nbt.Get[nbt.List](tag, "Pos")
			if len(p.Items) != 3 || p.Items[0] != nbt.Double(x) || p.Items[2] != nbt.Double(z) {
				continue
			}
			matches++
			id = cart.ID
			if pass == 0 {
				cartUUID = tag["UUID"]
			}
			if !reflect.DeepEqual(tag["UUID"], cartUUID) {
				t.Fatal("cart UUID changed on reload")
			}
			if tag["LootTable"] != nbt.String("minecraft:chests/abandoned_mineshaft") || tag["LootTableSeed"] != nbt.Long(lootSeed) {
				t.Fatalf("pending loot: %v", tag)
			}
			// The first physics step settles the cart onto its rail.
			if cy := float64(p.Items[1].(nbt.Double)); cy < y-0.6 || cy > y {
				t.Fatalf("cart height %v", p)
			}
		}
		if matches != 1 {
			t.Fatalf("pass %d: %d carts at fixture, want 1", pass, matches)
		}
		pigs := 0
		for _, mob := range srv.Mobs() {
			if mob.Kind == "minecraft:pig" && world.ChunkOf(mob.X) == pos.X && world.ChunkOf(mob.Z) == pos.Z {
				pigs++
			}
		}
		if pigs != 4 {
			t.Fatalf("pass %d: %d surface pigs, want 4 beside the cart", pass, pigs)
		}
		typeID, _ := registry.EntityTypeID("minecraft:chest_minecart")
		spawn := c.AwaitSpawn(typeID)
		for spawn.EntityID != id {
			spawn = c.AwaitSpawn(typeID)
		}
		if spawn.X != x || spawn.Z != z {
			t.Fatalf("visible cart %+v, want fixture %d", spawn, id)
		}
		if pass == 1 {
			srv.RunLine("tp Miner -473.5 -12 346.5")
			servertest.ExpectTeleport(t, c)
			w := &servertest.WindowedClient{Client: c, States: make(map[int32]int32), WantBlockEntityType: -1}
			w.UseOn(id)
			slots := w.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 0, 0, 0)
			found := false
			for _, slot := range slots[:27] {
				found = found || !slot.Empty()
			}
			if !found {
				t.Fatal("opening generated cart produced no loot")
			}
			tag, _ := srv.MinecartSaveTag(id)
			if tag["LootTable"] != nil {
				t.Fatal("opening did not consume the pending loot table")
			}
		}
		srv.FlushMobs()
		srv.FlushSaves()
		srv.Shutdown()
		count := 0
		for _, tag := range servertest.ReadChunkEntities(t, cfg.LevelName, pos) {
			if reflect.DeepEqual(tag["UUID"], cartUUID) {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("pass %d: saved cart count %d, want 1", pass, count)
		}
	}
}
