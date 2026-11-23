package world

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/nbt"
)

func TestEndGatewayTickPreservesCarriedSnapshotsAndSlots(t *testing.T) {
	t.Parallel()
	for _, count := range []int{1, 256} {
		t.Run(map[int]string{1: "singleton", 256: "populated"}[count], func(t *testing.T) {
			w := newTestWorld(t, NewFlatGenerator())
			pos := ChunkPos{0, 0}
			c, _ := w.Acquire(pos)
			defer w.Release(pos)
			var wantBeams [][3]int32
			for i := range count {
				kind := "minecraft:test_filler" // an id no live kind decodes, so it stays carried
				age := nbt.Long(2399 + (i/8)%2)
				if i%8 == 0 {
					kind = "minecraft:end_gateway"
					if age == 2399 {
						wantBeams = append(wantBeams, [3]int32{int32(i % 16), 80, int32(i / 16)})
					}
				}
				w.SetBlockEntityTag(int32(i%16), 80, int32(i/16), kind, nbt.Compound{"Age": age})
			}
			before := w.TickingCarriedOfKind("minecraft:end_gateway")
			// A pending save retains the compounds after releasing the world lock.
			saved, err := encodeChunkNBT(c)
			if err != nil {
				t.Fatal(err)
			}
			savedEntities, _ := nbt.Get[nbt.List](saved, "block_entities")
			slots := c.carriedBlockEntities.Items
			loaded := w.chunks[pos]
			loaded.dirty = false
			ticking := c.tickingCarried

			if beams := w.TickEndGateways(2400); !slices.Equal(beams, wantBeams) {
				t.Errorf("beams = %v, want %v", beams, wantBeams)
			}
			if !loaded.dirty || c.tickingCarried != ticking {
				t.Errorf("dirty = %v, ticking = %d, want true/%d", loaded.dirty, c.tickingCarried, ticking)
			}
			if len(c.carriedBlockEntities.Items) != count || &c.carriedBlockEntities.Items[0] != &slots[0] {
				t.Fatal("tick replaced the carried list's storage")
			}
			for i, item := range c.carriedBlockEntities.Items {
				entry, _ := nbt.CompoundView(item)
				x, y, z := carriedPos(entry)
				if x != int32(i%16) || y != 80 || z != int32(i/16) {
					t.Fatalf("slot %d moved to %d/%d/%d", i, x, y, z)
				}
				want := nbt.Long(2399 + (i/8)%2)
				saved, _ := nbt.CompoundView(savedEntities.Items[i])
				if old := saved["Age"]; old != want {
					t.Errorf("pending save's age at slot %d changed to %v", i, old)
				}
				if i%8 == 0 {
					want++
				}
				if entry["Age"] != want {
					t.Errorf("slot %d age = %v, want %v", i, entry["Age"], want)
				}
			}
			for _, at := range before {
				want := nbt.Long(2399 + ((at.Pos[2]*16+at.Pos[0])/8)%2)
				if at.Tag["Age"] != want {
					t.Errorf("reader's age at %v changed to %v", at.Pos, at.Tag["Age"])
				}
			}
			if beams := w.TickEndGateways(2400); len(beams) != 0 {
				t.Errorf("next tick repeated beams at %v", beams)
			}
		})
	}
}
