package survival

import (
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
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

// TestLavaCauldronBurns checks that a player standing in a lava cauldron
// catches fifteen seconds of flame and takes the lava's four.
// vanilla: LavaCauldronBlock.entityInside
func TestLavaCauldronBurns(t *testing.T) {
	t.Parallel()
	cfg := servertest.EnvironmentConfig()

	srv, connect := servertest.Start(t, cfg)
	tick := servertest.StopTicking(t, srv)
	c := connect()
	entityID := c.JoinAs("Cauldron")
	srv.SetFood("Cauldron", 17)

	if !srv.SetBlock(0, 64, 20, servertest.BlockState(t, "minecraft:lava_cauldron")) {
		t.Fatal("the cauldron's chunk was not loaded")
	}
	c.MoveToGround(0.5, 64, 20.5, true)
	c.Settle()

	tick(1)
	if got := c.ExpectHealth(); got.Health != 16 {
		t.Errorf("after one tick in a lava cauldron: health=%v, want 16", got.Health)
	}
	c.ExpectDamageEvent(entityID, "minecraft:lava", 0)
	if fire := srv.PlayerFire("Cauldron"); fire < 299 {
		t.Errorf("fire ticks = %d, want about 300", fire)
	}
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

// TestFireSeenByAnotherPlayer checks the half of a burn the victim does not
// see: the metadata the others are sent says their player is on fire while
// it burns, and says so no more once it is out.
func TestFireSeenByAnotherPlayer(t *testing.T) {
	t.Parallel()
	cfg := servertest.EnvironmentConfig()

	srv, connect := servertest.Start(t, cfg)
	tick := servertest.StopTicking(t, srv)

	watcher := connect()
	watcher.JoinAs("Watcher")

	burner := connect()
	burnerID := burner.JoinAs("Burner")

	watcher.ExpectPlayerAdded("Burner")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Burner")

	lava := servertest.BlockState(t, "minecraft:lava")
	if !srv.SetBlock(0, 64, 40, lava) {
		t.Fatal("the lava's chunk was not loaded")
	}
	burner.MoveToGround(0.5, 64, 40.5, true)
	burner.Settle()
	watcher.Settle()

	tick(1)
	// Both clients are told: the hurt they share, and the flame.
	if got := burner.ExpectHealth(); got.Health != 16 {
		t.Errorf("the burner's health after the lava = %v, want 16", got.Health)
	}
	burner.ExpectDamageEvent(burnerID, "minecraft:lava", 0)
	watcher.ExpectDamageEvent(burnerID, "minecraft:lava", 0)
	m := watcher.ExpectMetadata()
	if m.EntityID != burnerID || m.Flags&protocol.EntityFlagOnFire == 0 {
		t.Errorf("the watcher's metadata for the burner = %+v, want the on-fire bit", m)
	}

	// Out of the lava and into the water, and the flame goes off the
	// watcher's view of them too.
	water := servertest.BlockState(t, "minecraft:water")
	if !srv.SetBlock(0, 64, 41, water) {
		t.Fatal("the water's chunk was not loaded")
	}
	burner.MoveToGround(0.5, 64, 41.5, true)
	burner.Settle()
	tick(1)
	doused := false
	watcher.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlayEntityMetadata {
			m := servertest.ReadBurningMetadata(t, data)
			if m.EntityID == burnerID && m.Flags&protocol.EntityFlagOnFire == 0 {
				doused = true
			}
		}
	})
	if !doused {
		t.Error("the watcher was never told the burn had gone out")
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

// TestAThinFlowBelowTheEyesDoesNotDrown pins the habitat's eye read on the
// fluid's own height: vanilla's isEyeInFluid asks whether the eye is inside
// the fluid, so eyes above a thin flow's surface spend no breath, where the
// block alone would call the eyes wet.
// vanilla: LivingEntity.baseTick
func TestAThinFlowBelowTheEyesDoesNotDrown(t *testing.T) {
	t.Parallel()
	cfg := servertest.EnvironmentConfig()

	srv, connect := servertest.Start(t, cfg)
	tick := servertest.StopTicking(t, srv)
	c := connect()
	c.JoinAs("Wader")
	srv.SetFood("Wader", 17)

	// A level-7 flow in the eye cell, its surface a ninth of a block up: the
	// eyes above it, the body's box still reaching the water.
	if !srv.SetBlock(0, 65, 50, servertest.BlockStateWith(t, "minecraft:water", "level=7")) {
		t.Fatal("the water's chunk was not loaded")
	}
	c.MoveToGround(0.5, 64, 50.5, true)
	c.Settle()

	// A hundred ticks that would have spent a hundred of the breath with the
	// eyes wet: above the flow's own surface none of it is.
	tick(100)
	bitten := 0
	c.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlayDamageEvent {
			bitten++
		}
	})
	if air := srv.PlayerAir("Wader"); air != rules.MaxAirSupply {
		t.Errorf("the breath with the eyes above a thin flow = %d, want the full %d", air, rules.MaxAirSupply)
	}
	if bitten != 0 {
		t.Errorf("%d damage events arrived above a thin flow, want none", bitten)
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

// TestWaterBreathingStopsDrowning checks the breath the effects that breathe
// water keep: with water_breathing or conduit_power on, a body standing with
// its eyes under water spends no breath and takes no drowning, where the same
// body without one runs out on the ticks the drowning test pins.
func TestWaterBreathingStopsDrowning(t *testing.T) {
	t.Parallel()
	for _, effectName := range []string{"minecraft:water_breathing", "minecraft:conduit_power"} {
		t.Run(effectName, func(t *testing.T) {
			cfg := servertest.EnvironmentConfig()

			srv, connect := servertest.Start(t, cfg)
			tick := servertest.StopTicking(t, srv)
			c := connect()
			c.JoinAs("Gilled")
			srv.SetFood("Gilled", 17)
			srv.GiveEffect("Gilled", effectName, 6000, 0, true)
			c.Settle()

			// Two blocks of water over the ground, the eyes inside the
			// second, the pool the drowning tests use.
			water := servertest.BlockState(t, "minecraft:water")
			if !srv.SetBlock(0, 64, 56, water) || !srv.SetBlock(0, 65, 56, water) {
				t.Fatal("the water's chunk was not loaded")
			}
			c.MoveToGround(0.5, 64, 56.5, true)
			c.Settle()

			// Past the breath a body without the effect would have spent
			// and the bites that follow it: the breath stays full and the
			// health untouched.
			tick(420)
			bitten := 0
			c.SettleAndCollect(func(id int32, data []byte) {
				if id == protocol.IDPlayDamageEvent {
					bitten++
				}
			})
			if bitten != 0 {
				t.Errorf("%d damage events arrived under %s, want none", bitten, effectName)
			}
			if air := srv.PlayerAir("Gilled"); air != rules.MaxAirSupply {
				t.Errorf("the breath under %s = %d, want the full %d", effectName, air, rules.MaxAirSupply)
			}
			if got := srv.PlayerHealth("Gilled"); got != 20 {
				t.Errorf("health under %s = %v, want 20", effectName, got)
			}
		})
	}
}

// TestWaterPlantsAtTheEyesSpendBreath checks the breath a body spends with
// its eyes in a water plant: kelp and seagrass fill their own cell with water
// although they carry no waterlogged property, so the eyes in one are under
// water and the air runs down a tick at a time like in the water itself.
func TestWaterPlantsAtTheEyesSpendBreath(t *testing.T) {
	t.Parallel()
	plants := []string{"minecraft:kelp", "minecraft:kelp_plant", "minecraft:seagrass", "minecraft:tall_seagrass"}
	for _, plant := range plants {
		t.Run(plant, func(t *testing.T) {
			cfg := servertest.EnvironmentConfig()

			srv, connect := servertest.Start(t, cfg)
			tick := servertest.StopTicking(t, srv)
			c := connect()
			c.JoinAs("Weeded")
			srv.SetFood("Weeded", 17)

			// Water over the ground with the plant in the eye cell, the
			// block a body standing there holds its eyes in.
			water := servertest.BlockState(t, "minecraft:water")
			if !srv.SetBlock(0, 64, 50, water) || !srv.SetBlock(0, 65, 50, servertest.BlockState(t, plant)) {
				t.Fatal("the test's chunk was not loaded")
			}
			c.MoveToGround(0.5, 64, 50.5, true)
			c.Settle()

			// A hundred ticks with the eyes in the plant: the breath is
			// spent the way it is under plain water and nothing has bitten.
			tick(100)
			bitten := 0
			c.SettleAndCollect(func(id int32, data []byte) {
				if id == protocol.IDPlayDamageEvent {
					bitten++
				}
			})
			if air := srv.PlayerAir("Weeded"); air != rules.MaxAirSupply-100 {
				t.Errorf("the breath with the eyes in %s = %d, want %d", plant, air, rules.MaxAirSupply-100)
			}
			if bitten != 0 {
				t.Errorf("%d damage events arrived with breath to spare, want none", bitten)
			}
		})
	}
}

// TestWaterBreathingInKelpKeepsFullAir checks water_breathing in a water
// plant: the kelp at the eyes is water, but the effect spares the drain, so
// the bar stays full and nothing bites.
func TestWaterBreathingInKelpKeepsFullAir(t *testing.T) {
	t.Parallel()
	cfg := servertest.EnvironmentConfig()

	srv, connect := servertest.Start(t, cfg)
	tick := servertest.StopTicking(t, srv)
	c := connect()
	c.JoinAs("Gilled")
	srv.SetFood("Gilled", 17)
	srv.GiveEffect("Gilled", "minecraft:water_breathing", 6000, 0, true)
	c.Settle()

	// Water over the ground with the kelp in the eye cell.
	water := servertest.BlockState(t, "minecraft:water")
	if !srv.SetBlock(0, 64, 50, water) || !srv.SetBlock(0, 65, 50, servertest.BlockState(t, "minecraft:kelp")) {
		t.Fatal("the test's chunk was not loaded")
	}
	c.MoveToGround(0.5, 64, 50.5, true)
	c.Settle()

	// Past the breath a body without the effect would have spent and the
	// bites that follow it: the breath stays full and nothing has bitten.
	tick(420)
	bitten := 0
	c.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlayDamageEvent {
			bitten++
		}
	})
	if air := srv.PlayerAir("Gilled"); air != rules.MaxAirSupply {
		t.Errorf("the breath with water_breathing in the kelp = %d, want the full %d", air, rules.MaxAirSupply)
	}
	if bitten != 0 {
		t.Errorf("%d damage events arrived with water_breathing in the kelp, want none", bitten)
	}
}

