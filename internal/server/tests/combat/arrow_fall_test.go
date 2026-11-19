package combat

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The shaft let go of the block it stuck in: broken outright, and carried
// off by a piston. The arrow is watched through the wire from the draw to
// the pickup, the way the skeleton tests watch one.

// awaitArrowFalls reads the arrow's own metadata and motion until its
// in-ground flag has turned back off and a motion packet has carried it
// down, reporting whether the two arrived — the mark of a shaft let go of
// its block.
func awaitArrowFalls(t *testing.T, srv *server.Server, c *servertest.Client, arrowID int32) (left, fell bool) {
	t.Helper()
	c.ReadTicks(srv, servertest.SkeletonTicks, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayEntityMetadata:
			if protocol.NewReader(data).ReadVarInt() != arrowID {
				return false
			}
			if m := servertest.ReadArrowMetadata(c, data); m.Seen && !m.InGround {
				left = true
			}
		case protocol.IDPlaySetEntityMotion:
			r := protocol.NewReader(data)
			if r.ReadVarInt() == arrowID {
				if _, vy, _ := r.ReadLpVec3(); vy < 0 {
					fell = true
				}
			}
		}
		return left && fell
	})
	return left, fell
}

// TestAnArrowFallsWhenTheBlockItStuckInIsBroken shoots an arrow into a
// wall of stone, waits for the shaft to come to rest, and breaks the wall:
// the shaft is let go — its in-ground flag turns back off and its fall
// carries it down — and it can be taken where it lands.
func TestAnArrowFallsWhenTheBlockItStuckInIsBroken(t *testing.T) {
	t.Parallel()
	srv, c, playerID := servertest.JoinAs(t, servertest.BowTestConfig(), "Archer")
	servertest.GiveBowAndArrows(srv, "Archer")

	// The wall: three cells of stone at x=4, high enough that a level shot
	// meets its face.
	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the block registry has no stone")
	}
	for y := int32(64); y <= 66; y++ {
		if !srv.SetBlock(4, y, 0, stone) {
			t.Fatalf("could not place the wall at (4, %d, 0)", y)
		}
	}

	c.MoveTo(0.5, 64, 0.5, -90, 0)
	servertest.DrawBow(c, -90, 0)
	servertest.HoldDraw(t, srv, "Archer", 24)
	servertest.ReleaseBow(c)
	arrow := servertest.ExpectArrowSpawn(srv, c)
	stuck := c.ReadTicks(srv, servertest.SkeletonTicks, func(id int32, data []byte) bool {
		return id == protocol.IDPlayEntityMetadata &&
			protocol.NewReader(data).ReadVarInt() == arrow.EntityID &&
			servertest.ReadArrowMetadata(c, data).InGround
	})
	if !stuck {
		t.Fatal("the arrow never came to rest in the wall")
	}

	// The wall gone, the shaft the world no longer holds is let go.
	for y := int32(64); y <= 66; y++ {
		srv.SetBlock(4, y, 0, 0)
	}
	if left, fell := awaitArrowFalls(t, srv, c, arrow.EntityID); !left || !fell {
		t.Fatalf("the arrow whose block was broken: left=%v fell=%v, want both", left, fell)
	}

	// It comes down again and the shooter walks over and takes it.
	landed := c.ReadTicks(srv, servertest.SkeletonTicks, func(id int32, data []byte) bool {
		return id == protocol.IDPlayEntityMetadata &&
			protocol.NewReader(data).ReadVarInt() == arrow.EntityID &&
			servertest.ReadArrowMetadata(c, data).InGround
	})
	if !landed {
		t.Fatal("the freed arrow never came back to the ground")
	}
	c.MoveTo(3.4, 64, 0.5, -90, 0)
	picked, removed := false, false
	c.ReadTicks(srv, servertest.SkeletonTicks, func(gotID int32, data []byte) bool {
		switch gotID {
		case protocol.IDPlayPickupItem:
			if collected, collector, count := servertest.ReadPickupItem(c, data); collected == arrow.EntityID && collector == playerID && count == 1 {
				picked = true
			}
		case protocol.IDPlayRemoveEntities:
			r := protocol.NewReader(data)
			for range r.ReadVarInt() {
				if r.ReadVarInt() == arrow.EntityID {
					removed = true
				}
			}
		}
		return picked && removed
	})
	if !picked {
		t.Fatal("the fallen arrow could not be picked up where it landed")
	}
	if !removed {
		t.Fatal("the picked-up arrow was never taken out of the world")
	}
	if got := srv.InventorySlot("Archer", 37); got.Count != 5 {
		t.Errorf("the quiver holds %d arrows after the pickup, want the five one arrow came back to", got.Count)
	}
}

