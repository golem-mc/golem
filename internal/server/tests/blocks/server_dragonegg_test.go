package blocks

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// eggCells lists the cells around the egg that hold an egg.
func eggCells(srv interface{ BlockAt(x, y, z int32) uint16 }) [][3]int32 {
	var out [][3]int32
	for x := int32(-14); x <= 22; x++ {
		for y := int32(55); y <= 73; y++ {
			for z := int32(-16); z <= 20; z++ {
				if name, _ := servertest.StateProps(srv.BlockAt(x, y, z)); name == "minecraft:dragon_egg" {
					out = append(out, [3]int32{x, y, z})
				}
			}
		}
	}
	return out
}

func checkEggMoved(t *testing.T, cells [][3]int32) [3]int32 {
	t.Helper()
	if len(cells) != 1 {
		t.Fatalf("%d eggs after the click, want 1", len(cells))
	}
	d := cells[0]
	if d == [3]int32{4, 64, 2} {
		t.Fatalf("the egg stayed in its cell")
	}
	if d[0]-4 < -15 || d[0]-4 > 15 || d[1]-64 < -7 || d[1]-64 > 7 || d[2]-2 < -15 || d[2]-2 > 15 {
		t.Errorf("the egg landed at %v, outside 16x8x16 of its cell", d)
	}
	return d
}

// TestARightClickTeleportsTheDragonEgg: the egg leaves its cell for another
// air cell with a block below.
// vanilla: DragonEggBlock.useWithoutItem
func TestARightClickTeleportsTheDragonEgg(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	srv.StopTick()

	servertest.Setblock(t, c, 4, 63, 2, "dirt")
	servertest.Setblock(t, c, 4, 64, 2, "dragon_egg")
	c.Settle()
	c.UseBlock(4, 64, 2)
	c.Settle()

	d := checkEggMoved(t, eggCells(srv))
	if srv.BlockAt(d[0], d[1]-1, d[2]) == 0 {
		t.Errorf("the egg landed at %v over air", d)
	}
}

// TestASurvivalLeftClickTeleportsTheDragonEgg: the attack moves it too.
// vanilla: DragonEggBlock.attack
func TestASurvivalLeftClickTeleportsTheDragonEgg(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	srv.StopTick()

	servertest.Setblock(t, c, 4, 63, 2, "dirt")
	servertest.Setblock(t, c, 4, 64, 2, "dragon_egg")
	c.ChatCommand("gamemode survival")
	c.Settle()
	c.StartDigging(4, 64, 2)
	c.Settle()

	checkEggMoved(t, eggCells(srv))
}
