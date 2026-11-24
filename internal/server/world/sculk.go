package world

import (
	"math"
	"math/bits"
	"math/rand"

	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// Vibration listeners, the sculk sensor's half of the game-event system. The
// server posts a game event the rules judged a vibration; each sensor in range
// keeps the tick's nearest candidate, takes it next tick, waits out its travel
// and activates: 30 ticks of power by distance, then 10 of cooldown.
// vanilla: GameEventDispatcher.post, VibrationSystem, SculkSensorBlock

const (
	// sculkSensorRange is the listener radius in blocks.
	// vanilla: SculkSensorBlockEntity.VibrationUser.LISTENER_RANGE
	sculkSensorRange = 8
	// vanilla: SculkSensorBlock.ACTIVE_TICKS, SculkSensorBlock.COOLDOWN_TICKS
	sculkActiveTicks   = 30
	sculkCooldownTicks = 10

	// sculkTickEvent tags a sensor's phase-change entry so a waterlogged
	// sensor's fluid visit neither stands for it nor blocks it; the other tags
	// use bits 5-7 alone or 5 and 6 together.
	sculkTickEvent = 1<<6 | 1<<7

	// vanilla: CalibratedSculkSensorBlock.getActiveTicks,
	// CalibratedSculkSensorBlockEntity.VibrationUser.getListenerRadius
	calibratedActiveTicks = 10
	calibratedSensorRange = 16
	sculkShrieker         = "minecraft:sculk_shrieker"
)

// isSculkSensor reports whether a block is either sensor.
func isSculkSensor(block registry.Block) bool {
	return block == registry.BlockSculkSensor || block == registry.BlockCalibratedSculkSensor
}

// sensorRange is a sensor's listener radius in blocks.
func sensorRange(block registry.Block) int {
	if block == registry.BlockCalibratedSculkSensor {
		return calibratedSensorRange
	}
	return sculkSensorRange
}

// sensorActiveTicks is how long a sensor stays active.
func sensorActiveTicks(block registry.Block) int64 {
	if block == registry.BlockCalibratedSculkSensor {
		return calibratedActiveTicks
	}
	return sculkActiveTicks
}

// Vibration is one game event the rules judged a vibration, posted at the
// position it happened.
type Vibration struct {
	X, Y, Z float64
	// Event is the registered game event, saved by name in a listener.
	Event        registry.GameEvent
	unknownEvent string // An unregistered resonance from a loaded zero-frequency event.
	// Frequency is the event's vibration frequency, 1 to 15.
	Frequency int
	// Radius, when above 0, is the farthest a sensor still hears it.
	Radius int
	// PlaceOrDestroy marks block_place and block_destroy, which a sensor
	// ignores at its own cell.
	PlaceOrDestroy bool
	// Clicking marks the sculk_sensor_tendrils_clicking event, which only
	// a sculk shrieker listens to; Frequency is unused.
	Clicking bool
	// Player is the UUID of the player the event traces back to, uuid.Nil for
	// none. A shrieker ignores an event without one.
	// vanilla: SculkShriekerBlockEntity.tryGetPlayer
	Player uuid.UUID
	// Mob is the non-player source UUID; it never makes a shrieker eligible.
	Mob uuid.UUID
}

// vibrationInfo is a vibration as one listener took it: the source and its
// distance to the listener's centre.
// vanilla: VibrationInfo
type vibrationInfo struct {
	x, y, z      float64
	dist         float32
	event        registry.GameEvent
	unknownEvent string // Kept only for unregistered names loaded from NBT.
	freq         int
	player       uuid.UUID
	mob          uuid.UUID
}

// vibrationListener is one sensor's or shrieker's VibrationSystem.Data, kept
// in Physics.listeners while a vibration is waiting or in flight and copied
// into the block entity, which saves it under listener.
type vibrationListener struct {
	candidate     vibrationInfo
	candidateTick int64
	hasCandidate  bool
	current       vibrationInfo
	hasCurrent    bool
	travel        int
	// reloadParticle marks a loaded vibration in flight, whose particle is
	// sent again on its first tick.
	reloadParticle bool
}

// vibrationFrequency is each vibrating event's frequency; its keys are
// exactly the minecraft:vibrations game event tag, so a zero entry
// does not vibrate.
// vanilla: VibrationSystem.VIBRATION_FREQUENCY_FOR_EVENT
var vibrationFrequency = [registry.GameEventCount]int{
	registry.GameEventStep: 1, registry.GameEventSwim: 1, registry.GameEventFlap: 1,
	registry.GameEventProjectileLand: 2, registry.GameEventHitGround: 2, registry.GameEventSplash: 2, registry.GameEventBounce: 2,
	registry.GameEventItemInteractFinish: 3, registry.GameEventProjectileShoot: 3, registry.GameEventInstrumentPlay: 3,
	registry.GameEventEntityAction: 4, registry.GameEventElytraGlide: 4, registry.GameEventUnequip: 4,
	registry.GameEventEntityDismount: 5, registry.GameEventEquip: 5,
	registry.GameEventEntityInteract: 6, registry.GameEventShear: 6, registry.GameEventEntityMount: 6,
	registry.GameEventEntityDamage: 7,
	registry.GameEventDrink:        8, registry.GameEventEat: 8,
	registry.GameEventContainerClose: 9, registry.GameEventBlockClose: 9, registry.GameEventBlockDeactivate: 9, registry.GameEventBlockDetach: 9,
	registry.GameEventContainerOpen: 10, registry.GameEventBlockOpen: 10, registry.GameEventBlockActivate: 10, registry.GameEventBlockAttach: 10,
	registry.GameEventPrimeFuse: 10, registry.GameEventNoteBlockPlay: 10,
	registry.GameEventBlockChange:  11,
	registry.GameEventBlockDestroy: 12, registry.GameEventFluidPickup: 12,
	registry.GameEventBlockPlace: 13, registry.GameEventFluidPlace: 13,
	registry.GameEventEntityPlace: 14, registry.GameEventLightningStrike: 14, registry.GameEventTeleport: 14,
	registry.GameEventEntityDie: 15, registry.GameEventExplode: 15,
	registry.GameEventResonate1: 1, registry.GameEventResonate2: 2, registry.GameEventResonate3: 3, registry.GameEventResonate4: 4,
	registry.GameEventResonate5: 5, registry.GameEventResonate6: 6, registry.GameEventResonate7: 7, registry.GameEventResonate8: 8,
	registry.GameEventResonate9: 9, registry.GameEventResonate10: 10, registry.GameEventResonate11: 11, registry.GameEventResonate12: 12,
	registry.GameEventResonate13: 13, registry.GameEventResonate14: 14, registry.GameEventResonate15: 15,
}

// VibrationFrequency returns the game event's vibration frequency, 0 for an
// event that does not vibrate.
// vanilla: VibrationSystem.getGameEventFrequency
func VibrationFrequency(event registry.GameEvent) int {
	if int(event) >= len(vibrationFrequency) {
		return 0
	}
	return vibrationFrequency[event]
}

// listensForVibrations reports whether a state is a vibration listener the
// sections count.
func listensForVibrations(state uint16) bool {
	b := registry.BlockOf(state)
	return b == registry.BlockSculkSensor || b == registry.BlockCalibratedSculkSensor ||
		b == registry.BlockSculkShrieker
}

// offer keeps a tick's first candidate unless a nearer one, or an equally near
// one of higher frequency, arrives that tick.
// vanilla: VibrationSelector.shouldReplaceVibration
func (l *vibrationListener) offer(v vibrationInfo, tick int64) {
	if l.hasCandidate {
		if tick != l.candidateTick || v.dist > l.candidate.dist {
			return
		}
		if v.dist == l.candidate.dist && v.freq <= l.candidate.freq {
			return
		}
	}
	l.candidate, l.candidateTick, l.hasCandidate = v, tick, true
}

// vibrationStrength is the redstone power a vibration received from distance
// blocks away gives, at least 1.
// vanilla: VibrationSystem.getRedstoneStrengthForDistance
func vibrationStrength(distance float32, radius int) int {
	scale := 15.0 / float64(radius)
	return max(1, 15-int(math.Floor(scale*float64(distance))))
}

// vibrationListenersNear appends the listener cells within r blocks of (cx, cy,
// cz), by squared block distance, skipping sections with none.
// vanilla: EuclideanGameEventListenerRegistry.getPostableListenerPosition
func (w *World) vibrationListenersNear(cx, cy, cz, r int32, buf []blockKey) []blockKey {
	w.mu.RLock()
	defer w.mu.RUnlock()
	minSec := max((cy-r-worldMinY)>>4, 0)
	maxSec := min((cy+r-worldMinY)>>4, sectionCount-1)
	for chx := (cx - r) >> 4; chx <= (cx+r)>>4; chx++ {
		for chz := (cz - r) >> 4; chz <= (cz+r)>>4; chz++ {
			loaded, ok := w.loadedAt(ChunkPos{X: chx, Z: chz})
			if !ok {
				continue
			}
			for sec := minSec; sec <= maxSec; sec++ {
				s := &loaded.chunk.sections[sec]
				if s.listening == 0 {
					continue
				}
				baseY := worldMinY + sec*sectionSize
				for wi, word := range s.listeners {
					for ; word != 0; word &= word - 1 {
						i := int32(wi<<6 | bits.TrailingZeros64(word))
						x, y, z := chx<<4+i&15, int32(baseY)+i>>8, chz<<4+i>>4&15
						dx, dy, dz := x-cx, y-cy, z-cz
						if dx*dx+dy*dy+dz*dz <= r*r {
							buf = append(buf, blockKey{x, y, z})
						}
					}
				}
			}
		}
	}
	return buf
}

// Vibrate posts a vibration to the sensors in range; an inactive sensor with
// nothing in flight keeps it as the tick's candidate if best so far. Nothing
// changes until the next Tick.
// vanilla: GameEventDispatcher.post, VibrationSystem.Listener.handleGameEvent,
// SculkSensorBlockEntity.VibrationUser.canReceiveVibration
func (p *Physics) Vibrate(v Vibration) {
	if v.Frequency <= 0 && !v.Clicking {
		return
	}
	sensors := p.w.vibrationListenersNear(floorCell(v.X), floorCell(v.Y), floorCell(v.Z), calibratedSensorRange, nil)
	if len(sensors) == 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.offerVibration(v, sensors)
}

// SneakedPast reports whether a sculk sensor is in range of the point, so a
// sneaking player there avoided its vibration. Phase, occlusion and
// calibration are not checked: vanilla tests them after the sneaking rule.
// vanilla: VibrationSystem.Listener.handleGameEvent
func (p *Physics) SneakedPast(x, y, z float64) bool {
	cx, cy, cz := floorCell(x), floorCell(y), floorCell(z)
	sensors := p.w.vibrationListenersNear(cx, cy, cz, calibratedSensorRange, nil)
	if len(sensors) == 0 {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, key := range sensors {
		block := registry.BlockOf(p.w.BlockAt(key.x, key.y, key.z))
		if !isSculkSensor(block) {
			continue
		}
		r := int32(sensorRange(block))
		if dx, dy, dz := key.x-cx, key.y-cy, key.z-cz; dx*dx+dy*dy+dz*dz > r*r {
			continue
		}
		if l := p.listeners[key]; l != nil && l.hasCurrent {
			continue
		}
		return true
	}
	return false
}

// StepOnSensor has a body standing on the sculk sensor at (x, y, z) schedule
// its step as a candidate directly, skipping the occlusion, range and
// in-flight checks; a calibrated sensor still hears only its powered
// frequency. v is the body's step at its position.
// vanilla: SculkSensorBlock.stepOn, VibrationSystem.Listener.forceScheduleVibration
func (p *Physics) StepOnSensor(x, y, z int32, v Vibration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	key := blockKey{x, y, z}
	state := p.w.BlockAt(x, y, z)
	block := registry.BlockOf(state)
	if !isSculkSensor(block) || registry.StateValue(state, registry.PropSculkSensorPhase) != "inactive" || !p.hearsFrequency(key, state, v.Frequency) {
		return
	}
	l := p.listeners[key]
	if l == nil {
		l = &vibrationListener{}
		p.listeners[key] = l
	}
	before := *l
	l.offer(p.vibrationInfoFor(key, v), p.now)
	if *l != before {
		p.saveListener(key)
	}
}

func floorCell(v float64) int32 { return int32(math.Floor(v)) }

// vibrateLocked is Vibrate for a caller holding the engine's lock.
func (p *Physics) vibrateLocked(v Vibration) {
	p.offerVibration(v, p.w.vibrationListenersNear(floorCell(v.X), floorCell(v.Y), floorCell(v.Z), calibratedSensorRange, nil))
}

// offerVibration offers the vibration to each listener cell in range.
func (p *Physics) offerVibration(v Vibration, sensors []blockKey) {
	cx, cy, cz := floorCell(v.X), floorCell(v.Y), floorCell(v.Z)
	for _, key := range sensors {
		if v.PlaceOrDestroy && key == (blockKey{cx, cy, cz}) {
			continue
		}
		sensorState := p.w.BlockAt(key.x, key.y, key.z)
		block := registry.BlockOf(sensorState)
		// The shrieker hears the sensors' clicks only, the sensors every
		// other event.
		// vanilla: SculkShriekerBlockEntity.VibrationUser.getListenableEvents
		if v.Clicking {
			if block != registry.BlockSculkShrieker || registry.StateValue(sensorState, registry.PropShrieking) != "false" || v.Player == uuid.Nil {
				continue
			}
		} else if !isSculkSensor(block) || registry.StateValue(sensorState, registry.PropSculkSensorPhase) != "inactive" {
			continue
		}
		r := int32(sensorRange(block))
		if dx, dy, dz := key.x-cx, key.y-cy, key.z-cz; dx*dx+dy*dy+dz*dz > r*r ||
			(v.Radius > 0 && dx*dx+dy*dy+dz*dz > int32(v.Radius*v.Radius)) {
			continue
		}
		if !p.hearsFrequency(key, sensorState, v.Frequency) {
			continue
		}
		l := p.listeners[key]
		if l != nil && l.hasCurrent {
			continue
		}
		if VibrationOccluded(p.w, v.X, v.Y, v.Z, float64(key.x), float64(key.y), float64(key.z)) {
			continue
		}
		if l == nil {
			l = &vibrationListener{}
			p.listeners[key] = l
		}
		before := *l
		l.offer(p.vibrationInfoFor(key, v), p.now)
		if *l != before {
			p.saveListener(key)
		}
	}
}

// hearsFrequency reports whether a sensor state hears a frequency: a calibrated
// sensor only the one its back face is powered with, no power hearing
// everything.
// vanilla: CalibratedSculkSensorBlockEntity.VibrationUser.canReceiveVibration
func (p *Physics) hearsFrequency(key blockKey, state uint16, freq int) bool {
	if registry.BlockOf(state) != registry.BlockCalibratedSculkSensor {
		return true
	}
	want := p.signalAt(key.x, key.y, key.z, blocklogic.Opposite(registry.StateFacing(state)))
	return want == 0 || want == freq
}

// vibrationInfoFor is v as a listener at key takes it, with its distance to
// the listener's centre.
func (p *Physics) vibrationInfoFor(key blockKey, v Vibration) vibrationInfo {
	dx := v.X - (float64(key.x) + 0.5)
	dy := v.Y - (float64(key.y) + 0.5)
	dz := v.Z - (float64(key.z) + 0.5)
	return vibrationInfo{
		x: v.X, y: v.Y, z: v.Z,
		dist:         float32(math.Sqrt(dx*dx + dy*dy + dz*dz)),
		event:        v.Event,
		unknownEvent: v.unknownEvent,
		freq:         v.Frequency,
		player:       v.Player,
		mob:          v.Mob,
	}
}

// VibrationOccluded reports whether wool blocks every path: the line between
// cell centres, started a hair out of the source in each of the six directions,
// must cross an occluding block each time.
// vanilla: VibrationSystem.Listener.isOccluded
func VibrationOccluded(w BlockReader, x, y, z, ex, ey, ez float64) bool {
	// vanilla: Vec3.relative(direction, 1.0E-5f), a float constant
	const nudge = float64(float32(1.0e-5))
	fx := math.Floor(x) + 0.5
	fy := math.Floor(y) + 0.5
	fz := math.Floor(z) + 0.5
	tx, ty, tz := math.Floor(ex)+0.5, math.Floor(ey)+0.5, math.Floor(ez)+0.5
	for _, d := range [6][3]float64{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}} {
		ox, oy, oz := fx+d[0]*nudge, fy+d[1]*nudge, fz+d[2]*nudge
		blocked := TraverseBlocks(ox, oy, oz, tx-ox, ty-oy, tz-oz, func(x, y, z, _ int32) bool {
			return registry.BlockOf(w.BlockAt(x, y, z)).InTag(registry.TagIDOccludesVibrationSignals)
		})
		if !blocked {
			return false
		}
	}
	return true
}

// vibrationTicks runs each listening sensor's block entity tick: an earlier
// tick's candidate goes in flight at one block a tick and is received when its
// travel runs out; a listener with nothing in flight is dropped. The listeners
// loaded since the last tick are taken up first.
// vanilla: VibrationSystem.Ticker.tick
func (p *Physics) vibrationTicks(changes *[]BlockChange) {
	p.adoptListeners()
	for key, l := range p.listeners {
		before := *l
		if !l.hasCurrent && l.hasCandidate && l.candidateTick < p.now {
			l.current, l.hasCurrent, l.hasCandidate = l.candidate, true, false
			l.travel = travelTicks(l.current.dist)
			p.effects = append(p.effects, Effect{Vibration: &VibrationParticle{
				X: l.current.x, Y: l.current.y, Z: l.current.z,
				DestX: key.x, DestY: key.y, DestZ: key.z, ArrivalTicks: l.travel,
			}})
		}
		if l.hasCurrent {
			if l.reloadParticle {
				l.reloadParticle = false
				p.effects = append(p.effects, Effect{Vibration: reloadedParticle(key, l)})
			}
			l.travel = max(0, l.travel-1)
			if l.travel <= 0 {
				l.hasCurrent = false
				p.receiveVibration(key, l, changes)
			}
		}
		if !l.hasCandidate && !l.hasCurrent {
			delete(p.listeners, key)
		}
		if *l != before {
			p.saveListener(key)
		}
	}
}

// travelTicks is a vibration's flight time, a block a tick.
// vanilla: VibrationSystem.User.calculateTravelTimeInTicks
func travelTicks(dist float32) int {
	return int(math.Floor(float64(dist)))
}

// reloadedParticle is a loaded vibration's particle, sent from where along its
// line the remaining travel puts it.
// vanilla: VibrationSystem.Ticker.tryReloadVibrationParticle
func reloadedParticle(key blockKey, l *vibrationListener) *VibrationParticle {
	progress := 0.0
	if full := travelTicks(l.current.dist); full > 0 {
		progress = 1 - float64(l.travel)/float64(full)
	}
	lerp := func(from, to float64) float64 { return from + progress*(to-from) }
	return &VibrationParticle{
		X:     lerp(l.current.x, float64(key.x)+0.5),
		Y:     lerp(l.current.y, float64(key.y)+0.5),
		Z:     lerp(l.current.z, float64(key.z)+0.5),
		DestX: key.x, DestY: key.y, DestZ: key.z, ArrivalTicks: l.travel,
	}
}

// receiveVibration activates a still-inactive sensor with the power its
// distance to the source gives.
// vanilla: VibrationSystem.Ticker.receiveVibration,
// SculkSensorBlockEntity.VibrationUser.onReceiveVibration
func (p *Physics) receiveVibration(key blockKey, l *vibrationListener, changes *[]BlockChange) {
	state := p.w.BlockAt(key.x, key.y, key.z)
	block := registry.BlockOf(state)
	if block == registry.BlockSculkShrieker {
		// The shriek is the server's: it holds the players' warden trackers.
		// vanilla: SculkShriekerBlockEntity.VibrationUser.onReceiveVibration
		p.effects = append(p.effects, Effect{Shriek: &ShriekRequest{X: key.x, Y: key.y, Z: key.z, Player: l.current.player}})
		return
	}
	if !isSculkSensor(block) || registry.StateValue(state, registry.PropSculkSensorPhase) != "inactive" {
		return
	}
	// The distance is between the two block positions, not the exact source.
	// vanilla: VibrationSystem.Listener.distanceBetweenInBlocks
	dx := float64(int32(math.Floor(l.current.x)) - key.x)
	dy := float64(int32(math.Floor(l.current.y)) - key.y)
	dz := float64(int32(math.Floor(l.current.z)) - key.z)
	dist := float32(math.Sqrt(dx*dx + dy*dy + dz*dz))
	if be := p.w.EnsureBlockEntity(key.x, key.y, key.z, kindOfID(block.Name())); be != nil {
		be.Use(func(b *BlockEntity) { b.SculkFrequency = l.current.freq })
	}
	p.sculkSetPhase(key.x, key.y, key.z, state, "active", vibrationStrength(dist, sensorRange(block)), changes)
	p.scheduleBooked(p.sculkBooked, key.x, key.y, key.z, p.now+sensorActiveTicks(block), sculkTickEvent)
	p.sculkUpdateNeighbours(key.x, key.y, key.z, changes)
	p.sculkResonate(key, l.current.freq, l.current.player, l.current.mob)
	// vanilla: Level.gameEvent(SCULK_SENSOR_TENDRILS_CLICKING)
	click := Vibration{
		X: float64(key.x) + 0.5, Y: float64(key.y) + 0.5, Z: float64(key.z) + 0.5,
		Event: registry.GameEventSculkSensorTendrilsClicking, Clicking: true, Player: l.current.player, Mob: l.current.mob,
	}
	p.vibrateLocked(click)
	p.effects = append(p.effects, Effect{ListenerEvent: &click})
	p.sculkSound(key.x, key.y, key.z, state, registry.SoundBlockSculkSensorClicking)
}

// resonancePitch is the amethyst chime's pitch per frequency, 2^((tone-12)/12)
// over two octaves.
// vanilla: SculkSensorBlock.RESONANCE_PITCH_BEND, NoteBlock.getPitchFromNote
var resonancePitch = func() [16]float32 {
	tones := [16]int{0, 0, 2, 4, 6, 7, 9, 10, 12, 14, 15, 18, 19, 21, 22, 24}
	var out [16]float32
	for i, tone := range tones {
		out[i] = float32(math.Pow(2, float64(tone-12)/12))
	}
	return out
}()

// sculkResonate has each amethyst block around an activating sensor chime and
// post the frequency's resonance event, which nearby sensors hear in turn.
// vanilla: SculkSensorBlock.tryResonateVibration
func (p *Physics) sculkResonate(key blockKey, freq int, player, mob uuid.UUID) {
	for d := range dirCount {
		x, y, z := key.x+dirDelta[d][0], key.y+dirDelta[d][1], key.z+dirDelta[d][2]
		if !registry.BlockOf(p.w.BlockAt(x, y, z)).InTag(registry.TagIDVibrationResonators) {
			continue
		}
		v := Vibration{
			X: float64(x) + 0.5, Y: float64(y) + 0.5, Z: float64(z) + 0.5,
			Frequency: freq, Player: player, Mob: mob,
		}
		if freq == 0 {
			// Preserve the saved name when a loaded unregistered event resonates.
			v.unknownEvent = "minecraft:resonate_0"
		} else {
			// vanilla: VibrationSystem.RESONANCE_EVENTS, contiguous registry IDs.
			v.Event = registry.GameEventResonate1 + registry.GameEvent(freq-1)
		}
		p.vibrateLocked(v)
		p.effects = append(p.effects, Effect{ListenerEvent: &v})
		p.effects = append(p.effects, Effect{
			SoundID: registry.SoundBlockAmethystBlockResonate, X: x, Y: y, Z: z,
			Volume: 1, Pitch: resonancePitch[freq],
		})
	}
}

// sculkTick is the booked phase change: active to cooldown with power gone,
// cooldown to inactive.
// vanilla: SculkSensorBlock.tick, SculkSensorBlock.deactivate
func (p *Physics) sculkTick(x, y, z int32, changes *[]BlockChange) {
	state := p.w.BlockAt(x, y, z)
	block := registry.BlockOf(state)
	if block == registry.BlockSculkShrieker {
		p.shriekerTick(x, y, z, state, changes)
		return
	}
	if block == registry.BlockSculkCatalyst {
		p.catalystEndPulse(x, y, z, state, changes)
		return
	}
	if !isSculkSensor(block) {
		return
	}
	switch registry.StateValue(state, registry.PropSculkSensorPhase) {
	case "active":
		p.sculkSetPhase(x, y, z, state, "cooldown", 0, changes)
		p.scheduleBooked(p.sculkBooked, x, y, z, p.now+sculkCooldownTicks, sculkTickEvent)
		p.sculkUpdateNeighbours(x, y, z, changes)
	case "cooldown":
		p.sculkSetPhase(x, y, z, state, "inactive", 0, changes)
		p.sculkSound(x, y, z, state, registry.SoundBlockSculkSensorClickingStop)
	}
}

// sculkSetPhase writes phase and power with flags 3: neighbour update, shape
// pass, comparators.
func (p *Physics) sculkSetPhase(x, y, z int32, state uint16, phase string, power int, changes *[]BlockChange) {
	next, ok := registry.WithProp(state, registry.PropSculkSensorPhase, phase)
	if !ok {
		return
	}
	next, ok = registry.WithInt(next, registry.PropPower, power)
	if !ok {
		return
	}
	p.setClient(x, y, z, next, changes)
	p.wireWakeNeighbors(x, y, z, changes)
	p.shapeUpdateNeighbors(x, y, z, changes)
	p.analogOutputChanged(x, y, z, p.now)
}

// sculkUpdateNeighbours wakes the neighbours of the sensor and of the block
// below it, which the sensor strongly powers.
// vanilla: SculkSensorBlock.updateNeighbours
func (p *Physics) sculkUpdateNeighbours(x, y, z int32, changes *[]BlockChange) {
	p.wireWakeNeighbors(x, y, z, changes)
	p.wireWakeNeighbors(x, y-1, z, changes)
}

// sculkSound plays a sensor's click at pitch 0.8-1.0, silent when waterlogged.
// vanilla: SculkSensorBlock.activate, SculkSensorBlock.tick
func (p *Physics) sculkSound(x, y, z int32, state uint16, sound registry.Sound) {
	if registry.StateBool(state, registry.PropWaterlogged) {
		return
	}
	p.effects = append(p.effects, Effect{
		SoundID: sound, X: x, Y: y, Z: z, Volume: 1, Pitch: rand.Float32()*0.2 + 0.8,
	})
}

// sculkOnPlace drops power a sensor arrived with without a phase change booked
// (a setblock's state).
// vanilla: SculkSensorBlock.onPlace
func (p *Physics) sculkOnPlace(x, y, z int32, state, oldState uint16, changes *[]BlockChange) {
	if registry.BlockOf(oldState) == registry.BlockOf(state) {
		return
	}
	if registry.StatePower(state) == 0 || p.sculkBooked[blockKey{x, y, z}] {
		return
	}
	next, _ := registry.WithInt(state, registry.PropPower, 0)
	p.setClient(x, y, z, next, changes)
}
