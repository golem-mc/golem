package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

func TestASummonedGoatKeepsItsHeadNearTheBodyItWasGiven(t *testing.T) {
	t.Parallel()
	const name = "GoatRot"
	srv, c, _ := servertest.JoinAs(t, servertest.MobTestConfig(), name, servertest.OpsListing(t, name))
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)
	c.ChatCommand(`summon minecraft:goat 4.5 64 0.5 {NoAI:1b,Rotation:[180f,0f]}`)
	goat := c.WatchVariant(srv, "minecraft:goat")
	m, ok := servertest.MobByID(srv, goat.EntityID)
	if !ok {
		t.Fatal("goat missing")
	}
	// vanilla: the head is set while the body is still 0; wrapDegrees(180) is -180, so Goat.setYHeadRot clamps it to -15.
	if m.Yaw != 180 || m.HeadYaw != -15 {
		t.Fatalf("yaw %v head %v, want 180 and -15", m.Yaw, m.HeadYaw)
	}
}
