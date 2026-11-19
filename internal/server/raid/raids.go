package raid

import (
	"math"
	"math/rand"
)

// Raids is the raids of one dimension.
// vanilla: Raids
type Raids struct {
	level  Level
	raids  map[int]*Raid
	nextID int
	tick   int
	dirty  bool
}

// New holds no raids yet.
func New(level Level) *Raids {
	return &Raids{level: level, raids: map[int]*Raid{}, nextID: 1, dirty: true}
}

// Dirty reports whether the raids changed since MarkSaved.
func (rs *Raids) Dirty() bool { return rs.dirty }

// MarkSaved clears the dirty flag.
func (rs *Raids) MarkSaved() { rs.dirty = false }

// All lists the raids by id.
func (rs *Raids) All() map[int]*Raid { return rs.raids }

// Tick runs every raid one tick; with the raids rule off they all stop.
// vanilla: Raids.tick
func (rs *Raids) Tick(enabled bool) {
	rs.tick++
	for id, r := range rs.raids {
		if !enabled {
			r.Stop()
		}
		if r.isStopped() {
			delete(rs.raids, id)
			rs.dirty = true
			continue
		}
		r.Tick()
		rs.dirty = true
	}
	if rs.tick%200 == 0 {
		rs.dirty = true
	}
}

// RaidAt is the nearest active raid whose centre is within 96 blocks.
// vanilla: Raids.getNearbyRaid, ServerLevel.getRaidAt
func (rs *Raids) RaidAt(p Pos) *Raid {
	var best *Raid
	bestDist := float64(validRadiusSqr)
	for _, r := range rs.raids {
		if d := r.center.distSqr(p); r.active && d < bestDist {
			best, bestDist = r, d
		}
	}
	return best
}

// CreateOrExtend starts a raid for a raid omen or raises the existing one's
// omen level. It centres on the mean of pois (the village POIs around at,
// where the omen began), or on at when there are none; omen is the
// amplifier.
// vanilla: Raids.createOrExtendRaid
func (rs *Raids) CreateOrExtend(p Player, omen int, at Pos, pois []Pos) *Raid {
	if p.Spectator {
		return nil
	}
	center := at
	if len(pois) > 0 {
		var sx, sy, sz float64
		for _, q := range pois {
			sx, sy, sz = sx+float64(q.X), sy+float64(q.Y), sz+float64(q.Z)
		}
		n := float64(len(pois))
		center = Pos{int(math.Floor(sx / n)), int(math.Floor(sy / n)), int(math.Floor(sz / n))}
	}
	r := rs.RaidAt(center)
	if r == nil {
		r = newRaid(rs.level, rs, center, rand.Int63())
		r.note(Trigger, 0, p.UUID)
	}
	if !r.started && !rs.contains(r) {
		rs.nextID++
		rs.raids[rs.nextID] = r
		r.id = rs.nextID
	}
	if !r.started || r.omenLevel < MaxOmenLevel {
		r.AbsorbOmen(p.UUID, omen, true)
	}
	rs.dirty = true
	return r
}

// Discard forgets a raid, as a refused trigger leaves none.
func (rs *Raids) Discard(r *Raid) {
	for id, o := range rs.raids {
		if o == r {
			delete(rs.raids, id)
			rs.dirty = true
		}
	}
}

func (rs *Raids) contains(r *Raid) bool {
	for _, o := range rs.raids {
		if o == r {
			return true
		}
	}
	return false
}

// CanJoin reports whether a raider may join a raid it stands in.
// vanilla: Raids.canJoinRaid
func CanJoin(raider Raider, canJoinRaid bool) bool {
	return raider.Alive() && canJoinRaid && raider.NoActionTicks() <= maxNoActionTicks
}

// Get is the raid saved under id, nil for none.
// vanilla: Raids.get
func (rs *Raids) Get(id int) *Raid { return rs.raids[id] }

// ID is the id the raid is saved under, 0 until it is registered. It is set
// once, so it reads without the raids' lock.
// vanilla: Raids.getId
func (r *Raid) ID() int { return r.id }

// IDOf is the id a raid is saved under, 0 for none.
func (rs *Raids) IDOf(r *Raid) int {
	for id, o := range rs.raids {
		if o == r {
			return id
		}
	}
	return 0
}

// BadOmenTurns reports whether bad omen becomes raid omen: not peaceful, in a
// village, and any raid there can still take omen levels.
// vanilla: BadOmenMobEffect.applyEffectTick
func BadOmenTurns(difficulty uint8, inVillage bool, at *Raid) bool {
	return difficulty != difficultyPeaceful && inVillage && (at == nil || at.omenLevel < MaxOmenLevel)
}

// RaidOmenDue reports whether a raid omen with remaining ticks starts now.
// vanilla: RaidOmenMobEffect.shouldApplyEffectTickThisTick
func RaidOmenDue(remaining int32) bool { return remaining <= 1 }
