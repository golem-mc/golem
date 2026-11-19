package blocks

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The scaffolding: the distance and bottom properties a placement reads
// off the blocks around it, and the stability they give — what a
// cantilever travels outward on, one past the piece it hangs off. A piece
// stands at the distance of the column below it, zero over anything sturdy,
// one past the closest side otherwise, and at the ceiling of seven
// nowhere: a placement there is refused, a piece left there falls, and one
// whose support went collapses with its loot instead.
// vanilla: ScaffoldingBlock.getStateForPlacement, ScaffoldingBlock.tick

// scaffoldStateFor is one scaffolding state ID by its distance and bottom.
func scaffoldStateFor(t *testing.T, distance, bottom string) uint16 {
	t.Helper()
	s, ok := registry.FindState("minecraft:scaffolding", map[string]string{
		"bottom": bottom, "distance": distance, "waterlogged": "false",
	})
	if !ok {
		t.Fatalf("the registry has no scaffolding at distance %s, bottom %s", distance, bottom)
	}
	return s
}

// TestScaffoldingPlacementStates checks the state every kind of placement
// lands as: on the ground at distance zero, stacked over it at the distance
// the column below carries — a side click, the extension walk up — and
// cantilevered off the column's top, the top click's walk along the look,
// the only way a piece ever stands over air.
// vanilla: ScaffoldingBlock.getStateForPlacement
func TestScaffoldingPlacementStates(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())

	watcher := connect().Join("Watcher")

	actor := connect().Join("Actor")

	entityID := watcher.ExpectPlayerAdded("Actor")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Actor")

	scaffolding, _ := registry.ItemID("minecraft:scaffolding")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: scaffolding, Count: 1})
	watcher.ExpectEquipment(entityID)

	ground := scaffoldStateFor(t, "0", "false")
	cantilever := scaffoldStateFor(t, "1", "true")

	// The column stands a little east of the actor, who is at (0.5, 64, 0.5).
	actor.PlaceBlock(5, 63, 0, protocol.FaceTop)
	if got := watcher.ExpectBlockUpdate(); got.State != ground || got.X != 5 || got.Y != 64 || got.Z != 0 {
		t.Errorf("scaffolding placed on the ground = %d, want %d (distance 0)", got.State, ground)
	}

	// A side click on the column's piece walks up, over the piece itself.
	actor.PlaceBlock(5, 64, 0, protocol.FaceEast)
	if got := watcher.ExpectBlockUpdate(); got.State != ground || got.Y != 65 {
		t.Errorf("scaffolding stacked on scaffolding = %d, want %d (the distance the column below carries)",
			got.State, ground)
	}

	// A top click walks out along the look — west here — to the piece over
	// air every scaffolding build grows outward from.
	actor.FacePlayer(2.5, 64, 0.5, 90, -20)
	actor.PlaceBlock(5, 65, 0, protocol.FaceTop)
	if got := watcher.ExpectBlockUpdate(); got.State != cantilever || got.X != 4 || got.Y != 65 {
		t.Errorf("cantilevered scaffolding = %d, want %d (distance 1, bottom)", got.State, cantilever)
	}

	if got := srv.BlockAt(4, 65, 0); got != cantilever {
		t.Errorf("cantilever in the world = %d, want %d", got, cantilever)
	}
}

// TestScaffoldingBreaksWithItsLootWhenItsSupportGoes checks the collapse of
// a cantilever whose column loses the ground piece: the stacked piece and the
// arm lose their hold at the distance they carried, and rather than falling —
// the body only a piece standing at distance seven leaves as — they break
// with what their loot table gives.
// vanilla: ScaffoldingBlock.tick
func TestScaffoldingBreaksWithItsLootWhenItsSupportGoes(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))

	scaffolding, _ := registry.ItemID("minecraft:scaffolding")
	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: scaffolding, Count: 1})

	// The column grows as the other test's: ground placed on the stone,
	// the piece above it walked up by a side click, the arm walked out
	// along the look of a top click.
	c.PlaceBlock(5, 63, 0, protocol.FaceTop)
	c.PlaceBlock(5, 64, 0, protocol.FaceEast)
	c.FacePlayer(2.5, 64, 0.5, 90, -20)
	c.PlaceBlock(5, 65, 0, protocol.FaceTop)
	cantilever := scaffoldStateFor(t, "1", "true")
	servertest.WaitForBlock(t, srv, 4, 65, 0, func(s uint16) bool { return s == cantilever })

	// The ground piece of the column goes, and behind it the whole column:
	// the two pieces standing over air collapse, as items — the creative
	// break itself drops nothing.
	c.BreakBlock(5, 64, 0)
	servertest.WaitFor(t, srv, 160, func() bool {
		return len(srv.ItemsInDimension(world.DimensionName)) == 2
	})
	for _, pos := range [][3]int32{{5, 64, 0}, {5, 65, 0}, {4, 65, 0}} {
		if got := srv.BlockAt(pos[0], pos[1], pos[2]); got != 0 {
			t.Errorf("block at (%d, %d, %d) after the collapse = %d, want 0 (air)",
				pos[0], pos[1], pos[2], got)
		}
	}
	for _, item := range srv.ItemsInDimension(world.DimensionName) {
		if item.Item != "minecraft:scaffolding" {
			t.Errorf("the collapse dropped %q, want scaffolding", item.Item)
		}
		if item.Count != 1 {
			t.Errorf("the collapse dropped a stack of %d, want one piece each", item.Count)
		}
	}
}

