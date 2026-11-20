package plugins_test

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

func checkRegistry[T ~int32 | ~uint16](t *testing.T, r plugin.Registries, names []string, lookup func(plugin.Key) (T, bool), offset int) {
	t.Helper()
	for i, name := range names {
		key, err := plugin.ParseKey(name)
		if err != nil {
			t.Error(err)
			continue
		}
		id, ok := lookup(key)
		if !ok || int(id) != i+offset {
			t.Errorf("%s=%v,%v, want %d", name, id, ok, i+offset)
		}
		got, ok := r.KeyOf(id)
		if !ok || got != key {
			t.Errorf("KeyOf(%T(%v))=%v,%v, want %s", id, id, got, ok, key)
		}
	}
	if _, ok := lookup(plugin.MinecraftKey("not_registered")); ok {
		t.Error("unknown key resolved")
	}
}

func TestPluginRegistries(t *testing.T) {
	t.Parallel()
	_, h, run := worldPlugin(t, servertest.BlockTestConfig())
	run(func() {
		r := h.Registries()
		blocks := make([]string, registry.BlockCount)
		for i := range blocks {
			blocks[i] = registry.Block(i).Name()
		}
		checkRegistry(t, r, blocks, r.Block, 0)
		checkRegistry(t, r, registry.Items, r.Item, 0)
		checkRegistry(t, r, registry.EntityTypes, r.EntityType, 0)
		attrs := make([]string, len(registry.Attributes))
		for i, a := range registry.Attributes {
			attrs[i] = a.Name
		}
		checkRegistry(t, r, attrs, r.Attribute, 0)
		biomes := make([]string, registry.BiomeCount)
		for i := range biomes {
			biomes[i] = registry.Biome(i).Name()
		}
		checkRegistry(t, r, biomes, r.Biome, 0)
		checkRegistry(t, r, registry.MobEffects, r.Effect, 0)
		ench, _ := registry.Get("minecraft:enchantment")
		checkRegistry(t, r, ench.Entries, r.Enchantment, 0)
		damage, _ := registry.Get("minecraft:damage_type")
		checkRegistry(t, r, damage.Entries, r.DamageType, 1)
		checkRegistry(t, r, registry.SoundEvents, r.Sound, 1)
		checkRegistry(t, r, registry.ParticleTypes, r.Particle, 0)
		rules := make([]string, len(world.GameRuleTable))
		for i, r := range world.GameRuleTable {
			rules[i] = "minecraft:" + r.ID
		}
		checkRegistry(t, r, rules, r.GameRule, 0)
		infos := r.GameRules()
		if len(infos) != len(world.GameRuleTable) {
			t.Fatalf("GameRules=%d, want %d", len(infos), len(world.GameRuleTable))
		}
		for i, info := range infos {
			def := world.GameRuleTable[i]
			if int(info.ID) != i || info.Key != plugin.MinecraftKey(def.ID) || info.Boolean != (def.Kind == world.RuleBool) {
				t.Errorf("rule %d=%+v", i, info)
			}
			if got, ok := r.KeyOf(info.ID); !ok || got != info.Key {
				t.Errorf("KeyOf(%d)=%v,%v", i, got, ok)
			}
			if info.Boolean && (info.Min != 0 || info.Max != 1 || info.Default != map[bool]int{false: 0, true: 1}[def.DefBool]) {
				t.Errorf("bool rule %+v", info)
			}
			if !info.Boolean && (info.Default != int(def.DefInt) || info.Min != int(def.Min) || info.Max != int(def.Max)) {
				t.Errorf("int rule %+v", info)
			}
		}
		for _, name := range []string{"keep_inventory", "random_tick_speed"} {
			id, _ := r.GameRule(plugin.MinecraftKey(name))
			info := infos[id]
			w := h.Overworld()
			if w.GameRule(id) != info.Default {
				t.Errorf("%s=%d, default %d", name, w.GameRule(id), info.Default)
			}
			if _, err := h.Dispatch(h.Console(), "gamerule "+name+" "+map[bool]string{true: "true", false: "7"}[info.Boolean]); err != nil {
				t.Fatal(err)
			}
			want := 7
			if info.Boolean {
				want = 1
			}
			if w.GameRule(id) != want || r.GameRules()[id].Default != info.Default {
				t.Errorf("%s=%d default=%d after /gamerule", name, w.GameRule(id), r.GameRules()[id].Default)
			}
		}
		if _, ok := r.GameRule(plugin.Key{Namespace: "example", Path: "advance_time"}); ok {
			t.Error("foreign rule resolved")
		}
		for i, name := range registry.CustomStats {
			k, _ := plugin.ParseKey(name)
			s, ok := r.Statistic(k)
			if !ok || s.Type != registry.StatTypeCustom || s.Value != int32(i) {
				t.Errorf("statistic %s=%+v,%v", name, s, ok)
			}
			if got, ok := r.KeyOf(s); !ok || got != k {
				t.Errorf("statistic key=%v,%v, want %s", got, ok, k)
			}
		}
		if _, ok := r.Statistic(plugin.MinecraftKey("stone")); ok {
			t.Error("ambiguous typed statistic resolved")
		}
		for _, typ := range []registry.StatType{registry.StatTypeMined, registry.StatTypeUsed, registry.StatTypeKilled} {
			k, ok := r.KeyOf(plugin.Statistic{Type: typ, Value: 0})
			if !ok || k.Namespace != "minecraft" {
				t.Errorf("typed counter key=%v,%v", k, ok)
			}
		}
		invalid := []any{nil, 0, "minecraft:stone", registry.Block(registry.BlockCount), registry.Item(-1), registry.EntityType(-1), registry.AttributeID(-1), registry.Biome(-1), plugin.Effect(-1), plugin.Enchantment(-1), plugin.DamageType(0), plugin.Sound(0), plugin.ParticleType(-1), plugin.GameRule(-1), plugin.GameRule(256), plugin.Statistic{Type: -1}, plugin.Statistic{Type: registry.StatTypeMined, Value: -1}}
		for _, id := range invalid {
			if k, ok := r.KeyOf(id); ok {
				t.Errorf("invalid %T(%v) resolved to %v", id, id, k)
			}
		}
	})
}
