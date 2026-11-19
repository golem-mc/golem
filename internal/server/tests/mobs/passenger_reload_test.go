package mobs

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/google/uuid"
)

// vanilla: EntityType.loadEntityRecursive
func TestMobPassengersSurviveRegionReload(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.LevelName = t.TempDir()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	freeze := func(s *server.Server) { servertest.FreezeUntilHeld(t, s) }
	srv, c := servertest.Join(t, cfg, "Keeper", servertest.OpsListing(t, "Keeper"), freeze)
	servertest.HoldTicks(t, srv)
	c.ChatCommand(`summon pig 3.5 65 3.5 {NoAI:1b,PersistenceRequired:1b,Passengers:[{id:"minecraft:cow",NoAI:1b,PersistenceRequired:1b}]}`)
	servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
	c.Settle()
	pigs, cows := srv.MobIDsOfKind("minecraft:pig"), srv.MobIDsOfKind("minecraft:cow")
	if len(pigs) != 1 || len(cows) != 1 || !slices.Equal(srv.MobPassengerIDs(pigs[0]), cows) {
		t.Fatalf("initial mount = pigs %v, cows %v", pigs, cows)
	}
	srv.FlushMobs()
	c.Conn.Close()
	srv.Shutdown()
	restarted := *cfg
	back, again := servertest.Join(t, &restarted, "Keeper", servertest.OpsListing(t, "Keeper"), freeze)
	servertest.HoldTicks(t, back)
	servertest.RunTicks(t, back, 1)
	again.Settle()
	pigs, cows = back.MobIDsOfKind("minecraft:pig"), back.MobIDsOfKind("minecraft:cow")
	if len(pigs) != 1 || len(cows) != 1 {
		t.Fatalf("reloaded mobs = pigs %v, cows %v", pigs, cows)
	}
	if got := back.MobPassengerIDs(pigs[0]); !slices.Equal(got, cows) {
		t.Errorf("reloaded pig passengers = %v, want %v", got, cows)
	}
}

// vanilla: Entity.load, Entity.saveWithoutId
func TestCarriedPassengerTeamResavesAfterRegionReload(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.LevelName = t.TempDir()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	pos := world.ChunkPos{}
	pig, arrow, cow := uuid.New(), uuid.New(), uuid.New()
	servertest.PlantChunkEntities(t, cfg.LevelName, pos, nbt.Compound{
		"id": nbt.String("minecraft:pig"), "UUID": nbt.UUIDToIntArray(pig),
		"Pos":  nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(3.5), nbt.Double(65), nbt.Double(3.5)}},
		"NoAI": nbt.Byte(1), "PersistenceRequired": nbt.Byte(1),
		"Passengers": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{
			"id": nbt.String("minecraft:arrow"), "UUID": nbt.UUIDToIntArray(arrow), "Team": nbt.String("red"),
			"Passengers": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{
				"id": nbt.String("minecraft:cow"), "UUID": nbt.UUIDToIntArray(cow), "Team": nbt.String("red"),
			}}},
		}}},
	})
	srv, c := servertest.Join(t, cfg, "Keeper", servertest.OpsListing(t, "Keeper"), func(s *server.Server) {
		servertest.FreezeUntilHeld(t, s)
	})
	servertest.HoldTicks(t, srv)
	servertest.RunTicks(t, srv, 1)
	c.Settle()
	if got := srv.MobIDsOfKind("minecraft:pig"); len(got) != 1 {
		t.Fatalf("loaded pigs = %v, want one", got)
	}
	srv.FlushMobs()
	servertest.EntityFileIs(t, cfg.LevelName, pos, "minecraft:pig", func(saved nbt.Compound) {
		identity, _ := nbt.UUIDField(saved, "UUID")
		servertest.ExpectUUID(t, identity, pig, "saved pig")
		for _, rider := range []struct {
			kind string
			id   uuid.UUID
		}{{"minecraft:arrow", arrow}, {"minecraft:cow", cow}} {
			passengers, _ := nbt.Get[nbt.List](saved, "Passengers")
			if len(passengers.Items) != 1 {
				t.Fatalf("saved passengers = %v, want one %s", passengers, rider.kind)
			}
			var ok bool
			saved, ok = nbt.CompoundView(passengers.Items[0])
			if !ok {
				t.Fatalf("saved passenger = %T, want compound", passengers.Items[0])
			}
			if kind, _ := nbt.Get[nbt.String](saved, "id"); string(kind) != rider.kind {
				t.Fatalf("saved passenger kind = %q, want %s", kind, rider.kind)
			}
			identity, _ = nbt.UUIDField(saved, "UUID")
			servertest.ExpectUUID(t, identity, rider.id, "saved passenger")
			if _, has := saved["Team"]; has {
				t.Errorf("saved %s still has Team", rider.kind)
			}
		}
	})
}
