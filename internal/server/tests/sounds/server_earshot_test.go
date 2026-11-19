package sounds

// The earshot tests: the sounds an item frame, an armor stand, a thrown
// pearl and a landed shaft play reach the players within sixteen blocks
// and no one farther off in the same world.
// vanilla: ServerLevel.playSeededSound, PlayerList.broadcast

import (
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"slices"
	"testing"

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

// TestItemFrameSoundsStayWithinEarshot hangs a frame, fills it, turns its
// item, pops it and breaks the frame beside one player while another stands
// forty blocks off: each sound is heard at the wall and none far off.
// vanilla: ItemFrame.playPlacementSound, ItemFrame.interact, ItemFrame.dropItem
func TestItemFrameSoundsStayWithinEarshot(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	actor, watcher, _ := servertest.PaintingWatchers(t, connect)
	servertest.BuildPaintingWall(srv, 4, 64, 65, 5, 5)
	far := farListener(t, connect, actor, watcher)

	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: servertest.FrameItemID(t, "minecraft:item_frame"), Count: 1})
	actor.MoveToGround(4.5, 64, 3.5, true)
	actor.Settle()
	actor.PlaceBlock(4, 64, 5, protocol.FaceNorth)
	hung := watcher.ExpectFrameSpawn(t, protocol.EntityTypeItemFrame, servertest.WallFrameX, servertest.WallFrameY, servertest.WallFrameZ, 180, 0, protocol.FaceNorth)
	watcher.ExpectPaintingSound(t, "minecraft:entity.item_frame.place", servertest.WallFrameX, servertest.WallFrameY, servertest.WallFrameZ)

	actor.SetCreativeSlot(player.HotbarSlot, servertest.ItemStack(t, "minecraft:diamond", 1))
	actor.UseOn(hung.EntityID)
	servertest.FrameItemSlot(t, watcher, hung.EntityID)
	watcher.ExpectPaintingSound(t, "minecraft:entity.item_frame.add_item", servertest.WallFrameX, servertest.WallFrameY, servertest.WallFrameZ)
	actor.UseOn(hung.EntityID)
	servertest.FrameMetadataField(t, watcher, hung.EntityID, protocol.MetadataIndexFrameRotation)
	watcher.ExpectPaintingSound(t, "minecraft:entity.item_frame.rotate_item", servertest.WallFrameX, servertest.WallFrameY, servertest.WallFrameZ)

	actor.Attack(hung.EntityID)
	servertest.FrameItemSlot(t, watcher, hung.EntityID)
	watcher.ExpectPaintingSound(t, "minecraft:entity.item_frame.remove_item", servertest.WallFrameX, servertest.WallFrameY, servertest.WallFrameZ)
	actor.Attack(hung.EntityID)
	watcher.ExpectPaintingRemoval(t, hung.EntityID)
	watcher.ExpectPaintingSound(t, "minecraft:entity.item_frame.break", servertest.WallFrameX, servertest.WallFrameY, servertest.WallFrameZ)

	if heardAny(srv, far, "minecraft:entity.item_frame.place", "minecraft:entity.item_frame.add_item",
		"minecraft:entity.item_frame.rotate_item", "minecraft:entity.item_frame.remove_item",
		"minecraft:entity.item_frame.break") {
		t.Error("a player forty blocks from the frame heard its sound")
	}
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

// TestEnderPearlSoundsStayWithinEarshot throws a pearl at the thrower's
// feet while another player stands forty blocks off: the thrower hears the
// throw and the landing's teleport, the far player neither.
// vanilla: EnderpearlItem.use, ThrownEnderpearl.playSound
func TestEnderPearlSoundsStayWithinEarshot(t *testing.T) {
	t.Parallel()
	cfg := servertest.BowTestConfig()
	cfg.SimulationDistance = 2
	srv, connect := servertest.Start(t, cfg)
	c := connect()
	c.JoinAs("Thrower")
	srv.GiveSlot("Thrower", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:ender_pearl"), Count: 16})
	far := farListener(t, connect, c)

	c.MoveTo(0.5, 64, 0.5, -90, 45)
	c.Send(protocol.IDPlayUseItem, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.HandMain)
		b.PutVarInt(1)
		b.PutF32(-90)
		b.PutF32(45)
	})

	var heard []string
	c.ReadTicks(srv, 60, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySoundEffect {
			return false
		}
		if name, ok := servertest.SoundEventName(protocol.NewReader(data).ReadVarInt() - 1); ok {
			heard = append(heard, name)
		}
		return false
	})
	var thrown, teleported bool
	for _, name := range heard {
		switch name {
		case "minecraft:entity.ender_pearl.throw":
			thrown = true
		case "minecraft:entity.player.teleport":
			teleported = true
		}
	}
	if !thrown || !teleported {
		t.Fatalf("the thrower heard throw=%v teleport=%v, want both: %+v", thrown, teleported, heard)
	}
	if heardAny(srv, far, "minecraft:entity.ender_pearl.throw", "minecraft:entity.player.teleport") {
		t.Error("a player forty blocks from the pearl heard its sound")
	}
}

