package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// TestATotemLeavesAMobAtOneHealthExceptAgainstTheVoid checks that a lethal
// blow with a totem in either hand leaves the mob at health 1 with the totem
// spent and the save waiting for the tick, and that damage bypassing
// invulnerability (the void, /kill) still kills.
func TestATotemLeavesAMobAtOneHealthExceptAgainstTheVoid(t *testing.T) {
	t.Parallel()
	id, ok := registry.ItemID("minecraft:totem_of_undying")
	if !ok {
		t.Fatal("no totem item")
	}
	totem := protocol.ItemStack{ItemID: id, Count: 1}
	cases := []struct {
		name       string
		damageType registry.DamageType
		main, off  bool
		survives   bool
	}{
		{"main hand", rules.DamageTypeMobAttack, true, false, true},
		{"off hand", rules.DamageTypeMobAttack, false, true, true},
		{"void", rules.DamageTypeOutOfWorld, true, false, false},
		{"generic kill", registry.DamageTypeGenericKill, false, true, false},
		{"empty hands", rules.DamageTypeMobAttack, false, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := aimless(NewMob(TypeZombie, 1, 0.5, 64, 0.5, 1))
			if tc.main {
				m.MainHand = totem
			}
			if tc.off {
				m.OffHand = totem
			}
			m.Health = 1
			m.hurtByDamage(100, tc.damageType, 0, false, false, 0, 0)
			if m.Dead == tc.survives {
				t.Fatalf("Dead = %v, want %v", m.Dead, !tc.survives)
			}
			if tc.survives {
				if m.Health != 1 {
					t.Errorf("health = %v, want 1", m.Health)
				}
				if !m.MainHand.Empty() || !m.OffHand.Empty() {
					t.Error("the totem was not spent")
				}
				if !m.TotemSaved() {
					t.Error("the save is not waiting for the tick")
				}
			}
		})
	}
}
