package entities

import (
	"math"
	"sync/atomic"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The ender pearl as the wire carries it: a thrown body whose landing
// carries its own thrower with it. The throw shares the snowball's flight,
// with a cooldown of its own on the item; the landing teleports the owner
// (the player's Synchronize Player Position) with 5.0 of ender_pearl hurt
// on them; an owner who is dead, or asleep in the pearl's own dimension,
// is left behind while the pearl still breaks, and one in another
// dimension is carried across to the landing. The pearl on the wire is
// minecraft:ender_pearl (44), shown with
// its own stack in the metadata, and its break is quiet: no entity event,
// only the removal — vanilla's pearl draws its portal puff client-side
// (Level.addParticle is empty on a server), so nothing particle-shaped
// crosses the wire.

// doubleListOf reads a list tag of doubles from the compound, or false
// for a missing or differently shaped one.
func doubleListOf(compound nbt.Compound, key string) ([]float64, bool) {
	list, ok := nbt.Get[nbt.List](compound, key)
	if !ok || len(list.Items) != 3 {
		return nil, false
	}
	out := make([]float64, 3)
	for i, item := range list.Items {
		value, ok := item.(nbt.Double)
		if !ok {
			return nil, false
		}
		out[i] = float64(value)
	}
	return out, true
}

// TestAnEnderPearlTeleportsItsThrowerToItsLandingAndHurtsThem throws an
// ender pearl steeply down from the thrower's eyes and reads the whole
// flight and landing: the appear of the pearl as its own entity type with
// the throw's own sound behind it and its own stack in the metadata, the
// cooldown of twenty ticks the throw starts (cleared again when it runs
// out), and the landing's whole telling — the thrower put where the pearl
// broke by one Synchronize Player Position, 5.0 of the ender_pearl damage
// type landed on them, the teleport sound at the landing, and the pearl
// taken out of every view with no entity event at all (vanilla's pearl
// break is a client-side puff, nothing a server broadcasts).
func TestAnEnderPearlTeleportsItsThrowerToItsLandingAndHurtsThem(t *testing.T) {
	t.Parallel()
	cfg := servertest.BowTestConfig()
	cfg.SimulationDistance = 2
	srv, c, playerID := servertest.JoinAs(t, cfg, "Thrower")
	servertest.GivePearls(t, srv)

	// Aimed steeply down, the flight is two ticks of falling (the egg
	// flight's own shape), so the break sits close behind the throw.
	c.MoveTo(0.5, 64, 0.5, -90, 45)
	servertest.UseHeld(c, -90, 45)

	live := servertest.ReadPearlLive(c, srv, 60)
	pearls := servertest.SpawnsOf(live.Spawns, protocol.EntityTypeEnderPearl)
	if len(pearls) != 1 {
		t.Fatalf("the throw put %d ender pearls into the world, want one", len(pearls))
	}
	pearl := pearls[0]
	if pearl.OwnerID != playerID {
		t.Errorf("the pearl's data field names entity %d, want the thrower's %d", pearl.OwnerID, playerID)
	}
	// The birth: at the thrower's eye less its tenth, with the power of
	// one and a half behind it, the noise vanilla's inaccuracy adds
	// included.
	if d := pearl.Y - (64 + 1.62 - 0.1); d < -0.01 || d > 0.01 {
		t.Errorf("the pearl was born at y=%v, want the thrower's eye less its tenth", pearl.Y)
	}
	speed := pearl.Vx*pearl.Vx + pearl.Vy*pearl.Vy + pearl.Vz*pearl.Vz
	if speed < 2.1 || speed > 2.4 {
		t.Errorf("the pearl flies at %v blocks a tick squared, want the one and a half's square's neighbourhood", speed)
	}
	if pearl.Vy >= 0 {
		t.Errorf("a pearl thrown steeply down flew up: vy=%v", pearl.Vy)
	}

	// The throw's sound: the pearl's own, to the neutral category, half a
	// block loud from the thrower's feet.
	var throwSound *servertest.ThrownSound
	for i := range live.Sounds {
		if live.Sounds[i].Name == "minecraft:entity.ender_pearl.throw" {
			throwSound = &live.Sounds[i]
			break
		}
	}
	if throwSound == nil {
		t.Fatalf("no pearl throw sound was played: %+v", live.Sounds)
	}
	if throwSound.Category != protocol.SoundCategoryNeutral {
		t.Errorf("the throw's sound category = %d, want the neutral one", throwSound.Category)
	}
	if throwSound.X != 0.5 || throwSound.Y != 64 || throwSound.Z != 0.5 {
		t.Errorf("the throw's sound played from (%v %v %v), want the thrower's feet", throwSound.X, throwSound.Y, throwSound.Z)
	}
	if throwSound.Volume != 0.5 {
		t.Errorf("the throw's sound volume = %v, want the vanilla half", throwSound.Volume)
	}

	// The item the pearl is a stack of: one pearl, twice — the spawn's
	// telling and the tracked entity's first pass.
	var metas int
	for _, m := range live.MetasOf(pearl.EntityID) {
		{
			metas++
			if m.Count != 1 || m.Item != servertest.MustItem("minecraft:ender_pearl") {
				t.Errorf("the pearl's metadata carried %d of item %d, want one pearl", m.Count, m.Item)
			}
		}
	}
	if metas != 2 {
		t.Errorf("the pearl's metadata arrived %d times, want the spawn's and the first pass's", metas)
	}

	// The cooldown: twenty ticks of the item's own group, told at the
	// throw and cleared again once it runs out.
	pearlGroup := "minecraft:ender_pearl"
	cooldowns := live.CooldownsOf(pearlGroup)
	if len(cooldowns) < 2 || cooldowns[0].Ticks != 20 {
		t.Fatalf("the pearl's cooldowns = %+v, want twenty ticks started at the throw and cleared after", cooldowns)
	}
	if cooldowns[len(cooldowns)-1].Ticks != 0 {
		t.Errorf("the pearl's cooldown group was never cleared: %+v", cooldowns)
	}

	// The landing's teleport: the thrower put where the pearl broke — a
	// couple of blocks east at the ground's face, in their own two ticks
	// of flight — their look kept, exactly once.
	if len(live.Syncs) != 1 {
		t.Fatalf("the thrower was told %d teleports, want the landing's one", len(live.Syncs))
	}
	sync := live.Syncs[0]
	if sync.X < 1.5 || sync.X > 4.5 {
		t.Errorf("the teleport put the thrower at x=%v, want the landing's couple of blocks east", sync.X)
	}
	if sync.Y < 63.5 || sync.Y > 64.5 {
		t.Errorf("the teleport put the thrower at y=%v, want the ground's face", sync.Y)
	}
	if sync.Z < 0 || sync.Z > 1 {
		t.Errorf("the teleport put the thrower at z=%v, want the flight's own line", sync.Z)
	}
	if sync.Yaw != -90 || sync.Pitch != 45 {
		t.Errorf("the teleport carries yaw %v pitch %v, want the look the throw was made from", sync.Yaw, sync.Pitch)
	}

	// The landing's hurt: 5.0 of the ender_pearl damage type, the health
	// fifteen after it.
	damageTypes := map[int32]bool{}
	for _, d := range live.DamageEvents {
		if d.Id == playerID {
			damageTypes[d.DamageType] = true
		}
	}
	pearlDamage := registry.MustID("minecraft:damage_type", "minecraft:ender_pearl")
	if !damageTypes[pearlDamage] {
		t.Errorf("the landing hurt carried damage types %+v, want minecraft:ender_pearl (%d)", damageTypes, pearlDamage)
	}
	told := false
	for _, h := range live.Healths {
		if h == 15 {
			told = true
		}
	}
	if !told {
		t.Errorf("the thrower's healths = %+v, want the fifteen the 5.0 landing hurt leaves", live.Healths)
	}

	// The teleport's sound at the landing spot the synchronize put the
	// thrower at, and the pearl gone with no entity event to tell of it.
	var teleportSound bool
	for _, s := range live.Sounds {
		if s.Name == "minecraft:entity.player.teleport" &&
			s.Category == protocol.SoundCategoryPlayer &&
			math.Abs(s.X-sync.X) < 0.2 && math.Abs(s.Y-sync.Y) < 0.2 && math.Abs(s.Z-sync.Z) < 0.2 {
			teleportSound = true
		}
	}
	if !teleportSound {
		t.Errorf("no teleport sound played at the landing: %+v", live.Sounds)
	}
	if events := live.Events; len(events) != 0 {
		t.Errorf("the pearl's landing told entity events %v, want none (the break is a client-side puff)", events)
	}
	if !live.RemovedOnce(pearl.EntityID) {
		t.Error("the pearl was not taken out of the world exactly once")
	}

	// The survival hand paid one pearl of its sixteen.
	if left := srv.InventorySlot("Thrower", 36); left.Count != 15 {
		t.Errorf("the hotbar holds %d pearls after the throw, want the fifteen one throw left", left.Count)
	}
}

// TestAPearlLandsOnAMobWithAThrownHurtAndCarriesItsOwnerAlong throws a
// pearl at a chicken standing in the flight: the chicken takes the thrown
// blow — the damage event, the knockback along the flight, its own hurt
// sound, and the four health a no-damage hurt leaves — while the landing
// still carries the thrower to it, where an egg's break would leave them
// standing.
func TestAPearlLandsOnAMobWithAThrownHurtAndCarriesItsOwnerAlong(t *testing.T) {
	t.Parallel()
	cfg := servertest.BowTestConfig()
	cfg.SimulationDistance = 2
	// The landing leaves the owner in the air, where a test client never
	// falls: the flight check would kick it 80 ticks on, inside a loaded
	// run's reading.
	cfg.AllowFlight = true
	srv, c, _ := servertest.JoinAs(t, cfg, "Thrower")

	cowID := srv.SpawnMob("minecraft:chicken", 2.5, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeChicken)
	srv.SetMobSoundVariant(cowID, "minecraft:classic")
	servertest.GivePearls(t, srv)

	c.MoveTo(0.5, 64, 0.5, -90, 25)
	srv.MoveMob(cowID, 2.5, 0.5)
	servertest.UseHeld(c, -90, 25)

	live := servertest.ReadPearlLive(c, srv, 80)
	pearls := servertest.SpawnsOf(live.Spawns, protocol.EntityTypeEnderPearl)
	if len(pearls) != 1 {
		t.Fatalf("the throw put %d ender pearls into the world, want one", len(pearls))
	}
	pearl := pearls[0]

	// The blow on the chicken: the thrown damage type, the shove along the
	// flight, and the voice — a pearl's hit is the egg's own no-damage
	// thrown hurt.
	var cowDamages int
	for _, d := range live.DamageEvents {
		if d.Id == cowID {
			cowDamages++
			if want := registry.MustID("minecraft:damage_type", "minecraft:thrown"); d.DamageType != want {
				t.Errorf("the chicken's damage event type = %d, want minecraft:thrown (%d)", d.DamageType, want)
			}
		}
	}
	if cowDamages != 1 {
		t.Fatalf("the chicken was named in %d damage events, want the blow's one", cowDamages)
	}
	knocked := false
	for _, m := range live.Motions {
		// The shove lifts the cow; the motions after it only follow its fall
		// and its run.
		if m.Id != cowID || m.Vy <= 0 || knocked {
			continue
		}
		knocked = true
		if m.Vx < 0.3 || m.Vx > 0.5 {
			t.Errorf("the knockback ran east at vx=%v, want the flight's own way near four tenths", m.Vx)
		}
	}
	if !knocked {
		t.Error("the chicken was never knocked, want the shove along the flight")
	}
	hurtSound := false
	for _, s := range live.Sounds {
		if s.Name == "minecraft:entity.chicken.hurt" && s.X > 2 && s.X < 3 {
			hurtSound = true
		}
	}
	if !hurtSound {
		t.Errorf("no chicken hurt sound played from the chicken: %+v", live.Sounds)
	}

	// The landing still carries the owner to the blow's place: the
	// thrower is put by the chicken, within a couple of blocks of the
	// throw made the flight's own hit point.
	if len(live.Syncs) != 1 {
		t.Fatalf("the thrower was told %d teleports, want the landing's one", len(live.Syncs))
	}
	sync := live.Syncs[0]
	if sync.X < 1 || sync.X > 3 {
		t.Errorf("the teleport put the thrower at x=%v, want the blow's place at the chicken", sync.X)
	}
	if !live.RemovedOnce(pearl.EntityID) {
		t.Error("the pearl was not taken out of the world exactly once")
	}
}

// TestAPearlInFlightIsSavedWithItsPlayerAndResumedOnRejoining throws a
// pearl on a long flight, disconnects while it is in the air, and checks
// the player file it leaves behind: an ender_pearls list holding the pearl
// whole — its entity tag with the position, motion and owner in it, and
// its dimension — and the removal of the pearl from the world the leaving
// takes. The rejoin raises the pearl again for the returning player, and
// the flight that is left of it lands and teleports them.
func TestAPearlInFlightIsSavedWithItsPlayerAndResumedOnRejoining(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()
	srv, connect := servertest.Start(t, cfg)

	// A second player standing a few blocks west, off the pearl's own
	// flight, so the pearl's second life is told to someone while it is
	// away.
	watcher := connect()
	watcher.JoinAs("Watcher")
	watcher.MoveTo(-3.5, 64, 0.5, 90, 0)

	c := connect()
	c.JoinAs("Thrower")
	watcher.ExpectPlayerAdded("Thrower")
	servertest.GivePearls(t, srv)

	// Held from the throw to the rejoin's first tick, so the flight is cut
	// at the same tick however slow the machine is and the pearl neither
	// lands before the leaving nor flies on during the rejoin.
	servertest.HoldTicks(t, srv)

	// The pearl on a long flight, tilted east: twenty ticks after the
	// throw, it is high and still rising.
	c.MoveTo(0.5, 64, 0.5, -90, -60)
	servertest.UseHeld(c, -90, -60)
	live := servertest.ReadPearlLive(c, srv, 18)
	pearls := servertest.SpawnsOf(live.Spawns, protocol.EntityTypeEnderPearl)
	if len(pearls) != 1 {
		t.Fatalf("the throw put %d ender pearls into the world, want one", len(pearls))
	}
	pearl := pearls[0]
	if pearl.EntityID == 0 {
		t.Fatal("the pearl carries no entity id")
	}
	c.Disconnect()

	// The file carries the pearl whole.
	root := servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, "Thrower"))
	list, ok := nbt.Get[nbt.List](root, "ender_pearls")
	if !ok || len(list.Items) != 1 {
		t.Fatalf("ender_pearls = %v, %v; want the one pearl in flight", list, ok)
	}
	saved, ok := list.Items[0].(nbt.Compound)
	if !ok {
		t.Fatalf("the saved pearl is a %s, want a compound", list.Items[0].Type())
	}
	if got, _ := nbt.Get[nbt.String](saved, "id"); got != "minecraft:ender_pearl" {
		t.Errorf("the saved pearl's id = %v, want minecraft:ender_pearl", got)
	}
	if dim, _ := nbt.Get[nbt.String](saved, "ender_pearl_dimension"); dim != world.DimensionName {
		t.Errorf("the saved pearl's dimension = %v, want %v", dim, world.DimensionName)
	}
	pos, ok := doubleListOf(saved, "Pos")
	if !ok || len(pos) != 3 || pos[1] < 66 {
		t.Errorf("the saved pearl's Pos = %v, want the mid-flight height", pos)
	}
	motion, _ := doubleListOf(saved, "Motion")
	if len(motion) != 3 || motion[0] <= 0 || motion[1] <= 0 {
		t.Errorf("the saved pearl's Motion = %v, want a flight in it", motion)
	}
	if owner, ok := nbt.Get[nbt.IntArray](saved, "Owner"); ok {
		want := servertest.OfflineUUIDWords("Thrower")
		for i := range 4 {
			if uint32(owner[i]) != want[i] {
				t.Errorf("the saved pearl's owner word %d = %v, want the thrower's %v", i, owner[i], want[i])
			}
		}
	} else {
		t.Error("the saved pearl names no owner")
	}
	item, ok := saved["Item"].(nbt.Compound)
	if !ok {
		t.Fatal("the saved pearl has no Item stack")
	}
	if got, _ := nbt.Get[nbt.String](item, "id"); got != "minecraft:ender_pearl" {
		t.Errorf("the saved pearl's stack = %v, want one ender pearl", got)
	}

	// The leaving took the pearl out of the world: the watcher standing by
	// is told it is gone.
	seen := watcher.ReadTicks(srv, 20, func(gotID int32, data []byte) bool {
		if gotID != protocol.IDPlayRemoveEntities {
			return false
		}
		r := protocol.NewReader(data)
		count := r.ReadVarInt()
		for range count {
			if r.ReadVarInt() == pearl.EntityID {
				return true
			}
		}
		return false
	})
	if !seen {
		t.Error("the watcher was never told of the pearl's removal at the thrower's leaving")
	}

	// The rejoin: the pearl raised again for the returning player, the
	// flight's remainder flown, and the landing teleports them where the
	// pearl broke.
	servertest.FreezeUntilHeld(t, srv)
	servertest.ReleaseTicks(t, srv)
	again := connect()
	again.JoinAs("Thrower")
	servertest.HoldTicks(t, srv)

	resumed := servertest.ReadPearlLive(again, srv, 120)
	arrivals := servertest.SpawnsOf(resumed.Spawns, protocol.EntityTypeEnderPearl)
	if len(arrivals) != 1 {
		t.Fatalf("the rejoin raised %d pearls, want the one saved in flight", len(arrivals))
	}
	if arrivals[0].EntityID == pearl.EntityID {
		t.Error("the resumed pearl kept the departed body's entity id")
	}
	if len(resumed.Syncs) != 1 {
		t.Fatalf("the returning thrower was told %d teleports, want the landing's one", len(resumed.Syncs))
	}
	if pos := srv.PlayerPosition("Thrower"); pos.X < 5 {
		t.Errorf("the resumed landing put the thrower at x=%v, want the flight's landing well east", pos.X)
	}
	pearlDamage := registry.MustID("minecraft:damage_type", "minecraft:ender_pearl")
	hurt := false
	for _, d := range resumed.DamageEvents {
		if d.DamageType == pearlDamage {
			hurt = true
		}
	}
	if !hurt {
		t.Error("the resumed landing dealt no ender_pearl hurt")
	}
	if !resumed.RemovedOnce(arrivals[0].EntityID) {
		t.Error("the resumed pearl was not taken out of the world after its landing")
	}
}

