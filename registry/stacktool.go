package registry

import (
	"math"
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// blockNamesByID is BlockIDs inverted, for writing an inline block set back
// out by name.
var blockNamesByID = func() map[int32]string {
	m := make(map[int32]string, len(BlockIDs))
	for name, id := range BlockIDs {
		m[id] = name
	}
	return m
}()

// toolOf reads a minecraft:tool value in the shape its codec stores.
// vanilla: Tool.CODEC, Tool.Rule.CODEC
func toolOf(value nbt.Tag) (protocol.ToolComponent, bool) {
	entry, ok := nbt.CompoundView(value)
	if !ok {
		return protocol.ToolComponent{}, false
	}
	tool := protocol.ToolComponent{DefaultMiningSpeed: 1, DamagePerBlock: 1, CanDestroyBlocksInCreative: true}
	rules, ok := entry["rules"].(nbt.List)
	if !ok {
		return protocol.ToolComponent{}, false
	}
	for _, item := range rules.Items {
		rule, ok := toolRuleOf(item)
		if !ok {
			return protocol.ToolComponent{}, false
		}
		tool.Rules = append(tool.Rules, rule)
	}
	if field, has := entry["default_mining_speed"]; has {
		speed, ok := floatOf(field)
		if !ok {
			return protocol.ToolComponent{}, false
		}
		tool.DefaultMiningSpeed = speed
	}
	if field, has := entry["damage_per_block"]; has {
		damage, ok := nbt.IntValue(field)
		if !ok || damage < 0 {
			return protocol.ToolComponent{}, false
		}
		tool.DamagePerBlock = damage
	}
	if field, has := entry["can_destroy_blocks_in_creative"]; has {
		flag, ok := boolValue(field)
		if !ok {
			return protocol.ToolComponent{}, false
		}
		tool.CanDestroyBlocksInCreative = flag
	}
	return tool, true
}

func toolRuleOf(value nbt.Tag) (protocol.ToolRule, bool) {
	entry, ok := nbt.CompoundView(value)
	if !ok {
		return protocol.ToolRule{}, false
	}
	set, ok := blockSetOf(entry["blocks"])
	if !ok {
		return protocol.ToolRule{}, false
	}
	rule := protocol.ToolRule{Blocks: set}
	if field, has := entry["speed"]; has {
		speed, ok := floatOf(field)
		if !ok || !(speed > 0) {
			return protocol.ToolRule{}, false
		}
		rule.Speed, rule.HasSpeed = speed, true
	}
	if field, has := entry["correct_for_drops"]; has {
		flag, ok := boolValue(field)
		if !ok {
			return protocol.ToolRule{}, false
		}
		rule.Correct, rule.HasCorrect = flag, true
	}
	return rule, true
}

// blockSetOf reads a block holder set: a "#tag", one block, or a list of
// blocks and tags' worth of names.
// vanilla: RegistryCodecs.homogeneousList
func blockSetOf(value nbt.Tag) (protocol.BlockPredicateSet, bool) {
	set := protocol.BlockPredicateSet{Present: true}
	switch v := value.(type) {
	case nbt.String:
		if tag, isTag := strings.CutPrefix(string(v), "#"); isTag {
			tag = Qualify(tag)
			if !protocol.ValidIdentifier(tag) || !BlockTagKnown(tag) {
				return set, false
			}
			set.Tag = tag
			return set, true
		}
		id, ok := BlockIDs[Qualify(string(v))]
		set.Inline = []int32{id}
		return set, ok
	case nbt.List:
		set.Inline = []int32{}
		for _, item := range v.Items {
			name, ok := item.(nbt.String)
			id, known := BlockIDs[Qualify(string(name))]
			if !ok || !known {
				return set, false
			}
			set.Inline = append(set.Inline, id)
		}
		return set, true
	}
	return set, false
}

// floatOf reads a numeric tag as the float a codec's FLOAT reads.
func floatOf(value nbt.Tag) (float32, bool) {
	switch v := value.(type) {
	case nbt.Float:
		return float32(v), !math.IsNaN(float64(v))
	case nbt.Double:
		return float32(v), !math.IsNaN(float64(v))
	}
	n, ok := nbt.LongValue(value)
	return float32(n), ok
}

// toolTag writes a minecraft:tool in the shape toolOf reads, leaving the
// fields that hold their defaults out.
func toolTag(tool protocol.ToolComponent) *nbt.OrderedCompound {
	rules := nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{}}
	for _, rule := range tool.Rules {
		entry := nbt.NewOrderedCompound(1)
		entry.Set("blocks", blockSetTag(rule.Blocks))
		if rule.HasSpeed {
			entry.Set("speed", nbt.Float(rule.Speed))
		}
		if rule.HasCorrect {
			entry.Set("correct_for_drops", nbt.Byte(boolByte(rule.Correct)))
		}
		rules.Items = append(rules.Items, entry)
	}
	out := nbt.NewOrderedCompound(1)
	out.Set("rules", rules)
	if tool.DefaultMiningSpeed != 1 {
		out.Set("default_mining_speed", nbt.Float(tool.DefaultMiningSpeed))
	}
	if tool.DamagePerBlock != 1 {
		out.Set("damage_per_block", nbt.Int(tool.DamagePerBlock))
	}
	if !tool.CanDestroyBlocksInCreative {
		out.Set("can_destroy_blocks_in_creative", nbt.Byte(0))
	}
	return out
}

// vanilla: HolderSetCodec.directCodec, ExtraCodecs.compactListCodec
func blockSetTag(set protocol.BlockPredicateSet) nbt.Tag {
	if set.Tag != "" {
		return nbt.String("#" + set.Tag)
	}
	if len(set.Inline) == 1 {
		return nbt.String(blockNamesByID[set.Inline[0]])
	}
	names := nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{}}
	for _, id := range set.Inline {
		names.Items = append(names.Items, nbt.String(blockNamesByID[id]))
	}
	return names
}

func boolByte(b bool) int8 {
	if b {
		return 1
	}
	return 0
}
