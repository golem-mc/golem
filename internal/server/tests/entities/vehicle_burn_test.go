package entities

import (
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
