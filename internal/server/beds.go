package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// Bed clicks; the sleeping itself is in sleep.go.
// vanilla: BedBlock.useWithoutItem, ServerPlayer.startSleepInBed

// removeBedHalf removes one bed half with flags 3. vanilla: Level.removeBlock.
// If the wake's shape update breaks the other half, its loot roll refuses
// the half already taken.
func (s *Server) removeBedHalf(w *world.World, x, y, z int32) {
	old := w.BlockAt(x, y, z)
	if old == airBlockState || !w.SetBlock(x, y, z, airBlockState) {
		return
	}
	s.markChanged(w, x, y, z)
	s.blockChanged(w, x, y, z, old)
}

// destroyStrawBed breaks a straw bed by its rule: the sound, then the head
// set to air, whose shape update takes the foot.
// vanilla: StrawBedBlock.destroyBed
func (s *Server) destroyStrawBed(w *world.World, x, y, z int32) {
	s.broadcastSoundID(w, registry.SoundBlockStrawBedBreakLeave, protocol.SoundCategoryBlock,
		float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, 1, 1)
	s.removeBedHalf(w, x, y, z)
}

// useBed handles a bed click (every one is handled) by the bed rule of its
// block and dimension.
// vanilla: AbstractBedBlock.useWithoutItem
func (c *client) useBed(w *world.World, x, y, z int32, state uint16) (bool, error) {
	// A click retargets the head; a half without its partner does nothing.
	x, z, state, ok := rules.BedHeadOf(w, x, y, z, state)
	if !ok {
		c.useSwing = swingNone
		return true, nil
	}
	block := registry.BlockOf(state)
	rule := rules.BedRuleOf(w.DimensionTypeName(), block)

	if rule.DestroyOnUse {
		if rule.ErrorMessage != "" {
			c.send(protocol.IDPlaySystemChat,
				systemChatPayload(protocol.Text{Translate: rule.ErrorMessage}, true))
		}
		if block == registry.BlockStrawBed {
			c.srv.destroyStrawBed(w, x, y, z)
			return true, nil
		}
		c.explodeBed(w, x, y, z, state)
		return true, nil
	}

	// An occupied bed ends the click: the kick or the refusal is all that
	// happens. This reads the click's state; startSleeping's claim rechecks
	// under s.bedMu and refuses likewise if a racing click took the bed.
	// vanilla: BedBlock.useWithoutItem, kickVillagerOutOfBed
	if registry.StateBool(state, registry.PropOccupied) {
		if !c.srv.kickVillagerFromBed(w, x, y, z) {
			c.send(protocol.IDPlaySystemChat,
				systemChatPayload(protocol.Text{Translate: "block.minecraft.bed.occupied"}, true))
		}
		return true, nil
	}

	dir, known := facingDelta[registry.StateValue(state, registry.PropFacing)]
	if !known {
		return true, nil
	}
	pos := c.player.Position()
	footX, footZ := x-dir[0], z-dir[1]
	// The first refusal is vanilla's answer; the listener may overrule it.
	result := plugin.BedOK
	dark := !w.BrightOutside()
	canSetSpawn := false
	switch {
	case !rules.BedInRange(pos.X, pos.Y, pos.Z, x, y, z, footX, footZ):
		result = plugin.BedTooFarAway
	// vanilla: ServerPlayer.bedBlocked, the space above each half must be free.
	case !rules.BedFreeAt(w, x, y+1, z) || !rules.BedFreeAt(w, footX, y+1, footZ):
		result = plugin.BedObstructed
	default:
		canSetSpawn = rule.CanSetSpawn.Holds(dark)
		if canSetSpawn {
			// "set_spawn" is only sent when the spawn point moved.
			// vanilla: ServerPlayer.setRespawnPosition
			own := c.spawnPoint.Load()
			sameSpawn := own != nil &&
				own.dim == c.dimension().name &&
				own.pos.X == x && own.pos.Y == y && own.pos.Z == z
			c.setRespawn(world.SpawnPos{X: x, Y: y, Z: z}, pos.Yaw, pos.Pitch, c.dimension().name, false)
			if !sameSpawn {
				c.send(protocol.IDPlaySystemChat,
					systemChatPayload(protocol.Text{Translate: "block.minecraft.set_spawn"}, false))
			}
		}
		// The rule's sleep check runs before the monster check.
		if !rule.CanSleep.Holds(dark) {
			result = plugin.BedNotPossibleNow
		} else if c.player.GameMode() != protocol.GameModeCreative && c.monsterNearBed(w, x, y, z) {
			// vanilla: isPreventingPlayerRest, skipped in creative.
			result = plugin.BedNotSafe
		}
	}
	result, ok = c.pluginBedEnter(x, y, z, result)
	if !ok {
		return true, nil
	}
	if result == plugin.BedOK {
		if !c.startSleeping(w, x, y, z, block, canSetSpawn) {
			result = plugin.BedNotSafe + 1
		}
	}
	if key := bedRefusalMessage(result, rule.ErrorMessage); key != "" {
		c.send(protocol.IDPlaySystemChat, systemChatPayload(protocol.Text{Translate: key}, true))
	}
	return true, nil
}

