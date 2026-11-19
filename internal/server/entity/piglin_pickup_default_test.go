package entity

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
)

func TestMobLootPickupLoadDefaults(t *testing.T) {
	t.Parallel()
	now := func() int64 { return 0 }
	for _, kind := range []MobType{TypePiglin, TypePiglinBrute, TypeZombie} {
		t.Run(kind.Name(), func(t *testing.T) {
			m := NewMob(kind, 1, 0.5, 64, 0.5, 1)
			for _, tc := range []struct {
				name string
				tag  nbt.Compound
				want bool
			}{
				{"disabled", nbt.Compound{"CanPickUpLoot": nbt.Byte(0)}, false},
				{"omitted after disabled", nbt.Compound{}, kind != TypeZombie},
				{"enabled", nbt.Compound{"CanPickUpLoot": nbt.Byte(1)}, true},
				{"unreadable after enabled", nbt.Compound{"CanPickUpLoot": nbt.String("false")}, kind != TypeZombie},
			} {
				m.LoadFrom(tc.tag, nil, now)
				if m.CanPickUpLoot != tc.want {
					t.Errorf("%s: CanPickUpLoot = %v, want %v", tc.name, m.CanPickUpLoot, tc.want)
				}
				saved := nbt.Compound{}
				m.SaveTo(saved, now)
				if got, ok := nbt.ByteValue(saved["CanPickUpLoot"]); !ok || (got != 0) != tc.want {
					t.Errorf("%s: saved CanPickUpLoot = %v, want %v", tc.name, saved["CanPickUpLoot"], tc.want)
				}
			}
		})
	}
}
