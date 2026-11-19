package sounds

// The earshot tests: the sounds an item frame, an armor stand, a thrown
// pearl and a landed shaft play reach the players within sixteen blocks
// and no one farther off in the same world.
// vanilla: ServerLevel.playSeededSound, PlayerList.broadcast

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
)

// farListener connects a player at x=40.5, well past the sixteen blocks a
// sound at volume one carries from anything these tests do near the origin,
// and drops the traffic its arrival left on the others.
func farListener(t *testing.T, connect func() *servertest.Client, others ...*servertest.Client) *servertest.Client {
	t.Helper()
	far := connect().Join("Faraway")
	far.MoveTo(40.5, 64, 0.5, 0, 0)
	far.Settle()
	for _, c := range others {
		c.Settle()
	}
	return far
}

// heardAny reports whether any of the named sounds reached c within 40 ticks.
func heardAny(srv *server.Server, c *servertest.Client, names ...string) bool {
	c.T.Helper()
	return c.ReadTicksSkipping(srv, 40, nil, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySoundEffect {
			return false
		}
		r := protocol.NewReader(data)
		wireID := r.ReadVarInt()
		if wireID == 0 {
			return false
		}
		name, ok := servertest.SoundEventName(wireID - 1)
		if !ok {
			return false
		}
		return slices.Contains(names, name)
	})
}

// TestArmorStandSoundsStayWithinEarshot places an armor stand and punches
// it down from a creative hand while another player stands forty blocks
// off: the placer hears both sounds and the far player neither.
// vanilla: ArmorStandItem.useOn, ArmorStand.playBrokenSound
func TestArmorStandSoundsStayWithinEarshot(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	c := connect().Join("Placer")
	far := farListener(t, connect, c)

	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: servertest.RegistryItemID("minecraft:armor_stand"), Count: 1})
	c.MoveToGrounded(3.5, 64, 5.5, 180, 40, true)
	c.Settle()
	c.PlaceBlock(3, 63, 3, protocol.FaceTop)
	spawn := c.ExpectArmorStandSpawn()
	c.ExpectArmorStandSound("minecraft:entity.armor_stand.place", protocol.SoundCategoryBlock, 0.75, 0.8, 3.5, 64, 3.5)

	c.Attack(spawn.EntityID)
	c.ExpectRemoval(spawn.EntityID)
	c.ExpectArmorStandSound("minecraft:entity.armor_stand.break", protocol.SoundCategoryNeutral, 1.0, 1.0, 3.5, 64, 3.5)

	if heardAny(srv, far, "minecraft:entity.armor_stand.place", "minecraft:entity.armor_stand.break") {
		t.Error("a player forty blocks from the stand heard its sound")
	}
}

// TestShaftThrowSoundsStayWithinEarshot looses an arrow and throws a
// trident beside the shooter while another player stands forty blocks off:
// the shooter hears both and the far player neither.
// vanilla: BowItem.releaseUsing, TridentItem.releaseUsing
func TestShaftThrowSoundsStayWithinEarshot(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BowTestConfig())
	c := connect()
	c.JoinAs("Shooter")
	servertest.GiveBowAndArrows(srv, "Shooter")
	srv.GiveSlot("Shooter", 38, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:trident"), Count: 1})
	far := farListener(t, connect, c)
	c.MoveToGround(0.5, 64, 0.5, true)

	shootOnce(t, srv, c, "Shooter")
	servertest.ExpectSoundEvent(t, c, "minecraft:entity.arrow.shoot")
	if heardAny(srv, far, "minecraft:entity.arrow.shoot") {
		t.Error("a player forty blocks off heard an arrow loosed")
	}

	c.Send(protocol.IDPlayHeldItemIn, func(b *protocol.Buffer) { b.PutI16(2) })
	startTridentCharge(t, srv, c, "Shooter", -90, 0, 12)
	endTridentCharge(t, srv, c)
	servertest.ExpectSoundEvent(t, c, "minecraft:item.trident.throw")
	if heardAny(srv, far, "minecraft:item.trident.throw") {
		t.Error("a player forty blocks off heard a trident thrown")
	}
}

// shotPaintingX, shotPaintingY and shotPaintingZ are the centre of the kebab
// hangShotPainting hangs on the north face of the standard wall.
const (
	shotPaintingX = 4.5
	shotPaintingY = 64.5
	shotPaintingZ = 4.96875
)

// hangShotPainting hangs a kebab on the north face of the standard wall from
// a creative hand and returns it as the watcher saw it arrive.
func hangShotPainting(t *testing.T, srv *server.Server, actor, watcher *servertest.Client) servertest.PaintingSpawn {
	t.Helper()
	actor.SetCreativeSlot(player.HotbarSlot, servertest.PaintingStack(t, servertest.PaintingVariantID(t, "minecraft:kebab")))
	actor.MoveToGround(4.5, 64, 3.5, true)
	actor.Settle()
	actor.PlaceBlock(4, 64, 5, protocol.FaceNorth)
	got := watcher.ExpectPaintingSpawn(t, protocol.FaceNorth, 4, 64, 4)
	watcher.ExpectPaintingSound(t, "minecraft:entity.painting.place", shotPaintingX, shotPaintingY, shotPaintingZ)
	actor.ExpectBlockAck()
	actor.ExpectUseEchoes(4, 64, 5, protocol.FaceNorth)
	return got
}

// shootOnce draws the bow for fifteen hand-driven ticks and releases it.
func shootOnce(t *testing.T, srv *server.Server, c *servertest.Client, name string) {
	t.Helper()
	srv.StopTick()
	servertest.DrawBow(c, -90, 0)
	c.Settle()
	for i := 0; srv.BowTicks(name) < 15; i++ {
		if i == 15 {
			t.Fatalf("fifteen ticks drew the bow %d ticks, want 15", srv.BowTicks(name))
		}
		srv.TickSurvival()
	}
	servertest.ReleaseBow(c)
	c.SettleKeeping()
	srv.StartTick()
}

// startTridentCharge counts the trident's charge out in hand-driven ticks so
// a running loop cannot run past it, leaving the loop stopped and the draw held.
func startTridentCharge(t *testing.T, srv *server.Server, c *servertest.Client, name string, yaw, pitch float32, ticks int32) {
	t.Helper()
	c.Settle()
	srv.StopTick()
	servertest.DrawBow(c, yaw, pitch)
	c.Settle()
	for i := int32(0); srv.BowTicks(name) < ticks; i++ {
		if i == ticks+5 {
			t.Fatalf("%d hand ticks charged the trident %d ticks, want %d", ticks+5, srv.BowTicks(name), ticks)
		}
		srv.TickSurvival()
	}
}

// endTridentCharge releases the charge and starts the tick loop again.
func endTridentCharge(t *testing.T, srv *server.Server, c *servertest.Client) {
	t.Helper()
	servertest.ReleaseBow(c)
	c.SettleKeeping()
	servertest.HoldTicks(t, srv)
}
