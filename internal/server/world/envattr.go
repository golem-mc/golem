package world

import (
	"math"

	"github.com/golem-mc/golem/registry"
)

// envIndex selects the environment layers read by gameplay ticks.
type envIndex uint8

const (
	envMonstersBurn envIndex = iota
	envTurtleEggHatchChance
	envCreakingActive
	envBeesStayInHive
	envCatGiftChance
	envEyeblossomOpen
	envCloudHeight
	envSunAngle
	envFastLava
	envWaterEvaporates
	envRespawnAnchorWorks
	envPiglinsZombify
	envSnowGolemMelts
	envCanStartRaid
	envCount
)

var tickEnvironmentNames = [envCount]string{
	envMonstersBurn:         "minecraft:gameplay/monsters_burn",
	envTurtleEggHatchChance: "minecraft:gameplay/turtle_egg_hatch_chance",
	envCreakingActive:       "minecraft:gameplay/creaking_active",
	envBeesStayInHive:       "minecraft:gameplay/bees_stay_in_hive",
	envCatGiftChance:        "minecraft:gameplay/cat_waking_up_gift_chance",
	envEyeblossomOpen:       "minecraft:gameplay/eyeblossom_open",
	envCloudHeight:          "minecraft:visual/cloud_height",
	envSunAngle:             "minecraft:visual/sun_angle",
	envFastLava:             "minecraft:gameplay/fast_lava",
	envWaterEvaporates:      "minecraft:gameplay/water_evaporates",
	envRespawnAnchorWorks:   "minecraft:gameplay/respawn_anchor_works",
	envPiglinsZombify:       "minecraft:gameplay/piglins_zombify",
	envSnowGolemMelts:       "minecraft:gameplay/snow_golem_melts",
	envCanStartRaid:         "minecraft:gameplay/can_start_raid",
}

// LoadEnvironment resolves pack layers once, after registries and tags load.
func (w *World) LoadEnvironment() error {
	d, err := registry.DecodeEnvDimension(w.dim.typ)
	if err != nil {
		return err
	}
	w.setEnvironment(d)
	return nil
}

func (w *World) setEnvironment(d registry.EnvDimension) {
	w.environment = make(map[string]envLayers)
	for _, name := range registry.EnvAttributeNames() {
		l, ok := envLayersFor(d, name)
		if ok {
			w.environment[name] = l
		}
	}
	w.skyLight = w.environment["minecraft:gameplay/sky_light_level"]
	for i, name := range tickEnvironmentNames {
		w.tickEnvironment[i] = w.environment[name]
	}
}

func (w *World) envLayers(attribute string) (envLayers, bool) {
	if w.environment != nil {
		l, ok := w.environment[attribute]
		return l, ok
	}
	// A world without resolved pack layers reads the generated defaults.
	return envLayersFor(registry.EnvDimensionDefaults(w.dim.typ), attribute)
}

// environmentGlobal reads an attribute with no biome contribution.
func (w *World) environmentGlobal(attribute envIndex) float32 {
	return w.envValue(w.tickEnvLayers(attribute), "")
}

func (w *World) tickEnvLayers(attribute envIndex) *envLayers {
	if w.environment == nil {
		l, _ := w.envLayers(tickEnvironmentNames[attribute])
		return &l
	}
	return &w.tickEnvironment[attribute]
}

// The alphas of WeatherAttributes' FloatWithAlpha targets.
const (
	weatherRainAlpha    = 0.3125
	weatherThunderAlpha = 0.52734375
)

// weatherFloat is one float attribute's rain and thunder entries.
type weatherFloat struct{ rain, thunder registry.EnvFloatEntry }

// weatherFloats are the float attributes weather changes, by their
// FloatWithAlpha or set value; they live in code, not the data pack.
// vanilla: WeatherAttributes.RAIN, WeatherAttributes.THUNDER
var weatherFloats = map[string]weatherFloat{
	"minecraft:gameplay/bees_stay_in_hive": {
		rain:    registry.EnvFloatEntry{Op: registry.EnvOr, Value: 1, Alpha: 1},
		thunder: registry.EnvFloatEntry{Op: registry.EnvOr, Value: 1, Alpha: 1},
	},
	"minecraft:gameplay/sky_light_level": {
		rain:    registry.EnvFloatEntry{Op: registry.EnvAlphaBlend, Value: 4, Alpha: weatherRainAlpha},
		thunder: registry.EnvFloatEntry{Op: registry.EnvAlphaBlend, Value: 4, Alpha: weatherThunderAlpha},
	},
	"minecraft:visual/sky_light_factor": {
		rain:    registry.EnvFloatEntry{Op: registry.EnvAlphaBlend, Value: 0.24, Alpha: weatherRainAlpha},
		thunder: registry.EnvFloatEntry{Op: registry.EnvAlphaBlend, Value: 0.24, Alpha: weatherThunderAlpha},
	},
	"minecraft:visual/star_brightness": {
		rain:    registry.EnvFloatEntry{Op: registry.EnvOverride, Value: 0, Alpha: 1},
		thunder: registry.EnvFloatEntry{Op: registry.EnvOverride, Value: 0, Alpha: 1},
	},
}

