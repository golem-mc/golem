package world

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/golem-mc/golem/nbt"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/registry"
)

// structureProcessor changes or drops a template block; ok is false to drop
// it.
type structureProcessor interface {
	apply(state, world uint16, x, y, z int32, ref [3]int32) (out uint16, ok bool)
}

func (p ruleProcessor) apply(state, world uint16, x, y, z int32, ref [3]int32) (uint16, bool) {
	return p.process(state, world, x, y, z, ref), true
}

// holderSet is a HolderSet<Block>: a #tag, one block or a list of blocks.
type holderSet struct {
	tag    registry.BlockTagID
	tagged bool
	blocks []registry.Block
}

func parseHolderSet(raw json.RawMessage) (holderSet, error) {
	var names []string
	var one string
	if json.Unmarshal(raw, &one) == nil {
		names = []string{one}
	} else if err := json.Unmarshal(raw, &names); err != nil {
		return holderSet{}, fmt.Errorf("block set %s: %w", raw, err)
	}
	var set holderSet
	if len(names) == 1 {
		if tag, ok := strings.CutPrefix(names[0], "#"); ok {
			set.tag, set.tagged = registry.BlockTagOf(registry.Qualify(tag)), true
			return set, nil
		}
	}
	for _, n := range names {
		b, ok := registry.BlockByName(registry.Qualify(n))
		if !ok {
			return holderSet{}, fmt.Errorf("unknown block %s", n)
		}
		set.blocks = append(set.blocks, b)
	}
	return set, nil
}

func (h holderSet) contains(state uint16) bool {
	b := registry.BlockOf(state)
	if h.tagged {
		return b.InTag(h.tag)
	}
	return slices.Contains(h.blocks, b)
}

// blockRot drops the blocks of a tag (all blocks without one) with the
// complement of its integrity.
// vanilla: BlockRotProcessor
type blockRot struct {
	rottable  *holderSet
	integrity float32
}

func (b blockRot) apply(state, _ uint16, x, y, z int32, _ [3]int32) (uint16, bool) {
	rng := levelgen.NewLegacyRandom(blockPosSeed(x, y, z))
	return b.applyRandom(state, rng.NextFloat)
}

// applyRandom uses a supplied stream; the callback keeps positional RNGs on the stack.
func (b blockRot) applyRandom(state uint16, nextFloat func() float32) (uint16, bool) {
	if b.rottable != nil && !b.rottable.contains(state) || nextFloat() <= b.integrity {
		return state, true
	}
	return state, false
}

// ruleProcessor is a rule structure processor: the first rule whose tests pass
// replaces the block.
// vanilla: RuleProcessor
type ruleProcessor struct{ rules []processorRule }

// inputKind is a rule test's type; the zero value is random_block_match.
type inputKind uint8

const (
	inputRandomBlock inputKind = iota
	inputAny
	inputBlock
	inputState
	inputTag
	inputRandomState
)

// ruleTest is a RuleTest. input is the block random_block_match and
// block_match test, with probability the random tests' chance; state is
// blockstate_match's and random_blockstate_match's, tag tag_match's.
type ruleTest struct {
	kind        inputKind
	input       registry.Block
	probability float32
	state       uint16
	tag         registry.BlockTagID
}

// processorRule is a ProcessorRule; loc, when set, tests the block already in
// the world.
type processorRule struct {
	kind        inputKind
	input       registry.Block
	probability float32
	state       uint16
	tag         registry.BlockTagID
	loc         *ruleTest
	pos         *linearPos
	output      uint16
	modifier    blockEntityModifier
}

func (r processorRule) inputTest() ruleTest {
	return ruleTest{r.kind, r.input, r.probability, r.state, r.tag}
}

// modifierKind is a rule_block_entity_modifier type; the zero value is
// passthrough.
type modifierKind uint8

const (
	modPassthrough modifierKind = iota
	modClear
	modAppendStatic
	modAppendLoot
)

// blockEntityModifier is a RuleBlockEntityModifier; data is append_static's
// compound, loot append_loot's table.
type blockEntityModifier struct {
	kind modifierKind
	data nbt.Compound
	loot string
}

