package datapack

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// intVec reads a Vec3i, which is absent for zero.
func intVec(o obj, key string) [3]int32 {
	if _, ok := o[key]; !ok {
		return [3]int32{}
	}
	l := list(o[key], "an offset")
	if len(l) != 3 {
		fail("an offset of %d numbers", len(l))
	}
	var out [3]int32
	for i, x := range l {
		f, ok := num(x)
		if !ok || f != float64(int32(f)) {
			fail("%v is not an integer", x)
		}
		out[i] = int32(f)
	}
	return out
}

// gameEventOf reads trigger_game_event, which is absent for none.
func gameEventOf(o obj) registry.GameEvent {
	name := optStr(o, "trigger_game_event")
	if name == "" {
		return registry.GameEventNone
	}
	name = registry.Qualify(name)
	ev := registry.GameEventByName(name)
	if ev == registry.GameEventNone {
		fail("unknown game event %s", name)
	}
	return ev
}

const legacySimpleStateProvider = "minecraft:simple_state_provider"

// ErrLegacySimpleStateProvider is a registry decoding error that prevents world load.
var ErrLegacySimpleStateProvider = errors.New("unknown block state provider type " + legacySimpleStateProvider)

// vanilla: BlockStateProviderTypes.bootstrap
func stateProviderType(o obj) string {
	kind := registry.Qualify(requireStr(o, "type"))
	if kind == legacySimpleStateProvider {
		panic(lootFailure{fatal: ErrLegacySimpleStateProvider})
	}
	return kind
}

// simpleState reads a simple state provider or a bare state; a registry
// reference fails the decode. A state is a block name or {id, properties}.
// vanilla: BlockStateProvider.CODEC, BlockState.FULL_CODEC
func simpleState(v any) uint16 {
	if o, ok := v.(obj); ok && o["type"] != nil {
		if stateProviderType(o) != "minecraft:simple" {
			unsupported("a block state provider other than simple")
		}
		unknownKeys(o, "a simple state provider", "type", "state")
		v = required(o, "state")
	}
	if s, ok := v.(string); ok {
		v = obj{"id": s}
	}
	o := object(v, "a block state")
	unknownKeys(o, "a block state", "id", "properties")
	var props map[string]string
	if p, ok := o["properties"].(obj); ok {
		props = make(map[string]string, len(p))
		for k, x := range p {
			props[k] = str(x, "a state property")
		}
	}
	state, found := registry.FindState(registry.Qualify(requireStr(o, "id")), props)
	if !found {
		fail("unknown block state %v", o)
	}
	return state
}

// anchorOf reads a VerticalAnchor: {absolute}, {above_bottom} or {below_top}.
// vanilla: VerticalAnchor.CODEC
func anchorOf(v any) registry.EnchAnchor {
	o := object(v, "a vertical anchor")
	for _, kind := range []string{"absolute", "above_bottom", "below_top"} {
		if n, ok := o[kind]; ok {
			f, isNum := num(n)
			if !isNum || len(o) != 1 {
				fail("a malformed vertical anchor")
			}
			return registry.EnchAnchor{Kind: kind, Value: int32(f)}
		}
	}
	fail("a vertical anchor with none of absolute, above_bottom, below_top")
	return registry.EnchAnchor{}
}

