package explosions

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestABlastMergesTheStacksItsBlocksDrop turns the tnt decay rule off and
// primes a body over a slab of dirt: every dirt block gives one dirt item,
// and the blast spawns them merged into stacks of sixteen rather than one
// item entity per block.
func TestABlastMergesTheStacksItsBlocksDrop(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Watcher")
	srv.SetGameRule("tnt_explosion_drop_decay", false)

	dirtState, ok := registry.FindState("minecraft:dirt", nil)
	if !ok {
		t.Fatal("the block registry has no dirt")
	}
	dirt, ok := registry.ItemID("minecraft:dirt")
	if !ok {
		t.Fatal("the item registry has no dirt")
	}
	for x := int32(0); x <= 9; x++ {
		for y := int32(64); y <= 67; y++ {
			for z := int32(-4); z <= 5; z++ {
				srv.SetBlock(x, y, z, dirtState)
			}
		}
	}
	servertest.HoldTicks(t, srv)
	// Inside the slab, so the rays chew a ball rather than a crust.
	if srv.SpawnTNT(4.5, 66, 0.5, 20) == 0 {
		t.Fatal("the primed body never stood up")
	}
	var col blastCollector
	c.ReadTicks(srv, 60, func(id int32, data []byte) bool { return col.handle(c, id, data) })
	if !col.exploded {
		t.Fatal("the fuse never ran out")
	}
	drops := col.drops
	var stacks, items int
	for _, d := range drops {
		if d.Stack.Count > 16 {
			t.Errorf("a drop holds %d, want at most the collector's 16", d.Stack.Count)
		}
		if d.Stack.ItemID == dirt {
			stacks++
			items += int(d.Stack.Count)
		}
	}
	if items < 64 {
		t.Fatalf("the blast dropped %d dirt, want enough to fill several stacks", items)
	}
	if want := (items + 15) / 16; stacks != want {
		t.Errorf("%d item entities for %d dirt, want %d merged stacks", stacks, items, want)
	}
	t.Logf("%d dirt in %d item entities", items, stacks)
}