// EnvironmentFloat is a float-convertible environment attribute's value at a
// position, a loot context's read: the default, then the dimension type's
// entry, the biome's (positional attributes only, by the 4x4x4 cell and
// none in an unloaded chunk), each timeline's track at its clock and the
// weather, clamped into the attribute's range. An attribute that is not
// float-convertible reads 0.
// vanilla: EnvironmentAttributeReader.getValue(LootContext, EnvironmentAttribute),
// EnvironmentAttributeSystem.addDefaultLayers
func (w *World) EnvironmentFloat(attribute string, x, y, z float64) float32 {
	l, ok := w.envLayers(attribute)
	if !ok {
		return 0
	}
	return w.environmentFloat(&l, x, y, z)
}

func (w *World) environmentFloat(l *envLayers, x, y, z float64) float32 {
	if !l.attr.Float {
		return 0
	}
	biome := ""
	if l.hasBiome {
		biome = w.NoiseBiome(int32(math.Floor(x)), int32(math.Floor(y)), int32(math.Floor(z))).Name()
	}
	return w.envValue(l, biome)
}

// envLayers is a supported attribute's layers in one dimension type, looked up
// once: the type's entry, the timeline tracks and the weather's entries.
type envLayers struct {
	name     string
	attr     registry.EnvAttribute
	dim      registry.EnvFloatEntry
	hasDim   bool
	hasBiome bool
	tracks   []envTrack
	weather  weatherFloat
	// hasWeather is whether weather moves the attribute at all; a world that
	// cannot have weather skips the layer too.
	hasWeather bool
}

// envTrack is a timeline's track of one attribute and the clock it reads.
type envTrack struct {
	clock  string
	period int32
	track  registry.EnvFloatTrack
}

// SetTestTimelines installs a game test's layers over the defaults and returns
// a teardown restoring the previous layers, including an enclosing environment.
// vanilla: TestEnvironmentDefinition.Timelines.setup, teardown
func (w *World) SetTestTimelines(timelines []registry.EnvTimeline) func() {
	previous := w.testTimelines.Swap(&timelines)
	return func() { w.testTimelines.Store(previous) }
}

// envLayersFor collects one supported attribute's dimension and dynamic layers.
func envLayersFor(d registry.EnvDimension, attribute string) (l envLayers, ok bool) {
	l.attr, ok = registry.EnvAttributeByName(attribute)
	if !ok || !l.attr.Float && !l.attr.Discrete {
		return l, false
	}
	l.name = attribute
	l.hasBiome = l.attr.Positional && registry.EnvBiomeHasFloat(attribute)
	l.dim, l.hasDim = d.Attributes[attribute]
	for _, tl := range d.Timelines {
		if track, ok := tl.Tracks[attribute]; ok {
			l.tracks = append(l.tracks, envTrack{tl.Clock, tl.Period, track})
		}
	}
	l.weather, l.hasWeather = weatherFloats[attribute]
	return l, true
}

// envValue layers the attribute over its default: the dimension type's entry,
// the biome's (none for ""), each timeline and the weather, then clamps.
// vanilla: EnvironmentAttributeSystem.addDefaultLayers
func (w *World) envValue(l *envLayers, biome string) float32 {
	v := l.attr.Default
	if l.hasDim {
		v = l.dim.Apply(v)
	}
	if biome != "" {
		if e, ok := registry.EnvBiomeFloat(biome, l.name); ok {
			v = e.Apply(v)
		}
	}
	timelines := w.testTimelines.Load()
	if len(l.tracks) == 0 && !l.hasWeather && timelines == nil {
		return l.attr.Sanitize(v)
	}
	// Other dimensions read the overworld's clocks, and its timeMu guards
	// every world's sky levels too, so one lock covers both.
	t := w.timeWorld()
	t.timeMu.Lock()
	defer t.timeMu.Unlock()
	for _, tr := range l.tracks {
		if c := t.clock(tr.clock); c != nil {
			v = tr.track.Sample(c.total, tr.period).Apply(v)
		}
	}
	if l.hasWeather && w.CanHaveWeather() {
		thunder := w.sky.thunderLevel()
		rain := w.sky.rainLevel() - thunder
		// vanilla: WeatherAttributes.addLayer
		if rain > 0 {
			v = l.blend(rain, v, l.weather.rain.Apply(v))
		}
		if thunder > 0 {
			v = l.blend(thunder, v, l.weather.thunder.Apply(v))
		}
	}
	// Test timelines follow all default layers, including weather.
	if timelines != nil {
		for _, tl := range *timelines {
			if track, ok := tl.Tracks[l.name]; ok {
				if c := t.clock(tl.Clock); c != nil {
					v = track.Sample(c.total, tl.Period).Apply(v)
				}
			}
		}
	}
	return l.attr.Sanitize(v)
}

// InClouds reports a box of the height given, standing at the position,
// within the cloud layer. Only dimension types with a cloud height (the
// overworld's) have clouds.
// vanilla: Entity.isInClouds
func (w *World) InClouds(x, y, z, height float64) bool {
	l := w.tickEnvLayers(envCloudHeight)
	if !l.hasDim {
		return false
	}
	bottom := float64(w.environmentFloat(l, x, y, z))
	return y+height >= bottom && y <= bottom+4
}

// blend steps discrete attributes as soon as a weather layer has any weight.
// vanilla: LerpFunction.ofConstant
func (l *envLayers) blend(alpha, from, to float32) float32 {
	if l.attr.Discrete {
		return to
	}
	return registry.EnvLerp(alpha, from, to)
}
