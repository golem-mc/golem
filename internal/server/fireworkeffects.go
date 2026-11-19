package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"math"
	"slices"
)

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
