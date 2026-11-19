package entity

import (
	"github.com/golem-mc/golem/registry"
	"math"

	"github.com/golem-mc/golem/internal/server/rules"
)

// flapState is Entity.flyDist and the nextFlap and flapSpeed of a chicken or
// parrot.
type flapState struct {
	flyDist, nextFlap, flapSpeed float32
	started                      bool
}

// Ticks between flaps. vanilla: Bee and Phantom TICKS_PER_FLAP, Bat.isFlapping
const (
	beeTicksPerFlap     = 2
	phantomTicksPerFlap = 25
	batTicksPerFlap     = 10
)

// flapsInAir reports whether the kind overrides Entity.isFlapping.
func (m *Mob) flapsInAir() bool {
	switch m.Type {
	case TypeChicken, TypeParrot, TypeBee, TypeAllay, TypeBat, TypePhantom:
		return true
	}
	return false
}

// flapStep adds the tick's move to the flown distance and, while nothing
// supports the body, flaps when its kind says so.
// vanilla: Entity.applyMovementEmissionAndPlaySound, Entity.processFlappingMovement
func (m *Mob) flapStep(w World, seated bool, fromX, fromY, fromZ float64, events []MobEvent) []MobEvent {
	if seated || !m.flapsInAir() {
		return events
	}
	f := &m.flap
	if !f.started {
		f.started, f.nextFlap = true, 1
	}
	dx, dy, dz := m.X-fromX, m.Y-fromY, m.Z-fromZ
	f.flyDist += float32(math.Sqrt(dx*dx+dy*dy+dz*dz) * stepDistScale)
	flapped := m.isFlapping()
	if flapped && rules.IsAir(m.supportingState(w, m.width()/2)) {
		events = append(events, MobEvent{Mob: m, Kind: MobFlapped})
		switch m.Type {
		case TypeParrot:
			events = append(events, MobEvent{Mob: m, Kind: MobSounded, SoundID: registry.SoundEntityParrotFly, Volume: 0.15, Pitch: 1})
			fallthrough
		case TypeChicken:
			f.nextFlap = f.flyDist + f.flapSpeed/2
		}
	}
	if m.Type == TypeChicken || m.Type == TypeParrot {
		// vanilla: Chicken.calculateFlapping, Parrot.calculateFlapping
		change := float32(4)
		if m.OnGround || m.RidesVehicle && m.Type == TypeParrot {
			change = -1
		}
		f.flapSpeed = min(1, max(0, f.flapSpeed+change*0.3))
	}
	return events
}

// isFlapping is the kind's Entity.isFlapping.
func (m *Mob) isFlapping() bool {
	switch m.Type {
	case TypeChicken, TypeParrot:
		return m.flap.flyDist > m.flap.nextFlap
	case TypeBee:
		return !m.OnGround && m.Age%beeTicksPerFlap == 0
	case TypeAllay:
		return !m.OnGround
	case TypeBat:
		return !m.Resting && m.Age%batTicksPerFlap == 0
	case TypePhantom:
		return (int(m.ID)*3+int(m.Age))%phantomTicksPerFlap == 0
	}
	return false
}
