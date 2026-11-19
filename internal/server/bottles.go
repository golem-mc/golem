package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/advancement"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// handleUseBottle fills a bottle from a nearby dragon-owned cloud or water.
// vanilla: BottleItem.use
func (c *client) handleUseBottle(hand int32) bool {
	held := c.heldItem(hand)
	if held.Empty() || registry.Item(held.ItemID) != registry.ItemGlassBottle {
		return false
	}
	// A spectator's use stops before the item's own.
	// vanilla: ServerPlayerGameMode.useItem
	if c.player.GameMode() == protocol.GameModeSpectator {
		return false
	}
	w := c.theWorld()
	pos := c.player.Position()
	if c.fillDragonBreath(hand) {
		return true
	}
	// The eye is at the pose's height, lower when sneaking.
	fx, fy, fz := pos.X, pos.Y+c.dims(c.player.Appearance().Pose).EyeHeight, pos.Z
	dirX, dirY, dirZ := entity.LookVector(pos.Yaw, pos.Pitch)
	reach := c.blockRange()
	// The ray stops on any fluid source: water and lava, plus kelp, seagrass
	// and bubble columns, whose cells hold water; the check below rejects
	// non-water. vanilla: BottleItem.use, ClipContext.Fluid.SOURCE_ONLY
	hit, ok := w.ClipBlocks(fx, fy, fz, dirX*reach, dirY*reach, dirZ*reach, world.HoldsFluidSource)
	if !ok {
		return false
	}
	// vanilla: BottleItem.use, Level.mayInteract
	if !c.mayInteractAt(hit.X, hit.Z) || !world.HoldsWater(hit.State) {
		return false
	}
	// The sound plays to all but the filler, whose client predicts it.
	// vanilla: BottleItem.use, Level.playSound
	c.srv.broadcastSoundIDExcept(w, c,
		registry.SoundItemBottleFill, protocol.SoundCategoryNeutral,
		pos.X, pos.Y, pos.Z, 1.0, 1.0)
	c.gameEventAtCell(rules.GameEventFluidPickup, hit.X, hit.Y, hit.Z)

	// vanilla: BottleItem.turnBottleIntoItem
	c.awardItemStat(registry.StatTypeUsed, held.ItemID, 1)
	if water, ok := waterBottle(); ok {
		c.filledResult(hand, water, true)
	}
	return true
}

// waterBottle is one potion item holding the water potion.
func waterBottle() (protocol.ItemStack, bool) {
	potion := int32(registry.ItemPotion)
	water := protocol.ItemStack{ItemID: potion, Count: 1}
	water.SetPotion(registry.MustPotion("minecraft:water"))
	return water, true
}

// vanilla: BottleItem.use checks clouds before tracing the fluid ray.
func (c *client) fillDragonBreath(hand int32) bool {
	s, w, pos := c.srv, c.theWorld(), c.player.Position()
	dims := c.dims(c.player.Appearance().Pose)
	reach := dims.Width/2 + 2
	s.arrowsMu.Lock()
	s.mobsMu.Lock()
	found := false
	for _, it := range s.clouds {
		cloud := it.cloud
		if it.w != w || cloud.Dead {
			continue
		}
		owner := s.dimensionOfWorld(w).mobs.byUUID[it.ownerUUID]
		if owner != nil && owner.Type == entity.TypeEnderDragon {
			it.ownerDragon = true
		}
		if !it.ownerDragon {
			continue
		}
		r := float64(cloud.Radius())
		if cloud.X+r <= pos.X-reach || cloud.X-r >= pos.X+reach || cloud.Y+0.5 <= pos.Y-2 || cloud.Y >= pos.Y+dims.Height+2 || cloud.Z+r <= pos.Z-reach || cloud.Z-r >= pos.Z+reach {
			continue
		}
		cloud.Restore(cloud.TickCount, cloud.Radius()-0.5, cloud.RadiusPerTick())
		found = true
		break
	}
	s.mobsMu.Unlock()
	s.arrowsMu.Unlock()
	if !found {
		return false
	}
	s.broadcastSoundID(w, registry.SoundItemBottleFillDragonbreath, protocol.SoundCategoryNeutral, pos.X, pos.Y, pos.Z, 1, 1)
	s.gameEvent(w, rules.GameEventFluidPickup, pos.X, pos.Y, pos.Z, c.gameEventContext(0))
	c.interactedWithEntityAdvancements(advancement.EntityInteraction{Type: "minecraft:area_effect_cloud"}, int32(registry.ItemGlassBottle))
	c.awardItemStat(registry.StatTypeUsed, int32(registry.ItemGlassBottle), 1)
	c.filledResult(hand, protocol.ItemStack{ItemID: int32(registry.ItemDragonBreath), Count: 1}, true)
	return true
}
