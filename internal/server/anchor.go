package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The respawn anchor: glowstone charging, the nether spawn point a charged
// use sets, the blast elsewhere, the respawn spot, and each respawn's spent
// charge.

const (
	anchorMaxCharges = rules.AnchorMaxCharges
)

// anchorState reads the block at the position as the anchor it may be.
func anchorState(w *world.World, x, y, z int32) (state uint16, charges int, ok bool) {
	state = w.BlockAt(x, y, z)
	charges, ok = rules.AnchorState(state)
	return state, charges, ok
}

// chargeAnchor adds a charge to the anchor and plays its sound; props are
// its state, charges its count. It reports whether the raised
// state was found and written. vanilla: RespawnAnchorBlock.charge
func (s *Server) chargeAnchor(w *world.World, x, y, z int32, state uint16, charges int, ctx rules.GameEventContext) bool {
	after, found := rules.AnchorWithCharges(state, charges+1)
	if !found {
		return false
	}
	s.setBlockAndTell(w, x, y, z, after)
	ctx.Affected = after
	s.gameEvent(w, rules.GameEventBlockChange, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, ctx)
	s.broadcastSoundID(w,
		registry.SoundBlockRespawnAnchorCharge,
		protocol.SoundCategoryBlock, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, 1.0, 1.0)

	return true
}

// useRespawnAnchorCharge charges an anchor with one held glowstone in any
// dimension, reporting whether the click was spent. A full anchor declines,
// falling through to the block's use. vanilla: RespawnAnchorBlock.useItemOn, charge
func (c *client) useRespawnAnchorCharge(hand, x, y, z int32) bool {
	w := c.theWorld()
	state, charges, ok := anchorState(w, x, y, z)
	if !ok {
		return false
	}
	if held := c.heldItem(hand); !rules.AnchorCanCharge(charges, !held.Empty() && held.ItemID == int32(registry.ItemGlowstone)) {
		return false
	}
	if !c.srv.chargeAnchor(w, x, y, z, state, charges, c.gameEventContext(0)) {
		return false
	}
	c.spendHeld(hand)
	return true
}

// useRespawnAnchor is the anchor's use: charged, it sets the spawn in the
// nether and explodes elsewhere; empty, it does nothing. A main-hand click
// with glowstone in the off hand skips the anchor, leaving charging to the
// off hand's click. vanilla: RespawnAnchorBlock.useItemOn, useWithoutItem
func (c *client) useRespawnAnchor(hand, x, y, z int32) (bool, error) {
	w := c.theWorld()
	_, charges, ok := anchorState(w, x, y, z)
	if !ok {
		return false, nil
	}
	off := c.heldItem(protocol.HandOff)
	own := c.spawnPoint.Load()
	sameSpawn := own != nil && own.dim == c.dimension().name &&
		own.pos.X == x && own.pos.Y == y && own.pos.Z == z
	// If this is already the respawn point, vanilla consumes the click and
	// changes nothing, keeping a forced point and its angles.
	// vanilla: RespawnAnchorBlock.useWithoutItem
	switch rules.AnchorUse(charges, hand == protocol.HandMain,
		!off.Empty() && off.ItemID == int32(registry.ItemGlowstone), w.RespawnAnchorWorks(), sameSpawn) {
	case rules.AnchorUsePass:
		return false, nil
	case rules.AnchorUseBlast:
		c.useSwing = swingServer
		c.blastAnchor(w, x, y, z)
		return true, nil
	case rules.AnchorUseKeepPoint:
		c.useSwing = swingNone
		return true, nil
	}
	// vanilla: RespawnData.of, the anchor's point with no angle; a respawn
	// faces the body toward the anchor.
	c.useSwing = swingServer
	c.setRespawn(world.SpawnPos{X: x, Y: y, Z: z}, 0, 0, c.dimension().name, false)
	// vanilla: ServerPlayer.setRespawnPosition
	c.send(protocol.IDPlaySystemChat,
		systemChatPayload(protocol.Text{Translate: "block.minecraft.set_spawn"}, false))
	c.srv.broadcastSoundID(w,
		registry.SoundBlockRespawnAnchorSetSpawn,
		protocol.SoundCategoryBlock, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, 1.0, 1.0)

	return true, nil
}

