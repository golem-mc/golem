package world

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// TestGeneratedStructurePiecesMatchLocations follows references made by the
// real generators without generating terrain in the referenced start chunk.
func TestGeneratedStructurePiecesMatchLocations(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		id     string
		seed   int64
		x, z   int32
		nether bool
	}{
		{"minecraft:fortress", 0, 46, 9, true},
		{"minecraft:bastion_remnant", 0, -38, -16, true},
		{"minecraft:stronghold", 0, 0, 0, false},
		{"minecraft:ancient_city", 12345, -18, -18, false},
		{"minecraft:trial_chambers", 0, -22, -20, false},
	} {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			var gen Generator = NewOverworldGenerator(tc.seed)
			if tc.nether {
				gen = NewNetherGenerator(tc.seed)
			} else if tc.id == "minecraft:stronghold" {
				pos := gen.(*OverworldGenerator).strongholdChunks()[0]
				tc.x, tc.z = pos[0], pos[1]
			}
			saver := gen.(interface{ saveStructures(*Chunk) })
			start := newChunk(tc.x, tc.z)
			saver.saveStructures(start)
			structures, _ := nbt.Get[nbt.Compound](start.savedRoot(), "structures")
			starts, _ := nbt.Get[nbt.Compound](structures, "starts")
			tag, _ := nbt.Get[nbt.Compound](starts, tc.id)
			children, _ := nbt.Get[nbt.List](tag, "Children")
			if len(children.Items) == 0 {
				t.Fatal("no pieces at the vanilla start")
			}
			bb := children.Items[0].(nbt.Compound)["BB"].(nbt.IntArray)
			x, y, z := bb[0], bb[1], bb[2]
			pos := ChunkPos{X: x >> 4, Z: z >> 4}
			c := newChunk(pos.X, pos.Z)
			saver.saveStructures(c)
			w := newTestWorld(t, gen)
			w.mu.Lock()
			w.putChunk(pos, c, nil, false)
			w.mu.Unlock()
			if !w.StructureHasPieceAt(x, y, z, []string{tc.id}) {
				t.Fatal("a generated piece's corner did not match")
			}
			if w.LoadedChunks() != 1 {
				t.Fatal("query loaded terrain for a referenced start")
			}
			if w.StructureHasPieceAt(x, y, z, []string{"minecraft:end_city"}) {
				t.Fatal("a different structure matched")
			}
			if w.StructureHasPieceAt(x, 1000, z, []string{tc.id}) {
				t.Fatal("a position above the pieces matched")
			}
			if w.StructureHasPieceAt(x+16000, y, z, []string{tc.id}) {
				t.Fatal("an unloaded position matched")
			}
		})
	}
}

func TestFullStructureBoundsGrowWithTerrainAdaptation(t *testing.T) {
	w := newTestWorld(t, NewFlatGenerator())
	startPos := ChunkPos{}
	for _, tc := range []struct {
		id      string
		x, y, z int32
		want    bool
	}{
		{"minecraft:pillager_outpost", -12, 48, -12, true},
		{"minecraft:pillager_outpost", -13, 48, -12, false},
		{"minecraft:monument", -1, 60, 0, false},
		{"minecraft:monument", 0, 60, 0, true},
	} {
		c, _ := w.Acquire(startPos)
		c.setStructureStart(tc.id, structureStartNBT(tc.id, 0, 0, []nbt.Tag{
			nbt.Compound{"BB": nbt.IntArray{0, 60, 0, 15, 70, 15}},
		}))
		w.MarkDirty(startPos)
		w.Release(startPos)
		w.FlushSaves()
		q := ChunkPos{X: tc.x >> 4, Z: tc.z >> 4}
		c, _ = w.Acquire(q)
		c.setStructureReferences(tc.id, []int64{packChunk(0, 0)})
		if got := w.StructureContains(tc.x, tc.y, tc.z, tc.id, false); got != tc.want {
			t.Errorf("%s contains (%d,%d,%d) = %v, want %v", tc.id, tc.x, tc.y, tc.z, got, tc.want)
		}
		w.Release(q)
	}
}

func TestGeneratedHutAndMonumentSpawnBounds(t *testing.T) {
	g := NewOverworldGenerator(0)
	for _, tc := range []struct {
		id   string
		x, z int32
	}{
		{"minecraft:swamp_hut", -336, -171},
		{"minecraft:monument", -71, -15},
	} {
		t.Run(tc.id, func(t *testing.T) {
			s := g.growStructure(tc.id, tc.x, tc.z)
			if s == nil {
				t.Fatal("missing vanilla start")
			}
			bb := s.pieces[0].bounds()
			pos := ChunkPos{bb.minX >> 4, bb.minZ >> 4}
			w := newTestWorld(t, NewFlatGenerator())
			c, _ := w.Acquire(ChunkPos{tc.x, tc.z})
			c.setStructureStart(tc.id, s.nbt())
			w.MarkDirty(ChunkPos{tc.x, tc.z})
			c, _ = w.Acquire(pos)
			c.setStructureReferences(tc.id, []int64{packChunk(tc.x, tc.z)})
			if !w.StructureContains(bb.minX, bb.minY, bb.minZ, tc.id, tc.id == "minecraft:swamp_hut") {
				t.Fatal("generated structure's spawn bounds did not match its corner")
			}
			if w.StructureContains(bb.minX, bb.minY-1, bb.minZ, tc.id, tc.id == "minecraft:swamp_hut") {
				t.Fatal("position below structure matched")
			}
			w.Release(pos)
			w.Release(ChunkPos{tc.x, tc.z})
		})
	}
}
