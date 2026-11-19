package raids

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// ringBellAndWatch rings a bell on a pillager dx blocks east of it and
// collects whether the resonate sound reached the client over ticks.
func ringBellAndWatch(t *testing.T, dx int32, ticks int) (glowing, resonated bool) {
	t.Helper()
	srv, c, bell := bareBell(t, rules.DifficultyNormal)
	srv.SetBlock(bell[0], bell[1], bell[2], servertest.BellState(t, "floor", "north", false))
	id := srv.SpawnMob("minecraft:pillager", float64(bell[0]+dx)+0.5, float64(bell[1]), float64(bell[2])+0.5)
	srv.SetMobNoAI(id)
	resonate, ok := registry.SoundEventID("minecraft:block.bell.resonate")
	if !ok {
		t.Fatal("no resonate sound in the registry")
	}
	c.PlaceBlockAtCursor(bell[0], bell[1], bell[2], protocol.FaceNorth, 0.5, 0.5, 0.5)
	c.ReadTicks(srv, ticks, func(pid int32, data []byte) bool {
		if pid == protocol.IDPlaySoundEffect && protocol.NewReader(data).ReadVarInt()-1 == int32(resonate) {
			resonated = true
		}
		return false
	})
	_, _, glowing = srv.MobEffectOf(id, "minecraft:glowing")
	return glowing, resonated
}

// TestARungBellMakesNearbyRaidersGlow: a raider within 32 blocks starts the
// bell resonating 5 ticks into the swing, and 40 ticks later it glows.
// vanilla: BellBlockEntity.serverTick, makeRaidersGlow
func TestARungBellMakesNearbyRaidersGlow(t *testing.T) {
	t.Parallel()
	glowing, resonated := ringBellAndWatch(t, 20, 55)
	if !resonated {
		t.Errorf("no resonate sound for a raider 20 blocks from the bell")
	}
	if !glowing {
		t.Errorf("the raider 20 blocks from a rung bell is not glowing")
	}
}
