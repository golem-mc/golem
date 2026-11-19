package terrain

import (
	"math"
	"strconv"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
)

// normalInputFactor offsets the second field's sample point.
// vanilla: NormalNoise.INPUT_FACTOR
const normalInputFactor = 1.0181268882175227

// perlinDeviation is vanilla: PerlinNoise.STANDARD_DEVIATION.
const perlinDeviation = 0.2702247831245211

// NoiseParameters is a worldgen/noise entry. vanilla: NormalNoise.Parameters
type NoiseParameters struct {
	baseAmplitude float64
	baseOctave    int
	// octaveCount is 1 when unset, as in the data pack codec.
	octaveCount        int
	amplitudeModifiers []float64
	normalization      NoiseNormalization
}

// NoiseNormalization selects NormalNoise's amplitude normalization.
type NoiseNormalization uint8

const (
	NoiseNormalized NoiseNormalization = iota
	NoiseUnnormalized
	NoiseLegacyNormalization
)

// NewConfiguredNormalNoise builds the noise described by NormalNoise.DIRECT_CODEC.
func NewConfiguredNormalNoise(baseOctave int, baseAmplitude float64, octaveCount int, modifiers []float64, normalization NoiseNormalization) *NormalNoise {
	return NewNormalNoise(NoiseParameters{baseOctave: baseOctave, baseAmplitude: baseAmplitude, octaveCount: octaveCount, amplitudeModifiers: modifiers, normalization: normalization})
}

// octaveInfo is one octave of a NormalNoise. vanilla: NormalNoise.OctaveInfo
type octaveInfo struct {
	index     int
	frequency float64
	amplitude float64
}

// NormalNoise is the sum of two octave stacks, normalized to a target
// deviation. vanilla: NormalNoise
type NormalNoise struct {
	params              NoiseParameters
	octaves             []octaveInfo
	normalizationFactor float64
}

// NewNormalNoise is vanilla: NormalNoise.<init>.
func NewNormalNoise(p NoiseParameters) *NormalNoise {
	if p.octaveCount == 0 {
		p.octaveCount = 1
	}
	if p.baseAmplitude == 0 {
		p.baseAmplitude = 1
	}
	n := &NormalNoise{params: p}
	n.octaves = buildOctavesNormalized(p.baseOctave, p.baseAmplitude, p.octaveCount, p.amplitudeModifiers, p.normalization != NoiseUnnormalized)
	target := 0.0
	for _, o := range n.octaves {
		target += math.Abs(o.amplitude)
	}
	n.normalizationFactor = computeNormalizationFactor(target, n.octaves)
	if p.normalization == NoiseLegacyNormalization && n.normalizationFactor != 0 {
		n.normalizationFactor = parityNormalizationFactor(p.baseAmplitude, p.octaveCount, p.amplitudeModifiers)
	}
	return n
}

// NewParityNormalNoise builds a noise whose field matches the one before
// the normalization changed. vanilla: NormalNoise.createParity
func NewParityNormalNoise(firstOctave int, amplitudes ...float64) *NormalNoise {
	if len(amplitudes) == 0 {
		panic("terrain: a parity noise needs at least one amplitude")
	}
	return NewNormalNoise(NoiseParameters{
		baseAmplitude:      parityBaseAmplitude(firstOctave, amplitudes),
		baseOctave:         firstOctave,
		octaveCount:        len(amplitudes),
		amplitudeModifiers: amplitudes,
	})
}

// parityBaseAmplitude is vanilla: NormalNoise.computeParityBaseAmplitude.
func parityBaseAmplitude(baseOctave int, amplitudes []float64) float64 {
	octaves := buildOctaves(baseOctave, 1.0, len(amplitudes), amplitudes)
	target := 0.0
	for _, o := range octaves {
		target += math.Abs(o.amplitude)
	}
	factor := computeNormalizationFactor(target, octaves)
	if factor == 0.0 {
		return 1.0
	}
	return parityNormalizationFactor(1.0, len(amplitudes), amplitudes) / factor
}

// parityNormalizationFactor is vanilla: NormalNoise.computeParityNormalizationFactor.
func parityNormalizationFactor(baseAmplitude float64, octaveCount int, mods []float64) float64 {
	minOctave, maxOctave := math.MaxInt32, math.MinInt32
	for i := range octaveCount {
		if amplitudeModifier(mods, i) != 0.0 {
			minOctave, maxOctave = min(minOctave, i), max(maxOctave, i)
		}
	}
	span := float64(maxOctave - minOctave)
	return baseAmplitude * 0.5 * 0.3333333333333333 / (0.1 * (1.0 + 1.0/(span+1.0)))
}

// NoiseByName builds the registered noise of that name. It panics on an
// unknown name.
func NoiseByName(name string) *NormalNoise {
	p, ok := noiseParameters[name]
	if !ok {
		panic("terrain: unknown noise " + strconv.Quote(name))
	}
	return NewNormalNoise(p)
}

