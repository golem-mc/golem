package blocks

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"

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

// TestADragonEggStaysInsideTheWorldBorder: a cell outside the border is
// refused, so the egg lands within the 3x3 columns the border holds.
// vanilla: DragonEggBlock.teleport
func TestADragonEggStaysInsideTheWorldBorder(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	srv.StopTick()

	servertest.Setblock(t, c, 4, 63, 2, "dirt")
	servertest.Setblock(t, c, 4, 64, 2, "dragon_egg")
	c.ChatCommand("worldborder center 4 2")
	c.ChatCommand("worldborder set 3")
	c.Settle()
	c.UseBlock(4, 64, 2)
	c.Settle()

	d := checkEggMoved(t, eggCells(srv))
	if d[0] < 3 || d[0] > 5 || d[2] < 1 || d[2] > 3 {
		t.Errorf("the egg landed at %v, outside the border's 3x3 columns", d)
	}
}

// TestADragonEggTeleportSendsItsParticleEvent: the client draws the trail
// from level event 2015 at the old cell, whose data packs the offset to the
// new one with radii 16, 8, 16.
// vanilla: DragonEggBlock.teleport
func TestADragonEggTeleportSendsItsParticleEvent(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	srv.StopTick()

	servertest.Setblock(t, c, 4, 63, 2, "dirt")
	servertest.Setblock(t, c, 4, 64, 2, "dragon_egg")
	c.ChatCommand("gamemode survival")
	c.Settle()
	c.StartDigging(4, 64, 2)
	var events [][4]int32
	c.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlayWorldEvent {
			return
		}
		if event, x, y, z, d := servertest.ReadWorldEvent(t, data); event == protocol.WorldEventDragonEggTeleport {
			events = append(events, [4]int32{x, y, z, d})
		}
	})

	d := checkEggMoved(t, eggCells(srv))
	if len(events) != 1 {
		t.Fatalf("got %d dragon egg teleport events, want 1", len(events))
	}
	ev := events[0]
	if [3]int32{ev[0], ev[1], ev[2]} != [3]int32{4, 64, 2} {
		t.Errorf("the event is at %v, want the old cell 4 64 2", ev[:3])
	}
	if to := servertest.UnpackPosDiff(4, 64, 2, ev[3], 16, 8, 16); to != d {
		t.Errorf("the event's data points at %v, want the egg's new cell %v", to, d)
	}
}
