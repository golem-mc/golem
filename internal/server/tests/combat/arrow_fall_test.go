package combat

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
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
