package movement

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
)

// A body that lands on a block with a bounce restitution leaves it with that
// share of its landing speed reversed; stone gives nothing back, and a slower
// landing than a tick of gravity gives nothing either.
// vanilla: Entity.restituteMovementAfterCollisions

func itemVY(t *testing.T, srv *server.Server, id int32) (y, vy float64) {
	t.Helper()
	for _, it := range srv.ItemsInDimension(world.DimensionName) {
		if it.ID == id {
			return it.Y, it.VY
		}
	}
	t.Fatalf("item %d is gone", id)
	return 0, 0
}

// TestAnItemDroppedOnSlimeRebounds drops items ten blocks onto slime and onto
// stone. The slime item climbs back up by a large share of its fall, at 0.8
// of its landing speed because an item is not a living entity; the stone item
// never rises.
func TestAnItemDroppedOnSlimeRebounds(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.BlockTestConfig(), "Dropper")
	srv.StopTick()

	srv.SetBlock(4, 63, 20, servertest.BlockState(t, "minecraft:slime_block"))
	srv.SetBlock(8, 63, 20, servertest.BlockState(t, "minecraft:stone"))
	slimeID := srv.SpawnItemEntityMotion(4.5, 74, 20.5, "minecraft:stone", 1, 0, 0, 0)
	stoneID := srv.SpawnItemEntityMotion(8.5, 74, 20.5, "minecraft:stone", 1, 0, 0, 0)
	if slimeID == 0 || stoneID == 0 {
		t.Fatal("the dropped items did not spawn")
	}

	var landing, rebound, slimePeak, stonePeak float64
	stoneLanded := false
	rest := 64 + entity.ItemHalfSize
	for range 60 {
		_, before := itemVY(t, srv, slimeID)
		srv.TickEntities(1)
		y, after := itemVY(t, srv, slimeID)
		if landing == 0 && before < 0 && after > 0 {
			landing, rebound = before, after
		}
		slimePeak = math.Max(slimePeak, y-rest)
		if sy, _ := itemVY(t, srv, stoneID); sy-rest < 0.01 {
			stoneLanded = true
		} else if stoneLanded {
			stonePeak = math.Max(stonePeak, sy-rest)
		}
	}
	if landing == 0 {
		t.Fatal("the slime item never rebounded")
	}
	if want := 0.8 * -landing; math.Abs(rebound-want) > 0.1*want {
		t.Errorf("the item left the slime at %v after landing at %v, want about %v", rebound, landing, want)
	}
	if slimePeak < 3 {
		t.Errorf("the slime item peaked %v above the surface, want a visible rebound", slimePeak)
	}
	if stonePeak > 0.2 {
		t.Errorf("the stone item peaked %v above the surface, want it to stay down", stonePeak)
	}
}

// TestACowDroppedOnABedBouncesAtThreeQuarters drops a cow onto a bed, a straw
// bed and stone: the living body leaves the bed at 0.75 of its landing speed,
// and the straw bed, with no bounceRestitution, holds it down like stone.
func TestACowDroppedOnABedBouncesAtThreeQuarters(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.BlockTestConfig(), "Dropper")
	srv.StopTick()

	srv.SetBlock(4, 63, 20, servertest.BlockState(t, "minecraft:red_bed"))
	srv.SetBlock(8, 63, 20, servertest.BlockState(t, "minecraft:stone"))
	srv.SetBlock(12, 63, 20, servertest.BlockState(t, "minecraft:straw_bed"))
	bedCow := srv.SpawnMob("minecraft:cow", 4.5, 70, 20.5)
	stoneCow := srv.SpawnMob("minecraft:cow", 8.5, 70, 20.5)
	strawCow := srv.SpawnMob("minecraft:cow", 12.5, 70, 20.5)
	if bedCow == 0 || stoneCow == 0 || strawCow == 0 {
		t.Fatal("the cows did not spawn")
	}

	var landing, rebound, stoneRise, strawRise float64
	strawWalked := false
	for range 60 {
		_, before, _, _ := srv.MobVelocity(bedCow)
		srv.TickMobs(1)
		_, after, _, _ := srv.MobVelocity(bedCow)
		if landing == 0 && before < 0 && after > 0 {
			landing, rebound = before, after
		}
		_, sv, _, _ := srv.MobVelocity(stoneCow)
		stoneRise = math.Max(stoneRise, sv)
		// The straw cow is read only until its AI first walks it: a wander
		// that steps or jumps off the bed is not a bounce.
		if wx, wv, wz, _ := srv.MobVelocity(strawCow); wx != 0 || wz != 0 {
			strawWalked = true
		} else if !strawWalked {
			strawRise = math.Max(strawRise, wv)
		}
	}
	if landing == 0 {
		t.Fatal("the cow never bounced off the bed")
	}
	if want := 0.75 * -landing; math.Abs(rebound-want) > 0.1*want {
		t.Errorf("the cow left the bed at %v after landing at %v, want about %v", rebound, landing, want)
	}
	if stoneRise > 0.01 {
		t.Errorf("the cow on stone rose at %v, want it to stay down", stoneRise)
	}
	if strawRise > 0.01 {
		t.Errorf("the cow on a straw bed rose at %v, want it to stay down", strawRise)
	}
}