// blockPredicate reads a block predicate.
// vanilla: BlockPredicate.CODEC
func blockPredicate(o obj) *registry.EnchBlockPredicate {
	kind := registry.Qualify(requireStr(o, "type"))
	out := &registry.EnchBlockPredicate{Kind: strings.TrimPrefix(kind, "minecraft:"), Offset: intVec(o, "offset")}
	switch kind {
	case "minecraft:matching_blocks":
		unknownKeys(o, "a matching_blocks predicate", "type", "blocks", "offset")
		for _, x := range holderList(required(o, "blocks")) {
			name := registry.Qualify(str(x, "a block"))
			b, ok := registry.BlockByName(name)
			if !ok {
				fail("unknown block %s", name)
			}
			out.Blocks = append(out.Blocks, b)
		}
	case "minecraft:matching_block_tag":
		unknownKeys(o, "a matching_block_tag predicate", "type", "tag", "offset")
		out.Tag = registry.BlockTagOf(strings.TrimPrefix(registry.Qualify(requireStr(o, "tag")), "#"))
	case "minecraft:matching_fluids", "minecraft:matching_biomes":
		key := "fluids"
		if kind == "minecraft:matching_biomes" {
			key = "biomes"
		}
		unknownKeys(o, "a "+out.Kind+" predicate", "type", key, "offset")
		for _, x := range holderList(required(o, key)) {
			name := str(x, "a name")
			if strings.HasPrefix(name, "#") {
				unsupported("a %s predicate with a tag", out.Kind)
			}
			out.Names = append(out.Names, registry.Qualify(name))
		}
	case "minecraft:true", "minecraft:solid", "minecraft:replaceable", "minecraft:inside_world_bounds", "minecraft:unobstructed":
		unknownKeys(o, "a "+out.Kind+" predicate", "type", "offset")
	case "minecraft:has_sturdy_face":
		unknownKeys(o, "a has_sturdy_face predicate", "type", "offset", "direction")
		out.Face = requireStr(o, "direction")
		if !slices.Contains([]string{"down", "up", "north", "south", "west", "east"}, out.Face) {
			fail("direction %s", out.Face)
		}
	case "minecraft:would_survive":
		unknownKeys(o, "a would_survive predicate", "type", "offset", "state")
		out.State = simpleState(required(o, "state"))
	case "minecraft:height_range":
		unknownKeys(o, "a height_range predicate", "type", "min_inclusive", "max_inclusive")
		out.Bottom, out.Top = anchorOf(required(o, "min_inclusive")), anchorOf(required(o, "max_inclusive"))
	case "minecraft:volume_match":
		unknownKeys(o, "a volume_match predicate", "type", "min", "max", "match")
		out.Min, out.Max = intVec(o, "min"), intVec(o, "max")
		out.Terms = []*registry.EnchBlockPredicate{blockPredicate(object(required(o, "match"), "a block predicate"))}
	case "minecraft:not":
		unknownKeys(o, "a not predicate", "type", "predicate")
		out.Terms = []*registry.EnchBlockPredicate{blockPredicate(object(required(o, "predicate"), "a block predicate"))}
	case "minecraft:all_of", "minecraft:any_of":
		unknownKeys(o, "a "+out.Kind+" predicate", "type", "predicates")
		for _, x := range list(required(o, "predicates"), "predicates") {
			out.Terms = append(out.Terms, blockPredicate(object(x, "a block predicate")))
		}
	default:
		unsupported("block predicate %s is not supported by this server", kind)
	}
	return out
}

func optPredicate(o obj) *registry.EnchBlockPredicate {
	if p, ok := optObject(o, "predicate"); ok {
		return blockPredicate(p)
	}
	return nil
}

// ErrRemovedBlockStateProvider reports a retired provider type in a replace effect.
var ErrRemovedBlockStateProvider = errors.New("removed block state provider type")

// stateProvider reads a block_state provider: a simple one gives its state, a
// pure one its raw JSON for the server to compile.
// vanilla: BlockStateProvider.CODEC
func stateProvider(v any) (state uint16, raw any) {
	if o, ok := v.(obj); ok && o["type"] != nil && stateProviderType(o) != "minecraft:simple" {
		checkReplaceProvider(o)
		return 0, plainJSON(v)
	}
	return simpleState(v), nil
}

// vanilla: BlockStateProviderTypes.bootstrap
func checkReplaceProvider(o obj) {
	kind := stateProviderType(o)
	switch kind {
	case "minecraft:simple", "minecraft:rotated_block_provider", "minecraft:weighted", "minecraft:noise",
		"minecraft:noise_threshold", "minecraft:dual_noise":
	case "minecraft:randomized_int", "minecraft:randomized_int_state_provider":
		if source, ok := required(o, "source").(obj); ok && source["type"] != nil {
			checkReplaceProvider(source)
		}
	case "minecraft:weighted_state_provider", "minecraft:noise_provider",
		"minecraft:noise_threshold_provider", "minecraft:dual_noise_provider":
		panic(lootFailure{fatal: fmt.Errorf("%w %q", ErrRemovedBlockStateProvider, kind)})
	default:
		unsupported("a block state provider of type %s", kind)
	}
}

// plainJSON copies a decoded tree with its numbers as float64, the form the
// world's provider compiler reads.
func plainJSON(v any) any {
	switch x := v.(type) {
	case obj:
		out := make(obj, len(x))
		for k, e := range x {
			out[k] = plainJSON(e)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = plainJSON(e)
		}
		return out
	case json.Number:
		f, _ := x.Float64()
		return f
	}
	return v
}

