package protocol

// ToolComponent is a stack's own minecraft:tool, which replaces the item's
// default one for every digging rule. Its rules' block sets stay as the wire
// carries them; the rules package matches them against a block.
// vanilla: Tool.STREAM_CODEC
type ToolComponent struct {
	Rules                      []ToolRule
	DefaultMiningSpeed         float32
	DamagePerBlock             int32
	CanDestroyBlocksInCreative bool
}

// ToolRule is one rule of a ToolComponent. HasSpeed and HasCorrect say
// whether the rule carries the optional Speed and Correct.
type ToolRule struct {
	Blocks     BlockPredicateSet
	Speed      float32
	HasSpeed   bool
	Correct    bool
	HasCorrect bool
}

// readTool reads a minecraft:tool value: the rules, the default mining
// speed, the damage per block and the creative flag.
// vanilla: Tool.STREAM_CODEC, Tool.Rule.STREAM_CODEC
func readTool(r *Reader) (ToolComponent, bool) {
	count := r.ReadVarInt()
	if r.Err() != nil || count < 0 {
		return ToolComponent{}, false
	}
	// A rule is at least a set count and two absent flags.
	t := ToolComponent{Rules: make([]ToolRule, 0, min(count, int32(r.Remaining()/3)))}
	for range count {
		set, ok := readBlockSet(r)
		if !ok {
			return ToolComponent{}, false
		}
		rule := ToolRule{Blocks: set}
		if rule.HasSpeed = r.ReadBool(); rule.HasSpeed {
			rule.Speed = r.ReadF32()
		}
		if rule.HasCorrect = r.ReadBool(); rule.HasCorrect {
			rule.Correct = r.ReadBool()
		}
		if r.Err() != nil {
			return ToolComponent{}, false
		}
		t.Rules = append(t.Rules, rule)
	}
	t.DefaultMiningSpeed = r.ReadF32()
	t.DamagePerBlock = r.ReadVarInt()
	t.CanDestroyBlocksInCreative = r.ReadBool()
	if r.Err() != nil {
		return ToolComponent{}, false
	}
	return t, true
}

// ToolOf returns the stack's own minecraft:tool, false for a stack without
// one, which digs by its item's default.
func (s ItemStack) ToolOf() (ToolComponent, bool) {
	for _, raw := range s.Raw {
		if raw.Type != DataComponentTool {
			continue
		}
		r := NewReader(raw.Data)
		t, ok := readTool(r)
		return t, ok && r.Remaining() == 0
	}
	return ToolComponent{}, false
}

// SetTool lays a minecraft:tool on the stack in its wire shape, replacing any
// the stack had.
func (s *ItemStack) SetTool(t ToolComponent) {
	var value Buffer
	value.PutVarInt(int32(len(t.Rules)))
	for _, rule := range t.Rules {
		putBlockSet(&value, rule.Blocks)
		value.PutBool(rule.HasSpeed)
		if rule.HasSpeed {
			value.PutF32(rule.Speed)
		}
		value.PutBool(rule.HasCorrect)
		if rule.HasCorrect {
			value.PutBool(rule.Correct)
		}
	}
	value.PutF32(t.DefaultMiningSpeed)
	value.PutVarInt(t.DamagePerBlock)
	value.PutBool(t.CanDestroyBlocksInCreative)
	s.Raw = append([]RawComponent(nil), s.Raw...)
	s.putRaw(RawComponent{Type: DataComponentTool, Data: value.Bytes()})
}

// putBlockSet writes a holder set in the shape readBlockSet reads.
func putBlockSet(b *Buffer, set BlockPredicateSet) {
	if set.Tag != "" {
		b.PutVarInt(0)
		b.PutString(set.Tag)
		return
	}
	b.PutVarInt(int32(len(set.Inline)) + 1)
	for _, id := range set.Inline {
		b.PutVarInt(id)
	}
}