// TestInvulnerableModesRefillBreath checks the breath the creative and
// spectator modes get back: a body that switches to one of them part-way
// through a dive stops spending breath and climbs back to the full bar, both
// with the eyes still under water and after surfacing. vanilla's baseTick
// only stops the drain for a body that cannot drown, so the refill runs.
func TestInvulnerableModesRefillBreath(t *testing.T) {
	t.Parallel()
	modes := []struct {
		name string
		id   int32
	}{
		{"creative", protocol.GameModeCreative},
		{"spectator", protocol.GameModeSpectator},
	}
	wets := []struct {
		wet   bool
		where string
	}{
		{true, "in the water"},
		{false, "out of the water"},
	}
	for _, mode := range modes {
		for _, wet := range wets {
			t.Run(mode.name+"/"+wet.where, func(t *testing.T) {
				cfg := servertest.EnvironmentConfig()

				srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Spared"))
				tick := servertest.StopTicking(t, srv)
				c := connect()
				c.JoinAs("Spared")
				srv.SetFood("Spared", 17)

				water := servertest.BlockState(t, "minecraft:water")
				if !srv.SetBlock(0, 64, 50, water) || !srv.SetBlock(0, 65, 50, water) {
					t.Fatal("the water's chunk was not loaded")
				}
				c.MoveToGround(0.5, 64, 50.5, true)
				c.Settle()

				// Half the breath spent in survival, then the switch to a
				// mode that cannot drown.
				tick(150)
				spent := rules.MaxAirSupply - 150
				if air := srv.PlayerAir("Spared"); air != spent {
					t.Fatalf("the breath before the switch = %d, want %d", air, spent)
				}
				c.SendChangeGameMode(mode.id)
				servertest.PollUntil(t, servertest.ExpectTimeout, func() bool { return srv.PlayerGameMode("Spared") == mode.id })
				if !wet.wet {
					c.MoveToGround(0.5, 64, 51.5, true)
					c.Settle()
				}

				// The missing breath climbs back four a tick.
				tick(50)
				if air := srv.PlayerAir("Spared"); air != rules.MaxAirSupply {
					t.Errorf("the breath of a %s body %s = %d, want the full %d",
						mode.name, wet.where, air, rules.MaxAirSupply)
				}
			})
		}
	}
}

