package entities

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: Entity.lavaHurt, VehicleEntity.hurtServer
func TestAMinecartInLavaBreaksAndDropsItsItem(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Burner", servertest.OpsListing(t, "Burner"))
	servertest.HoldTicks(t, srv)
	// The last rail crosses lava and leads into its pool; the cart straddles the edge.
	srv.SetBlock(3, 63, 4, registry.MustState("minecraft:lava", nil))
	srv.SetBlock(3, 64, 4, registry.MustState("minecraft:rail", map[string]string{"shape": "east_west"}))
	srv.SetBlock(4, 64, 4, registry.MustState("minecraft:lava", nil))
	c.ChatCommand(`summon minecraft:minecart 3.9 64.0625 4.5 {Motion:[0.4d,0d,0d],CustomName:"Bob"}`)
	cart := c.AwaitSpawn(protocol.EntityTypeMinecart).EntityID
	c.AwaitMetadataFor(cart)
	servertest.RunTicks(t, srv, 5)
	if _, _, _, _, ok := srv.MinecartState(cart); ok {
		t.Fatal("cart still standing after 5 ticks in lava")
	}
	stack := servertest.ExpectDroppedStack(t, c)
	if registry.Item(stack.ItemID) != registry.ItemMinecart || stack.Count != 1 || stack.CustomName == nil || stack.CustomName.Text != "Bob" {
		t.Fatalf("lava drop = %+v, want one named minecart", stack)
	}
}

// vanilla: Entity.lavaHurt, VehicleEntity.hurtServer
func TestABoatInLavaBreaksAndDropsItsItem(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Burner", servertest.OpsListing(t, "Burner"))
	servertest.HoldTicks(t, srv)
	srv.SetBlock(4, 64, 4, registry.MustState("minecraft:lava", nil))
	c.ChatCommand(`summon minecraft:oak_boat 4.5 64 4.5 {CustomName:"Bob"}`)
	boat := c.AwaitSpawn(protocol.EntityTypeOakBoat).EntityID
	c.AwaitMetadataFor(boat)
	servertest.RunTicks(t, srv, 5)
	if _, _, _, _, _, ok := srv.BoatState(boat); ok {
		t.Fatal("boat still afloat after 5 ticks in lava")
	}
	stack := servertest.ExpectDroppedStack(t, c)
	if registry.Item(stack.ItemID) != registry.ItemOakBoat || stack.Count != 1 || stack.CustomName == nil || stack.CustomName.Text != "Bob" {
		t.Fatalf("lava drop = %+v, want one named oak boat", stack)
	}
}

// vanilla: LivingEntity.dismountVehicle, Entity.lavaHurt
func TestABoatDestroyedByLavaKeepsItsRidersColumn(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Wrecked", servertest.OpsListing(t, "Wrecked"))
	servertest.HoldTicks(t, srv)
	c.ChatCommand(`summon oak_boat 4.5 64.5 4.5 {Invulnerable:1b,NoGravity:1b}`)
	boat := c.AwaitSpawn(protocol.EntityTypeOakBoat).EntityID
	c.ChatCommand(`ride Wrecked mount @e[type=oak_boat,limit=1]`)
	servertest.AwaitPassengersFor(c, boat)
	c.Settle()
	srv.SetBlock(4, 64, 4, registry.MustState("minecraft:lava", nil))
	c.ChatCommand(`data merge entity @e[type=oak_boat,limit=1] {Invulnerable:0b}`)
	c.Settle()
	servertest.RunTicks(t, srv, 1)
	if _, _, _, _, _, ok := srv.BoatState(boat); ok {
		t.Fatal("boat survived the damaging lava tick")
	}
	if riders := servertest.AwaitPassengersFor(c, boat); len(riders) != 0 {
		t.Fatalf("destroyed boat passengers = %v, want none", riders)
	}
	if !c.ReadTicksSkipping(srv, 1, servertest.BoatSkip, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySyncPosition {
			return false
		}
		pos := servertest.ReadSyncPosition(t, data)
		if pos.X != 4.5 || pos.Y != 64.5 || pos.Z != 4.5 {
			t.Errorf("lava dismount = (%v %v %v), want (4.5 64.5 4.5)", pos.X, pos.Y, pos.Z)
		}
		return true
	}) {
		t.Fatal("destroyed boat sent no dismount position")
	}
	stack := servertest.ExpectDroppedStack(t, c)
	if registry.Item(stack.ItemID) != registry.ItemOakBoat || stack.Count != 1 {
		t.Fatalf("lava drop = %+v, want one oak boat", stack)
	}
}

