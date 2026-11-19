package world

//go:generate go run ../../../tools/gen state-provider-golden

import (
	"strings"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/internal/server/world/terrain"
	"github.com/golem-mc/golem/registry"
)

// providerCompiler resolves the holders in BlockStateProvider.CODEC.
type providerCompiler struct{ lookup func(string) (string, bool) }

func (c providerCompiler) resolve(v any) any {
	lookup := c.lookup
	if lookup == nil {
		lookup = levelgen.OverworldStateProvider
	}
	name, ref := v.(string)
	if !ref {
		return v
	}
	name = registry.Qualify(name)
	doc, ok := lookup(name)
	if !ok {
		modelFail("unknown state provider %q", name)
	}
	return parseJSON(doc)
}

func stateOrExisting(opt optState) stateProvider {
	return func(r *decorRegion, p [3]int32) uint16 {
		if state, ok := opt(r, p); ok {
			return state
		}
		return r.get(p[0], p[1], p[2])
	}
}

// vanilla: RandomBlockProvider.getState
func (c providerCompiler) randomBlock(o jobj) optState {
	jonly(o, "blocks")
	names := jstrings(o["blocks"])
	var states []uint16
	if len(names) == 1 && strings.HasPrefix(names[0], "#") {
		// The draw indexes a tag in the data pack's order; the registry's
		// members are sorted, so a tag whose order differs has its own table.
		states = orderedTagStates[names[0][1:]]
		if states == nil {
			var ok bool
			if names, ok = registry.BlockTagMembers(names[0][1:]); !ok {
				modelFail("unknown block tag %q", o["blocks"])
			}
		}
	}
	if states == nil {
		states = make([]uint16, len(names))
		for i, name := range names {
			states[i] = compileBlockState(name)
		}
	}
	return func(r *decorRegion, _ [3]int32) (uint16, bool) {
		if len(states) == 0 {
			return 0, false
		}
		return states[r.rng.NextIntN(int32(len(states)))], true
	}
}

// vanilla: RotatedBlockProvider.getState
func (c providerCompiler) rotated(o jobj) stateProvider {
	jonly(o, "state", "direction")
	source := c.state(o["state"])
	direction, fixed := o["direction"].(string)
	directions := [...]string{"down", "up", "north", "south", "west", "east"}
	if fixed {
		valid := false
		for _, d := range directions {
			valid = valid || direction == d
		}
		if !valid {
			modelFail("unknown direction %q", direction)
		}
	}
	return func(r *decorRegion, p [3]int32) uint16 {
		d := direction
		if !fixed {
			d = directions[r.rng.NextIntN(6)]
		}
		axis := "y"
		switch d {
		case "north", "south":
			axis = "z"
		case "east", "west":
			axis = "x"
		}
		state := source(r, p)
		if s, ok := registry.WithProp(state, registry.PropAxis, axis); ok {
			state = s
		}
		if s, ok := registry.WithProp(state, registry.PropFacing, d); ok {
			state = s
		}
		return state
	}
}

// compileNativeNoise reads NormalNoise.DIRECT_CODEC.
func compileNativeNoise(o jobj) *terrain.NormalNoise {
	jonly(o, "base_amplitude", "base_octave", "octave_count", "normalize", "amplitude_modifiers")
	base, count := o.integer("base_octave"), o.integerOr("octave_count", 1)
	amplitude := 1.0
	if o.has("base_amplitude") {
		amplitude = o.num("base_amplitude")
	}
	mode := terrain.NoiseNormalized
	if o.has("normalize") {
		switch o["normalize"] {
		case true:
		case false:
			mode = terrain.NoiseUnnormalized
		case "legacy":
			mode = terrain.NoiseLegacyNormalization
		default:
			modelFail("invalid noise normalization %v", o["normalize"])
		}
	}
	var modifiers []float64
	if o.has("amplitude_modifiers") {
		for _, v := range o.arr("amplitude_modifiers") {
			m := v.(float64)
			if m < 0 || m > 1e6 {
				modelFail("invalid noise amplitude modifier %v", m)
			}
			modifiers = append(modifiers, m)
		}
	}
	if base < -32 || base > 32 || count < 1 || count > 32 || amplitude < float64(float32(1e-5)) || amplitude > 1e6 || (len(modifiers) != 0 && len(modifiers) != int(count)) {
		modelFail("invalid noise parameters %v", o)
	}
	return terrain.NewConfiguredNormalNoise(int(base), amplitude, int(count), modifiers, mode)
}
