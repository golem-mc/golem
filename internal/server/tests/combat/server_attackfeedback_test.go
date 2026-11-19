package combat

import (
	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
)

// The feedback vanilla's Player.attack gives a blow beside the hurt: the
// critical animations, the attack sounds and the particles.

// feedbackFixture is an attacker and a watcher in one world, the attacker
// on the ground at (2.5, 64, 5.5) and standing still.
func feedbackFixture(t *testing.T) (srv *server.Server, attacker, watcher *servertest.Client) {
	t.Helper()
	srv, attacker, watcher, _ = feedbackFixtureOn(t, servertest.CombatTestConfig())
	return srv, attacker, watcher
}

// feedbackFixtureOn is feedbackFixture on cfg, also returning the connector.
func feedbackFixtureOn(t *testing.T, cfg *config.Config) (srv *server.Server, attacker, watcher *servertest.Client, connect func() *servertest.Client) {
	t.Helper()
	srv, connect = servertest.Start(t, cfg)
	watcher = connect()
	watcher.JoinAs("Watcher")
	attacker = connect()
	attacker.JoinAs("Fencer")
	// Twice, so the last reported move is none and nothing sweeps by speed.
	attacker.MoveToGround(2.5, 64, 5.5, true)
	attacker.MoveToGround(2.5, 64, 5.5, true)
	attacker.Settle()
	watcher.Settle()
	return srv, attacker, watcher, connect
}

// cow plants a cow in reach of the fixture's attacker and drains its spawn.
func feedbackCow(srv *server.Server, attacker, watcher *servertest.Client) int32 {
	id := srv.SpawnMob("minecraft:cow", 4.5, 64, 5.5)
	attacker.Settle()
	watcher.Settle()
	return id
}

func TestAFallingBlowShowsTheCritical(t *testing.T) {
	t.Parallel()
	srv, attacker, watcher := feedbackFixture(t)
	srv.GiveSlot("Fencer", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:iron_sword"), Count: 1})
	servertest.AwaitFullSwing(t, srv, "Fencer")
	cow := feedbackCow(srv, attacker, watcher)

	attacker.MoveToGround(2.5, 65, 5.5, false)
	attacker.MoveToGround(2.5, 64.4, 5.5, false)
	attacker.Attack(cow)
	for _, c := range []*servertest.Client{attacker, watcher} {
		h := c.HearBlow(cow)
		if !slices.Equal(h.Animations, []uint8{protocol.AnimationCriticalHit}) {
			t.Errorf("animations of the cow = %v, want the critical hit alone", h.Animations)
		}
		if !slices.Equal(h.Sounds, []string{"crit"}) {
			t.Errorf("attack sounds = %v, want crit alone", h.Sounds)
		}
	}
}

func TestAStandingBlowIsNoCritical(t *testing.T) {
	t.Parallel()
	srv, attacker, watcher := feedbackFixture(t)
	servertest.AwaitFullSwing(t, srv, "Fencer")
	cow := feedbackCow(srv, attacker, watcher)

	attacker.Attack(cow)
	h := attacker.HearBlow(cow)
	if len(h.Animations) != 0 {
		t.Errorf("a standing fist's blow animated the cow with %v", h.Animations)
	}
	if !slices.Equal(h.Sounds, []string{"strong"}) {
		t.Errorf("attack sounds = %v, want strong alone", h.Sounds)
	}
}

func TestAStandingSwordSweeps(t *testing.T) {
	t.Parallel()
	srv, attacker, watcher := feedbackFixture(t)
	srv.GiveSlot("Fencer", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:iron_sword"), Count: 1})
	servertest.AwaitFullSwing(t, srv, "Fencer")
	cow := feedbackCow(srv, attacker, watcher)

	attacker.Attack(cow)
	attacker.Settle() // the blow is taken before the watcher looks
	h := watcher.HearBlow(cow)
	if len(h.Animations) != 0 {
		t.Errorf("a sweep animated the cow with %v", h.Animations)
	}
	if !slices.Equal(h.Sounds, []string{"sweep"}) {
		t.Errorf("attack sounds = %v, want sweep alone", h.Sounds)
	}
	if !slices.Contains(h.Particles, protocol.ParticleSweepAttack) {
		t.Errorf("particles = %v, want the sweep's", h.Particles)
	}
}

// A step of 0.259 blocks is over the sweep threshold at the default speed
// (0.25) and under it with Speed I (0.3), so the attribute decides the sweep.
// vanilla: Player.isSweepAttack, getSpeed
func TestASweepScalesWithMovementSpeed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		speed bool
		sweep bool
	}{{"default speed", false, false}, {"speed I", true, true}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, attacker, watcher := feedbackFixture(t)
			srv.GiveSlot("Fencer", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:iron_sword"), Count: 1})
			servertest.AwaitFullSwing(t, srv, "Fencer")
			cow := feedbackCow(srv, attacker, watcher)
			// With the loop stopped the known movement cannot expire.
			srv.StopTick()
			if tc.speed {
				srv.GiveEffect("Fencer", "minecraft:speed", 1200, 0, false)
				srv.TickSurvival()
			}
			attacker.MoveToGround(2.759, 64, 5.5, true)
			attacker.Settle()

			attacker.Attack(cow)
			h := attacker.HearBlow(cow)
			if got := slices.Contains(h.Sounds, "sweep"); got != tc.sweep {
				t.Errorf("sounds = %v, sweep = %v, want %v", h.Sounds, got, tc.sweep)
			}
		})
	}
}

