package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// eggPitches winds the hen's countdown for n eggs and returns the pitch of
// each egg sound the client hears.
func eggPitches(t *testing.T, c *servertest.Client, hen func(), n int) []float32 {
	t.Helper()
	var pitches []float32
	for range n {
		hen()
		c.SettleAndCollect(func(id int32, data []byte) {
			if id != protocol.IDPlaySoundEffect {
				return
			}
			if name, _, _, _, _, _, pitch := servertest.ReadAmbient(t, data); name == "minecraft:entity.chicken.egg" {
				pitches = append(pitches, pitch)
			}
		})
	}
	return pitches
}

// TestAHensEggSoundVariesInPitch checks Chicken.aiStep's pitch roll.
func TestAHensEggSoundVariesInPitch(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	srv.StopTick()
	srv.SetDayTime(18000)
	c := connect().Join("Farmer")
	id := srv.SpawnMob("minecraft:chicken", 2.5, 64, 1.5)
	c.ExpectMobSpawn(protocol.EntityTypeChicken)

	pitches := eggPitches(t, c, func() { srv.SetEggTimer(id, 1); srv.TickMobs(1) }, 8)
	if len(pitches) != 8 {
		t.Fatalf("heard %d egg sounds, want 8", len(pitches))
	}
	varied := false
	for _, p := range pitches {
		if p < 0.8 || p > 1.2 {
			t.Errorf("egg pitch %v outside [0.8, 1.2]", p)
		}
		if p != pitches[0] {
			varied = true
		}
	}
	if !varied {
		t.Errorf("egg pitch stayed at %v over 8 eggs", pitches[0])
	}
}

// TestAHensEggSoundReachesOnlyThoseInEarshot checks Entity.playSound's range
// for volume 1.0, 16 blocks.
func TestAHensEggSoundReachesOnlyThoseInEarshot(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	srv.StopTick()
	srv.SetDayTime(18000)
	near := connect().Join("Near")
	far := connect().Join("Far")
	far.MoveTo(42.5, 64, 0.5, 0, 0)
	far.Settle()
	near.Settle()
	id := srv.SpawnMob("minecraft:chicken", 2.5, 64, 1.5)
	near.ExpectMobSpawn(protocol.EntityTypeChicken)

	srv.SetEggTimer(id, 1)
	srv.TickMobs(1)
	if got := len(eggPitches(t, near, func() {}, 1)); got != 1 {
		t.Errorf("the player 5 blocks away heard %d egg sounds, want 1", got)
	}
	if got := len(eggPitches(t, far, func() {}, 1)); got != 0 {
		t.Errorf("the player 40 blocks away heard %d egg sounds, want 0", got)
	}
}

// TestASilentHensEggMakesNoSound checks Entity.playSound's isSilent gate.
func TestASilentHensEggMakesNoSound(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	srv.StopTick()
	srv.SetDayTime(18000)
	c := connect().Join("Farmer")
	id := srv.SpawnMob("minecraft:chicken", 2.5, 64, 1.5)
	c.ExpectMobSpawn(protocol.EntityTypeChicken)
	srv.SetMobSilent(id, true)

	before := srv.ItemCount()
	srv.SetEggTimer(id, 1)
	srv.TickMobs(1)
	if got := len(eggPitches(t, c, func() {}, 1)); got != 0 {
		t.Errorf("a silent hen's egg played %d sounds, want 0", got)
	}
	if srv.ItemCount() != before+1 {
		t.Error("the silent hen laid no egg")
	}
}