// TestArrowHitSoundsStayWithinEarshot lands shafts in the ground, on an
// armor stand and on a boat, and deflects a trident off an invisible stand,
// all beside one player while another stands forty blocks off: each hit is
// heard close by and none far off.
// vanilla: AbstractArrow.onHitBlock, AbstractArrow.onHitEntity, ThrownTrident.onHitEntity
func TestArrowHitSoundsStayWithinEarshot(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.SurvivalTestConfig(), servertest.OpsListing(t, "Archer"))
	c := connect().Join("Archer")
	c.MoveToGrounded(3.5, 64, 6.5, 0, 0, true)
	c.Settle()
	far := farListener(t, connect, c)
	hits := []string{"minecraft:entity.arrow.hit", "minecraft:item.trident.hit", "minecraft:item.trident.hit_ground"}

	// Into the ground beside the archer.
	if id := srv.ShootArrow(0.5, 66, 3.5, 0, -1, 0); id == 0 {
		t.Fatal("the ground shot never flew")
	}
	servertest.ExpectSoundEvent(t, c, "minecraft:entity.arrow.hit")
	if heardAny(srv, far, hits...) {
		t.Error("a player forty blocks off heard a shaft land in the ground")
	}

	// Into a stand, which it breaks.
	c.ChatCommand(`summon minecraft:armor_stand 3.5 64 3.5`)
	c.ExpectArmorStandSpawn()
	if id := srv.ShootArrow(1.5, 64.5, 3.5, 10, 0, 0); id == 0 {
		t.Fatal("the stand shot never flew")
	}
	servertest.ExpectSoundEvent(t, c, "minecraft:entity.arrow.hit")
	servertest.WaitFor(t, srv, 200, func() bool { return servertest.StandsStanding(srv) == 0 })
	if heardAny(srv, far, hits...) {
		t.Error("a player forty blocks off heard a shaft land on a stand")
	}

	// A trident off an invisible stand, which refuses the hurt.
	c.ChatCommand(`summon minecraft:armor_stand 3.5 64 3.5 {Invisible:1b}`)
	c.ExpectArmorStandSpawn()
	if id := srv.ShootTrident(1.5, 64.5, 3.5, 10, 0, 0); id == 0 {
		t.Fatal("the trident never flew")
	}
	servertest.ExpectSoundEvent(t, c, "minecraft:item.trident.hit")
	if heardAny(srv, far, hits...) {
		t.Error("a player forty blocks off heard a trident deflect off a stand")
	}

	// Into a boat's hull.
	if srv.SpawnBoat(0.5, 64, 2.5, "minecraft:oak_boat") == 0 {
		t.Fatal("the boat never stood up")
	}
	if id := srv.ShootArrow(0.5, 64.4, -0.5, 0, 0, 3); id == 0 {
		t.Fatal("the boat shot never flew")
	}
	servertest.ExpectSoundEvent(t, c, "minecraft:entity.arrow.hit")
	if heardAny(srv, far, hits...) {
		t.Error("a player forty blocks off heard a shaft land on a boat")
	}
}

// TestAnArrowsPaintingHitStaysWithinEarshot breaks a painting with an
// ownerless shaft beside two players while a third stands forty blocks off:
// the shaft's hit is heard at the wall and not far off.
// vanilla: AbstractArrow.onHitEntity
func TestAnArrowsPaintingHitStaysWithinEarshot(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	actor, watcher, _ := servertest.PaintingWatchers(t, connect)
	servertest.BuildPaintingWall(srv, 4, 64, 65, 5, 5)
	far := farListener(t, connect, actor, watcher)
	hung := hangShotPainting(t, srv, actor, watcher)
	actor.MoveToGround(2.5, 64, 3.5, true)
	actor.Settle()

	if id := srv.ShootArrow(shotPaintingX, shotPaintingY, 1.5, 0, 0, 10); id == 0 {
		t.Fatal("the arrow never flew")
	}
	watcher.ExpectPaintingRemoval(t, hung.EntityID)
	servertest.ExpectSoundEvent(t, watcher, "minecraft:entity.arrow.hit")
	if heardAny(srv, far, "minecraft:entity.arrow.hit") {
		t.Error("a player forty blocks off heard a shaft land on a painting")
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
