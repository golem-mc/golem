package entity

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func TestMobLoadsUnqualifiedModelAttributes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		kind   MobType
		list   string
		health float64
	}{
		{"horse rolls", TypeHorse, `[{id:"max_health",base:42},{id:"movement_speed",base:0.25f},{id:"jump_strength",base:1s}]`, 42},
		{"wolf taming", TypeWolf, `[{id:"max_health",base:40b}]`, 8},
		{"follow bonus", TypeCow, `[{id:"follow_range",base:16,modifiers:[{id:"random_spawn_bonus",amount:0.5f,operation:"add_multiplied_base"}]}]`, 10},
		{"zombie leader", TypeZombie, `[{id:"max_health",base:20,modifiers:[{id:"leader_zombie_bonus",amount:1b,operation:"add_multiplied_total"}]},{id:"spawn_reinforcements",base:0.1d,modifiers:[{id:"kept",amount:0.5f,operation:"add_value"},{id:"leader_zombie_bonus",amount:0.25d,operation:"add_value"}]}]`, 40},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tag, err := nbt.ParseSNBT("{Health:100f,attributes:" + tc.list + "}")
			if err != nil {
				t.Fatal(err)
			}
			m := NewMob(tc.kind, 1, 0, 64, 0, 1)
			m.LoadFrom(tag, nil, func() int64 { return 0 })
			if m.Attributes() != nil {
				t.Error("modelled attributes installed a map")
			}
			if m.Health != tc.health {
				t.Errorf("health = %v, want %v", m.Health, tc.health)
			}
			if tc.kind == TypeHorse && (m.Horse.Speed != 0.25 || m.Horse.Jump != 1) {
				t.Errorf("horse rolls = %v, %v", m.Horse.Speed, m.Horse.Jump)
			}
			saved := nbt.NewOrderedCompound(24)
			m.SaveWith(saved, func() int64 { return 0 }, tag["attributes"], nil, nil, nil)
			list := saved.Entries()["attributes"].(nbt.List)
			seen := map[string]bool{}
			keptModifier := false
			for _, item := range list.Items {
				entry, ordered := item.(*nbt.OrderedCompound)
				if !ordered {
					t.Fatalf("native attribute = %T, want ordered compound", item)
				}
				fields := entry.Entries()
				id := string(fields["id"].(nbt.String))
				if id != registry.Qualify(id) || seen[id] {
					t.Errorf("noncanonical or duplicate saved attribute %q", id)
				}
				seen[id] = true
				if _, ok := fields["base"].(nbt.Double); !ok {
					t.Errorf("%s saved base = %T, want double", id, fields["base"])
				}
				mods, _ := nbt.Get[nbt.List](fields, "modifiers")
				for _, mod := range mods.Items {
					if _, ok := mod.(*nbt.OrderedCompound); !ok {
						t.Errorf("native modifier = %T, want ordered compound", mod)
					}
					fields, _ := nbt.CompoundView(mod)
					if fields["id"] == nbt.String("minecraft:kept") {
						keptModifier = true
						if fields["amount"] != nbt.Double(0.5) || fields["operation"] != nbt.String("add_value") {
							t.Errorf("unowned modifier changed: %v", fields)
						}
					}
				}
			}
			if tc.kind == TypeZombie && !keptModifier {
				t.Error("unowned modifier was lost")
			}
			back := NewMob(tc.kind, 2, 0, 64, 0, 2)
			back.LoadFrom(saved.Entries(), nil, func() int64 { return 0 })
			if back.Health != m.Health || back.MaxHealth != m.MaxHealth || back.Traits() != m.Traits() || back.Horse.Speed != m.Horse.Speed || back.Horse.Jump != m.Horse.Jump {
				t.Errorf("native attribute reload changed health, traits or horse rolls")
			}
			attrs := m.BuildAttributes(nbt.List{})
			if tc.name == "follow bonus" && attrs.Value(registry.AttributeFollowRange) != 24 {
				t.Errorf("follow range = %v, want 24", attrs.Value(registry.AttributeFollowRange))
			}
		})
	}
}
