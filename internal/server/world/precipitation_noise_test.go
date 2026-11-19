package world

import (
	"math"
	"testing"
)

// TestBiomeNoisesMatchThe263Jar pins the biome noises as raw bits read from
// the 26.3 jar: the temperature noise at (x/8, z/8) times 8 (float), the
// frozen noise times 7 and the biome info noise at 0.2 and 0.09 (doubles).
func TestBiomeNoisesMatchThe263Jar(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		x, z               int32
		height             int32
		large, edge, small uint64
	}{
		{0, 0, 0, 0, 0, 0},
		{16, 16, -1065389294, 4616721745175379968, 4604350260456718336, 13826031844891557888},
		{-7, 13, 1079165126, 4595883413184970752, 4591746363783380992, 4590108049045192704},
		{100, -250, -1062856993, 4600966908022358016, 13810522051728900096, 4591895668120879104},
		{37, 91, -1063078446, 13832355820773310464, 4594355231071928320, 13821881699597811712},
		{512, 512, -1070946290, 4606092534341435392, 4600043046061473792, 13824082956773228544},
		{-4096, 2048, 1080611741, 13834088640361267200, 13815215989811314688, 13820108157277437952},
		{16, -15, 928002150, 4600264570274054144, 13827141430901997568, 13827519474460262400},
		{8, 8, -1058762869, 4613176234640146432, 13824901488419274752, 4601798728834088960},
		{-1234, 999, 1063476559, 4613791825860231168, 4603070141159178240, 4599609923305734144},
		{3000, -77, -1076601848, 4610939205218992128, 13826681309444964352, 4605667175812825088},
	} {
		fx, fz := float64(tc.x), float64(tc.z)
		height := biomeTemperatureNoise.Get2D(float64(float32(tc.x)/8), float64(float32(tc.z)/8)) * 8
		large := float64(frozenTemperatureNoise.Get2D(fx*0.05, fz*0.05) * 7)
		edge := float64(biomeInfoNoise.Get2D(fx*0.2, fz*0.2))
		small := float64(biomeInfoNoise.Get2D(fx*0.09, fz*0.09))
		if got := int32(math.Float32bits(height)); got != tc.height {
			t.Errorf("height noise at %d,%d: bits %d, want %d", tc.x, tc.z, got, tc.height)
		}
		if got := math.Float64bits(large); got != tc.large {
			t.Errorf("frozen noise at %d,%d: bits %d, want %d", tc.x, tc.z, got, tc.large)
		}
		if got := math.Float64bits(edge); got != tc.edge {
			t.Errorf("info noise at 0.2 at %d,%d: bits %d, want %d", tc.x, tc.z, got, tc.edge)
		}
		if got := math.Float64bits(small); got != tc.small {
			t.Errorf("info noise at 0.09 at %d,%d: bits %d, want %d", tc.x, tc.z, got, tc.small)
		}
	}
}
