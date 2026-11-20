package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// Buckets pick up and pour water and lava sources, both via Use Item
// (vanilla BucketItem has no useOn): the empty bucket takes the first fluid
// source or waterlogged block the ray meets, the filled one pours where the
// ray stops. Each change is an ordinary block change (broadcast, relit,
// handed to physics), so fluids flow and stop. Milking is not modelled here.

// fluidKind is a bucket fluid: source state, bucket item, fill and empty
// sounds.
type fluidKind struct {
	block      registry.Block
	source     uint16
	item       registry.Item
	fillSound  registry.Sound
	emptySound registry.Sound
	// neutral plays the empty sound in the neutral category, not block.
	// vanilla: MobBucketItem.playEmptySound
	neutral bool
}

// emptyCategory is the sound category of the empty sound.
func (f fluidKind) emptyCategory() int32 {
	if f.neutral {
		return protocol.SoundCategoryNeutral
	}
	return protocol.SoundCategoryBlock
}

// fluids are in vanilla's fluid order, water first, via MustState since
// state IDs move between versions.
var fluids = []fluidKind{
	{
		block:      registry.BlockWater,
		source:     registry.BlockWater.DefaultState(),
		item:       registry.ItemWaterBucket,
		fillSound:  registry.SoundItemBucketFill,
		emptySound: registry.SoundItemBucketEmpty,
	},
	{
		block:      registry.BlockLava,
		source:     registry.BlockLava.DefaultState(),
		item:       registry.ItemLavaBucket,
		fillSound:  registry.SoundItemBucketFillLava,
		emptySound: registry.SoundItemBucketEmptyLava,
	},
}

// mobBucketPour is a fish bucket's water, with the mob bucket's empty sound
// in the neutral category. vanilla: MobBucketItem.playEmptySound
func mobBucketPour(item registry.Item) fluidKind {
	pour := fluids[0]
	pour.emptySound = registry.SoundItemBucketEmptyFish
	if item == registry.ItemAxolotlBucket {
		pour.emptySound = registry.SoundItemBucketEmptyAxolotl
	}
	if item == registry.ItemTadpoleBucket {
		pour.emptySound = registry.SoundItemBucketEmptyTadpole
	}
	pour.neutral = true
	return pour
}

// fluidOf is the bucket fluid whose block is b.
func fluidOf(b registry.Block) (fluidKind, bool) {
	for _, fluid := range fluids {
		if fluid.block == b {
			return fluid, true
		}
	}
	return fluidKind{}, false
}

// handleUseBucket is Use Item for a bucket: empty takes the first fluid
// source or waterlogged block the ray meets, draining it and refilling the
// hand; filled pours a source where the ray stops, emptying the hand via the
// filled result. vanilla: BucketItem.use, Item.getPlayerPOVHitResult
func (c *client) handleUseBucket(hand int32) bool {
	held := c.heldItem(hand)
	if held.Empty() {
		return false
	}
	heldItem := registry.Item(held.ItemID)
	var pour fluidKind
	for _, fluid := range fluids {
		if fluid.item == heldItem {
			pour = fluid
			break
		}
	}
	if registry.Item(held.ItemID) == registry.ItemSulfurCubeBucket {
		return c.emptySulfurCubeBucket(hand, held)
	}
	if _, mob := entity.BucketMobKind(registry.Item(held.ItemID)); mob {
		pour = mobBucketPour(heldItem)
	}
	w := c.theWorld()
	pos := c.player.Position()
	// The eye is at the pose's height, lower when sneaking.
	fx, fy, fz := pos.X, pos.Y+c.dims(c.player.Appearance().Pose).EyeHeight, pos.Z
	dirX, dirY, dirZ := entity.LookVector(pos.Yaw, pos.Pitch)
	reach := c.blockRange()
	if pour.source != 0 {
		return c.pourBucket(w, pour, fx, fy, fz, dirX*reach, dirY*reach, dirZ*reach, hand, held)
	}
	if registry.Item(held.ItemID) != registry.ItemBucket {
		return false
	}
	hit, ok := w.ClipBlocks(fx, fy, fz, dirX*reach, dirY*reach, dirZ*reach, rules.BucketScoopable)
	if !ok {
		return false
	}
	// The clicked block gates the use: adventure only scoops where can_place_on
	// allows. vanilla: BucketItem.use, Player.mayUseItemAt
	if !rules.MayUseItemAt(c.player.GameMode(), held, adventureTarget(w, hit.X, hit.Y, hit.Z, hit.State)) || !c.mayInteractAt(hit.X, hit.Z) {
		return false
	}
	target, fluidBlock, ok := rules.BucketScoopTarget(hit.State)
	if !ok {
		return false
	}
	pickup := pickupFor(fluidBlock)
	filled := protocol.ItemStack{ItemID: int32(pickup.item), Count: 1}
	filled, ok = c.pluginBucket(hand, true, hit.X, hit.Y, hit.Z, hit.Face, held, filled)
	if !ok {
		return false
	}
	c.srv.pickUpBlock(w, hit.X, hit.Y, hit.Z, target, pickup)
	c.gameEventAtCell(rules.GameEventFluidPickup, hit.X, hit.Y, hit.Z)
	// At the player's feet, in the Players category. vanilla: BucketItem.use, Player.playSound
	c.srv.broadcastSoundIDExcept(w, c,
		pickup.sound, protocol.SoundCategoryPlayer, pos.X, pos.Y, pos.Z, 1.0, 1.0)
	// vanilla: BucketItem.use
	c.awardItemStat(registry.StatTypeUsed, held.ItemID, 1)
	c.filledResult(hand, filled, true)
	c.filledBucketAdvancements(filled)
	return true
}

