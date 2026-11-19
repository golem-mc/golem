package entity

import (
	"reflect"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func TestMooshroomStewEffectsCodec(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		list nbt.List
		want []protocol.StewEffect
	}{
		{"empty", nbt.List{Elem: nbt.TypeCompound}, []protocol.StewEffect{}},
		{"defaults and multiple", nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
			nbt.Compound{"id": nbt.String("fire_resistance")},
			nbt.Compound{"id": nbt.String("minecraft:poison"), "duration": nbt.Double(12.9)},
		}}, []protocol.StewEffect{{Effect: registry.MustMobEffect("minecraft:fire_resistance"), Duration: 160}, {Effect: registry.MustMobEffect("minecraft:poison"), Duration: 12}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewMob(TypeMooshroom, 1, 0, 64, 0, 1)
			now := func() int64 { return 0 }
			m.LoadFrom(nbt.Compound{"stew_effects": tc.list}, nil, now)
			if !reflect.DeepEqual(m.StewEffects, tc.want) {
				t.Fatalf("effects %+v, want %+v", m.StewEffects, tc.want)
			}
			tag := nbt.Compound{}
			m.SaveTo(tag, now)
			m.LoadFrom(tag, nil, now)
			if !reflect.DeepEqual(m.StewEffects, tc.want) {
				t.Fatalf("reloaded effects %+v, want %+v", m.StewEffects, tc.want)
			}
			m.StewEffects = nil
			m.SaveTo(tag, now)
			if _, ok := tag["stew_effects"]; ok {
				t.Error("cleared effects remained in saved NBT")
			}
		})
	}
}
