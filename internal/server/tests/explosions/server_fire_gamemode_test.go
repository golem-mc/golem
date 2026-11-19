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
