package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// stateArgsWorld is a flat world with a tall grass on dirt at (1, 64, 1), a
// note block under air at (3, 64, 3) and a north-facing double copper chest
// at (5..6, 64, 5).
func stateArgsWorld(b *testing.B) (*World, *Physics) {
	w, _ := NewWorld(b.TempDir(), NewFlatGenerator())
	b.Cleanup(func() { _ = w.Close() })
	w.Acquire(ChunkPos{})
	w.SetBlock(1, GroundLevel-1, 1, registry.MustState("minecraft:dirt", nil))
	w.SetBlock(1, GroundLevel, 1, registry.MustState("minecraft:tall_grass", map[string]string{"half": "lower"}))
	w.SetBlock(1, GroundLevel+1, 1, registry.MustState("minecraft:tall_grass", map[string]string{"half": "upper"}))
	w.SetBlock(3, GroundLevel, 3, registry.MustState(noteBlockName, nil))
	w.SetBlock(5, GroundLevel, 5, registry.MustState("minecraft:copper_chest", map[string]string{"facing": "north", "type": "left"}))
	w.SetBlock(6, GroundLevel, 5, registry.MustState("minecraft:copper_chest", map[string]string{"facing": "north", "type": "right"}))
	return w, NewPhysics(w)
}

// BenchmarkDoublePlantNeighbourChange is a write under a tall grass's lower
// half, with its notification.
func BenchmarkDoublePlantNeighbourChange(b *testing.B) {
	benchWriteNear(b, 1, GroundLevel-1, 1)
}

// BenchmarkDoublePlantSideChange is a write beside a supported tall grass.
func BenchmarkDoublePlantSideChange(b *testing.B) {
	benchWriteNear(b, 0, GroundLevel, 1)
}

// BenchmarkOrdinaryNeighbourChange is a write with no double plant around it.
func BenchmarkOrdinaryNeighbourChange(b *testing.B) {
	benchWriteNear(b, 9, GroundLevel, 9)
}

// benchWriteNear swaps the block at (x, y, z) between grass block and dirt (both support vegetation) and
// notifies the change, as a block write does.
func benchWriteNear(b *testing.B, x, y, z int32) {
	w, p := stateArgsWorld(b)
	stone := registry.MustState("minecraft:grass_block", map[string]string{"snowy": "false"})
	dirt := registry.MustState("minecraft:dirt", nil)
	w.SetBlock(x, y, z, stone)
	lower := w.BlockAt(1, GroundLevel, 1)
	b.ReportAllocs()
	next := dirt
	for range b.N {
		old, _ := w.SwapBlock(x, y, z, next)
		p.NotifyChanged(x, y, z, old)
		if next == dirt {
			next = stone
		} else {
			next = dirt
		}
	}
	if w.BlockAt(1, GroundLevel, 1) != lower {
		b.Fatal("the tall grass changed")
	}
}

// BenchmarkDoublePlantSurvives is the placement's survival question for a
// tall grass's lower half on dirt.
func BenchmarkDoublePlantSurvives(b *testing.B) {
	w, _ := stateArgsWorld(b)
	lower := w.BlockAt(1, GroundLevel, 1)
	b.ReportAllocs()
	for range b.N {
		if !w.DoublePlantSurvives(lower, 1, GroundLevel, 1) {
			b.Fatal("the tall grass does not survive on dirt")
		}
	}
}

// BenchmarkNoteBlockAttack is a left-click on a note block under air: the
// note's sound and block action.
func BenchmarkNoteBlockAttack(b *testing.B) {
	_, p := stateArgsWorld(b)
	b.ReportAllocs()
	for range b.N {
		if _, effects := p.AttackBlock(3, GroundLevel, 3); len(effects) != 1 {
			b.Fatalf("the note block played %d effects, want 1", len(effects))
		}
	}
}

// BenchmarkNoteBlockUse is a right-click on a note block: the note steps up
// and plays.
func BenchmarkNoteBlockUse(b *testing.B) {
	_, p := stateArgsWorld(b)
	b.ReportAllocs()
	for range b.N {
		if used, _, effects, _ := p.UseBlock(3, GroundLevel, 3, ""); !used || len(effects) != 1 {
			b.Fatalf("the note block's use answered %v with %d effects", used, len(effects))
		}
	}
}

// BenchmarkCopperChestShapeUpdate is a double copper chest half's shape
// update from its partner, which keeps the pair as it stands.
func BenchmarkCopperChestShapeUpdate(b *testing.B) {
	w, p := stateArgsWorld(b)
	half := w.BlockAt(5, GroundLevel, 5)
	b.ReportAllocs()
	var changes []BlockChange
	for range b.N {
		changes = changes[:0]
		p.shapeUpdate(5, GroundLevel, 5, [3]int32{-1, 0, 0}, &changes)
	}
	if w.BlockAt(5, GroundLevel, 5) != half || len(changes) != 0 {
		b.Fatalf("the copper chest changed: %v", changes)
	}
}
