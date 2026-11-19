package entity

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/nbt"
)

func TestNativeMemoryRecordChildrenSurviveLoad(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		kind MobType
		mems string
	}{
		{TypeAxolotl, `"minecraft:has_hunting_cooldown":{value:1b,ttl:1000L},"minecraft:play_dead_ticks":{value:1000}`},
		{TypeSniffer, `"minecraft:sniff_cooldown":{value:{},ttl:1000L},"minecraft:sniffer_explored_positions":{value:[{dimension:"minecraft:overworld",pos:[I;1,64,2]}]}`},
		{TypeWarden, `"minecraft:is_emerging":{value:{},ttl:1000L},"minecraft:is_sniffing":{value:{}}`},
		{TypeVillager, `"minecraft:home":{value:{dimension:"minecraft:overworld",pos:[I;1,64,2]}},"minecraft:last_slept":{value:123L}`},
		{TypePiglin, `"minecraft:hunted_recently":{value:1b,ttl:1000L}`},
		{TypeNautilus, `"minecraft:angry_at":{value:[I;1,2,3,4],ttl:1000L}`},
		{TypeAllay, `"minecraft:liked_player":{value:[I;1,2,3,4]},"minecraft:liked_noteblock_position":{value:{dimension:"minecraft:overworld",pos:[I;1,64,2]}}`},
	} {
		t.Run(tc.kind.Name(), func(t *testing.T) {
			input, err := nbt.ParseSNBT("{Brain:{memories:{" + tc.mems + "}}}")
			if err != nil {
				t.Fatal(err)
			}
			m := NewMob(tc.kind, 1, 0, 64, 0, 1)
			m.LoadFrom(input, nil, nil)
			saved := nbt.NewOrderedCompound(24)
			m.SaveWith(saved, func() int64 { return 0 }, nil, nil, nil, nil)
			if _, ok := saved.Entries()["Brain"].(*nbt.OrderedCompound); !ok {
				t.Fatal("native Brain is not ordered")
			}
			brain, _ := nbt.CompoundView(saved.Entries()["Brain"])
			if _, ok := brain["memories"].(*nbt.OrderedCompound); !ok {
				t.Fatalf("native memories container = %T, want ordered compound", brain["memories"])
			}
			memories := brainMemories(saved.Entries())
			if len(memories) == 0 {
				t.Fatal("native save lost all supplied memories")
			}
			for key, tag := range memories {
				if _, ok := tag.(*nbt.OrderedCompound); !ok {
					t.Errorf("native memory %s = %T, want ordered compound", key, tag)
				}
			}
			back := NewMob(tc.kind, 2, 0, 64, 0, 2)
			back.LoadFrom(saved.Entries(), nil, nil)
			again := nbt.NewOrderedCompound(24)
			back.SaveWith(again, func() int64 { return 0 }, nil, nil, nil, nil)
			if !nbt.Equal(saved.Entries()["Brain"], again.Entries()["Brain"]) {
				t.Errorf("native memory values changed on reload: %v -> %v", brainMemories(saved.Entries()), brainMemories(again.Entries()))
			}
		})
	}
}

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