// vanilla: BaseFireBlock.entityInside, VehicleEntity.hurtServer
func TestAMinecartInFireBreaksWithinTenTicks(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Burner", servertest.OpsListing(t, "Burner"))
	servertest.HoldTicks(t, srv)
	srv.SetBlock(4, 64, 4, registry.MustState("minecraft:fire", nil))
	c.ChatCommand(`summon minecraft:minecart 4.5 64.0625 4.5`)
	cart := c.AwaitSpawn(protocol.EntityTypeMinecart).EntityID
	servertest.RunTicks(t, srv, 10)
	if _, _, _, _, ok := srv.MinecartState(cart); ok {
		t.Fatal("cart still standing after 10 ticks in fire")
	}
}

// TestABoatInFireTellsWatchersItIsOnFire: the on-fire bit of the shared
// flags is sent once the hull's burn has started counting down.
// vanilla: Entity.baseTick, BaseFireBlock.fireIgnite
func TestABoatInFireTellsWatchersItIsOnFire(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig(), servertest.OpsListing(t, "Burner"))
	c := connect().Join("Burner")
	watcher := connect().Join("Watcher")
	servertest.HoldTicks(t, srv)
	srv.SetBlock(4, 64, 4, registry.MustState("minecraft:fire", nil))
	// Keep the hull alive until the watcher receives its on-fire flag.
	c.ChatCommand(`summon oak_boat 4.5 64.5 4.5 {Invulnerable:1b,NoGravity:1b}`)
	boat := c.AwaitSpawn(protocol.EntityTypeOakBoat).EntityID
	flags, ok := watcher.MetaEntryWithinTicks(srv, 3, boat, protocol.MetadataIndexEntityFlags)
	if !ok || flags.ByteVal&protocol.EntityFlagOnFire == 0 {
		t.Fatalf("flags=%+v, received=%v, want on-fire", flags, ok)
	}
}

// TestRainClearsABoatsOnFireFlagWhileItStillTouchesLava keeps the hull in
// lava across the weather change, so rain must win over ignition.
// vanilla: Entity.applyEffectsFromBlocks
func TestRainClearsABoatsOnFireFlagWhileItStillTouchesLava(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Stoker", servertest.OpsListing(t, "Stoker"))
	servertest.HoldTicks(t, srv)
	if !srv.SetBlock(4, 64, 4, registry.MustState("minecraft:lava", nil)) {
		t.Fatal("SetBlock failed")
	}
	// Keep the hull alive in lava, with its top above the fluid's rain-blocking cell.
	c.ChatCommand(`summon minecraft:oak_boat 4.5 64.5 4.5 {Invulnerable:1b,NoGravity:1b}`)
	boat := c.AwaitSpawn(protocol.EntityTypeOakBoat).EntityID
	c.AwaitMetadataFor(boat)
	flags, ok := c.MetaEntryWithinTicks(srv, 3, boat, protocol.MetadataIndexEntityFlags)
	if !ok || flags.ByteVal&protocol.EntityFlagOnFire == 0 {
		t.Fatalf("in dry lava: flags = %+v (received %v), want on-fire", flags, ok)
	}
	srv.SetWeatherState(true, false, 100000, 100000, 0)
	for range 400 {
		srv.TickWeather()
	}
	flags, ok = c.MetaEntryWithinTicks(srv, 3, boat, protocol.MetadataIndexEntityFlags)
	if !ok || flags.ByteVal&protocol.EntityFlagOnFire != 0 {
		t.Fatalf("in rainy lava: flags = %+v (received %v), want on-fire cleared", flags, ok)
	}
	if x, y, z, _, _, alive := srv.BoatState(boat); !alive || x != 4.5 || y != 64.5 || z != 4.5 {
		t.Fatalf("boat moved out of lava: (%v, %v, %v), alive %v", x, y, z, alive)
	}
}

