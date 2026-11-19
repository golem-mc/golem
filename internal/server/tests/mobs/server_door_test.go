package mobs

import (
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
