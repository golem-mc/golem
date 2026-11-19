package world

import (
	"math"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/registry"
)

type icebergShape struct {
	r                   *decorRegion
	origin              treePos
	state               uint16
	ellipse, snowOnTop  bool
	angle               float64
	rotation            icebergRotation
	a, c, width, height int32
}

// icebergFeature is IcebergFeature.place with BlockStateConfiguration.
func icebergFeature(state uint16) featureFn {
	return func(r *decorRegion, p treePos) bool {
		// Vanilla ignores the placement height and anchors both halves at sea level.
		p[1] = SeaLevel
		rng := r.rng
		s := icebergShape{r: r, origin: p, state: state, snowOnTop: rng.NextDouble() > .7}
		s.angle = rng.NextDouble() * 2 * math.Pi
		s.a, s.c = 11-rng.NextIntN(5), 3+rng.NextIntN(3)
		s.ellipse = rng.NextDouble() > .7
		if s.ellipse {
			s.rotation.sin, s.rotation.cos = icebergSinCos(s.angle)
			s.height = rng.NextIntN(6) + 6
		} else {
			s.height = rng.NextIntN(15) + 3
		}
		if !s.ellipse && rng.NextDouble() > .9 {
			s.height += rng.NextIntN(19) + 7
		}
		underWaterHeight := min(s.height+rng.NextIntN(11), 18)
		s.width = min(s.height+rng.NextIntN(7)-rng.NextIntN(5), 11)
		a := int32(11)
		if s.ellipse {
			a = s.a
		}
		for x := -a; x < a; x++ {
			for z := -a; z < a; z++ {
				for y := int32(0); y < s.height; y++ {
					var radius int32
					if s.ellipse {
						scale := (1 - float32(y*y)/float32(s.height)) * float32(s.width)
						radius = int32(math.Ceil(float64(scale / 2)))
					} else {
						radius = icebergRoundRadius(rng, y, s.height, s.width)
					}
					if !s.ellipse && x >= radius {
						continue
					}
					s.generateBlock(x, y, z, radius, a, s.height)
				}
			}
		}
		s.smooth()
		for x := -a; x < a; x++ {
			for z := -a; z < a; z++ {
				for y := int32(-1); y > -underWaterHeight; y-- {
					newA := a
					if s.ellipse {
						newA = int32(math.Ceil(float64(float32(a) * (1 - float32(y*y)/(float32(underWaterHeight)*8)))))
					}
					radius := icebergSteepRadius(rng, -y, underWaterHeight, s.width)
					if x >= radius {
						continue
					}
					s.generateBlock(x, y, z, radius, newA, underWaterHeight)
				}
			}
		}
		cutChance := .7
		if s.ellipse {
			cutChance = .1
		}
		if rng.NextDouble() > cutChance {
			s.cutOut()
		}
		return true
	}
}

// generateBlock is IcebergFeature.generateIcebergBlock and setIcebergBlock.
func (s *icebergShape) generateBlock(x, y, z, radius, a, height int32) {
	rng := s.r.rng
	var dist float64
	if s.ellipse {
		c := s.c
		if y > 0 && height-y <= 3 {
			c -= 4 - (height - y)
		}
		dist = s.rotation.distance(x, z, a, c)
	} else {
		off := 10 * min(max(rng.NextFloat(), .2), .8) / float32(radius)
		dist = float64(off) + float64(x*x) + float64(z*z) - float64(radius*radius)
	}
	if !(dist < 0) {
		return
	}
	compare := -.5
	if !s.ellipse {
		compare = float64(-6 - rng.NextIntN(3))
	}
	if dist > compare && rng.NextDouble() > .9 {
		return
	}
	p := at(s.origin, treePos{x, y, z})
	b := registry.BlockOf(s.r.get(p[0], p[1], p[2]))
	if !overworldAirBlocks[b] && b != registry.BlockSnowBlock && b != registry.BlockIce && b != registry.BlockWater {
		return
	}
	randomness := !s.ellipse || rng.NextDouble() > .05
	divisor := int32(2)
	if s.ellipse {
		divisor = 3
	}
	state := s.state
	if s.snowOnTop && b != registry.BlockWater && float64(height-y) <= float64(rng.NextIntN(max(1, height/divisor)))+float64(height)*.6 && randomness {
		state = registry.BlockSnowBlock.DefaultState()
	}
	s.r.set(p[0], p[1], p[2], state)
}

// icebergRoundRadius is IcebergFeature.heightDependentRadiusRound.
func icebergRoundRadius(rng *levelgen.WorldgenRandom, y, height, width int32) int32 {
	k := 3.5 - rng.NextFloat()
	scale := (1 - float32(y*y)/(float32(height)*k)) * float32(width)
	if height > 15+rng.NextIntN(5) {
		tempY := y
		if y < 3+rng.NextIntN(6) {
			tempY = y / 2
		}
		scale = (1 - float32(tempY)/(float32(height)*k*.4)) * float32(width)
	}
	return int32(math.Ceil(float64(scale / 2)))
}

