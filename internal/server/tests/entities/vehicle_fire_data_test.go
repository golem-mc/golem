package entities

import (
	"fmt"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// vanilla: Entity.saveWithoutId, Entity.load, AbstractMinecart.tick
func TestVehicleFireDataMatchesIgnition(t *testing.T) {
	for _, kind := range []string{"oak_boat", "minecart"} {
		for _, source := range []string{"lava", "fire"} {
			t.Run(kind+"/"+source, func(t *testing.T) {
				t.Parallel()
				srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Stoker", servertest.OpsListing(t, "Stoker"))
				servertest.HoldTicks(t, srv)
				srv.SetBlock(4, 64, 4, registry.MustState("minecraft:"+source, nil))
				target := "@e[type=minecraft:" + kind + ",limit=1]"
				c.ChatCommand("summon minecraft:" + kind + " 4.5 64.5 4.5 {Invulnerable:1b,NoGravity:1b}")
				typ, _ := registry.EntityTypeID("minecraft:" + kind)
				c.AwaitSpawn(typ)
				c.Settle()
				servertest.RunTicks(t, srv, 1)
				want := "300s"
				if source == "fire" {
					want = "160s"
				}
				if got := awaitEntityDataCompound(t, srv, c, target, "Fire"); got != want {
					t.Fatalf("ignited Fire = %s, want %s", got, want)
				}
				// An unrelated /data write must carry the live burn through Entity.load.
				c.ChatCommand("data merge entity " + target + " {Tags:[stoked]}")
				c.Settle()
				if got := awaitEntityDataCompound(t, srv, c, target, "Fire"); got != want {
					t.Fatalf("Fire after unrelated write = %s, want %s", got, want)
				}
				srv.SetBlock(4, 64, 4, 0)
				c.ChatCommand("data merge entity " + target + " {Fire:40s}")
				c.Settle()
				if got := awaitEntityDataCompound(t, srv, c, target, "Fire"); got != "40s" {
					t.Fatalf("written Fire = %s, want 40s", got)
				}
				servertest.RunTicks(t, srv, 1)
				want = "40s"
				if kind == "oak_boat" {
					want = "39s"
				}
				if got := awaitEntityDataCompound(t, srv, c, target, "Fire"); got != want {
					t.Errorf("Fire after tick in air = %s, want %s", got, want)
				}
				c.ChatCommand("data merge entity " + target + " {Fire:0s}")
				c.Settle()
				servertest.RunTicks(t, srv, 1)
				if got := awaitEntityDataCompound(t, srv, c, target, "Fire"); got != "0s" {
					t.Errorf("cleared Fire = %s, want 0s", got)
				}
				c.ChatCommand("data merge entity " + target + " {Fire:-12.5f}")
				c.Settle()
				if got := awaitEntityDataCompound(t, srv, c, target, "Fire"); got != "-13s" {
					t.Errorf("signed lenient Fire read = %s, want -13s", got)
				}
				c.ChatCommand("data remove entity " + target + " Fire")
				c.Settle()
				if got := awaitEntityDataCompound(t, srv, c, target, "Fire"); got != "0s" {
					t.Errorf("absent Fire default = %s, want 0s", got)
				}
			})
		}
	}
}

// vanilla: BaseFireBlock.fireIgnite, AbstractBoat.tick, OldMinecartBehavior.tick
func TestVehicleNegativeFireContact(t *testing.T) {
	for _, kind := range []string{"oak_boat", "minecart"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Stoker", servertest.OpsListing(t, "Stoker"))
			servertest.HoldTicks(t, srv)
			srv.SetBlock(4, 70, 4, registry.BlockFire.DefaultState())
			target := "@e[type=minecraft:" + kind + ",limit=1]"
			c.ChatCommand("summon minecraft:" + kind + " 4.5 70.5 4.5 {Invulnerable:1b,NoGravity:1b}")
			typ, _ := registry.EntityTypeID("minecraft:" + kind)
			c.AwaitSpawn(typ)
			c.ChatCommand("data merge entity " + target + " {Fire:-12s}")
			c.Settle()
			for _, step := range []struct {
				ticks int
				want  string
			}{{1, "-10s"}, {4, "-2s"}, {1, "160s"}} {
				servertest.RunTicks(t, srv, step.ticks)
				if got := awaitEntityDataCompound(t, srv, c, target, "Fire"); got != step.want {
					t.Fatalf("Fire after %d ticks = %s, want %s", step.ticks, got, step.want)
				}
			}
		})
	}
}