func lootModifier(table string) blockEntityModifier {
	return blockEntityModifier{kind: modAppendLoot, loot: table}
}

// apply returns the block entity data after the modifier; the tag it is given
// is never changed.
// vanilla: Clear, Passthrough, AppendStatic, AppendLoot
func (m blockEntityModifier) apply(rng *levelgen.LegacyRandomSource, tag nbt.Compound) nbt.Compound {
	switch m.kind {
	case modClear:
		return nbt.Compound{}
	case modAppendStatic:
		out, _ := nbt.Copy(tag).(nbt.Compound)
		if out == nil {
			out = nbt.Compound{}
		}
		mergeCompound(out, m.data)
		return out
	case modAppendLoot:
		out := maps.Clone(tag)
		if out == nil {
			out = nbt.Compound{}
		}
		out["LootTable"] = nbt.String(m.loot)
		out["LootTableSeed"] = nbt.Long(rng.NextLong())
		return out
	}
	return tag
}

// mergeCompound copies src into dst, merging nested compounds.
// vanilla: CompoundTag.merge
func mergeCompound(dst, src nbt.Compound) {
	for k, v := range src {
		if sub, ok := v.(nbt.Compound); ok {
			if cur, ok := dst[k].(nbt.Compound); ok {
				mergeCompound(cur, sub)
				continue
			}
		}
		dst[k] = nbt.Copy(v)
	}
}

// linearPos is the linear_pos position predicate (axis < 0, the Manhattan
// distance) or axis_aligned_linear_pos (axis 0, 1, 2 for x, y, z).
// vanilla: LinearPosTest, AxisAlignedLinearPosTest
type linearPos struct {
	axis                 int8
	minChance, maxChance float32
	minDist, maxDist     int32
}

type ruleTestJSON struct {
	Type        string         `json:"predicate_type"`
	Block       string         `json:"block"`
	Probability float32        `json:"probability"`
	Tag         string         `json:"tag"`
	BlockState  blockStateJSON `json:"block_state"`
}