func TestASharpSwordShowsTheMagicCritical(t *testing.T) {
	t.Parallel()
	srv, attacker, watcher := feedbackFixture(t)
	srv.GiveSlot("Fencer", player.HotbarSlot, servertest.EnchantedStack(t, "minecraft:iron_sword", "minecraft:sharpness", 1))
	servertest.AwaitFullSwing(t, srv, "Fencer")
	cow := feedbackCow(srv, attacker, watcher)

	attacker.Attack(cow)
	for _, c := range []*servertest.Client{attacker, watcher} {
		if h := c.HearBlow(cow); !slices.Equal(h.Animations, []uint8{protocol.AnimationMagicCriticalHit}) {
			t.Errorf("animations of the cow = %v, want the magic critical alone", h.Animations)
		}
	}
}

func TestASpammedBlowIsWeak(t *testing.T) {
	t.Parallel()
	srv, attacker, watcher := feedbackFixture(t)
	cow := feedbackCow(srv, attacker, watcher)
	// With the loop stopped no tick of recovery slips in.
	srv.StopTick()
	srv.GiveSlot("Fencer", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:iron_sword"), Count: 1})
	srv.TickSurvival()

	attacker.Attack(cow)
	if h := attacker.HearBlow(cow); !slices.Equal(h.Sounds, []string{"weak"}) {
		t.Errorf("attack sounds = %v, want weak alone", h.Sounds)
	}
	// The same blow again inside the cow's hurt window takes nothing.
	attacker.Attack(cow)
	if h := attacker.HearBlow(cow); !slices.Equal(h.Sounds, []string{"nodamage"}) {
		t.Errorf("attack sounds of a blow the cow shrugged off = %v, want nodamage alone", h.Sounds)
	}
}

// fallingBlowIsPlain checks that the fixture's attacker, mid-fall, lands a
// blow with no critical animation or sound.
func fallingBlowIsPlain(t *testing.T, attacker, watcher *servertest.Client, cow int32) {
	t.Helper()
	attacker.Attack(cow)
	for _, c := range []*servertest.Client{attacker, watcher} {
		h := c.HearBlow(cow)
		if len(h.Animations) != 0 {
			t.Errorf("animations of the cow = %v, want none", h.Animations)
		}
		if slices.Contains(h.Sounds, "crit") {
			t.Errorf("attack sounds = %v, want no crit", h.Sounds)
		}
	}
}

func TestABlindFallingBlowIsNoCritical(t *testing.T) {
	t.Parallel()
	srv, attacker, watcher := feedbackFixture(t)
	srv.GiveSlot("Fencer", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:iron_sword"), Count: 1})
	srv.GiveEffect("Fencer", "minecraft:blindness", 600, 0, false)
	servertest.AwaitFullSwing(t, srv, "Fencer")
	cow := feedbackCow(srv, attacker, watcher)

	attacker.MoveToGround(2.5, 65, 5.5, false)
	attacker.MoveToGround(2.5, 64.4, 5.5, false)
	fallingBlowIsPlain(t, attacker, watcher, cow)
}

func TestASeatedFallingBlowIsNoCritical(t *testing.T) {
	t.Parallel()
	srv, attacker, watcher := feedbackFixture(t)
	srv.GiveSlot("Fencer", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:iron_sword"), Count: 1})
	servertest.AwaitFullSwing(t, srv, "Fencer")
	cow := feedbackCow(srv, attacker, watcher)

	attacker.MoveToGround(2.5, 65, 5.5, false)
	attacker.MoveToGround(2.5, 64.4, 5.5, false)
	boat := srv.SpawnBoat(2.5, 64.4, 5.5, "minecraft:oak_boat")
	attacker.UseOn(boat)
	attacker.Settle()
	if got := srv.BoatPassengerIDs(boat); len(got) != 1 {
		t.Fatalf("the boat holds %v, want the fencer aboard", got)
	}
	fallingBlowIsPlain(t, attacker, watcher, cow)
}
