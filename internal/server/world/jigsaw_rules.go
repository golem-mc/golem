package world

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"strings"

	"github.com/golem-mc/golem/nbt"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/registry"
)

// structureProcessor changes or drops a template block; ok is false to drop
// it.
type structureProcessor interface {
	apply(state, world uint16, x, y, z, refY int32) (out uint16, ok bool)
}

func (p ruleProcessor) apply(state, world uint16, x, y, z, refY int32) (uint16, bool) {
	return p.process(state, world, x, y, z, refY), true
}

// blockRot drops the blocks of a tag (all blocks without one) with the
// complement of its integrity.
// vanilla: BlockRotProcessor
type blockRot struct {
	tag       registry.BlockTagID
	tagged    bool
	integrity float32
}

func (b blockRot) apply(state, _ uint16, x, y, z, _ int32) (uint16, bool) {
	rng := levelgen.NewLegacyRandom(blockPosSeed(x, y, z))
	return b.applyRandom(state, rng.NextFloat)
}

// applyRandom uses a supplied stream; the callback keeps positional RNGs on the stack.
func (b blockRot) applyRandom(state uint16, nextFloat func() float32) (uint16, bool) {
	if b.tagged && !registry.BlockOf(state).InTag(b.tag) || nextFloat() <= b.integrity {
		return state, true
	}
	return state, false
}

// ruleProcessor is a rule structure processor: the first rule whose tests pass
// replaces the block.
// vanilla: RuleProcessor
type ruleProcessor struct{ rules []processorRule }

// inputKind is the input predicate's type; the zero value is
// random_block_match.
type inputKind uint8

const (
	inputRandomBlock inputKind = iota
	inputAny
	inputBlock
	inputState
	inputTag
)

// processorRule is a ProcessorRule; its location predicate is always true or
// a block_match on the block already in the world.
type processorRule struct {
	kind inputKind
	// input is the block random_block_match and block_match test, with
	// probability the former's chance; state is blockstate_match's, tag
	// tag_match's.
	input       registry.Block
	probability float32
	state       uint16
	tag         registry.BlockTagID
	// locBlock is the world block the location predicate wants, when
	// locBlocked is set.
	locBlock   registry.Block
	locBlocked bool
	pos        *axisLinearPos
	output     uint16
	loot       string
}

// axisLinearPos is the axis_aligned_linear_pos position predicate, along the
// Y axis.
// vanilla: AxisAlignedLinearPosTest
type axisLinearPos struct {
	minChance, maxChance float32
	minDist, maxDist     int32
}

type processorRuleJSON struct {
	Modifier *struct {
		Type string `json:"type"`
		Loot string `json:"loot_table"`
	} `json:"block_entity_modifier"`
	Input struct {
		Type        string         `json:"predicate_type"`
		Block       string         `json:"block"`
		Probability float32        `json:"probability"`
		Tag         string         `json:"tag"`
		BlockState  blockStateJSON `json:"block_state"`
	} `json:"input_predicate"`
	Location struct {
		Type  string `json:"predicate_type"`
		Block string `json:"block"`
	} `json:"location_predicate"`
	Position *struct {
		Type      string  `json:"predicate_type"`
		MinChance float32 `json:"min_chance"`
		MaxChance float32 `json:"max_chance"`
		MinDist   int32   `json:"min_dist"`
		MaxDist   int32   `json:"max_dist"`
		Axis      string  `json:"axis"`
	} `json:"position_predicate"`
	Output blockStateJSON `json:"output_state"`
}