// TestBubbleColumnSparesTheBreath checks the one water cell the breath
// ignores: vanilla's baseTick skips the air loss when the eye block is a
// bubble column, so a survival body there holds its breath, and a sparer
// that switched to a mode that cannot drown refills under one too.
// vanilla: LivingEntity.baseTick
func TestBubbleColumnSparesTheBreath(t *testing.T) {
	t.Parallel()
	t.Run("survival keeps its breath", func(t *testing.T) {
		cfg := servertest.EnvironmentConfig()

		srv, connect := servertest.Start(t, cfg)
		tick := servertest.StopTicking(t, srv)
		c := connect()
		c.JoinAs("Buoyed")
		srv.SetFood("Buoyed", 17)

		// Water under the bubble column in the eye cell.
		water := servertest.BlockState(t, "minecraft:water")
		if !srv.SetBlock(0, 64, 50, water) || !srv.SetBlock(0, 65, 50, servertest.BlockState(t, "minecraft:bubble_column")) {
			t.Fatal("the test's chunk was not loaded")
		}
		c.MoveToGround(0.5, 64, 50.5, true)
		c.Settle()

		// A hundred ticks with the eyes in the bubble column: the breath
		// never runs down and nothing has bitten.
		tick(100)
		bitten := 0
		c.SettleAndCollect(func(id int32, data []byte) {
			if id == protocol.IDPlayDamageEvent {
				bitten++
			}
		})
		if air := srv.PlayerAir("Buoyed"); air != rules.MaxAirSupply {
			t.Errorf("the breath in the bubble column = %d, want the full %d", air, rules.MaxAirSupply)
		}
		if bitten != 0 {
			t.Errorf("%d damage events arrived in the bubble column, want none", bitten)
		}
	})

	t.Run("creative refills under one", func(t *testing.T) {
		cfg := servertest.EnvironmentConfig()

		srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Buoyed"))
		tick := servertest.StopTicking(t, srv)
		c := connect()
		c.JoinAs("Buoyed")
		srv.SetFood("Buoyed", 17)

		water := servertest.BlockState(t, "minecraft:water")
		if !srv.SetBlock(0, 64, 50, water) || !srv.SetBlock(0, 65, 50, water) {
			t.Fatal("the test's chunk was not loaded")
		}
		c.MoveToGround(0.5, 64, 50.5, true)
		c.Settle()

		// Half the breath spent under the water, then the eye cell turned
		// to a bubble column and the mode switched to one that spares it.
		tick(150)
		if air := srv.PlayerAir("Buoyed"); air != rules.MaxAirSupply-150 {
			t.Fatalf("the breath before the switch = %d, want %d", air, rules.MaxAirSupply-150)
		}
		if !srv.SetBlock(0, 65, 50, servertest.BlockState(t, "minecraft:bubble_column")) {
			t.Fatal("the eye cell would not become a bubble column")
		}
		c.SendChangeGameMode(protocol.GameModeCreative)
		servertest.PollUntil(t, servertest.ExpectTimeout, func() bool { return srv.PlayerGameMode("Buoyed") == protocol.GameModeCreative })

		// The missing breath climbs back four a tick.
		tick(50)
		if air := srv.PlayerAir("Buoyed"); air != rules.MaxAirSupply {
			t.Errorf("the breath in the bubble column = %d, want the full %d", air, rules.MaxAirSupply)
		}
	})
}

