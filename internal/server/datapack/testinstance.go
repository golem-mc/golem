package datapack

import (
	"errors"
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// TestInstance is a minecraft:test_instance entry, the fields the test
// instance block reads.
// vanilla: GameTestInstance, TestData
type TestInstance struct {
	Type string // "minecraft:block_based" or "minecraft:function"
	// Function is a function test's test function; "" for a block-based one.
	Function string
	// Environment is the environment's id; "" for one written inline.
	Environment string
	// Env is the environment the test's batch runs in.
	Env       TestEnvironment
	Dimension string
	Structure string
	// Rotation indexes vanilla's Rotation order (none, clockwise_90, 180,
	// counterclockwise_90).
	Rotation int32
	Padding  int32 // 0 to 128
	// MaxTicks is the tick count after which a test with no result times
	// out, at least 1; SetupTicks are waited before it starts.
	MaxTicks, SetupTicks int32
	// MaxAttempts and RequiredSuccesses, both at least 1, make a test with
	// more than one attempt flaky: it runs again until it has passed
	// RequiredSuccesses times or can no longer.
	MaxAttempts, RequiredSuccesses int32
	// Required tells a failure that fails the run from an optional one.
	Required bool
	// SkyAccess leaves the barrier ceiling off the structure.
	SkyAccess bool
}

// testRotations are the Rotation codec's names in enum order.
var testRotations = []string{"none", "clockwise_90", "180", "counterclockwise_90"}

// DecodeTestInstance reads a test instance entry, resolving its environment
// through envs, failing where vanilla's codec does.
// vanilla: GameTestInstance.DIRECT_CODEC, TestData.CODEC
func DecodeTestInstance(tag nbt.Tag, envs TestEnvironments) (TestInstance, error) {
	c, ok := tag.(nbt.Compound)
	if !ok {
		return TestInstance{}, errors.New("not a map")
	}
	id := func(key string, required bool) (string, error) {
		t, has := c[key]
		if !has {
			if required {
				return "", errors.New("no key " + key)
			}
			return "", nil
		}
		s, ok := t.(nbt.String)
		if !ok || !protocol.ValidIdentifier(string(s)) {
			return "", errors.New(key + " is no identifier")
		}
		return registry.Qualify(string(s)), nil
	}
	intIn := func(key string, required bool, def, lo, hi float64) (int32, error) {
		t, has := c[key]
		if !has {
			if required {
				return 0, errors.New("no key " + key)
			}
			return int32(def), nil
		}
		v, ok := nbt.NumberValue(t)
		if !ok || v < lo || v > hi {
			return 0, errors.New(key + " is out of range")
		}
		return int32(v), nil
	}
	var ti TestInstance
	var err error
	if ti.Type, err = id("type", true); err != nil {
		return ti, err
	}
	switch ti.Type {
	case "minecraft:block_based":
	case "minecraft:function":
		if ti.Function, err = id("function", true); err != nil {
			return ti, err
		}
	default:
		return ti, errors.New("unknown test instance type " + ti.Type)
	}
	// The environment is a holder: an id names an entry, a map is inline.
	if _, inline := c["environment"].(nbt.Compound); !inline {
		if ti.Environment, err = id("environment", true); err != nil {
			return ti, err
		}
	}
	if ti.Env, err = DecodeTestEnvironment(c["environment"], envs); err != nil {
		return ti, err
	}
	if ti.Dimension, err = id("dimension", false); err != nil {
		return ti, err
	}
	if ti.Dimension == "" {
		ti.Dimension = "minecraft:overworld"
	}
	if ti.Structure, err = id("structure", true); err != nil {
		return ti, err
	}
	const maxInt = 1<<31 - 1
	for _, f := range []struct {
		key      string
		required bool
		lo       float64
		into     *int32
	}{{"max_ticks", true, 1, &ti.MaxTicks}, {"setup_ticks", false, 0, &ti.SetupTicks},
		{"max_attempts", false, 1, &ti.MaxAttempts}, {"required_successes", false, 1, &ti.RequiredSuccesses}} {
		if *f.into, err = intIn(f.key, f.required, f.lo, f.lo, maxInt); err != nil {
			return ti, err
		}
	}
	// Codec.BOOL reads any number through NbtOps, so only another type
	// fails; required defaults to true.
	ti.Required = true
	for _, key := range []string{"required", "manual_only", "sky_access"} {
		if t, has := c[key]; has {
			v, ok := nbt.NumberValue(t)
			if !ok {
				return ti, errors.New(key + " is no boolean")
			}
			// vanilla: NbtOps.getBooleanValue (byteValue)
			switch key {
			case "required":
				ti.Required = int8(int64(v)) != 0
			case "sky_access":
				ti.SkyAccess = int8(int64(v)) != 0
			}
		}
	}
	if ti.Padding, err = intIn("padding", false, 0, 0, 128); err != nil {
		return ti, err
	}
	if t, has := c["rotation"]; has {
		s, _ := t.(nbt.String)
		if ti.Rotation = int32(slices.Index(testRotations, string(s))); ti.Rotation < 0 {
			return ti, errors.New("unknown rotation " + nbt.Stringify(t))
		}
	}
	return ti, nil
}