// vanilla: Entity.baseTick
func TestABoatLeavingLavaTakesItsPeriodicBurnImmediately(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Stoker", servertest.OpsListing(t, "Stoker"))
	servertest.HoldTicks(t, srv)
	srv.SetBlock(4, 64, 4, registry.MustState("minecraft:lava", nil))
	c.ChatCommand(`summon minecraft:oak_boat 4.5 64.5 4.5 {Invulnerable:1b,NoGravity:1b}`)
	boat := c.AwaitSpawn(protocol.EntityTypeOakBoat).EntityID
	c.AwaitMetadataFor(boat)
	servertest.RunTicks(t, srv, 1)
	// Lava left a 300-tick burn; its first tick in air must already hurt.
	srv.SetBlock(4, 64, 4, 0)
	srv.RunLine(`data merge entity @e[type=minecraft:oak_boat,limit=1] {Invulnerable:0b}`)
	srv.RunLine(`damage @e[type=minecraft:oak_boat,limit=1] 3.2`)
	servertest.RunTicks(t, srv, 1)
	if _, _, _, _, _, ok := srv.BoatState(boat); ok {
		t.Fatal("boat survived the periodic burn on its first tick out of lava")
	}
	stack := servertest.ExpectDroppedStack(t, c)
	if registry.Item(stack.ItemID) != registry.ItemOakBoat || stack.Count != 1 {
		t.Fatalf("burn drop = %+v, want one oak boat", stack)
	}
}

// vanilla: Entity.lavaHurt, Entity.playEntityOnFireExtinguishedSound
func TestVehicleLavaSoundRequiresSuccessfulHurt(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"minecart", "oak_boat"} {
		for _, invulnerable := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/invulnerable=%v", kind, invulnerable), func(t *testing.T) {
				srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Listener", servertest.OpsListing(t, "Listener"))
				servertest.HoldTicks(t, srv)
				srv.SetBlock(4, 64, 4, registry.MustState("minecraft:lava", nil))
				inv := 0
				if invulnerable {
					inv = 1
				}
				c.ChatCommand(fmt.Sprintf(`summon minecraft:%s 4.5 64.5 4.5 {Invulnerable:%db,NoGravity:1b}`, kind, inv))
				entityType := int32(protocol.EntityTypeMinecart)
				if kind == "oak_boat" {
					entityType = protocol.EntityTypeOakBoat
				}
				c.AwaitSpawn(entityType)
				c.Settle()
				heard, _, _ := servertest.CollectSoundsTicks(c, srv, 1)
				burns := 0
				for _, sound := range heard {
					if sound.Name != "minecraft:entity.generic.burn" {
						continue
					}
					burns++
					if sound.Category != protocol.SoundCategoryNeutral || sound.Volume != 0.4 || sound.Pitch < 2 || sound.Pitch > 2.4 {
						t.Errorf("lava sound = %+v", sound)
					}
				}
				if (burns > 0) == invulnerable {
					t.Errorf("lava sounds = %d, invulnerable %v", burns, invulnerable)
				}
				if kind != "oak_boat" || !invulnerable {
					return
				}
				srv.SetBlock(4, 64, 4, registry.MustState("minecraft:water", nil))
				heard, _, _ = servertest.CollectSoundsTicks(c, srv, 1)
				if got := servertest.CountSounds(heard, "minecraft:entity.generic.extinguish_fire", protocol.SoundCategoryNeutral); got != 1 {
					t.Errorf("extinguish sounds = %d, want 1: %+v", got, heard)
				}
				heard, _, _ = servertest.CollectSoundsTicks(c, srv, 1)
				if got := servertest.CountSounds(heard, "minecraft:entity.generic.extinguish_fire", protocol.SoundCategoryNeutral); got != 0 {
					t.Errorf("extinguish repeated %d times", got)
				}
			})
		}
	}
}
