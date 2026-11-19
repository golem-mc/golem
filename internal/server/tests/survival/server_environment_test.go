package survival

import (
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// The environment's damage to players, the vanilla way: the burns of lava
// and fire and the burning that carries on after them, the drowning of deep
// water, the pricks of cactus and berry bush, the sear of magma underfoot
// and the crush of a block that fills the space a head is in. Each test
// drives the survival ticks itself, so the rhythm a source hurts on is
// measured in ticks rather than estimated in wall-clock time: the main loop
// is stopped and every tick's survival pass is run by hand, the way the
// loading-grace tests do.

// drainMobEffects counts the Update Mob Effect packets the hand-driven ticks
// sent, the way a test proves a body the environment spared took no effect.
func drainMobEffects(c *servertest.Client) int {
	c.T.Helper()
	count := 0
	c.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlayUpdateMobEffect {
			count++
		}
	})
	return count
}

// expectEnvSound reads until a Sound Effect carrying the named sound
// arrives, skipping the background traffic, and checks where it played:
// the fizz of a landed lava bite is the one the environment's tests care
// to pin.
func expectEnvSound(srv *server.Server, t *testing.T, c *servertest.Client, sound string) {
	t.Helper()
	want, ok := registry.SoundEventID(sound)
	if !ok {
		t.Fatalf("no sound event for %s", sound)
	}
	want++
	if c.ReadTicks(srv, 100, func(id int32, data []byte) bool {
		t.Helper()
		switch id {
		case protocol.IDPlaySoundEffect:
			r := protocol.NewReader(data)
			if got := r.ReadVarInt(); got != want {
				t.Fatalf("sound = %d, want %d (%s)", got, want, sound)
			}
			if got := r.ReadVarInt(); got != int32(protocol.SoundCategoryPlayer) {
				t.Errorf("sound category = %d, want %d (the players')", got, protocol.SoundCategoryPlayer)
			}
			r.ReadI32()
			r.ReadI32()
			r.ReadI32()
			if got := r.ReadF32(); got != 0.4 {
				t.Errorf("sound volume = %v, want 0.4", got)
			}
			return true
		}
		return false
	}) {
		return
	}
	t.Fatalf("no %s sound arrived", sound)
}

// TestLavaBurns checks the lava's own bite: four half-hearts the moment a
// body stands in it, again ten ticks later, and the burning it leaves
// showing in the metadata for everyone to see.
func TestLavaBurns(t *testing.T) {
	t.Parallel()
	cfg := servertest.EnvironmentConfig()

	srv, connect := servertest.Start(t, cfg)
	tick := servertest.StopTicking(t, srv)
	c := connect()
	entityID := c.JoinAs("Singed")
	// The fed body's own regeneration would heal between the bites and
	// blur the rhythm, the way it would in vanilla; a bar below the
	// regeneration line keeps the bites alone.
	srv.SetFood("Singed", 17)

	// A lava source where the player is about to stand, their own cell the
	// body the vanilla scan asks after.
	lava := servertest.BlockState(t, "minecraft:lava")
	if !srv.SetBlock(0, 64, 20, lava) {
		t.Fatal("the lava's chunk was not loaded")
	}
	c.MoveToGround(0.5, 64, 20.5, true)
	c.Settle()

	// The first tick in the lava: four half-hearts gone, the event naming
	// the lava and nobody behind it, and the flame showing.
	tick(1)
	got := c.ExpectHealth()
	if want := float32(16.0); got.Health != want {
		t.Errorf("after one tick in lava: health=%v, want %v", got.Health, want)
	}
	c.ExpectDamageEvent(entityID, "minecraft:lava", 0)
	// The fizz vanilla's lavaHurt plays for a bite that lands.
	expectEnvSound(srv, t, c, "minecraft:entity.generic.burn")
	metadata := c.ExpectMetadata()
	if metadata.EntityID != entityID || metadata.Flags&protocol.EntityFlagOnFire == 0 {
		t.Errorf("metadata after a tick in lava = %+v, want the on-fire bit set for the player's own body", metadata)
	}

	// Nine more ticks: nothing, the window the first bite armed still
	// standing. The tenth bites again.
	tick(9)
	if n := servertest.DrainDamage(c); n != 0 {
		t.Errorf("%d damage events arrived inside the invulnerability window, want none", n)
	}
	tick(1)
	got = c.ExpectHealth()
	if want := float32(12.0); got.Health != want {
		t.Errorf("after the second lava bite: health=%v, want %v", got.Health, want)
	}
	c.ExpectDamageEvent(entityID, "minecraft:lava", 0)
}