// icebergSteepRadius is IcebergFeature.heightDependentRadiusSteep.
func icebergSteepRadius(rng *levelgen.WorldgenRandom, y, height, width int32) int32 {
	k := 1 + rng.NextFloat()/2
	scale := (1 - float32(y)/(float32(height)*k)) * float32(width)
	return int32(math.Ceil(float64(scale / 2)))
}

type icebergRotation struct{ sin, cos float64 }

func (r icebergRotation) distance(x, z, a, c int32) float64 {
	dx := (float64(x)*r.cos - float64(z)*r.sin) / float64(a)
	dz := (float64(x)*r.sin + float64(z)*r.cos) / float64(c)
	return dx*dx + dz*dz - 1
}

// cutOut is IcebergFeature.generateCutOut.
func (s *icebergShape) cutOut() {
	rng := s.r.rng
	signX, signZ := int32(1), int32(1)
	if rng.NextBoolean() {
		signX = -1
	}
	if rng.NextBoolean() {
		signZ = -1
	}
	x := rng.NextIntN(max(s.width/2-2, 1))
	if rng.NextBoolean() {
		x = s.width/2 + 1 - rng.NextIntN(max(s.width-s.width/2-1, 1))
	}
	z := rng.NextIntN(max(s.width/2-2, 1))
	if rng.NextBoolean() {
		z = s.width/2 + 1 - rng.NextIntN(max(s.width-s.width/2-1, 1))
	}
	if s.ellipse {
		x = rng.NextIntN(max(s.a-5, 1))
		z = x
	}
	x, z = signX*x, signZ*z
	angle := s.angle + math.Pi/2
	if !s.ellipse {
		angle = rng.NextDouble() * 2 * math.Pi
	}
	rotation := icebergRotation{}
	rotation.sin, rotation.cos = icebergSinCos(angle)
	for y := int32(0); y < s.height-3; y++ {
		radius := icebergRoundRadius(rng, y, s.height, s.width)
		s.carve(radius, y, x, z, rotation, false)
	}
	// The loop bound draws again on every check, including the last.
	for y := int32(-1); y > -s.height+rng.NextIntN(5); y-- {
		radius := icebergSteepRadius(rng, -y, s.height, s.width)
		s.carve(radius, y, x, z, rotation, true)
	}
}

// carve is IcebergFeature.carve and removeFloatingSnowLayer.
func (s *icebergShape) carve(radius, y, originX, originZ int32, rotation icebergRotation, underWater bool) {
	a := radius + 1 + s.a/3
	c := min(radius-3, 3) + s.c/2 - 1
	for x := -a; x < a; x++ {
		for z := -a; z < a; z++ {
			if !(rotation.distance(x-originX, z-originZ, a, c) < 0) {
				continue
			}
			p := at(s.origin, treePos{x, y, z})
			if !isIcebergState(s.r.get(p[0], p[1], p[2])) {
				continue
			}
			if underWater {
				s.r.set(p[0], p[1], p[2], registry.BlockWater.DefaultState())
			} else {
				s.r.set(p[0], p[1], p[2], airState)
				if registry.BlockOf(s.r.get(p[0], p[1]+1, p[2])) == registry.BlockSnow {
					s.r.set(p[0], p[1]+1, p[2], airState)
				}
			}
		}
	}
}

func isIcebergState(state uint16) bool {
	b := registry.BlockOf(state)
	return b == registry.BlockPackedIce || b == registry.BlockSnowBlock || b == registry.BlockBlueIce
}

// smooth is IcebergFeature.smooth; it runs before the submerged half is placed.
func (s *icebergShape) smooth() {
	a := s.width / 2
	if s.ellipse {
		a = s.a
	}
	for x := -a; x <= a; x++ {
		for z := -a; z <= a; z++ {
			for y := int32(0); y <= s.height; y++ {
				p := at(s.origin, treePos{x, y, z})
				state := s.r.get(p[0], p[1], p[2])
				if !isIcebergState(state) && registry.BlockOf(state) != registry.BlockSnow {
					continue
				}
				if s.r.isAir(p[0], p[1]-1, p[2]) {
					s.r.set(p[0], p[1], p[2], airState)
					s.r.set(p[0], p[1]+1, p[2], airState)
					continue
				}
				if !isIcebergState(state) {
					continue
				}
				sides := 0
				for _, d := range horizontalDirs {
					if !isIcebergState(s.r.get(p[0]+d[0], p[1], p[2]+d[1])) {
						sides++
					}
				}
				if sides >= 3 {
					s.r.set(p[0], p[1], p[2], airState)
				}
			}
		}
	}
}
