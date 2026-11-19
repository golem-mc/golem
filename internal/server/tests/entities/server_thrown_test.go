package entities

import (
	"math"
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/registry"
)

// The thrown projectiles as the wire carries them, the way the arrow's own
// are: a client that uses an egg or a snowball is told the flight's whole
// story — the sound of the throw, the Spawn Entity the body arrives as, the
// item the metadata says it is, the tracked entity's first pass, and the
// break's event and removal. The shapes here are the ones the vanilla
// server's own traffic was read off, the captures kept beside the tools'
// notes.

// TestAThrownEggFliesAndBreaksTheWayTheWireTellsIt uses an egg aimed down
// at the ground and checks the whole stream against the shapes the vanilla
// server's own traffic was read off: the throw's sound at the thrower's
// feet, the spawn at the eye less its tenth named for the thrower, the
// item one to a stack, the tracked entity's first pass the tick after, and
// the break's event and removal where the flight ended. The stack the
// survival hand threw from is one shorter for it.
func TestAThrownEggFliesAndBreaksTheWayTheWireTellsIt(t *testing.T) {
	t.Parallel()
	// The simulation distance is taken down to the render distance the
	// client asks for: the flight this test reads is a per-tick stream, and
	// a world filling the simulation distance behind the join would spend
	// ticks the flight's arithmetic assumes are even.
	cfg := servertest.BowTestConfig()
	cfg.SimulationDistance = 2
	srv, c, playerID := servertest.JoinAs(t, cfg, "Thrower")
	srv.GiveSlot("Thrower", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:egg"), Count: 16})

	// A throw pitched steeply down: the flight is two ticks of falling
	// and no more, which keeps the tenth-tick rhythm out of the stream
	// and the break close behind the first pass.
	c.MoveTo(0.5, 64, 0.5, -90, 45)
	servertest.UseHeld(c, -90, 45)

	traffic := servertest.ReadThrownTraffic(c, srv, 60)
	egg := traffic.SpawnOfType(protocol.EntityTypeEgg)
	if egg == nil {
		t.Fatal("no egg came into the world for the throw")
	}
	if egg.OwnerID != playerID {
		t.Errorf("the egg's data field names entity %d, want the thrower's %d", egg.OwnerID, playerID)
	}
	// The birth: at the thrower's eye less its tenth, over their feet,
	// and moving down and east at the power of one and a half.
	if d := egg.Y - (64 + 1.62 - 0.1); d < -0.01 || d > 0.01 {
		t.Errorf("the egg was born at y=%v, want the thrower's eye less its tenth", egg.Y)
	}
	if d := egg.Z - 0.5; d < -0.01 || d > 0.01 {
		t.Errorf("the egg was born at z=%v, want the thrower's own", egg.Z)
	}
	// The aim carries vanilla's inaccuracy, a triangle of up to 0.0172275
	// on each axis of the unit heading before the power scales it, which
	// spreads the square of one and a half from about 2.12 to 2.39.
	speed := egg.Vx*egg.Vx + egg.Vy*egg.Vy + egg.Vz*egg.Vz
	if speed < 2.1 || speed > 2.4 {
		t.Errorf("the egg flies at %v blocks a tick squared, want the one and a half's square's neighbourhood", speed)
	}
	if egg.Vy >= 0 {
		t.Errorf("a throw pitched down flew up: vy=%v", egg.Vy)
	}

	// The sound of the throw: the egg's own, at the thrower's feet, half
	// of a block loud, at the pitch the roll of two fifths past eight
	// tenths divides four tenths into — between the third and the half.
	var throwSound *servertest.ThrownSound
	for i := range traffic.Sounds {
		if traffic.Sounds[i].Name == "minecraft:entity.egg.throw" {
			throwSound = &traffic.Sounds[i]
			break
		}
	}
	if throwSound == nil {
		t.Fatal("no egg throw sound was played")
	}
	if throwSound.Category != protocol.SoundCategoryPlayer {
		t.Errorf("the throw's sound category = %d, want the players'", throwSound.Category)
	}
	if throwSound.X != 0.5 || throwSound.Y != 64 || throwSound.Z != 0.5 {
		t.Errorf("the throw's sound played from (%v %v %v), want the thrower's feet", throwSound.X, throwSound.Y, throwSound.Z)
	}
	if throwSound.Volume != 0.5 {
		t.Errorf("the throw's sound volume = %v, want the vanilla half", throwSound.Volume)
	}
	if throwSound.Pitch < 0.3 || throwSound.Pitch > 0.51 {
		t.Errorf("the throw's sound pitch = %v, want the roll's third to a half", throwSound.Pitch)
	}

	// The item the egg is a stack of: one egg, twice — the spawn's
	// telling and the tracked entity's first pass sending it again.
	metas := traffic.MetasOf(egg.EntityID)
	if len(metas) != 2 {
		t.Fatalf("the egg's metadata arrived %d times, want the spawn's and the first pass's", len(metas))
	}
	for i, m := range metas {
		if m.Count != 1 || m.Item != servertest.MustItem("minecraft:egg") {
			t.Errorf("metadata %d carried %d of item %d, want one egg", i, m.Count, m.Item)
		}
	}

	// The tracked entity's first pass: a position packet with no distance
	// in it, standing on nothing.
	positions := traffic.PositionsOf(egg.EntityID)
	if len(positions) != 1 {
		t.Fatalf("the egg's position packets = %d, want the first pass's one", len(positions))
	}
	if p := positions[0]; p.Dx != 0 || p.Dy != 0 || p.Dz != 0 || p.OnGround {
		t.Errorf("the first pass carried deltas (%d %d %d) on ground %v, want the noughts and off the ground", p.Dx, p.Dy, p.Dz, p.OnGround)
	}

	// The break: the event the egg's own break plays and the body out of
	// the world, once each.
	events := traffic.EventsOf(egg.EntityID)
	if len(events) != 1 || events[0].Status != protocol.EntityStatusDeath {
		t.Fatalf("the egg's entity events = %#v, want the break's one of status three", events)
	}
	if !traffic.RemovedOnce(egg.EntityID) {
		t.Error("the egg was not taken out of the world exactly once")
	}

	// The survival hand paid one egg of its sixteen.
	if left := srv.InventorySlot("Thrower", 36); left.Count != 15 {
		t.Errorf("the hotbar holds %d eggs after the throw, want the fifteen one throw left", left.Count)
	}
}

// TestAnEggBreaksIntoChicksShownAsBabies throws the egg whose dice hatch
// one chick and reads the chick's arrival whole: a chicken's Spawn Entity
// where the egg broke, and the metadata the spawn carries saying what the
// chick is — the baby flag's own index, a boolean set, the smaller box the
// metadata tells the client the body is.
func TestAnEggBreaksIntoChicksShownAsBabies(t *testing.T) {
	t.Parallel()
	srv, c, playerID := servertest.JoinAs(t, servertest.BowTestConfig(), "Hatcher")

	// A seed whose first roll of eight lands the hatch and whose next
	// roll of thirty-two passes: one chick, the second roll spent. The
	// dice are the body's own, and the throw's spread spent six draws of
	// them before the break's rolls asked, so the search draws those six
	// first, the way the server's throw does.
	var oneSeed int64 = -1
	for seed := int64(1); seed < 100000; seed++ {
		rng := rand.New(rand.NewSource(seed))
		for range 6 {
			rng.Float64()
		}
		if rng.Intn(8) == 0 && rng.Intn(32) != 0 {
			oneSeed = seed
			break
		}
	}
	if oneSeed < 0 {
		t.Fatal("no seed under a hundred thousand hatched one chick")
	}

	// The throw straight down at the thrower's feet, the way the flight
	// test throws, so the break is close behind the first pass.
	srv.ThrowThrown(entity.ThrownEgg, 0.5, 64+1.62-0.1, 0.5, -90, 45, playerID, oneSeed)

	// The stream for the break and what follows: one chicken's spawn and
	// its state, the baby among the fields.
	var (
		chick   *servertest.ThrownSpawn
		chickID int32
		baby    bool
		hasMeta bool
	)
	c.ReadTicks(srv, 60, func(gotID int32, data []byte) bool {
		switch gotID {
		case protocol.IDPlaySpawnEntity:
			spawn := servertest.ReadThrownSpawn(c, data)
			if spawn.EntityType == protocol.EntityTypeChicken && chick == nil {
				chick = &spawn
				chickID = spawn.EntityID
			}
		case protocol.IDPlayEntityMetadata:
			r := protocol.NewReader(data)
			if id := r.ReadVarInt(); chick == nil || id != chickID {
				return false
			}
			for r.Err() == nil {
				index := r.ReadU8()
				if index == protocol.MetadataEnd {
					break
				}
				fieldType := r.ReadVarInt()
				if index == protocol.MetadataIndexBaby {
					if fieldType != protocol.MetadataBoolean {
						t.Fatalf("the chick's baby field type = %d, want %d (a boolean)", fieldType, protocol.MetadataBoolean)
					}
					baby = r.ReadU8() != 0
					hasMeta = true
					continue
				}
				servertest.SkipMetadataField(c, r, fieldType)
			}
		}
		return false
	})
	if chick == nil {
		t.Fatal("the egg's break hatched no chicken the client was shown")
	}
	if !hasMeta {
		t.Fatal("the chick's metadata never said what it is")
	}
	if !baby {
		t.Error("the chick's metadata carried no baby flag")
	}
	// The hatch is where the egg broke — a pitched-down throw's two ticks
	// of flight land it a couple of blocks from the feet, the spread the
	// throw's dice spent riding on top of that.
	if d := chick.X - 0.5; d < -2.5 || d > 2.5 {
		t.Errorf("the chick hatched at x=%v, want the break's neighbourhood of the feet", chick.X)
	}
}

// TestAThrowRidesItsThrower throws four snowballs east from the same spot:
// one standing, one released right after a reported step east, one after a
// reported rise with the feet on the ground, and one after the same rise
// off it. The throw rides the thrower's own movement, vanilla
// Projectile.shootFromRotation adding getKnownMovement to the throw's
// velocity — so the running throw leaves by the step's three the faster,
// the grounded rise rides nothing, and the airborne one rides the rise,
// whose staying out of the spawn's level pitch says the heading was read
// before the ride joined.
func TestAThrowRidesItsThrower(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.BowTestConfig(), "Thrower")
	srv.GiveSlot("Thrower", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:snowball"), Count: 16})

	// Every throw is aimed level east, made with the move report named
	// below the last one the client sent. A throw's aim carries noise of a
	// few hundredths either way, so each comparison leaves that much room.
	c.MoveTo(0.5, 64, 0.5, -90, 0)
	c.MoveTo(0.5, 64, 0.5, -90, 0) // the same spot again, the movement the standing throw rides nought
	servertest.UseHeld(c, -90, 0)
	standing := c.ExpectThrownSpawn(srv, protocol.EntityTypeSnowball)

	c.MoveToGrounded(3.5, 64, 0.5, -90, 0, true)
	servertest.UseHeld(c, -90, 0)
	running := c.ExpectThrownSpawn(srv, protocol.EntityTypeSnowball)

	c.MoveToGrounded(3.5, 64.42, 0.5, -90, 0, true)
	servertest.UseHeld(c, -90, 0)
	grounded := c.ExpectThrownSpawn(srv, protocol.EntityTypeSnowball)

	c.MoveToGrounded(3.5, 64.84, 0.5, -90, 0, false)
	servertest.UseHeld(c, -90, 0)
	airborne := c.ExpectThrownSpawn(srv, protocol.EntityTypeSnowball)

	if got := running.Vx - standing.Vx; math.Abs(got-3) > 0.12 {
		t.Errorf("the running throw left %v faster than the standing one, want the reported step's three", got)
	}
	if got := grounded.Vy - standing.Vy; math.Abs(got) > 0.12 {
		t.Errorf("a rise reported with the feet on the ground rode the throw %v, want the vertical part left out", got)
	}
	if got := airborne.Vy - standing.Vy; math.Abs(got-0.42) > 0.12 {
		t.Errorf("a rise reported off the ground rode the throw %v, want the reported rise's 0.42", got)
	}
	// The heading is the throw velocity's own, whatever the ride was: the
	// rise would turn the spawn's pitch the sixteen of the combined flight
	// if the ride came first. The pitch rides the wire as a byte of the
	// circle, a degree and a bit to the step, so the comparison leaves
	// that much room.
	if got := airborne.Pitch; got > 2 && got < 358 {
		t.Errorf("a throw released off the ground spawned at pitch %v, want the throw velocity's own level", got)
	}
}

// TestAThrownEggBreaksOnAMobWithTheHurtItsLandingCarries throws an egg at a
// chicken standing in its way and checks what the hit tells the world: the
// damage event naming the chicken and the thrown damage the blow carries,
// the knockback the hurt's deal leaves along the flight, the chicken's hurt
// sound at the chicken — and no flinch besides, vanilla's hurt animation
// being a player's own packet and a mob told of by the damage event alone.
// A hurt with no damage in it opens the half-second window all the same and
// leaves the health it found. The egg's break and its taking away ride the
// same second as they ever did.
func TestAThrownEggBreaksOnAMobWithTheHurtItsLandingCarries(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.BowTestConfig(), "Thrower")

	cowID := srv.SpawnMob("minecraft:chicken", 2.5, 64, 0.5)
	chicken := c.ExpectMobSpawn(protocol.EntityTypeChicken)
	if chicken.EntityID != cowID {
		t.Fatalf("spawned chicken's entity ID = %d, want the one SpawnMob reported (%d)", chicken.EntityID, cowID)
	}
	// The classic voice, whatever the finalize's dice chose; the variants
	// get their own test.
	srv.SetMobSoundVariant(cowID, "minecraft:classic")
	srv.GiveSlot("Thrower", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:egg"), Count: 16})

	c.MoveTo(0.5, 64, 0.5, -90, 25)
	// The chicken pinned where the aim points, the stroll its own mind
	// wanders on unable to walk it out of the way of an egg that takes
	// two ticks to cross the distance.
	srv.MoveMob(cowID, 2.5, 0.5)
	servertest.UseHeld(c, -90, 25)

	traffic := servertest.ReadThrownTraffic(c, srv, 80)
	egg := traffic.SpawnOfType(protocol.EntityTypeEgg)
	if egg == nil {
		t.Fatal("no egg came into the world for the throw")
	}
	events := traffic.EventsOf(egg.EntityID)
	if len(events) != 1 || events[0].Status != protocol.EntityStatusDeath {
		t.Fatalf("the egg's entity events = %#v, want the break's one", events)
	}
	if !traffic.RemovedOnce(egg.EntityID) {
		t.Error("the egg was not taken out of the world exactly once")
	}

	// The damage event: the chicken named its victim, the thrown type the
	// hurting — the type registry's own number for minecraft:thrown.
	damageEvents := traffic.DamageEventsOf(cowID)
	if len(damageEvents) != 1 {
		t.Fatalf("the chicken was named in %d damage events, want the one the blow's landing sends", len(damageEvents))
	}
	thrownType := registry.MustID("minecraft:damage_type", "minecraft:thrown")
	if damageEvents[0].DamageType != thrownType {
		t.Errorf("the damage event's type = %d, want minecraft:thrown (%d)", damageEvents[0].DamageType, thrownType)
	}
	// No flinch: the hurt animation is a player's own packet, and the
	// chicken hears of its hurt by the damage event alone.
	if anims := traffic.HurtAnimationsOf(cowID); len(anims) != 0 {
		t.Errorf("the chicken was told %d hurt animations, want none a thrown blow sends a mob", len(anims))
	}

	// The knockback: along the flight the egg came on — east, the yaw of
	// the throw — with the jump out of the ground the vanilla deal grants
	// a body standing on one, and nothing sideways worth the name.
	knocked := false
	for _, m := range traffic.Motions {
		// The shove lifts the cow; the motions after it only follow its fall
		// and its run.
		if m.Id != cowID || m.Vy <= 0 || knocked {
			continue
		}
		knocked = true
		if m.Vx < 0.3 || m.Vx > 0.5 {
			t.Errorf("the knockback ran east at vx=%v, want the flight's own way near four tenths", m.Vx)
		}
		if m.Vy < 0.3 || m.Vy > 0.5 {
			t.Errorf("the knockback jumped at vy=%v, want the grounded body's four tenths", m.Vy)
		}
		if m.Vz < -0.15 || m.Vz > 0.15 {
			t.Errorf("the knockback shoved sideways at vz=%v, want next to nothing off a straight flight", m.Vz)
		}
	}
	if !knocked {
		t.Error("the chicken was never knocked, want the shove along the flight")
	}

	// The hurt's voice: the chicken's own, played from where the chicken
	// stands.
	hurtSound := false
	for _, s := range traffic.Sounds {
		if s.Name == "minecraft:entity.chicken.hurt" &&
			s.X > 2 && s.X < 3 && s.Y > 63.5 && s.Y < 64.5 && s.Z > 0 && s.Z < 1 {
			hurtSound = true
		}
	}
	if !hurtSound {
		t.Errorf("no chicken hurt sound played from the chicken: %+v", traffic.Sounds)
	}

	// The health: a hurt carrying nothing changes nothing, so every word
	// the chicken's health was told in says the four it came with.
	for _, h := range traffic.HealthsOf(cowID) {
		if h != 4 {
			t.Errorf("the chicken's health was told %v, want the four a no-damage hurt leaves", h)
		}
	}
}

// TestAnEggHatchesAChickWhereItsRollGrantsOne throws a seeded egg straight
// down over open ground and waits out the break: where the seed's first
// roll of eight lands, a chicken stands where the egg broke — and where it
// passes, none does.
func TestAnEggHatchesAChickWhereItsRollGrantsOne(t *testing.T) {
	t.Parallel()
	srv, c, playerID := servertest.JoinAs(t, servertest.BowTestConfig(), "Thrower")

	// A seed whose rolls grant the one-chick hatch, found the way the
	// entity tests find theirs.
	seed := int64(-1)
	for candidate := int64(1); candidate < 100000; candidate++ {
		if thrownRolls(candidate) == 1 {
			seed = candidate
			break
		}
	}
	if seed < 0 {
		t.Fatal("no seed under a hundred thousand hatched one chick")
	}

	// A steep throw straight down: the flight is two ticks and the break
	// lands almost under the thrower's feet.
	eggID := srv.ThrowThrown(entity.ThrownEgg, 0.5, 64+1.62-0.1, 0.5, -90, 80, playerID, seed)

	traffic := servertest.ReadThrownTraffic(c, srv, 80)
	egg := traffic.SpawnOfType(protocol.EntityTypeEgg)
	if egg == nil || egg.EntityID != eggID {
		t.Fatalf("the seeded throw's spawn = %#v, want egg %d", egg, eggID)
	}
	if events := traffic.EventsOf(eggID); len(events) != 1 {
		t.Fatalf("the egg's entity events = %#v, want the break's one", events)
	}

	// The chick: a chicken of the world, hatched where the egg broke —
	// on the ground's face.
	chick := traffic.SpawnOfType(protocol.EntityTypeChicken)
	if chick == nil {
		t.Fatal("the roll that grants a chick hatched none")
	}
	if chick.Y < 63.9 || chick.Y > 64.1 {
		t.Errorf("the chick hatched at y=%v, want the ground's face", chick.Y)
	}
}
