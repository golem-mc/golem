package entity

import "math"

// The leash: a mob tied to a player or a leash knot. The holder reaches the
// tick as a snapshot; the lead's elastic pull and snap run in the tick. Link
// packets, the dropped item and sounds are the server's.

// LeashHolder is a player or leash knot as this tick sees it: X, Y, Z its
// feet, Width and Height its box.
type LeashHolder struct {
	EntityID      int32
	X, Y, Z       float64
	Width, Height float64
	// Present says the holder may still hold a lead: alive and not a spectator.
	// A holder gone from the world is passed as absent too.
	// vanilla: Entity.canInteractWithLevel
	Present bool
	// SameLevel says the holder is in the mob's dimension. A holder elsewhere
	// keeps the lead (vanilla drops it only for death or leaving) but there is
	// no pull, having no distance to measure.
	// vanilla: Leashable.tickLeash
	SameLevel bool
	// Knot says the holder is a leash knot, not a player.
	Knot bool
	// Trader says the holder is a wandering trader, and DespawnDelay is its
	// ticks left, which its leashed llamas leave with.
	// vanilla: TraderLlama.maybeDespawn
	Trader       bool
	DespawnDelay int32
}

// LeashSnapDistance is the box-centre distance at which a lead snaps;
// LeashElasticDistance the elastic reach, shortened by both widths before
// the pull.
// vanilla: Leashable.LEASH_TOO_FAR_DIST, Leashable.LEASH_ELASTIC_DIST
const (
	LeashSnapDistance    = 12.0
	LeashElasticDistance = 6.0
)

// LeashDistances is the kind's elastic reach and snap distance.
// vanilla: HappyGhast.leashElasticDistance, HappyGhast.leashSnapDistance
func LeashDistances(t MobType) (elastic, snap float64) {
	if t == TypeHappyGhast {
		return 10, 16
	}
	return LeashElasticDistance, LeashSnapDistance
}

// LeashRestoreTicks is how long a lead carried across dimensions or read
// from a save waits for its holder before dropping.
// vanilla: Leashable.restoreLeashFromSave
const LeashRestoreTicks = 100

// CarryLeashAcross re-ties a mob after dimension travel as an unresolved
// lead: the holder is elsewhere, so the lead drops after LeashRestoreTicks
// unless the holder arrives first.
// vanilla: Leashable.restoreLeashFromSave
func (m *Mob) CarryLeashAcross(holderID int32) {
	m.LeashHolderID = holderID
	m.leashDelayed = true
	m.leashDelayedTicks = 0
}

// RemoveLeash takes a lead off and clears the home, returning the holder it
// had. A mob without a lead keeps everything it has, home included — the
// taking-off runs for every mount, lead or none. The dropped item, the link
// packet and the sound are the caller's. The home cleared is whatever home
// the mob had, whether a lead (PathfinderMob.whenLeashedTo) or something else
// set it, as vanilla's clearHome is blind to where the home came from.
// vanilla: Leashable.dropLeash, Mob.onLeashRemoved
func (m *Mob) RemoveLeash() int32 {
	holder := m.LeashHolderID
	if holder == 0 {
		return 0
	}
	m.LeashHolderID = 0
	m.home = homeState{}
	return holder
}

// Spring damping: a displacement along the body's own motion is scaled by
// 0.3, and the pull is added with per-axis elasticity and a relative-velocity
// share. Only the drag half of the relative velocity applies, since the
// holder's snapshot carries no motion.
// vanilla: Leashable.computeDampenedSpringInteraction, AXIS_SPECIFIC_ELASTICITY, STIFFNESS
const (
	leashDampen          = 0.3
	leashElasticityXandZ = 0.8
	leashElasticityY     = 0.2
	leashRelativeDrag    = 0.11
)

// Leashable is Mob.canBeLeashed: false for Enemy kinds, the fish, the tadpole,
// the bat, the turtle, the panda, the villager and the wandering trader.
// vanilla: AbstractVillager.canBeLeashed, Panda.canBeLeashed, WaterAnimal.canBeLeashed, AmbientCreature.canBeLeashed
func Leashable(t MobType) bool {
	return (!Enemy(t) || IsHoglinKind(t)) && !FishKind(t) && t != TypeTadpole && t != TypeBat && t != TypeTurtle && t != TypePanda && t != TypeVillager && t != TypeWanderingTrader
}

