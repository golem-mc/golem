package server

import (
	"math/rand"
	"slices"
	"sync"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// mobLocated is one mob's pack location_changed and tick check, queued under
// the mob lock and run after it.
type mobLocated struct {
	w       *world.World
	mob     *entity.Mob
	x, y, z float64
	changed int32 // the event's Level: entity.LocatedMoved, entity.LocatedLanded and a bit per changed piece
}

// locatedScratch is the per-walk state of mobLocated: the context, the
// wearer snapshot it builds on first use and the closure that builds it, so a
// walk allocates none of them.
type locatedScratch struct {
	ctx    rules.EnchContext
	body   lootBody
	l      mobLocated
	bodyOf func() *rules.LootEntity
}

var locatedScratches = sync.Pool{New: func() any {
	sc := new(locatedScratch)
	sc.bodyOf = sc.fill
	return sc
}}

func (sc *locatedScratch) fill() *rules.LootEntity {
	l, m := sc.l, sc.l.mob
	return sc.body.fill(l.w, m.Type.Name(), l.x, l.y, l.z, m.BbWidth()/2,
		rules.LootEntityFlags{OnGround: m.OnGround}, int64(m.Age), [3]float64{m.VX, m.VY, m.VZ})
}

// reset readies the scratch for the walk l and returns its context. The
// scratch must be put back after the walk: nothing may keep the context.
func (sc *locatedScratch) reset(l mobLocated) *rules.EnchContext {
	sc.l = l
	fillEntityItemEnchContext(&sc.ctx, l.w, l.x, l.y, l.z, int32(l.mob.Type))
	sc.ctx.World = l.w
	sc.ctx.Rand = l.mob.Rand
	sc.ctx.User.Riding = l.mob.RidesVehicle
	sc.ctx.BodyOf = sc.bodyOf
	return &sc.ctx
}

func (sc *locatedScratch) release() {
	sc.l, sc.ctx = mobLocated{}, rules.EnchContext{}
	locatedScratches.Put(sc)
}

// locatedSound is a play_sound entry resolved at its action's level.
type locatedSound struct {
	sound  protocol.SoundHolder
	volume float32
	pitch  float32
}

// resolveLocatedSound picks the sound for the level and samples its pitch.
func resolveLocatedSound(ps *registry.EnchPlaySound, level int32, rng *rand.Rand) locatedSound {
	sound := ps.Sound
	if len(ps.Sounds) > 0 {
		sound = ps.Sounds[min(max(int(level)-1, 0), len(ps.Sounds)-1)]
	}
	return locatedSound{sound: sound, volume: ps.Volume, pitch: ps.PitchWith(rng)}
}

// mobLocated runs the entries of a worn pack enchantment: the location_changed
// entries of the pieces whose stack changed, and of all of them when the
// mob's cell changed or it landed (an attribute entry puts its modifier on
// the mob's map, made on first use), and each tick the tick entries. A
// replace_disk entry is returned for the batch, which sees every body.
// vanilla: EnchantmentHelper.runLocationChangedEffects, tickEffects
func (s *Server) mobLocated(l mobLocated) []mobDisk {
	m := l.mob
	s.mobsMu.Lock()
	ent := s.herdOf(l.w).book[m]
	if ent == nil || m.Dead {
		s.mobsMu.Unlock()
		return nil
	}
	pieces := m.EnchEquipment()
	if l.changed&^(entity.LocatedMoved|entity.LocatedLanded) == 0 && !slices.ContainsFunc(pieces, func(p rules.EnchPiece) bool {
		return rules.HasLocated(p.Stack)
	}) {
		// No piece changed and none carries an enchantment: nothing to step,
		// and an empty slot has nothing active to stop.
		s.mobsMu.Unlock()
		return nil
	}
	sc := locatedScratches.Get().(*locatedScratch)
	defer sc.release()
	ctx := sc.reset(l)
	// Backing arrays for the usual one or two entries, so a walk with nothing
	// to add makes no slice.
	var applyBuf, removeBuf, actionBuf [2]rules.LocatedAction
	apply, remove, actions := applyBuf[:0], removeBuf[:0], actionBuf[:0]
	for i, p := range pieces {
		changed := l.changed&(1<<i) != 0
		if changed || l.changed&entity.LocatedMoved != 0 {
			a, r := m.Located().Step(p.Slot, p.Stack, ctx, changed)
			apply, remove = append(apply, a...), append(remove, r...)
		}
		if l.changed&entity.LocatedLanded == 0 {
			actions = append(actions, rules.LocatedEffects(p.Stack, p.Slot, true, ctx, m.Rand)...)
		}
	}
	actions = append(actions, apply...)
	if len(actions) == 0 && len(remove) == 0 {
		s.mobsMu.Unlock()
		return nil
	}
	if len(apply)+len(remove) > 0 {
		if m.Attributes() == nil {
			m.SetAttributes(m.BuildAttributes(savedAttributes(ent.saved)))
		}
		entity.ApplyLocatedMods(m.Attributes(), apply, remove)
	}
	var changes []effect.Change
	var sounds []locatedSound
	for _, a := range actions {
		for _, in := range a.MobEffects(m.Rand) {
			if ch, ok := m.AddEffect(in); ok {
				changes = append(changes, ch)
			}
		}
		if ps := a.Entry.Effect.PlaySound; ps != nil && !m.Silent {
			sounds = append(sounds, resolveLocatedSound(ps, a.Level, m.Rand))
		}
	}
	var tell splashMobTell
	if len(changes) > 0 {
		tell = mobEffectTellOf(ent, changes)
	}
	s.mobsMu.Unlock()
	if len(changes) > 0 {
		s.tellSplashMob(l.w, tell)
	}
	for _, sd := range sounds {
		s.broadcastSoundHolder(l.w, sd.sound, mobSoundCategory(m.Type), l.x, l.y, l.z, sd.volume, sd.pitch)
	}
	body := enchantTarget{uuid: ent.uuid, x: l.x, y: l.y, z: l.z, width: m.BbWidth(), height: m.BbHeight(), vx: m.VX, vy: m.VY, vz: m.VZ, entityID: m.ID}
	var disks []mobDisk
	for _, a := range actions {
		e := a.Entry.Effect
		if d, ok := diskOf(l.w, &e, a.Level, body); ok {
			disks = append(disks, d)
		}
		s.applyPackEffects(l.w, e, a.Level, body)
		if cd := e.ChangeItemDamage; cd != nil {
			if piece := slices.IndexFunc(pieces, func(p rules.EnchPiece) bool { return p.Slot == a.Slot }); piece >= 0 {
				s.wearPostAttack(l.w, attackBody{mob: m}, piece, int32(rules.EnchLevelValueAt(cd, a.Level)))
			}
		}
	}
	return disks
}
