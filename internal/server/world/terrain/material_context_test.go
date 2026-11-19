package terrain

import ()

type countedMaterialNoise struct {
	Noise
	calls int
}

func (n *countedMaterialNoise) Get3D(x, y, z float64) float32 {
	n.calls++
	return float32(x + y + z)
}

type countedMaterialBiomes struct {
	goldenBiomeSource
	biome, calls int
}

func (b *countedMaterialBiomes) At(_, _, _ int32) int {
	b.calls++
	return b.biome
}

type extensionMaterialNoise struct{ Noise }

func (*extensionMaterialNoise) Get3D(_, _, _ float64) float32 { return 1 }
