package terrain

import (
	"fmt"
	"testing"
)

var goldenDimensions = map[string]Dimension{
	"overworld": Overworld, "large_biomes": OverworldLarge, "amplified": OverworldAmplified, "nether": Nether, "end": End,
}

// goldenStates caches the random states a golden file names.
type goldenStates map[string]*RandomState

func (m goldenStates) get(t *testing.T, dim string, seed int64) *RandomState {
	key := fmt.Sprintf("%s/%d", dim, seed)
	if rs, ok := m[key]; ok {
		return rs
	}
	d, ok := goldenDimensions[dim]
	if !ok {
		t.Fatalf("unknown dimension %q", dim)
	}
	rs := NewRandomState(d, seed)
	m[key] = rs
	return rs
}

func routerField(r Router, name string) Sampler {
	switch name {
	case "temperature":
		return r.Temperature
	case "vegetation":
		return r.Vegetation
	case "continents":
		return r.Continents
	case "erosion":
		return r.Erosion
	case "depth":
		return r.Depth
	case "ridges":
		return r.Ridges
	case "chunkSurfaceLevel":
		return r.ChunkSurfaceLevel
	case "finalDensity":
		return r.FinalDensity
	}
	return nil
}

func aquiferField(a *AquiferConfig, name string) Sampler {
	switch name {
	case "barrier":
		return a.Barrier
	case "floodedness":
		return a.FluidLevelFloodedness
	case "spread":
		return a.FluidLevelSpread
	case "lava":
		return a.Lava
	case "exclusion":
		return a.Exclusion
	case "surfaceLevel":
		return a.SurfaceLevel
	}
	return nil
}

func TestGoldenRouter(t *testing.T) {
	states := goldenStates{}
	for _, r := range goldenOf(t, "router") {
		rs := states.get(t, r.fields[0], r.int(t, 1))
		s := routerField(rs.Router, r.fields[2])
		c := NewContext(nil)
		checkBits(t, r, s.Sample(c, int32(r.int(t, 3)), int32(r.int(t, 4)), int32(r.int(t, 5))))
	}
	for _, r := range goldenOf(t, "aquiferfn") {
		rs := states.get(t, r.fields[0], r.int(t, 1))
		s := aquiferField(rs.Router.Aquifer, r.fields[2])
		c := NewContext(nil)
		checkBits(t, r, s.Sample(c, int32(r.int(t, 3)), int32(r.int(t, 4)), int32(r.int(t, 5))))
	}
}