// TestFireBurnsAndOutlivesItsBlock checks the fire block's bite and the
// burn that outlives it: standing in fire bites through the same
// invulnerability window the lava paces itself by, once every ten ticks
// however the fire's own tick and the burn's mix themselves; and once the
// body steps out of the fire the burning carries on, the flame still
// showing and the on-fire damage keeping its once-a-second rhythm, until
// the water puts it out.
func TestFireBurnsAndOutlivesItsBlock(t *testing.T) {
	t.Parallel()
	cfg := servertest.EnvironmentConfig()

	srv, connect := servertest.Start(t, cfg)
	tick := servertest.StopTicking(t, srv)
	c := connect()
	entityID := c.JoinAs("Torch")

	inFire := registry.MustID("minecraft:damage_type", "minecraft:in_fire")
	fire := servertest.BlockState(t, "minecraft:fire")
	if !srv.SetBlock(0, 64, 30, fire) {
		t.Fatal("the fire's chunk was not loaded")
	}
	c.MoveToGround(0.5, 64, 30.5, true)
	c.Settle()

	// Standing in the fire: the first tick's bite is the fire's own - the
	// burn has not begun -, then the rhythm the window keeps.
	tick(1)
	got := c.ExpectHealth()
	if want := float32(19.0); got.Health != want {
		t.Errorf("after one tick in fire: health=%v, want %v", got.Health, want)
	}
	c.ExpectDamageEvent(entityID, "minecraft:in_fire", 0)
	metadata := c.ExpectMetadata()
	if metadata.EntityID != entityID || metadata.Flags&protocol.EntityFlagOnFire == 0 {
		t.Errorf("metadata after a tick in fire = %+v, want the on-fire bit set", metadata)
	}
	tick(9)
	if n := servertest.DrainDamage(c); n != 0 {
		t.Errorf("%d damage events arrived inside the invulnerability window, want none", n)
	}
	tick(1)
	c.ExpectHealth()
	c.ExpectSurvival(protocol.IDPlayDamageEvent) // the fire's or the burn's own

	// Out of the fire, and the burn goes on for forty ticks: no more
	// in_fire, the flame still showing, the once-a-second rhythm biting
	// where it is due.
	c.MoveToGround(0.5, 64, 31.5, true)
	c.Settle()
	tick(40)
	burnt := 0
	doused := false
	c.SettleAndCollect(func(id int32, data []byte) {
		switch id {
		case protocol.IDPlayDamageEvent:
			r := protocol.NewReader(data)
			r.ReadVarInt()
			if got := r.ReadVarInt(); got == inFire {
				t.Error("out of the fire, the damage type is still in_fire")
			}
			burnt++
		case protocol.IDPlayEntityMetadata:
			m := servertest.ReadBurningMetadata(t, data)
			// The flame rides the metadata unchanged while it burns, so
			// the only word the burn-out ticks could send about it is
			// that it went out - and it must not have.
			if m.EntityID == entityID && m.Flags&protocol.EntityFlagOnFire == 0 {
				doused = true
			}
		}
	})
	if burnt < 1 {
		t.Error("forty ticks out of the fire left no on-fire damage, want the burn to carry on")
	}
	if doused {
		t.Error("forty ticks out of the fire put the flame out early")
	}

	// The water puts it out: into the water, and the flame goes off the
	// metadata and no more damage follows.
	water := servertest.BlockState(t, "minecraft:water")
	if !srv.SetBlock(0, 64, 32, water) || !srv.SetBlock(0, 65, 32, water) {
		t.Fatal("the water's chunk was not loaded")
	}
	c.MoveToGround(0.5, 64, 32.5, true)
	c.Settle()
	tick(1)
	dousedByWater := false
	c.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlayEntityMetadata {
			m := servertest.ReadBurningMetadata(t, data)
			if m.EntityID == entityID && m.Flags&protocol.EntityFlagOnFire == 0 {
				dousedByWater = true
			}
		}
	})
	if !dousedByWater {
		t.Error("a tick in the water did not put the flame out of the metadata")
	}
	tick(20)
	if n := servertest.DrainDamage(c); n != 0 {
		t.Errorf("%d damage events arrived after the douse, want the burn to be out", n)
	}
}

