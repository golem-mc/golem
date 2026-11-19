package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

func TestAStrollingCowDoesNotRandomlyLookAway(t *testing.T) {
	t.Parallel()
	const name = "StrollWatcher"
	srv, c, _ := servertest.JoinAs(t, servertest.MobTestConfig(), name, servertest.OpsListing(t, name))
	c.ChatCommand("fill -12 63 -12 12 63 12 minecraft:stone")
	c.ChatCommand("fill -12 64 -12 12 67 12 minecraft:air")
	c.ChatCommand("setblock 0 87 0 minecraft:stone")
	c.MoveToGround(0.5, 88, 0.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)
	id := srv.SpawnMobSeeded("minecraft:cow", 0.5, 64, 0.5, 1)
	prev, _ := servertest.MobByID(srv, id)
	straight, checked := 0, 0
	for range 1200 {
		servertest.RunTicks(t, srv, 1)
		m, ok := servertest.MobByID(srv, id)
		if !ok {
			t.Fatal("cow disappeared")
		}
		if math.Hypot(m.X-prev.X, m.Z-prev.Z) > 0.02 &&
			math.Abs(math.Remainder(float64(m.Yaw-prev.Yaw), 360)) < 0.1 {
			straight++
		} else {
			straight = 0
		}
		// Allow the previous glance and head easing to finish after starting
		// a walk or changing direction; only inspect a sustained straight walk.
		if straight > 20 {
			checked++
			if d := math.Abs(math.Remainder(float64(m.HeadYaw-m.Yaw), 360)); d > 15 {
				t.Fatalf("cow looked %.1f degrees away during a straight stroll", d)
			}
		}
		prev = m
	}
	if checked == 0 {
		t.Fatal("cow never took a sustained straight stroll")
	}
}

func TestACowTurnsItsHeadTenDegreesATickAndTiltsItNoMoreThanFortyDegrees(t *testing.T) {
	t.Parallel()
	const name = "CowWatcher"
	srv, c, _ := servertest.JoinAs(t, servertest.MobTestConfig(), name, servertest.OpsListing(t, name))
	c.ChatCommand(`setblock 2 68 0 minecraft:stone`)
	c.MoveToGround(2.5, 69, 0.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)
	c.ChatCommand(`summon minecraft:cow 0.5 64 0.5 {attributes:[{id:"minecraft:movement_speed",base:0.0d}]}`)
	cow := c.WatchVariant(srv, "minecraft:cow")
	m, ok := servertest.MobByID(srv, cow.EntityID)
	if !ok {
		t.Fatal("cow missing")
	}
	prev := float64(m.HeadYaw)
	for range 600 {
		servertest.RunTicks(t, srv, 1)
		m, ok = servertest.MobByID(srv, cow.EntityID)
		if !ok {
			t.Fatal("cow disappeared")
		}
		yaw, pitch := float64(m.HeadYaw), float64(m.Pitch)
		if d := math.Mod(yaw-prev+540, 360) - 180; math.Abs(d) > 10.001 {
			t.Fatalf("head turned %.3f degrees in one tick", d)
		}
		if math.Abs(pitch) > 40.001 {
			t.Fatalf("pitch %.3f exceeds 40", pitch)
		}
		prev = yaw
		if math.Abs(yaw+90) < 1 && math.Abs(pitch+40) < 0.5 {
			return
		}
	}
	t.Fatalf("head never settled on yaw -90, pitch -40 (yaw %.1f pitch %.1f)", m.HeadYaw, m.Pitch)
}

func TestASwimmingDolphinCanTurnItsHeadWhileFollowingAPath(t *testing.T) {
	t.Parallel()
	srv, c := squidTests(t)
	servertest.HoldTicks(t, srv)
	id := summonSquid(t, c, "minecraft:dolphin", fishType(t, "minecraft:dolphin"), pondCentreX, -8.5)
	previous, ok := fishAt(srv, id)
	if !ok {
		t.Fatal("dolphin missing")
	}
	for range 400 {
		servertest.RunTicks(t, srv, 1)
		m, ok := fishAt(srv, id)
		if !ok {
			t.Fatal("dolphin disappeared")
		}
		moved := math.Hypot(m.X-previous.X, m.Z-previous.Z)
		// A yaw change distinguishes active steering from momentum after a
		// path ends, when the move control no longer sets the head yaw.
		steered := math.Abs(math.Mod(float64(m.Yaw-previous.Yaw)+540, 360) - 180)
		turned := math.Abs(math.Mod(float64(m.HeadYaw-m.Yaw)+540, 360) - 180)
		if moved > 0.01 && steered > 0.01 && turned > 0.5 {
			return
		}
		previous = m
	}
	t.Fatal("swimming dolphin's move control always overwrote its head turn")
}