func amplitudeModifier(mods []float64, i int) float64 {
	if len(mods) == 0 {
		return 1.0
	}
	return mods[i]
}

// buildOctaves builds normalized octaves.
func buildOctaves(baseOctave int, baseAmplitude float64, octaveCount int, mods []float64) []octaveInfo {
	return buildOctavesNormalized(baseOctave, baseAmplitude, octaveCount, mods, true)
}

// vanilla: NormalNoise.buildOctaves
func buildOctavesNormalized(baseOctave int, baseAmplitude float64, octaveCount int, mods []float64, normalize bool) []octaveInfo {
	frequency := math.Pow(2.0, float64(baseOctave))
	amplitude := baseAmplitude
	if normalize {
		amplitude *= math.Pow(0.5, -float64(octaveCount-1)) / (math.Pow(0.5, -float64(octaveCount)) - 1.0)
	}
	octaves := make([]octaveInfo, 0, octaveCount)
	for i := range octaveCount {
		if m := amplitudeModifier(mods, i); m != 0.0 {
			octaves = append(octaves, octaveInfo{baseOctave + i, frequency, amplitude * m})
		}
		frequency *= 2.0
		amplitude *= 0.5
	}
	return octaves
}

// computeNormalizationFactor is vanilla: NormalNoise.computeNormalizationFactor.
func computeNormalizationFactor(targetAmplitude float64, octaves []octaveInfo) float64 {
	variance := 0.0
	for _, o := range octaves {
		d := perlinDeviation * math.Abs(o.amplitude)
		variance += d * d
	}
	inputDeviation := math.Sqrt(variance)
	if inputDeviation == 0.0 {
		return 0.0
	}
	inputSumDeviation := inputDeviation * math.Sqrt(2.0)
	return targetAmplitude * 0.3333333333333333 / inputSumDeviation
}

func (o octaveInfo) seed() string { return "octave_" + strconv.Itoa(o.index) }

// Create draws the octaves from two forks of the random source.
// vanilla: NormalNoise.create
func (n *NormalNoise) Create(r levelgen.RandomSource) *NoiseStack {
	first := r.ForkPositional()
	second := r.ForkPositional()
	var b NoiseStackBuilder
	for _, o := range n.octaves {
		firstNoise := NewPerlinNoise(first.FromHashOf(o.seed()))
		secondNoise := NewPerlinNoise(second.FromHashOf(o.seed()))
		valueFactor := float32(n.normalizationFactor * o.amplitude)
		b.Add(firstNoise, o.frequency, valueFactor)
		b.Add(secondNoise, o.frequency*normalInputFactor, valueFactor)
	}
	return b.Build()
}

// CreateForLegacyNetherBiome draws the octaves the way the nether biome
// noises always were. vanilla: NormalNoise.createForLegacyNetherBiome
func (n *NormalNoise) CreateForLegacyNetherBiome(r levelgen.RandomSource) *NoiseStack {
	amps := n.params.amplitudeModifiers
	if len(amps) == 0 {
		amps = make([]float64, n.params.octaveCount)
		for i := range amps {
			amps[i] = 1.0
		}
	}
	first := createForLegacyNetherBiome(r, n.params.baseOctave, amps)
	second := createForLegacyNetherBiome(r, n.params.baseOctave, amps)
	valueFactor := float32(n.normalizationFactor * n.params.baseAmplitude)
	var b NoiseStackBuilder
	return b.AddStack(first, 1.0, valueFactor).AddStack(second, normalInputFactor, valueFactor).Build()
}

// createForLegacyNetherBiome is vanilla: LegacyFbmInitializer.createForLegacyNetherBiome.
func createForLegacyNetherBiome(r levelgen.RandomSource, firstOctave int, amps []float64) *NoiseStack {
	octaves := len(amps)
	zero := -firstOctave
	levels := make([]*PerlinNoise, octaves)
	zeroOctave := NewPerlinNoise(r)
	if zero >= 0 && zero < octaves && amps[zero] != 0 {
		levels[zero] = zeroOctave
	}
	for i := zero - 1; i >= 0; i-- {
		if i < octaves && amps[i] != 0 {
			levels[i] = NewPerlinNoise(r)
			continue
		}
		r.ConsumeCount(262)
	}
	if zero < octaves-1 {
		panic("terrain: positive octaves are temporarily disabled")
	}
	factor := math.Pow(2.0, float64(-zero))
	valueFactor := math.Pow(2.0, float64(octaves-1)) / (math.Pow(2.0, float64(octaves)) - 1.0)
	var b NoiseStackBuilder
	for i, noise := range levels {
		if noise != nil {
			b.Add(noise, factor, float32(valueFactor*amps[i]))
		}
		factor *= 2.0
		valueFactor /= 2.0
	}
	return b.Build()
}
