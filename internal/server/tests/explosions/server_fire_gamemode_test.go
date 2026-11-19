package explosions

import (
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// The burn across a game mode switch and the blocks that put it out: a
// survival body switched to a mode that cannot be hurt loses its flame where
// vanilla's Player.setRemainingFireTicks holds the count at one, a burning
// invulnerable body in water is doused like anyone else, and a water or
// powder-snow cauldron puts the body out and drops a level.

// TestInvulnerableModeEndsABurn checks the mode switch while burning: a body
// that takes a fifteen-second lava burn in survival and is switched to a
// mode that cannot be hurt loses the flame within the couple of ticks
// vanilla gives it — Player.setRemainingFireTicks holds an invulnerable
// player's positive count at one — so the on-fire bit leaves the metadata.
func TestInvulnerableModeEndsABurn(t *testing.T) {
	t.Parallel()
	modes := []struct {
		name string
		id   int32
	}{
		{"creative", protocol.GameModeCreative},
		{"spectator", protocol.GameModeSpectator},
	}
	for _, mode := range modes {
		t.Run(mode.name, func(t *testing.T) {
			cfg := servertest.EnvironmentConfig()

			srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Flicker"))
			tick := servertest.StopTicking(t, srv)
			c := connect()
			entityID := c.JoinAs("Flicker")

			// The lava lights the whole fifteen-second burn, the flame
			// showing for the survival body.
			lava := servertest.BlockState(t, "minecraft:lava")
			if !srv.SetBlock(0, 64, 70, lava) {
				t.Fatal("the lava's chunk was not loaded")
			}
			c.MoveToGround(0.5, 64, 70.5, true)
			c.Settle()
			tick(1)
			c.Settle()
			if fire := srv.PlayerFire("Flicker"); fire <= 0 {
				t.Fatalf("the lava left %d fire ticks, want a burn to carry", fire)
			}

			// Out of the lava and into the mode that cannot be hurt: the
			// burn runs its last ticks out and the flame goes off the
			// metadata.
			c.MoveToGround(0.5, 64, 72.5, true)
			c.Settle()
			c.SendChangeGameMode(mode.id)
			servertest.WaitFor(t, srv, 1200, func() bool { return srv.PlayerGameMode("Flicker") == mode.id })

			tick(3)
			doused := false
			c.SettleAndCollect(func(id int32, data []byte) {
				if id == protocol.IDPlayEntityMetadata {
					m := servertest.ReadBurningMetadata(t, data)
					if m.EntityID == entityID && m.Flags&protocol.EntityFlagOnFire == 0 {
						doused = true
					}
				}
			})
			if !doused {
				t.Errorf("the %s body never lost the on-fire bit after the switch", mode.name)
			}
			if fire := srv.PlayerFire("Flicker"); fire > 0 {
				t.Errorf("the %s body kept %d fire ticks, want the burn cleared", mode.name, fire)
			}
		})
	}
}

// TestInvulnerableBurnGoesOutInWater checks the water on a body that cannot
// be hurt: a burning creative body that walks into water is doused like
// anyone else, the on-fire bit leaving the metadata where the water reaches
// every mode's box.
func TestInvulnerableBurnGoesOutInWater(t *testing.T) {
	t.Parallel()
	cfg := servertest.EnvironmentConfig()
	cfg.GameMode = "creative"

	srv, connect := servertest.Start(t, cfg)
	tick := servertest.StopTicking(t, srv)
	c := connect()
	entityID := c.JoinAs("Quenched")
	c.MoveToGround(0.5, 64, 74.5, true)
	c.Settle()

	// A burn the mode cannot feel, set the way a switch from survival or a
	// flame-tipped arrow leaves it: the server shows the flame.
	srv.SetPlayerFire("Quenched", 100)
	tick(1)
	c.Settle()

	// Into water: the flame leaves the metadata on the tick.
	water := servertest.BlockState(t, "minecraft:water")
	if !srv.SetBlock(0, 64, 74, water) || !srv.SetBlock(0, 65, 74, water) {
		t.Fatal("the water's chunk was not loaded")
	}
	c.Settle()
	tick(1)
	doused := false
	c.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlayEntityMetadata {
			m := servertest.ReadBurningMetadata(t, data)
			if m.EntityID == entityID && m.Flags&protocol.EntityFlagOnFire == 0 {
				doused = true
			}
		}
	})
	if !doused {
		t.Error("a creative body in water kept the on-fire bit, want the water to put it out")
	}
	if fire := srv.PlayerFire("Quenched"); fire > 0 {
		t.Errorf("the creative body in water kept %d fire ticks, want the water to put it out", fire)
	}
}