// loadStructureProcessors reads a processor list by name or an inline list.
func loadStructureProcessors(raw json.RawMessage, read func(string) ([]byte, error)) (procs []structureProcessor, protected bool, err error) {
	var id string
	if json.Unmarshal(raw, &id) == nil {
		b, err := read("minecraft/worldgen/processor_list/" + strings.TrimPrefix(registry.Qualify(id), "minecraft:") + ".json")
		if err != nil {
			return nil, false, err
		}
		raw = b
	}
	var list struct {
		Processors []struct {
			Type     string              `json:"processor_type"`
			Delegate json.RawMessage     `json:"delegate"`
			Limit    json.RawMessage     `json:"limit"`
			Rules    []processorRuleJSON `json:"rules"`
			Value    string              `json:"value"`
			// Integrity and Rottable are a block_rot's.
			Integrity float32 `json:"integrity"`
			Rottable  string  `json:"rottable_blocks"`
		} `json:"processors"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, false, err
	}
	var out []structureProcessor
	for _, p := range list.Processors {
		if p.Type == "minecraft:protected_blocks" && p.Value == "#minecraft:features_cannot_replace" {
			protected = true
			continue
		}
		if p.Type == "minecraft:capped" {
			delegates, prot, err := loadStructureProcessors(json.RawMessage(`{"processors":[`+string(p.Delegate)+`]}`), read)
			if err != nil {
				return nil, false, err
			}
			if prot || len(delegates) != 1 {
				return nil, false, fmt.Errorf("unsupported capped delegate")
			}
			lo, hi, uniform, err := cappedLimit(p.Limit)
			if err != nil {
				return nil, false, err
			}
			out = append(out, cappedProcessor{delegate: delegates[0], min: lo, max: hi, uniform: uniform})
			continue
		}
		if p.Type == "minecraft:block_rot" {
			rot := blockRot{integrity: p.Integrity}
			if tag, ok := strings.CutPrefix(p.Rottable, "#"); ok {
				rot.tag, rot.tagged = registry.BlockTagOf(registry.Qualify(tag)), true
			} else if p.Rottable != "" {
				return nil, false, fmt.Errorf("unsupported rottable_blocks %s", p.Rottable)
			}
			out = append(out, rot)
			continue
		}
		if p.Type != "minecraft:rule" {
			return nil, false, fmt.Errorf("unsupported processor %s", p.Type)
		}
		rp := ruleProcessor{}
		for _, r := range p.Rules {
			rule, err := newProcessorRule(r)
			if err != nil {
				return nil, false, err
			}
			rp.rules = append(rp.rules, rule)
		}
		out = append(out, rp)
	}
	return out, protected, nil
}

func newProcessorRule(r processorRuleJSON) (processorRule, error) {
	rule := processorRule{}
	if r.Modifier != nil {
		if r.Modifier.Type != "minecraft:append_loot" {
			return rule, fmt.Errorf("unsupported block entity modifier %s", r.Modifier.Type)
		}
		rule.loot = r.Modifier.Loot
	}
	switch r.Location.Type {
	case "minecraft:always_true":
	case "minecraft:block_match":
		b, ok := registry.BlockByName(registry.Qualify(r.Location.Block))
		if !ok {
			return processorRule{}, fmt.Errorf("unknown block %s", r.Location.Block)
		}
		rule.locBlock, rule.locBlocked = b, true
	default:
		return processorRule{}, fmt.Errorf("unsupported location predicate %s", r.Location.Type)
	}
	switch r.Input.Type {
	case "minecraft:always_true":
		rule.kind = inputAny
	case "minecraft:random_block_match", "minecraft:block_match":
		b, ok := registry.BlockByName(registry.Qualify(r.Input.Block))
		if !ok {
			return processorRule{}, fmt.Errorf("unknown block %s", r.Input.Block)
		}
		rule.input, rule.probability = b, r.Input.Probability
		if r.Input.Type == "minecraft:block_match" {
			rule.kind = inputBlock
		}
	case "minecraft:blockstate_match":
		state, ok := registry.FindState(registry.Qualify(r.Input.BlockState.Name), r.Input.BlockState.Properties)
		if !ok {
			return processorRule{}, fmt.Errorf("unknown block state %s", r.Input.BlockState.Name)
		}
		rule.kind, rule.state = inputState, state
	case "minecraft:tag_match":
		rule.kind, rule.tag = inputTag, registry.BlockTagOf(registry.Qualify(strings.TrimPrefix(r.Input.Tag, "#")))
	default:
		return processorRule{}, fmt.Errorf("unsupported input predicate %s", r.Input.Type)
	}
	if p := r.Position; p != nil {
		if p.Type != "minecraft:axis_aligned_linear_pos" || (p.Axis != "" && p.Axis != "y") || p.MinDist >= p.MaxDist {
			return processorRule{}, fmt.Errorf("unsupported position predicate %s", p.Type)
		}
		rule.pos = &axisLinearPos{p.MinChance, p.MaxChance, p.MinDist, p.MaxDist}
	}
	state, ok := registry.FindState(registry.Qualify(r.Output.Name), r.Output.Properties)
	if !ok {
		return processorRule{}, fmt.Errorf("unknown block state %s", r.Output.Name)
	}
	rule.output = state
	return rule, nil
}

// process returns the state the processor puts in place of state at the world
// position, drawing from a random seeded by the position; world is the block
// the world holds there.
// vanilla: RuleProcessor.processBlock
func (p ruleProcessor) process(state, world uint16, x, y, z int32, refY int32) uint16 {
	out, _ := p.processTag(state, world, x, y, z, refY, nil)
	return out
}

func (p ruleProcessor) processTag(state, world uint16, x, y, z, refY int32, tag nbt.Compound) (uint16, nbt.Compound) {
	rng := levelgen.NewLegacyRandom(blockPosSeed(x, y, z))
	for _, r := range p.rules {
		if r.matches(state, rng) && (!r.locBlocked || registry.BlockOf(world) == r.locBlock) && (r.pos == nil || r.pos.test(y, refY, rng)) {
			if r.loot != "" {
				tag = maps.Clone(tag)
				if tag == nil {
					tag = nbt.Compound{}
				}
				tag["LootTable"] = nbt.String(r.loot)
				tag["LootTableSeed"] = nbt.Long(rng.NextLong())
			}
			return r.output, tag
		}
	}
	return state, tag
}

// matches is the input predicate.
// vanilla: RandomBlockMatchTest.test, BlockMatchTest, BlockStateMatchTest, TagMatchTest
func (r processorRule) matches(state uint16, rng *levelgen.LegacyRandomSource) bool {
	switch r.kind {
	case inputAny:
		return true
	case inputBlock:
		return registry.BlockOf(state) == r.input
	case inputState:
		return state == r.state
	case inputTag:
		return registry.BlockOf(state).InTag(r.tag)
	}
	return registry.BlockOf(state) == r.input && rng.NextFloat() < r.probability
}

// test is the position predicate along Y from the reference position; the
// distance and the lerp are floats, so the chance rounds as a float.
// vanilla: AxisAlignedLinearPosTest.test, Mth.clampedLerp
func (a *axisLinearPos) test(y, refY int32, rng *levelgen.LegacyRandomSource) bool {
	dist := abs32(y - refY)
	rnd := rng.NextFloat()
	factor := (float32(dist) - float32(a.minDist)) / (float32(a.maxDist) - float32(a.minDist))
	var chance float32
	switch {
	case factor < 0:
		chance = a.minChance
	case factor > 1:
		chance = a.maxChance
	default:
		chance = a.minChance + float32(factor*(a.maxChance-a.minChance))
	}
	return rnd <= chance
}

// applyProcessor preserves block entity data through state-only processors.
func applyProcessor(p structureProcessor, state, world uint16, x, y, z, refY int32, tag nbt.Compound) (uint16, nbt.Compound, bool) {
	if rule, ok := p.(ruleProcessor); ok {
		state, tag = rule.processTag(state, world, x, y, z, refY, tag)
		return state, tag, true
	}
	state, ok := p.apply(state, world, x, y, z, refY)
	return state, tag, ok
}

// cappedProcessor defers its delegate until all blocks have been processed.
// vanilla: CappedProcessor
type cappedProcessor struct {
	delegate structureProcessor
	min, max int32
	uniform  bool
}

func (p cappedProcessor) apply(state, _ uint16, _, _, _, _ int32) (uint16, bool) { return state, true }

func cappedLimit(raw json.RawMessage) (int32, int32, bool, error) {
	var n int32
	if json.Unmarshal(raw, &n) == nil && n > 0 {
		return n, n, false, nil
	}
	var v struct {
		Type  string `json:"type"`
		Value int32  `json:"value"`
		Min   int32  `json:"min_inclusive"`
		Max   int32  `json:"max_inclusive"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return 0, 0, false, err
	}
	switch v.Type {
	case "minecraft:constant":
		if v.Value > 0 {
			return v.Value, v.Value, false, nil
		}
	case "minecraft:uniform":
		if v.Min > 0 && v.Max >= v.Min {
			return v.Min, v.Max, true, nil
		}
	}
	return 0, 0, false, fmt.Errorf("unsupported capped limit %s", raw)
}

