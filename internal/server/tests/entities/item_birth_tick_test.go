package entities

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// vanilla: EntityTickList.forEach, ItemEntity.tick
func TestVehicleLavaDropsFirstStepOnTheNextTick(t *testing.T) {
	t.Parallel()
	for _, kind := range []struct {
		name string
		typ  int32
	}{
		{"oak_boat", protocol.EntityTypeOakBoat},
		{"minecart", protocol.EntityTypeMinecart},
	} {
		t.Run(kind.name, func(t *testing.T) {
			t.Parallel()
			cfg := servertest.BlockTestConfig()
			cfg.ViewDistance = config.MinDistance
			cfg.SimulationDistance = config.MinDistance
			srv, c := servertest.Join(t, cfg, "Stoker", servertest.OpsListing(t, "Stoker"))
			servertest.HoldTicks(t, srv)
			srv.SetBlock(4, 64, 4, registry.MustState("minecraft:lava", nil))
			c.ChatCommand(fmt.Sprintf(`summon minecraft:%s 4.5 64.5 4.5 {NoGravity:1b}`, kind.name))
			c.AwaitSpawn(kind.typ)
			var items []server.TestItem
			for range 3 {
				servertest.RunTicks(t, srv, 1)
				items = srv.ItemsInDimension(world.DimensionName)
				if len(items) != 0 {
					break
				}
			}
			if len(items) != 1 {
				t.Fatalf("birth tick: %d items, want one vehicle drop", len(items))
			}
			it := items[0]
			if it.X != 4.5 || it.Y != 64.5+entity.ItemHalfSize || it.Z != 4.5 || it.VY != 0.2 || it.Age != 0 || it.PickupDelay != entity.BreakPickupDelay || it.Health != 5 || srv.ItemFire(it.ID) > 0 {
				t.Fatalf("birth tick: %+v, fire=%d; want unstepped, undamaged drop at vehicle's feet", it, srv.ItemFire(it.ID))
			}
			c.Settle()
			positionSent := false
			c.ReadTicks(srv, 1, func(packet int32, data []byte) bool {
				switch packet {
				case protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot:
					r := protocol.NewReader(data)
					if r.ReadVarInt() != it.ID {
						return false
					}
					r.ReadVarInt() // properties
					dx, dy, dz := r.ReadI16(), r.ReadI16(), r.ReadI16()
					if dx != 0 || dy != 0 || dz != 0 {
						t.Errorf("first movement delta = (%d, %d, %d), want the unstepped spawn position", dx, dy, dz)
					}
					positionSent = true
				case protocol.IDPlayTeleportEntity:
					r := protocol.NewReader(data)
					if r.ReadVarInt() != it.ID {
						return false
					}
					r.ReadVarInt() // linear position path
					x, y, z := r.ReadF64(), r.ReadF64(), r.ReadF64()
					if x != it.X || y != it.Y-entity.ItemHalfSize || z != it.Z {
						t.Errorf("first movement position = (%g, %g, %g), want the unstepped spawn position", x, y, z)
					}
					positionSent = true
				}
				return false
			})
			if !positionSent {
				t.Error("no initial movement baseline sent")
			}
			items = srv.ItemsInDimension(world.DimensionName)
			if len(items) != 1 || items[0].Age != 1 || items[0].Health != 1 || items[0].Y <= it.Y || srv.ItemFire(it.ID) <= 0 {
				t.Fatalf("first step: %+v, fire=%d; want moving, burning drop still alive", items, srv.ItemFire(it.ID))
			}
			servertest.RunTicks(t, srv, 1)
			if items = srv.ItemsInDimension(world.DimensionName); len(items) != 0 {
				t.Fatalf("second step: %+v, want lava to consume drop", items)
			}
		})
	}
}