// placeBlockFromInside is placeBlockAtCursor with the client's ray-trace
// "in a block" flag carried as well, which is what the scaffolding walk's
// sneaking branch reads. The cursor sits at the centre of the face.
func placeBlockFromInside(c *servertest.Client, x, y, z, face int32, inside bool) {
	c.T.Helper()
	c.Send(protocol.IDPlayUseItemOn, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.HandMain)
		b.PutPosition(x, y, z)
		b.PutVarInt(face)
		b.PutF32(0.5)
		b.PutF32(0.5)
		b.PutF32(0.5)
		b.PutBool(inside)
		b.PutBool(false) // world border hit
		b.PutVarInt(1)   // sequence
	})
}

// TestClickingScaffoldingWithScaffoldingExtendsTheColumn checks the walks a
// click on an existing piece makes: a side click walks the column up to the
// first free cell, a top click walks out along the player's look and over a
// piece standing in the way, a sneaking click walks the clicked face over
// the piece beside it, and a click the client traced from inside walks the
// face's far side. Each piece lands with the distance and bottom the blocks
// around the cell make, and the clicked column keeps its pieces.
// vanilla: ScaffoldingBlockItem.updatePlacementContext, ScaffoldingBlock
// .getDistance
func TestClickingScaffoldingWithScaffoldingExtendsTheColumn(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())

	watcher := connect().Join("Watcher")

	actor := connect().Join("Actor")

	entityID := watcher.ExpectPlayerAdded("Actor")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Actor")

	// Every piece stands on the column under it (distance 0) or at the
	// distance its neighbour gives it, so the stability visit that follows
	// the placement rewrites none of them.
	column := scaffoldStateFor(t, "0", "false")
	cases := []struct {
		desc     string
		click    [3]int32
		face     int32
		sneaking bool
		inside   bool
		wantAt   [3]int32
		// the piece the walk lands, by the distance and bottom the
		// blocks around the cell make.
		wantDistance string
		wantBottom   string
	}{
		// A side click walks up whatever the column's height: the fourth
		// piece stands above the top one, inheriting its distance.
		{"a side click on a column's top",
			[3]int32{6, 66, 8}, protocol.FaceNorth, false, false,
			[3]int32{6, 67, 8}, "0", "false"},
		// A top click walks out along the look (south here), over the piece
		// straight ahead to the free cell past it — one further out than
		// the piece it skipped, which itself stands one past the column.
		{"a top click looking south",
			[3]int32{16, 66, 8}, protocol.FaceTop, false, false,
			[3]int32{16, 66, 10}, "2", "true"},
		// Sneaking walks the clicked face, out over the piece beside the
		// clicked one to the first free cell.
		{"a sneaking side click",
			[3]int32{26, 64, 8}, protocol.FaceEast, true, false,
			[3]int32{28, 64, 8}, "0", "false"},
		// A sneaking click traced from inside the block walks out the
		// clicked face's far side instead of the face itself.
		{"a sneaking click from inside",
			[3]int32{36, 64, 8}, protocol.FaceSouth, true, true,
			[3]int32{36, 64, 7}, "0", "false"},
	}
	// The columns the cases click: three tall for the first two clicks, a
	// pair for the sneaking walk, one for the inside click, with the
	// wayside piece the top click walks over standing beside the column.
	for _, y := range []int32{64, 65, 66} {
		if !srv.SetBlock(6, y, 8, column) || !srv.SetBlock(16, y, 8, column) {
			t.Fatal("the clicked column's chunk was not loaded")
		}
	}
	for _, p := range [][3]int32{{26, 64, 8}, {27, 64, 8}, {36, 64, 8}} {
		if !srv.SetBlock(p[0], p[1], p[2], column) {
			t.Fatalf("the piece at (%d,%d,%d) could not stand: its chunk was not loaded", p[0], p[1], p[2])
		}
	}
	// The wayside piece the top click walks over stands one past the column.
	srv.SetBlock(16, 66, 9, scaffoldStateFor(t, "1", "true"))

	scaffolding, _ := registry.ItemID("minecraft:scaffolding")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: scaffolding, Count: 1})
	watcher.ExpectEquipment(entityID)

	sneaking := false
	for _, tc := range cases {
		// The stand is south of the click, so no walked cell holds the
		// body, and the look is what the top click follows.
		actor.FacePlayer(float64(tc.click[0])+0.5, 64, float64(tc.click[2])+2.5, 0, -30)
		if tc.sneaking != sneaking {
			actor.SendSneak(tc.sneaking)
			sneaking = tc.sneaking
		}
		if tc.inside {
			placeBlockFromInside(actor, tc.click[0], tc.click[1], tc.click[2], tc.face, true)
		} else {
			actor.PlaceBlock(tc.click[0], tc.click[1], tc.click[2], tc.face)
		}

		want := servertest.BlockChange{
			X: tc.wantAt[0], Y: tc.wantAt[1], Z: tc.wantAt[2],
			State: scaffoldStateFor(t, tc.wantDistance, tc.wantBottom),
		}
		if got := watcher.ExpectBlockUpdate(); got != want {
			t.Errorf("%s: watcher heard %+v, want %+v", tc.desc, got, want)
		}
		if state := srv.BlockAt(tc.wantAt[0], tc.wantAt[1], tc.wantAt[2]); state != want.State {
			t.Errorf("%s: world block at (%d,%d,%d) = %d, want %d",
				tc.desc, tc.wantAt[0], tc.wantAt[1], tc.wantAt[2], state, want.State)
		}
		// The clicked piece keeps its own cell: the walk retargets the
		// placement rather than replacing what was clicked.
		if state := srv.BlockAt(tc.click[0], tc.click[1], tc.click[2]); state != column {
			t.Errorf("%s: the clicked piece at (%d,%d,%d) = %d, want the column's %d",
				tc.desc, tc.click[0], tc.click[1], tc.click[2], state, column)
		}
	}
}
