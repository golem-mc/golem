package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// jockeyRider summons a spider carrying a skeleton at the given spot and
// returns the newest skeleton's entity ID.
func jockeyRider(t *testing.T, srv *server.Server, c *servertest.Client, x, y, z string) int32 {
	t.Helper()
	c.ChatCommand(`summon minecraft:spider ` + x + ` ` + y + ` ` + z + ` {NoAI:1b,Passengers:[{id:"minecraft:skeleton",NoAI:1b}]}`)
	c.Settle()
	var id int32
	for _, m := range srv.Mobs() {
		if m.Kind == "minecraft:skeleton" && m.ID > id {
			id = m.ID
		}
	}
	if id == 0 {
		t.Fatal("no skeleton summoned")
	}
	return id
}

// TestAJockeyRiderFallsOffUnderWater: a skeleton on a spider is unseated once
// its eyes are under water, and stays seated on dry ground.
// vanilla: LivingEntity.baseTick, Entity.dismountsUnderwater
func TestAJockeyRiderFallsOffUnderWater(t *testing.T) {
	t.Parallel()
	const name = "Diver"
	srv, c, _ := servertest.JoinAs(t, servertest.MobTestConfig(), name, servertest.OpsListing(t, name))
	servertest.HoldTicks(t, srv)
	c.ChatCommand("fill 6 64 6 10 70 10 minecraft:water")
	c.Settle()
	wet := jockeyRider(t, srv, c, "8.5", "64", "8.5")
	if srv.MobVehicleID(wet) == 0 {
		t.Fatal("skeleton not seated on the spider")
	}
	servertest.RunTicks(t, srv, 2)
	if v := srv.MobVehicleID(wet); v != 0 {
		t.Fatalf("skeleton under water still rides %d", v)
	}

	c.ChatCommand("kill @e[type=!minecraft:player]")
	c.Settle()
	dry := jockeyRider(t, srv, c, "-8.5", "64", "-8.5")
	servertest.RunTicks(t, srv, 20)
	if srv.MobVehicleID(dry) == 0 {
		t.Fatal("skeleton on dry ground was unseated")
	}
}
