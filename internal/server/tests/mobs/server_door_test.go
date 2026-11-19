package mobs

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// doorSiege walls the player into a ring two blocks out, two high, with one
// door of the kind given in its east side, and stands a zombie outside it.
// The ring is too tall to climb, so the door is the only way in. It returns the
// cells of the door's halves and runs the siege for ticks, reporting whether
// the lower half was gone by the end. An observer sees every packet and
// returns true to end the siege early.
func doorSiege(t *testing.T, difficulty uint8, door string, breaksDoors bool, ticks uint64, observe func(id int32, data []byte) bool) (lowerGone bool) {
	t.Helper()
	srv, connect := servertest.Start(t, servertest.HostileTestConfig())
	srv.SetDifficulty(difficulty)
	c := connect()
	c.JoinAs("Besieged")
	srv.SetDayTime(18000)
	srv.SetSpawning(false)

	pos := srv.PlayerPosition("Besieged")
	px, py, pz := int32(math.Floor(pos.X)), int32(math.Floor(pos.Y)), int32(math.Floor(pos.Z))
	stone := registry.BlockStates["minecraft:stone"]
	for dx := int32(-2); dx <= 2; dx++ {
		for dz := int32(-2); dz <= 2; dz++ {
			if max(dx, -dx, dz, -dz) != 2 {
				continue
			}
			srv.SetBlock(px+dx, py, pz+dz, stone)
			srv.SetBlock(px+dx, py+1, pz+dz, stone)
		}
	}
	for half, y := range map[string]int32{"lower": py, "upper": py + 1} {
		srv.SetBlock(px+2, y, pz, servertest.BlockStateWith(t, door,
			"facing=west,half="+half+",hinge=left,open=false,powered=false"))
	}

	// Seed 4 rolls an adult: a baby is faster and smaller,
	// and its speed modifier fails ExpectMobSpawn.
	id := srv.SpawnMobSeeded("minecraft:zombie", pos.X+3.5, pos.Y, pos.Z, 4)
	c.ExpectMobSpawn(protocol.EntityTypeZombie)
	if breaksDoors {
		srv.MakeMobLeader(id)
	}
	// Held: the siege is hundreds of ticks, run back to back.
	servertest.HoldTicks(t, srv)
	// The zombie cannot see its prey through the wall, so a blow from inside
	// gives it the grudge that sends it at the door.
	c.Attack(id)
	c.PumpTicks(srv, ticks, func(id int32, data []byte) bool {
		if observe != nil {
			return observe(id, data)
		}
		return srv.BlockAt(px+2, py, pz) == 0
	})
	return srv.BlockAt(px+2, py, pz) == 0
}

// TestAZombieBreaksDownADoorOnHard sends a door-breaking zombie at a walled-in
// player on hard: it cracks the wooden door for 240 ticks and removes it.
// vanilla: BreakDoorGoal.tick
func TestAZombieBreaksDownADoorOnHard(t *testing.T) {
	t.Parallel()
	if !doorSiege(t, 3, "minecraft:oak_door", true, 900, nil) {
		t.Error("the zombie left the wooden door standing on hard")
	}
}

// TestAZombieLeavesTheDoorOnNormal holds the difficulty gate: the same siege
// on normal breaks nothing.
func TestAZombieLeavesTheDoorOnNormal(t *testing.T) {
	t.Parallel()
	if doorSiege(t, 2, "minecraft:oak_door", true, 450, nil) {
		t.Error("the zombie broke the door on normal")
	}
}

// TestAZombieThatCannotBreakDoorsLeavesThem holds the flag: a zombie without
// CanBreakDoors breaks nothing on hard.
func TestAZombieThatCannotBreakDoorsLeavesThem(t *testing.T) {
	t.Parallel()
	if doorSiege(t, 3, "minecraft:oak_door", false, 450, nil) {
		t.Error("a zombie without the flag broke the door")
	}
}

// TestABreakingZombieTellsTheCrackAndTheBreak reads the siege off the wire:
// the crack sound (1019) while it works, the crack stages up to 10, and the
// break sound (1021) with the destroy particles (2001) when the door goes.
// vanilla: BreakDoorGoal.tick
func TestABreakingZombieTellsTheCrackAndTheBreak(t *testing.T) {
	t.Parallel()
	events := map[int32]int{}
	type cell struct{ x, y, z, data int32 }
	var breakCell cell
	var destroys []cell
	var stages []int
	doorSiege(t, 3, "minecraft:oak_door", true, 900, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayWorldEvent:
			ev := int32(binary.BigEndian.Uint32(data))
			events[ev]++
			r := protocol.NewReader(data[4:])
			var c cell
			c.x, c.y, c.z = r.ReadPosition()
			c.data = int32(r.ReadI32())
			switch ev {
			case 1021:
				breakCell = c
			case 2001:
				destroys = append(destroys, c)
			}
		case protocol.IDPlayBlockDestruction:
			// entity id VarInt, position, stage
			r := protocol.NewReader(data)
			r.ReadVarInt()
			r.ReadPosition()
			stages = append(stages, int(r.ReadU8()))
		}
		// The clear (255) ends the goal; the break sound and both destroy
		// events come with the removal, so the wire has said everything.
		return len(stages) > 0 && stages[len(stages)-1] == 255 && events[1021] > 0 && len(destroys) >= 2
	})
	if events[1019] == 0 {
		t.Error("no crack sound (1019) was sent")
	}
	if events[1021] != 1 {
		t.Errorf("%d break sounds (1021), want 1", events[1021])
	}
	// The upper half's own shape-update break plays a 2001 one block up, with
	// its door state; the goal's own names the lower cell and, read after the
	// removal, air (0).
	var own []cell
	for _, d := range destroys {
		if d.x == breakCell.x && d.y == breakCell.y && d.z == breakCell.z {
			own = append(own, d)
		}
	}
	if len(own) != 1 || own[0].data != 0 {
		t.Errorf("destroy events at the door cell %v, want exactly one with data 0", own)
	}
	// The final 255 is the clear (-1) the goal's stop sends.
	if n := len(stages); n < 12 || stages[n-2] != 10 || stages[n-1] != 255 {
		t.Errorf("crack stages %v, want a climb to 10 then the clear", stages)
	}
}