// TestNautilusHoldsBreath checks breath_of_the_nautilus on its own: unlike
// water_breathing and conduit_power, it does not fill the breath back up while
// the eyes are under water. A body that dives with it freezes where its air
// stands under water — no drain, no refill there (vanilla:
// MobEffectUtil.shouldEffectsRefillAirsupply) — and the breath climbs back
// once the head is out.
func TestNautilusHoldsBreath(t *testing.T) {
	t.Parallel()
	cfg := servertest.EnvironmentConfig()

	srv, connect := servertest.Start(t, cfg)
	tick := servertest.StopTicking(t, srv)
	c := connect()
	c.JoinAs("Nautilus")
	srv.SetFood("Nautilus", 17)

	water := servertest.BlockState(t, "minecraft:water")
	if !srv.SetBlock(0, 64, 54, water) || !srv.SetBlock(0, 65, 54, water) {
		t.Fatal("the water's chunk was not loaded")
	}
	c.MoveToGround(0.5, 64, 54.5, true)
	c.Settle()

	// A hundred ticks of the breath spent first, so the hold has something
	// to hold and a refill would show as a climb back to the full breath.
	tick(100)
	held := rules.MaxAirSupply - 100
	if air := srv.PlayerAir("Nautilus"); air != held {
		t.Fatalf("the breath before the effect = %d, want %d", air, held)
	}

	srv.GiveEffect("Nautilus", "minecraft:breath_of_the_nautilus", 6000, 0, true)
	c.Settle()
	// Four times the breath the drain would have spent: with the nautilus
	// alone neither the drain nor the refill runs, so the air stays put.
	tick(400)
	bitten := 0
	c.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlayDamageEvent {
			bitten++
		}
	})
	if bitten != 0 {
		t.Errorf("%d damage events arrived under breath_of_the_nautilus, want none", bitten)
	}
	if air := srv.PlayerAir("Nautilus"); air != held {
		t.Errorf("the breath under breath_of_the_nautilus = %d, want it held at %d", air, held)
	}

	// Head back out of the water: the nautilus no longer holds the breath
	// with the eyes dry, so it climbs four a tick to the full breath.
	c.MoveToGround(0.5, 64, 53.5, true)
	c.Settle()
	tick(30)
	if air := srv.PlayerAir("Nautilus"); air != rules.MaxAirSupply {
		t.Errorf("the breath after surfacing with breath_of_the_nautilus = %d, want the full %d", air, rules.MaxAirSupply)
	}
}