// CanBeLeashed is the per-mob Mob.canBeLeashed: the kind is leashable and
// neither an angry wolf, a mob-controlled camel husk nor an angry, targeting or
// ridden nautilus.
// vanilla: Mob.canBeLeashed, CamelHusk.canBeLeashed, Wolf.canBeLeashed
func CanBeLeashed(m *Mob) bool {
	if !Leashable(m.Type) || m.MobControlled() || m.Type == TypeWolf && m.Angry() {
		return false
	}
	return !IsNautilus(m.Type) || m.nautilus.angerUntil <= m.Age && m.TargetID == 0 &&
		(m.Type != TypeZombieNautilus || len(m.Passengers) == 0)
}

// LeashableBy reports whether the holder may hold this mob's lead: the mob can
// be leashed and the holder is present and within reach, measured between box
// centres.
// vanilla: Leashable.canHaveALeashAttachedTo
func LeashableBy(m *Mob, holder *LeashHolder) bool {
	_, snap := LeashDistances(m.Type)
	return CanBeLeashed(m) && LeashReaches(m.X, m.Y, m.Z, m.BbHeight(), snap, holder)
}

// LeashReaches reports whether the holder is within a lead's reach of a body
// standing at (x, y, z) whose lead snaps past snap, measured between box
// centres.
// vanilla: Leashable.canHaveALeashAttachedTo
func LeashReaches(x, y, z, height, snap float64, holder *LeashHolder) bool {
	dy := holder.Y + 0.5*holder.Height - (y + 0.5*height)
	return math.Hypot(math.Hypot(holder.X-x, holder.Z-z), dy) <= snap
}

// LeashTickBody runs a tick of a lead on a body at (x, y, z) with the given
// box and yaw and the lead's elastic and snap distances, writing the pull
// into the velocity; found is false if h was
// not in the tick's list. An absent holder drops the lead; one in another
// dimension holds it without pull; past the full reach it breaks; inside
// the elastic band it pulls (pulled). Mobs and boats handle the drop alike.
// vanilla: Leashable.tickLeash
func LeashTickBody(x, y, z, width, height float64, yaw float32, elastic, snap float64,
	vx, vy, vz *float64, h LeashHolder, found bool) (drop, broke, pulled bool) {
	if !found || !h.Present {
		return true, false, false
	}
	if !h.SameLevel {
		return false, false, false
	}
	// Measured between box centres (leashDistanceTo).
	dy := h.Y + 0.5*h.Height - (y + 0.5*height)
	d := math.Hypot(math.Hypot(h.X-x, h.Z-z), dy)
	if d > snap {
		return true, true, false
	}
	if d > elastic-width-h.Width {
		return false, false, LeashSpring(x, y, z, width, height, yaw, elastic, vx, vy, vz, &h)
	}
	return false, false, false
}

// leashStep is Leashable.tickLeash for a mob: an absent holder drops the
// lead; a stretch past the elastic band pulls; past the full reach it snaps,
// heard at the holder. The holder is Mobs.Tick's snapshot; the drop's packet
// and sound are the server's.
// vanilla: Leashable.tickLeash
func (m *Mob) leashStep(w World, events []MobEvent) []MobEvent {
	h := m.leash
	m.leash = nil
	m.Horse.knotHeld = h != nil && h.Knot
	m.Horse.traderLead, m.Horse.traderDelay = h != nil && h.Trader, 0
	if m.Horse.traderLead {
		m.Horse.traderDelay = h.DespawnDelay
	}
	if m.LeashHolderID == 0 {
		return events
	}
	// A dead body holds no lead (canInteractWithLevel), including one that
	// died earlier this tick.
	if m.Dead {
		return append(events, m.leashDropEvent(m.RemoveLeash()))
	}
	// A lead carried through dimension travel waits for a holder here, and
	// drops once the grace passes.
	// vanilla: Leashable.restoreLeashFromSave
	if m.leashDelayed {
		if h != nil && h.SameLevel {
			m.leashDelayed, m.leashDelayedTicks = false, 0
		} else {
			m.leashDelayedTicks++
			if m.leashDelayedTicks <= LeashRestoreTicks {
				return events
			}
			m.leashDelayed, m.leashDelayedTicks = false, 0
			holderID := m.LeashHolderID
			m.LeashHolderID = 0
			// The grace's end clears the leash data without onLeashRemoved,
			// so the home the crossing carried with the body stands. The
			// lead it drops is this crossing's own: vanilla removes a
			// crossed mob's leash with no item
			// (Entity.removeAfterChangingDimensions).
			// vanilla: Leashable.restoreLeashFromSave
			return append(events, m.leashDropEvent(holderID))
		}
	}
	var holder LeashHolder
	if h != nil {
		holder = *h
	}
	elastic, snap := LeashDistances(m.Type)
	drop, broke, pulled := LeashTickBody(m.X, m.Y, m.Z, m.BbWidth(), m.BbHeight(), m.Yaw, elastic, snap,
		&m.VX, &m.VY, &m.VZ, holder, h != nil)
	if drop {
		holderID := m.RemoveLeash()
		if broke {
			ev := m.leashDropEvent(holderID)
			ev.Broke = true
			ev.X, ev.Y, ev.Z = holder.X, holder.Y, holder.Z
			return append(events, ev)
		}
		return append(events, m.leashDropEvent(holderID))
	}
	if h != nil && h.SameLevel {
		// vanilla: PathfinderMob.whenLeashedTo
		m.RestrictTo(int32(math.Floor(h.X)), int32(math.Floor(h.Y)), int32(math.Floor(h.Z)),
			int32(elastic)-1)
		if !pulled && m.Type != TypeAllay && m.Type != TypeHappyGhast && !m.panicking() {
			m.walkToLeashHolder(w, h)
		}
	}
	// A body pulled up banks no fall: onElasticLeashPull caps it.
	// vanilla: Entity.checkFallDistanceAccumulation
	if pulled {
		CapFallAccumulation(m.VY, &m.FallDistance)
	}
	// vanilla: AbstractHorse.onElasticLeashPull
	if pulled && IsEquine(m.Type) {
		m.SetMetadataHorseEating(false)
	}
	return events
}

