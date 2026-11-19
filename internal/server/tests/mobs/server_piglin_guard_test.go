package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// Piglins anger at a player who opens a guarded container in their sight or
// breaks a guarded block.
// vanilla: PiglinAi.angerNearbyPiglins

func openChestBeside(t *testing.T, c *servertest.Client, srv *server.Server) {
	t.Helper()
	c.ChatCommand("setblock 3 64 0 minecraft:chest")
	c.Settle()
	c.UseBlockWith(0, 3, 64, 0)
	c.Settle()
	servertest.RunTicks(t, srv, 1)
}

func TestAPiglinAngersAtAPlayerOpeningAChestInItsSight(t *testing.T) {
	t.Parallel()
	c, id, srv := barterWorld(t)
	p := srv.SpawnMob(barterPiglin, 6.5, 64, 0.5)
	servertest.RunTicks(t, srv, 2)
	openChestBeside(t, c, srv)
	if srv.MobTargetID(p) != id {
		t.Fatal("the piglin did not turn on the player who opened a chest in front of it")
	}
}

// gearedPlayers puts golden boots on each named player so that no piglin
// hunts them unprovoked.
func gearedPlayers(c *servertest.Client, names ...string) {
	for _, n := range names {
		c.ChatCommand("item replace entity " + n + " armor.feet with minecraft:golden_boots")
	}
	c.Settle()
}
