package entity

import (
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// The warden's dynamic VibrationSystem listener. The server offers each game
// event; the listener keeps the nearest event offered in one tick, then flies
// to its eye for one block per tick.
// vanilla: Warden.VibrationUser, VibrationSystem.Ticker, VibrationSelector

const (
	wardenVibrationRadius         = 16
	wardenVibrationCooldownTicks  = 40
	wardenRecentProjectileTicks   = 100
	wardenProjectileOwnerDistance = 30
)

type wardenVibration struct {
	x, y, z         float64
	distance        float32
	event           registry.GameEvent
	unknownEvent    string // Kept only for unregistered names loaded from NBT.
	tick            int64
	source          uuid.UUID
	projectileOwner uuid.UUID
	hasVibration    bool
}

// WardenVibrationParticle is the client's vibration particle aimed at the
// warden as an entity position source.
// vanilla: VibrationParticleOption, EntityPositionSource
type WardenVibrationParticle struct {
	X, Y, Z      float64
	ArrivalTicks int
}

// WardenSource is the living entity behind an offered event: a player or a
// mob body snapshot, or only its UUID when it has left the world.
type WardenSource struct {
	ID     uuid.UUID
	Player *Player
	Mob    *Mob
}

// WardenOfferVibration offers a game event, true if this listener took it. A
// nil source is an event without a living source; a resolved one is validated
// as vanilla's canTargetEntity does.
func (m *Mob) WardenOfferVibration(w World, event registry.GameEvent, x, y, z float64, source *WardenSource, projectileOwner uuid.UUID) bool {
	s := &m.warden
	if m.Type != TypeWarden || m.Dead || m.NoAI || m.wardenUnderground() ||
		s.vibrating || s.vibrationCooldown > 0 || !WardenListensFor(event) || !w.InBorder(int32(math.Floor(x)), int32(math.Floor(z))) {
		return false
	}
	view := tickView{w: w}
	switch {
	case source == nil:
	case source.Player != nil:
		if _, ok := m.wardenCheck(&view, *source.Player, source.Player.width(), nil); !ok {
			return false
		}
	case source.Mob != nil:
		if _, _, ok := m.wardenCheckMob(&view, source.Mob); !ok {
			return false
		}
	}
	ear := m.Y + m.eyeHeight()
	dx, dy, dz := x-m.X, y-ear, z-m.Z
	distance := math.Sqrt(dx*dx + dy*dy + dz*dz)
	if distance > wardenVibrationRadius || world.VibrationOccluded(w, x, y, z, m.X, ear, m.Z) {
		return false
	}
	sourceID := uuid.Nil
	if source != nil {
		sourceID = source.ID
	}
	offered := wardenVibration{x: x, y: y, z: z, distance: float32(distance), event: event, source: sourceID,
		projectileOwner: projectileOwner, hasVibration: true}
	tick := int64(m.GameTime)
	// An older saved candidate is kept against any offer from a later tick.
	// vanilla: VibrationSelector.shouldReplaceVibration
	if !s.vibrationCandidate.hasVibration ||
		s.vibrationOfferedAge == tick && wardenShouldReplaceVibration(offered, s.vibrationCandidate) {
		offered.tick = tick
		s.vibrationCandidate, s.vibrationOfferedAge = offered, tick
	}
	return true
}

// WardenListensFor reads the warden_can_listen tag: every vibration except
// flap, plus sculk clicks and a shriek.
// vanilla: GameEventTags.WARDEN_CAN_LISTEN
func WardenListensFor(event registry.GameEvent) bool {
	return event == rules.GameEventSculkSensorTendrilsClicking || event == rules.GameEventShriek ||
		event != registry.GameEventFlap && world.VibrationFrequency(event) != 0
}

// wardenShouldReplaceVibration keeps the nearest offer from one game tick,
// breaking distance ties by frequency.
// vanilla: VibrationSelector.shouldReplaceVibration
func wardenShouldReplaceVibration(next, old wardenVibration) bool {
	if next.distance < old.distance {
		return true
	}
	return next.distance == old.distance &&
		world.VibrationFrequency(next.event) > world.VibrationFrequency(old.event)
}

func (m *Mob) wardenVibrationTick(t *tickView) {
	s := &m.warden
	if s.vibrationCooldown > 0 {
		s.vibrationCooldown--
	}
	if s.recentProjectile > 0 {
		s.recentProjectile--
	}
	if !s.vibrating && s.vibrationCandidate.hasVibration && s.vibrationOfferedAge < m.GameTime {
		s.vibration, s.vibrating = s.vibrationCandidate, true
		s.vibrationCandidate = wardenVibration{}
		s.vibrationOfferedAge = -1
		full := int(math.Floor(float64(s.vibration.distance)))
		s.vibrateTicks = full
		m.queueWardenVibration(t, s.vibration.x, s.vibration.y, s.vibration.z, full)
	}
	if !s.vibrating {
		return
	}
	if s.vibrationReload {
		s.vibrationReload = false
		progress := 0.0
		if full := int(math.Floor(float64(s.vibration.distance))); full > 0 {
			progress = 1 - float64(s.vibrateTicks)/float64(full)
		}
		x := s.vibration.x + progress*(m.X-s.vibration.x)
		y := s.vibration.y + progress*(m.Y+m.eyeHeight()-s.vibration.y)
		z := s.vibration.z + progress*(m.Z-s.vibration.z)
		m.queueWardenVibration(t, x, y, z, s.vibrateTicks)
	}
	s.vibrateTicks = max(0, s.vibrateTicks-1)
	if s.vibrateTicks > 0 {
		return
	}
	s.vibrating = false
	m.receiveWardenVibration(t, s.vibration)
}

func (m *Mob) queueWardenVibration(t *tickView, x, y, z float64, arrival int) {
	t.events = append(t.events, MobEvent{Mob: m, Kind: MobWardenVibration,
		Vibration: &WardenVibrationParticle{X: x, Y: y, Z: z, ArrivalTicks: arrival}})
}

// receiveWardenVibration animates the tendrils, raises anger and records where
// to investigate.
// vanilla: Warden.VibrationUser.onReceiveVibration
func (m *Mob) receiveWardenVibration(t *tickView, v wardenVibration) {
	s := &m.warden
	s.vibrationCooldown = wardenVibrationCooldownTicks
	t.events = append(t.events,
		MobEvent{Mob: m, Kind: MobStatus, Status: int32(protocol.EntityStatusWardenTendril)},
		MobEvent{Mob: m, Kind: MobSounded, SoundID: registry.SoundEntityWardenTendrilClicks, Volume: 5, Pitch: m.VoicePitch()})

	source, _, sourceOK := m.wardenSuspectByUUID(t, v.source)
	suspicious := Waypoint{int32(math.Floor(v.x)), int32(math.Floor(v.y)), int32(math.Floor(v.z))}
	if v.projectileOwner != uuid.Nil {
		owner, ownerPlayer, ok := m.wardenSuspectByUUID(t, v.projectileOwner)
		if ok && Dist2(m.X, m.Y, m.Z, ownerPlayer.X, ownerPlayer.Y, ownerPlayer.Z) <= wardenProjectileOwnerDistance*wardenProjectileOwnerDistance {
			if s.recentProjectile != 0 {
				suspicious = Waypoint{int32(math.Floor(ownerPlayer.X)), int32(math.Floor(ownerPlayer.Y)), int32(math.Floor(ownerPlayer.Z))}
				m.wardenIncreaseAnger(t, owner.id, 35, true)
			} else {
				m.wardenIncreaseAnger(t, owner.id, 10, true)
			}
		}
		s.recentProjectile = wardenRecentProjectileTicks
	} else if v.source != uuid.Nil {
		if sourceOK {
			m.wardenIncreaseAnger(t, source.id, 35, true)
		}
	}

	if m.wardenActiveAnger() < 80 {
		active := m.wardenActiveSuspectID()
		if v.projectileOwner != uuid.Nil || active == 0 || sourceOK && active == source.id {
			m.wardenDisturbanceAt(t, suspicious)
		}
	}
}

func (m *Mob) wardenSuspectByUUID(t *tickView, id uuid.UUID) (wardenSuspect, Player, bool) {
	for _, p := range t.players {
		if p.UUID == id {
			s, ok := m.wardenCheck(t, p, p.width(), nil)
			return s, p, ok
		}
	}
	if m.WardenIdentities == nil {
		return wardenSuspect{}, Player{}, false
	}
	for _, o := range t.others {
		if m.WardenIdentities.MobUUID(o) != id {
			continue
		}
		s, p, ok := m.wardenCheckMob(t, o)
		if ok {
			if id := m.WardenIdentities.MobUUID(o); id != uuid.Nil {
				s.uuid, p.UUID = id, id
			}
			return s, p, true
		}
	}
	return wardenSuspect{}, Player{}, false
}

func (m *Mob) wardenActiveSuspectID() int32 {
	for _, suspect := range m.warden.anger {
		if suspect.id != 0 {
			return suspect.id
		}
	}
	return 0
}

// vanilla: VibrationInfo.CODEC
func (v wardenVibration) encode() *nbt.OrderedCompound {
	out := nbt.NewOrderedCompound(5)
	out.Set("game_event", nbt.String(v.event.Name()))
	if v.unknownEvent != "" {
		out.Set("game_event", nbt.String(v.unknownEvent))
	}
	out.Set("distance", nbt.Float(v.distance))
	out.Set("pos", nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{
		nbt.Double(v.x), nbt.Double(v.y), nbt.Double(v.z),
	}})
	if v.source != uuid.Nil {
		out.Set("source", nbt.UUIDToIntArray(v.source))
	}
	if v.projectileOwner != uuid.Nil {
		out.Set("projectile_owner", nbt.UUIDToIntArray(v.projectileOwner))
	}
	return out
}