func replaceEffect(e obj, disk bool) *registry.EnchReplace {
	known := []string{"type", "offset", "predicate", "block_state", "trigger_game_event"}
	if disk {
		known = append(known, "radius", "height")
	}
	unknownKeys(e, "a replace effect", known...)
	out := &registry.EnchReplace{Offset: intVec(e, "offset"), Predicate: optPredicate(e), GameEvent: gameEventOf(e)}
	out.State, out.Provider = stateProvider(required(e, "block_state"))
	if disk {
		out.Radius, out.Height = levelValueOf(required(e, "radius")), levelValueOf(required(e, "height"))
	}
	return out
}

func setPropertiesEffect(e obj) *registry.EnchSetProperties {
	unknownKeys(e, "a set_block_properties effect", "type", "offset", "properties", "trigger_game_event")
	out := &registry.EnchSetProperties{Offset: intVec(e, "offset"), GameEvent: gameEventOf(e)}
	for k, v := range object(required(e, "properties"), "block properties") {
		value := str(v, "a property value")
		if p, ok := registry.PropNamed(k); ok {
			out.Props = append(out.Props, registry.PropValue{P: p, V: value})
		}
	}
	return out
}

func floatRange(v any) registry.EnchFloatRange {
	if v == nil {
		return registry.EnchFloatRange{}
	}
	if f, ok := num(v); ok {
		return registry.EnchFloatRange{Min: float32(f), Max: float32(f)}
	}
	o := object(v, "a float provider")
	if registry.Qualify(requireStr(o, "type")) != "minecraft:uniform" {
		unsupported("a float provider other than a constant or uniform")
	}
	return registry.EnchFloatRange{Min: float32(requireNum(o, "min_inclusive")), Max: float32(requireNum(o, "max_exclusive"))}
}

func particleAxis(pos, vel obj) registry.EnchParticleAxis {
	unknownKeys(pos, "a position source", "type", "offset", "scale")
	unknownKeys(vel, "a velocity source", "movement_scale", "base")
	out := registry.EnchParticleAxis{Offset: float32(optNum(pos, "offset", 0)), Scale: float32(optNum(pos, "scale", 1)),
		Movement: float32(optNum(vel, "movement_scale", 0)), Base: floatRange(vel["base"])}
	switch requireStr(pos, "type") {
	case "entity_position":
		if out.Scale != 1 {
			unsupported("Cannot scale an entity position coordinate source")
		}
	case "in_bounding_box":
		out.InBox = true
	default:
		unsupported("position source %s", requireStr(pos, "type"))
	}
	return out
}

// particleOf reads a particle given as a name or as an object with its
// options, and returns its ID and encoded options.
// vanilla: ParticleTypes.CODEC
func particleOf(v any) (id int32, options []byte) {
	var tag nbt.Tag
	switch p := v.(type) {
	case string:
		tag = nbt.Compound{"type": nbt.String(p)}
	case obj:
		var err error
		if tag, err = protocol.JSONValueTag(p); err != nil {
			fail("particle: %v", err)
		}
	default:
		fail("a particle that is neither a name nor an object")
	}
	id, options, ok := registry.ParticleOptions(tag)
	if !ok {
		unsupported("particle %v is not supported by this server", v)
	}
	return id, options
}

func particlesEffect(e obj) *registry.EnchParticles {
	unknownKeys(e, "a spawn_particles effect", "type", "particle", "horizontal_position", "vertical_position",
		"horizontal_velocity", "vertical_velocity", "speed")
	id, options := particleOf(required(e, "particle"))
	return &registry.EnchParticles{
		Particle:   id,
		Options:    options,
		Horizontal: particleAxis(object(required(e, "horizontal_position"), "a position source"), object(required(e, "horizontal_velocity"), "a velocity source")),
		Vertical:   particleAxis(object(required(e, "vertical_position"), "a position source"), object(required(e, "vertical_velocity"), "a velocity source")),
		Speed:      floatRange(e["speed"]),
	}
}

func summonEffect(e obj) (bolt string, summon *registry.EnchSummon) {
	unknownKeys(e, "a summon entity effect", "type", "entity", "join_team")
	joinTeam := flag(e, "join_team", false)
	var names []string
	for _, x := range holderList(required(e, "entity")) {
		name := registry.Qualify(str(x, "an entity type"))
		if strings.HasPrefix(name, "#") || !entityKnown(name) {
			unsupported("summon_entity for %s", name)
		}
		names = append(names, name)
	}
	if len(names) == 1 && names[0] == "minecraft:lightning_bolt" {
		return names[0], nil
	}
	return "", &registry.EnchSummon{Entities: names, JoinTeam: joinTeam}
}

func entityKnown(name string) bool {
	_, ok := registry.EntityTypeByName(name)
	return ok
}