// walkToLeashHolder orders a walk to two blocks short of the holder, or ends
// the walk when already that close.
// vanilla: PathfinderMob.closeRangeLeashBehaviour
func (m *Mob) walkToLeashHolder(w World, h *LeashHolder) {
	dx, dy, dz := h.X-m.X, h.Y-m.Y, h.Z-m.Z
	dist := math.Sqrt(dx*dx + dy*dy + dz*dz)
	if dist <= 2 {
		m.stopSeeking()
		return
	}
	k := (dist - 2) / dist
	pace := 1.0
	if IsLlama(m.Type) {
		pace = 2.0 // vanilla: Llama.followLeashSpeed
	}
	m.seekTo(w, m.X+dx*k, m.Y+dy*k, m.Z+dz*k, pace, navReach)
}

// leashDropEvent is the MobLeashDropped for a lead coming off now, with the
// mob's current position, since the tick moves the body before the server
// reports it.
func (m *Mob) leashDropEvent(holderID int32) MobEvent {
	return MobEvent{Mob: m, Kind: MobLeashDropped, HolderID: holderID,
		MobX: m.X, MobY: m.Y, MobZ: m.Z}
}

// deathLeashDrop drops the lead of a mob that died after its leash step:
// vanilla's tickLeash would catch it next tick, which this tick's early
// return would skip.
func (m *Mob) deathLeashDrop(events []MobEvent) []MobEvent {
	if m.LeashHolderID == 0 {
		return events
	}
	return append(events, m.leashDropEvent(m.RemoveLeash()))
}

// LeashSpring is a stretched lead's elastic pull on a body at (x, y, z)
// toward h: the displacement beyond the elastic slack is added to vx, vy, vz scaled
// by per-axis elasticity, damped to 0.3 where the body already moves that
// way, less a drag share of the body's motion. It reports whether the spring
// reached. The torque and the holder's motion (not in its snapshot) are
// ignored.
// vanilla: Leashable.checkElasticInteractions, Leashable.computeDampenedSpringInteraction
func LeashSpring(x, y, z, width, height float64, yaw float32, elastic float64, vx, vy, vz *float64, h *LeashHolder) bool {
	rad := float64(yaw) * math.Pi / 180
	// The body's attachment point, a half-width out the way its yaw faces.
	mx := x - 0.5*width*math.Sin(rad)
	my := y + 0.5*height
	mz := z + 0.5*width*math.Cos(rad)
	dx, dy, dz := h.X-mx, h.Y+0.5*h.Height-my, h.Z-mz
	d := math.Sqrt(dx*dx + dy*dy + dz*dz)
	if d < elastic {
		return false
	}
	pull := (d - elastic) / d
	dx, dy, dz = dx*pull, dy*pull, dz*pull
	if *vx*dx+*vy*dy+*vz*dz >= 0 {
		dx, dy, dz = dx*leashDampen, dy*leashDampen, dz*leashDampen
	}
	*vx += leashElasticityXandZ*dx - leashRelativeDrag**vx
	*vy += leashElasticityY*dy - leashRelativeDrag**vy
	*vz += leashElasticityXandZ*dz - leashRelativeDrag**vz
	return true
}