func decodeWardenVibration(c nbt.Compound) (wardenVibration, bool) {
	name, ok := nbt.Get[nbt.String](c, "game_event")
	if !ok || name == "" || !protocol.ValidIdentifier(string(name)) {
		return wardenVibration{}, false
	}
	distance, ok := nbt.FloatField(c, "distance")
	if !ok || !(distance >= 0 && distance <= math.MaxFloat32) {
		return wardenVibration{}, false
	}
	pos, ok := nbt.Get[nbt.List](c, "pos")
	if !ok || len(pos.Items) != 3 {
		return wardenVibration{}, false
	}
	xyz := [3]float64{}
	for i, item := range pos.Items {
		switch value := item.(type) {
		case nbt.Byte:
			xyz[i] = float64(value)
		case nbt.Short:
			xyz[i] = float64(value)
		case nbt.Int:
			xyz[i] = float64(value)
		case nbt.Long:
			xyz[i] = float64(value)
		case nbt.Float:
			xyz[i] = float64(value)
		case nbt.Double:
			xyz[i] = float64(value)
		default:
			return wardenVibration{}, false
		}
	}
	out := wardenVibration{x: xyz[0], y: xyz[1], z: xyz[2], distance: distance,
		event: registry.GameEventByName(registry.Qualify(string(name))), hasVibration: true}
	if out.event == registry.GameEventNone {
		out.unknownEvent = registry.Qualify(string(name))
	}
	out.source, _ = nbt.UUIDField(c, "source")
	out.projectileOwner, _ = nbt.UUIDField(c, "projectile_owner")
	return out, true
}

