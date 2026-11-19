package containers

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// TestContainerSoundRollsTheSoundSource opens a chest with the tick loop
// stopped and both dice seeded, and reads the pitch back off the wire: the
// lid's roll is the sound source's first, not the environment's, whose
// sequence the tick loop runs too.
func TestContainerSoundRollsTheSoundSource(t *testing.T) {
	t.Parallel()
	const soundSeed, environmentSeed = 73831, 91121
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Listener")

	chest, ok := registry.FindState("minecraft:chest", nil)
	if !ok {
		t.Fatal("the registry has no chest")
	}
	if !srv.SetBlock(4, 64, 0, chest) {
		t.Fatal("the chest has no loaded chunk")
	}
	c.Settle()

	// With the loop stopped the environment source does not roll between the
	// seed and the open, so both sources are the test's to place: the pitch
	// the open plays is the sound source's first.
	srv.StopTick()
	srv.SetSoundSeed(soundSeed)
	srv.SetEnvironmentSeed(environmentSeed)

	c.UseBlock(4, 64, 0)
	pitch := servertest.AwaitSoundPitch(t, c, "minecraft:block.chest.open")
	want := float32(0.9 + rand.New(rand.NewSource(soundSeed)).Float64()*0.1)
	if pitch != want {
		t.Errorf("the chest open played at pitch %v, want the sound source's first roll %v", pitch, want)
	}
}