// TestWaterBreathingRefillsMidDive gives water_breathing to a body part-way
// down its breath: the drain stops and the air climbs back four a tick to
// the full breath, so a body that finds the effect under water recovers.
func TestWaterBreathingRefillsMidDive(t *testing.T) {
	t.Parallel()
	cfg := servertest.EnvironmentConfig()

	srv, connect := servertest.Start(t, cfg)
	tick := servertest.StopTicking(t, srv)
	c := connect()
	c.JoinAs("Refilled")
	srv.SetFood("Refilled", 17)

	water := servertest.BlockState(t, "minecraft:water")
	if !srv.SetBlock(0, 64, 55, water) || !srv.SetBlock(0, 65, 55, water) {
		t.Fatal("the water's chunk was not loaded")
	}
	c.MoveToGround(0.5, 64, 55.5, true)
	c.Settle()

	// Half the breath spent, then the effect: the hundred and fifty
	// missing ticks climb back at four apiece, inside forty.
	tick(150)
	srv.GiveEffect("Refilled", "minecraft:water_breathing", 6000, 0, true)
	c.Settle()
	tick(50)
	if air := srv.PlayerAir("Refilled"); air != rules.MaxAirSupply {
		t.Errorf("the breath after gaining water_breathing mid-dive = %d, want the full %d", air, rules.MaxAirSupply)
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

// TestAWitherRoseWithersTheCreativeAndSparesTheSpectator checks the flower's
// gate against the modes: a creative body takes the wither even though its
// mode refuses the bite, while a spectator's noPhysics keeps it out of the
// flower altogether.
func TestAWitherRoseWithersTheCreativeAndSparesTheSpectator(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		mode   string
		spared bool
	}{
		{"creative", false},
		{"spectator", true},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			t.Parallel()
			cfg := servertest.EnvironmentConfig()
			cfg.Difficulty = "normal"
			cfg.GameMode = tc.mode

			srv, connect := servertest.Start(t, cfg)
			tick := servertest.StopTicking(t, srv)
			c := connect()
			entityID := c.JoinAs("Builder")

			rose := servertest.BlockState(t, "minecraft:wither_rose")
			if !srv.SetBlock(10, 64, 50, rose) {
				t.Fatal("the wither rose's chunk was not loaded")
			}
			c.MoveToGround(10.5, 64, 50.7, true)
			c.Settle()

			tick(1)
			if tc.spared {
				if n := drainMobEffects(c); n != 0 {
					t.Errorf("the spectator took %d effect packets from the rose, want none", n)
				}
				return
			}
			c.ExpectMobEffectAdded(entityID, registry.MustMobEffect("minecraft:wither"), 0, 40)
		})
	}
}