func TestRetainedBrainMemoryMapHistory(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		kind                        MobType
		retained, modeled, memories string
	}{
		{TypeVillager, "minecraft:golem_detected_recently", "minecraft:home", `"minecraft:golem_detected_recently":{value:1b,ttl:1000L},"minecraft:home":{value:{dimension:"minecraft:overworld",pos:[I;1,64,2]}}`},
		// home exercises Go's existing retention, not Warden AI's registered slots.
		{TypeWarden, "minecraft:home", "minecraft:sniff_cooldown", `"minecraft:home":{value:{dimension:"minecraft:overworld",pos:[I;1,64,2]},ttl:1000L},"minecraft:sniff_cooldown":{value:{},ttl:1000L}`},
	} {
		t.Run(tc.kind.Name(), func(t *testing.T) {
			for _, plain := range []bool{false, true} {
				input, err := nbt.ParseOrderedSNBT("{Brain:{memories:{" + tc.memories + "}}}")
				if err != nil {
					t.Fatal(err)
				}
				if plain {
					input = nbt.PlainCopy(input).(nbt.CompoundTag)
				}
				original := nbt.Snapshot(input)
				m := NewMob(tc.kind, 1, 0, 64, 0, 1)
				m.SetMemoryMapOrder(NewMemoryMapOrder(map[string]uint32{tc.retained: 1, tc.modeled: 2}, 0))
				m.LoadFrom(input.Entries(), nil, nil)
				retained := m.vb.brainRest
				if tc.kind == TypeWarden {
					retained = m.warden.memories
				}
				if !slices.Equal(retained.keys, []string{tc.retained}) {
					t.Fatalf("modeled entries not filtered from retained builder: %v", retained.keys)
				}
				// Mutating the decoder's input must not change retained child history or values.
				inMemory := brainMemories(input.Entries())[tc.retained].(nbt.CompoundTag)
				inMemory.Set("ttl", nbt.Long(9))
				if nbt.Equal(retained.entries[tc.retained], inMemory) {
					t.Fatal("retained child aliases the loaded compound")
				}
				save := func(m *Mob) *nbt.OrderedCompound {
					tag := nbt.NewOrderedCompound(24)
					m.SaveWith(tag, func() int64 { return 0 }, nil, nil, nil, nil)
					return tag
				}
				saved := save(m)
				originalBrain := original.(nbt.CompoundTag).Entries()["Brain"]
				if !nbt.Equal(saved.Entries()["Brain"], originalBrain) {
					t.Fatalf("native save lost values: %s", nbt.Stringify(saved.Entries()["Brain"]))
				}
				// Saving must use the mob's current context, including when loaded earlier.
				order := NewMemoryMapOrder(map[string]uint32{tc.retained: 1, tc.modeled: 2}, 0xffffffff)
				m.SetMemoryMapOrder(order)
				clone := m.cloneMemoryMap(retained)
				if clone.order != order {
					t.Fatal("clone kept the old context")
				}
				child := clone.entries[tc.retained].(nbt.CompoundTag)
				child.Set("ttl", nbt.Long(2000))
				clone.Set(tc.retained, child)
				if !slices.Equal(clone.keys, retained.keys) {
					t.Fatal("overwrite reordered retained entry")
				}
				if nbt.Equal(clone.entries[tc.retained], retained.entries[tc.retained]) {
					t.Fatal("clone shares child storage")
				}
				filtered := m.cloneMemoryMap(clone)
				filtered.Delete(tc.retained)
				if len(filtered.keys) != 0 || len(clone.keys) != 1 {
					t.Fatal("filtered clone changed its source")
				}
				if tc.kind == TypeVillager {
					m.vb.brainRest = clone
				} else {
					m.warden.memories = clone
				}
				saved = save(m)
				back := NewMob(tc.kind, 2, 0, 64, 0, 2)
				back.SetMemoryMapOrder(order)
				back.LoadFrom(saved.Entries(), nil, nil)
				again := save(back)
				if !nbt.Equal(saved.Entries()["Brain"], again.Entries()["Brain"]) {
					t.Fatal("retained values changed on reload")
				}
				mems := brainMemories(again.Entries())
				for _, key := range []string{tc.retained, tc.modeled} {
					memory := mems[key].(nbt.CompoundTag)
					before := brainMemories(saved.Entries())[key].(nbt.CompoundTag)
					if !slices.Equal(nbt.HashOrderedKeys(memory), nbt.HashOrderedKeys(before)) {
						t.Fatalf("%s child history changed", key)
					}
					if value, ok := memory.Entries()["value"].(nbt.CompoundTag); ok {
						beforeValue := before.Entries()["value"].(nbt.CompoundTag)
						if !slices.Equal(nbt.HashOrderedKeys(value), nbt.HashOrderedKeys(beforeValue)) {
							t.Fatal("GlobalPos child history changed")
						}
					}
				}
				if mems[tc.retained].(nbt.CompoundTag).Entries()["ttl"] != nbt.Long(2000) {
					t.Fatal("retained overwrite was lost")
				}
			}
		})
	}
}
