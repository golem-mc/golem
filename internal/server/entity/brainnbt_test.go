package entity

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
)

func TestNativeMobRecordChildrenSurviveLoad(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		kind   MobType
		tag    string
		fields []string
	}{
		{TypeWarden, `{anger:{suspects:[{uuid:[I;1,2,3,4],anger:50}]},listener:{selector:{event:{game_event:"minecraft:step",distance:2f,pos:[1d,64d,2d],source:[I;1,2,3,4],projectile_owner:[I;5,6,7,8]},tick:123L},event_delay:1000}}`, []string{"anger", "listener"}},
		{TypeVillager, `{VillagerData:{type:"minecraft:taiga",profession:"minecraft:farmer",level:3},Gossips:[{Target:[I;1,2,3,4],Type:"major_positive",Value:10}]}`, []string{"VillagerData", "Gossips"}},
		{TypeMooshroom, `{Type:"brown",stew_effects:[{id:"minecraft:speed",duration:200}]}`, []string{"stew_effects"}},
	} {
		t.Run(tc.kind.Name(), func(t *testing.T) {
			input, err := nbt.ParseSNBT(tc.tag)
			if err != nil {
				t.Fatal(err)
			}
			m := NewMob(tc.kind, 1, 0, 64, 0, 1)
			m.LoadFrom(input, nil, nil)
			saved := nbt.NewOrderedCompound(24)
			m.SaveWith(saved, func() int64 { return 0 }, nil, nil, nil, nil)
			back := NewMob(tc.kind, 2, 0, 64, 0, 2)
			back.LoadFrom(saved.Entries(), nil, nil)
			again := nbt.NewOrderedCompound(24)
			back.SaveWith(again, func() int64 { return 0 }, nil, nil, nil, nil)
			for _, key := range tc.fields {
				field := saved.Entries()[key]
				if list, ok := field.(nbt.List); ok {
					if len(list.Items) == 0 {
						t.Fatalf("native %s lost its entries", key)
					}
					field = list.Items[0]
				}
				if _, ok := field.(*nbt.OrderedCompound); !ok {
					t.Errorf("native %s = %T, want ordered compound", key, field)
				}
				if !nbt.Equal(saved.Entries()[key], again.Entries()[key]) {
					t.Errorf("native %s changed on reload", key)
				}
			}
		})
	}
}
