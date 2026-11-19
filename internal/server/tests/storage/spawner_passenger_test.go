package storage

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/google/uuid"
)

// vanilla: BaseSpawner.serverTick, EntityType.loadPassengersRecursive, Entity.saveWithoutId
func TestSpawnerPassengerUUIDResaves(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, _ := servertest.Join(t, cfg, "Keeper", func(s *server.Server) {
		servertest.FreezeUntilHeld(t, s)
	})
	servertest.HoldTicks(t, srv)
	srv.RunLine(`setblock 4 64 2 spawner`)
	srv.RunLine(`data merge block 4 64 2 {Delay:0s,SpawnCount:1s,SpawnData:{entity:{id:"minecraft:pig",Pos:[4.5d,65d,4.5d],NoAI:1b,Passengers:[{id:"minecraft:arrow"}]},custom_spawn_rules:{block_light_limit:[0,15],sky_light_limit:[0,15]}}}`)
	srv.TickBlockEntities()
	if pigs := srv.MobIDsOfKind("minecraft:pig"); len(pigs) != 1 {
		t.Fatalf("spawned pigs = %v, want one", pigs)
	}
	srv.FlushMobs()
	servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{}, "minecraft:pig", func(saved nbt.Compound) {
		passengers, _ := nbt.Get[nbt.List](saved, "Passengers")
		if len(passengers.Items) != 1 {
			t.Fatalf("saved passengers = %v, want one", passengers)
		}
		passenger, ok := nbt.CompoundView(passengers.Items[0])
		if !ok || passenger["id"] != nbt.String("minecraft:arrow") {
			t.Fatalf("saved passenger = %v, want an arrow", passengers.Items[0])
		}
		if id, ok := nbt.UUIDField(passenger, "UUID"); !ok || id == uuid.Nil {
			t.Fatalf("saved passenger UUID = %v (valid %v), want a nonzero UUID", passenger["UUID"], ok)
		}
	})
}
