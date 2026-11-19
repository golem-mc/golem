package blocks

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// dripstoneState looks up a pointed dripstone state by its column
// direction and thickness.
func dripstoneState(t *testing.T, direction, thickness string) uint16 {
	t.Helper()
	s, ok := registry.FindState("minecraft:pointed_dripstone", map[string]string{
		"vertical_direction": direction, "thickness": thickness, "waterlogged": "false",
	})
	if !ok {
		t.Fatalf("no pointed dripstone with vertical_direction=%s thickness=%s", direction, thickness)
	}
	return s
}

// TestDripstoneThicknessFollowsItsColumn checks that placing a pointed
// dripstone writes the thickness SpeleothemBlock.calculateSpeleothemThickness
// derives from the column, for a growing stalagmite, for a stalactite whose
// middle block goes, and where the two meet.
// vanilla: SpeleothemBlock.getStateForPlacement, calculateSpeleothemThickness,
// updateShape
func TestDripstoneThicknessFollowsItsColumn(t *testing.T) {
	t.Parallel()
	srv, actor := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))
	actor.MoveToGround(0.5, 64, 0.5, true)
	actor.Settle()

	drip, _ := registry.ItemID("minecraft:pointed_dripstone")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: drip, Count: 1})

	// --- A stalagmite grows one block at a time on the ground ---
	actor.FacePlayer(0.5, 64, 0.5, 0, 0)
	actor.PlaceBlock(1, 63, 1, protocol.FaceTop) // (1,64,1)
	actor.PlaceBlock(1, 64, 1, protocol.FaceTop) // (1,65,1)
	actor.PlaceBlock(1, 65, 1, protocol.FaceTop) // (1,66,1)
	actor.Settle()

	if got, want := srv.BlockAt(1, 64, 1), dripstoneState(t, "up", "base"); got != want {
		t.Errorf("stalagmite bottom = %d, want %d (base)", got, want)
	}
	if got, want := srv.BlockAt(1, 65, 1), dripstoneState(t, "up", "frustum"); got != want {
		t.Errorf("stalagmite middle = %d, want %d (frustum)", got, want)
	}
	if got, want := srv.BlockAt(1, 66, 1), dripstoneState(t, "up", "tip"); got != want {
		t.Errorf("stalagmite top = %d, want %d (tip)", got, want)
	}

	// A fourth block pushes the frustum up: the block now two under the tip
	// is a middle, the bottom stays a base.
	actor.PlaceBlock(1, 66, 1, protocol.FaceTop) // (1,67,1)
	actor.Settle()

	if got, want := srv.BlockAt(1, 64, 1), dripstoneState(t, "up", "base"); got != want {
		t.Errorf("four-tall stalagmite bottom = %d, want %d (base)", got, want)
	}
	if got, want := srv.BlockAt(1, 65, 1), dripstoneState(t, "up", "middle"); got != want {
		t.Errorf("four-tall stalagmite second = %d, want %d (middle)", got, want)
	}
	if got, want := srv.BlockAt(1, 66, 1), dripstoneState(t, "up", "frustum"); got != want {
		t.Errorf("four-tall stalagmite third = %d, want %d (frustum)", got, want)
	}
	if got, want := srv.BlockAt(1, 67, 1), dripstoneState(t, "up", "tip"); got != want {
		t.Errorf("four-tall stalagmite top = %d, want %d (tip)", got, want)
	}

	// --- A stalactite hangs three deep; breaking its middle retips the base ---
	actor.ChatCommand("setblock 2 67 2 minecraft:stone")
	actor.FacePlayer(0.5, 64, 0.5, 0, -90)
	actor.PlaceBlock(2, 67, 2, protocol.FaceBottom) // (2,66,2)
	actor.PlaceBlock(2, 66, 2, protocol.FaceBottom) // (2,65,2)
	actor.PlaceBlock(2, 65, 2, protocol.FaceBottom) // (2,64,2)
	actor.Settle()

	if got, want := srv.BlockAt(2, 66, 2), dripstoneState(t, "down", "base"); got != want {
		t.Errorf("stalactite top = %d, want %d (base)", got, want)
	}
	if got, want := srv.BlockAt(2, 65, 2), dripstoneState(t, "down", "frustum"); got != want {
		t.Errorf("stalactite middle = %d, want %d (frustum)", got, want)
	}
	if got, want := srv.BlockAt(2, 64, 2), dripstoneState(t, "down", "tip"); got != want {
		t.Errorf("stalactite bottom = %d, want %d (tip)", got, want)
	}

	actor.BreakBlock(2, 65, 2)
	actor.Settle()
	if got, want := srv.BlockAt(2, 66, 2), dripstoneState(t, "down", "tip"); got != want {
		t.Errorf("stalactite base after its middle went = %d, want %d (tip)", got, want)
	}

	// --- A stalactite whose tip meets a stalagmite merges ---
	actor.FacePlayer(0.5, 64, 0.5, 0, 0)
	actor.PlaceBlock(0, 63, 2, protocol.FaceTop) // stalagmite at (0,64,2)
	actor.ChatCommand("setblock 0 66 2 minecraft:stone")
	actor.FacePlayer(0.5, 64, 0.5, 0, -90)
	actor.PlaceBlock(0, 66, 2, protocol.FaceBottom) // stalactite at (0,65,2)
	actor.Settle()

	if got, want := srv.BlockAt(0, 64, 2), dripstoneState(t, "up", "tip_merge"); got != want {
		t.Errorf("stalagmite meeting a stalactite = %d, want %d (tip_merge)", got, want)
	}
	if got, want := srv.BlockAt(0, 65, 2), dripstoneState(t, "down", "tip_merge"); got != want {
		t.Errorf("stalactite meeting a stalagmite = %d, want %d (tip_merge)", got, want)
	}

	// --- Sneaking skips that merge: the two stay plain tips ---
	actor.FacePlayer(0.5, 64, 0.5, 0, 0)
	actor.PlaceBlock(3, 63, 2, protocol.FaceTop) // stalagmite at (3,64,2)
	actor.ChatCommand("setblock 3 66 2 minecraft:stone")
	actor.Send(protocol.IDPlayPlayerInput, func(b *protocol.Buffer) { b.PutU8(protocol.InputSneak) })
	actor.FacePlayer(0.5, 64, 0.5, 0, -90)
	actor.PlaceBlock(3, 66, 2, protocol.FaceBottom) // stalactite at (3,65,2)
	actor.Settle()

	if got, want := srv.BlockAt(3, 64, 2), dripstoneState(t, "up", "tip"); got != want {
		t.Errorf("stalagmite under a sneaked stalactite = %d, want %d (tip)", got, want)
	}
	if got, want := srv.BlockAt(3, 65, 2), dripstoneState(t, "down", "tip"); got != want {
		t.Errorf("sneaked stalactite over a stalagmite = %d, want %d (tip)", got, want)
	}
}

