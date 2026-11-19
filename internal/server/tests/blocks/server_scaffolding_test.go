package blocks

import (
	"bytes"
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/entity"
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

// TestScaffoldingCantileverStaysAndAnUnsupportedPieceFalls checks stability
// both ways: a piece the command leaves standing at distance seven falls as a
// falling block and settles at the distance the ground reads, while the
// cantilever built by hand keeps standing, its distance the only thing
// holding it up. A placement whose distance would be seven — against a wall
// with nothing but air below — is refused.
// vanilla: ScaffoldingBlock.tick, ScaffoldingBlock.canSurvive
func TestScaffoldingCantileverStaysAndAnUnsupportedPieceFalls(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))

	scaffolding, _ := registry.ItemID("minecraft:scaffolding")
	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: scaffolding, Count: 1})

	ground := scaffoldStateFor(t, "0", "false")
	cantilever := scaffoldStateFor(t, "1", "true")

	// The column grows the way the clicks now do: the ground placed on the
	// stone, the piece above it walked up by a side click, the arm walked
	// out along the look of a top click.
	c.PlaceBlock(5, 63, 0, protocol.FaceTop)
	c.PlaceBlock(5, 64, 0, protocol.FaceEast)
	c.FacePlayer(2.5, 64, 0.5, 90, -20)
	c.PlaceBlock(5, 65, 0, protocol.FaceTop)
	servertest.WaitForBlock(t, srv, 4, 65, 0, func(s uint16) bool { return s == cantilever })
	groundLevel := srv.BlockAt(5, 64, 0)

	// A piece with nothing at all around it — the command sets the default
	// state, whose distance seven a placement would refuse — leaves as a
	// falling block: the cell it stood in clears, the body carries the
	// bottom the falling state reads, and it lands at the distance the
	// ground gives.
	hanging := scaffoldStateFor(t, "7", "true")
	c.ChatCommand("setblock 8 66 3 minecraft:scaffolding")
	c.AwaitBlockState(8, 66, 3, 0)
	spawn := c.AwaitFallingSpawn()
	if spawn.Data != int32(hanging) {
		t.Errorf("falling body carries %d, want %d (distance 7, bottom)", spawn.Data, hanging)
	}
	servertest.WaitForBlock(t, srv, 8, 64, 3, func(s uint16) bool { return s == ground })
	if got := srv.BlockAt(8, 65, 3); got != 0 {
		t.Errorf("block above the landing = %d, want 0 (air)", got)
	}

	// The fall and the landing took their seconds, longer than a whole
	// unsupported column needs to come down: the cantilever has not moved.
	if got := srv.BlockAt(4, 65, 0); got != cantilever {
		t.Errorf("the cantilever after the fall = %d, want %d (still standing)", got, cantilever)
	}
	if got := srv.BlockAt(5, 65, 0); got != ground {
		t.Errorf("the column top after the fall = %d, want %d (still standing)", got, ground)
	}
	if got := srv.BlockAt(5, 64, 0); got != groundLevel {
		t.Errorf("the column base after the fall = %d, want %d (still standing)", got, groundLevel)
	}

	// A placement that would hang at distance seven is refused: clicking the
	// side of a stone column leaves the cell beside it air, with nothing
	// falling either.
	c.ChatCommand("setblock 2 65 3 minecraft:stone")
	c.Settle()
	c.PlaceBlock(2, 65, 3, protocol.FaceWest)
	c.Settle()
	if got := srv.BlockAt(1, 65, 3); got != 0 {
		t.Errorf("placement that would hang = %d, want 0 (air, the placement refused)", got)
	}
	if got := srv.FallingCount(); got != 0 {
		t.Errorf("%d falling blocks left in the world after the refused placement, want none", got)
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

// TestAClickPastTheBuildLimitSendsTheActionBarLine checks the walk that runs
// off the world's top: nothing is placed — the clicked column and the cell
// beside it hold what they held — and the placer alone hears the action
// bar's build.tooHigh, with the world's top as the number it names.
// vanilla: ScaffoldingBlockItem.updatePlacementContext,
// ServerPlayer.sendBuildLimitMessage
func TestAClickPastTheBuildLimitSendsTheActionBarLine(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())

	watcher := connect().Join("Watcher")

	actor := connect().Join("Actor")
	actor.SetChatMode(protocol.ChatModeHidden)

	// A column that reaches the world's own top: the flat world's overworld
	// runs from -64 to 319, so the piece at 319 is the last one that can
	// stand, and any walk up from it is past the build limit.
	const top = int32(319)
	column := scaffoldStateFor(t, "0", "false")
	for y := int32(64); y <= top; y++ {
		if !srv.SetBlock(6, y, 8, column) {
			t.Fatal("the column's chunk was not loaded")
		}
	}

	scaffolding, _ := registry.ItemID("minecraft:scaffolding")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: scaffolding, Count: 1})
	actor.FacePlayer(6.5, float64(top), 10.5, 0, -30)
	actor.PlaceBlock(6, top, 8, protocol.FaceSouth)

	// The line answers on the action bar, red, with the world's top.
	var want protocol.Buffer
	want.PutText(protocol.Text{
		Translate: "build.tooHigh",
		With:      []protocol.Text{{Text: "319"}},
		Color:     protocol.ColorRed,
	})
	want.PutBool(true) // over the hotbar
	data, ok := servertest.ReadUntil(t, actor, servertest.ExpectTimeout, protocol.IDPlaySystemChat)
	if !ok {
		t.Fatal("no build-limit line within " + servertest.ExpectTimeout.String())
	}
	if !bytes.Equal(data, want.Bytes()) {
		t.Errorf("the action bar line = % X, want % X", data, want.Bytes())
	}
	actor.Settle()
	if state := srv.BlockAt(6, top, 8); state != column {
		t.Errorf("the column's top piece = %d, want the column's %d (nothing in its cell)", state, column)
	}
	if state := srv.BlockAt(6, top, 9); state != 0 {
		t.Errorf("the cell beside the clicked piece = %d, want air", state)
	}
	// Nothing was placed, so the watcher has heard no block update beyond
	// the joins: the clicked piece and its neighbour are what the echoes
	// told the placer alone.
	servertest.HoldTicks(t, srv)
	watcher.ExpectNoneWithinTicks(srv, 40, servertest.PacketIs(protocol.IDPlayBlockUpdate))
}

