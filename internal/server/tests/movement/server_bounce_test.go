package movement

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
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

// TestACowDroppedOnAShelfMushroomBouncesWithItsSound drops a cow onto a
// shelf mushroom: it bounces at the bed's 0.75 and the block's bounce sound
// plays at the mushroom.
// vanilla: Blocks.SHELF_MUSHROOM bounceRestitution, ShelfMushroomBlock.bounceOn
func TestACowDroppedOnAShelfMushroomBouncesWithItsSound(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Dropper")
	srv.StopTick()

	srv.SetBlock(4, 63, 21, servertest.BlockState(t, "minecraft:stone"))
	srv.SetBlock(4, 63, 20, servertest.MustState(t, "minecraft:shelf_mushroom", map[string]string{"facing": "north", "age": "0"}))
	cow := srv.SpawnMob("minecraft:cow", 4.5, 70, 20.5)
	if cow == 0 {
		t.Fatal("the cow did not spawn")
	}
	c.MoveToGround(4.5, 64, 15.5, true) // within the sound's 16 blocks
	c.Settle()

	var landing, rebound float64
	for range 60 {
		_, before, _, _ := srv.MobVelocity(cow)
		srv.TickMobs(1)
		_, after, _, _ := srv.MobVelocity(cow)
		if before < 0 && after > 0 {
			landing, rebound = before, after
			break
		}
	}
	if landing == 0 {
		t.Fatal("the cow never bounced off the shelf mushroom")
	}
	// The rebound has had the rest of the tick's gravity and drag.
	if rebound < 0.6*-landing || rebound > 0.75*-landing {
		t.Errorf("the cow left the shelf mushroom at %v after landing at %v, want 0.6 to 0.75 of it", rebound, landing)
	}

	bounce, _ := registry.SoundEventID("minecraft:block.shelf_mushroom.bounce")
	var heard bool
	c.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlaySoundEffect {
			return
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt()-1 != bounce {
			return
		}
		heard = true
		r.ReadVarInt()
		if x, y, z := r.ReadI32(), r.ReadI32(), r.ReadI32(); x != 4*8+4 || y != 63*8+4 || z != 20*8+4 {
			t.Errorf("bounce sound at fixed (%d, %d, %d), want the mushroom's centre", x, y, z)
		}
	})
	if !heard {
		t.Error("the cow's bounce played no shelf mushroom bounce sound")
	}
}

// TestBodiesLandingOnTheEdgeOfSlimeBounce lands a cow and an item with their
// centres over air and part of their box on a slime block.
func TestBodiesLandingOnTheEdgeOfSlimeBounce(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.BlockTestConfig(), "Dropper")
	srv.StopTick()

	srv.SetBlock(4, 63, 20, servertest.BlockState(t, "minecraft:air"))
	srv.SetBlock(14, 63, 20, servertest.BlockState(t, "minecraft:air"))
	srv.SetBlock(5, 63, 20, servertest.BlockState(t, "minecraft:slime_block"))
	srv.SetBlock(15, 63, 20, servertest.BlockState(t, "minecraft:slime_block"))
	cow := srv.SpawnMob("minecraft:cow", 4.95, 70, 20.5)
	item := srv.SpawnItemEntityMotion(14.99, 74, 20.5, "minecraft:stone", 1, 0, 0, 0)
	if cow == 0 || item == 0 {
		t.Fatal("the bodies did not spawn")
	}

	var cowUp, itemUp bool
	for range 60 {
		srv.TickMobs(1)
		srv.TickEntities(1)
		if _, vy, _, _ := srv.MobVelocity(cow); vy > 0.3 {
			cowUp = true
		}
		if _, vy := itemVY(t, srv, item); vy > 0.3 {
			itemUp = true
		}
	}
	if !cowUp {
		t.Error("the cow landing on the edge of slime did not bounce")
	}
	if !itemUp {
		t.Error("the item landing on the edge of slime did not bounce")
	}
}

// A boat dropped onto a shelf mushroom whose backing block lies under the
// far part of its hull rests on stone backing and rebounds off slime backing;
// one clear of the backing bounces on the mushroom.
// vanilla: Entity.getOnPos, mainSupportingBlockPos, restituteMovementAfterCollisions
func TestABoatBesideAShelfMushroomBackingLandsOnTheBacking(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		backing string
		z       float64
		wantY   float64 // after 20 ticks; 0 means any height above the floor
		bounce  bool
	}{
		{"stone_backed", "minecraft:stone", 4.5, 201, false},
		{"stone_clear", "minecraft:stone", 4.0, 0, true},
		{"slime_backed", "minecraft:slime_block", 4.5, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := servertest.BlockTestConfig()
			cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
			srv, _ := servertest.Join(t, cfg, "Dropper")
			servertest.HoldTicks(t, srv)
			stone := servertest.BlockState(t, "minecraft:stone")
			for x := int32(-2); x <= 2; x++ {
				for z := int32(2); z <= 7; z++ {
					srv.SetBlock(x, 199, z, stone)
				}
			}
			srv.SetBlock(0, 200, 4, servertest.MustState(t, "minecraft:shelf_mushroom", map[string]string{"facing": "north", "age": "0"}))
			srv.SetBlock(0, 200, 5, servertest.BlockState(t, tc.backing))
			id := srv.SpawnBoat(0.5, 205.1875, tc.z, "minecraft:oak_boat")
			bounced := false
			for range 20 {
				servertest.RunTicks(t, srv, 1)
				if _, vy, _, _ := srv.BoatVelocity(id); vy > 0 {
					bounced = true
				}
			}
			_, y, _, _, grounded, ok := srv.BoatState(id)
			_, vy, _, _ := srv.BoatVelocity(id)
			if !ok {
				t.Fatal("the boat is gone")
			}
			if bounced != tc.bounce {
				t.Errorf("boat bounced = %v, want %v", bounced, tc.bounce)
			}
			if tc.wantY != 0 && (y != tc.wantY || !grounded || vy != 0) {
				t.Errorf("boat at Y %v, grounded %v, VY %v; want Y %v, grounded, VY 0", y, grounded, vy, tc.wantY)
			}
		})
	}
}