// TestAPearlLandingInAnotherDimensionCarriesItsOwner throws a pearl through
// a nether portal — the same crossing a thrown egg makes — and lets it land
// in the nether: vanilla carries the owner across to the pearl's place even
// though the thrower stood in the overworld, so the thrower ends beside
// where the pearl broke rather than left at the throw.
// vanilla: ThrownEnderpearl.onHit, ThrownEnderpearl.isAllowedToTeleportOwner
func TestAPearlLandingInAnotherDimensionCarriesItsOwner(t *testing.T) {
	t.Parallel()
	cfg := servertest.CrossingTestConfig(t)
	srv, connect := servertest.Start(t, cfg)

	thrower := connect().Join("Thrower")

	// step names the wait the test is in; a failure logs it with the state
	// the waits cannot say themselves.
	step := "joining"
	var carried atomic.Bool
	var crossed, removed bool
	arrivalCooldown := int32(-1)
	var spot server.TestItem
	var arrival servertest.ThrownSpawn
	srv.SetPearlCarryHook(func() { carried.Store(true) })
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		t.Logf("failed in step %q: spot=(%v, %v, %v) arrival pearl=(%v, %v, %v) crossed=%v removed=%v arrivalCooldown=%d carryRan=%v thrower in %q at %+v; overworld pearls %+v, nether pearls %+v",
			step, spot.X, spot.Y, spot.Z, arrival.X, arrival.Y, arrival.Z, crossed, removed, arrivalCooldown, carried.Load(),
			srv.PlayerDimension("Thrower"), srv.PlayerPosition("Thrower"),
			srv.ThrownInDimension(world.DimensionName), srv.ThrownInDimension(world.NetherName))
	})

	servertest.LightPortal(t, srv, thrower)

	// A stone dropped through first builds the nether portal the pearl is
	// to arrive by, and says where it stands.
	stone, ok := registry.ItemID("minecraft:stone")
	if !ok {
		t.Fatal("the item registry has no minecraft:stone")
	}
	thrower.SetCreativeSlot(36, protocol.ItemStack{ItemID: stone, Count: 1})
	thrower.MoveTo(5.0, servertest.PortalFloorY, 5.5, 180, 0)
	servertest.DropHeld(thrower, 5, servertest.PortalFloorY, 5)
	step = "the dropped stone reaching the nether"
	servertest.UntilTicks(t, srv, 400, step, func() bool {
		return len(srv.ItemsInDimension(world.NetherName)) == 1 &&
			len(srv.ItemsInDimension(world.DimensionName)) == 0
	})
	spot = srv.ItemsInDimension(world.NetherName)[0]

	// The mover in the nether the arrival is shown to.
	mover := connect().Join("Arrival")
	srv.MovePlayerToDimension("Arrival", world.NetherName,
		float64(int(spot.X)), float64(int(spot.Y)), float64(int(spot.Z))+2)
	step = "the nether arriving for the mover"
	if _, ok := servertest.ReadUntilViewSent(t, mover); !ok {
		t.Fatal("the nether never arrived for the mover")
	}

	// The pearl, thrown level into the portal from a block south of it.
	srv.GiveSlot("Thrower", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:ender_pearl"), Count: 16})
	thrower.MoveTo(5.0, servertest.PortalFloorY, 6.0, 180, 0)
	// The same spot again, the movement the throw rides nought.
	thrower.MoveTo(5.0, servertest.PortalFloorY, 6.0, 180, 0)
	servertest.UseHeld(thrower, 180, 0)
	step = "the thrown pearl spawning for the thrower"
	thrower.ExpectThrownSpawn(srv, protocol.EntityTypeEnderPearl)

	// The crossing and the far-side landing, as the nether's standing
	// player is told: the arrival's own spawn, then its flight ending. The
	// thrower's own stream is drained throughout — its mailbox must not back
	// up behind the Respawn that carries it across.
	step = "the pearl's arrival spawning for the nether watcher"
	arrival = awaitPearlSpawn(t, srv, mover)
	// The Respawn carrying the thrower across says their portal cooldown:
	// the pearl's, taken before the carry and still whole on arrival, so the
	// nether portal the creative thrower is set down beside does not take
	// them straight back.
	step = "the thrower crossing and the pearl's landing"
	servertest.UntilTicks(t, srv, 400, "the thrower crossing, its Respawn and the pearl's landing reaching the nether watcher", func() bool {
		thrower.ReadTicks(srv, 0, func(gotID int32, data []byte) bool {
			if gotID == protocol.IDPlayRespawn && arrivalCooldown < 0 {
				arrivalCooldown = servertest.RespawnPortalCooldown(t, data)
			}
			return false
		})
		mover.ReadTicks(srv, 0, func(gotID int32, data []byte) bool {
			if gotID != protocol.IDPlayRemoveEntities {
				return false
			}
			r := protocol.NewReader(data)
			count := r.ReadVarInt()
			for range count {
				if r.ReadVarInt() == arrival.EntityID {
					removed = true
				}
			}
			return false
		})
		if srv.PlayerDimension("Thrower") == world.NetherName {
			crossed = true
		}
		return crossed && removed && arrivalCooldown >= 0
	})
	if want := int32(10); arrivalCooldown != want { // vanilla: Player.getDimensionChangingDelay
		t.Errorf("the thrower arrived with portal cooldown %d, want %d", arrivalCooldown, want)
	}
	if !crossed {
		t.Fatalf("the thrower stayed in %q, want carried to the pearl in %q",
			srv.PlayerDimension("Thrower"), world.NetherName)
	}
	if !removed {
		t.Fatal("the pearl landing in the nether was never removed")
	}
	// The owner stands where the pearl broke, in the nether and not at the
	// throw they left behind.
	pos := srv.PlayerPosition("Thrower")
	if math.Abs(pos.X-arrival.X) > 24 || math.Abs(pos.Z-arrival.Z) > 24 {
		t.Errorf("the thrower landed at (%v, %v), want them beside the pearl's nether arrival near (%v, %v)",
			pos.X, pos.Z, arrival.X, arrival.Z)
	}
}

// awaitPearlSpawn waits for the next ender pearl to come into the world
// and reads its spawn, stepping over the traffic the crossing and the
// other bodies carry.
func awaitPearlSpawn(t *testing.T, srv *server.Server, c *servertest.Client) servertest.ThrownSpawn {
	t.Helper()
	var spawn servertest.ThrownSpawn
	found := c.ReadTicks(srv, 400, func(gotID int32, data []byte) bool {
		if gotID != protocol.IDPlaySpawnEntity {
			return false
		}
		s := servertest.ReadThrownSpawn(c, data)
		if s.EntityType != protocol.EntityTypeEnderPearl {
			return false
		}
		spawn = s
		return true
	})
	if !found {
		t.Fatal("no ender pearl came into the world")
	}
	return spawn
}