// decodeWardenListener reads the saved VibrationSystem.Data. Unlike decode a
// refused vibration drops alone rather than rejecting the whole listener.
func decodeWardenListener(c nbt.Compound) (candidate, current wardenVibration, travel int) {
	selector, ok := nbt.Get[nbt.Compound](c, "selector")
	if !ok {
		return wardenVibration{}, wardenVibration{}, 0
	}
	tick, tickOK := nbt.LongValue(selector["tick"])
	if !tickOK {
		return wardenVibration{}, wardenVibration{}, 0
	}
	candidate.hasVibration = false
	if event, ok := nbt.Get[nbt.Compound](selector, "event"); ok {
		candidate, _ = decodeWardenVibration(event)
	}
	candidate.tick = tick
	if event, ok := nbt.Get[nbt.Compound](c, "event"); ok {
		current, _ = decodeWardenVibration(event)
	}
	delay, delayOK := nbt.IntValue(c["event_delay"])
	if !delayOK {
		return candidate, current, 0
	}
	if delay >= 0 {
		travel = int(delay)
	} else {
		return wardenVibration{}, wardenVibration{}, 0
	}
	return candidate, current, travel
}

// vanilla: VibrationSelector.CODEC, VibrationSystem.Data.CODEC
func vibrationListenerTag(candidate, current wardenVibration, travel int) *nbt.OrderedCompound {
	selector := nbt.NewOrderedCompound(2)
	tick := int64(-1)
	if candidate.event != registry.GameEventNone || candidate.unknownEvent != "" {
		selector.Set("event", candidate.encode())
		tick = candidate.tick
	}
	selector.Set("tick", nbt.Long(tick))
	listener := nbt.NewOrderedCompound(3)
	if current.hasVibration {
		listener.Set("event", current.encode())
	}
	listener.Set("selector", selector)
	listener.Set("event_delay", nbt.Int(travel))
	return listener
}
