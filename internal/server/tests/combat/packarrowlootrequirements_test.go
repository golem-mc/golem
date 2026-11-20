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
		{"sneaking shooter", `{"condition":"minecraft:entity_properties","entity":"attacker","predicate":{"minecraft:flags":{"is_sneaking":true}}}`, true, false, true},
		{"untagged target", `{"condition":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:nbt":"{Tags:[\"a\"]}"}}`, false, false, false},
		{"arrow's own saved data", `{"condition":"minecraft:entity_properties","entity":"direct_attacker","predicate":{"minecraft:nbt":"{pickup:1b}"}}`, false, false, true},
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
			full, _ := srv.MobHealth(golem)
			// Until the arrow lands: the golem is hurt or dead.
			servertest.UntilTicks(t, srv, 60, "arrow hits the golem", func() bool {
				health, alive := srv.MobHealth(golem)
				return !alive || health < full
			})
			health, alive := srv.MobHealth(golem)
			if dead := !alive || health <= 0; dead != tc.kills {
				t.Fatalf("golem health %v alive %v, want killed %v", health, alive, tc.kills)
			}
		})
	}
}
