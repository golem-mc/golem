package advancements

import (
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// beaconAt places a beacon at (4,64,0) on a pyramid of the given number of
// iron layers, with the clock stepped by hand, and steps to the cycle its
// pyramid is counted on. The returned clients are one beside the beacon and
// one far outside its reach.
func beaconAt(t *testing.T, layers int32) (srv *server.Server, near, far *servertest.WindowedClient) {
	t.Helper()
	clock := servertest.NewStepClock()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig(), func(s *server.Server) {
		s.SetTickClock(clock, nil)
		s.SetKeepAlive(time.Hour, time.Hour)
	})
	near = servertest.WindowedOf(t, "Near", connect())
	far = servertest.WindowedOf(t, "Far", connect())
	far.MoveTo(40.5, 64, 0.5, 0, 0)
	far.Settle()
	iron := registry.BlockStates["minecraft:iron_block"]
	for step := int32(1); step <= layers; step++ {
		for dx := -step; dx <= step; dx++ {
			for dz := -step; dz <= step; dz++ {
				if !srv.SetBlock(4+dx, 64-step, dz, iron) {
					t.Fatalf("the chunk at (%d,%d,%d) was not loaded for the pyramid", 4+dx, 64-step, dz)
				}
			}
		}
	}
	near.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:beacon", 1))
	near.PlaceBlock(4, 63, 0, protocol.FaceTop)
	near.SettleAndCollect(func(int32, []byte) {})
	servertest.StepTicks(t, srv, clock, 1)
	near.ExpectBlockAck()
	for {
		servertest.StepTicks(t, srv, clock, 1)
		if age, _ := srv.Time(); age%80 == 0 {
			return srv, near, far
		}
	}
}

// TestALitBeaconObtainsItsConstructionForPlayersInReach: a beacon coming
// alive over a pyramid of one layer is a construct_beacon at level 1 for the
// player beside it and not for the one forty blocks off, and does not pass
// the level-4 criterion.
// vanilla: BeaconBlockEntity.tick, ConstructBeaconTrigger
func TestALitBeaconObtainsItsConstructionForPlayersInReach(t *testing.T) {
	t.Parallel()
	srv, _, _ := beaconAt(t, 1)
	if !srv.AdvancementObtained("Near", "minecraft:nether/create_beacon", "beacon") {
		t.Error("lighting a beacon did not obtain create_beacon for the player beside it")
	}
	if srv.AdvancementObtained("Far", "minecraft:nether/create_beacon", "beacon") {
		t.Error("lighting a beacon obtained create_beacon for a player forty blocks away")
	}
	if srv.AdvancementObtained("Near", "minecraft:nether/create_full_beacon", "beacon") {
		t.Error("a one-layer pyramid obtained create_full_beacon")
	}
}

// TestALitBeaconOnAFullPyramidObtainsBeaconator: level 4 passes both
// construction criteria.
// vanilla: ConstructBeaconTrigger.TriggerInstance.matches
func TestALitBeaconOnAFullPyramidObtainsBeaconator(t *testing.T) {
	t.Parallel()
	srv, _, _ := beaconAt(t, 4)
	if !srv.AdvancementObtained("Near", "minecraft:nether/create_full_beacon", "beacon") {
		t.Error("a four-layer pyramid did not obtain create_full_beacon")
	}
	if !srv.AdvancementObtained("Near", "minecraft:nether/create_beacon", "beacon") {
		t.Error("a four-layer pyramid did not obtain create_beacon")
	}
}
