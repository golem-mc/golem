package combat

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestPackArrowLootRequirements tests a bow enchantment whose damage effect
// carries loot conditions on the shooter and on the hit body: an arrow that
// meets them kills an iron golem, one that does not leaves it alive.
// vanilla: Enchantment.damageContext, EnchantmentHelper.modifyDamage
func TestPackArrowLootRequirements(t *testing.T) {
	for _, tc := range []struct {
		name, requirement string
		sneak, tag, kills bool
	}{
		{"standing shooter", `{"condition":"minecraft:entity_properties","entity":"attacker","predicate":{"minecraft:flags":{"is_sneaking":true}}}`, false, false, false},
		{"sneaking shooter", `{"condition":"minecraft:entity_properties","entity":"attacker","predicate":{"minecraft:flags":{"is_sneaking":true}}}`, true, false, true},
		{"untagged target", `{"condition":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:nbt":"{Tags:[\"a\"]}"}}`, false, false, false},
		{"tagged target", `{"condition":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:nbt":"{Tags:[\"a\"]}"}}`, false, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, c := joinArcher(t, bowPack(t, fmt.Sprintf(
				`"minecraft:damage":[{"effect":{"type":"minecraft:add","value":1000},"requirements":%s}]`, tc.requirement), nil))
			c.SendSneak(tc.sneak)
			c.Settle()
			golem := srv.SpawnMob("minecraft:iron_golem", 4.5, 64, 0.5)
			srv.SetMobNoAI(golem)
			if tc.tag {
				srv.RunLine("tag @e[type=iron_golem] add a")
			}
			shootEast(t, srv, c)
			servertest.RunTicks(t, srv, servertest.SkeletonTicks)
			health, alive := srv.MobHealth(golem)
			if dead := !alive || health <= 0; dead != tc.kills {
				t.Fatalf("golem health %v alive %v, want killed %v", health, alive, tc.kills)
			}
		})
	}
}
