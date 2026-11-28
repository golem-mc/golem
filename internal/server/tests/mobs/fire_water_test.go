package mobs

import (
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
	x := int32(20)
	for _, tc := range cases {
		srv.RunLine(tc.line)
		srv.RunLine("summon " + tc.kind + " " + itoa(x) + ".5 64 " + itoa(x) + ".5 {NoAI:1b,Fire:" + tc.fire + "}")
		x += 10
	}
	servertest.RunTicks(t, srv, 1)
	for i, tc := range cases {
		ids := srv.MobIDsOfKind(tc.kind)
		_ = i
		for _, id := range ids {
			x, _, _, _ := srv.MobPos(id)
			if int(x)/10 != 2+i {
				continue
			}
			hp, _ := srv.MobHealth(id)
			fire := srv.MobFire(id)
			if tc.fire == "301s" && fire != 0 {
				t.Errorf("%s: Fire %d, want 0", tc.name, fire)
			}
			if hp != tc.wantHealth {
				t.Errorf("%s: health %v, want %v", tc.name, hp, tc.wantHealth)
			}
		}
	}
}

func itoa(n int32) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}
