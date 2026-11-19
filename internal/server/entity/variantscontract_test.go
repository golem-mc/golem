package entity

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// vanilla: each variant's getAmbientSound/getHurtSound/getDeathSound/getStepSound
func TestHostileVariantsUseTheirSoundSets(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		typ  MobType
		base string
		step bool
	}{
		{TypeHusk, "husk", true},
		{TypeDrowned, "drowned", true},
		{TypeStray, "stray", true},
		{TypeBogged, "bogged", true},
		{TypeCaveSpider, "spider", true},
		{TypeZombieVillager, "zombie_villager", true},
		{TypeWitch, "witch", false},
	} {
		t.Run(tc.typ.Name(), func(t *testing.T) {
			got := NewMob(tc.typ, 1, 0, 64, 0, 1).Sounds()
			base := "minecraft:entity." + tc.base
			sound := func(suffix string) registry.Sound {
				id, ok := registry.SoundEventID(base + suffix)
				if !ok {
					t.Fatalf("missing sound %s%s", base, suffix)
				}
				return registry.Sound(id + 1)
			}
			want := MobSounds{Ambient: sound(".ambient"), Hurt: sound(".hurt"), Death: sound(".death")}
			if tc.step {
				id, _ := registry.SoundEventID(base + ".step")
				want.Step = registry.Sound(id + 1)
			}
			if got != want {
				t.Errorf("sounds = %+v, want %+v", got, want)
			}
		})
	}
	// vanilla: Slime and MagmaCube.getHurtSound/getDeathSound
	for _, typ := range []MobType{TypeSlime, TypeMagmaCube} {
		m := NewMob(typ, 1, 0, 64, 0, 1)
		for _, size := range []int{1, 2, 4} {
			m.SetSize(size, true)
			base := "minecraft:entity." + strings.TrimPrefix(typ.Name(), "minecraft:")
			suffix := ""
			if size == 1 {
				suffix = "_small"
			}
			got := m.Sounds()
			if got.Hurt.Name() != base+".hurt"+suffix || got.Death.Name() != base+".death"+suffix || got.Ambient.Name() != "" {
				t.Errorf("%s size %d: sounds = %+v", typ, size, got)
			}
		}
	}
}

// vanilla: VanillaEntityLoot.generate; equipment is tested separately.
func TestHostileVariantsRollTheirOwnLoot(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		typ   MobType
		size  int
		items string
	}{
		{TypeHusk, 0, "rotten_flesh iron_ingot carrot potato"},
		{TypeDrowned, 0, "rotten_flesh copper_ingot"},
		{TypeStray, 0, "arrow bone tipped_arrow"},
		{TypeBogged, 0, "arrow bone tipped_arrow"},
		{TypeCaveSpider, 0, "string spider_eye"},
		{TypeZombieVillager, 0, "rotten_flesh iron_ingot carrot potato"},
		{TypeSlime, 1, "slime_ball"},
		{TypeMagmaCube, 2, "magma_cream"},
		{TypeWitch, 0, "glowstone_dust sugar spider_eye glass_bottle gunpowder stick redstone"},
	} {
		t.Run(tc.typ.Name(), func(t *testing.T) {
			m := NewMob(tc.typ, 1, 0, 64, 0, 1)
			if tc.size > 0 {
				m.SetSize(tc.size, true)
			}
			m.playerMemoryTicks = 100
			seen := map[string]bool{}
			for range 2000 {
				for _, drop := range m.RollDrops(rules.LootKiller{}, nil) {
					name, _ := registry.ItemName(drop.ItemID)
					seen[strings.TrimPrefix(name, "minecraft:")] = true
				}
			}
			want := strings.Fields(tc.items)
			for _, item := range want {
				if !seen[item] {
					t.Errorf("2000 deaths never dropped %s", item)
				}
			}
			if len(seen) != len(want) {
				t.Errorf("drops = %v, want only %s", seen, tc.items)
			}
		})
	}
	for _, tc := range []struct {
		typ  MobType
		size int
	}{{TypeSlime, 2}, {TypeSlime, 4}, {TypeMagmaCube, 1}} {
		m := NewMob(tc.typ, 1, 0, 64, 0, 1)
		m.SetSize(tc.size, true)
		for range 100 {
			if drops := m.RollDrops(rules.LootKiller{}, nil); len(drops) != 0 {
				t.Fatalf("%s size %d dropped %+v", tc.typ, tc.size, drops)
			}
		}
	}
}