// TestAnArrowFallsWhenAPistonCarriesItsBlockAway stands a sticky piston
// with a dirt block in front of it, shoots an arrow into the block, and
// retracts the piston: the block is pulled away and the shaft it held is
// let go to fall.
func TestAnArrowFallsWhenAPistonCarriesItsBlockAway(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.BowTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))

	// The piston at (4,65,0) facing east rides its head out to (5,65,0)
	// and leaves the dirt at (6,65,0), at the level a level shot flies.
	c.ChatCommand("setblock 4 65 0 sticky_piston[facing=east]")
	c.ChatCommand("setblock 6 65 0 dirt")
	c.ChatCommand("setblock 4 65 1 redstone_block")
	servertest.AwaitTicks(t, srv, 8)

	// The shaft into the dirt's east face.
	arrowID := srv.ShootArrow(8.0, 65.5, 0.5, -3, 0, 0)
	stuck := c.ReadTicks(srv, servertest.SkeletonTicks, func(id int32, data []byte) bool {
		return id == protocol.IDPlayEntityMetadata &&
			protocol.NewReader(data).ReadVarInt() == arrowID &&
			servertest.ReadArrowMetadata(c, data).InGround
	})
	if !stuck {
		t.Fatal("the arrow never came to rest in the block")
	}

	// The power gone, the sticky head pulls the dirt back to (5,65,0),
	// leaving the arrow's cell clear.
	c.ChatCommand("setblock 4 65 1 air")
	servertest.AwaitTicks(t, srv, 8)
	dirt, ok := registry.FindState("minecraft:dirt", nil)
	if !ok {
		t.Fatal("the block registry has no dirt")
	}
	if got := srv.BlockAt(5, 65, 0); got != dirt {
		t.Fatalf("the pulled block at (5,65,0) = %d, want the dirt %d", got, dirt)
	}
	if got := srv.BlockAt(6, 65, 0); got != 0 {
		t.Fatalf("the cell the arrow stuck in holds %d, want the air the pull left", got)
	}

	if left, fell := awaitArrowFalls(t, srv, c, arrowID); !left || !fell {
		t.Fatalf("the arrow whose block a piston carried off: left=%v fell=%v, want both", left, fell)
	}
	if !srv.ArrowAlive(arrowID) {
		t.Error("the freed arrow left the world")
	}
}

// TestAStuckArrowSavesTheBlockItHit replaces the block under a stuck shaft
// and saves it: the inBlockState written is the state the shaft met, kept
// on the arrow, and not the block that took its place, which re-reading
// the world at save time would have found.
func TestAStuckArrowSavesTheBlockItHit(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()
	cfg.LevelName = t.TempDir()

	srv, c := servertest.Join(t, cfg, "Keeper")

	srv.SpawnStuckArrow("allowed", 14.5, 64, 14.5)
	c.AwaitSpawn(protocol.EntityTypeArrow)

	dirt, ok := registry.FindState("minecraft:dirt", nil)
	if !ok {
		t.Fatal("the block registry has no dirt")
	}
	if !srv.SetBlock(14, 63, 14, dirt) {
		t.Fatal("could not replace the stone under the arrow")
	}

	srv.FlushMobs()
	srv.Shutdown()

	servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, "minecraft:arrow", func(compound nbt.Compound) {
		inBlock := compound["inBlockState"]
		if name, _ := registry.BlockStateOf(inBlock); name != "minecraft:stone" {
			t.Errorf("inBlockState = %v, want the stone the shaft hit", inBlock)
		}
	})
}
