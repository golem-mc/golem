package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// TestACowLuredFromTheMiddleOfAPondWalksOut plants a cow in the middle of a
// pond two deep and seven wide and holds a lure on the bank beyond it. The
// water is over the body's head everywhere but its top cell, so without the
// canFloat half of vanilla's WalkNodeEvaluator.getFloorLevel the surface is
// no node, no path leaves the pond and the cow stands still; a body that may
// float reads the water's own half-block top as the floor under its feet and
// walks the surface to the bank.
func TestACowLuredFromTheMiddleOfAPondWalksOut(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.MobTestConfig(), "Herder")

	// The pond straddles the chunk border, so every chunk it covers is held
	// for the write; SetBlock refuses a column the world does not hold.
	water := servertest.BlockState(t, "minecraft:water")
	for _, at := range [][2]int32{{-3, -3}, {-3, 3}, {3, -3}, {3, 3}} {
		release := srv.HoldChunk(at[0], at[1])
		t.Cleanup(release)
	}
	for x := int32(-3); x <= 3; x++ {
		for z := int32(-3); z <= 3; z++ {
			if !srv.SetBlock(x, 63, z, water) || !srv.SetBlock(x, 62, z, water) {
				t.Fatal("the world did not take the pond")
			}
		}
	}

	srv.GiveSlot("Herder", player.HotbarSlot,
		protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:wheat"), Count: 1})

	// The bank, three blocks past the pond's eastern lip, a walk longer
	// than the stop distance so the cow has to leave the water.
	c.MoveToGrounded(7.5, 64, 0.5, 90, 0, true)
	c.Settle()
	servertest.HoldTicks(t, srv)

	// A cow in the middle, its feet in the pond's top cell. Adult whatever
	// the spawn's dice chose, so the walk is the lure's and not a calf's.
	cow := srv.SpawnMob("minecraft:cow", 0.5, 63, 0.5)
	if cow == 0 {
		t.Fatal("SpawnMob came to nothing")
	}
	srv.SetMobBaby(cow, false)
	c.ExpectMobSpawn(protocol.EntityTypeCow)

	// The walk is watched for the block cell the cow's feet stand in: the
	// pond's water is x -3..3, so the cow is out once that cell is on the
	// bank at x>=4.
	startX, startY, startZ, ok := srv.MobPos(cow)
	if !ok {
		t.Fatal("the cow is gone from the world")
	}
	if math.Floor(startX) >= 4 {
		t.Fatalf("the cow spawned already out of the pond at %.2f, want it planted in the middle", startX)
	}
	out := c.PumpTicks(srv, 400, func(int32, []byte) bool {
		x, _, _, ok := srv.MobPos(cow)
		return ok && math.Floor(x) >= 4
	})
	if !out {
		x, y, z, _ := srv.MobPos(cow)
		t.Fatalf("the cow stands at %.2f,%.2f,%.2f %d ticks after the lure, want it out of the pond on the bank",
			x, y, z, 400)
	}
	t.Logf("the cow walked from %.2f,%.2f,%.2f out to the bank", startX, startY, startZ)
}

// TestAMobStepsOffTheShelfToReachItsPrey walks the walk-off over the wire: a
// mob on a shelf one block over the flat floor must come straight off the
// shelf's front edge to its prey rather than around the side. A wide body's
// footprint reaches past the lip, so the cell at the foot is only a step
// where a floor under the footprint's far cells counts — vanilla's
// WalkNodeEvaluator.getPathTypeOfMob — while the target's own width rule is
// unchanged. The grass case lays short grass over the foot and the lip, which
// a non-colliding block must not turn back into an obstacle.
func TestAMobStepsOffTheShelfToReachItsPrey(t *testing.T) {
	t.Parallel()
	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the block registry has no stone")
	}
	grass, ok := registry.FindState("minecraft:short_grass", nil)
	if !ok {
		t.Fatal("the block registry has no short grass")
	}

	for _, kind := range []struct {
		name  string
		kind  string
		etype int32
	}{
		{"spider", "minecraft:spider", protocol.EntityTypeSpider},
		{"zombie", "minecraft:zombie", protocol.EntityTypeZombie},
	} {
		for _, plant := range []bool{false, true} {
			name := kind.name
			if plant {
				name += " through grass"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				srv, connect := servertest.Start(t, servertest.HostileTestConfig())
				srv.SetDayTime(18000) // midnight, whose dark is no daylight burn
				c := connect()
				c.JoinAs("Prey")

				// A shelf two cells wide and three deep, its top one block
				// over the flat floor's feet.
				for x := int32(1); x <= 2; x++ {
					for z := int32(-1); z <= 1; z++ {
						srv.SetBlock(x, 64, z, stone)
					}
				}
				if plant {
					srv.SetBlock(0, 64, 0, grass)
					srv.SetBlock(1, 65, 0, grass)
				}
				c.MoveToGround(-0.5, 64, 0.5, true)
				c.Settle()
				servertest.HoldTicks(t, srv)

				id := srv.SpawnMob(kind.kind, 1.5, 65, 0.5)
				c.ExpectMobSpawn(kind.etype)

				// Only the ticks before the mob first reaches the foot
				// count: the farthest it strays from the line to the prey
				// is the tell between straight off the edge and around
				// the side.
				maxZ, arrived := 0.5, false
				c.PumpTicks(srv, 300, func(int32, []byte) bool {
					x, y, z, ok := srv.MobPos(id)
					if !ok {
						return false
					}
					if !arrived {
						maxZ = math.Max(maxZ, z)
					}
					if y < 65 && math.Hypot(x-(-0.5), z-0.5) < 1.5 {
						arrived = true
						return true
					}
					return false
				})
				if !arrived {
					x, y, z, _ := srv.MobPos(id)
					t.Fatalf("the mob never came down to the foot; it stands at %.2f,%.2f,%.2f", x, y, z)
				}
				if maxZ > 1.0 {
					t.Errorf("the mob strayed to z=%.2f on its way down, want it straight off the shelf's front edge", maxZ)
				}
			})
		}
	}
}