// TestDrowning checks the breath: the air runs down a tick at a time with
// the eyes under water and shows in the metadata as it goes, nothing bites
// until the breath has run twenty ticks past nothing, and the bite that
// does is the drown's own two.
func TestDrowning(t *testing.T) {
	t.Parallel()
	cfg := servertest.EnvironmentConfig()

	srv, connect := servertest.Start(t, cfg)
	tick := servertest.StopTicking(t, srv)
	c := connect()
	entityID := c.JoinAs("Diver")
	// The fed body's own regeneration would heal between the drowning
	// bites and blur the rhythm, the way it would in vanilla; a bar below
	// the regeneration line keeps the bites alone.
	srv.SetFood("Diver", 17)

	// Two blocks of water over the ground, the eyes of a body standing in
	// it inside the second.
	water := servertest.BlockState(t, "minecraft:water")
	if !srv.SetBlock(0, 64, 50, water) || !srv.SetBlock(0, 65, 50, water) {
		t.Fatal("the water's chunk was not loaded")
	}
	c.MoveToGround(0.5, 64, 50.5, true)
	c.Settle()

	// The first hundred ticks: the air runs down and the metadata says so,
	// and nothing bites.
	tick(100)
	air := int32(-1)
	sawAir := false
	bitten := 0
	c.SettleAndCollect(func(id int32, data []byte) {
		switch id {
		case protocol.IDPlayEntityMetadata:
			m := servertest.ReadBurningMetadata(t, data)
			if m.EntityID == entityID && m.HasAir {
				air, sawAir = m.Air, true
			}
		case protocol.IDPlayDamageEvent:
			bitten++
		}
	})
	if !sawAir {
		t.Fatal("a hundred ticks under water sent no air metadata")
	}
	// Three hundred minus a hundred ticks of breath: the last metadata
	// said two hundred.
	if want := int32(rules.MaxAirSupply - 100); air != want {
		t.Errorf("the air metadata said %d after a hundred ticks under water, want %d", air, want)
	}
	if bitten != 0 {
		t.Errorf("%d damage events arrived with breath to spare, want none", bitten)
	}

	// Three hundred and nineteen ticks under water since the dive: the
	// breath has run to one tick short of the floor, and nothing has
	// bitten.
	tick(rules.MaxAirSupply + 19 - 100)
	if n := servertest.DrainDamage(c); n != 0 {
		t.Errorf("%d damage events arrived a tick before the floor, want the bite to wait for it", n)
	}
	// The three hundred and twentieth is the one: twenty past nothing, two
	// half-hearts, the event naming the drowning. The bubble event vanilla
	// broadcasts ahead of the blow arrives with it.
	tick(1)
	expectDrownEvent(t, c, entityID)
	got := c.ExpectHealth()
	if want := float32(18.0); got.Health != want {
		t.Errorf("at the drowning floor: health=%v, want %v", got.Health, want)
	}
	c.ExpectDamageEvent(entityID, "minecraft:drown", 0)

	// The next bite is twenty ticks behind, the counter having been reset
	// for the body still under water.
	tick(19)
	if n := servertest.DrainDamage(c); n != 0 {
		t.Errorf("%d damage events arrived before the next drowning was due", n)
	}
	tick(1)
	expectDrownEvent(t, c, entityID)
	got = c.ExpectHealth()
	if want := float32(16.0); got.Health != want {
		t.Errorf("at the second drowning: health=%v, want %v", got.Health, want)
	}
	c.ExpectDamageEvent(entityID, "minecraft:drown", 0)
}

// expectDrownEvent reads the next Entity Event and checks it is the bubble
// event vanilla broadcasts for the breath that drowned, on the body that
// drowned.
func expectDrownEvent(t *testing.T, c *servertest.Client, entityID int32) {
	t.Helper()
	// The air metadata the hand-run ticks broadcast can still be on its
	// way on a slow machine; the diver's own metadata is stepped over.
	for id, got := range c.Packets(servertest.ExpectTimeout) {
		if id == protocol.IDPlayEntityMetadata && protocol.NewReader(got).ReadVarInt() == entityID {
			continue
		}
		c.Unread(servertest.PushedPacket{Id: id, Data: got})
		break
	}
	r := protocol.NewReader(c.ExpectSurvival(protocol.IDPlayEntityEvent))
	if got := r.ReadI32(); got != entityID {
		t.Errorf("entity event for %d, want %d", got, entityID)
	}
	if got := r.ReadU8(); got != protocol.EntityEventDrown {
		t.Errorf("entity event = %d, want %d (the drowning's own)", got, protocol.EntityEventDrown)
	}
}

