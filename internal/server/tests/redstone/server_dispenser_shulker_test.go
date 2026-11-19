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
