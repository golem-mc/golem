package mobs

import (
	"fmt"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

const allay = "minecraft:allay"

func allayType() int32 { id, _ := registry.EntityTypeID(allay); return int32(id) }

// allayWorld joins a survival player at (0.5, 64, 0.5) holding count of item.
func allayWorld(t *testing.T, item string, count int32) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := servertest.BreedingPlayer(t, "survival", "Keeper", item, count)
	servertest.HoldTicks(t, srv)
	return srv, c
}

func TestAnAllayBringsAMatchingItemToItsPlayer(t *testing.T) {
	t.Parallel()
	srv, c := allayWorld(t, "minecraft:diamond", 1)
	id := srv.SpawnMob(allay, 2.5, 65, 0.5)
	c.ExpectMobSpawn(allayType())
	c.UseOn(id)
	c.Settle()
	if got := srv.InventorySlot("Keeper", player.HotbarSlot).Count; got != 0 {
		t.Fatalf("diamonds left = %d, want 0", got)
	}
	if held := srv.MobEquipment(id, "mainhand"); held.ItemID != servertest.MustItemID(t, "minecraft:diamond") {
		t.Fatalf("allay holds %v, want a diamond", held)
	}
	srv.SpawnItemEntity(5.5, 64, 0.5, "minecraft:diamond", 1)
	near := false
	servertest.UntilTicks(t, srv, 400, "a diamond thrown near the player", func() bool {
		for _, it := range srv.ItemsInDimension("minecraft:overworld") {
			// The first diamond lies 5 blocks off; the delivered one lands by the player.
			if math.Abs(it.X-0.5) < 3 && math.Abs(it.Z-0.5) < 3 {
				near = true
			}
		}
		return near
	})
}

func TestAnEmptyHandTakesTheHeldItemBack(t *testing.T) {
	t.Parallel()
	srv, c := allayWorld(t, "minecraft:diamond", 1)
	id := srv.SpawnMob(allay, 2.5, 65, 0.5)
	c.ExpectMobSpawn(allayType())
	c.UseOn(id)
	c.Settle()
	c.UseOn(id)
	c.Settle()
	if got := srv.InventorySlot("Keeper", player.HotbarSlot); got.Count != 1 || got.ItemID != servertest.MustItemID(t, "minecraft:diamond") {
		t.Fatalf("hotbar = %v, want the diamond back", got)
	}
	if held := srv.MobEquipment(id, "mainhand"); !held.Empty() {
		t.Fatalf("allay still holds %v", held)
	}
}

func TestAnAllayArrivingAtAPlayingJukeboxDances(t *testing.T) {
	t.Parallel()
	srv, c := allayWorld(t, "minecraft:music_disc_cat", 1)
	srv.SetBlock(3, 64, 0, registry.BlockStates["minecraft:jukebox"])
	c.UseBlock(3, 64, 0)
	c.Settle()
	id := srv.SpawnMob(allay, 2.5, 65, 0.5)
	c.ExpectMobSpawn(allayType())
	// The song's own events have passed; the 20-tick repeat reaches the allay.
	servertest.TickBlockEntitiesByHand(srv, 21)
	srv.TickMobs(2)
	if m := servertest.MetaOf(c.AwaitMetadataFor(id), protocol.MetadataIndexAllayDancing); m == nil || !m.FlagVal {
		t.Fatalf("dancing metadata = %v, want true", m)
	}
}

// The note block sits within the player's reach; the player then walks off so
// the note's top and the player's eyes are far apart.
const (
	allayNoteX, allayNoteZ = 3, 2
	awayX, awayZ           = -5.5, 0.5
)

// noteAllay gives an allay a diamond, plays a note block and moves the player away.
func noteAllay(t *testing.T, cover registry.Block) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := allayWorld(t, "minecraft:diamond", 1)
	srv.SetBlock(allayNoteX, 64, allayNoteZ, registry.BlockStates["minecraft:note_block"])
	if cover != registry.BlockAir {
		srv.SetBlockAndTell(allayNoteX, 65, allayNoteZ, cover.DefaultState())
	}
	id := srv.SpawnMob(allay, 2.5, 65, 0.5)
	c.ExpectMobSpawn(allayType())
	c.UseOn(id)
	c.Settle()
	c.UseBlock(allayNoteX, 64, allayNoteZ)
	c.Settle()
	srv.TickPhysics(8)
	c.MoveToGround(awayX, 64, awayZ, true)
	c.Settle()
	return srv, c
}

// diamondLandsNear lays a diamond and waits for one to rest
// within 4 blocks of (x, z), the other being the player's or the note's.
func diamondLandsNear(t *testing.T, srv *server.Server, x, z float64) {
	t.Helper()
	srv.SpawnItemEntity(0.5, 64, -4.5, "minecraft:diamond", 1)
	servertest.UntilTicks(t, srv, 400, "a diamond delivered", func() bool {
		for _, it := range srv.ItemsInDimension("minecraft:overworld") {
			if math.Abs(it.X-x) < 4 && math.Abs(it.Z-z) < 4 {
				return true
			}
		}
		return false
	})
}

func TestAnAllayDeliversToAPlayedNoteBlock(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		cover registry.Block
	}{
		{"sounding", registry.BlockAir},
		{"silent_head", registry.BlockPlayerHead},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := noteAllay(t, tc.cover)
			diamondLandsNear(t, srv, allayNoteX+0.5, allayNoteZ+0.5)
		})
	}
}

func TestASavedAllayStillDeliversToItsPlayer(t *testing.T) {
	t.Parallel()
	srv, c := allayWorld(t, "", 0)
	w := servertest.OfflineUUIDWords("Keeper")
	srv.RunLine(fmt.Sprintf("summon minecraft:allay 2.5 65 0.5 {equipment:{mainhand:{id:\"minecraft:diamond\",count:1}},Brain:{memories:{\"minecraft:liked_player\":{value:[I;%d,%d,%d,%d]}}}}",
		int32(w[0]), int32(w[1]), int32(w[2]), int32(w[3])))
	c.ExpectMobSpawn(allayType())
	srv.SpawnItemEntity(5.5, 64, 0.5, "minecraft:diamond", 1)
	near := false
	servertest.UntilTicks(t, srv, 400, "a diamond thrown near the player", func() bool {
		for _, it := range srv.ItemsInDimension("minecraft:overworld") {
			if math.Abs(it.X-0.5) < 3 && math.Abs(it.Z-0.5) < 3 {
				near = true
			}
		}
		return near
	})
}