// blastAnchor removes a charged anchor and explodes at the hole, for a
// charged use outside the nether; the anchor drops nothing.
// vanilla: RespawnAnchorBlock.explode
func (c *client) blastAnchor(w *world.World, x, y, z int32) {
	state := w.BlockAt(x, y, z)
	if state == airBlockState || !w.SetBlock(x, y, z, airBlockState) {
		return
	}
	c.srv.markChanged(w, x, y, z)
	c.srv.blockChanged(w, x, y, z, state)
	b := blast{
		w:          w,
		x:          float64(x) + 0.5,
		y:          float64(y) + 0.5,
		z:          float64(z) + 0.5,
		radius:     5,
		damageType: rules.DamageTypeBadRespawnPoint,
		rng:        c.srv.lootRng,
		fire:       true,
		// The block_explosion_drop_decay rule applies like a bed's.
		interaction: rules.BlockExplosionInteraction(c.srv.world),
		source:      &blastSourceBlock{x: x, y: y, z: z, state: state},
	}
	if rules.AnchorFlooded(w, x, y, z) {
		cell := [3]int32{x, y, z}
		resistance := registry.BlockWater.ExplosionResistance()
		b.blockResist = func(bx, by, bz int32) (float64, bool) {
			return resistance, bx == cell[0] && by == cell[1] && bz == cell[2]
		}
	}
	c.srv.runExplosion(b)
}

// anchorAnswer is what a nether spawn point's block is for a respawn: an
// anchor hosting the body, an anchor refusing, or not an anchor.
type anchorAnswer uint8

const (
	anchorAbsent anchorAnswer = iota
	anchorHosted
	anchorRefused
)

// anchorRespawn resolves a nether spawn point at an anchor: the first free
// cell around it, facing it, spending a charge when the respawn consumes
// its block and the point is not forced. No charges, or no free cell,
// refuses without spending.
// vanilla: ServerPlayer.findRespawnAndUseSpawnBlock
func (c *client) anchorRespawn(own *respawnPos, consume bool) (respawnPlacement, anchorAnswer) {
	w := c.srv.dimensionByName(world.NetherName).w
	// Hold the chunks before reading the anchor: BlockAt reads air for an
	// unloaded chunk, so an anchor unloaded by walking away would look gone.
	// The walk reaches one cell out.
	release := holdChunksAround(w, own.pos.X, own.pos.Z, 1)
	defer release()
	state, charges, ok := anchorState(w, own.pos.X, own.pos.Y, own.pos.Z)
	if !ok {
		return respawnPlacement{}, anchorAbsent
	}
	if rules.AnchorRefusesRespawn(charges, own.forced) {
		return respawnPlacement{}, anchorRefused
	}
	px, py, pz, found := rules.AnchorStandUpSpot(w, own.pos.X, own.pos.Y, own.pos.Z)
	if !found {
		return respawnPlacement{}, anchorRefused
	}
	if consume && !own.forced {
		if after, found := rules.AnchorWithCharges(state, charges-1); found {
			c.srv.setBlockAndTell(w, own.pos.X, own.pos.Y, own.pos.Z, after)
		}
	}
	return respawnPlacement{
		x:   px,
		y:   py,
		z:   pz,
		yaw: rules.LookAtYaw(px, pz, own.pos.X, own.pos.Z),
	}, anchorHosted
}

// anchorDepletePayload is the Sound Effect a respawn ends with while its
// point is still an anchor: the spent charge's sound, to the new body only;
// nil when no anchor is there. vanilla: PlayerList.respawn
func (c *client) anchorDepletePayload() []byte {
	own := c.spawnPoint.Load()
	if own == nil {
		return nil
	}
	w := c.srv.dimensionByName(own.dim).w
	if _, ok := rules.AnchorState(w.BlockAt(own.pos.X, own.pos.Y, own.pos.Z)); !ok {
		return nil
	}
	return soundEffectIDAt(registry.SoundBlockRespawnAnchorDeplete,
		protocol.SoundCategoryBlock,
		float64(own.pos.X), float64(own.pos.Y), float64(own.pos.Z), 1.0, 1.0)
}