// TestWaterCauldronDousesABurningPlayer checks the layered cauldrons: a
// burning body inside a water cauldron loses its fire and drops the cauldron
// a level, and a powder-snow cauldron melts to the water cauldron below it —
// vanilla's LayeredCauldronBlock.entityInside.
func TestWaterCauldronDousesABurningPlayer(t *testing.T) {
	t.Parallel()
	cauldrons := []struct {
		name      string
		block     string
		level     int
		wantBlock string
		wantLevel int
	}{
		{"water", "minecraft:water_cauldron", 3, "minecraft:water_cauldron", 2},
		{"water to empty", "minecraft:water_cauldron", 1, "minecraft:cauldron", 0},
		{"powder snow", "minecraft:powder_snow_cauldron", 2, "minecraft:water_cauldron", 1},
	}
	for _, cauldron := range cauldrons {
		t.Run(cauldron.name, func(t *testing.T) {
			cfg := servertest.EnvironmentConfig()

			srv, connect := servertest.Start(t, cfg)
			tick := servertest.StopTicking(t, srv)
			c := connect()
			entityID := c.JoinAs("Soaked")
			c.MoveToGround(0.5, 64, 76.5, true)
			c.Settle()

			// A burn the server shows, so the douse has an on-state to
			// clear.
			srv.SetPlayerFire("Soaked", 200)
			tick(1)
			c.Settle()

			// The cauldron in the cell the body's own box stands in.
			state := servertest.BlockStateWith(t, cauldron.block, "level="+strconv.Itoa(cauldron.level))
			if !srv.SetBlock(0, 64, 76, state) {
				t.Fatal("the cauldron's chunk was not loaded")
			}
			c.Settle()
			tick(1)
			doused := false
			c.SettleAndCollect(func(id int32, data []byte) {
				if id == protocol.IDPlayEntityMetadata {
					m := servertest.ReadBurningMetadata(t, data)
					if m.EntityID == entityID && m.Flags&protocol.EntityFlagOnFire == 0 {
						doused = true
					}
				}
			})
			if !doused {
				t.Error("the cauldron did not put the burn out")
			}
			if fire := srv.PlayerFire("Soaked"); fire > 0 {
				t.Errorf("the burning body kept %d fire ticks in the cauldron, want it put out", fire)
			}
			want := servertest.BlockState(t, cauldron.wantBlock)
			if cauldron.wantLevel > 0 {
				want = servertest.BlockStateWith(t, cauldron.wantBlock, "level="+strconv.Itoa(cauldron.wantLevel))
			}
			if got := srv.BlockAt(0, 64, 76); got != want {
				name, props, _ := registry.BlockStateName(got)
				t.Errorf("the cauldron became %s %s, want %s level %d",
					name, props, cauldron.wantBlock, cauldron.wantLevel)
			}
		})
	}
}

// extinguishSoundID is the wire ID of the generic extinguish sound, for the
// tests that count the fizzes a run of ticks sent.
func extinguishSoundID(t *testing.T) int32 {
	t.Helper()
	id, ok := registry.SoundEventID("minecraft:entity.generic.extinguish_fire")
	if !ok {
		t.Fatal("no sound event for the generic extinguish")
	}
	return id + 1
}