// TestDrowningRecover checks the breath the other way: out of the water the
// air comes back four a tick, the metadata counting it up, and no drowning
// waits in the counter that was reset.
func TestDrowningRecover(t *testing.T) {
	t.Parallel()
	cfg := servertest.EnvironmentConfig()

	srv, connect := servertest.Start(t, cfg)
	tick := servertest.StopTicking(t, srv)
	c := connect()
	entityID := c.JoinAs("Surfacer")

	water := servertest.BlockState(t, "minecraft:water")
	if !srv.SetBlock(0, 64, 52, water) || !srv.SetBlock(0, 65, 52, water) {
		t.Fatal("the water's chunk was not loaded")
	}
	c.MoveToGround(0.5, 64, 52.5, true)
	c.Settle()

	// Long enough under water to be well past the half of the breath, then
	// into the dry cell beside the pool: the air climbs four a tick and the
	// metadata climbs with it.
	tick(150)
	c.MoveToGround(0.5, 64, 51.5, true)
	c.Settle()
	tick(10)
	air := int32(-1)
	bitten := 0
	c.SettleAndCollect(func(id int32, data []byte) {
		switch id {
		case protocol.IDPlayEntityMetadata:
			m := servertest.ReadBurningMetadata(t, data)
			if m.EntityID == entityID && m.HasAir {
				air = m.Air
			}
		case protocol.IDPlayDamageEvent:
			bitten++
		}
	})
	if want := int32(rules.MaxAirSupply - 150 + 40); air != want {
		t.Errorf("the air metadata said %d after the climb, want %d", air, want)
	}
	if bitten != 0 {
		t.Errorf("%d damage events arrived while surfacing, want none", bitten)
	}
}

// TestCactusPricks checks the cactus: one half-heart for standing against
// it, on the ten-tick rhythm every bite shares.
func TestCactusPricks(t *testing.T) {
	t.Parallel()
	cfg := servertest.EnvironmentConfig()

	srv, connect := servertest.Start(t, cfg)
	tick := servertest.StopTicking(t, srv)
	c := connect()
	entityID := c.JoinAs("Pricked")
	// The fed body's own regeneration would heal between the bites and
	// blur the rhythm, the way it would in vanilla; a bar below the
	// regeneration line keeps the bites alone.
	srv.SetFood("Pricked", 17)

	cactus := servertest.BlockState(t, "minecraft:cactus")
	if !srv.SetBlock(10, 64, 50, cactus) {
		t.Fatal("the cactus's chunk was not loaded")
	}
	// Standing so the body's own box reaches into the cactus's cell, the
	// way a body pressed against one does.
	c.MoveToGround(10.5, 64, 50.7, true)
	c.Settle()

	tick(1)
	got := c.ExpectHealth()
	if want := float32(19.0); got.Health != want {
		t.Errorf("after one tick against the cactus: health=%v, want %v", got.Health, want)
	}
	c.ExpectDamageEvent(entityID, "minecraft:cactus", 0)
	tick(9)
	if n := servertest.DrainDamage(c); n != 0 {
		t.Errorf("%d damage events arrived inside the invulnerability window, want none", n)
	}
	tick(1)
	if got := c.ExpectHealth(); got.Health != 18 {
		t.Errorf("after the second cactus bite: health=%v, want 18", got.Health)
	}
	c.ExpectDamageEvent(entityID, "minecraft:cactus", 0)
}