// scaffoldColumn builds a floor at y 63 and scaffolding at the rows given.
func scaffoldColumn(t *testing.T, srv *server.Server, x, z int32, rows ...int32) {
	t.Helper()
	srv.SetLoadedBlock(x, 63, z, servertest.MustState(t, "minecraft:stone", nil))
	for _, y := range rows {
		if !srv.SetBlock(x, y, z, scaffoldStateFor(t, "0", "false")) {
			t.Fatal("the scaffolding's chunk was not loaded")
		}
	}
}

// TestAnItemInsideScaffoldingSlidesPastItsLegs shoves a stack through the
// cell of a scaffolding piece, level with a corner leg: the body is not
// above the cell, so the leg is empty to it.
// vanilla: ScaffoldingBlock.getCollisionShape
func TestAnItemInsideScaffoldingSlidesPastItsLegs(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.SurvivalConfig(), "Dropper")
	scaffoldColumn(t, srv, 2, 3, 64)
	srv.SpawnItemEntityMotion(1.5, 64.5, 3.05, "minecraft:stick", 1, 0.3, 0, 0)
	rest := func() float64 {
		items := srv.ItemsInDimension(world.DimensionName)
		if len(items) != 1 {
			return -1
		}
		return items[0].X
	}
	// Past the leg's far face (3.0 + 2/16) and the stick's half size.
	servertest.WaitForTicks(t, srv, 100, func() bool { return rest() > 2+2.0/16+entity.ItemHalfSize })
	if x := rest(); x <= 2+2.0/16+entity.ItemHalfSize {
		t.Errorf("the stick stopped at x %v, caught on the leg", x)
	}
}

// TestAZombieStandsOnTopOfScaffolding drops a zombie onto a scaffolding
// column: seen from above, the top plate holds it.
// vanilla: ScaffoldingBlock.getCollisionShape
func TestAZombieStandsOnTopOfScaffolding(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.SurvivalConfig(), "Watcher")
	scaffoldColumn(t, srv, 2, 3, 64)
	srv.SpawnMob("minecraft:zombie", 2.5, 67, 3.5)
	y := func() float64 {
		for _, m := range srv.Mobs() {
			if m.Kind == "minecraft:zombie" {
				return m.Y
			}
		}
		return -1
	}
	servertest.WaitForTicks(t, srv, 100, func() bool { return y() == 65 })
	if got := y(); got != 65 {
		t.Errorf("the zombie stands at y %v, want the plate's top at 65", got)
	}
}
