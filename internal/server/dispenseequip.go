package server

import (
	"cmp"
	"github.com/google/uuid"
	"math"
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/inventory"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// dispenseEquipSlots are the player slots an equippable component can name,
// the off hand included.
var dispenseEquipSlots = map[string]command.ItemSlot{
	"head":    command.ItemSlotHead,
	"chest":   command.ItemSlotChest,
	"legs":    command.ItemSlotLegs,
	"feet":    command.ItemSlotFeet,
	"offhand": command.ItemSlotOffHand,
	"body":    command.ItemSlotBody,
	"saddle":  command.ItemSlotSaddle,
}

// equipReach bounds how far a mob's centre can lie from the cell and still
// have a box reaching into it: half the ender dragon's 16-block width, the
// widest body, plus the cell's own half.
const equipReach = 9.0

// equipCell is the cell a dispenser fires into and the piece it fires.
type equipCell struct {
	w     *world.World
	box   entity.AABB
	x, z  float64
	stack protocol.ItemStack
	item  registry.EquippableItem
}

// equipTarget is a body a dispenser may dress; id orders the candidates, and
// try dresses it, reporting false when the piece does not fit.
type equipTarget struct {
	id   int32
	uuid uuid.UUID
	fits func() bool // whether try would succeed, changing nothing
	try  func() (equipFx, bool)
}

// equipFx is what a dressed body tells the world: where, in which sound
// category, and the context of the equip game event.
type equipFx struct {
	x, y, z   float64
	category  int32
	silent    bool
	skipSound bool           // sound was emitted already or the component names another slot
	sound     registry.Sound // replaces the item's equip sound when set
	event     bool
	ctx       rules.GameEventContext
}

// dispenseEquipment puts the item on the first body in the face's cell that
// takes it, players, mobs and armor stands alike in entity ID order, which is
// vanilla's order only for bodies that never changed chunk section (it walks
// sections by key, then insertion order). With nobody to take it, a carved
// pumpkin or wither skull stays in the dispenser with the fail sound and any
// other piece is dropped.
// vanilla: EquipmentDispenseItemBehavior.execute, DispenseItemBehavior (carved pumpkin)
func (s *Server) dispenseEquipment(w *world.World, visit dispenseVisit, d world.Dispense, dice *rules.JavaRandom) dispenseAct {
	if registry.Item(visit.taken.ItemID) == registry.ItemWitherSkeletonSkull && s.dispenseWitherSkull(w, d) {
		s.dispenseDefaultSound(w, d)
		s.dispenseAnimation(w, d)
		return dispenseAct{}
	}
	if registry.Item(visit.taken.ItemID) == registry.ItemCarvedPumpkin && s.dispenseCarvedPumpkin(w, d) {
		s.dispenseDefaultSound(w, d)
		s.dispenseAnimation(w, d)
		return dispenseAct{}
	}
	item, _ := registry.StackEquippable(visit.taken)
	targets := s.equipTargets(w, visit.taken, d)
	for _, target := range targets {
		if fx, ok := target.try(); ok {
			s.equipFeedback(w, item, fx)
			s.dispenseDefaultSound(w, d)
			s.dispenseAnimation(w, d)
			return dispenseAct{}
		}
	}
	if visit.behaviour == inventory.DispenseOptionalEquipment {
		s.broadcastLevelEvent(w, protocol.WorldEventDispenseFail, d.X, d.Y, d.Z, 0)
		s.dispenseAnimation(w, d)
		return dispenseAct{back: visit.taken}
	}
	s.dispenseItem(w, visit.taken, d, dice)
	s.dispenseDefaultSound(w, d)
	s.dispenseAnimation(w, d)
	return dispenseAct{}
}

// equipTargets lists the bodies in the face's cell that a dispenser may dress
// with the piece, in entity ID order.
func (s *Server) equipTargets(w *world.World, taken protocol.ItemStack, d world.Dispense) []equipTarget {
	item, _ := registry.StackEquippable(taken)
	tx, ty, tz := faceOffset(d.X, d.Y, d.Z, int32(d.Dir))
	cell := equipCell{
		w: w, box: entity.NewAABB(float64(tx), float64(ty), float64(tz), float64(tx+1), float64(ty+1), float64(tz+1)),
		x: float64(tx) + 0.5, z: float64(tz) + 0.5, stack: taken, item: item,
	}
	var targets []equipTarget
	// vanilla: LivingEntity.canEquipWithDispenser (dispensable)
	if item.Dispensable {
		targets = s.equipPlayersIn(cell)
		targets = s.equipMobsIn(cell, targets)
		targets = s.equipStandsIn(cell, targets)
	}
	slices.SortStableFunc(targets, func(a, b equipTarget) int { return cmp.Compare(a.id, b.id) })
	return targets
}

// equipFeedback plays the item's equip sound and posts the equip event.
// vanilla: LivingEntity.onEquipItem
func (s *Server) equipFeedback(w *world.World, item registry.EquippableItem, fx equipFx) {
	if !fx.silent && !fx.skipSound {
		sound := item.EquipSound
		if fx.sound != registry.SoundNone {
			sound = fx.sound.Holder()
		}
		s.broadcastSoundHolder(w, sound, fx.category, fx.x, fx.y, fx.z, 1, 1)
	}
	if fx.event {
		s.gameEvent(w, rules.GameEventEquip, fx.x, fx.y, fx.z, fx.ctx)
	}
}

// slotEquipFeedback is equipFeedback for a written equipment slot (Set
// Equipment index wire: 0 main hand, 1 off hand, 2-5 feet to head) going from
// old to now: the new piece plays its sound only in the slot it names, and a
// stack with no equippable posts the unequip event.
// vanilla: LivingEntity.onEquipItem
func (s *Server) slotEquipFeedback(w *world.World, wire int, old, now protocol.ItemStack, fx equipFx) {
	if old.Same(now) {
		return
	}
	eq, ok := registry.StackEquippable(now)
	if !ok || now.Empty() {
		s.gameEvent(w, rules.GameEventUnequip, fx.x, fx.y, fx.z, fx.ctx)
		return
	}
	fx.skipSound = eq.Slot != [...]string{"mainhand", "offhand", "feet", "legs", "chest", "head"}[wire]
	fx.event = true
	s.equipFeedback(w, eq, fx)
}

// armorChanged tells the world about a player's armour slot (window slots
// 5-8) going from old to now: the new piece's equip sound, and the equip or
// unequip event. The wearer's own client plays nothing, so the sound goes to
// everyone in range.
// vanilla: LivingEntity.onEquipItem, Player.doesEmitEquipEvent
func (c *client) armorChanged(slot int, old, now protocol.ItemStack) {
	if c.player.GameMode() == protocol.GameModeSpectator || (old.Empty() && now.Empty()) || old.Same(now) {
		return
	}
	at := c.player.Position()
	w := c.theWorld()
	if eq, ok := registry.StackEquippable(now); ok && !now.Empty() {
		if worn, ok := player.EquipmentSlotOf(eq.Slot); ok && worn == slot {
			c.srv.broadcastSoundHolder(w, eq.EquipSound, protocol.SoundCategoryPlayer, at.X, at.Y, at.Z, 1, 1)
		}
		c.srv.gameEvent(w, rules.GameEventEquip, at.X, at.Y, at.Z, c.gameEventContext(0))
		return
	}
	c.srv.gameEvent(w, rules.GameEventUnequip, at.X, at.Y, at.Z, c.gameEventContext(0))
}

// equipPlayersIn lists the living, non-spectator players whose box crosses
// the cell; a creative player counts.
// vanilla: LivingEntity.canEquipWithDispenser
func (s *Server) equipPlayersIn(cell equipCell) []equipTarget {
	slot, ok := dispenseEquipSlots[cell.item.Slot]
	if !ok || !cell.item.AllowsEntity("minecraft:player") {
		return nil
	}
	one := cell.stack
	one.Count = 1
	var targets []equipTarget
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, c := range s.clients {
		if c.theWorld() != cell.w || c.player.GameMode() == protocol.GameModeSpectator || c.player.Vitals().Dead {
			continue
		}
		pos := c.player.Position()
		d := c.dims(c.player.Appearance().Pose)
		if !cell.box.Crosses(entity.NewAABB(pos.X-d.Width/2, pos.Y, pos.Z-d.Width/2,
			pos.X+d.Width/2, pos.Y+d.Height, pos.Z+d.Width/2)) {
			continue
		}
		put := func(commit bool) bool {
			equipped := false
			c.UpdateItemIn(slot, func(worn protocol.ItemStack) (protocol.ItemStack, bool) {
				equipped = worn.Empty()
				return one, equipped && commit
			})
			return equipped
		}
		targets = append(targets, equipTarget{id: c.player.EntityID, uuid: c.UUID(), fits: func() bool { return put(false) }, try: func() (equipFx, bool) {
			equipped := put(true)
			at := c.player.Position()
			// UpdateItemIn emits armor feedback; other slots still need it here.
			armor := slot >= command.ItemSlotFeet && slot < command.ItemSlotFeet+player.ArmorSlots
			return equipFx{x: at.X, y: at.Y, z: at.Z, category: protocol.SoundCategoryPlayer,
				skipSound: armor, event: !armor && slot != command.ItemSlotOffHand, ctx: c.gameEventContext(0)}, equipped
		}})
	}
	return targets
}

// equipMobsIn appends the mobs whose box crosses the cell and may take the
// piece; the tick's equipment diff tells the viewers.
func (s *Server) equipMobsIn(cell equipCell, targets []equipTarget) []equipTarget {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	h := s.herdOf(cell.w)
	for _, m := range h.herd {
		if math.Abs(m.X-cell.x) > equipReach || math.Abs(m.Z-cell.z) > equipReach || !cell.box.Crosses(entity.NewAABB(m.BlastBox())) {
			continue
		}
		ent := h.book[m]
		targets = append(targets, equipTarget{id: m.ID, uuid: ent.uuid, fits: func() bool {
			s.mobsMu.Lock()
			defer s.mobsMu.Unlock()
			_, live := h.book[m]
			return live && m.CanDispenserEquip(cell.stack)
		}, try: func() (equipFx, bool) {
			s.mobsMu.Lock()
			defer s.mobsMu.Unlock()
			if _, live := h.book[m]; !live || !m.DispenserEquip(cell.stack) {
				return equipFx{}, false
			}
			return equipFx{x: m.X, y: m.Y, z: m.Z, category: mobSoundCategory(m.Type), silent: m.Silent, event: true,
				sound: m.EquipSoundOverride(cell.item.Slot)}, true
		}})
	}
	return targets
}

// equipStandsIn appends the armor stands whose box crosses the cell; a stand
// that takes the piece tells the viewers.
func (s *Server) equipStandsIn(cell equipCell, targets []equipTarget) []equipTarget {
	s.armorStandsMu.Lock()
	defer s.armorStandsMu.Unlock()
	for _, ent := range s.armorStands {
		if ent.w != cell.w || !cell.box.Crosses(entity.NewAABB(ent.stand.BlastBox())) {
			continue
		}
		targets = append(targets, equipTarget{id: ent.stand.ID, uuid: ent.uuid, fits: func() bool {
			s.armorStandsMu.Lock()
			defer s.armorStandsMu.Unlock()
			return ent.stand.CanDispenserEquip(cell.stack)
		}, try: func() (equipFx, bool) {
			s.armorStandsMu.Lock()
			if !ent.stand.DispenserEquip(cell.stack) {
				s.armorStandsMu.Unlock()
				return equipFx{}, false
			}
			equipment := armorStandEquipment(ent)
			fx := equipFx{x: ent.stand.X, y: ent.stand.Y, z: ent.stand.Z, category: protocol.SoundCategoryNeutral, event: true}
			s.armorStandsMu.Unlock()
			s.broadcastWorld(cell.w, protocol.IDPlayEntityEquipment, equipment)
			return fx, true
		}})
	}
	return targets
}

// dispenseEquipTarget is the identity of the body the dispenser would dress with the piece, or
// false when the piece is no equipment behaviour, is placed on a base or nobody takes it.
func (s *Server) dispenseEquipTarget(w *world.World, visit dispenseVisit, d world.Dispense) (uuid.UUID, bool) {
	if visit.behaviour != inventory.DispenseEquipment && visit.behaviour != inventory.DispenseOptionalEquipment {
		return uuid.UUID{}, false
	}
	// Placed on a base instead of worn.
	switch registry.Item(visit.taken.ItemID) {
	case registry.ItemWitherSkeletonSkull:
		if s.witherSkullPlaces(w, d) {
			return uuid.UUID{}, false
		}
	case registry.ItemCarvedPumpkin:
		if carvedPumpkinPlaces(w, d) {
			return uuid.UUID{}, false
		}
	}
	for _, target := range s.equipTargets(w, visit.taken, d) {
		if target.fits() {
			return target.uuid, true
		}
	}
	return uuid.UUID{}, false
}

// dispenseHoneycomb waxes the copper block the face opens onto, spending the
// honeycomb; any other target gets the plain drop. A double copper chest's
// other half stays bare, and nobody is excepted from the wax-on event.
// vanilla: DispenseItemBehavior (honeycomb), HoneycombItem.getWaxed
func (s *Server) dispenseHoneycomb(w *world.World, visit dispenseVisit, d world.Dispense, dice *rules.JavaRandom) dispenseAct {
	tx, ty, tz := faceOffset(d.X, d.Y, d.Z, int32(d.Dir))
	use, ok := rules.HoneycombWax(w.BlockAt(tx, ty, tz))
	if !ok {
		s.dispenseDefaultDrop(w, visit, d, dice)
		return dispenseAct{}
	}
	s.setBlockAndTell(w, tx, ty, tz, use.State)
	s.broadcastLevelEvent(w, use.LevelEvent, tx, ty, tz, 0)
	s.broadcastSoundID(w, use.Sound, protocol.SoundCategoryBlock, float64(tx)+0.5, float64(ty)+0.5, float64(tz)+0.5, 1, 1)
	s.dispenseDefaultSound(w, d)
	s.dispenseAnimation(w, d)
	return dispenseAct{}
}