// vanilla: OldMinecartBehavior.tick, Entity.applyEffectsFromBlocks, AbstractMinecart.tick
func TestMinecartLavaIgnitesAfterRain(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Stoker", servertest.OpsListing(t, "Stoker"))
	servertest.HoldTicks(t, srv)
	srv.SetBlock(4, 150, 4, registry.BlockLava.DefaultState())
	c.ChatCommand("summon minecraft:minecart 4.5 150.5 4.5 {Invulnerable:1b,NoGravity:1b}")
	c.AwaitSpawn(protocol.EntityTypeMinecart)
	c.Settle()
	servertest.RunTicks(t, srv, 1)
	target := "@e[type=minecraft:minecart,limit=1]"
	if got := awaitEntityDataCompound(t, srv, c, target, "Fire"); got != "300s" {
		t.Fatalf("dry lava Fire = %s, want 300s", got)
	}
	srv.SetWeatherState(true, false, 100000, 100000, 0)
	for range 400 {
		srv.TickWeather()
	}
	servertest.RunTicks(t, srv, 1)
	if got := awaitEntityDataCompound(t, srv, c, target, "Fire"); got != "300s" {
		t.Errorf("rainy lava Fire = %s, want 300s", got)
	}
	// Without lava's final ignition, the same rain must still clear the cart.
	srv.SetBlock(4, 150, 4, 0)
	servertest.RunTicks(t, srv, 1)
	if got := awaitEntityDataCompound(t, srv, c, target, "Fire"); got != "0s" {
		t.Errorf("rain without lava Fire = %s, want 0s", got)
	}
}

// vanilla: Entity.saveWithoutId, Entity.load, Entity.baseTick, AbstractMinecart.tick
func TestReloadedVehicleKeepsItsBurn(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	srv, c := servertest.Join(t, cfg, "Stoker", servertest.OpsListing(t, "Stoker"))
	servertest.HoldTicks(t, srv)
	for i, kind := range []string{"oak_boat", "minecart"} {
		x := int32(4 + i*3)
		srv.SetBlock(x, 70, 4, registry.BlockLava.DefaultState())
		c.ChatCommand(fmt.Sprintf("summon minecraft:%s %d.5 70.5 4.5 {Invulnerable:1b,NoGravity:1b}", kind, x))
		typ, _ := registry.EntityTypeID("minecraft:" + kind)
		c.AwaitSpawn(typ)
	}
	c.Settle()
	servertest.RunTicks(t, srv, 1)
	// Persist the lava's 300 ticks, then leave it before the first restored tick.
	srv.SetBlock(4, 70, 4, 0)
	srv.SetBlock(7, 70, 4, 0)
	srv.FlushMobs()
	for _, kind := range []string{"oak_boat", "minecart"} {
		servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{}, "minecraft:"+kind, func(tag nbt.Compound) {
			if tag["Fire"] != nbt.Short(300) {
				t.Errorf("saved %s Fire = %v, want 300s", kind, tag["Fire"])
			}
		})
	}
	c.Disconnect()
	srv.Shutdown()
	restarted := *cfg
	againSrv, connect := servertest.Start(t, &restarted, servertest.OpsListing(t, "Stoker"))
	servertest.FreezeUntilHeld(t, againSrv)
	again := connect().Join("Stoker")
	servertest.HoldTicks(t, againSrv)
	servertest.UntilTicks(t, againSrv, 20, "saved vehicles wake", func() bool {
		return len(againSrv.SelectedEntities("minecraft:oak_boat")) == 1 && len(againSrv.SelectedEntities("minecraft:minecart")) == 1
	})
	for _, kind := range []string{"oak_boat", "minecart"} {
		target := "@e[type=minecraft:" + kind + ",limit=1]"
		if got := awaitEntityDataCompound(t, againSrv, again, target, "Fire"); got != "300s" {
			t.Fatalf("reloaded %s Fire = %s, want 300s", kind, got)
		}
		again.ChatCommand("data merge entity " + target + " {Invulnerable:0b}")
		again.ChatCommand("damage " + target + " 3.2")
	}
	again.Settle()
	servertest.RunTicks(t, againSrv, 1)
	if got := len(againSrv.SelectedEntities("minecraft:oak_boat")); got != 0 {
		t.Error("reloaded boat survived its due periodic burn")
	}
	if got := len(againSrv.SelectedEntities("minecraft:minecart")); got != 1 {
		t.Fatal("reloaded cart took periodic fire damage")
	}
	if got := awaitEntityDataCompound(t, againSrv, again, "@e[type=minecraft:minecart,limit=1]", "Fire"); got != "300s" {
		t.Errorf("cart counted down its fire: %s", got)
	}
}

// vanilla: AbstractMinecart.tick
func TestMinecartFireDoesNotPublishOverlay(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Stoker", servertest.OpsListing(t, "Stoker"))
	servertest.HoldTicks(t, srv)
	r := servertest.NewMetadataRecorder(t, srv, c)
	packets := r.Run(`summon minecart 4.5 70 4.5 {Fire:300s,NoGravity:1b}`)
	id := servertest.SpawnedID(packets, protocol.EntityTypeMinecart)
	if id == 0 {
		t.Fatal("no cart spawned")
	}
	r.Record("burn", id, packets)
	r.Record("burn", id, r.Read(2))
	r.Record("burn", id, r.Run(`data merge entity @e[type=minecart,limit=1] {Fire:160s}`))
	r.Record("burn", id, r.Read(2))
	for _, payload := range r.Got["burn"] {
		if slices.Contains(metadataIndices(t, payload), protocol.MetadataIndexEntityFlags) {
			t.Errorf("cart published shared flags for its ignition: %s", payload)
		}
	}
}
