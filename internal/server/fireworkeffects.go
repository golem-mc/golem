package server

import (
	"cmp"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"math"
	"slices"
)

// burstMobsLocked applies one burst while fireworksMu and mobsMu are held.
// vanilla: FireworkRocketEntity.dealExplosionDamage
func (s *Server) burstMobsLocked(mobHits []thrownMobHit, burst fireworkBurst, clients []*client) []thrownMobHit {
	amount := rules.FireworkBurstDamage(burst.explosions)
	if amount <= 0 {
		return mobHits
	}
	dim := s.dimensionOfWorld(burst.it.w)
	box := burst.it.rocket.Box().Inflate(rules.FireworkBurstRadius)
	minX, minY, minZ, maxX, maxY, maxZ := box.Corners()
	bounds := [6]float64{minX, minY, minZ, maxX, maxY, maxZ}
	area, indexed := smallChunkArea(&bounds)
	// Damage listeners can move or remove bodies while the locks are released.
	var nearbyBuffer [16]*entity.Mob
	nearby := nearbyBuffer[:0]
	dim.mobs.mobsInArea(area, indexed, func(m *entity.Mob) bool {
		if box.Crosses(entity.NewAABB(m.BlastBox())) {
			nearby = append(nearby, m)
		}
		return true
	})
	slices.SortFunc(nearby, func(a, b *entity.Mob) int { return cmp.Compare(a.ID, b.ID) })
	var owner *client
	for _, c := range clients {
		if c.player != nil && c.player.EntityID == burst.it.ownerID {
			owner = c
			break
		}
	}
	shooter := func() *entity.Mob {
		if source := s.mobByEntityIDLocked(burst.it.w, burst.it.ownerID); source != nil {
			return source.mob
		}
		return nil
	}
	for _, m := range nearby {
		if dim.mobs.record(m) == nil {
			continue
		}
		d := math.Sqrt(entity.Dist2(burst.x, burst.y, burst.z, m.X, m.Y, m.Z))
		if !rules.FireworkBurstInBurstRange(d) {
			continue
		}
		minX, minY, minZ, maxX, maxY, maxZ := m.BlastBox()
		if !rules.FireworkBurstVisible(burst.it.w,
			burst.x, burst.y, burst.z, minX, minY, minZ, maxX, maxY, maxZ) {
			continue
		}
		from := ownerHitSource(m, owner, shooter)
		landed, softened := m.HurtByFirework(rules.FireworkBurstFalloffDamage(amount, d),
			rules.DamageTypeFireworks, burst.it.ownerID,
			owner != nil, creativeClient(owner),
			burst.x, burst.z, from)
		if !landed {
			continue
		}
		hit := thrownMobHit{
			damageType: rules.DamageTypeFireworks,
			w:          burst.it.w,
			victimID:   m.ID,
			kind:       m.Type,
			sound:      mobSound(m, true),
			volume:     m.SoundVolume(),
			pitch:      m.VoicePitch(),
			silent:     m.Silent,
			causeID:    causeEntityID(&damageCause{entityID: burst.it.ownerID}),
			directID:   causeEntityID(&damageCause{entityID: burst.it.rocket.ID}),
			x:          m.X, y: m.Y, z: m.Z,
			vx: m.VX, vy: m.VY, vz: m.VZ,
			softened: softened,
		}
		if ent := dim.mobs.book[m]; ent != nil {
			hit.health = ent.syncHealth()
			hit.watchers = trackedWatchers(ent.tracked)
		}
		mobHits = append(mobHits, hit)
	}
	return mobHits
}

func (s *Server) burstArmorStands(bursts []fireworkBurst) {
	// A burst breaks any visible armor stand in reach outright, whatever the
	// amount: the fireworks damage type is in is_explosion. An invulnerable
	// stand is spared unless the burst is a creative player's. The stands
	// lock is taken alone, and each burst's creative source read before it,
	// the read taking the server lock.
	// vanilla: ArmorStand.hurtServer
	burstCreative := make([]bool, len(bursts))
	for i, burst := range bursts {
		if owner := s.entityClient(burst.it.ownerID); owner != nil {
			burstCreative[i] = owner.player.GameMode() == protocol.GameModeCreative
		}
	}
	s.armorStandsMu.Lock()
	var goneStands []*armorStandEntity
	for i, burst := range bursts {
		if rules.FireworkBurstDamage(burst.explosions) <= 0 {
			continue
		}
		for _, ent := range s.armorStands {
			if ent.w != burst.it.w || ent.stand.Dead || ent.stand.Invisible || ent.stand.Marker {
				continue
			}
			if ent.stand.InvulnerableTo(rules.DamageTypeFireworks, burstCreative[i]) {
				continue
			}
			d := math.Sqrt(entity.Dist2(burst.x, burst.y, burst.z, ent.stand.X, ent.stand.Y, ent.stand.Z))
			if !rules.FireworkBurstInBurstRange(d) {
				continue
			}
			minX, minY, minZ, maxX, maxY, maxZ := ent.stand.BlastBox()
			if !rules.FireworkBurstVisible(burst.it.w,
				burst.x, burst.y, burst.z, minX, minY, minZ, maxX, maxY, maxZ) {
				continue
			}
			// The stand stays in the list until the walk is over, so a
			// second burst in reach of it must not mark it twice.
			if slices.Contains(goneStands, ent) {
				continue
			}
			goneStands = append(goneStands, ent)
		}
	}
	// Removed after the walk: DeleteFunc shifts the tail down under the
	// ranged slice and would leave a vacated slot read as a nil stand.
	for _, ent := range goneStands {
		s.removeArmorStandLocked(ent)
	}
	s.armorStandsMu.Unlock()
	for _, ent := range goneStands {
		s.breakArmorStandTail(ent, armorStandBreak{equipment: true})
	}

}
