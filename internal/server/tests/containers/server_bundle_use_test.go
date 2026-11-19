package containers

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// bundleHolder joins a survivor holding a bundle of 3 stacks, newest first,
// in the first hotbar slot, with the main loop held. The last result is a
// second player standing nearby.
func bundleHolder(t *testing.T) (*server.Server, *servertest.Client, *servertest.Client) {
	t.Helper()
	srv, connect := servertest.Start(t, servertest.SurvivalTestConfig())
	c := connect().Join("Holder")
	bundle := protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:bundle"), Count: 1}
	bundle.SetBundleContents([]protocol.ItemStack{
		{ItemID: servertest.ItemIDOrDie(t, "minecraft:stick"), Count: 5},
		{ItemID: servertest.ItemIDOrDie(t, "minecraft:apple"), Count: 2},
		{ItemID: servertest.ItemIDOrDie(t, "minecraft:cobblestone"), Count: 7},
	})
	srv.GiveSlot("Holder", player.HotbarSlot, bundle)
	watcher := connect().Join("Watcher")
	watcher.Settle()
	servertest.HoldTicks(t, srv)
	return srv, c, watcher
}

// soundHeard is one Sound Effect packet by registry reference: the sound, the
// category, where it played in eighths of a block, and how loud.
type soundHeard struct {
	sound         int32
	category      int32
	x, y, z       int32
	volume, pitch float32
}

func (s *soundHeard) read(t *testing.T, data []byte) {
	t.Helper()
	r := protocol.NewReader(data)
	s.sound, s.category = r.ReadVarInt(), r.ReadVarInt()
	s.x, s.y, s.z = r.ReadI32(), r.ReadI32(), r.ReadI32()
	s.volume, s.pitch = r.ReadF32(), r.ReadF32()
	if err := r.Err(); err != nil {
		t.Fatalf("sound effect: %v", err)
	}
}

// soundRegistryWireID is the registry id plus one that the Sound Effect
// packet names a sound by.
func soundRegistryWireID(t *testing.T, name string) int32 {
	t.Helper()
	id, ok := registry.SoundEventID(name)
	if !ok {
		t.Fatalf("no sound event for %s", name)
	}
	return id + 1
}

// startBundleUse sends Use Item and waits for the server to act on it.
func startBundleUse(t *testing.T, srv *server.Server, c *servertest.Client) {
	t.Helper()
	c.UseItemRightClicks(protocol.HandMain)
	c.Settle()
	if srv.BowTicks("Holder") != 1 {
		t.Fatalf("the bundle use did not start")
	}
}

// bundleSounds counts the bundle remove_one and drop_contents sounds the
// client has been sent and checks their category, volume and pitch.
func bundleSounds(t *testing.T, srv *server.Server, c *servertest.Client) (removes, drops []soundHeard) {
	t.Helper()
	removeID := soundRegistryWireID(t, "minecraft:item.bundle.remove_one")
	dropID := soundRegistryWireID(t, "minecraft:item.bundle.drop_contents")
	c.ReadTicks(srv, 0, func(pid int32, data []byte) bool {
		if pid != protocol.IDPlaySoundEffect {
			return false
		}
		var s soundHeard
		s.read(t, data)
		if s.sound != removeID && s.sound != dropID {
			return false
		}
		if s.category != protocol.SoundCategoryPlayer || s.volume != 0.8 || s.pitch < 0.8 || s.pitch > 1.2 {
			t.Fatalf("bundle sound %d category %d volume %v pitch %v", s.sound, s.category, s.volume, s.pitch)
		}
		if s.sound == removeID {
			removes = append(removes, s)
		} else {
			drops = append(drops, s)
		}
		return false
	})
	return removes, drops
}

// TestAHeldBundleDropsItsStacksOneByOne holds a bundle of 3 stacks in use: the
// first use tick drops one, then nothing for ten ticks, then one every other
// tick, each with the drop sound, until the bundle is empty.
// vanilla: BundleItem.onUseTick
func TestAHeldBundleDropsItsStacksOneByOne(t *testing.T) {
	t.Parallel()
	srv, c, watcher := bundleHolder(t)
	startBundleUse(t, srv, c)

	// use tick k runs after k+1 ticks; drops are due at k=0, 12, 14.
	want := map[int]int{1: 1, 11: 1, 12: 1, 13: 2, 14: 2, 15: 3}
	items := 0
	for tick := 1; tick <= 15; tick++ {
		servertest.RunTicks(t, srv, 1)
		if w, ok := want[tick]; ok {
			items = w
		}
		if got := srv.ItemCount(); got != items {
			t.Fatalf("after %d ticks %d stacks dropped, want %d", tick, got, items)
		}
	}
	// The holder's client plays remove_one itself, so only drop_contents
	// reaches it; the other player hears both.
	removes, drops := bundleSounds(t, srv, watcher)
	if len(removes) != 3 || len(drops) != 3 {
		t.Fatalf("watcher got %d remove_one and %d drop_contents sounds, want 3 each", len(removes), len(drops))
	}
	holderRemoves, holderDrops := bundleSounds(t, srv, c)
	if len(holderRemoves) != 0 || len(holderDrops) != 3 {
		t.Fatalf("holder got %d remove_one and %d drop_contents sounds, want 0 and 3", len(holderRemoves), len(holderDrops))
	}
	// Fixed-point positions are eighths of a block: the drop sound sits at
	// the block centre, so every coordinate is 4 mod 8.
	for _, s := range drops {
		if s.x%8 != 4 || s.y%8 != 4 || s.z%8 != 4 {
			t.Fatalf("drop sound at %d,%d,%d, want a block centre", s.x, s.y, s.z)
		}
	}
	if left := srv.InventorySlot("Holder", player.HotbarSlot).BundleContents(); len(left) != 0 {
		t.Fatalf("bundle still holds %d stacks", len(left))
	}
	servertest.RunTicks(t, srv, 10)
	if got := srv.ItemCount(); got != 3 {
		t.Fatalf("%d items on the ground after the bundle emptied, want 3", got)
	}
}

// TestReleasingABundleStopsTheDrops lets go after the first stack: the other
// two stay in the bundle.
func TestReleasingABundleStopsTheDrops(t *testing.T) {
	t.Parallel()
	srv, c, _ := bundleHolder(t)
	startBundleUse(t, srv, c)
	servertest.RunTicks(t, srv, 1)
	if got := srv.ItemCount(); got != 1 {
		t.Fatalf("%d stacks dropped on the first tick, want 1", got)
	}
	c.ReleaseUseItem()
	c.Settle()
	servertest.RunTicks(t, srv, 30)
	if got := srv.ItemCount(); got != 1 {
		t.Fatalf("%d stacks on the ground after the release, want 1", got)
	}
	if left := srv.InventorySlot("Holder", player.HotbarSlot).BundleContents(); len(left) != 2 {
		t.Fatalf("bundle holds %d stacks, want 2", len(left))
	}
}
