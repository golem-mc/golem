package world

import (
	"math"
	"reflect"
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestSectionCollisionCounts(t *testing.T) {
	for _, name := range []string{"air", "water", "short_grass", "stone", "powder_snow", "lava", "scaffolding", "moving_piston", "shulker_box"} {
		t.Run(name, func(t *testing.T) {
			block, ok := registry.BlockByName("minecraft:" + name)
			if !ok {
				t.Fatal(name)
			}
			state := block.DefaultState()
			want := 1
			if name == "air" || name == "water" || name == "short_grass" {
				want = 0
			}
			s := section{fill: airState}
			s.set(17, state, false)
			if s.colliding != want {
				t.Fatalf("after set: %d, want %d", s.colliding, want)
			}
			s.recalcCounts()
			if s.colliding != want {
				t.Fatalf("after recount: %d, want %d", s.colliding, want)
			}
			s.set(17, airState, false)
			if s.colliding != 0 {
				t.Fatalf("after remove: %d", s.colliding)
			}
			s.setAll(state)
			if s.colliding != want*blocksPerSection {
				t.Fatalf("after setAll: %d", s.colliding)
			}
			s.colliding = -1
			s.recalcCounts()
			if s.colliding != want*blocksPerSection {
				t.Fatalf("after uniform recount: %d", s.colliding)
			}
		})
	}
}

func TestCollisionCellsMatchDenseRead(t *testing.T) {
	w := newTestWorld(t, NewFlatGenerator())
	for i, pos := range []ChunkPos{{-1, -1}, {-1, 0}, {0, -1}, {0, 0}} {
		c, _ := w.Acquire(pos)
		defer w.Release(pos)
		// Uniform, four-bit, five-bit and direct sections, like BlocksIn.
		states := []int{1, 15, 31, 257}[i]
		for y := int32(48); y < 80; y++ {
			for z := range int32(16) {
				for x := range int32(16) {
					c.SetBlock(x, y, z, uint16((int(x+16*z+256*(y-48)))%states))
				}
			}
		}
	}
	for _, box := range [][6]int32{
		{-18, 46, -18, 18, 81, 18},
		{-18, worldMinY - 2, -2, 2, worldMinY + 2, 2},
		{-2, worldMinY + worldHeight - 2, -2, 2, worldMinY + worldHeight + 2, 2},
		{0, 64, 0, 0, 64, 0},
		{2, 65, 2, 1, 66, 3},
		{0, math.MaxInt32 - 1, 0, 1, math.MaxInt32, 1},
		{0, math.MinInt32, 0, 1, math.MinInt32 + 1, 1},
	} {
		got := w.CollisionCellsIn(box[0], box[1], box[2], box[3], box[4], box[5], []CollisionCell{{State: 1}})
		var want []CollisionCell
		for x := box[0]; x <= box[3]; x++ {
			for y := max(box[1], worldMinY); y <= min(box[4], worldMinY+worldHeight-1); y++ {
				for z := box[2]; z <= box[5]; z++ {
					state := w.BlockAt(x, y, z)
					if MayCollide(state) {
						_, plain := PlainCollision(state)
						want = append(want, CollisionCell{x, y, z, state, plain})
					}
				}
			}
		}
		if !reflect.DeepEqual(append([]CollisionCell(nil), got...), want) {
			t.Fatalf("box %v: sparse cells differ from ordered BlockAt reads", box)
		}
	}
}
