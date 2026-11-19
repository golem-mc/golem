package datapack

import (
	"errors"
	"maps"
	"slices"
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// TestEnvironment is a test environment definition with every holder it
// names resolved: what a batch sets up before its tests and tears down after.
// vanilla: TestEnvironmentDefinition
type TestEnvironment struct {
	// Type is the definition's type, e.g. "minecraft:game_rules".
	Type string
	// AllOf are an all_of's definitions, set up in order.
	AllOf []TestEnvironment
	// Clock and Time are a clock_time's world clock and its total ticks.
	Clock string
	Time  int64
	// Difficulty is a difficulty's, 0 peaceful to 3 hard.
	Difficulty uint8
	// Setup and Teardown are a function's function ids, "" for none.
	Setup, Teardown string
	// Rules are a game_rules' rules, sorted by id.
	Rules []TestRule
	// Weather is a weather's: 0 clear, 1 rain, 2 thunder.
	Weather int
	// Timelines are a timeline_attributes' resolved float tracks, in layer order.
	Timelines []registry.EnvTimeline
}

// TestRule is one rule a game_rules environment sets: its plain id (as
// world.GameRuleTable keys it) and value, 0 or 1 for a boolean rule.
type TestRule struct {
	ID    string
	Value int32
}

// testEnvironmentTypes are the definition types in registration order.
// vanilla: TestEnvironmentDefinition.bootstrap
var testEnvironmentTypes = []string{"minecraft:all_of", "minecraft:clock_time", "minecraft:difficulty",
	"minecraft:function", "minecraft:game_rules", "minecraft:timeline_attributes", "minecraft:weather"}

// testWeathers are the weather codec's names in enum order.
var testWeathers = []string{"clear", "rain", "thunder"}

// testDifficulties are the difficulty codec's names in id order.
var testDifficulties = []string{"peaceful", "easy", "normal", "hard"}

// TestEnvironments resolves what a test environment holder may name.
type TestEnvironments struct {
	// Entry returns the test_environment registry's entry id.
	Entry func(id string) (nbt.Tag, bool)
	// Rule reports a game rule's kind and range by its plain id; ok is
	// false for a rule that does not exist.
	Rule func(id string) (isBool bool, min, max int32, ok bool)
}

// maxEnvironmentDepth bounds nested holders, so entries naming each other
// fail rather than recurse forever.
const maxEnvironmentDepth = 64

// DecodeTestEnvironment reads a test environment holder, an entry's id or
// an inline definition, failing where vanilla's codec does.
// vanilla: TestEnvironmentDefinition.CODEC
func DecodeTestEnvironment(tag nbt.Tag, envs TestEnvironments) (TestEnvironment, error) {
	return decodeEnvironmentHolder(tag, envs, 0)
}

func decodeEnvironmentHolder(tag nbt.Tag, envs TestEnvironments, depth int) (TestEnvironment, error) {
	if depth > maxEnvironmentDepth {
		return TestEnvironment{}, errors.New("test environments nest too deep")
	}
	tag = nbt.ListElement(tag)
	if s, ok := tag.(nbt.String); ok {
		if !protocol.ValidIdentifier(string(s)) {
			return TestEnvironment{}, errors.New("environment is no identifier")
		}
		id := registry.Qualify(string(s))
		entry, ok := envs.Entry(id)
		if !ok {
			return TestEnvironment{}, errors.New("unknown test environment " + id)
		}
		tag = entry
	}
	c, ok := tag.(nbt.Compound)
	if !ok {
		return TestEnvironment{}, errors.New("environment is not a map")
	}
	return decodeEnvironment(c, envs, depth)
}

// decodeEnvironment reads one definition.
// vanilla: TestEnvironmentDefinition.DIRECT_CODEC
func decodeEnvironment(c nbt.Compound, envs TestEnvironments, depth int) (TestEnvironment, error) {
	var env TestEnvironment
	typ, ok := c["type"].(nbt.String)
	if !ok || !protocol.ValidIdentifier(string(typ)) {
		return env, errors.New("environment has no type")
	}
	env.Type = registry.Qualify(string(typ))
	if !slices.Contains(testEnvironmentTypes, env.Type) {
		return env, errors.New("unknown test environment type " + env.Type)
	}
	switch env.Type {
	case "minecraft:all_of":
		list, ok := c["definitions"].(nbt.List)
		if !ok {
			return env, errors.New("all_of has no definitions")
		}
		env.AllOf = make([]TestEnvironment, 0, len(list.Items))
		for _, item := range list.Items {
			def, err := decodeEnvironmentHolder(item, envs, depth+1)
			if err != nil {
				return env, err
			}
			env.AllOf = append(env.AllOf, def)
		}
	case "minecraft:clock_time":
		clock, ok := c["clock"].(nbt.String)
		if !ok || !protocol.ValidIdentifier(string(clock)) {
			return env, errors.New("clock_time has no clock")
		}
		env.Clock = registry.Qualify(string(clock))
		if r, _ := registry.Get("minecraft:world_clock"); r.ID(env.Clock) < 0 {
			return env, errors.New("unknown world clock " + env.Clock)
		}
		// vanilla: ExtraCodecs.NON_NEGATIVE_INT
		v, ok := nbt.IntValue(c["time"])
		if !ok || v < 0 {
			return env, errors.New("clock_time's time is out of range")
		}
		env.Time = int64(v)
	case "minecraft:difficulty":
		name, _ := c["difficulty"].(nbt.String)
		d := slices.Index(testDifficulties, string(name))
		if d < 0 {
			return env, errors.New("unknown difficulty " + string(name))
		}
		env.Difficulty = uint8(d)
	case "minecraft:function":
		for key, into := range map[string]*string{"setup": &env.Setup, "teardown": &env.Teardown} {
			t, has := c[key]
			if !has {
				continue
			}
			s, ok := t.(nbt.String)
			if !ok || !protocol.ValidIdentifier(string(s)) {
				return env, errors.New(key + " is no identifier")
			}
			*into = registry.Qualify(string(s))
		}
	case "minecraft:game_rules":
		rules, ok := c["rules"].(nbt.Compound)
		if !ok {
			return env, errors.New("game_rules has no rules")
		}
		var err error
		if env.Rules, err = decodeTestRules(rules, envs); err != nil {
			return env, err
		}
	case "minecraft:timeline_attributes":
		list, ok := c["timelines"].(nbt.List)
		if !ok {
			return env, errors.New("timeline_attributes has no timelines")
		}
		for _, item := range list.Items {
			tl, err := decodeTestTimeline(nbt.ListElement(item))
			if err != nil {
				return env, err
			}
			env.Timelines = append(env.Timelines, tl)
		}
	case "minecraft:weather":
		name, _ := c["weather"].(nbt.String)
		if env.Weather = slices.Index(testWeathers, string(name)); env.Weather < 0 {
			return env, errors.New("unknown weather " + string(name))
		}
	}
	return env, nil
}

// decodeTestRules reads a game rule map: each key a game rule's id, each
// value read by the rule's own codec.
// vanilla: GameRuleMap.CODEC, GameRules.register (Codec.BOOL, Codec.intRange)
func decodeTestRules(rules nbt.Compound, envs TestEnvironments) ([]TestRule, error) {
	out := make([]TestRule, 0, len(rules))
	for _, key := range slices.Sorted(maps.Keys(rules)) {
		if !protocol.ValidIdentifier(key) {
			return nil, errors.New("game rule " + key + " is no identifier")
		}
		id, vanilla := strings.CutPrefix(registry.Qualify(key), "minecraft:")
		isBool, lo, hi, ok := envs.Rule(id)
		if !vanilla || !ok {
			return nil, errors.New("unknown game rule " + key)
		}
		f, ok := nbt.NumberValue(rules[key])
		v, _ := nbt.IntValue(rules[key])
		switch {
		case !ok:
			return nil, errors.New("game rule " + key + " is no number")
		case isBool:
			// vanilla: NbtOps.getBooleanValue (doubleValue != 0)
			v = 0
			if f != 0 {
				v = 1
			}
		case v < lo || v > hi:
			return nil, errors.New("game rule " + key + " is out of range")
		}
		out = append(out, TestRule{ID: id, Value: v})
	}
	return out, nil
}
