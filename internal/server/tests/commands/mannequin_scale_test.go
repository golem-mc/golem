package commands

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: LivingEntity.getDimensions, LivingEntity.onAttributeUpdated
func TestMannequinBoxFollowsScale(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.ViewDistance = config.MinDistance
	cfg.SimulationDistance = config.MinDistance
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	servertest.HoldTicks(t, srv)
	servertest.FreezeUntilHeld(t, srv)

	probe := func(x, h float64) bool {
		t.Helper()
		c.ChatCommand(fmt.Sprintf("execute positioned %v %v 5.5 if entity @e[tag=b,dx=0,dy=0,dz=0]", 5.5+x, 64+h))
		return bytes.Equal(c.ExpectSystemChatPastMobs(), conditionalPassCountForTest(1))
	}
	c.ChatCommand(`summon mannequin 5.5 64 5.5 {Tags:["b"],NoGravity:1b,attributes:[{id:"minecraft:scale",base:2.0}]}`)
	c.ExpectSystemChatPastMobs()
	for _, tc := range []struct {
		x, h float64
		want bool
	}{{0, 3.5, true}, {0.55, 1, true}, {0, 3.65, false}, {0.65, 1, false}} {
		if got := probe(tc.x, tc.h); got != tc.want {
			t.Errorf("scale 2 probe x=%v h=%v found = %v, want %v", tc.x, tc.h, got, tc.want)
		}
	}
	c.ChatCommand("attribute @e[tag=b,limit=1] minecraft:scale base set 0.5")
	c.ExpectSystemChatPastMobs()
	for _, tc := range []struct {
		h    float64
		want bool
	}{{0.85, true}, {0.95, false}} {
		if got := probe(0, tc.h); got != tc.want {
			t.Errorf("scale 0.5 probe h=%v found = %v, want %v", tc.h, got, tc.want)
		}
	}
}
