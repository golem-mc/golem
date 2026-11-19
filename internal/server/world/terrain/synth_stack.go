package terrain

// noiseLayer is one noise of a stack. vanilla: NoiseStack.Layer
type noiseLayer struct {
	noise     Noise
	frequency float64
	amplitude float32
}

// NoiseStack sums noises at their own frequency and amplitude. vanilla: NoiseStack
type NoiseStack struct{ layers []noiseLayer }

// NoiseStackBuilder collects the layers of a NoiseStack. vanilla: NoiseStack.Builder
type NoiseStackBuilder struct{ layers []noiseLayer }

// Add appends a layer. vanilla: NoiseStack.Builder.add
func (b *NoiseStackBuilder) Add(n Noise, frequency float64, amplitude float32) *NoiseStackBuilder {
	b.layers = append(b.layers, noiseLayer{n, frequency, amplitude})
	return b
}

// AddStack appends a stack's layers rescaled. vanilla: NoiseStack.Builder.addStack
func (b *NoiseStackBuilder) AddStack(s *NoiseStack, frequency float64, amplitude float32) *NoiseStackBuilder {
	for _, l := range s.layers {
		b.layers = append(b.layers, noiseLayer{l.noise, l.frequency * frequency, float32(l.amplitude * amplitude)})
	}
	return b
}

// Build is vanilla: NoiseStack.Builder.build.
func (b *NoiseStackBuilder) Build() *NoiseStack { return &NoiseStack{layers: b.layers} }

// Layers is the number of layers. vanilla: NoiseStack.layers
func (s *NoiseStack) Layers() int { return len(s.layers) }

// Get3D is vanilla: NoiseStack.get(double, double, double).
func (s *NoiseStack) Get3D(x, y, z float64) float32 {
	var value float32
	for _, l := range s.layers {
		value += float32(l.amplitude * l.noise.Get3D(x*l.frequency, y*l.frequency, z*l.frequency))
	}
	return value
}

// Get2D is vanilla: NoiseStack.get(double, double).
func (s *NoiseStack) Get2D(x, y float64) float32 {
	var value float32
	for _, l := range s.layers {
		value += float32(l.amplitude * l.noise.Get2D(x*l.frequency, y*l.frequency))
	}
	return value
}

// AddToVolume is vanilla: NoiseStack.addToVolume.
func (s *NoiseStack) AddToVolume(out []float32, v Volume, xzScale, yScale float64, amplitude float32) {
	for _, l := range s.layers {
		l.noise.AddToVolume(out, v, xzScale*l.frequency, yScale*l.frequency, float32(amplitude*l.amplitude))
	}
}