// TestAWitherRoseWithersAStandingBody checks the wither rose: a body standing
// in the flower is planted the wither's full forty ticks at amplifier 0 on the
// tick it steps in, and that wither bites a tick later, on the rhythm it keeps
// while the flower renews it.
func TestAWitherRoseWithersAStandingBody(t *testing.T) {
	t.Parallel()
	cfg := servertest.EnvironmentConfig()
	cfg.Difficulty = "normal"

	srv, connect := servertest.Start(t, cfg)
	tick := servertest.StopTicking(t, srv)
	c := connect()
	entityID := c.JoinAs("Withered")
	// A bar below the regeneration line keeps the body's own healing out of
	// the bites, the way the cactus test does.
	srv.SetFood("Withered", 17)

	rose := servertest.BlockState(t, "minecraft:wither_rose")
	if !srv.SetBlock(10, 64, 50, rose) {
		t.Fatal("the wither rose's chunk was not loaded")
	}
	c.MoveToGround(10.5, 64, 50.7, true)
	c.Settle()

	// The flower plants its wither after the tick's own effects, so the
	// effect packet arrives on the first tick; telling it apart from the
	// renewals that follow is what the freshly-added flags are for.
	tick(1)
	c.ExpectMobEffectAdded(entityID, registry.MustMobEffect("minecraft:wither"), 0, 40)

	tick(1)
	var bitten bool
	var health float32 = -1
	c.SettleAndCollect(func(id int32, data []byte) {
		switch id {
		case protocol.IDPlaySetHealth:
			health = servertest.ExpectHealthReport(c, data).Health
		case protocol.IDPlayDamageEvent:
			r := protocol.NewReader(data)
			victim, kind := r.ReadVarInt(), r.ReadVarInt()
			if victim == entityID && kind == registry.MustID("minecraft:damage_type", "minecraft:wither") {
				bitten = true
			}
		}
	})
	if !bitten {
		t.Error("the wither the rose planted never bit the body")
	}
	if health != 19 {
		t.Errorf("after the wither's first bite: health=%v, want 19", health)
	}
}

// TestSuffocationCrushes checks the wall: a body buried in the stone — the
// cells its feet and its eyes stand in both filled — crushes for one bite
// every half second, and taking the stone over its eyes away stops it. The
// body is buried rather than only head-blocked on purpose: a body whose
// feet cell stays air fits the crawl's half-block shape where it stands,
// and the pose the vanilla server draws it in then ducks its eyes out of
// the stone — one last bite and the crush is over, which the swimmer test
// below pins. Only a body the crawl cannot fit keeps the standing shape
// and keeps the stone in its eyes.
func TestSuffocationCrushes(t *testing.T) {
	t.Parallel()
	cfg := servertest.EnvironmentConfig()

	srv, connect := servertest.Start(t, cfg)
	tick := servertest.StopTicking(t, srv)
	c := connect()
	entityID := c.JoinAs("Buried")
	// The fed body's own regeneration would heal between the bites and
	// blur the rhythm, the way it would in vanilla; a bar below the
	// regeneration line keeps the bites alone.
	srv.SetFood("Buried", 17)

	stone := servertest.BlockState(t, "minecraft:stone")
	if !srv.SetBlock(10, 64, 60, stone) {
		t.Fatal("the wall's chunk was not loaded")
	}
	if !srv.SetBlock(10, 65, 60, stone) {
		t.Fatal("the wall's chunk was not loaded")
	}
	c.MoveToGround(10.5, 64, 60.5, true)
	c.Settle()

	tick(1)
	got := c.ExpectHealth()
	if want := float32(19.0); got.Health != want {
		t.Errorf("after one tick in the wall: health=%v, want %v", got.Health, want)
	}
	c.ExpectDamageEvent(entityID, "minecraft:in_wall", 0)
	tick(9)
	if n := servertest.DrainDamage(c); n != 0 {
		t.Errorf("%d damage events arrived inside the invulnerability window, want none", n)
	}
	tick(1)
	if got := c.ExpectHealth(); got.Health != 18 {
		t.Errorf("after the second crush: health=%v, want 18", got.Health)
	}
	c.ExpectDamageEvent(entityID, "minecraft:in_wall", 0)

	// The stone over the eyes gone, the crush stops: the body's feet stay
	// in the stone, so the pose stays standing and the eyes stay where the
	// stone was — which is now air.
	if !srv.SetBlock(10, 65, 60, 0) {
		t.Fatal("the wall's chunk was not loaded")
	}
	c.Settle()
	tick(20)
	if n := servertest.DrainDamage(c); n != 0 {
		t.Errorf("%d damage events arrived with the wall gone, want none", n)
	}
}

