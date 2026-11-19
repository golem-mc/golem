package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
)

// spawnBundle reads the packet ids between the Bundle Delimiter that opens
// the first bundle and the one that closes it. A Spawn Entity outside a
// bundle fails the test.
func spawnBundle(c *servertest.Client) []int32 {
	c.T.Helper()
	c.WatchBundles = true
	var ids []int32
	open := false
	for id := range c.Stream() {
		switch {
		case id == protocol.IDPlayBundleDelimiter && !open:
			open = true
		case id == protocol.IDPlayBundleDelimiter:
			return ids
		case open:
			ids = append(ids, id)
		case id == protocol.IDPlaySpawnEntity:
			c.T.Fatal("a Spawn Entity arrived outside a Bundle Delimiter pair")
		}
	}
	c.T.Fatal("no complete bundle arrived")
	return nil
}

// TestEntitySpawnArrivesInOneBundle checks a body's spawn, state and
// equipment arrive between two Bundle Delimiters.
// vanilla: ServerEntity.addPairing
func TestEntitySpawnArrivesInOneBundle(t *testing.T) {
	t.Parallel()
	c := servertest.DataWorld(t, "Bundler")
	c.ChatCommand(`summon minecraft:armor_stand 2.5 64 2.5 {equipment:{head:{id:"minecraft:iron_helmet",count:1}}}`)
	ids := spawnBundle(c)
	want := []int32{protocol.IDPlaySpawnEntity, protocol.IDPlayEntityMetadata, protocol.IDPlayEntityEquipment}
	if !slices.Equal(ids, want) {
		t.Fatalf("bundled packets = %v, want %v", ids, want)
	}
	c.ChatCommand(`summon minecraft:pig 4.5 70 4.5`)
	ids = spawnBundle(c)
	if len(ids) < 2 || ids[0] != protocol.IDPlaySpawnEntity || ids[1] != protocol.IDPlayEntityMetadata {
		t.Fatalf("a mob's bundled packets = %v, want the spawn then its metadata first", ids)
	}
	c.Disconnect()
}

// TestPlayerSpawnArrivesInOneBundle checks a player's body reaches a viewer
// in one bundle on every path that shows it: the join showing the others to
// the newcomer, the respawn and the arrival from another dimension.
// vanilla: ServerEntity.addPairing
func TestPlayerSpawnArrivesInOneBundle(t *testing.T) {
	t.Parallel()
	want := []int32{protocol.IDPlaySpawnEntity, protocol.IDPlayEntityMetadata, protocol.IDPlayEntityEquipment}
	cfg := config.Default()
	cfg.OnlineMode = false
	_, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Actor"))

	actor := connect().Join("Actor")

	// Join: the newcomer is shown the player already there.
	joiner := connect()
	joiner.WatchBundles = true
	joiner.Login("Joiner")
	joiner.Configure()
	if ids := spawnBundle(joiner); !slices.Equal(ids, want) {
		t.Errorf("join bundle = %v, want %v", ids, want)
	}
	joiner.Disconnect()

	// Respawn: the others see the new body.
	watcher := connect().Join("Watcher")
	actor.ChatCommand("kill")
	actor.AwaitPacket(protocol.IDPlayCombatDeath)
	watcher.Settle()
	actor.Send(protocol.IDPlayClientCommandIn, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.ClientCommandPerformRespawn)
	})
	if ids := spawnBundle(watcher); !slices.Equal(ids, want) {
		t.Errorf("respawn bundle = %v, want %v", ids, want)
	}

	// Dimension change: the others in the destination see the arrival.
	actor.ChatCommand("execute in minecraft:the_nether run tp @s 8 40 8")
	watcher.Settle()
	actor.ChatCommand("execute in minecraft:overworld run tp @s 8 80 8")
	if ids := spawnBundle(watcher); !slices.Equal(ids, want) {
		t.Errorf("dimension arrival bundle = %v, want %v", ids, want)
	}
}

// TestVehicleAndMountSpawnBundles checks a boat and a mob carrying a
// passenger arrive in one bundle.
// vanilla: ServerEntity.addPairing
func TestVehicleAndMountSpawnBundles(t *testing.T) {
	t.Parallel()
	c := servertest.DataWorld(t, "Bundler")
	c.ChatCommand(`summon minecraft:oak_boat 2.5 64 2.5`)
	ids := spawnBundle(c)
	if len(ids) == 0 || ids[0] != protocol.IDPlaySpawnEntity {
		t.Errorf("boat bundle = %v, want the spawn first", ids)
	}
	c.ChatCommand(`summon minecraft:pig 4.5 64 4.5 {Passengers:[{id:"minecraft:chicken"}]}`)
	ids = spawnBundle(c)
	if len(ids) < 2 || ids[0] != protocol.IDPlaySpawnEntity || ids[1] != protocol.IDPlayEntityMetadata {
		t.Errorf("mount bundle = %v, want the spawn then its metadata first", ids)
	}
	c.Disconnect()
}
