package entity

import (
	"math"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// The step sound a walking mob makes: Entity.applyMovementEmissionAndPlaySound
// counts the way walked and plays the sound of the block underfoot each time
// it passes the next whole step.

const (
	// stepDistScale turns a move into step distance.
	stepDistScale = 0.6
	// stepVolume is Entity.playStepSound's share of the sound type's volume.
	stepVolume = 0.15
)

// stepState is Entity.moveDist and nextStep.
type stepState struct {
	moveDist, nextStep float32
	// started is false until nextStep takes its first value, 1.
	started bool
}

// advance counts movement toward the next stride.
func (st *stepState) advance(distance float32) bool {
	if !st.started {
		st.started, st.nextStep = true, 1
	}
	st.moveDist += distance
	return st.moveDist > st.nextStep
}

// stepSoundStep adds a move of (dx, dy, dz) to the way walked and, once it
// passes the next step, makes the step sound of the block under the feet; a
// body in water that is not walking makes the swim sound instead. The caller
// leaves out seated bodies and the kinds that move by their own rules.
// vanilla: Entity.applyMovementEmissionAndPlaySound
func (m *Mob) stepSoundStep(w World, dx, dy, dz float64, events []MobEvent) []MobEvent {
	st := &m.stride
	hw := m.width() / 2
	flat := float32(math.Hypot(dx, dz) * stepDistScale)
	walked := flat
	var supporting uint16
	var looked bool
	// Only a body moving vertically can be climbing, which counts the climb.
	if full := float32(math.Sqrt(dx*dx+dy*dy+dz*dz) * stepDistScale); full > flat {
		supporting, looked = m.supportingState(w, hw), true
		if rules.IsStateClimbable(supporting) {
			walked = full
		}
	}
	if !st.advance(walked) {
		return events
	}
	if !looked {
		supporting = m.supportingState(w, hw)
	}
	if rules.IsAir(supporting) {
		return events
	}
	ex, ey, ez, effect := rules.OnPosCell(w, m.X, m.Y, m.Z, hw)
	// A lava walker's liquid collision is absent from the static block shapes.
	if m.kind().walksOnLava && rules.IsAir(effect) {
		at := m.cell()
		if state := w.BlockAt(at.X, at.Y, at.Z); rules.IsLava(state) {
			ex, ey, ez, effect = at.X, at.Y, at.Z, state
		}
	}
	sounds := m.stepsOn(effect, dy)
	if sounds {
		events = m.stepEvent(w, ex, ey, ez, effect, events)
	}
	walks := m.stepsOn(supporting, dy)
	if sounds || walks {
		st.nextStep = m.nextStep()
		// A warden dampens vibrations, so nothing hears its steps.
		if walks && m.Type != TypeWarden {
			events = append(events, MobEvent{Mob: m, Kind: MobStepped, BlockState: supporting})
		}
	} else if m.InWater() {
		st.nextStep = m.nextStep()
		events = m.swimEvent(events)
		if m.Type != TypeWarden {
			events = append(events, MobEvent{Mob: m, Kind: MobSwamStride})
		}
	}
	return events
}

// supportingState is the block getOnPos names.
func (m *Mob) supportingState(w World, hw float64) uint16 {
	x, y, z := rules.StandingBlock(w, m.X, m.Y, m.Z, hw)
	return w.BlockAt(x, y, z)
}

// stepsOn reports whether a body stepping on state moves like a walker: on
// the ground, climbing, or crouching without rising or falling, and not
// swimming.
// vanilla: Entity.vibrationAndSoundEffectsFromBlock
func (m *Mob) stepsOn(state uint16, dy float64) bool {
	if rules.IsAir(state) {
		return false
	}
	return (m.OnGround || rules.IsStateClimbable(state) || m.Crouching && dy == 0) && !m.Swimming
}

// nextStep is Entity.nextStep: the next whole number of steps, or a strider's
// 0.6 further.
// vanilla: Entity.nextStep, Strider.nextStep
func (m *Mob) nextStep() float32 {
	if m.Type == TypeWarden {
		return m.stride.moveDist + 0.55
	}
	if m.Type == TypeStrider {
		return m.stride.moveDist + 0.6
	}
	return float32(int32(m.stride.moveDist) + 1)
}

// stepEvent queues the step sound for the block at (x, y, z).
func (m *Mob) stepEvent(w World, x, y, z int32, state uint16, events []MobEvent) []MobEvent {
	sound, volume, pitch, ok := m.stepVoice(w, x, y, z, state)
	if ok {
		events = append(events, MobEvent{Mob: m, Kind: MobSounded, SoundID: sound, Volume: volume, Pitch: pitch})
	}
	// vanilla: Horse.playGallopSound
	if ok && m.Type == TypeHorse && sound == registry.SoundEntityHorseGallop && m.Rand.Intn(10) == 0 {
		breathe := registry.SoundEntityHorseBreathe
		if m.IsBaby() {
			breathe = registry.SoundEntityBabyHorseBreathe
		}
		events = append(events, MobEvent{Mob: m, Kind: MobSounded, SoundID: breathe, Volume: volume * 4, Pitch: pitch})
	}
	// vanilla: Entity.walkingStepSound, Entity.playAmethystStepSound
	if chime, ok := m.crystal.Chime(state, int64(m.Age), m.Rand.Float32); ok {
		events = append(events, MobEvent{Mob: m, Kind: MobSounded, SoundID: chime.Event, Volume: chime.Volume, Pitch: chime.Pitch})
	}
	return events
}

// stepVoice is the kind's step sound, or the block's sound type's when the
// kind has none of its own.
// vanilla: Entity.playStepSound and its overrides
func (m *Mob) stepVoice(w World, x, y, z int32, state uint16) (sound registry.Sound, volume, pitch float32, ok bool) {
	switch m.Type {
	case TypeNautilus, TypeZombieNautilus, TypeHappyGhast:
		return registry.SoundNone, 0, 0, false
	case TypeStrider:
		// vanilla: Strider.playStepSound
		if m.InLava() {
			return registry.SoundEntityStriderStepLava, 1, 1, true
		}
	case TypeHorse, TypeDonkey, TypeMule, TypeSkeletonHorse, TypeZombieHorse:
		return m.horseStepVoice(w, x, y, z, state)
	case TypeCamel:
		// vanilla: Camel.playStepSound
		return m.camelStepVoice(state), 1, 1, true
	case TypeCamelHusk:
		// vanilla: CamelHusk.playStepSound
		return m.camelStepVoice(state), 0.4, 1, true
	}
	if m.Type == TypeTurtle {
		// vanilla: Turtle.playStepSound
		if m.IsBaby() {
			return registry.SoundEntityTurtleShambleBaby, stepVolume, 1, true
		}
		return registry.SoundEntityTurtleShamble, stepVolume, 1, true
	}
	if own := m.Sounds(); own.Step != registry.SoundNone {
		volume = own.StepVolume
		if volume == 0 {
			volume = stepVolume
		}
		return own.Step, volume, 1, true
	}
	sound, volume, pitch, ok = rules.BlockStepSound(state)
	return sound, volume * stepVolume, pitch, ok
}

// horseStepVoice scales hoof sounds to the block or its snow cover. A
// passenger starts five clops followed by a gallop every third step.
// vanilla: AbstractHorse.playStepSound
func (m *Mob) horseStepVoice(w World, x, y, z int32, state uint16) (registry.Sound, float32, float32, bool) {
	if b := registry.BlockOf(state); b == registry.BlockWater || b == registry.BlockLava {
		return registry.SoundNone, 0, 0, false
	}
	event, volume, pitch, ok := rules.BlockStepSound(state)
	if above := w.BlockAt(x, y+1, z); registry.BlockOf(above) == registry.BlockSnow {
		event, volume, pitch, ok = rules.BlockStepSound(above)
	}
	if !ok {
		return registry.SoundNone, 0, 0, false
	}
	if !m.Chested() && (m.Horse.Rider != uuid.Nil || len(m.Passengers) > 0 || len(m.Horse.ExtraRiderIDs) > 0) {
		m.Horse.gallopSoundCounter++
		switch count := m.Horse.gallopSoundCounter; {
		case count <= 5:
			return registry.SoundEntityHorseStepWood, volume * stepVolume, pitch, true
		case count%3 == 0:
			return registry.SoundEntityHorseGallop, volume * stepVolume, pitch, true
		default:
			return registry.SoundNone, 0, 0, false
		}
	}
	sound := registry.SoundEntityHorseStep
	if m.IsBaby() {
		sound = registry.SoundEntityBabyHorseStep
	}
	switch event {
	case registry.SoundBlockWoodStep, registry.SoundBlockNetherWoodStep, registry.SoundBlockStemStep, registry.SoundBlockCherryWoodStep, registry.SoundBlockBambooWoodStep:
		sound = registry.SoundEntityHorseStepWood
	}
	return sound, volume * stepVolume, pitch, true
}

// swimEvent queues the sound of a body moving through water: its volume
// follows the speed, its pitch varies.
// vanilla: Entity.waterSwimSound, Entity.playSwimSound
func (m *Mob) swimEvent(events []MobEvent) []MobEvent {
	speed := math.Sqrt(m.VX*m.VX*0.2+m.VY*m.VY+m.VZ*m.VZ*0.2) * 0.35
	pitch := 1 + (m.Rand.Float32()-m.Rand.Float32())*0.4
	volume := float32(math.Min(1, speed))
	if m.Type == TypeTurtle {
		// vanilla: Turtle.playSwimSound
		volume *= 1.5
	}
	// vanilla: SkeletonHorse.playSwimSound
	if m.Type == TypeSkeletonHorse {
		if m.OnGround {
			volume = 0.3
		} else {
			volume = min(0.1, volume*25)
		}
	}
	return append(events, MobEvent{Mob: m, Kind: MobSounded, SoundID: m.swimEventName(),
		Volume: volume, Pitch: pitch})
}

// hostileSounding reports the kinds that play the hostile swim and splash
// sounds: subclasses of Monster, and the hoglin.
// vanilla: Monster.getSwimSound, Hoglin.getSwimSound
func (m *Mob) hostileSounding() bool {
	return m.monsterClass() || m.Type == TypeHoglin
}

// swimEventName is the kind's swim event.
// vanilla: Entity.getSwimSound, Monster.getSwimSound, Drowned.getSwimSound
func (m *Mob) swimEventName() registry.Sound {
	switch {
	case m.Type == TypeSkeletonHorse:
		// vanilla: SkeletonHorse.getSwimSound
		if m.OnGround {
			if m.Horse.Rider != uuid.Nil || len(m.Passengers) > 0 || len(m.Horse.ExtraRiderIDs) > 0 {
				m.Horse.gallopSoundCounter++
				if m.Horse.gallopSoundCounter > 5 {
					if m.Horse.gallopSoundCounter%3 == 0 {
						return registry.SoundEntitySkeletonHorseGallopWater
					}
					return registry.SoundEntitySkeletonHorseSwim
				}
			}
			return registry.SoundEntitySkeletonHorseStepWater
		}
		return registry.SoundEntitySkeletonHorseSwim
	case m.Type == TypeNautilus:
		// vanilla: Nautilus.getSwimSound
		if m.IsBaby() {
			return registry.SoundEntityBabyNautilusSwim
		}
		return registry.SoundEntityNautilusSwim
	case m.Type == TypeZombieNautilus:
		if m.IsBaby() {
			return registry.SoundEntityBabyNautilusSwim
		}
		return registry.SoundEntityZombieNautilusSwim
	case m.Type == TypeDolphin:
		return registry.SoundEntityDolphinSwim
	case m.Type == TypeDrowned:
		return registry.SoundEntityDrownedSwim
	case m.Type == TypeTurtle:
		return registry.SoundEntityTurtleSwim
	case m.Type == TypeAxolotl:
		// vanilla: Axolotl.getSwimSound
		return registry.SoundEntityAxolotlSwim
	case m.hostileSounding():
		return registry.SoundEntityHostileSwim
	}
	return registry.SoundEntityGenericSwim
}

// RiddenStepSounds counts accepted client movement toward a living vehicle's steps.
// vanilla: Entity.applyMovementEmissionAndPlaySound
func (m *Mob) RiddenStepSounds(w World, fromX, fromY, fromZ float64) []MobEvent {
	// The report can cross the shoreline between base ticks.
	updateFluid(w, m.mobBox(), m.X, m.Y+m.eyeHeight(), m.Z, false, &m.waterFluid, &m.lavaFluid)
	return m.stepSoundStep(w, m.X-fromX, m.Y-fromY, m.Z-fromZ, nil)
}