// pourBucket places the fluid source where the ray stops: in the hit cell
// if rules.CanPlaceLiquid accepts it, else beyond the face the ray entered,
// waterlogging that cell when it accepts the water.
// A target already holding that source still empties the hand. Where the
// dimension evaporates water, that happens instead.
// vanilla: BucketItem.use, BucketItem.emptyContents
func (c *client) pourBucket(w *world.World, pour fluidKind, fx, fy, fz, dx, dy, dz float64, hand int32, held protocol.ItemStack) bool {
	hit, ok := w.ClipBlocks(fx, fy, fz, dx, dy, dz, nil)
	if !ok {
		return false
	}
	// The clicked block gates the pour as it gates the scoop.
	// vanilla: Player.mayUseItemAt
	// vanilla: BucketItem.use, Level.mayInteract
	if !rules.MayUseItemAt(c.player.GameMode(), held, adventureTarget(w, hit.X, hit.Y, hit.Z, hit.State)) || !c.mayInteractAt(hit.X, hit.Z) {
		return false
	}
	nx, ny, nz, state := hit.X, hit.Y, hit.Z, pour.source
	hitBlock := false
	if logged, ok := rules.CanPlaceLiquid(pour.block, hit.State, c.player.Appearance().Sneaking,
		c.player.GameMode() == protocol.GameModeCreative); ok {
		state, hitBlock = logged, true
	} else {
		offset := protocol.FaceOffset[hit.Face]
		nx, ny, nz = hit.X+offset[0], hit.Y+offset[1], hit.Z+offset[2]
	}
	old := w.BlockAt(nx, ny, nz)
	// The neighbour cell takes the water into itself too; the sneak no longer
	// matters there. vanilla: BucketItem.emptyContents (hitResult null)
	if !hitBlock {
		if logged, ok := rules.CanPlaceLiquid(pour.block, old, false,
			c.player.GameMode() == protocol.GameModeCreative); ok {
			state, hitBlock = logged, true
		}
	}
	if !hitBlock && old != state && !rules.CanBeReplacedByFluid(old) {
		return false
	}
	// The creative hand keeps the filled bucket and gains another.
	// vanilla: getEmptySuccessItem
	result := protocol.ItemStack{ItemID: int32(registry.ItemBucket), Count: 1}
	if c.player.GameMode() == protocol.GameModeCreative {
		result = held
	}
	result, ok = c.pluginBucket(hand, false, nx, ny, nz, hit.Face, held, result)
	if !ok {
		return false
	}
	// Where water evaporates, the source never lands: the hiss plays and the
	// hand still empties. vanilla: BucketItem.emptyContents
	if pour.block == registry.BlockWater && w.WaterEvaporates() {
		c.srv.evaporateWater(w, nx, ny, nz, c)
	} else if old != state {
		if !hitBlock {
			c.srv.dropWashedOutBlock(w, nx, ny, nz, old)
		}
		if hitBlock {
			// vanilla: BucketItem.emptyContents: the block's placeLiquid sound plays
			// before the bucket's.
			c.srv.waterlogEffects(w, old, nx, ny, nz)
		}
		c.bucketChange(nx, ny, nz, state, pour.emptySound, pour.emptyCategory())
	}
	// A fish bucket still releases its fish where water evaporates, and posts
	// only entity_place: MobBucketItem.playEmptySound posts nothing.
	// vanilla: BucketItem.use, MobBucketItem.checkExtraContent
	if c.srv.releaseBucketMob(c.dimension(), held, float64(nx)+0.5, float64(ny), float64(nz)+0.5) {
		c.gameEventAtCell(rules.GameEventEntityPlace, nx, ny, nz)
	} else if pour.block != registry.BlockWater || !w.WaterEvaporates() {
		// vanilla: BucketItem.playEmptySound; not reached where water evaporates
		c.gameEventAtCell(rules.GameEventFluidPlace, nx, ny, nz)
	}
	// vanilla: BucketItem.use
	c.placedBlockAdvancements(w, nx, ny, nz, held)
	c.awardItemStat(registry.StatTypeUsed, held.ItemID, 1)
	c.filledResult(hand, result, true)
	return true
}