type processorRuleJSON struct {
	Modifier *struct {
		Type string          `json:"type"`
		Loot string          `json:"loot_table"`
		Data json.RawMessage `json:"data"`
	} `json:"block_entity_modifier"`
	Input    ruleTestJSON  `json:"input_predicate"`
	Location *ruleTestJSON `json:"location_predicate"`
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
			Value    json.RawMessage     `json:"value"`
			// Integrity and Rottable are a block_rot's.
			Integrity float32          `json:"integrity"`
			Rottable  json.RawMessage  `json:"rottable_blocks"`
			Blocks    []blockStateJSON `json:"blocks"`
			Heightmap *string          `json:"heightmap"`
			Offset    int32            `json:"offset"`
			Mossiness float32          `json:"mossiness"`
		} `json:"processors"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, false, err
	}
	var out []structureProcessor
	for _, p := range list.Processors {
		switch p.Type {
		case "minecraft:protected_blocks":
			set, err := parseHolderSet(p.Value)
			if err != nil {
				return nil, false, err
			}
			if set.tagged && set.tag == registry.TagIDFeaturesCannotReplace {
				protected = true
			} else {
				out = append(out, protectedBlocks{set})
			}
			continue
		case "minecraft:capped":
			delegates, prot, err := loadStructureProcessors(json.RawMessage(`{"processors":[`+string(p.Delegate)+`]}`), read)
			if err != nil {
				return nil, false, err
			}
			if prot || len(delegates) != 1 {
				return nil, false, fmt.Errorf("unsupported capped delegate")
			}
			if _, ok := delegates[0].(gravityProcessor); ok {
				return nil, false, fmt.Errorf("unsupported capped delegate gravity")
			}
			lo, hi, uniform, err := cappedLimit(p.Limit)
			if err != nil {
				return nil, false, err
			}
			out = append(out, cappedProcessor{delegate: delegates[0], min: lo, max: hi, uniform: uniform})
			continue
		case "minecraft:block_rot":
			rot := blockRot{integrity: p.Integrity}
			if len(p.Rottable) != 0 {
				set, err := parseHolderSet(p.Rottable)
				if err != nil {
					return nil, false, err
				}
				rot.rottable = &set
			}
			out = append(out, rot)
			continue
		case "minecraft:block_ignore":
			ignore := blockIgnore{}
			for _, name := range p.Blocks {
				b, ok := registry.BlockByName(registry.Qualify(name.Name))
				if !ok {
					return nil, false, fmt.Errorf("unknown block %s", name.Name)
				}
				ignore.blocks = append(ignore.blocks, b)
			}
			out = append(out, ignore)
			continue
		case "minecraft:gravity":
			g := gravityProcessor{kind: hmWorldSurfaceWG, offset: p.Offset}
			if p.Heightmap != nil {
				kind, ok := heightmapKinds[*p.Heightmap]
				if !ok {
					return nil, false, fmt.Errorf("unsupported heightmap %s", *p.Heightmap)
				}
				g.kind = kind
			}
			out = append(out, g)
			continue
		case "minecraft:block_age":
			out = append(out, blockAge{mossiness: p.Mossiness})
			continue
		case "minecraft:blackstone_replace":
			out = append(out, blackstoneProcessor{})
			continue
		case "minecraft:lava_submerged_block":
			out = append(out, lavaSubmerged{})
			continue
		case "minecraft:nop", "minecraft:jigsaw_replacement":
			// Template blocks reach the processors already through the jigsaw
			// replacement, so jigsaw_replacement has nothing left to do.
			out = append(out, nopProcessor{})
			continue
		case "minecraft:rule":
		default:
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
	if m := r.Modifier; m != nil {
		switch m.Type {
		case "minecraft:passthrough":
		case "minecraft:clear":
			rule.modifier.kind = modClear
		case "minecraft:append_loot":
			rule.modifier = lootModifier(m.Loot)
		case "minecraft:append_static":
			var text string
			if json.Unmarshal(m.Data, &text) != nil {
				return rule, fmt.Errorf("unsupported append_static data %s", m.Data)
			}
			data, err := nbt.ParseSNBT(text)
			if err != nil {
				return rule, fmt.Errorf("append_static data: %w", err)
			}
			rule.modifier = blockEntityModifier{kind: modAppendStatic, data: data}
		default:
			return rule, fmt.Errorf("unsupported block entity modifier %s", m.Type)
		}
	}
	if r.Location != nil {
		loc, err := newRuleTest(*r.Location, "location")
		if err != nil {
			return rule, err
		}
		if loc.kind != inputAny {
			rule.loc = &loc
		}
	}
	in, err := newRuleTest(r.Input, "input")
	if err != nil {
		return rule, err
	}
	rule.kind, rule.input, rule.probability, rule.state, rule.tag = in.kind, in.input, in.probability, in.state, in.tag
	if p := r.Position; p != nil {
		lin := linearPos{axis: -1, minChance: p.MinChance, maxChance: p.MaxChance, minDist: p.MinDist, maxDist: p.MaxDist}
		switch p.Type {
		case "minecraft:always_true":
		case "minecraft:linear_pos":
		case "minecraft:axis_aligned_linear_pos":
			axis := strings.Index("xyz", p.Axis)
			if p.Axis == "" {
				axis = 1
			}
			if axis < 0 || len(p.Axis) > 1 {
				return rule, fmt.Errorf("unknown axis %s", p.Axis)
			}
			lin.axis = int8(axis)
		default:
			return rule, fmt.Errorf("unsupported position predicate %s", p.Type)
		}
		if p.Type != "minecraft:always_true" && lin.minDist >= lin.maxDist {
			return rule, fmt.Errorf("invalid position range [%d,%d]", lin.minDist, lin.maxDist)
		}
		if p.Type != "minecraft:always_true" {
			rule.pos = &lin
		}
	}
	state, ok := registry.FindState(registry.Qualify(r.Output.Name), r.Output.Properties)
	if !ok {
		return rule, fmt.Errorf("unknown block state %s", r.Output.Name)
	}
	rule.output = state
	return rule, nil
}

// newRuleTest reads a RuleTest; where names the field in an error.
func newRuleTest(j ruleTestJSON, where string) (ruleTest, error) {
	var t ruleTest
	switch j.Type {
	case "minecraft:always_true":
		t.kind = inputAny
	case "minecraft:random_block_match", "minecraft:block_match":
		b, ok := registry.BlockByName(registry.Qualify(j.Block))
		if !ok {
			return t, fmt.Errorf("unknown block %s", j.Block)
		}
		t.input, t.probability = b, j.Probability
		if j.Type == "minecraft:block_match" {
			t.kind = inputBlock
		}
	case "minecraft:blockstate_match", "minecraft:random_blockstate_match":
		state, ok := registry.FindState(registry.Qualify(j.BlockState.Name), j.BlockState.Properties)
		if !ok {
			return t, fmt.Errorf("unknown block state %s", j.BlockState.Name)
		}
		t.kind, t.state, t.probability = inputState, state, j.Probability
		if j.Type == "minecraft:random_blockstate_match" {
			t.kind = inputRandomState
		}
	case "minecraft:tag_match":
		t.kind, t.tag = inputTag, registry.BlockTagOf(registry.Qualify(strings.TrimPrefix(j.Tag, "#")))
	default:
		return t, fmt.Errorf("unsupported %s predicate %s", where, j.Type)
	}
	return t, nil
}

// process returns the state the processor puts in place of state at the world
// position, drawing from a random seeded by the position; world is the block
// the world holds there.
// vanilla: RuleProcessor.processBlock
func (p ruleProcessor) process(state, world uint16, x, y, z int32, ref [3]int32) uint16 {
	out, _ := p.processTag(state, world, x, y, z, ref, nil)
	return out
}

func (p ruleProcessor) processTag(state, world uint16, x, y, z int32, ref [3]int32, tag nbt.Compound) (uint16, nbt.Compound) {
	rng := levelgen.NewLegacyRandom(blockPosSeed(x, y, z))
	for _, r := range p.rules {
		if r.inputTest().matches(state, rng) && (r.loc == nil || r.loc.matches(world, rng)) && (r.pos == nil || r.pos.test(x, y, z, ref, rng)) {
			return r.output, r.modifier.apply(rng, tag)
		}
	}
	return state, tag
}

// matches is the rule test.
// vanilla: RandomBlockMatchTest.test, BlockMatchTest, BlockStateMatchTest, TagMatchTest, RandomBlockStateMatchTest
func (t ruleTest) matches(state uint16, rng *levelgen.LegacyRandomSource) bool {
	switch t.kind {
	case inputAny:
		return true
	case inputBlock:
		return registry.BlockOf(state) == t.input
	case inputState:
		return state == t.state
	case inputTag:
		return registry.BlockOf(state).InTag(t.tag)
	case inputRandomState:
		return state == t.state && rng.NextFloat() < t.probability
	}
	return registry.BlockOf(state) == t.input && rng.NextFloat() < t.probability
}

// test is the position predicate from the reference position; the distance
// and the lerp are floats, so the chance rounds as a float.
// vanilla: LinearPosTest.test, AxisAlignedLinearPosTest.test, Mth.clampedLerp
func (a *linearPos) test(x, y, z int32, ref [3]int32, rng *levelgen.LegacyRandomSource) bool {
	d := [3]int32{abs32(x - ref[0]), abs32(y - ref[1]), abs32(z - ref[2])}
	dist := d[0] + d[1] + d[2]
	if a.axis >= 0 {
		dist = d[a.axis]
	}
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
func applyProcessor(p structureProcessor, state, world uint16, x, y, z int32, ref [3]int32, tag nbt.Compound) (uint16, nbt.Compound, bool) {
	if rule, ok := p.(ruleProcessor); ok {
		state, tag = rule.processTag(state, world, x, y, z, ref, tag)
		return state, tag, true
	}
	state, ok := p.apply(state, world, x, y, z, ref)
	return state, tag, ok
}

// runProcessors applies a processor list to one template block, which a
// gravity processor may move; tmplY is its Y in the template, and the
// processors after a move see the world at the new position.
// vanilla: StructureTemplate.processBlockInfos
func runProcessors(procs []structureProcessor, state uint16, tag nbt.Compound, x, y, z, tmplY int32, ref [3]int32,
	world func(x, y, z int32) uint16, height func(kind heightmapKind, x, z int32) int32) (uint16, nbt.Compound, int32, bool) {
	var w uint16
	loaded := false
	for _, proc := range procs {
		if g, ok := proc.(gravityProcessor); ok {
			y = height(g.kind, x, z) + g.offset + tmplY
			loaded = false
			continue
		}
		if !loaded {
			w, loaded = world(x, y, z), true
		}
		var ok bool
		if state, tag, ok = applyProcessor(proc, state, w, x, y, z, ref, tag); !ok {
			return 0, nil, y, false
		}
	}
	return state, tag, y, true
}

// gravityProcessor moves a block to the heightmap plus its template height.
// vanilla: GravityProcessor
type gravityProcessor struct {
	kind   heightmapKind
	offset int32
}

func (gravityProcessor) apply(state, _ uint16, _, _, _ int32, _ [3]int32) (uint16, bool) {
	return state, true
}

// protectedBlocks drops the blocks over a block the world holds from the set.
// vanilla: ProtectedBlockProcessor
type protectedBlocks struct{ set holderSet }

func (p protectedBlocks) apply(state, world uint16, _, _, _ int32, _ [3]int32) (uint16, bool) {
	return state, !p.set.contains(world)
}

// blockIgnore drops the listed blocks.
// vanilla: BlockIgnoreProcessor
type blockIgnore struct{ blocks []registry.Block }

func (p blockIgnore) apply(state, _ uint16, _, _, _ int32, _ [3]int32) (uint16, bool) {
	return state, !slices.Contains(p.blocks, registry.BlockOf(state))
}

// nopProcessor changes nothing.
// vanilla: NopProcessor
type nopProcessor struct{}

func (nopProcessor) apply(state, _ uint16, _, _, _ int32, _ [3]int32) (uint16, bool) {
	return state, true
}

// lavaSubmerged turns a block that is not a full cube into lava where the
// world already holds lava.
// vanilla: LavaSubmergedBlockProcessor
type lavaSubmerged struct{}

func (lavaSubmerged) apply(state, world uint16, _, _, _ int32, _ [3]int32) (uint16, bool) {
	if registry.BlockOf(world) == registry.BlockLava && !registry.CollisionShapeFull(state) {
		return registry.BlockLava.DefaultState(), true
	}
	return state, true
}

// blockAge weathers stone bricks, stairs, slabs, walls and obsidian.
// vanilla: BlockAgeProcessor
type blockAge struct{ mossiness float32 }

func (p blockAge) apply(state, _ uint16, x, y, z int32, _ [3]int32) (uint16, bool) {
	return ruinedPortalAge(state, levelgen.NewLegacyRandom(blockPosSeed(x, y, z)), p.mossiness), true
}

// blackstoneProcessor is BlackstoneReplaceProcessor.
type blackstoneProcessor struct{}

func (blackstoneProcessor) apply(state, _ uint16, _, _, _ int32, _ [3]int32) (uint16, bool) {
	return blackstoneReplace(state), true
}

// cappedProcessor defers its delegate until all blocks have been processed.
// vanilla: CappedProcessor
type cappedProcessor struct {
	delegate structureProcessor
	min, max int32
	uniform  bool
}

func (p cappedProcessor) apply(state, _ uint16, _, _, _ int32, _ [3]int32) (uint16, bool) {
	return state, true
}

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
func (p cappedProcessor) finalize(placed *PlacedTemplate, seed int64, pos [3]int32, ref [3]int32, world func(int32, int32, int32) uint16) {
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
		state, tag, ok := applyProcessor(p.delegate, b.State, oldWorld, b.X, b.Y, b.Z, ref, placed.Tags[i])
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
