package items

import (
	"math"
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestBurpPitchRoundsBoundsInFloat32 checks the burp's pitch bits on the wire
// for a draw where float32(0.9)+f*float32(0.1) differs from Mth.randomBetween.
// vanilla: FoodProperties.onConsume, Mth.randomBetween
func TestBurpPitchRoundsBoundsInFloat32(t *testing.T) {
	t.Parallel()
	lo, hi := float32(0.9), float32(1.0)
	// Pick a seed whose
	// burp draw differs between the two formulas.
	// Eat pitch (2 draws) and the consume animation draws precede the burp.
	const drawsBeforeBurp = 6
	seed := int64(-1)
	for s := int64(1); s < 10000 && seed < 0; s++ {
		rng := rand.New(rand.NewSource(s))
		for range drawsBeforeBurp {
			rng.Float32()
		}
		f := rng.Float32()
		if math.Float32bits(float32(f*(hi-lo))+lo) != math.Float32bits(lo+f*float32(0.1)) {
			seed = s
		}
	}
	if seed < 0 {
		t.Fatal("no seed distinguishes the formulas")
	}

	cfg := servertest.SurvivalTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	cfg.LevelName = t.TempDir()
	srv, connect := servertest.Start(t, cfg)
	servertest.FreezeUntilHeld(t, srv)
	actor := connect().Join("Actor")
	actor.MoveToGround(0.5, 64, 0.5, true)
	servertest.HoldTicks(t, srv)
	srv.SetSpawning(false)
	srv.SetSoundSeed(seed)
	srv.RunLine(`item replace entity Actor weapon.mainhand with stick[consumable={consume_seconds:0},food={nutrition:1,saturation:0,can_always_eat:true}]`)
	actor.Settle()
	actor.UseItemRightClicks(protocol.HandMain)
	actor.Settle()
	servertest.RunTicks(t, srv, 1)
	rng := rand.New(rand.NewSource(seed))
	for range drawsBeforeBurp {
		rng.Float32()
	}
	want := float32(rng.Float32()*(hi-lo)) + lo
	got := float32(-1)
	actor.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlaySoundEffect {
			return
		}
		sound := servertest.DecodePlaySoundEffect(t, data)
		if sound.Category == protocol.SoundCategoryPlayer {
			got = sound.Pitch
		}
	})
	if math.Float32bits(got) != math.Float32bits(want) {
		t.Fatalf("burp pitch = %v, want %v", got, want)
	}
}
