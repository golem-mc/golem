package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

func TestPiglinHeadLowersDetectionRange(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{barterPiglin, bruteKind} {
		for _, slot := range []string{"", "head", "feet"} {
			t.Run(kind+"/"+slot, func(t *testing.T) {
				t.Parallel()
				c, playerID, srv := piglinWorld(t)
				if slot != "" {
					c.ChatCommand("item replace entity Slayer armor." + slot + " with minecraft:piglin_head")
					c.Settle()
				}
				mob := srv.SpawnMob(kind, 11.5, 64, 0.5)
				// Keep idle strolls outside the reduced range without disabling AI.
				c.ChatCommand("attribute @e[type=" + kind + ",limit=1] minecraft:movement_speed base set 0")
				c.Settle()
				targeted := false
				for range 60 {
					servertest.RunTicks(t, srv, 1)
					target := srv.MobTargetID(mob)
					if slot == "head" && target != 0 {
						t.Fatalf("wearing a piglin head: target = %d, want 0 at nine blocks", target)
					}
					targeted = targeted || target == playerID
				}
				if slot != "head" && !targeted {
					t.Fatal("piglin head absent or in the wrong slot: player was never targeted within 60 ticks")
				}
			})
		}
	}
}
