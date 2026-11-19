package terrain

import (
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
)

// TestParityNormalNoiseMatches263 pins NormalNoise.createParity(...).create(
// WorldgenRandom(LegacyRandomSource(seed))).get as raw float bits read from
// the 26.3 jar: the noises geodes and the noise state providers use.
func TestParityNormalNoiseMatches263(t *testing.T) {
	t.Parallel()
	type point struct {
		x, y, z float64
		bits    int32
	}
	for _, tc := range []struct {
		firstOctave int
		amplitudes  string
		seed        int64
		points      []point
	}{
		{-4, "1", 12345, []point{
			{0.0, 0.0, 0.0, -1098447469},
			{3.0, 70.0, -5.0, 1052905340},
			{-120.0, 12.0, 45.0, 1050317426},
			{1000.0, -40.0, 777.0, -1135965004},
			{16.5, 64.0, -31.25, 1057558991},
		}},
		{0, "1", 2345, []point{
			{0.0, 0.0, 0.0, -1101599464},
			{3.0, 70.0, -5.0, -1084557438},
			{-120.0, 12.0, 45.0, -1098316299},
			{1000.0, -40.0, 777.0, -1105683880},
			{16.5, 64.0, -31.25, -1108204080},
		}},
		{-3, "1", 2345, []point{
			{0.0, 0.0, 0.0, 1052978301},
			{3.0, 70.0, -5.0, 1053156424},
			{-120.0, 12.0, 45.0, 1041288778},
			{1000.0, -40.0, 777.0, -1112935494},
			{16.5, 64.0, -31.25, -1093915370},
		}},
		{-10, "1", 7, []point{
			{0.0, 0.0, 0.0, -1105077262},
			{3.0, 70.0, -5.0, -1113180418},
			{-120.0, 12.0, 45.0, -1098822818},
			{1000.0, -40.0, 777.0, -1098332876},
			{16.5, 64.0, -31.25, -1118954788},
		}},
		{-4, "1,0.5,0,1", 99, []point{
			{0.0, 0.0, 0.0, -1102333528},
			{3.0, 70.0, -5.0, -1104703636},
			{-120.0, 12.0, 45.0, -1112193015},
			{1000.0, -40.0, 777.0, 1032459495},
			{16.5, 64.0, -31.25, 1052907881},
		}},
	} {
		var amps []float64
		for a := range strings.SplitSeq(tc.amplitudes, ",") {
			f, err := strconv.ParseFloat(a, 64)
			if err != nil {
				t.Fatal(err)
			}
			amps = append(amps, f)
		}
		noise := NewParityNormalNoise(tc.firstOctave, amps...).Create(levelgen.NewLegacyRandom(tc.seed))
		for _, p := range tc.points {
			if got := int32(math.Float32bits(noise.Get3D(p.x, p.y, p.z))); got != p.bits {
				t.Errorf("octave %d amplitudes %s seed %d at %v,%v,%v: bits %d, want %d", tc.firstOctave, tc.amplitudes, tc.seed, p.x, p.y, p.z, got, p.bits)
			}
		}
	}
}
