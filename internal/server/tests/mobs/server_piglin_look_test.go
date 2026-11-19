package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

func TestAPiglinLooksAtAPlayerHoldingGold(t *testing.T) {
	t.Parallel()
	for _, hand := range []string{"mainhand", "offhand"} {
		t.Run(hand, func(t *testing.T) {
			t.Parallel()
			srv, connect := servertest.Start(t, servertest.HostileTestConfig(), servertest.OpsListing(t, "Slayer"))
			servertest.FreezeUntilHeld(t, srv)
			c := connect()
			c.JoinAs("Slayer")
			c.MoveToGround(0.5, 64, 0.5, true)
			c.ChatCommand("item replace entity Slayer armor.feet with minecraft:golden_boots")
			c.Settle()
			servertest.HoldTicks(t, srv)
			// Six blocks east and eight north puts the player exactly ten
			// blocks away, outside the ordinary idle glance's eight.
			id := srv.SpawnMobSeeded(barterPiglin, 6.5, 64, -7.5, 5891)
			c.ChatCommand("attribute @e[type=minecraft:piglin,limit=1] minecraft:movement_speed base set 0")
			c.Settle()
			servertest.RunTicks(t, srv, 20)
			if delta := piglinLookError(t, srv, id); delta <= 10 {
				t.Fatalf("empty hands forced the piglin to face the player: error %.1f degrees", delta)
			}
			c.ChatCommand("item replace entity Slayer weapon." + hand + " with minecraft:gold_ingot")
			c.Settle()
			servertest.RunTicks(t, srv, 20)
			if delta := piglinLookError(t, srv, id); delta > 10 {
				t.Fatalf("holding gold in %s: head yaw misses the player by %.1f degrees", hand, delta)
			}
		})
	}
}

func piglinLookError(t *testing.T, srv *server.Server, id int32) float64 {
	t.Helper()
	p := srv.PlayerPosition("Slayer")
	for _, m := range srv.Mobs() {
		if m.ID != id {
			continue
		}
		if m.NoAI || srv.MobTargetID(id) != 0 {
			t.Fatal("the piglin must have AI and remain peaceful")
		}
		want := math.Atan2(m.X-p.X, p.Z-m.Z) * 180 / math.Pi
		return math.Abs(math.Mod(float64(m.HeadYaw)-want+540, 360) - 180)
	}
	t.Fatal("piglin disappeared")
	return 0
}
