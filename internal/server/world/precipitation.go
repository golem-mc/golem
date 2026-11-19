package world

import (
	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/internal/server/world/terrain"
	"github.com/golem-mc/golem/registry"
)

// What the sky drops on a cell, vanilla's Biome.Precipitation.
type precipitation uint8

const (
	precipNone precipitation = iota
	precipRain
	precipSnow
)

// rainTemperatureFloor is Biome.warmEnoughToRain's 0.15; below it the sky
// snows.
const rainTemperatureFloor = 0.15

// precipitationAt is what the sky drops on the cell: none where the biome
// declares none (desert, savanna, badlands), snow where too cold at that
// height, rain otherwise.
// vanilla: Biome.getPrecipitationAt
func (w *World) precipitationAt(x, y, z int32) precipitation {
	return w.precipitationFrom(w.Biome(x, y, z).Climate(), x, y, z)
}

// precipitationFrom is precipitationAt over a biome already looked up, as
// tickPrecipitation does: the biome may come from another cell than the
// temperature.
// vanilla: Biome.getPrecipitationAt
func (w *World) precipitationFrom(climate registry.BiomeClimate, x, y, z int32) precipitation {
	if !climate.Precipitation {
		return precipNone
	}
	if !w.warmEnoughToRain(x, y, z, climate) {
		return precipSnow
	}
	return precipRain
}

// warmEnoughToRain reports the height-adjusted temperature at the cell is at
// least 0.15.
// vanilla: Biome.warmEnoughToRain
func (w *World) warmEnoughToRain(x, y, z int32, climate registry.BiomeClimate) bool {
	return w.temperatureAt(x, y, z, climate) >= rainTemperatureFloor
}

// temperatureAt is the modified base temperature, less a noisy correction
// growing with height above the snow line (sea level plus 17); at or below the
// line it stands.
// vanilla: Biome.getHeightAdjustedTemperature
func (w *World) temperatureAt(x, y, z int32, climate registry.BiomeClimate) float32 {
	return heightAdjustedTemperature(w.SeaLevel(), x, y, z, climate)
}

// heightAdjustedTemperature is temperatureAt for a sea level.
func heightAdjustedTemperature(seaLevel, x, y, z int32, climate registry.BiomeClimate) float32 {
	base := climate.Temperature
	if climate.Frozen {
		base = frozenTemperature(x, z, base)
	}
	snowLevel := seaLevel + 17
	if y <= snowLevel {
		return base
	}
	// Vanilla truncates y and noise*8 to float32 before the sum; the order is
	// load-bearing.
	noise := float32(biomeTemperatureNoise.Get2D(float64(float32(x)/8), float64(float32(z)/8)) * 8)
	return base - (noise+float32(y)-float32(snowLevel))*0.05/40
}

// biomeTemperatureNoise is vanilla's Biome.TEMPERATURE_NOISE.
var biomeTemperatureNoise = terrain.NewSimplexNoiseWithoutOffset(levelgen.NewLegacyRandom(1234))

// frozenTemperatureNoise and biomeInfoNoise are FROZEN_TEMPERATURE_NOISE and
// BIOME_INFO_NOISE, which Biome.TemperatureModifier.FROZEN reads.
var (
	frozenTemperatureNoise = func() *terrain.NoiseStack {
		r := levelgen.NewLegacyRandom(3456)
		var b terrain.NoiseStackBuilder
		b.Add(terrain.NewSimplexNoiseWithoutOffset(r), 1.0, 0.14285715)
		b.Add(terrain.NewSimplexNoiseWithoutOffset(r), 0.5, 0.2857143)
		b.Add(terrain.NewSimplexNoiseWithoutOffset(r), 0.25, 0.5714286)
		return b.Build()
	}()
	biomeInfoNoise = terrain.NewSimplexNoiseWithoutOffset(levelgen.NewLegacyRandom(2345))
)

// frozenTemperature reads 0.2 (warm enough for rain) where the frozen noise is
// cold enough, else the base temperature.
// vanilla: Biome.TemperatureModifier.FROZEN
func frozenTemperature(x, z int32, base float32) float32 {
	fx, fz := float64(x), float64(z)
	cold := float64(frozenTemperatureNoise.Get2D(fx*0.05, fz*0.05)*7) + float64(biomeInfoNoise.Get2D(fx*0.2, fz*0.2))
	if cold < 0.3 && float64(biomeInfoNoise.Get2D(fx*0.09, fz*0.09)) < 0.8 {
		return 0.2
	}
	return base
}

// legacyRandom is java.util.Random's 48-bit LCG. Vanilla seeds its simplex
// temperature noise and the end's spike shuffle from one, so both must match
// Java draw for draw.
type legacyRandom struct{ state int64 }

func newLegacyRandom(seed int64) *legacyRandom {
	r := &legacyRandom{}
	r.state = (seed ^ 0x5DEECE66D) & (1<<48 - 1)
	return r
}

// next returns the top bits of the next 48-bit draw.
func (r *legacyRandom) next(bits uint) int32 {
	r.state = (r.state*0x5DEECE66D + 0xB) & (1<<48 - 1)
	return int32(uint64(r.state) >> (48 - bits))
}

// nextLong is java.util.Random.nextLong's (next(32) << 32) + next(32).
func (r *legacyRandom) nextLong() int64 {
	return int64(r.next(32))<<32 + int64(r.next(32))
}

// nextInt is java.util.Random.nextInt(bound), rejection loop and all.
func (r *legacyRandom) nextInt(bound int32) int32 {
	if bound&(bound-1) == 0 {
		return int32((int64(bound) * int64(r.next(31))) >> 31)
	}
	for {
		bits := r.next(31)
		val := bits % bound
		if bits-val+(bound-1) >= 0 {
			return val
		}
	}
}