// finalize uses a fresh positional stream for each cap, independent of per-block draws.
// vanilla: CappedProcessor.finalizeProcessing, Util.toShuffledList
func (p cappedProcessor) finalize(placed *PlacedTemplate, seed int64, pos [3]int32, refY int32, world func(int32, int32, int32) uint16) {
	n := len(placed.Changes)
	if n == 0 {
		return
	}
	rng := levelgen.NewLegacyRandom(seed).ForkPositional().At(pos[0], pos[1], pos[2])
	limit := p.min
	if p.uniform {
		limit += rng.NextIntN(p.max - p.min + 1)
	}
	limit = min(limit, int32(n))
	if limit < 1 {
		return
	}
	indices := make([]int, n)
	for i := range indices {
		indices[i] = i
	}
	for i := n; i > 1; i-- {
		j := int(rng.NextIntN(int32(i)))
		indices[i-1], indices[j] = indices[j], indices[i-1]
	}
	for _, i := range indices {
		b := &placed.Changes[i]
		var oldWorld uint16
		if world != nil {
			oldWorld = world(b.X, b.Y, b.Z)
		}
		state, tag, ok := applyProcessor(p.delegate, b.State, oldWorld, b.X, b.Y, b.Z, refY, placed.Tags[i])
		if !ok || state == b.State && reflect.DeepEqual(tag, placed.Tags[i]) {
			continue
		}
		b.State, placed.Tags[i] = state, tag
		limit--
		if limit == 0 {
			break
		}
	}
}

// blockStateJSON accepts BlockState.CODEC's compact identifier and property object forms.
type blockStateJSON struct {
	Name       string            `json:"Name"`
	Properties map[string]string `json:"Properties"`
}

func (s *blockStateJSON) UnmarshalJSON(data []byte) error {
	if err := json.Unmarshal(data, &s.Name); err == nil {
		return nil
	}
	var object struct {
		Name       string            `json:"Name"`
		ID         string            `json:"id"`
		Properties map[string]string `json:"properties"`
	}
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	s.Name, s.Properties = object.ID, object.Properties
	if s.Name == "" {
		s.Name = object.Name
	}
	return nil
}
