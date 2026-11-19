package mobs

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// The step sound of a walking mob on the wire: the sound of the block under
// its feet at 0.15 of the sound type's volume, a kind's own sound in its
// place, none from a silent body.

// footstep is one step-sound packet.
type footstep struct {
	name          string
	volume, pitch float32
}

// walkOnFloor lays a strip of the named block under a mob's way to a player
// twelve blocks off, lets the mob walk the strip for ticks, and returns the
// step sounds it made.
func walkOnFloor(t *testing.T, kind string, etype int32, floor string, ticks uint64, silent bool) []footstep {
	t.Helper()
	srv, connect := servertest.Start(t, servertest.HostileTestConfig())
	srv.SetDayTime(18000) // midnight, whose dark is no daylight burn
	c := connect()
	c.JoinAs("Prey")
	laid := servertest.BlockState(t, floor)
	for x := int32(-2); x <= 16; x++ {
		for z := int32(-2); z <= 2; z++ {
			srv.SetBlock(x, 63, z, laid)
		}
	}
	c.MoveToGround(-0.5, 64, 0.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)
	id := srv.SpawnMob(kind, 12.5, 64, 0.5)
	c.ExpectMobSpawn(etype)
	if silent && !srv.SetMobSilent(id, true) {
		t.Fatal("the mob is gone before it is silenced")
	}
	return heardSteps(t, srv, c, ticks)
}

// heardSteps runs the server for ticks and returns the step sounds heard.
func heardSteps(t *testing.T, srv *server.Server, c *servertest.Client, ticks uint64) []footstep {
	t.Helper()
	var heard []footstep
	c.PumpTicks(srv, ticks, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySoundEffect {
			return false
		}
		name, _, _, _, _, volume, pitch := servertest.ReadAmbient(t, data)
		if strings.HasSuffix(name, ".step") {
			heard = append(heard, footstep{name, volume, pitch})
		}
		return false
	})
	return heard
}

// TestAWalkingMobStepsOnTheSoundOfTheBlockUnderItsFeet walks a creeper, a
// kind with no step sound of its own, over stone, grass, sand and wool: each
// step it makes is the floor's sound type's step event at 0.15 of its volume.
// vanilla: Entity.playStepSound
func TestAWalkingMobStepsOnTheSoundOfTheBlockUnderItsFeet(t *testing.T) {
	t.Parallel()
	for floor, want := range map[string]string{
		"minecraft:stone":       "minecraft:block.stone.step",
		"minecraft:grass_block": "minecraft:block.grass.step",
		"minecraft:sand":        "minecraft:block.sand.step",
		"minecraft:white_wool":  "minecraft:block.wool.step",
	} {
		t.Run(floor, func(t *testing.T) {
			t.Parallel()
			steps := walkOnFloor(t, "minecraft:creeper", protocol.EntityTypeCreeper, floor, 60, false)
			if len(steps) < 3 {
				t.Fatalf("a creeper walking 60 ticks on %s made %d step sounds, want several", floor, len(steps))
			}
			for _, s := range steps {
				if s.name != want || s.volume != 0.15 || s.pitch != 1 {
					t.Errorf("a creeper on %s stepped %s at volume %v, pitch %v, want %s at 0.15, 1", floor, s.name, s.volume, s.pitch, want)
				}
			}
		})
	}
}