// evaporateWater is the nether's water pour: no source, just the
// extinguish hiss from the cell and eight large smoke at its corner, which
// everyone sees. except is the pourer whose client played the hiss, nil for
// everyone. vanilla: BucketItem.emptyContents
func (s *Server) evaporateWater(w *world.World, x, y, z int32, except *client) {
	s.broadcastSoundIDExcept(w, except,
		registry.SoundBlockFireExtinguish, protocol.SoundCategoryBlock,
		float64(x)+0.5, float64(y)+0.5, float64(z)+0.5,
		0.5, 2.6+(s.soundRng.Float32()-s.soundRng.Float32())*0.8)
	px, py, pz := float64(x), float64(y), float64(z)
	s.sendParticlePayload(w, motionParticlesPayload(protocol.ParticleLargeSmoke, px, py, pz,
		1, 1, 1, 0, 0, 0, 8, particleRandomAlternative), px, py, pz)
}

// bucketChange sets the block and propagates it; the sound goes to all but
// the clicker, whose client predicted it.
func (c *client) bucketChange(x, y, z int32, state uint16, sound registry.Sound, category int32) {
	w := c.theWorld()
	old := w.BlockAt(x, y, z)
	if !w.SetBlock(x, y, z, state) {
		return
	}
	c.srv.markChanged(w, x, y, z)
	c.srv.relightAsync(w, x, z)
	c.srv.broadcastSoundIDExcept(w, c,
		sound, category, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, 1.0, 1.0)

	c.srv.blockChanged(w, x, y, z, old)
}

// pourFluidAt pours a source for a dispenser if the block takes fluid,
// dropping what it washes out. The same source already there still counts
// as a pour with sound. A waterloggable block takes the water into its own
// cell; the dispenser's nil user never sneaks and is never creative. It
// reports whether it poured.
// vanilla: DispenserBlock's bucket behavior, BucketItem.emptyContents
func (s *Server) pourFluidAt(w *world.World, x, y, z int32, fluid fluidKind) bool {
	old := w.BlockAt(x, y, z)
	same := old == fluid.source
	logged, ownCell := rules.CanPlaceLiquid(fluid.block, old, false, false)
	if !same && !ownCell && !rules.CanBeReplacedByFluid(old) {
		return false
	}
	if fluid.block == registry.BlockWater && w.WaterEvaporates() {
		s.evaporateWater(w, x, y, z, nil)
		return true
	}
	state := fluid.source
	if same {
		state = old
	} else if ownCell {
		state = logged
	} else {
		s.dropWashedOutBlock(w, x, y, z, old)
	}
	if state != old {
		if !w.SetBlock(x, y, z, state) {
			return false
		}
		s.markChanged(w, x, y, z)
		s.relightAsync(w, x, z)
		if ownCell {
			s.waterlogEffects(w, old, x, y, z)
		}
	}
	s.broadcastSoundID(w,
		fluid.emptySound, fluid.emptyCategory(),
		float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, 1.0, 1.0)

	if !same {
		s.blockChanged(w, x, y, z, old)
	}
	return true
}

