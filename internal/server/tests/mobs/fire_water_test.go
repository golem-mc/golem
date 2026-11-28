package mobs

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: Entity.baseTick, WaterFluid.entityInside, Entity.applyEffectsFromBlocks
func TestWaterTouchingTheBoxPutsOutFireAfterTheBurn(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.MobTestConfig(), "Burner",
		servertest.OpsListing(t, "Burner"))
	servertest.HoldTicks(t, srv)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()

	cases := []struct {
		name, line, kind string
		fire             string
		wantHealth       float64
	}{
		{"head cell", `setblock 20 65 20 minecraft:light[waterlogged=true]`, "minecraft:zombie", "301s", 20},
		{"side cell", `setblock 31 64 30 minecraft:light[waterlogged=true]`, "minecraft:iron_golem", "301s", 100},
		{"summoned in water", `setblock 40 64 40 minecraft:water`, "minecraft:zombie", "300s", 19},
	}
	for i, tc := range cases {
		x := 20 + 10*i
		srv.RunLine(tc.line)
		srv.RunLine(fmt.Sprintf("summon %s %d.5 64 %d.5 {NoAI:1b,Fire:%s}", tc.kind, x, x, tc.fire))
	}
	servertest.RunTicks(t, srv, 1)
	for i, tc := range cases {
		found := false
		for _, id := range srv.MobIDsOfKind(tc.kind) {
			if x, _, _, _ := srv.MobPos(id); int(x) != 20+10*i {
				continue
			}
			found = true
			hp, _ := srv.MobHealth(id)
			if fire := srv.MobFire(id); fire != 0 {
				t.Errorf("%s: Fire %d, want 0", tc.name, fire)
			}
			if hp != tc.wantHealth {
				t.Errorf("%s: health %v, want %v", tc.name, hp, tc.wantHealth)
			}
		}
		if !found {
			t.Fatalf("%s: summoned %s not found", tc.name, tc.kind)
		}
	}
}
