package server

import (
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

type pluginRegistries struct{}

func (*pluginHost) Registries() plugin.Registries { return pluginRegistries{} }

func (pluginRegistries) Block(k plugin.Key) (registry.Block, bool) {
	return registry.BlockByName(k.String())
}
func (pluginRegistries) Item(k plugin.Key) (registry.Item, bool) {
	return registry.ItemByName(k.String())
}
func (pluginRegistries) EntityType(k plugin.Key) (registry.EntityType, bool) {
	return registry.EntityTypeByName(k.String())
}
func (pluginRegistries) Attribute(k plugin.Key) (registry.AttributeID, bool) {
	a, ok := registry.AttributeByName(k.String())
	return registry.AttributeID(a.ID), ok
}
func (pluginRegistries) Biome(k plugin.Key) (registry.Biome, bool) {
	return registry.BiomeByName(k.String())
}
func (pluginRegistries) Effect(k plugin.Key) (plugin.Effect, bool) {
	id, ok := registry.MobEffectID(k.String())
	return plugin.Effect(id), ok
}
func (pluginRegistries) Enchantment(k plugin.Key) (plugin.Enchantment, bool) {
	r, _ := registry.Get("minecraft:enchantment")
	id := r.ID(k.String())
	return plugin.Enchantment(id), id >= 0
}
func (pluginRegistries) DamageType(k plugin.Key) (plugin.DamageType, bool) {
	return registry.DamageTypeByName(k.String())
}
func (pluginRegistries) Sound(k plugin.Key) (plugin.Sound, bool) {
	id, ok := registry.SoundEventID(k.String())
	return plugin.Sound(id + 1), ok
}
func (pluginRegistries) Particle(k plugin.Key) (plugin.ParticleType, bool) {
	id, _, ok := registry.ParticleID(k.String())
	return plugin.ParticleType(id), ok
}
func (pluginRegistries) GameRule(k plugin.Key) (plugin.GameRule, bool) {
	if k.Namespace != "minecraft" {
		return 0, false
	}
	id := world.RuleIndex(k.Path)
	return plugin.GameRule(id), id != world.RuleUnknown
}
func (pluginRegistries) GameRules() []plugin.GameRuleInfo {
	out := make([]plugin.GameRuleInfo, len(world.GameRuleTable))
	for i, r := range world.GameRuleTable {
		info := plugin.GameRuleInfo{ID: plugin.GameRule(i), Key: plugin.MinecraftKey(r.ID)}
		if r.Kind == world.RuleBool {
			info.Boolean, info.Max = true, 1
			if r.DefBool {
				info.Default = 1
			}
		} else {
			info.Default, info.Min, info.Max = int(r.DefInt), int(r.Min), int(r.Max)
		}
		out[i] = info
	}
	return out
}
func (pluginRegistries) Statistic(k plugin.Key) (plugin.Statistic, bool) {
	t, v, ok := registry.StatID(registry.StatCustom, k.String())
	return plugin.Statistic{Type: registry.StatType(t), Value: v}, ok
}

func registryEntry(names []string, id int64) string {
	if id < 0 || id >= int64(len(names)) {
		return ""
	}
	return names[id]
}

func (pluginRegistries) KeyOf(id any) (plugin.Key, bool) {
	var name string
	switch id := id.(type) {
	case registry.Block:
		if id < registry.BlockCount {
			name = id.Name()
		}
	case registry.Item:
		name, _ = registry.ItemName(int32(id))
	case registry.EntityType:
		name = id.Name()
	case registry.AttributeID:
		if id >= 0 && int(id) < len(registry.Attributes) {
			name = id.Attribute().Name
		}
	case registry.Biome:
		name = id.Name()
	case plugin.Effect:
		name = registry.MobEffectName(int32(id))
	case plugin.Enchantment:
		r, _ := registry.Get("minecraft:enchantment")
		name = registryEntry(r.Entries, int64(id))
	case plugin.DamageType:
		name = id.Name()
	case plugin.Sound:
		name = id.Name()
	case plugin.ParticleType:
		name = registryEntry(registry.ParticleTypes, int64(id))
	case plugin.GameRule:
		if id >= 0 && int(id) < len(world.GameRuleTable) {
			name = "minecraft:" + world.GameRuleIndex(id).String()
		}
	case plugin.Statistic:
		_, name, _ = registry.StatName(int32(id.Type), id.Value)
	}
	k, err := plugin.ParseKey(name)
	return k, name != "" && err == nil
}
