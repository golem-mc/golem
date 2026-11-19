package world

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/nbt"
)

func TestTemplateEntityBlockPosFollowsPlacedPos(t *testing.T) {
	for _, id := range []string{"minecraft:cushion", "minecraft:leash_knot", "minecraft:item_frame", "minecraft:glow_item_frame"} {
		e := TemplateEntity{
			Pos:      [3]float64{2.5, 1, 3.5},
			BlockPos: [3]int32{2, 1, 3},
			NBT:      nbt.Compound{"id": nbt.String(id), "block_pos": nbt.IntArray{2, 1, 3}},
		}
		got, ok := e.place(TemplatePlacement{Origin: [3]int32{-100, 64, 50}, Rotation: RotateClockwise90})
		if !ok {
			t.Fatal(id)
		}
		want := nbt.IntArray{int32(math.Floor(got.Pos[0])), int32(math.Floor(got.Pos[1])), int32(math.Floor(got.Pos[2]))}
		if bp, _ := nbt.Get[nbt.IntArray](got.Tag, "block_pos"); !nbt.Equal(bp, want) {
			t.Fatalf("%s: block_pos %v, want %v", id, bp, want)
		}
	}
	e := TemplateEntity{Pos: [3]float64{1, 1, 1}, NBT: nbt.Compound{"id": nbt.String("minecraft:painting"), "block_pos": nbt.IntArray{1, 1, 1}}}
	got, _ := e.place(TemplatePlacement{Origin: [3]int32{10, 10, 10}})
	if bp, _ := nbt.Get[nbt.IntArray](got.Tag, "block_pos"); bp[0] != 1 {
		t.Fatalf("painting block_pos changed: %v", bp)
	}
}
