package entities

import (
	"math"
	"sync"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
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

func TestAPearlLandingTakesItsRidingOwnerOffTheirBoat(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	srv, c := servertest.Join(t, cfg, "Thrower", servertest.OpsListing(t, "Thrower"))
	servertest.HoldTicks(t, srv)
	c.MoveTo(0.5, 64, 0.5, -90, 10)
	boat := srv.SpawnBoat(0.5, 64, 0.5, "minecraft:oak_boat")
	c.ChatCommand("ride @s mount @e[type=oak_boat,limit=1]")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.mount.success")
	if got := servertest.AwaitPassengersFor(c, boat); len(got) != 1 || got[0] != c.PlayerEntityID {
		t.Fatalf("boat passengers = %v, want the thrower", got)
	}
	servertest.GivePearls(t, srv)
	c.Settle()
	servertest.UseHeld(c, -90, 10)
	// Handle the throw before advancing its flight on the held loop.
	c.Settle()

	var dismounted bool
	var landing servertest.SyncPositionView
	landed := false
	for range 40 {
		servertest.RunTicks(t, srv, 1)
		c.ReadTicks(srv, 0, func(id int32, data []byte) bool {
			switch id {
			case protocol.IDPlaySetPassengers:
				vehicle, riders := servertest.ReadPassengers(c, data)
				if vehicle == boat {
					dismounted = len(riders) == 0
				}
			case protocol.IDPlaySyncPosition:
				if !dismounted {
					t.Error("landing position arrived before the boat's empty Set Passengers")
				}
				landing = servertest.ReadSyncPosition(t, data)
				landed = true
			}
			return false
		})
		if landed {
			break
		}
	}
	if !landed {
		t.Fatal("the pearl did not land within 40 ticks")
	}
	if rider, _ := srv.BoatRider(boat); rider != "" {
		t.Errorf("boat rider after landing = %q, want none", rider)
	}
	if landing.X < 2 || landing.X > 15 || landing.Y < 64 || landing.Y > 65 || math.Abs(landing.Z-0.5) > 1 {
		t.Errorf("landing = %+v, want east of the boat at ground level", landing)
	}
	// A stale seat would put the thrower back on the boat on the next tick.
	servertest.RunTicks(t, srv, 2)
	if pos := srv.PlayerPosition("Thrower"); pos.X != landing.X || pos.Y != landing.Y || pos.Z != landing.Z {
		t.Errorf("thrower position = %+v, want the landing %+v", pos, landing)
	}
	if dismounted {
		c.ChatCommand("ride @s dismount")
		servertest.AwaitSystemChatSaying(t, c, "commands.ride.not_riding")
	}
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

// TestAPearlLandsItsOwnerAtThePreMovePositionNotTheWallFace throws a pearl
// level into a stone wall standing in its flight and checks where the
// landing puts the owner: at the pearl's position at the start of the tick
// it hit — vanilla's oldPosition — not the point on the wall's west face the
// flight was clipped at, which would stand the owner's 0.6-wide body in the
// wall.
// vanilla: ThrownEnderpearl.onHit
func TestAPearlLandsItsOwnerAtThePreMovePositionNotTheWallFace(t *testing.T) {
	t.Parallel()
	cfg := servertest.BowTestConfig()
	cfg.SimulationDistance = 2
	// The landing leaves the owner in the air, where a test client never
	// falls: the flight check would kick it 80 ticks on, inside a loaded
	// run's reading.
	cfg.AllowFlight = true
	srv, c, _ := servertest.JoinAs(t, cfg, "Thrower")
	servertest.GivePearls(t, srv)

	// A stone wall three blocks east, tall enough to meet a level throw.
	stone := servertest.MustState(t, "minecraft:stone", nil)
	for y := int32(64); y <= 66; y++ {
		for z := int32(-1); z <= 1; z++ {
			srv.SetBlock(3, y, z, stone)
		}
	}

	// Aimed level east: the pearl crosses the two blocks to the wall's west
	// face at x=3 and breaks on it. The second move is the same spot, so the
	// throw rides no movement.
	c.MoveTo(0.5, 64, 0.5, -90, 0)
	c.MoveTo(0.5, 64, 0.5, -90, 0)
	servertest.UseHeld(c, -90, 0)

	live := servertest.ReadPearlLive(c, srv, 60)
	pearls := servertest.SpawnsOf(live.Spawns, protocol.EntityTypeEnderPearl)
	if len(pearls) != 1 {
		t.Fatalf("the throw put %d ender pearls into the world, want one", len(pearls))
	}
	if len(live.Syncs) != 1 {
		t.Fatalf("the thrower was told %d teleports, want the landing's one", len(live.Syncs))
	}
	sync := live.Syncs[0]
	// At the start of the tick it hit, the pearl stands near x=2, a block
	// short of the face; the face itself would put the teleport at x≈3.
	if sync.X > 2.5 {
		t.Errorf("the teleport put the thrower at x=%v, want the pearl's pre-move place clear of the wall face at x=3", sync.X)
	}
	if sync.X < 1 {
		t.Errorf("the teleport put the thrower at x=%v, want them by the wall", sync.X)
	}
}

// TestAPearlThrowWaitsOutItsCooldown tries a second pearl throw while the
// first throw's cooldown still runs: nothing happens — no second pearl,
// no second throw sound, no second stack spent — until the twenty ticks
// have passed, when the throw works again. The respawn is a fresh body
// with the cooldowns of the dead one left behind, so a pearl thrown
// straight after is not still on the old one's; and a creative player
// spends no pearl while the cooldown's twenty ticks still answer for
// them.
func TestAPearlThrowWaitsOutItsCooldown(t *testing.T) {
	t.Parallel()
	cfg := servertest.BowTestConfig()
	cfg.SimulationDistance = 2
	srv, c, _ := servertest.JoinAs(t, cfg, "Thrower", servertest.OpsListing(t, "Thrower"))
	servertest.GivePearls(t, srv)

	c.MoveTo(0.5, 64, 0.5, -90, 45)
	servertest.UseHeld(c, -90, 45)
	// Two more uses right after, both inside the cooldown's twenty ticks:
	// neither throws, and neither spends a pearl either.
	c.MoveTo(0.5, 64, 0.5, -90, 45)
	servertest.UseHeld(c, -90, 45)
	c.MoveTo(0.5, 64, 0.5, -90, 45)
	servertest.UseHeld(c, -90, 45)

	live := servertest.ReadPearlLive(c, srv, 40)
	if n := len(servertest.SpawnsOf(live.Spawns, protocol.EntityTypeEnderPearl)); n != 1 {
		t.Fatalf("the throws put %d pearls into the world, want the first one alone", n)
	}
	throws := 0
	for _, s := range live.Sounds {
		if s.Name == "minecraft:entity.ender_pearl.throw" {
			throws++
		}
	}
	if throws != 1 {
		t.Errorf("the trick heard %d throw sounds, want the first one alone", throws)
	}
	if left := srv.InventorySlot("Thrower", 36); left.Count != 15 {
		t.Errorf("the hotbar holds %d pearls, want the fifteen the landing outside the cooldown refused to spend", left.Count)
	}

	// The twenty ticks having run out (the reading spent more than a
	// second of them), the throw works again.
	c.MoveTo(0.5, 64, 0.5, -90, 45)
	servertest.UseHeld(c, -90, 45)
	live = servertest.ReadPearlLive(c, srv, 40)
	if n := len(servertest.SpawnsOf(live.Spawns, protocol.EntityTypeEnderPearl)); n != 1 {
		t.Fatalf("the throw after the cooldown put %d pearls into the world, want one", n)
	}
	if left := srv.InventorySlot("Thrower", 36); left.Count != 14 {
		t.Errorf("the hotbar holds %d pearls after the new throw, want the fourteen", left.Count)
	}

	// The death and the respawn: the fresh body carries no old cooldown,
	// so a pearl thrown straight away flies, and its own twenty ticks
	// start it over. The death is taken well out of the spawn the respawn
	// returns to — the position settled first, so the dropped stack lands
	// where the throw was made and the respawned player never walks back
	// into it — keeping the counting slot honest.
	c.MoveTo(33.5, 64, 0.5, -90, 45)
	servertest.WaitFor(t, srv, 200, func() bool { return srv.PlayerPosition("Thrower").X > 33 })
	servertest.UseHeld(c, -90, 45)
	if !srv.Hurt("Thrower", 100) {
		t.Fatal("the killing blow did not land")
	}
	// The last flight's spawn and its vanish at the death, drained before
	// the respawn's own reading.
	servertest.ReadPearlLive(c, srv, 6)
	c.Send(protocol.IDPlayClientCommandIn, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.ClientCommandPerformRespawn)
	})
	if _, ok := servertest.ReadUntil(t, c, servertest.ExpectTimeout, protocol.IDPlayRespawn); !ok {
		t.Fatal("no respawn")
	}
	c.MarkLoaded()
	servertest.GivePearls(t, srv)
	c.MoveTo(0.5, 64, 0.5, -90, 45)
	servertest.UseHeld(c, -90, 45)
	live = servertest.ReadPearlLive(c, srv, 40)
	if n := len(servertest.SpawnsOf(live.Spawns, protocol.EntityTypeEnderPearl)); n != 1 {
		t.Fatalf("the throw after the respawn put %d pearls into the world, want the fresh cooldown refusing none", n)
	}
	if left := srv.InventorySlot("Thrower", 36); left.Count != 15 {
		t.Errorf("the hotbar holds %d pearls after the respawn's throw, want the fifteen one throw left", left.Count)
	}

	// The creative hand spends nothing, the cooldown answering all the
	// same: one throw per twenty ticks however the pearls arrive.
	c.ChatCommand("gamemode creative")
	c.ExpectSystemChat("commands.gamemode.success.self", false, protocol.Text{Translate: "gameMode.creative"})
	for range 2 {
		c.MoveTo(0.5, 64, 0.5, -90, 45)
		servertest.UseHeld(c, -90, 45)
	}
	live = servertest.ReadPearlLive(c, srv, 40)
	if n := len(servertest.SpawnsOf(live.Spawns, protocol.EntityTypeEnderPearl)); n != 1 {
		t.Fatalf("the creative throws put %d pearls into the world, want the first one alone of the still-cooling second", n)
	}
	if left := srv.InventorySlot("Thrower", 36); left.Count != 15 {
		t.Errorf("the hotbar holds %d pearls after the creative throw, want the fifteen the survival throw left", left.Count)
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

// TestAPearlVanishesWhenItsDyingOwnerIsToldToLoseIt throws a pearl
// straight up — fifty ticks of flight before it comes back down — and
// kills the thrower while it is away: by the
// ender_pearls_vanish_on_death gamerule (true as vanilla leaves it), the
// pearl is taken out of the world mid-flight, with none of a landing's
// telling to follow: no teleport of the dead body, no hurt, no sound.
func TestAPearlVanishesWhenItsDyingOwnerIsToldToLoseIt(t *testing.T) {
	t.Parallel()
	cfg := servertest.BowTestConfig()
	cfg.AllowFlight = true
	srv, c, _ := servertest.JoinAs(t, cfg, "Thrower")
	servertest.GivePearls(t, srv)
	// Held from the throw, so the death lands mid-flight however slow the
	// machine is.
	servertest.HoldTicks(t, srv)

	// Straight up: the flight is a hundred ticks of rising and falling.
	c.MoveTo(0.5, 64, 0.5, -90, -80)
	servertest.UseHeld(c, -90, -80)

	live := servertest.ReadPearlLive(c, srv, 14)
	pearls := servertest.SpawnsOf(live.Spawns, protocol.EntityTypeEnderPearl)
	if len(pearls) != 1 {
		t.Fatalf("the throw put %d ender pearls into the world, want one", len(pearls))
	}
	pearl := pearls[0]

	// The death, while the pearl is still well above the world.
	if !srv.Hurt("Thrower", 100) {
		t.Fatal("the killing blow did not land")
	}

	live = servertest.ReadPearlLive(c, srv, 40)
	if !live.RemovedOnce(pearl.EntityID) {
		t.Error("the pearl was not taken out of the world exactly once after the owner's death")
	}
	if len(live.Syncs) != 0 {
		t.Errorf("the dead thrower was told %d teleports, want none", len(live.Syncs))
	}
	pearlDamage := registry.MustID("minecraft:damage_type", "minecraft:ender_pearl")
	for _, d := range live.DamageEvents {
		if d.DamageType == pearlDamage {
			t.Errorf("the vanished pearl dealt a landing's ender_pearl hurt to entity %d", d.Id)
		}
	}
	for _, s := range live.Sounds {
		if s.Name == "minecraft:entity.player.teleport" {
			t.Error("a teleport sound played with nobody teleported")
		}
	}
}

// TestAPearlKeptPastADeathLandsLeavingItsOwnerBehind throws a pearl on a
// long flat flight, turns the vanish gamerule off, and kills the thrower
// mid-flight: the pearl flies on, held together with the chunk it crosses
// that no player keeps (the chunk hold read through the flight), lands far
// away with the landing's own telling to no one — the dead owner left
// where they fell — and gives the chunk up again once it is done.
func TestAPearlKeptPastADeathLandsLeavingItsOwnerBehind(t *testing.T) {
	t.Parallel()
	cfg := servertest.BowTestConfig()
	cfg.AllowFlight = true
	// The load radius a player holds is the simulation distance, so both
	// of the distances come down to one: the chunks two or more east are
	// then held by the pearl alone or by no one.
	cfg.ViewDistance = 1
	cfg.SimulationDistance = 1
	srv, c, _ := servertest.JoinAs(t, cfg, "Thrower")
	srv.SetGameRule("ender_pearls_vanish_on_death", false)
	servertest.GivePearls(t, srv)
	// Held from the throw, so the death lands mid-flight however slow the
	// machine is.
	servertest.HoldTicks(t, srv)

	// A flight tens of blocks long: up thirty degrees from the level, the
	// pearl crosses chunks no player holds (view distance one keeps just
	// the chunks about the spawn) before it comes down to the ground.
	c.MoveTo(0.5, 64, 0.5, -90, -30)
	servertest.UseHeld(c, -90, -30)

	live := servertest.ReadPearlLive(c, srv, 14)
	pearls := servertest.SpawnsOf(live.Spawns, protocol.EntityTypeEnderPearl)
	if len(pearls) != 1 {
		t.Fatalf("the throw put %d ender pearls into the world, want one", len(pearls))
	}
	pearl := pearls[0]

	// The death mid-flight; by the turned-off gamerule the pearl is kept.
	if !srv.Hurt("Thrower", 100) {
		t.Fatal("the killing blow did not land")
	}

	// The flight over the unloaded chunks: the pearl holds the chunk it
	// is over, and the hold follows it out past the spawn's own chunks.
	// readPearlLive left nothing unread, so the polls below watch the
	// world rather than the wire.
	servertest.UntilTicks(t, srv, 80, "a chunk past the spawn's held by the pearl alone", func() bool {
		for chunk := int32(2); chunk <= 7; chunk++ {
			if srv.ChunkLoadedInDimension(world.DimensionName, chunk*16, 0) {
				return true
			}
		}
		return false
	})

	// The landing far away: the pearl is removed, and nothing of a
	// teleport reaches the dead body.
	servertest.AwaitTicks(t, srv, 2)
	if pos := srv.PlayerPosition("Thrower"); pos.X > 4 || pos.Z > 4 {
		t.Errorf("the dead thrower stands at (%v, %v), want the death place at the throw", pos.X, pos.Z)
	}
	// The observer lost the pearl's track as it left their range, so the
	// landing's own removal may already have been preceded by the one that
	// leaving the range sent. What has to hold is that the pearl is out of
	// the world once its flight is over, and that the dead owner is told
	// nothing of a landing.
	live = servertest.ReadPearlLive(c, srv, 40)
	if thrown := srv.ThrownInDimension(world.DimensionName); len(thrown) != 0 {
		t.Errorf("the pearl was still in the world after its landing: %+v", thrown)
	}
	if n := live.RemovalCount(pearl.EntityID); n > 1 {
		t.Errorf("the pearl's removal reached the dead thrower %d times, want at most the one of leaving their range", n)
	}
	if len(live.Syncs) != 0 {
		t.Errorf("the dead thrower was told %d teleports, want none", len(live.Syncs))
	}
	for _, s := range live.Sounds {
		if s.Name == "minecraft:entity.player.teleport" {
			t.Error("a teleport sound played with nobody teleported")
		}
	}

	// The hold given up: every chunk past the spawn's is unloaded again,
	// the players (the dead thrower among them) holding only what they
	// stand on.
	servertest.UntilTicks(t, srv, 40, "every chunk past the spawn's unloaded again", func() bool {
		for chunk := int32(2); chunk <= 7; chunk++ {
			if srv.ChunkLoadedInDimension(world.DimensionName, chunk*16, 0) {
				return false
			}
		}
		return true
	})
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
	servertest.WaitFor(t, srv, 400, func() bool {
		return len(srv.ItemsInDimension(world.NetherName)) == 1 &&
			len(srv.ItemsInDimension(world.DimensionName)) == 0
	})
	spot := srv.ItemsInDimension(world.NetherName)[0]

	// The mover in the nether the arrival is shown to.
	mover := connect().Join("Arrival")
	srv.MovePlayerToDimension("Arrival", world.NetherName,
		float64(int(spot.X)), float64(int(spot.Y)), float64(int(spot.Z))+2)
	if _, ok := servertest.ReadUntilViewSent(t, mover); !ok {
		t.Fatal("the nether never arrived for the mover")
	}

	// The pearl, thrown level into the portal from a block south of it.
	srv.GiveSlot("Thrower", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:ender_pearl"), Count: 16})
	thrower.MoveTo(5.0, servertest.PortalFloorY, 6.0, 180, 0)
	// The same spot again, the movement the throw rides nought.
	thrower.MoveTo(5.0, servertest.PortalFloorY, 6.0, 180, 0)
	servertest.UseHeld(thrower, 180, 0)
	thrower.ExpectThrownSpawn(srv, protocol.EntityTypeEnderPearl)

	// The crossing and the far-side landing, as the nether's standing
	// player is told: the arrival's own spawn, then its flight ending. The
	// thrower's own stream is drained throughout — its mailbox must not back
	// up behind the Respawn that carries it across.
	arrival := awaitPearlSpawn(t, srv, mover)
	crossed, removed := false, false
	servertest.UntilTicks(t, srv, 400, "the thrower crossing and the pearl's landing reaching the nether watcher", func() bool {
		thrower.ReadTicks(srv, 0, func(int32, []byte) bool { return false })
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
		return crossed && removed
	})
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

// TestAPearlCarriesItsTagsAcrossAPortal tags a pearl in flight in the
// overworld and lets the lit portal carry it across: the twin the crossing
// raises in the nether answers a selector run there by the tag it was given
// on the other side. The crossing itself the tests above hold; this one the
// tags.
// vanilla: Entity.teleportCrossDimension
func TestAPearlCarriesItsTagsAcrossAPortal(t *testing.T) {
	t.Parallel()
	cfg := servertest.CrossingTestConfig(t)
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Thrower", "Arrival"))

	thrower := connect().Join("Thrower")

	servertest.LightPortal(t, srv, thrower)

	// A stone dropped through first builds the nether portal the pearl is
	// to arrive by.
	stone, ok := registry.ItemID("minecraft:stone")
	if !ok {
		t.Fatal("the item registry has no minecraft:stone")
	}
	thrower.SetCreativeSlot(36, protocol.ItemStack{ItemID: stone, Count: 1})
	thrower.MoveTo(5.0, servertest.PortalFloorY, 5.5, 180, 0)
	servertest.DropHeld(thrower, 5, servertest.PortalFloorY, 5)
	servertest.WaitFor(t, srv, 400, func() bool {
		return len(srv.ItemsInDimension(world.NetherName)) == 1 &&
			len(srv.ItemsInDimension(world.DimensionName)) == 0
	})
	spot := srv.ItemsInDimension(world.NetherName)[0]

	// The mover in the nether the arrival is shown to.
	mover := connect().Join("Arrival")
	srv.MovePlayerToDimension("Arrival", world.NetherName,
		float64(int(spot.X)), float64(int(spot.Y)), float64(int(spot.Z))+2)
	if _, ok := servertest.ReadUntilViewSent(t, mover); !ok {
		t.Fatal("the nether never arrived for the mover")
	}

	// The loop is stopped and the crossing stepped by hand, so the twin
	// stays in the nether to be read however loaded the run is: a pearl in
	// real-time flight lands before the read under -race.
	srv.StopTick()

	// The throw, the tag written in behind it: the add runs on the same
	// connection, ahead of the tick that carries the pearl across, so the
	// tag is on the body before it leaves the overworld.
	srv.GiveSlot("Thrower", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:ender_pearl"), Count: 16})
	thrower.MoveTo(5.0, servertest.PortalFloorY, 6.0, 180, 0)
	// The same spot again, the movement the throw rides nought.
	thrower.MoveTo(5.0, servertest.PortalFloorY, 6.0, 180, 0)
	servertest.UseHeld(thrower, 180, 0)
	thrower.ChatCommand("tag @e[type=minecraft:ender_pearl] add marked")
	pearl := thrower.ExpectThrownSpawn(srv, protocol.EntityTypeEnderPearl)
	servertest.AwaitCommandFeedbackText(t, thrower, "commands.tag.add.success.single",
		protocol.Text{Text: "marked"},
		servertest.EntityDisplayComponent("minecraft:ender_pearl", "entity.minecraft.ender_pearl", pearl.Uuid))

	// The flight through the portal, one tick at a time: tickThrowables
	// carries the portal pass, so the body crosses within a couple of ticks
	// and then stays put, still tagged, for the read in the nether.
	for i := 0; i < 40 && len(srv.ThrownInDimension(world.NetherName)) == 0; i++ {
		srv.TickEntities(1)
	}
	if got := srv.ThrownInDimension(world.NetherName); len(got) != 1 {
		t.Fatalf("the nether holds %d thrown bodies, want the one pearl that crossed", len(got))
	}

	// The twin in the nether is the tagged body still, read where it flies
	// by the player standing there.
	awaitPearlSpawn(t, srv, mover)
	mover.ChatCommand("tag @e[type=minecraft:ender_pearl,tag=marked] list")
	servertest.AwaitCommandFeedbackText(t, mover, "commands.tag.list.single.success",
		servertest.EntityDisplayComponent("minecraft:ender_pearl", "entity.minecraft.ender_pearl", pearl.Uuid),
		servertest.TagCountText(1), servertest.GreenTagText("marked"))
}

// TestAPearlLandingInAnotherDimensionDoesNotStallTheTick holds the crossing
// thrower's mailbox writer before the pearl lands in the nether: the
// crossing must leave the tick for its own goroutine, as every other
// dimension change does, so the loop keeps ticking while the crossing waits
// on the writer — the stall a socket that stopped reading would make. The
// landing's removal reaching the nether watcher is what says the tick got
// past the pearl itself.
func TestAPearlLandingInAnotherDimensionDoesNotStallTheTick(t *testing.T) {
	t.Parallel()
	cfg := servertest.CrossingTestConfig(t)
	srv, connect := servertest.Start(t, cfg)

	thrower := connect().Join("Thrower")

	servertest.LightPortal(t, srv, thrower)

	// A stone dropped through first builds the nether portal the pearl is
	// to arrive by.
	stone, ok := registry.ItemID("minecraft:stone")
	if !ok {
		t.Fatal("the item registry has no minecraft:stone")
	}
	thrower.SetCreativeSlot(36, protocol.ItemStack{ItemID: stone, Count: 1})
	thrower.MoveTo(5.0, servertest.PortalFloorY, 5.5, 180, 0)
	servertest.DropHeld(thrower, 5, servertest.PortalFloorY, 5)
	servertest.WaitFor(t, srv, 400, func() bool {
		return len(srv.ItemsInDimension(world.NetherName)) == 1 &&
			len(srv.ItemsInDimension(world.DimensionName)) == 0
	})
	spot := srv.ItemsInDimension(world.NetherName)[0]

	// The mover in the nether the arrival is shown to.
	mover := connect().Join("Arrival")
	srv.MovePlayerToDimension("Arrival", world.NetherName,
		float64(int(spot.X)), float64(int(spot.Y)), float64(int(spot.Z))+2)
	if _, ok := servertest.ReadUntilViewSent(t, mover); !ok {
		t.Fatal("the nether never arrived for the mover")
	}

	// The pearl, thrown level into the portal.
	srv.GiveSlot("Thrower", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:ender_pearl"), Count: 16})
	thrower.MoveTo(5.0, servertest.PortalFloorY, 6.0, 180, 0)
	thrower.MoveTo(5.0, servertest.PortalFloorY, 6.0, 180, 0)
	servertest.HoldTicks(t, srv)
	servertest.UseHeld(thrower, 180, 0)
	awaitPearlSpawn(t, srv, thrower)

	// Hold the thrower's writer before the pearl lands: the crossing's own
	// send-off drains through it, so a crossing run on the tick would park
	// the whole loop here.
	// The loop is held from the throw on, or a loaded machine lets the
	// pearl land and the crossing run before the hold is on.
	release := srv.HoldMailbox("Thrower")
	t.Cleanup(release)

	arrival := awaitPearlSpawn(t, srv, mover)

	// The landing tells the nether watcher the pearl is gone; nothing of
	// that reaches the tick's tail if the crossing is still on it.
	// The thrower's dimension is read the moment the removal arrives: the
	// crossing's own goroutine is only held by the writer from then on, and a
	// check a tick later would race it.
	dimAtRemoval := ""
	removed := mover.ReadTicks(srv, 200, func(gotID int32, data []byte) bool {
		if gotID != protocol.IDPlayRemoveEntities {
			return false
		}
		r := protocol.NewReader(data)
		for range r.ReadVarInt() {
			if r.ReadVarInt() == arrival.EntityID {
				dimAtRemoval = srv.PlayerDimension("Thrower")
				return true
			}
		}
		return false
	})
	if !removed {
		t.Fatal("the landing never reached the tick while the crossing's writer was held")
	}
	// The crossing is parked on the held writer, not on the tick: the
	// thrower has not moved and the loop runs on.
	if dimAtRemoval != world.DimensionName {
		t.Errorf("the thrower was already in %q while the writer was held", dimAtRemoval)
	}
	servertest.AwaitTicks(t, srv, 5)

	release()
	servertest.WaitFor(t, srv, 400, func() bool {
		return srv.PlayerDimension("Thrower") == world.NetherName
	})
}

// TestAPearlDoesNotCarryAnOwnerWhoDiesBeforeTheCrossing plants a death in
// the window between the landing's tick check and the crossing's goroutine:
// the dead owner is left where they fell rather than carried to the pearl's
// dimension.
func TestAPearlDoesNotCarryAnOwnerWhoDiesBeforeTheCrossing(t *testing.T) {
	t.Parallel()
	cfg := servertest.CrossingTestConfig(t)

	reached := make(chan struct{})
	proceed := make(chan struct{})
	var once sync.Once
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Thrower"), func(s *server.Server) {
		s.SetPearlCarryHook(func() {
			once.Do(func() { close(reached) })
			<-proceed
		})
	})

	thrower := connect().Join("Thrower")

	servertest.LightPortal(t, srv, thrower)

	// A stone dropped through first builds the nether portal the pearl is
	// to arrive by.
	stone, ok := registry.ItemID("minecraft:stone")
	if !ok {
		t.Fatal("the item registry has no minecraft:stone")
	}
	thrower.SetCreativeSlot(36, protocol.ItemStack{ItemID: stone, Count: 1})
	thrower.MoveTo(5.0, servertest.PortalFloorY, 5.5, 180, 0)
	servertest.DropHeld(thrower, 5, servertest.PortalFloorY, 5)
	servertest.WaitFor(t, srv, 400, func() bool {
		return len(srv.ItemsInDimension(world.NetherName)) == 1 &&
			len(srv.ItemsInDimension(world.DimensionName)) == 0
	})
	spot := srv.ItemsInDimension(world.NetherName)[0]

	// The mover in the nether the arrival is shown to.
	mover := connect().Join("Arrival")
	srv.MovePlayerToDimension("Arrival", world.NetherName,
		float64(int(spot.X)), float64(int(spot.Y)), float64(int(spot.Z))+2)
	if _, ok := servertest.ReadUntilViewSent(t, mover); !ok {
		t.Fatal("the nether never arrived for the mover")
	}

	// The pearl, thrown level into the portal.
	srv.GiveSlot("Thrower", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:ender_pearl"), Count: 16})
	thrower.MoveTo(5.0, servertest.PortalFloorY, 6.0, 180, 0)
	thrower.MoveTo(5.0, servertest.PortalFloorY, 6.0, 180, 0)
	servertest.UseHeld(thrower, 180, 0)
	thrower.ExpectThrownSpawn(srv, protocol.EntityTypeEnderPearl)

	// The crossing is held at its start; the owner is killed in the window
	// before it proceeds. A blow lands only outside creative.
	select {
	case <-reached:
	case <-time.After(20 * time.Second):
		t.Fatal("the pearl never reached its crossing")
	}
	thrower.ChatCommand("gamemode survival")
	thrower.ExpectSystemChat("commands.gamemode.success.self", false, protocol.Text{Translate: "gameMode.survival"})
	if !srv.Hurt("Thrower", 100) {
		t.Fatal("the killing blow did not land")
	}
	close(proceed)

	// The goroutine runs on; the dead owner stays in the overworld.
	servertest.AwaitTicks(t, srv, 10)
	if dim := srv.PlayerDimension("Thrower"); dim != world.DimensionName {
		t.Errorf("the dead owner was carried to %q, want left in %q", dim, world.DimensionName)
	}
}

// TestAnEnderPearlStaysInTheEndUntilTheCreditsAreSeen throws a pearl through
// the end's own portal: a thrower who has not seen the credits keeps the
// pearl in the end with them, and the same throw once they have crosses the
// pearl out, its landing carrying the owner home.
// vanilla: ThrownEnderpearl.canTeleport
func TestAnEnderPearlStaysInTheEndUntilTheCreditsAreSeen(t *testing.T) {
	t.Parallel()
	cfg := servertest.CrossingTestConfig(t)
	srv, c := servertest.Join(t, cfg, "Thrower")

	arrival := world.EndArrival()
	srv.MovePlayerToDimension("Thrower", world.EndName,
		float64(arrival.X), float64(arrival.Y), float64(arrival.Z))
	// Drain the arrival through its chunks, the crossing's own Respawn among
	// them, so a later wait does not take it for a pearl's crossing.
	if _, ok := servertest.ReadUntilViewSent(t, c); !ok {
		t.Fatal("the end never arrived")
	}

	// Two columns of end portal in the flight's path — the pearl leaves the
	// hand above the feet cell — so the throw meets the portal whatever its
	// exact landing cell. The feet cell stays free: a player standing in an
	// end portal would be shown the credits and marked seen before the
	// throw, which is the state the pearl is meant to be refused in.
	endPortal := srv.EndPortalState()
	for _, dz := range []int32{0, 1} {
		for dy := int32(1); dy <= 2; dy++ {
			if !srv.SetBlockInDimension(world.EndName, arrival.X, arrival.Y+dy, arrival.Z+dz, endPortal) {
				t.Fatalf("placing the end's own end portal at z+%d, y+%d failed", dz, dy)
			}
		}
	}

	// The pearl, thrown north through the portal from the platform's edge.
	srv.GiveSlot("Thrower", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:ender_pearl"), Count: 16})
	aim := func() {
		c.MoveTo(float64(arrival.X)+0.5, float64(arrival.Y), float64(arrival.Z)+2.5, 180, 0)
		c.MoveTo(float64(arrival.X)+0.5, float64(arrival.Y), float64(arrival.Z)+2.5, 180, 0)
		servertest.UseHeld(c, 180, 0)
		c.ExpectThrownSpawn(srv, protocol.EntityTypeEnderPearl)
	}

	// Unseen: the pearl is refused the crossing, so its flight stays in the
	// end and the owner is never carried out.
	aim()
	crossed := c.ReadTicksSkipping(srv, 60, nil, func(id int32, data []byte) bool {
		return id == protocol.IDPlayRespawn
	})
	if crossed || srv.PlayerDimension("Thrower") != world.EndName {
		t.Errorf("an unseen owner's pearl crossed out of the end, want it held in %q", world.EndName)
	}

	// Seen: the same throw crosses the pearl, and its landing carries the
	// owner home.
	srv.SetSeenCredits("Thrower", true)
	servertest.AwaitTicks(t, srv, 20)
	aim()
	servertest.UntilTicks(t, srv, 400, "the seen owner's pearl carrying them home", func() bool {
		c.ReadTicks(srv, 0, func(int32, []byte) bool { return false })
		return srv.PlayerDimension("Thrower") == world.DimensionName
	})
	if got := srv.PlayerDimension("Thrower"); got != world.DimensionName {
		t.Errorf("a seen owner's pearl left them in %q, want carried to %q", got, world.DimensionName)
	}
}

// TestAnEnderPearlWithAnOfflineOwnerCrossesTheEnd spawns an ender pearl
// owned by no online player inside the end's own portal: vanilla's
// canTeleport asks the credits only of a ServerPlayer owner, so an offline
// owner's pearl crosses out where an unseen owner's is held in.
// vanilla: ThrownEnderpearl.canTeleport
func TestAnEnderPearlWithAnOfflineOwnerCrossesTheEnd(t *testing.T) {
	t.Parallel()
	cfg := servertest.CrossingTestConfig(t)
	srv, _ := servertest.Start(t, cfg)

	arrival := world.EndArrival()
	release := srv.HoldChunkInDimension(world.EndName, arrival.X, arrival.Z)
	defer release()
	endPortal := srv.EndPortalState()
	for _, dz := range []int32{0, 1} {
		for dy := int32(0); dy <= 2; dy++ {
			if !srv.SetBlockInDimension(world.EndName, arrival.X, arrival.Y+dy, arrival.Z+dz, endPortal) {
				t.Fatalf("placing the end's own end portal at z+%d, y+%d failed", dz, dy)
			}
		}
	}

	// The pearl, spawned inside the portal with the identity of no online
	// player: it must cross the end out rather than being held for a
	// missing owner.
	if id := srv.ThrowEnderPearlInDimension(world.EndName,
		float64(arrival.X)+0.5, float64(arrival.Y)+0.5, float64(arrival.Z)+0.5,
		180, 0, 0, uuid.New(), 1); id == 0 {
		t.Fatal("the offline owner's pearl never came into the end")
	}
	if got := len(srv.ThrownInDimension(world.EndName)); got != 1 {
		t.Fatalf("the end holds %d thrown bodies after the throw, want 1", got)
	}
	servertest.WaitFor(t, srv, 400, func() bool {
		return len(srv.ThrownInDimension(world.EndName)) == 0
	})
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