// TestWaterloggedSurvivalTickIsNotHeldByAFluidVisit checks that the survival
// visit of a waterlogged block runs on time although a five-tick water visit
// for the same cell is already queued.
// vanilla: SpeleothemBlock.updateShape, BigDripleafStemBlock.updateShape book
// the block tick; the water visit is a fluid tick in a queue of its own.
func TestWaterloggedSurvivalTickIsNotHeldByAFluidVisit(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		block     string
		supportDY int32
		leaf      bool
	}{
		{"stalagmite breaks after one tick", "pointed_dripstone[vertical_direction=up,thickness=tip,waterlogged=true]", -1, false},
		{"stalactite falls after two ticks", "pointed_dripstone[vertical_direction=down,thickness=tip,waterlogged=true]", 1, false},
		{"big dripleaf stem breaks after one tick", "big_dripleaf_stem[facing=north,waterlogged=true]", -1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.AttachActor(t)
			srv.StopTick()
			const x, y, z = 20, 80, 20
			servertest.Setblock(t, c, x, y+tc.supportDY, z, "clay")
			servertest.Setblock(t, c, x, y, z, tc.block)
			servertest.Setblock(t, c, x+1, y, z, "stone")
			if tc.leaf {
				servertest.Setblock(t, c, x, y+1, z, "big_dripleaf[facing=north,waterlogged=false]")
			}
			srv.TickPhysics(1)
			standing := srv.BlockAt(x, y, z)
			servertest.Setblock(t, c, x, y+tc.supportDY, z, "air")
			srv.TickPhysics(2)
			if srv.BlockAt(x, y, z) == standing {
				t.Error("the waterlogged block still stands 2 ticks after losing its support")
			}
		})
	}
}
