package redstone

import (
	"sort"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// shulkerDispense stands a dispenser at (4, 64, 0) holding one box (they stack to one),
// carrying a diamond stack and the name "Stash", and runs one dispense out
// of the face. prepare changes the world first. The cell the face opens onto
// is returned.
func shulkerDispense(t *testing.T, item string, face int32, prepare func(srv *server.Server, w *servertest.WindowedClient)) (*server.Server, [3]int32) {
	t.Helper()
	srv, w := servertest.Dispensing(t)
	box := servertest.ItemStack(t, item, 1)
	box.Container = []protocol.ItemStack{servertest.ItemStack(t, "minecraft:diamond", 64)}
	box.CustomName = &protocol.Text{Text: "Stash"}
	if !srv.SetInventoryAt(4, 64, 0, 0, box) {
		t.Fatal("the dispenser has no block entity to fill")
	}
	if prepare != nil {
		prepare(srv, w)
	}
	w.DrainRecorded()
	srv.Dispense(4, 64, 0, face)
	off := protocol.FaceOffset[face]
	return srv, [3]int32{4 + off[0], 64 + off[1], 0 + off[2]}
}

func shulkerFacing(t *testing.T, state uint16, want string) {
	t.Helper()
	name, raw, ok := registry.BlockStateName(state)
	if !ok {
		t.Fatalf("state %d is no block", state)
	}
	if got := registry.ParseStateProps(raw)["facing"]; got != want {
		t.Errorf("%s faces %q, want %q", name, got, want)
	}
}

// checkPlacedShulker asserts the box stands in the cell with its contents and
// name, and that the dispenser gave it up.
func checkPlacedShulker(t *testing.T, srv *server.Server, item string, cell [3]int32, wantFacing string) {
	t.Helper()
	state := srv.BlockAt(cell[0], cell[1], cell[2])
	if name, _, _ := registry.BlockStateName(state); name != item {
		t.Fatalf("the cell holds %s, want %s", name, item)
	}
	shulkerFacing(t, state, wantFacing)
	held := srv.InventoryAt(cell[0], cell[1], cell[2])
	if len(held) != 27 || held[0].Count != 64 {
		t.Errorf("the placed box holds %+v, want 64 diamonds in slot 0", held)
	}
	tag, ok := srv.BlockEntityTagAt(cell[0], cell[1], cell[2])
	if !ok || tag["CustomName"] == nil {
		t.Errorf("the placed box has tag %v, want its CustomName kept", tag)
	}
	if left := srv.InventoryAt(4, 64, 0)[0]; !left.Empty() {
		t.Errorf("the dispenser keeps %+v, want it empty", left)
	}
}

// TestADispenserPlacesAShulkerBox: every colour of shulker box is placed in
// the cell the face opens onto, facing the dispenser's way over an empty
// cell below and up over a solid one, with its contents and name.
// vanilla: ShulkerBoxDispenseBehavior.execute
func TestADispenserPlacesAShulkerBox(t *testing.T) {
	t.Parallel()
	var colours []string
	for name := range protocol.BlockShulkerBoxIDs {
		colours = append(colours, name)
	}
	sort.Strings(colours)
	if len(colours) != 17 {
		t.Fatalf("%d shulker box colours, want 17", len(colours))
	}
	for _, colour := range colours {
		for _, tc := range []struct {
			name string
			hole bool
			want string
		}{{"over air", true, "south"}, {"over a floor", false, "up"}} {
			t.Run(colour+" "+tc.name, func(t *testing.T) {
				t.Parallel()
				item := "minecraft:" + colour
				srv, cell := shulkerDispense(t, item, protocol.FaceSouth, func(srv *server.Server, _ *servertest.WindowedClient) {
					if tc.hole {
						srv.SetBlock(4, 63, 1, registry.MustState("minecraft:air", nil))
					}
				})
				checkPlacedShulker(t, srv, item, cell, tc.want)
			})
		}
	}
}

// TestADispenserPlacesAShulkerBoxOutOfEveryFace: the box takes the face's
// direction over an empty cell below the target, whichever horizontal face
// the dispenser fires, and up otherwise.
func TestADispenserPlacesAShulkerBoxOutOfEveryFace(t *testing.T) {
	t.Parallel()
	air := registry.MustState("minecraft:air", nil)
	for _, tc := range []struct {
		name  string
		face  int32
		clear [][3]int32
		want  string
	}{
		{"north", protocol.FaceNorth, [][3]int32{{4, 63, -1}}, "north"},
		{"east", protocol.FaceEast, [][3]int32{{5, 63, 0}}, "east"},
		{"west", protocol.FaceWest, [][3]int32{{3, 63, 0}}, "west"},
		{"up", protocol.FaceTop, nil, "up"},
		{"down", protocol.FaceBottom, [][3]int32{{4, 63, 0}, {4, 62, 0}}, "down"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			item := "minecraft:shulker_box"
			srv, cell := shulkerDispense(t, item, tc.face, func(srv *server.Server, _ *servertest.WindowedClient) {
				for _, c := range tc.clear {
					srv.SetBlock(c[0], c[1], c[2], air)
				}
			})
			checkPlacedShulker(t, srv, item, cell, tc.want)
		})
	}
}

// TestADispenserPlacesAShulkerBoxInAReplaceableCell: grass and water give way
// to the box.
func TestADispenserPlacesAShulkerBoxInAReplaceableCell(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		block string
		props map[string]string
	}{
		{"short grass", "minecraft:short_grass", nil},
		{"water", "minecraft:water", map[string]string{"level": "0"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			item := "minecraft:shulker_box"
			srv, cell := shulkerDispense(t, item, protocol.FaceSouth, func(srv *server.Server, _ *servertest.WindowedClient) {
				srv.SetBlock(4, 64, 1, registry.MustState(tc.block, tc.props))
			})
			checkPlacedShulker(t, srv, item, cell, "up")
		})
	}
}

// TestADispenserKeepsAShulkerBoxItCannotPlace: a solid block or a player in
// the cell fails the dispense: the box stays, nothing drops.
func TestADispenserKeepsAShulkerBoxItCannotPlace(t *testing.T) {
	t.Parallel()
	stone := registry.MustState("minecraft:stone", nil)
	for _, tc := range []struct {
		name    string
		prepare func(srv *server.Server, w *servertest.WindowedClient)
		holds   func(state uint16) bool
	}{
		{"a solid block", func(srv *server.Server, _ *servertest.WindowedClient) { srv.SetBlock(4, 64, 1, stone) },
			func(state uint16) bool { return state == stone }},
		{"a player", func(_ *server.Server, w *servertest.WindowedClient) {
			w.FacePlayer(4.5, 64, 1.5, 0, 0)
			w.Settle()
		}, func(state uint16) bool { return state == registry.MustState("minecraft:air", nil) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, cell := shulkerDispense(t, "minecraft:shulker_box", protocol.FaceSouth, tc.prepare)
			if got := srv.BlockAt(cell[0], cell[1], cell[2]); !tc.holds(got) {
				t.Errorf("the cell = %d, want it unchanged", got)
			}
			if left := srv.InventoryAt(4, 64, 0)[0]; left.Count != 1 {
				t.Errorf("the dispenser keeps %+v, want the box", left)
			}
			if items := srv.ItemsInDimension("minecraft:overworld"); len(items) != 0 {
				t.Errorf("%d items on the ground, want none", len(items))
			}
		})
	}
}
