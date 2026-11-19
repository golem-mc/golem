package movement

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"

	"github.com/golem-mc/golem/internal/server/world"
)

// A burning body in powder snow is put out, and the block melts for a player
// and for a mob only while mob_griefing is on.
// vanilla: PowderSnowBlock.entityInside

func TestPowderSnowPutsOutAndMeltsForABurningPlayer(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.EnvironmentConfig())
	tick := servertest.StopTicking(t, srv)
	c := connect()
	c.JoinAs("Smoulder")
	c.MoveToGround(0.5, 64, 90.5, true)
	c.Settle()

	servertest.WaitFor(t, srv, 1200, func() bool { return srv.SetBlock(0, 64, 90, servertest.BlockState(t, "minecraft:powder_snow")) })
	srv.SetPlayerFire("Smoulder", 200)
	tick(1)
	c.Settle()

	if fire := srv.PlayerFire("Smoulder"); fire > 0 {
		t.Errorf("the burning player kept %d fire ticks in powder snow, want it put out", fire)
	}
	if srv.BlockAt(0, 64, 90) != 0 {
		t.Error("the powder snow stayed where a burning player stood in it, want it melted")
	}
}

func TestPowderSnowLeavesACoolPlayerAlone(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.EnvironmentConfig())
	tick := servertest.StopTicking(t, srv)
	c := connect()
	c.JoinAs("Cool")
	c.MoveToGround(0.5, 64, 92.5, true)
	c.Settle()

	snow := servertest.BlockState(t, "minecraft:powder_snow")
	servertest.WaitFor(t, srv, 1200, func() bool { return srv.SetBlock(0, 64, 92, snow) })
	tick(3)
	c.Settle()
	if srv.BlockAt(0, 64, 92) != snow {
		t.Error("the powder snow melted under a player who was not burning")
	}
}

func TestPowderSnowPutsOutABurningMobAndMeltsByTheRule(t *testing.T) {
	t.Parallel()
	for _, grief := range []bool{true, false} {
		name := "griefing off"
		if grief {
			name = "griefing on"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.MobTestConfig(), "Warden", servertest.OpsListing(t, "Warden"))
			c.Settle()
			srv.SetDayTime(servertest.Midnight)
			srv.SetGameRule(world.RuleMobGriefing.String(), grief)

			cow := srv.SpawnMob("minecraft:cow", 4.5, 64, 0.5)
			c.ExpectMobSpawn(protocol.EntityTypeCow)
			srv.StopTick()
			c.Settle()
			if !srv.SetMobNoAI(cow) {
				t.Fatal("the cow could not be held")
			}
			snow := servertest.BlockState(t, "minecraft:powder_snow")
			if !srv.SetBlock(4, 64, 0, snow) {
				t.Fatal("the chunk was not loaded")
			}
			srv.SetMobFire(cow, 200)
			srv.TickMobs(1)
			c.Settle()

			if fire := srv.MobFire(cow); fire > 0 {
				t.Errorf("the burning cow kept %d fire ticks in powder snow, want it put out", fire)
			}
			melted := srv.BlockAt(4, 64, 0) != snow
			if melted != grief {
				t.Errorf("powder snow melted = %v under a burning cow with mob_griefing %v", melted, grief)
			}
		})
	}
}

// A burning arrow at 3 blocks a tick passes a powder snow cell that neither
// its start box (x 7.75-8.25) nor its end box (x 4.75-5.25) overlaps: the
// cells swept between them put it out, melting the snow while mob_griefing
// is on.
// vanilla: AbstractArrow.stepMoveAndHit, Entity.checkInsideBlocks,
// BlockGetter.forEachBlockIntersectedBetween
func TestPowderSnowPutsOutAnArrowFlyingThroughIt(t *testing.T) {
	t.Parallel()
	for _, grief := range []bool{true, false} {
		name, want := "griefing off", "minecraft:powder_snow"
		if grief {
			name, want = "griefing on", "minecraft:air"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv, _ := servertest.AttachActor(t)
			srv.StopTick()
			srv.SetGameRule(world.RuleMobGriefing.String(), grief)
			if !srv.SetBlock(6, 65, 0, servertest.BlockState(t, "minecraft:powder_snow")) {
				t.Fatal("could not place the powder snow")
			}
			id := srv.ShootArrow(8.0, 65.2, 0.5, -3, 0, 0)
			srv.IgniteArrow(id, 100)
			servertest.DriveTargetTicks(srv, 1)

			if fire := srv.ArrowFire(id); fire != 0 {
				t.Errorf("the arrow kept %d fire ticks past the powder snow, want 0", fire)
			}
			servertest.ExpectBlockNamed(t, srv, 6, 65, 0, want, nil)
		})
	}
}

// A burning arrow is put out by a water cell it flies through, though
// neither its start box nor its end box touches the water, and keeps its
// fire when it passes over a low flowing-water surface without meeting it.
// vanilla: Entity.collidedWithFluid
func TestWaterPutsOutAnArrowFlyingThroughIt(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, props string
		y           float64
		out         bool
	}{
		{"source", "level=0", 65.2, true},
		{"over a low surface", "level=7", 65.5, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			srv, _ := servertest.AttachActor(t)
			srv.StopTick()
			// level=7 stands 1/9 of a block high: below the arrow's box at 65.5.
			if !srv.SetBlock(6, 65, 0, servertest.BlockStateWith(t, "minecraft:water", c.props)) {
				t.Fatal("could not place the water")
			}
			id := srv.ShootArrow(8.0, c.y, 0.5, -3, 0, 0)
			srv.IgniteArrow(id, 100)
			servertest.DriveTargetTicks(srv, 1)

			fire := srv.ArrowFire(id)
			if c.out && fire != 0 {
				t.Errorf("the arrow kept %d fire ticks past the water, want 0", fire)
			}
			if !c.out && fire == 0 {
				t.Error("the arrow passing over the water was put out")
			}
		})
	}
}
