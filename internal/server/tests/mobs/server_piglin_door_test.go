package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestAPiglinBruteOpensTheDoorToItsPrey walls a player in with a wooden door
// and stands a piglin brute outside: it paths through the door, opening it.
// vanilla: AbstractPiglin.applyOpenDoorsAbility, PiglinBruteAi.initCoreActivity
func TestAPiglinBruteOpensTheDoorToItsPrey(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.HostileTestConfig())
	srv.SetDifficulty(2)
	c := connect()
	c.JoinAs("Besieged")
	srv.SetDayTime(18000)
	srv.SetSpawning(false)

	pos := srv.PlayerPosition("Besieged")
	px, py, pz := int32(math.Floor(pos.X)), int32(math.Floor(pos.Y)), int32(math.Floor(pos.Z))
	stone := registry.BlockStates["minecraft:stone"]
	for dx := int32(-2); dx <= 2; dx++ {
		for dz := int32(-2); dz <= 2; dz++ {
			if max(dx, -dx, dz, -dz) == 2 {
				srv.SetBlock(px+dx, py, pz+dz, stone)
				srv.SetBlock(px+dx, py+1, pz+dz, stone)
			}
		}
	}
	for half, y := range map[string]int32{"lower": py, "upper": py + 1} {
		srv.SetBlock(px+2, y, pz, servertest.BlockStateWith(t, "minecraft:oak_door",
			"facing=west,half="+half+",hinge=left,open=false,powered=false"))
	}
	open := servertest.BlockStateWith(t, "minecraft:oak_door", "facing=west,half=lower,hinge=left,open=true,powered=false")

	id := srv.SpawnMob("minecraft:piglin_brute", pos.X+3.5, pos.Y, pos.Z)
	// The wall hides the player, so a blow from inside gives the brute the
	// grudge that sends it at the door.
	c.Attack(id)
	opened := false
	c.PumpTicks(srv, 600, func(int32, []byte) bool {
		opened = srv.BlockAt(px+2, py, pz) == open
		return opened
	})
	if !opened {
		t.Error("the piglin brute never opened the door between it and its prey")
	}
}
