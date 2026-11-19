package sounds

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// placedSound is one Sound Effect packet with where it was played.
type placedSound struct {
	name          string
	category      int32
	x, z          float64
	volume, pitch float32
}

// playerSoundsTicks returns the Sound Effects c is sent over the next n ticks
// that a moving player makes: steps, chimes, swims and splashes.
func playerSoundsTicks(c *servertest.Client, srv *server.Server, n int) []placedSound {
	c.T.Helper()
	var heard []placedSound
	c.ReadTicks(srv, n, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySoundEffect || !servertest.IsPlayerMovementSound(data) {
			return false
		}
		r := protocol.NewReader(data)
		soundID := r.ReadVarInt()
		name := ""
		if soundID != 0 {
			name, _ = servertest.SoundEventName(soundID - 1)
		} else {
			name = r.ReadString(32767)
		}
		s := placedSound{name: name, category: r.ReadVarInt()}
		s.x = float64(r.ReadI32()) / 8
		r.ReadI32()
		s.z = float64(r.ReadI32()) / 8
		s.volume, s.pitch = r.ReadF32(), r.ReadF32()
		heard = append(heard, s)
		return false
	})
	return heard
}

// countNamed is how many of heard are named name on the players category.
func countNamed(heard []placedSound, name string) int {
	n := 0
	for _, s := range heard {
		if s.name == name && s.category == protocol.SoundCategoryPlayer {
			n++
		}
	}
	return n
}

// TestOtherPlayersHearAPlayerWalkSplashAndSwim walks a player over the
// stone flat and into a pool two deep: the other player hears each step as
// block.stone.step at 0.15 by the walker, one splash as the walker enters
// the water, and the swim sound as they go on in it; the walker, whose own
// client plays these, is sent none of them.
// vanilla: Entity.applyMovementEmissionAndPlaySound, Entity.updateFluidInteraction,
// Player.playStepSound, Player.playSound
func TestOtherPlayersHearAPlayerWalkSplashAndSwim(t *testing.T) {
	t.Parallel()
	srv, walker := servertest.Join(t, servertest.SurvivalTestConfig(), "Walker")
	listener := servertest.DialServer(t, srv).Join("Listener")
	water := servertest.BlockState(t, "minecraft:water")
	for x := int32(6); x <= 10; x++ {
		for z := int32(-1); z <= 1; z++ {
			srv.SetBlock(x, 62, z, water)
			srv.SetBlock(x, 63, z, water)
		}
	}
	listener.MoveToGround(3.5, 64, 4.5, true)
	listener.Settle()
	walker.Settle()
	servertest.HoldTicks(t, srv)
	playerSoundsTicks(listener, srv, 1)

	ownSounds := 0
	ownSound := func(id int32, data []byte) {
		if id == protocol.IDPlaySoundEffect && servertest.IsPlayerMovementSound(data) {
			ownSounds++
		}
	}
	for x := 0.75; x <= 5.5; x += 0.25 {
		walker.MoveToGround(x, 64, 0.5, true)
	}
	walker.SettleAndCollect(ownSound)
	heard := playerSoundsTicks(listener, srv, 1)
	steps := countNamed(heard, "minecraft:block.stone.step")
	if steps < 2 || steps != len(heard) {
		t.Fatalf("listener heard %+v over a walk of 4.75 blocks, want two or more block.stone.step and nothing else", heard)
	}
	for _, s := range heard {
		if s.volume != 0.15 || s.pitch != 1 {
			t.Errorf("step at volume %v pitch %v, want 0.15 and 1", s.volume, s.pitch)
		}
		if s.x < 0.5 || s.x > 5.5 || math.Abs(s.z-0.5) > 0.25 {
			t.Errorf("step at (%v, %v), want on the walker's way along z 0.5", s.x, s.z)
		}
	}

	// Over the pool, then down into it, then on along its floor.
	walker.MoveToGround(6.5, 64, 0.5, false)
	walker.MoveToGround(6.5, 63, 0.5, false)
	walker.MoveToGround(6.5, 62, 0.5, true)
	walker.SettleAndCollect(ownSound)
	heard = playerSoundsTicks(listener, srv, 1)
	splashes := countNamed(heard, "minecraft:entity.player.splash") + countNamed(heard, "minecraft:entity.player.splash.high_speed")
	if splashes != 1 {
		t.Errorf("listener heard %+v entering the pool, want one entity.player.splash", heard)
	}

	for x := 6.75; x <= 9.5; x += 0.25 {
		walker.MoveToGround(x, 62, 0.5, true)
	}
	walker.SettleAndCollect(ownSound)
	heard = playerSoundsTicks(listener, srv, 1)
	if countNamed(heard, "minecraft:entity.player.swim") == 0 {
		t.Errorf("listener heard %+v going on in the pool, want entity.player.swim", heard)
	}
	if n := countNamed(heard, "minecraft:entity.player.splash") + countNamed(heard, "minecraft:entity.player.splash.high_speed"); n != 0 {
		t.Errorf("listener heard %d more splashes going on in the pool, want none", n)
	}

	if ownSounds != 0 {
		t.Errorf("walker was sent %d of its own movement sounds, want none", ownSounds)
	}
}