// bedRefusalMessage is the overlay for a sleep attempt's result; ruleMessage
// is the bed rule's own message for a refusal by the dimension. A result past
// BedNotSafe is a bed taken meanwhile.
func bedRefusalMessage(r plugin.BedResult, ruleMessage string) string {
	switch r {
	case plugin.BedTooFarAway:
		return "block.minecraft.bed.too_far_away"
	case plugin.BedObstructed:
		return "block.minecraft.bed.obstructed"
	case plugin.BedNotSafe:
		return "block.minecraft.bed.not_safe"
	case plugin.BedNotPossibleHere, plugin.BedNotPossibleNow:
		return ruleMessage
	case plugin.BedOK:
		return ""
	}
	return "block.minecraft.bed.occupied"
}

// explodeBed removes both halves of a colored bed and explodes it (radius 5,
// bad_respawn_point), outside every lock like the tick's blasts.
// vanilla: BedBlock.destroyOnUse
func (c *client) explodeBed(w *world.World, x, y, z int32, state uint16) {
	c.srv.removeBedHalf(w, x, y, z)
	if d, known := facingDelta[registry.StateValue(state, registry.PropFacing)]; known {
		if foot := w.BlockAt(x-d[0], y, z-d[1]); registry.BlockOf(foot) == registry.BlockOf(state) {
			c.srv.removeBedHalf(w, x-d[0], y, z-d[1])
		}
	}
	c.srv.runExplosion(blast{
		w:          w,
		x:          float64(x) + 0.5,
		y:          float64(y) + 0.5,
		z:          float64(z) + 0.5,
		radius:     5,
		damageType: rules.DamageTypeBadRespawnPoint,
		rng:        c.srv.lootRng,
		fire:       true,
		// The block_explosion_drop_decay rule applies.
		interaction: rules.BlockExplosionInteraction(c.srv.world),
		source:      &blastSourceBlock{x: x, y: y, z: z, state: state},
	})
}

// kickVillagerFromBed wakes a villager sleeping in the bed head and frees the
// bed, reporting whether there was one.
func (s *Server) kickVillagerFromBed(w *world.World, x, y, z int32) bool {
	herd := s.herdOf(w)
	s.mobsMu.Lock()
	var ops []entity.BedOp
	for m := range herd.book {
		if m.BedHeadSleeper(x, y, z) {
			m.WakeSleeper()
			ops = append(ops, m.TakeBedOps()...)
		}
	}
	s.mobsMu.Unlock()
	for _, op := range ops {
		s.setBedOccupied(w, op.X, op.Y, op.Z, op.Occupied)
	}
	return len(ops) > 0
}
