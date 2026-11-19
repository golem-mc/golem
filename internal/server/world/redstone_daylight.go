package world

import (
	"math"

	"github.com/golem-mc/golem/registry"
)

// The daylight detector: power from the sky light over the block and the sun,
// reread every 20 game-time ticks by its block entity's ticker, inverted by a
// use, and delivered out of every face.
// vanilla: DaylightDetectorBlock

// javaRound is Java's Math.round: floor(v + 0.5), so ties go up.
// vanilla: Math.round
func javaRound(v float32) int {
	return int(math.Floor(float64(v) + 0.5))
}

// daylightPower weighs effective skylight by the sun angle in degrees.
// vanilla: DaylightDetectorBlock.updateSignalStrength
func daylightPower(effective int, inverted bool, sunDegrees float32) int {
	target := effective
	if inverted {
		target = 15 - target
	} else if target > 0 {
		sunAngle := sunDegrees * (float32(math.Pi) / 180)
		offset := float32(0)
		if sunAngle >= float32(math.Pi) {
			offset = float32(math.Pi) * 2
		}
		sunAngle += (offset - sunAngle) * 0.2
		target = javaRound(float32(target) * MthCos(float64(sunAngle)))
	}
	return min(max(target, 0), 15)
}

// daylightUpdateSignal reads the sky light at the block less the darkening
// through daylightPower; a change is a flag-3 write, so wire and lamps re-read
// and an observer sees the shape pass.
// vanilla: DaylightDetectorBlock.updateSignalStrength
func (p *Physics) daylightUpdateSignal(x, y, z int32, inverted bool, changes *[]BlockChange) {
	target := daylightPower(int(p.w.SkyLightAt(x, y, z))-p.w.SkyDarken(), inverted,
		p.w.environmentFloat(p.w.tickEnvLayers(envSunAngle), float64(x), float64(y), float64(z)))
	state := p.w.BlockAt(x, y, z)
	if registry.StatePower(state) == target {
		return
	}
	next, _ := registry.WithInt(state, registry.PropPower, target)
	p.setClient(x, y, z, next, changes)
	p.wireWakeNeighbors(x, y, z, changes)
	p.shapeUpdateNeighbors(x, y, z, changes)
}

// daylightDetectorUsed flips inverted with a flag-2 write (its shape pass still
// runs) and rereads the power. The caller has already checked the hand may
// build.
// vanilla: DaylightDetectorBlock.useWithoutItem
func (p *Physics) daylightDetectorUsed(x, y, z int32, state uint16, changes *[]BlockChange) {
	inverted := !registry.StateBool(state, registry.PropInverted)
	next, _ := registry.WithBool(state, registry.PropInverted, inverted)
	p.setClient(x, y, z, next, changes)
	p.shapeUpdateNeighbors(x, y, z, changes)
	p.effects = append(p.effects, Effect{GameEvent: gameEventBlockChange, GameEventByUser: true, X: x, Y: y, Z: z})
	p.daylightUpdateSignal(x, y, z, inverted, changes)
}

// DaylightDetectorUpdate recomputes a detector's power, the visit its ticker
// runs every 20 game-time ticks, and reports what the change reached. The
// nether has no sky light and no ticker, so its detectors keep whatever power a
// use left.
// vanilla: DaylightDetectorBlock.getTicker, tickEntity
func (p *Physics) DaylightDetectorUpdate(x, y, z int32) ([]BlockChange, []Effect, []TNTPrime) {
	if !p.w.dim.skyLight {
		return nil, nil, nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	state := p.w.BlockAt(x, y, z)
	if !isDaylightDetector[registry.BlockOf(state)] {
		return nil, nil, nil
	}
	var changes []BlockChange
	p.effects, p.wakePrimes = nil, nil
	p.daylightUpdateSignal(x, y, z, registry.StateBool(state, registry.PropInverted), &changes)
	effects, primes := p.takeWake()
	return changes, effects, primes
}