// vanilla: Chicken.aiStep, EntityTickList.forEach
func TestMobTickDropsWaitWhileExistingItemsStep(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance = config.MinDistance
	cfg.SimulationDistance = config.MinDistance
	srv, _ := servertest.Join(t, cfg, "Farmer")
	servertest.HoldTicks(t, srv)
	chicken := srv.SpawnMob("minecraft:chicken", 4.5, 64, 4.5)
	srv.SetEggTimer(chicken, 1)
	stone := srv.SpawnItemEntityMotion(7.5, 66.5, 4.5, "minecraft:stone", 1, 0, 0, 0)
	servertest.RunTicks(t, srv, 1)
	items := srv.ItemsInDimension(world.DimensionName)
	if len(items) != 2 {
		t.Fatalf("birth tick: %+v, want the stone and newly laid egg", items)
	}
	for _, it := range items {
		if it.ID == stone {
			if it.Age != 1 || it.Y >= 66.5 {
				t.Errorf("existing item did not step: %+v", it)
			}
		} else if registry.Item(it.Stack.ItemID) != registry.ItemEgg || it.Age != 0 || it.VY != 0.2 || it.PickupDelay != entity.BreakPickupDelay {
			t.Errorf("new egg already stepped: %+v", it)
		}
	}
	servertest.RunTicks(t, srv, 1)
	items = srv.ItemsInDimension(world.DimensionName)
	if len(items) != 2 {
		t.Fatalf("next tick: %+v, want both items still alive", items)
	}
	for _, it := range items {
		wantAge := 1
		if it.ID == stone {
			wantAge = 2
		}
		if it.Age != wantAge {
			t.Errorf("next tick: item %d age=%d, want %d", it.ID, it.Age, wantAge)
		}
	}
}

// vanilla: ChunkMap.tick, ServerEntity.sendChanges
func TestFrozenNewbornItemGetsItsInitialPositionCorrection(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance = config.MinDistance
	cfg.SimulationDistance = config.MinDistance
	srv, c := servertest.Join(t, cfg, "Farmer", servertest.OpsListing(t, "Farmer"))
	servertest.HoldTicks(t, srv)
	srv.SetTickFrozen(true)
	c.ChatCommand(`summon chicken 4.5 64 4.5 {NoAI:1b,EggLayTime:1}`)
	c.AwaitSpawn(protocol.EntityTypeChicken)
	c.Settle()
	c.ChatCommand("tick step 1")
	c.ExpectTickingStep(1)
	servertest.RunTicks(t, srv, 1)
	spawn := servertest.ParseItemSpawn(c.ExpectWithinTicks(srv, 0, protocol.IDPlaySpawnEntity))
	corrected := false
	c.ReadTicks(srv, 1, func(packet int32, data []byte) bool {
		if packet != protocol.IDPlayEntityPosition {
			return false
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != spawn.EntityID {
			return false
		}
		r.ReadVarInt() // position path properties
		if dx, dy, dz := r.ReadI16(), r.ReadI16(), r.ReadI16(); dx != 0 || dy != 0 || dz != 0 {
			t.Errorf("initial correction = (%d, %d, %d), want unchanged birth position", dx, dy, dz)
		}
		corrected = true
		return false
	})
	if !corrected {
		t.Error("no initial position correction while frozen; client keeps its predicted birth step")
	}
	items := srv.ItemsInDimension(world.DimensionName)
	if len(items) != 1 || items[0].Age != 0 || items[0].VY != 0.2 || items[0].PickupDelay != entity.BreakPickupDelay || items[0].Health != 5 {
		t.Fatalf("frozen newborn = %+v, want unstepped egg", items)
	}
	c.ChatCommand("tick step 1")
	c.ExpectTickingStep(1)
	servertest.RunTicks(t, srv, 1)
	items = srv.ItemsInDimension(world.DimensionName)
	if len(items) != 1 || items[0].Age != 1 || items[0].Y <= spawn.Y+entity.ItemHalfSize || items[0].PickupDelay != entity.BreakPickupDelay-1 {
		t.Fatalf("following step = %+v, want egg's first movement", items)
	}
}

// vanilla: LightningBolt.tick, EntityTickList.forEach
func TestLightningSpilledItemsWaitUntilTheNextTick(t *testing.T) {
	t.Parallel()
	srv, c, x, y, z := boltBeside(t)
	srv.SpawnItemStack(x, y+entity.ItemHalfSize, z, protocol.ItemStack{
		ItemID: int32(registry.ItemShulkerBox), Count: 1,
		Container: []protocol.ItemStack{{ItemID: int32(registry.ItemStone), Count: 1}},
	})
	summonBolt(t, srv, c, x, y, z)
	servertest.RunTicks(t, srv, 1)
	items := srv.ItemsInDimension(world.DimensionName)
	if len(items) != 1 {
		t.Fatalf("strike tick: %+v, want the spilled stone", items)
	}
	if it := items[0]; registry.Item(it.Stack.ItemID) != registry.ItemStone || it.Age != 0 || it.VY != 0.2 || it.Health != 5 {
		t.Fatalf("spilled item already stepped: %+v", it)
	}
}