// TestAWitherRoseSparesAPeacefulWorld checks the flower's other gate: in a
// peaceful world it plants nothing, whatever the body standing in it.
func TestAWitherRoseSparesAPeacefulWorld(t *testing.T) {
	t.Parallel()
	cfg := servertest.EnvironmentConfig()
	cfg.Difficulty = "peaceful"

	srv, connect := servertest.Start(t, cfg)
	tick := servertest.StopTicking(t, srv)
	c := connect()
	c.JoinAs("Farmer")

	rose := servertest.BlockState(t, "minecraft:wither_rose")
	if !srv.SetBlock(10, 64, 50, rose) {
		t.Fatal("the wither rose's chunk was not loaded")
	}
	c.MoveToGround(10.5, 64, 50.7, true)
	c.Settle()

	tick(3)
	if n := drainMobEffects(c); n != 0 {
		t.Errorf("the peaceful world took %d effect packets from the rose, want none", n)
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

// TestSwimmingUnderABlockKeepsBreathing checks the duck: a body standing in
// a channel of water a block tall, its eyes in the stone of the ceiling
// above, is drawn down into the crawl the moment the world no longer fits
// the shape it stands in — and the crawl's eyes, four tenths up, are back
// in the water. The pose refresh runs at a tick's end on the world the
// ticks before saw, so the stone takes its one bite on the first tick;
// after it the eyes drink and the stone never reaches them again.
func TestSwimmingUnderABlockKeepsBreathing(t *testing.T) {
	t.Parallel()
	cfg := servertest.EnvironmentConfig()

	srv, connect := servertest.Start(t, cfg)
	tick := servertest.StopTicking(t, srv)
	c := connect()
	entityID := c.JoinAs("Aqua")
	// The fed body's own regeneration would heal between the bites and
	// blur the rhythm, the way it would in vanilla; a bar below the
	// regeneration line keeps the bites alone.
	srv.SetFood("Aqua", 17)

	stone := servertest.BlockState(t, "minecraft:stone")
	if !srv.SetBlock(0, 64, 50, servertest.BlockState(t, "minecraft:water")) {
		t.Fatal("the channel's chunk was not loaded")
	}
	if !srv.SetBlock(0, 65, 50, stone) {
		t.Fatal("the channel's chunk was not loaded")
	}
	c.MoveToGround(0.5, 64, 50.5, true)
	c.Settle()

	tick(1)
	if got := c.ExpectHealth(); got.Health != 19.0 {
		t.Errorf("after one tick under the stone: health=%v, want 19", got.Health)
	}
	c.ExpectDamageEvent(entityID, "minecraft:in_wall", 0)
	if got := c.ExpectMetadata(); got.Pose != protocol.PoseSwimming {
		t.Errorf("pose = %d, want %d (swimming)", got.Pose, protocol.PoseSwimming)
	}

	// From the second tick the eyes are the crawl's own, down in the
	// water: the breath burns and the stone never reaches them again.
	tick(1)
	first := servertest.ReadBurningMetadata(t, c.ExpectPlay(protocol.IDPlayEntityMetadata))
	if !first.HasAir || first.Air >= rules.MaxAirSupply {
		t.Errorf("air after a tick in the water = %d (carried %v), want it draining below %d",
			first.Air, first.HasAir, rules.MaxAirSupply)
	}
	tick(1)
	second := servertest.ReadBurningMetadata(t, c.ExpectPlay(protocol.IDPlayEntityMetadata))
	if !second.HasAir || second.Air >= first.Air {
		t.Errorf("air after another tick = %d, want it below the %d the tick before held",
			second.Air, first.Air)
	}
	tick(12)
	if n := servertest.DrainDamage(c); n != 0 {
		t.Errorf("%d damage events arrived once the body crawled, want none", n)
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

// TestBumpingTheHeadDoesNotCrush checks the ceiling: a body jumping with its
// head into the block above it has stopped rising, not walked into a wall.
// The eyes' scan vanilla suffocates by is a hair-thin slab across the eyes,
// which the clipped rise leaves a block short of the ceiling.
func TestBumpingTheHeadDoesNotCrush(t *testing.T) {
	t.Parallel()
	cfg := servertest.EnvironmentConfig()

	srv, connect := servertest.Start(t, cfg)
	tick := servertest.StopTicking(t, srv)
	c := connect()
	c.JoinAs("Bonker")
	// The fed body's own regeneration would heal between the bites and
	// blur the rhythm, the way it would in vanilla; a bar below the
	// regeneration line keeps the bites alone.
	srv.SetFood("Bonker", 17)

	// A ceiling two blocks above the floor: exactly the headroom a body
	// standing under it has, so a jump is clipped the twentieth of the way
	// into it.
	stone := servertest.BlockState(t, "minecraft:stone")
	if !srv.SetBlock(10, 66, 60, stone) {
		t.Fatal("the ceiling's chunk was not loaded")
	}
	c.MoveToGround(10.5, 64, 60.5, true)
	c.Settle()

	// The jump: the rise the ceiling clips, held there across a run of
	// survival ticks, and the fall back to the floor. The ticks are the
	// ones that would judge where the head is, and each window is read
	// before the next step, the way a swallow of packets would hide the
	// hurt.
	c.MoveToGround(10.5, 64.2, 60.5, false)
	c.Settle()
	tick(5)
	if n := servertest.DrainDamage(c); n != 0 {
		t.Errorf("%d damage events arrived for a jump into a ceiling, want none", n)
	}
	c.MoveToGround(10.5, 64, 60.5, true)
	c.Settle()
	tick(2)
	if n := servertest.DrainDamage(c); n != 0 {
		t.Errorf("%d damage events arrived for the landing under a ceiling, want none", n)
	}
}

// TestMagmaSears checks the magma block: the ground itself burns the feet
// that stand on it, unless the body is sneaking its way across.
func TestMagmaSears(t *testing.T) {
	t.Parallel()
	cfg := servertest.EnvironmentConfig()

	srv, connect := servertest.Start(t, cfg)
	tick := servertest.StopTicking(t, srv)
	c := connect()
	entityID := c.JoinAs("Sole")
	// The fed body's own regeneration would heal between the bites and
	// blur the rhythm, the way it would in vanilla; a bar below the
	// regeneration line keeps the bites alone.
	srv.SetFood("Sole", 17)

	magma := servertest.BlockState(t, "minecraft:magma_block")
	if !srv.SetBlock(20, 63, 50, magma) {
		t.Fatal("the magma's chunk was not loaded")
	}
	c.MoveToGround(20.5, 64, 50.5, true)
	c.Settle()

	tick(1)
	got := c.ExpectHealth()
	if want := float32(19.0); got.Health != want {
		t.Errorf("after one tick on the magma: health=%v, want %v", got.Health, want)
	}
	c.ExpectDamageEvent(entityID, "minecraft:hot_floor", 0)
	tick(9)
	if n := servertest.DrainDamage(c); n != 0 {
		t.Errorf("%d damage events arrived inside the invulnerability window, want none", n)
	}
	tick(1)
	if got := c.ExpectHealth(); got.Health != 18 {
		t.Errorf("after the second sear: health=%v, want 18", got.Health)
	}
	c.ExpectDamageEvent(entityID, "minecraft:hot_floor", 0)

	// A sneaking body walks it unseared: the key held, the ticks that
	// follow change nothing.
	c.Send(protocol.IDPlayPlayerInput, func(b *protocol.Buffer) { b.PutU8(protocol.InputSneak) })
	c.Settle()
	tick(10)
	if n := servertest.DrainDamage(c); n != 0 {
		t.Errorf("%d damage events arrived under a sneaking body, want the magma to spare them", n)
	}
}

// TestBerryBushPricks checks the berry bush: a bush with berries pricks
// what moves through it, a standing body and a growing bush are left alone.
func TestBerryBushPricks(t *testing.T) {
	t.Parallel()
	cfg := servertest.EnvironmentConfig()

	srv, connect := servertest.Start(t, cfg)
	tick := servertest.StopTicking(t, srv)
	c := connect()
	entityID := c.JoinAs("Picker")

	bush := servertest.BlockStateWith(t, "minecraft:sweet_berry_bush", "age=3")
	if !srv.SetBlock(20, 64, 60, bush) {
		t.Fatal("the bush's chunk was not loaded")
	}
	c.MoveToGround(20.5, 64, 60.5, true)
	c.Settle()

	// The step that arrived here does not keep pricking: the idle report a
	// standing client sends clears the movement the bush measures, and the
	// body that then stands in the bush is left alone.
	c.MoveToGround(20.5, 64, 60.5, true)
	c.Settle()

	// Standing in the bush pricks nobody.
	tick(1)
	if n := servertest.DrainDamage(c); n != 0 {
		t.Errorf("%d damage events arrived for a standing body, want the bush to wait for movement", n)
	}

	// A step through it pricks.
	c.MoveToGround(20.6, 64, 60.5, true)
	c.Settle()
	tick(1)
	got := c.ExpectHealth()
	if want := float32(19.0); got.Health != want {
		t.Errorf("after a step through the bush: health=%v, want %v", got.Health, want)
	}
	c.ExpectDamageEvent(entityID, "minecraft:sweet_berry_bush", 0)
}

// TestASneakingStepThroughABerryBushPricks checks that sneaking spares nobody
// from a bush, and that a body that stops inside it is pricked once.
func TestASneakingStepThroughABerryBushPricks(t *testing.T) {
	t.Parallel()
	cfg := servertest.EnvironmentConfig()

	srv, connect := servertest.Start(t, cfg)
	tick := servertest.StopTicking(t, srv)
	c := connect()
	c.JoinAs("Sneaker")

	bush := servertest.BlockStateWith(t, "minecraft:sweet_berry_bush", "age=3")
	if !srv.SetBlock(20, 64, 60, bush) {
		t.Fatal("the bush's chunk was not loaded")
	}
	c.MoveToGround(20.5, 64, 60.5, true)
	c.Settle()
	servertest.DrainDamage(c)

	c.SendSneak(true)
	c.MoveToGround(20.6, 64, 60.5, true)
	c.Settle()
	tick(1)
	if n := servertest.DrainDamage(c); n != 1 {
		t.Fatalf("%d damage events for a sneaking step through the bush, want 1", n)
	}

	for range 12 {
		c.EndTick()
		c.Settle()
		tick(1)
	}
	if n := servertest.DrainDamage(c); n != 0 {
		t.Errorf("%d damage events for a body standing still in the bush, want 0", n)
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

// TestLavaKillsWithItsOwnWords checks the last page: a body the lava bites
// down to nothing dies the lava's death, the message in chat and on the
// respawn screen both naming it.
func TestLavaKillsWithItsOwnWords(t *testing.T) {
	t.Parallel()
	cfg := servertest.EnvironmentConfig()

	srv, connect := servertest.Start(t, cfg)
	tick := servertest.StopTicking(t, srv)
	c := connect()
	entityID := c.JoinAs("Melted")
	// The fed body's own regeneration would heal between the bites and
	// blur the count, the way it would in vanilla; a bar below the
	// regeneration line keeps the bites alone.
	srv.SetFood("Melted", 17)

	lava := servertest.BlockState(t, "minecraft:lava")
	if !srv.SetBlock(40, 64, 20, lava) {
		t.Fatal("the lava's chunk was not loaded")
	}
	c.MoveToGround(40.5, 64, 20.5, true)
	c.Settle()

	// Five bites of four, ten ticks apart: the bar empty, the death the
	// lava's own. The first bite also lights the flame, whose metadata the
	// later bites leave alone.
	for i := range 5 {
		tick(1)
		if i < 4 {
			tick(9)
		}
		c.ExpectHealth()
		c.ExpectSurvival(protocol.IDPlayDamageEvent)
		if i == 0 {
			// Read past the chunk traffic the move to x=40 still has on
			// its way, an unload among it, which expectMetadata does not.
			c.ExpectSurvival(protocol.IDPlayEntityMetadata)
		}
	}
	lavaMessage := protocol.Text{
		Translate: "death.attack.lava",
		With:      []protocol.Text{servertest.PlayerDisplayComponent("Melted")},
	}
	c.ExpectCombatDeath(entityID, lavaMessage)
	c.ExpectDeathMessage(lavaMessage)
}

// TestLavaWearsOnlyTheChestPiecesFireCanHurt checks the worn-piece wear a
// fire blow spares: a netherite chestplate is damage_resistant to is_fire
// and an elytra is not damage_on_hurt, so neither wears for two lava
// bites; an iron chestplate does.
// vanilla: LivingEntity.doHurtEquipment
func TestLavaWearsOnlyTheChestPiecesFireCanHurt(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		item string
		worn bool
	}{
		{"minecraft:iron_chestplate", true},
		{"minecraft:netherite_chestplate", false},
		{"minecraft:elytra", false},
	} {
		t.Run(tc.item, func(t *testing.T) {
			t.Parallel()
			srv, connect := servertest.Start(t, servertest.EnvironmentConfig())
			tick := servertest.StopTicking(t, srv)
			c := connect()
			c.JoinAs("Singed")
			srv.SetFood("Singed", 17)
			id, ok := registry.ItemID(tc.item)
			if !ok {
				t.Fatalf("no item %s", tc.item)
			}
			srv.GiveSlot("Singed", player.ArmorSlot+1, protocol.ItemStack{ItemID: id, Count: 1})
			if !srv.SetBlock(0, 64, 20, servertest.BlockState(t, "minecraft:lava")) {
				t.Fatal("the lava's chunk was not loaded")
			}
			c.MoveToGround(0.5, 64, 20.5, true)
			c.Settle()
			tick(11)
			got := srv.InventorySlot("Singed", player.ArmorSlot+1)
			if worn := got.Damage > 0; worn != tc.worn {
				t.Errorf("%s Damage = %d after two lava bites, worn = %v, want %v", tc.item, got.Damage, worn, tc.worn)
			}
		})
	}
}

// TestBreathAtTheTopNinthOfASurfaceSource pins the fluid height in the eye
// read: a source with air above reaches 8/9 of its block, so eyes in the
// block's top ninth are out of the water and refill the breath, while eyes
// just under the surface still spend it.
// vanilla: FluidState.getHeight
func TestBreathAtTheTopNinthOfASurfaceSource(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		feet float64 // eyes are 1.62 above; the source surface is at y 65 + 8/9
		want int
	}{
		{"eyes in the top ninth refill", 64.32, 100 + 4*10},
		{"eyes just below the surface drain", 64.2, 100 - 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, connect := servertest.Start(t, servertest.EnvironmentConfig())
			tick := servertest.StopTicking(t, srv)
			c := connect()
			c.JoinAs("Floater")
			srv.SetFood("Floater", 17)

			water := servertest.BlockState(t, "minecraft:water")
			if !srv.SetBlock(0, 64, 50, water) || !srv.SetBlock(0, 65, 50, water) {
				t.Fatal("the water's chunk was not loaded")
			}
			c.MoveToGround(0.5, tc.feet, 50.5, false)
			c.Settle()
			srv.SetPlayerAir("Floater", 100)
			tick(10)
			if got := srv.PlayerAir("Floater"); got != tc.want {
				t.Errorf("the breath after ten ticks = %d, want %d", got, tc.want)
			}
		})
	}
}
