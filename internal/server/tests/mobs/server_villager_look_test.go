package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

func TestAnIdleVillagerLooksAtACatThenTracksItsNewPosition(t *testing.T) {
	t.Parallel()
	const name = "CatWatcher"
	srv, c, _ := servertest.JoinAs(t, servertest.MobTestConfig(), name, servertest.OpsListing(t, name))
	c.MoveToGround(0.5, 64, 14.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)
	srv.SetDayTime(1000)
	c.ChatCommand(`summon minecraft:cat 4.5 64 0.5 {NoAI:1b}`)
	c.WatchVariant(srv, "minecraft:cat")
	c.ChatCommand(`summon minecraft:villager 0.5 64 0.5 {attributes:[{id:"minecraft:movement_speed",base:0.0d}]}`)
	villager := c.WatchVariant(srv, "minecraft:villager")
	waitForLook := func(want float64) {
		t.Helper()
		for range 40 {
			advanceVillager(t, srv, c, 5, &villager)
			m, ok := servertest.MobByID(srv, villager.EntityID)
			if !ok {
				t.Fatal("villager disappeared")
			}
			if math.Abs(float64(m.HeadYaw)-want) < 3 {
				return
			}
		}
		t.Fatalf("villager never looked toward yaw %.0f", want)
	}
	waitForLook(-90)
	c.ChatCommand(`tp @e[type=minecraft:cat,limit=1] 0.5 64 4.5`)
	c.ExpectSystemChatPastMobs()
	waitForLook(0)
}

func TestAnIdleVillagerLooksAtAndWalksTowardAPlayer(t *testing.T) {
	t.Parallel()
	const name = "LookWalker"
	srv, c, _ := servertest.JoinAs(t, servertest.MobTestConfig(), name, servertest.OpsListing(t, name))
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)
	srv.SetDayTime(1000)
	c.ChatCommand(`summon minecraft:villager 6.5 64 0.5`)
	villager := c.WatchVariant(srv, "minecraft:villager")
	for range 40 {
		advanceVillager(t, srv, c, 10, &villager)
		m, ok := servertest.MobByID(srv, villager.EntityID)
		if !ok {
			t.Fatal("villager disappeared")
		}
		dx, dz := m.X-0.5, m.Z-0.5
		want := math.Atan2(dx, -dz) * 180 / math.Pi
		if dx*dx+dz*dz <= 9 && math.Abs(math.Remainder(float64(m.HeadYaw)-want, 360)) < 10 {
			return
		}
	}
	t.Fatal("villager did not look at and approach the player within 400 ticks")
}
