package redstone

import (
	"strconv"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// composterChanges records the block_change contexts posted at a cell's
// centre.
func composterChanges(srv *server.Server, x, y, z int32) func() []rules.GameEventContext {
	var mu sync.Mutex
	var got []rules.GameEventContext
	srv.ObserveGameEventContexts(func(e string, ex, ey, ez float64, ctx rules.GameEventContext) {
		if e == rules.GameEventBlockChange.Name() && ex == float64(x)+0.5 && ey == float64(y)+0.5 && ez == float64(z)+0.5 {
			mu.Lock()
			got = append(got, ctx)
			mu.Unlock()
		}
	})
	return func() []rules.GameEventContext {
		mu.Lock()
		defer mu.Unlock()
		return append([]rules.GameEventContext(nil), got...)
	}
}

// sourceless reports whether an event context has no source entity.
func sourceless(ctx rules.GameEventContext) bool {
	return ctx.Player == uuid.Nil && ctx.Mob == uuid.Nil && ctx.ProjectileOwner == uuid.Nil
}

// TestAHopperFeedingAComposterPostsBlockChangePerRise: each seed a hopper
// composts that raises the level posts one sourceless block_change on the
// new state; a seed whose roll fails posts none.
// vanilla: ComposterBlock.InputContainer.setChanged, addLayer
func TestAHopperFeedingAComposterPostsBlockChangePerRise(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	srv.SetLoadedBlock(4, 63, 0, composterLevel(t, "0"))
	if !srv.PlaceContainer(4, 64, 0, "minecraft:hopper") {
		t.Fatal("could not place the hopper over the composter")
	}
	srv.StopTick()
	srv.SetLevelSeed(1)
	const seeds = 12
	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:wheat_seeds", seeds)) {
		t.Fatal("the hopper has no block entity to fill")
	}
	changes := composterChanges(srv, 4, 63, 0)

	servertest.TickBlockEntitiesByHand(srv, 8*seeds+1)

	left := int32(0)
	if hopper := srv.InventoryAt(4, 64, 0); !servertest.NothingIn(hopper) {
		left = hopper[0].Count
	}
	_, props := servertest.StatePropsOf(t, srv, 4, 63, 0)
	level, _ := strconv.Atoi(props["level"])
	fed := seeds - int(left)
	if level == 0 || fed <= level {
		t.Fatalf("%d seeds fed raised the level to %d; the test needs both rises and failed rolls", fed, level)
	}
	got := changes()
	if len(got) != level {
		t.Fatalf("block_change events = %d for %d rises out of %d seeds fed, want one per rise", len(got), level, fed)
	}
	for i, ctx := range got {
		if !sourceless(ctx) {
			t.Errorf("rise %d: block_change has a source %+v, want none", i+1, ctx)
		}
		if want := composterLevel(t, strconv.Itoa(i+1)); ctx.Affected != want {
			t.Errorf("rise %d: block_change affected state %d, want level %d (%d)", i+1, ctx.Affected, i+1, want)
		}
	}
}

// TestAHopperDrainingAComposterPostsBlockChange: the hopper below that takes
// a full composter's bone meal posts one sourceless block_change on the
// emptied state. vanilla: ComposterBlock.OutputContainer.setChanged, empty
func TestAHopperDrainingAComposterPostsBlockChange(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	srv.SetLoadedBlock(4, 64, 0, composterLevel(t, "0"))
	if !srv.PlaceContainer(4, 63, 0, "minecraft:hopper") {
		t.Fatal("could not place the hopper under the composter")
	}
	srv.StopTick()
	srv.SetLoadedBlock(4, 64, 0, composterLevel(t, "8"))
	changes := composterChanges(srv, 4, 64, 0)

	servertest.TickBlockEntitiesByHand(srv, 9)
	if hopper := srv.InventoryAt(4, 63, 0); servertest.NothingIn(hopper) {
		t.Fatal("the hopper took nothing from the full composter")
	}
	got := changes()
	if len(got) != 1 {
		t.Fatalf("block_change events = %d after the drain, want 1", len(got))
	}
	if !sourceless(got[0]) {
		t.Errorf("block_change has a source %+v, want none", got[0])
	}
	if want := composterLevel(t, "0"); got[0].Affected != want {
		t.Errorf("block_change affected state %d, want the emptied composter's %d", got[0].Affected, want)
	}
}