// waterlogEffects emits a container's placeLiquid sound and extinguish event.
// vanilla: CampfireBlock.placeLiquid, CandleBlock.placeLiquid, DriedGhastBlock.placeLiquid
func (s *Server) waterlogEffects(w *world.World, old uint16, x, y, z int32) {
	_, sound, _ := blocklogic.PlaceWater(old)
	if sound == registry.SoundNone {
		return
	}
	s.broadcastSoundID(w, sound, protocol.SoundCategoryBlock,
		float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, 1, 1)
	if registry.StateLit(old) {
		s.gameEvent(w, rules.GameEventBlockChange, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, rules.GameEventContext{})
	}
}

// dropWashedOutBlock drops the loot of a block a poured source replaces,
// rolled before the source is written so a double plant's halves still find
// each other. vanilla: BucketItem.emptyContents, Level.destroyBlock
func (s *Server) dropWashedOutBlock(w *world.World, x, y, z int32, state uint16) {
	if state == airBlockState {
		return
	}
	s.rollBlockLoot(w, x, y, z, state, &rules.LootContext{
		BlockState: state,
		HasBlock:   true,
		BlockAt: func(dx, dy, dz int32) uint16 {
			return w.BlockAt(x+dx, y+dy, z+dz)
		},
		Level: s.lootLevelAtBlock(w, x, y, z),
	})
}

// bucketPickup is what an empty bucket takes from a block: the filled
// bucket and the sound.
type bucketPickup struct {
	block registry.Block
	item  registry.Item
	sound registry.Sound
}

// pickupFor is the pickup for BucketScoopTarget's block: a bucket fluid or
// powder snow.
func pickupFor(b registry.Block) bucketPickup {
	if b == registry.BlockPowderSnow {
		return bucketPickup{b, registry.ItemPowderSnowBucket, registry.SoundItemBucketFillPowderSnow}
	}
	fluid, _ := fluidOf(b)
	return bucketPickup{b, fluid.item, fluid.fillSound}
}

// pickUpBlock writes the block a pickup leaves and propagates it, for
// player and dispenser alike. Powder snow breaks with its break event; the
// sound is not played here, since the player's goes to all but the clicker.
// vanilla: BucketPickup.pickupBlock, PowderSnowBlock.pickupBlock
func (s *Server) pickUpBlock(w *world.World, x, y, z int32, after uint16, pickup bucketPickup) bool {
	old := w.BlockAt(x, y, z)
	if !w.SetBlock(x, y, z, after) {
		return false
	}
	s.markChanged(w, x, y, z)
	s.relightAsync(w, x, z)
	if pickup.block == registry.BlockPowderSnow {
		s.broadcastLevelEvent(w, protocol.WorldEventBlockBreak, x, y, z, int32(old))
	}
	s.blockChanged(w, x, y, z, old)
	return true
}

// replaceHeldWith puts stack in the hand and sends the slot, since the
// client waits for the server's confirmation, then broadcasts equipment.
func (c *client) replaceHeldWith(hand int32, stack protocol.ItemStack) {
	var payload []byte
	c.player.Inventory(func(inv *player.Inventory) {
		slot := player.HotbarSlot + int(inv.Selected)
		if hand == protocol.HandOff {
			slot = player.OffHandSlot
		}
		inv.Set(slot, stack)
		payload = inv.WindowSlot(playerWindowID, int32(slot), inv.Slots[slot])
	})
	if err := c.writePacket(protocol.IDPlayContainerSlot, "set container slot", payload); err != nil {
		c.log.Debug("swapping a held bucket failed", "error", err)
	}
	c.broadcastEquipment()
}