// TestSneakingUnderABlockCrawls checks the crawl: a body that ducks under a
// gap a crouch no longer fits — the closed trapdoor over its head the
// classic — is drawn in the swim pose, vanilla's own fallback when neither
// the standing nor the crouching shape fits where the body stands, without
// a word from the client: the pose follows the world, on the tick the
// world changed.
func TestSneakingUnderABlockCrawls(t *testing.T) {
	t.Parallel()
	cfg := servertest.EnvironmentConfig()

	srv, connect := servertest.Start(t, cfg)
	tick := servertest.StopTicking(t, srv)
	c := connect()
	c.JoinAs("Crawler")
	// The fed body's own regeneration would heal between the bites and
	// blur the rhythm, the way it would in vanilla; a bar below the
	// regeneration line keeps the bites alone.
	srv.SetFood("Crawler", 17)

	// The flat ground a body stands on is the world's own; the trapdoor
	// over the head is the test's.
	c.MoveToGround(0.5, 64, 60.5, true)
	c.Settle()

	// The sneak alone is the crouch: the shape the body asks for fits
	// where it stands.
	c.Send(protocol.IDPlayPlayerInput, func(b *protocol.Buffer) { b.PutU8(protocol.InputSneak) })
	if got := c.ExpectMetadata(); got.Pose != protocol.PoseSneaking {
		t.Errorf("pose = %d, want %d (crouching)", got.Pose, protocol.PoseSneaking)
	}

	// The closed trapdoor over the head takes the gap down below what a
	// crouch holds: the next tick's pose refresh draws the body down into
	// the crawl, the swim pose's own shape.
	trapdoor := servertest.BlockStateWith(t, "minecraft:oak_trapdoor",
		"facing=north,half=bottom,open=false,powered=false,waterlogged=false")
	if !srv.SetBlock(0, 65, 60, trapdoor) {
		t.Fatal("the crawl's chunk was not loaded")
	}
	c.Settle()
	tick(1)
	if got := c.ExpectMetadata(); got.Pose != protocol.PoseSwimming {
		t.Errorf("pose = %d, want %d (swimming, the crawl)", got.Pose, protocol.PoseSwimming)
	}

	// The crawl's eyes sit four tenths up, clear of the plate: the world
	// crushes the body no more for standing where it does.
	tick(12)
	if n := servertest.DrainDamage(c); n != 0 {
		t.Errorf("%d damage events arrived once the body crawled, want none", n)
	}
}

// TestCreativeIsUntouchedByTheEnvironment checks the mode that cannot be
// hurt: standing in lava and under water costs a creative player nothing,
// shows no flame and drowns no breath.
func TestCreativeIsUntouchedByTheEnvironment(t *testing.T) {
	t.Parallel()
	cfg := servertest.EnvironmentConfig()
	cfg.GameMode = "creative"

	srv, connect := servertest.Start(t, cfg)
	tick := servertest.StopTicking(t, srv)
	c := connect()
	c.JoinAs("Untouchable")

	lava := servertest.BlockState(t, "minecraft:lava")
	water := servertest.BlockState(t, "minecraft:water")
	if !srv.SetBlock(30, 64, 20, lava) || !srv.SetBlock(30, 64, 30, water) || !srv.SetBlock(30, 65, 30, water) {
		t.Fatal("the test's chunks were not loaded")
	}
	c.MoveToGround(30.5, 64, 20.5, true)
	c.Settle()
	tick(30)
	hurt, burnt := 0, false
	c.SettleAndCollect(func(id int32, data []byte) {
		switch id {
		case protocol.IDPlayDamageEvent:
			hurt++
		case protocol.IDPlayEntityMetadata:
			m := servertest.ReadBurningMetadata(t, data)
			if m.Flags&protocol.EntityFlagOnFire != 0 {
				burnt = true
			}
		}
	})
	if hurt != 0 {
		t.Errorf("%d damage events arrived for a creative player in lava, want none", hurt)
	}
	if burnt {
		t.Error("a creative player in lava shows the on-fire bit")
	}
	c.MoveToGround(30.5, 64, 30.5, true)
	c.Settle()
	tick(rules.MaxAirSupply + 40)
	hurt = 0
	c.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlayDamageEvent {
			hurt++
		}
	})
	if hurt != 0 {
		t.Errorf("%d damage events arrived for a creative player under water, want none", hurt)
	}
}
