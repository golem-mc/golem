package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/registry"
)

// A MobMoaned override still evaluates AmbientVoice before replacing the sound.
// Skipping those dice would change the mob's later AI and voice pitch.
func TestMobMoanIDOverrideKeepsAmbientDice(t *testing.T) {
	m := entity.NewMob(entity.TypeAllay, 1, 1.5, 64, 2.5, 17)
	control := entity.NewMob(entity.TypeAllay, 1, 1.5, 64, 2.5, 17)
	control.AmbientVoice()
	ev := entity.MobEvent{Mob: m, Kind: entity.MobMoaned, SoundID: registry.SoundEntityAllayItemThrown, Volume: 1, Pitch: 0.75}
	var fx mobEffects
	tick := mobDimTick{}
	tick.event(&fx, nil, ev)
	if m.Rand.Int63() != control.Rand.Int63() {
		t.Fatal("sound override changed ambient random draws")
	}
	if len(fx.sends) != 1 {
		t.Fatalf("got %d sounds, want one", len(fx.sends))
	}
	got := decodeSoundEffect(t, fx.sends[0].data)
	if got.sound.registryID != int32(registry.SoundEntityAllayItemThrown)-1 || got.category != protocol.SoundCategoryNeutral || got.volume != 1 || got.pitch != 0.75 {
		t.Fatalf("override packet = %+v", got)
	}
}
