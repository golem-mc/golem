package server

import (
	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/inventory"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// The dispense visit the redstone engine's rising edge hands the server:
// decisions are package inventory's (dispense.go); this carries them out
// and tells clients.

// dispenseVisit is a visit's decision, made under the dispenser's lock: the
// slot the dice picked and the one item taken from it, removed before the
// act so act and commit see the slot without it. gone means the entity left
// the world between lookup and lock; empty means nothing to dispense, which
// still clicks (see runDispense).
type dispenseVisit struct {
	gone      bool
	empty     bool
	slot      int
	taken     protocol.ItemStack
	behaviour inventory.DispenseBehaviour
}

// dispenseAct is what the act, run without the dispenser's lock, leaves for
// the commit: the stack the slot is owed (the kept item, a swapped bucket,
// or nothing if spent) and a dropper's target container, synced by the
// caller.
type dispenseAct struct {
	back   protocol.ItemStack
	pushed containerTarget
}

// runDispense is one dispense visit; a container without a block entity
// dispenses nothing.
//
// The dispenser's lock never reaches the engine: decide under it, act
// without it, commit under it again. A pour's block change and bone meal's
// growth wait for the engine's lock, and a comparator reading this
// dispenser holds the engine's lock while waiting for the dispenser's, so
// carrying one into the other would deadlock. Leftovers and viewer syncs
// also wait for the lock to drop (a sync retakes it); what no slot takes is
// dropped.
func (s *Server) runDispense(w *world.World, d world.Dispense) {
	block := registry.BlockOf(w.BlockAt(d.X, d.Y, d.Z))
	if block == registry.BlockCrafter {
		s.runCrafter(w, d)
		return
	}
	dropper := block == registry.BlockDropper
	if !dropper && block != registry.BlockDispenser {
		return
	}
	be := w.BlockEntity(d.X, d.Y, d.Z)
	if be == nil {
		return
	}
	if decideHook := s.dispenseDecideHook; decideHook != nil {
		decideHook()
	}
	dice := s.dispenseRng

	var visit dispenseVisit
	be.Use(func(b *world.BlockEntity) {
		visit = s.decideDispense(w, b, dice)
	})
	// A dispenser gone between lookup and lock dispenses nothing, silently:
	// vanilla's dispenseFrom returns quietly when the block entity is missing,
	// and 1001 is only an empty one's click.
	if visit.gone {
		return
	}
	if visit.empty {
		s.broadcastLevelEvent(w,
			protocol.WorldEventDispenseFail, d.X, d.Y, d.Z, 0)
		// vanilla: DispenserBlock.dispenseFrom
		s.gameEvent(w, world.GameEventBlockActivate, float64(d.X)+0.5, float64(d.Y)+0.5, float64(d.Z)+0.5,
			rules.GameEventContext{Affected: w.BlockAt(d.X, d.Y, d.Z)})
		return
	}

	if dispenseActHook := s.dispenseActHook; dispenseActHook != nil {
		dispenseActHook()
	}
	var act dispenseAct
	if cancelled := s.pluginDispense(w, &visit, &d, dropper); cancelled {
		// The item goes back to its slot; nothing fires and nothing sounds.
		act.back = visit.taken
	} else {
		act = s.dispenseWorld(w, be, dropper, visit, d, dice)
	}
	if act.pushed != nil {
		act.pushed.sync(s, w)
		// Container-side changes from the push (a composter's level, a disc's
		// song, a pot's or shelf's items) apply after the dispenser's lock, as a
		// hopper's settle does.
		act.pushed.settle(s, w)
	}
	left := s.commitDispense(w, be, visit, act)
	s.syncContainerViewers(w, d.X, d.Y, d.Z)
	if !left.Empty() {
		s.dispenseItem(w, left, d, dice)
	}
}

// decideDispense picks the slot by dice and removes its one item, under the
// dispenser's lock. The act runs without the lock and a click may move the
// stack meanwhile, so the commit returning only what the act gave back
// leaves nothing to duplicate. TouchEntity confirms the entity is still the
// world's (runDispense looked it up before locking, and the chunk may have
// left) and marks the take for the chunk's next save.
func (s *Server) decideDispense(w *world.World, b *world.BlockEntity, dice *rules.JavaRandom) dispenseVisit {
	if !w.TouchEntity(b) {
		return dispenseVisit{gone: true}
	}
	w.UnpackLoot(b)
	slot := inventory.DispenseSlot(containerSlots(b, nil), dice)
	if slot < 0 {
		return dispenseVisit{empty: true}
	}
	stack := b.Slots[slot]
	rest := stack
	rest.Count--
	b.Put(slot, rest)
	taken := stack
	taken.Count = 1
	return dispenseVisit{slot: slot, taken: taken, behaviour: inventory.DispenseBehaviourOf(stack)}
}

// dispenseWorld acts on the decision without the dispenser's lock: world
// writes, spawns and sounds happen here, returning only what the slot is
// owed.
func (s *Server) dispenseWorld(w *world.World, be *world.BlockEntity, dropper bool, visit dispenseVisit, d world.Dispense, dice *rules.JavaRandom) dispenseAct {
	if dropper {
		return s.dispenseDropper(w, be, visit, d, dice)
	}
	switch visit.behaviour {
	case inventory.DispenseArmorStand:
		s.dispenseArmorStand(w, visit.taken, d)
	case inventory.DispenseChest:
		if !s.dispenseChest(w, d) {
			s.dispenseDefaultDrop(w, visit, d, dice)
		}
	case inventory.DispenseWaterBottle:
		return s.dispenseWaterBottle(w, visit, d, dice)
	case inventory.DispenseSulfurBlock:
		if !s.dispenseSulfurBlock(w, visit.taken, d) {
			s.dispenseDefaultDrop(w, visit, d, dice)
		}
	case inventory.DispenseArrow:
		s.dispenseArrow(w, visit, d)
	case inventory.DispenseThrown:
		s.dispenseThrown(w, visit, d)
	case inventory.DispenseFluidBucket:
		return s.dispenseFluidBucket(w, visit, d, dice)
	case inventory.DispenseEmptyBucket:
		return s.dispenseBucket(w, visit, d, dice)
	case inventory.DispenseTNT:
		return s.dispenseTNT(w, visit, d)
	case inventory.DispenseBoneMeal:
		return s.dispenseBoneMeal(w, visit, d)
	case inventory.DispenseBrush:
		return s.dispenseBrush(w, visit, d)
	case inventory.DispenseShears:
		return s.dispenseShears(w, visit, d)
	case inventory.DispenseFlintAndSteel:
		return s.dispenseFlintAndSteel(w, visit, d)
	case inventory.DispenseGlowstone:
		return s.dispenseGlowstone(w, visit, d, dice)
	case inventory.DispenseGlassBottle:
		return s.dispenseGlassBottle(w, visit, d, dice)
	case inventory.DispenseSpawnEgg:
		return s.dispenseSpawnEgg(w, visit, d, dice)
	case inventory.DispenseMinecart:
		s.dispenseMinecart(w, visit, d, dice)
	case inventory.DispenseFirework:
		s.dispenseFirework(w, d, visit.taken)
	case inventory.DispenseWindCharge:
		s.dispenseWindCharge(w, d, visit.taken, dice)
	case inventory.DispenseBoat:
		s.dispenseBoat(w, visit, d, dice)
	case inventory.DispenseShulkerBox:
		return s.dispenseShulkerBox(w, visit, d)
	case inventory.DispenseFireCharge:
		s.dispenseFireCharge(w, d, visit.taken)
	case inventory.DispenseEquipment, inventory.DispenseOptionalEquipment:
		return s.dispenseEquipment(w, visit, d, dice)
	case inventory.DispenseHoneycomb:
		return s.dispenseHoneycomb(w, visit, d, dice)
	default:
		s.dispenseDefaultDrop(w, visit, d, dice)
	}
	return dispenseAct{}
}

// commitDispense returns what the act gave back, under the dispenser's
// lock: to the slot the item left if it fits, else the other slots; what
// none take is returned for the caller to drop. TouchEntity refuses an
// entity no longer the world's at its position (a break or unload during
// the lockless act would take a stack put into the orphan with it) and
// marks the chunk in the same step, so a save that read the slots before
// the return re-saves rather than dropping the change.
func (s *Server) commitDispense(w *world.World, be *world.BlockEntity, visit dispenseVisit, act dispenseAct) (left protocol.ItemStack) {
	be.Use(func(b *world.BlockEntity) {
		if act.back.Empty() {
			return
		}
		if !w.TouchEntity(b) {
			left = act.back
			return
		}
		left = inventory.DispenseGiveBack(containerSlots(b, nil), visit.slot, act.back)
	})
	return left
}

// dispenseDropper pushes one item into the container the face opens onto,
// or drops it if none; a dropper's push is silent. It returns the target
// container for the caller to sync; be is the push's source only for the
// target hopper's bookkeeping, which reads nothing the dispenser lock
// guards.
func (s *Server) dispenseDropper(w *world.World, be *world.BlockEntity, visit dispenseVisit, d world.Dispense, dice *rules.JavaRandom) dispenseAct {
	tx, ty, tz := faceOffset(d.X, d.Y, d.Z, int32(d.Dir))
	target := s.containerAt(w, tx, ty, tz)
	if target == nil {
		s.dispenseDefaultDrop(w, visit, d, dice)
		return dispenseAct{}
	}
	spent := false
	target.use(w, containerMove{fn: func(dest inventory.Block) {
		if pourInto(be, target.entity(), dest, visit.taken, oppositeFace(int32(d.Dir)), true).Empty() {
			spent = true
		}
	}})
	if spent {
		return dispenseAct{pushed: target}
	}
	return dispenseAct{back: visit.taken, pushed: target}
}

// dispenseDefaultDrop is vanilla's DefaultDispenseItemBehavior: one item
// dropped from the face, with sound and smoke.
func (s *Server) dispenseDefaultDrop(w *world.World, visit dispenseVisit, d world.Dispense, dice *rules.JavaRandom) {
	s.dispenseItem(w, visit.taken, d, dice)
	s.dispenseDefaultSound(w, d)
	s.dispenseAnimation(w, d)
}

// dispenseAim is the direction a launched projectile is aimed along: the
// face's step, or the one a plugin's BlockDispenseEvent set.
func dispenseAim(d world.Dispense) (x, y, z float64) {
	if d.Aim != nil {
		return d.Aim[0], d.Aim[1], d.Aim[2]
	}
	dx, dy, dz := faceStep(int32(d.Dir))
	return float64(dx), float64(dy), float64(dz)
}

// dispenseItem spawns one item of a stack out of the dispenser's face.
func (s *Server) dispenseItem(w *world.World, stack protocol.ItemStack, d world.Dispense, dice *rules.JavaRandom) {
	one := stack
	one.Count = 1
	px, py, pz, vx, vy, vz := inventory.DispenseDropMotion(dice, d.X, d.Y, d.Z, int32(d.Dir))
	// vanilla: DefaultDispenseItemBehavior.spawnItem (no pickup delay)
	s.spawnItem(w, one, px, py, pz, vx, vy, vz, 0)
}

// dispenseArrow shoots an arrow anyone may pick up, the launch sound
// replacing the click. A tipped arrow carries its effects, a spectral one
// its own wire type.
// vanilla: ArrowItem.asProjectile, SpectralArrowItem.asProjectile
func (s *Server) dispenseArrow(w *world.World, visit dispenseVisit, d world.Dispense) {
	dx, dy, dz := dispenseAim(d)
	px, py, pz := inventory.DispenseProjectileStart(d.X, d.Y, d.Z, int32(d.Dir))
	s.shootArrow(w, visit.taken, px, py, pz, dx, dy, dz)
	s.broadcastLevelEvent(w,
		protocol.WorldEventProjectileLaunch, d.X, d.Y, d.Z, 0)

	s.dispenseAnimation(w, d)
}

// shootArrow fires a pickable arrow from the point along the direction at
// the dispensers' power and spread.
func (s *Server) shootArrow(w *world.World, ammo protocol.ItemStack, px, py, pz, dx, dy, dz float64) {
	vx, vy, vz := entity.ShootVelocity(dx, dy, dz,
		inventory.DispenseProjectilePower, inventory.DispenseProjectileInaccuracy, s.levelRng)
	yaw, pitch := entity.ArrowAngles(vx, vy, vz)
	it := newArrowEntity(s, w, px, py, pz, vx, vy, vz, yaw, pitch,
		false, entity.ArrowBaseDamage,
		entity.PickupAllowed,
		0, uuid.UUID{}, protocol.ItemStack{}, 0)
	armArrow(it, ammo)
	s.launchArrow(it)
}

// dispenseFireCharge fires a small fireball a block out of the face along
// its step, with the fire charge dispense event.
// vanilla: FireChargeItem.asProjectile, FireChargeItem.createDispenseConfig
func (s *Server) dispenseFireCharge(w *world.World, d world.Dispense, stack protocol.ItemStack) {
	dx, dy, dz := dispenseAim(d)
	px, py, pz := inventory.DispenseFireChargeStart(d.X, d.Y, d.Z, int32(d.Dir))
	s.launchCharge(entity.ThrownSmallFireball, w, px, py, pz, dx, dy, dz, stack)
	s.broadcastLevelEvent(w, levelEventFireChargeDispense, d.X, d.Y, d.Z, 0)

	s.dispenseAnimation(w, d)
}

// dispenseFirework launches an ownerless rocket with its shot velocity kept.
// vanilla: FireworkRocketItem.asProjectile, createDispenseConfig
func (s *Server) dispenseFirework(w *world.World, d world.Dispense, stack protocol.ItemStack) {
	dx, dy, dz := dispenseAim(d)
	px, py, pz := inventory.DispenseFireworkStart(d.X, d.Y, d.Z, int32(d.Dir))
	it := s.newFirework(w, stack, px, py, pz, 0, uuid.Nil, 0)
	vx, vy, vz := entity.ShootVelocity(dx, dy, dz, 0.5, 1, s.levelRng)
	it.rocket.ShotAtAngle = true
	it.rocket.VX, it.rocket.VY, it.rocket.VZ = vx, vy, vz
	it.rocket.Yaw, it.rocket.Pitch = entity.ArrowAngles(vx, vy, vz)
	s.registerFirework(it)
	s.broadcastLevelEvent(w, 1004, d.X, d.Y, d.Z, 0) // vanilla: FireworkRocketItem.createDispenseConfig
	s.dispenseAnimation(w, d)
}

// dispenseWindCharge keeps WindChargeItem.asProjectile's unnormalised motion;
// its shoot override deliberately does nothing.
func (s *Server) dispenseWindCharge(w *world.World, d world.Dispense, stack protocol.ItemStack, dice *rules.JavaRandom) {
	dx, dy, dz := dispenseAim(d)
	px, py, pz := inventory.DispenseFireChargeStart(d.X, d.Y, d.Z, int32(d.Dir))
	const spread = 0.11485000000000001 // vanilla: WindChargeItem.asProjectile
	vx, vy, vz := dice.Triangle(float64(dx), spread), dice.Triangle(float64(dy), spread), dice.Triangle(float64(dz), spread)
	yaw, pitch := entity.ArrowAngles(vx, vy, vz)
	seed := s.levelRng.Int63()
	s.standThrown(entity.ThrownWindCharge, w, px, py, pz, vx, vy, vz, yaw, pitch, 0, uuid.Nil, seed, entity.NewRolls(seed), stack)
	s.broadcastLevelEvent(w, levelEventWindChargeDispense, d.X, d.Y, d.Z, 0)
	s.dispenseAnimation(w, d)
}

// dispenseThrown fires an egg, snowball, bottle o' enchanting or potion
// with vanilla's ProjectileDispenseBehavior configs (default, plus the
// potion's overrides), the launch sound replacing the click, and no owner.
// The taken stack becomes the body's, so a potion keeps its contents.
func (s *Server) dispenseThrown(w *world.World, visit dispenseVisit, d world.Dispense) {
	dx, dy, dz := dispenseAim(d)
	px, py, pz := inventory.DispenseProjectileStart(d.X, d.Y, d.Z, int32(d.Dir))
	s.shootThrown(w, visit.taken, px, py, pz, dx, dy, dz)
	s.broadcastLevelEvent(w,
		protocol.WorldEventProjectileLaunch, d.X, d.Y, d.Z, 0)

	s.dispenseAnimation(w, d)
}

// shootThrown throws the stack's egg, snowball, bottle o' enchanting or
// potion from the point along the direction at the item's power and spread.
func (s *Server) shootThrown(w *world.World, stack protocol.ItemStack, px, py, pz, dx, dy, dz float64) {
	seed := s.levelRng.Int63()
	rolls := entity.NewRolls(seed)
	kind := entity.ThrownEgg
	power, inaccuracy := inventory.DispenseProjectilePower, inventory.DispenseProjectileInaccuracy
	switch registry.Item(stack.ItemID) {
	case registry.ItemSnowball:
		kind = entity.ThrownSnowball
	case registry.ItemSplashPotion:
		kind = entity.ThrownSplashPotion
		power, inaccuracy = inventory.DispensePotionPower, inventory.DispensePotionInaccuracy
	case registry.ItemLingeringPotion:
		kind = entity.ThrownLingeringPotion
		power, inaccuracy = inventory.DispensePotionPower, inventory.DispensePotionInaccuracy
	case registry.ItemExperienceBottle:
		kind = entity.ThrownExperienceBottle
		power, inaccuracy = inventory.DispensePotionPower, inventory.DispensePotionInaccuracy
	}
	vx, vy, vz := entity.ShootVelocity(dx, dy, dz, power, inaccuracy, rolls.Rand)
	thrown := entity.NewThrown(s.nextEntity(), kind, px, py, pz, vx, vy, vz, 0, seed)
	thrown.Rand, thrown.RandSrc = rolls.Rand, rolls.Src
	if it := thrownShell(thrown, w, uuid.Nil, stack); it != nil {
		s.launchThrown(it)
	}
}

// dispenseFluidBucket pours where the face opens onto something
// replaceable, leaving the empty bucket; a refused pour drops the bucket. A
// fish bucket pours water and releases its fish there.
func (s *Server) dispenseFluidBucket(w *world.World, visit dispenseVisit, d world.Dispense, dice *rules.JavaRandom) dispenseAct {
	tx, ty, tz := faceOffset(d.X, d.Y, d.Z, int32(d.Dir))
	var pour fluidKind
	item := registry.Item(visit.taken.ItemID)
	if sound, solid := rules.SolidBucketPlaceSound(item); solid {
		if s.dispenseSolidBucket(w, tx, ty, tz, item, sound) {
			return s.dispenseSwap(w, d, registry.ItemBucket)
		}
		s.dispenseDefaultDrop(w, visit, d, dice)
		return dispenseAct{}
	}
	for _, fluid := range fluids {
		if fluid.item == item {
			pour = fluid
			break
		}
	}
	if registry.Item(visit.taken.ItemID) == registry.ItemSulfurCubeBucket {
		s.broadcastSoundID(w, registry.SoundItemBucketEmptySulfurCube, protocol.SoundCategoryNeutral, float64(tx)+0.5, float64(ty)+0.5, float64(tz)+0.5, 1, 1)
		s.releaseBucketMob(s.dimensionOfWorld(w), visit.taken, float64(tx)+0.5, float64(ty), float64(tz)+0.5)
		return s.dispenseSwap(w, d, registry.ItemBucket)
	}
	_, mobBucket := entity.BucketMobKind(registry.Item(visit.taken.ItemID))
	if mobBucket {
		pour = mobBucketPour(item)
	}
	if pour.source != 0 && s.pourFluidAt(w, tx, ty, tz, pour) {
		if mobBucket {
			// vanilla: DispenseItemBehavior.filledBucketBehavior, MobBucketItem.checkExtraContent
			s.releaseBucketMob(s.dimensionOfWorld(w), visit.taken, float64(tx)+0.5, float64(ty), float64(tz)+0.5)
		}
		return s.dispenseSwap(w, d, registry.ItemBucket)
	}
	s.dispenseDefaultDrop(w, visit, d, dice)
	return dispenseAct{}
}

// dispenseSolidBucket sets the bucket's block into an air cell, with the
// bucket's sound. vanilla: SolidBucketItem.emptyContents
func (s *Server) dispenseSolidBucket(w *world.World, x, y, z int32, item registry.Item, sound registry.Sound) bool {
	b, ok := item.Block()
	if !ok || w.BlockAt(x, y, z) != airBlockState || !w.SetBlock(x, y, z, b.DefaultState()) {
		return false
	}
	s.markChanged(w, x, y, z)
	s.relightAsync(w, x, z)
	s.broadcastSoundID(w, sound, protocol.SoundCategoryBlock,
		float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, 1.0, 1.0)
	s.blockChanged(w, x, y, z, airBlockState)
	return true
}

// dispenseBucket picks up what the face opens onto, leaving the filled
// bucket; with nothing to take, it drops the bucket. Only the dispenser's
// click plays, no fill sound. vanilla: DispenseItemBehavior (Items.BUCKET)
func (s *Server) dispenseBucket(w *world.World, visit dispenseVisit, d world.Dispense, dice *rules.JavaRandom) dispenseAct {
	tx, ty, tz := faceOffset(d.X, d.Y, d.Z, int32(d.Dir))
	if after, fluid, ok := rules.BucketScoopTarget(w.BlockAt(tx, ty, tz)); ok {
		pickup := pickupFor(fluid)
		if s.pickUpBlock(w, tx, ty, tz, after, pickup) {
			return s.dispenseSwap(w, d, pickup.item)
		}
	}
	s.dispenseDefaultDrop(w, visit, d, dice)
	return dispenseAct{}
}

// dispenseSwap is a bucket behaviour's success: click and smoke, the new
// bucket left for the commit.
func (s *Server) dispenseSwap(w *world.World, d world.Dispense, became registry.Item) dispenseAct {
	s.dispenseDefaultSound(w, d)
	s.dispenseAnimation(w, d)
	return dispenseAct{back: swapInto(became)}
}

// swapInto builds the one-item stack a bucket swap leaves in the slot.
func swapInto(item registry.Item) protocol.ItemStack {
	return protocol.ItemStack{ItemID: int32(item), Count: 1}
}

// dispenseTNT primes TNT half a block into the target with the primed
// sound; with tnt_explodes off, the fail sound and the item is kept.
// vanilla: DispenseItemBehavior (TNT)
func (s *Server) dispenseTNT(w *world.World, visit dispenseVisit, d world.Dispense) dispenseAct {
	if !w.TNTExplodes() {
		s.broadcastLevelEvent(w,
			protocol.WorldEventDispenseFail, d.X, d.Y, d.Z, 0)
		s.dispenseAnimation(w, d)
		return dispenseAct{back: visit.taken}
	}
	if s.dispenseSulfurBlock(w, visit.taken, d) {
		return dispenseAct{}
	}
	tx, ty, tz := faceOffset(d.X, d.Y, d.Z, int32(d.Dir))
	body := s.spawnTNT(w, float64(tx)+0.5, float64(ty), float64(tz)+0.5,
		inventory.DispenseTNTFuse, 0, "", uuid.UUID{}, true)
	s.broadcastSoundID(w,
		registry.SoundEntityTntPrimed, protocol.SoundCategoryBlock,
		body.tnt.X, body.tnt.Y, body.tnt.Z, 1.0, 1.0)

	s.dispenseDefaultSound(w, d)
	s.dispenseAnimation(w, d)
	return dispenseAct{}
}

// dispenseBoneMeal grows what the face opens onto; nothing to grow is the
// fail sound with the item kept.
func (s *Server) dispenseBoneMeal(w *world.World, visit dispenseVisit, d world.Dispense) dispenseAct {
	tx, ty, tz := faceOffset(d.X, d.Y, d.Z, int32(d.Dir))
	changes, eventPos, drops, used := s.physicsFor(w).DispenseBoneMeal(tx, ty, tz)
	if !used {
		s.broadcastLevelEvent(w,
			protocol.WorldEventDispenseFail, d.X, d.Y, d.Z, 0)

		s.dispenseAnimation(w, d)
		return dispenseAct{back: visit.taken}
	}
	s.boneMealResults(w, eventPos, drops, changes)
	s.broadcastLevelEvent(w,
		protocol.WorldEventDispense, d.X, d.Y, d.Z, 0)

	s.dispenseAnimation(w, d)
	return dispenseAct{}
}

// dispenseShears is vanilla's ShearsDispenseItemBehavior: a full hive in the
// target cell first, then a mob crossing it, then a leashed boat, knot or player; a shear wears the shears, and
// nothing to shear keeps them with the fail click.
func (s *Server) dispenseShears(w *world.World, visit dispenseVisit, d world.Dispense) dispenseAct {
	tx, ty, tz := faceOffset(d.X, d.Y, d.Z, int32(d.Dir))
	if !s.shearBeehive(w, visit.taken, tx, ty, tz) &&
		!s.shearMobInCell(w, visit.taken, tx, ty, tz) &&
		!s.cutLeashesInCell(w, tx, ty, tz) {
		s.broadcastLevelEvent(w,
			protocol.WorldEventDispenseFail, d.X, d.Y, d.Z, 0)

		s.dispenseAnimation(w, d)
		return dispenseAct{back: visit.taken}
	}
	return s.dispenseWorn(w, visit, d)
}

// shearMobInCell cuts the leads of, else shears, the first mob crossing the
// cell that has either, dropping its take in the blocks category as
// vanilla's dispenser does; a mooshroom becomes its cow.
// vanilla: ShearsDispenseItemBehavior.tryShearEntity
func (s *Server) shearMobInCell(w *world.World, tool protocol.ItemStack, x, y, z int32) bool {
	for _, ent := range s.mobsIn(w, float64(x), float64(y), float64(z),
		float64(x)+1, float64(y)+1, float64(z)+1) {
		// vanilla: Entity.shearOffAllLeashConnections
		if drops, lx, ly, lz := s.cutLeashesOfMob(ent); s.finishLeashShear(w, drops, lx, ly, lz, rules.GameEventContext{}, protocol.SoundCategoryNeutral) {
			return true
		}
		take, done := s.shearOffAsking(ent, tool, s.pluginDispenserShear(ent))
		if !done {
			continue
		}
		s.tellShear(w, take, protocol.SoundCategoryBlock)
		if ent.mob.Type == entity.TypeMooshroom {
			s.convertShearedMooshroom(ent)
		}
		return true
	}
	return false
}

// cutLeashesInCell cuts the leads of the first boat or knot crossing the cell
// that has any: a boat's own and those it holds, a knot's held ones.
// vanilla: ShearsDispenseItemBehavior.tryShearEntity
func (s *Server) cutLeashesInCell(w *world.World, x, y, z int32) bool {
	minX, minY, minZ := float64(x), float64(y), float64(z)
	maxX, maxY, maxZ := minX+1, minY+1, minZ+1
	for _, it := range s.boatsIn(w, minX, minY, minZ, maxX, maxY, maxZ) {
		if drops, bx, by, bz := s.cutLeashesOfBoat(it); s.finishLeashShear(w, drops, bx, by, bz, rules.GameEventContext{}, protocol.SoundCategoryNeutral) {
			return true
		}
	}
	for _, t := range s.knotsIn(w, minX, minY, minZ, maxX, maxY, maxZ) {
		if s.finishLeashShear(w, s.cutKnotLeashes(t), t.x, t.y, t.z, rules.GameEventContext{}, protocol.SoundCategoryNeutral) {
			return true
		}
	}
	return s.cutLeashesOfPlayersInCell(w, minX, minY, minZ, maxX, maxY, maxZ)
}

// cutLeashesOfPlayersInCell cuts the leads held by the first non-spectator
// player whose box crosses the cell and who holds any; a player is not
// leashable, but cuts what they hold.
// vanilla: ShearsDispenseItemBehavior.tryShearEntity, Entity.shearOffAllLeashConnections
func (s *Server) cutLeashesOfPlayersInCell(w *world.World, minX, minY, minZ, maxX, maxY, maxZ float64) bool {
	var holders []entity.LeashHolder
	s.mu.RLock()
	for _, c := range s.clients {
		if c.player == nil || c.theWorld() != w {
			continue
		}
		h := playerLeashHolder(c)
		if !h.Present || h.X+0.5*h.Width <= minX || h.X-0.5*h.Width >= maxX ||
			h.Y+h.Height <= minY || h.Y >= maxY ||
			h.Z+0.5*h.Width <= minZ || h.Z-0.5*h.Width >= maxZ {
			continue
		}
		holders = append(holders, h)
	}
	s.mu.RUnlock()
	for _, h := range holders {
		s.mobsMu.Lock()
		drops := s.cutHeldLeashesLocked(w, h.EntityID, h.X, h.Y+0.5*h.Height, h.Z, nil)
		s.mobsMu.Unlock()
		if s.finishLeashShear(w, drops, h.X, h.Y, h.Z, rules.GameEventContext{}, protocol.SoundCategoryNeutral) {
			return true
		}
	}
	return false
}

// shearBeehive shears a full hive or nest in the cell: the shear sound, one
// stack of three honeycomb (vanilla's harvest/beehive) at the block, and
// honey back to 0 and the bees that can leave freed; false if not a full hive. vanilla:
// ShearsDispenseItemBehavior.tryShearBeehive, BeehiveBlock.dropHoneycomb.
func (s *Server) shearBeehive(w *world.World, tool protocol.ItemStack, x, y, z int32) bool {
	state, ok := rules.FullHiveAt(w, x, y, z)
	if !ok {
		return false
	}
	dropX, dropY, dropZ := float64(x)+0.5, float64(y)+0.5, float64(z)+0.5
	s.broadcastSoundID(w,
		registry.SoundBlockBeehiveShear, protocol.SoundCategoryBlock,
		dropX, dropY, dropZ, 1.0, 1.0)

	s.dropHiveHarvest(w, x, y, z, state, tool, rules.LootEntity{})
	s.releaseHiveAndResetHoney(w, x, y, z, state)
	return true
}

// emptyHive resets a hive's honey to 0.
func (s *Server) emptyHive(w *world.World, x, y, z int32, state uint16) {
	s.setBlockAndTell(w, x, y, z, rules.EmptiedHive(state))
}

// dispenseGlassBottle fills the bottle from a full hive, else from water in
// the target cell, leaving the filled bottle for the commit; otherwise it
// throws the bottle out with the fail click.
// vanilla: DispenseItemBehavior, Items.GLASS_BOTTLE
func (s *Server) dispenseGlassBottle(w *world.World, visit dispenseVisit, d world.Dispense, dice *rules.JavaRandom) dispenseAct {
	tx, ty, tz := faceOffset(d.X, d.Y, d.Z, int32(d.Dir))
	var filled protocol.ItemStack
	if state, full := rules.FullHiveAt(w, tx, ty, tz); full {
		filled = swapInto(registry.ItemHoneyBottle)
		s.releaseHiveAndResetHoney(w, tx, ty, tz, state)
	} else if world.HoldsWater(w.BlockAt(tx, ty, tz)) {
		filled, _ = waterBottle()
	}
	if filled.Empty() {
		s.dispenseItem(w, visit.taken, d, dice)
		s.broadcastLevelEvent(w,
			protocol.WorldEventDispenseFail, d.X, d.Y, d.Z, 0)

		s.dispenseAnimation(w, d)
		return dispenseAct{}
	}
	s.dispenseDefaultSound(w, d)
	s.dispenseAnimation(w, d)
	return dispenseAct{back: filled}
}

// dispenseFlintAndSteel is vanilla's FlintAndSteelDispenseItemBehavior: fire
// into air in the target cell (lighting a complete portal frame instead),
// lighting an unlit campfire, candle or candle cake, or priming TNT and
// leaving air. It succeeds and wears the flint and steel whatever it hit,
// except TNT kept from priming by tnt_explodes, which fails.
// vanilla: FlintAndSteelDispenseItemBehavior.execute
func (s *Server) dispenseFlintAndSteel(w *world.World, visit dispenseVisit, d world.Dispense) dispenseAct {
	tx, ty, tz := faceOffset(d.X, d.Y, d.Z, int32(d.Dir))
	if s.igniteSulfurCubeAt(w, tx, ty, tz) {
		return s.dispenseWorn(w, visit, d)
	}
	state := w.BlockAt(tx, ty, tz)
	if world.IsKnownAirState(state) {
		// vanilla: BaseFireBlock.canBePlacedAt: fire only into air, where it
		// survives or where a complete portal frame stands (which the fire's
		// onPlace lights instead).
		if s.ignitePortalAt(w, tx, ty, tz) {
			return s.dispenseWorn(w, visit, d)
		}
		if rules.FireSurvivesAt(w, tx, ty, tz) {
			s.setBlockAndTell(w, tx, ty, tz, rules.FireStateForPlacement(w, tx, ty, tz))
			return s.dispenseWorn(w, visit, d)
		}
	}
	if lit, ok := rules.FlintLight(state); ok {
		s.setBlockAndTell(w, tx, ty, tz, lit)
		return s.dispenseWorn(w, visit, d)
	}
	if registry.BlockOf(state) == registry.BlockTnt {
		if !w.TNTExplodes() {
			s.broadcastLevelEvent(w,
				protocol.WorldEventDispenseFail, d.X, d.Y, d.Z, 0)
			s.dispenseAnimation(w, d)
			return dispenseAct{back: visit.taken}
		}
		s.primeTNT(w, world.TNTPrime{X: tx, Y: ty, Z: tz})
		s.setBlockAndTell(w, tx, ty, tz, airBlockState)
	}
	return s.dispenseWorn(w, visit, d)
}

// igniteSulfurCubeAt primes the first explosive cube in the cell and reports
// whether there was one, even when tnt_explodes kept it from priming.
// vanilla: FlintAndSteelDispenseItemBehavior.tryIgniteExplosiveEntities
func (s *Server) igniteSulfurCubeAt(w *world.World, x, y, z int32) bool {
	mobs := s.mobsIn(w, float64(x), float64(y), float64(z), float64(x+1), float64(y+1), float64(z+1))
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for _, ent := range mobs {
		if ent.mob.Type == entity.TypeSulfurCube && ent.mob.SulfurCanExplode() {
			ent.mob.PrimeSulfurWithRule(false, w.TNTExplodes())
			return true
		}
	}
	return false
}

// dispenseGlowstone charges a respawn anchor the face opens onto, spending
// the glowstone; a full anchor fails and keeps it, other blocks get the
// plain drop.
// vanilla: DispenseItemBehavior (glowstone), RespawnAnchorBlock.charge
func (s *Server) dispenseGlowstone(w *world.World, visit dispenseVisit, d world.Dispense, dice *rules.JavaRandom) dispenseAct {
	tx, ty, tz := faceOffset(d.X, d.Y, d.Z, int32(d.Dir))
	state, charges, ok := anchorState(w, tx, ty, tz)
	if !ok {
		s.dispenseDefaultDrop(w, visit, d, dice)
		return dispenseAct{}
	}
	if charges >= anchorMaxCharges {
		s.broadcastLevelEvent(w,
			protocol.WorldEventDispenseFail, d.X, d.Y, d.Z, 0)

		s.dispenseAnimation(w, d)
		return dispenseAct{back: visit.taken}
	}
	s.chargeAnchor(w, tx, ty, tz, state, charges, rules.GameEventContext{})
	s.dispenseDefaultSound(w, d)
	s.dispenseAnimation(w, d)
	return dispenseAct{}
}

// dispenseSpawnEgg spawns the egg's mob in the face's cell, spending the
// egg. An unsimulated kind or one canSpawn refuses (a monster in peaceful)
// spawns nothing and keeps the egg; click and smoke still play.
// vanilla: SpawnEggItemBehavior.execute, EntityType.create
func (s *Server) dispenseSpawnEgg(w *world.World, visit dispenseVisit, d world.Dispense, _ *rules.JavaRandom) dispenseAct {
	kind, simulated, _ := heldSpawnEgg(visit.taken)
	s.dispenseDefaultSound(w, d)
	s.dispenseAnimation(w, d)
	if !simulated || !entity.AllowedAt(kind, w.Difficulty()) {
		return dispenseAct{back: visit.taken}
	}
	width, _ := entity.KindDims(kind)
	px, py, pz := rules.DispenserEggPosition(w, d.X, d.Y, d.Z, int32(d.Dir), width/2)
	s.standEggMob(s.dimensionOfWorld(w), visit.taken, kind, px, py, pz)
	return dispenseAct{}
}

// dispenseMinecart places a cart on the rail in front or below an air cell.
// vanilla: MinecartDispenseItemBehavior.execute
func (s *Server) dispenseMinecart(w *world.World, visit dispenseVisit, d world.Dispense, dice *rules.JavaRandom) {
	tx, ty, tz := faceOffset(d.X, d.Y, d.Z, int32(d.Dir))
	front := w.BlockAt(tx, ty, tz)
	px, py, pz, ok := inventory.DispenseMinecartSpot(d.X, d.Y, d.Z, int32(d.Dir), front, w.BlockAt(tx, ty-1, tz), world.IsKnownAirState(front))
	if !ok {
		s.dispenseDefaultDrop(w, visit, d, dice)
		return
	}
	kind, _ := entity.MinecartOfItem(registry.Item(visit.taken.ItemID))
	stack := visit.taken
	// Dispensers have no operator to authorize restricted entity data.
	// vanilla: EntityType.updateCustomEntityTag
	if kind == entity.MinecartCommand || kind == entity.MinecartSpawner {
		stack.Raw = nil
	}
	s.spawnMinecartFromItem(w, kind, px, py, pz, stack)
	s.dispenseDefaultSound(w, d)
	s.dispenseAnimation(w, d)
}

// dispenseBoat floats the boat on water the face opens onto, or on air
// above water; any other target drops it as an item.
// vanilla: BoatDispenseItemBehavior.execute
func (s *Server) dispenseBoat(w *world.World, visit dispenseVisit, d world.Dispense, dice *rules.JavaRandom) {
	tx, ty, tz := faceOffset(d.X, d.Y, d.Z, int32(d.Dir))
	inWater := world.HoldsWater(w.BlockAt(tx, ty, tz))
	if !inWater && (!world.IsKnownAirState(w.BlockAt(tx, ty, tz)) || !world.HoldsWater(w.BlockAt(tx, ty-1, tz))) {
		s.dispenseDefaultDrop(w, visit, d, dice)
		return
	}
	name, _ := entity.HeldBoat(visit.taken)
	px, py, pz, yaw := inventory.DispenseBoatSpot(d.X, d.Y, d.Z, int32(d.Dir), 2*entity.BoatHalfWidth, inWater)
	dispensed := s.newBoatFromItem(w, name, px, py, pz, yaw, visit.taken)
	s.registerBoat(dispensed)
	s.bodySpawned(dispensed.uuid, plugin.SpawnDispenser)
	s.dispenseDefaultSound(w, d)
	s.dispenseAnimation(w, d)
}

// dispenseShulkerBox places the box in the face's cell with the stack's
// contents and name; a cell it cannot take keeps it with the fail click.
// vanilla: ShulkerBoxDispenseBehavior.execute, BlockItem.place
func (s *Server) dispenseShulkerBox(w *world.World, visit dispenseVisit, d world.Dispense) dispenseAct {
	tx, ty, tz := faceOffset(d.X, d.Y, d.Z, int32(d.Dir))
	box, _ := registry.Item(visit.taken.ItemID).Block()
	face, facing := inventory.DispenseShulkerFacing(int32(d.Dir), world.IsKnownAirState(w.BlockAt(tx, ty-1, tz)))
	state, ok := registry.WithProp(box.DefaultState(), registry.PropFacing, facing)
	// DirectionalPlaceContext.canPlace asks canBeReplaced for the target cell.
	if !ok || !rules.CanBeReplaced(w.BlockAt(tx, ty, tz), registry.BlockOf(state), rules.PlacementContext{Face: face, ReplacingClickedOn: true}) ||
		s.obstructed(w, tx, ty, tz, state) {
		s.broadcastLevelEvent(w,
			protocol.WorldEventDispenseFail, d.X, d.Y, d.Z, 0)

		s.dispenseAnimation(w, d)
		return dispenseAct{back: visit.taken}
	}
	old := w.BlockAt(tx, ty, tz)
	if s.writeBlock(w, tx, ty, tz, state, nil, writeLight|writeLightAfter) {
		if be := w.EnsureBlockEntity(tx, ty, tz, world.KindShulkerBox); be != nil {
			be.Use(func(b *world.BlockEntity) {
				if typeID, tag, ok := visit.taken.BlockEntityDataOf(); ok {
					b.LoadBlockEntityData(typeID, tag)
				}
				b.ApplyItemStack(visit.taken)
			})
		}
		s.broadcastSoundID(w,
			rules.BlockPlaceSound(registry.BlockOf(state)), protocol.SoundCategoryBlock,
			float64(tx)+0.5, float64(ty)+0.5, float64(tz)+0.5, 1.0, 0.8)

		s.updateNeighborShapes(w, tx, ty, tz)
		s.blockChanged(w, tx, ty, tz, old)
	}
	s.dispenseDefaultSound(w, d)
	s.dispenseAnimation(w, d)
	return dispenseAct{}
}

// dispenseWorn is a tool-wearing behaviour's outcome: click and smoke, and
// the worn tool owed to the slot, or nothing if it broke.
func (s *Server) dispenseWorn(w *world.World, visit dispenseVisit, d world.Dispense) dispenseAct {
	s.dispenseDefaultSound(w, d)
	s.dispenseAnimation(w, d)
	worn, _ := rules.Wear(visit.taken, rules.ProcessDurabilityChange(visit.taken, 1, s.levelRng))
	return dispenseAct{back: worn}
}

// dispenseDefaultSound is the dispense click; dispenseAnimation is the
// smoke at the face.
func (s *Server) dispenseDefaultSound(w *world.World, d world.Dispense) {
	s.broadcastLevelEvent(w,
		protocol.WorldEventDispense, d.X, d.Y, d.Z, 0)

}

func (s *Server) dispenseAnimation(w *world.World, d world.Dispense) {
	s.broadcastLevelEvent(w,
		protocol.WorldEventDispenseAnim, d.X, d.Y, d.Z, int32(d.Dir))

}
