package entity

import (
	"testing"
)

// seatWorld reads air from the blocks, which testWorld does not.
type seatWorld struct{ testWorld }

func (w seatWorld) IsAirAt(x, y, z int32) bool { return w.BlockAt(x, y, z) == 0 }

// TestACatSeatIsAFreeChestALitFurnaceOrABedsFoot: the head of a bed, a chest
// somebody has open and an unlit furnace are no seat, and a seat needs air
// above it.
// vanilla: CatSitOnBlockGoal.isValidTarget
func TestACatSeatIsAFreeChestALitFurnaceOrABedsFoot(t *testing.T) {
	cat := NewMob(TypeCat, 1, 0.5, 64, 0.5, 1)
	g := newCatSitOnBlockGoal(cat)
	bed := func(part string) uint16 {
		return beeBlock(t, "minecraft:red_bed", map[string]string{"part": part, "facing": "south", "occupied": "false"})
	}
	chest := beeBlock(t, "minecraft:chest", map[string]string{"facing": "north"})
	furnace := func(lit string) uint16 {
		return beeBlock(t, "minecraft:furnace", map[string]string{"facing": "north", "lit": lit})
	}
	cat.ChestOpen = func(x, y, z int32) bool { return x == 9 }
	for _, tc := range []struct {
		name  string
		state uint16
		x     int32
		above uint16
		want  bool
	}{
		{"bed foot", bed("foot"), 0, 0, true},
		{"bed head", bed("head"), 0, 0, false},
		{"free chest", chest, 0, 0, true},
		{"open chest", chest, 9, 0, false},
		{"covered chest", chest, 0, beeBlock(t, "minecraft:stone", nil), false},
		{"lit furnace", furnace("true"), 0, 0, true},
		{"unlit furnace", furnace("false"), 0, 0, false},
	} {
		w := seatWorld{newTestWorld()}
		w.blocks.set(tc.x, 64, 0, tc.state)
		if tc.above != 0 {
			w.blocks.set(tc.x, 65, 0, tc.above)
		}
		if got := g.seat(w, tc.x, 64, 0); got != tc.want {
			t.Errorf("%s: seat = %v, want %v", tc.name, got, tc.want)
		}
	}
}