// TestCauldronDoesNotFizzForTheFiresOwnFlame checks the sound's gate against
// the block pass: a body standing half in a water cauldron and half in fire
// is lit by the fire block every tick the cauldron takes the burn back,
// which is no burn for the extinguish fizz — vanilla plays it only for a
// burn the blocks found. The cauldron still drops its levels.
// vanilla: Entity.applyEffectsFromBlocks, LayeredCauldronBlock.handleEntityOnFireInside
func TestCauldronDoesNotFizzForTheFiresOwnFlame(t *testing.T) {
	t.Parallel()
	cfg := servertest.EnvironmentConfig()

	srv, connect := servertest.Start(t, cfg)
	tick := servertest.StopTicking(t, srv)
	c := connect()
	c.JoinAs("Halfway")

	// A water cauldron and a fire in the two cells the body's own box
	// spans: at x=1.0 the box reaches into both. The chunk sender loads
	// the cells around the body off the read loop, so the writes wait for
	// the chunks to arrive.
	cauldron := servertest.BlockStateWith(t, "minecraft:water_cauldron", "level=3")
	fire := servertest.BlockState(t, "minecraft:fire")
	servertest.WaitFor(t, srv, 1200, func() bool { return srv.SetBlock(0, 64, 78, cauldron) })
	servertest.WaitFor(t, srv, 1200, func() bool { return srv.SetBlock(1, 64, 78, fire) })
	c.MoveToGround(1.0, 64, 78.5, true)
	c.Settle()

	fizz := 0
	extinguish := extinguishSoundID(t)
	tick(30)
	c.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlaySoundEffect {
			return
		}
		if r := protocol.NewReader(data); r.ReadVarInt() == extinguish {
			fizz++
		}
	})
	if fizz != 0 {
		t.Errorf("the half-cauldron body fizzed %d times over thirty ticks, want none for the fire's own flame", fizz)
	}
}

// TestSpectatorCauldronLeavesItAlone checks the block-effects gate: a
// spectator has no block effects, so a water cauldron under their flame holds
// its level where a survival body's would drop.
// vanilla: LayeredCauldronBlock.handleEntityOnFireInside, Entity.isAffectedByBlocks
func TestSpectatorCauldronLeavesItAlone(t *testing.T) {
	t.Parallel()
	cfg := servertest.EnvironmentConfig()

	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Ghost"))
	tick := servertest.StopTicking(t, srv)
	c := connect()
	c.JoinAs("Ghost")
	c.MoveToGround(0.5, 64, 80.5, true)
	c.Settle()

	c.SendChangeGameMode(protocol.GameModeSpectator)
	servertest.WaitFor(t, srv, 1200, func() bool { return srv.PlayerGameMode("Ghost") == protocol.GameModeSpectator })

	cauldron := servertest.BlockStateWith(t, "minecraft:water_cauldron", "level=3")
	servertest.WaitFor(t, srv, 1200, func() bool { return srv.SetBlock(0, 64, 80, cauldron) })
	srv.SetPlayerFire("Ghost", 200)
	c.Settle()
	tick(1)
	c.SettleAndCollect(func(int32, []byte) {})
	if got := srv.BlockAt(0, 64, 80); got != cauldron {
		name, props, _ := registry.BlockStateName(got)
		t.Errorf("a spectator's cauldron became %s %s, want the level 3 held", name, props)
	}
}

// TestCauldronIgnoresEmptyAndLava checks the fills left out: the empty and
// lava cauldrons carry no water or powder snow to drop, so a burning body
// standing in one keeps its flame and the block is left alone.
// vanilla: LayeredCauldronBlock
func TestCauldronIgnoresEmptyAndLava(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"minecraft:cauldron", "minecraft:lava_cauldron"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cfg := servertest.EnvironmentConfig()

			srv, connect := servertest.Start(t, cfg)
			tick := servertest.StopTicking(t, srv)
			c := connect()
			c.JoinAs("Unmoved")
			c.MoveToGround(0.5, 64, 82.5, true)
			c.Settle()

			state := servertest.BlockState(t, name)
			servertest.WaitFor(t, srv, 1200, func() bool { return srv.SetBlock(0, 64, 82, state) })
			srv.SetPlayerFire("Unmoved", 200)
			c.Settle()
			tick(1)
			c.SettleAndCollect(func(int32, []byte) {})
			if fire := srv.PlayerFire("Unmoved"); fire <= 0 {
				t.Errorf("the %s put the burn out, want it left alone", name)
			}
			if got := srv.BlockAt(0, 64, 82); got != state {
				bn, props, _ := registry.BlockStateName(got)
				t.Errorf("the %s became %s %s, want it left alone", name, bn, props)
			}
		})
	}
}
