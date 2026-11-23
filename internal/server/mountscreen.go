package server

import (
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/inventory"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// mountScreenRangeExtra is the buffer a mount screen keeps beyond the entity
// interaction range. vanilla: AbstractMountInventoryMenu.stillValid
const mountScreenRangeExtra = 4.0

// inventory runs fn under the player's inventory lock; a mount's window takes
// the mob lock first, since the mob tick reads players under it.
func (v windowView) inventory(c *client, fn func(*player.Inventory)) {
	if v.mount == nil {
		c.player.Inventory(fn)
		return
	}
	v.srv.mobsMu.Lock()
	c.player.Inventory(fn)
	sounds := v.mount.equipSounds
	v.mount.equipSounds = nil
	m := v.mount.mob
	x, y, z, silent := m.X, m.Y, m.Z, m.Silent
	v.srv.mobsMu.Unlock()
	if silent {
		return
	}
	for _, sound := range sounds {
		v.srv.broadcastSoundHolder(v.mount.w, sound, protocol.SoundCategoryNeutral, x, y, z, 1, 1)
	}
}

// mountLive reports whether the mount is alive and still in its world's
// herd. The caller holds the mob lock.
func (s *Server) mountLive(ent *mobEntity) bool {
	return ent.mob.Alive() && s.herdOf(ent.w).book[ent.mob] == ent
}

// mountSlots is a horse's saddle, body armour and chest slots as a window
// Container, in the mount screen's order; the caller holds the mob lock.
type mountSlots struct {
	ent *mobEntity
}

// gear returns the saddle or armour stack, and false for a chest slot.
func (b mountSlots) gear(slot int) (protocol.ItemStack, bool) {
	switch slot {
	case inventory.HorseSaddleSlot:
		return b.ent.mob.Horse.Saddle, true
	case inventory.HorseArmorSlot:
		return b.ent.mob.Body, true
	}
	return protocol.ItemStack{}, false
}

// Slot is what the given one of the mount's slots holds; a chest slot past
// the chest reads empty.
func (b mountSlots) Slot(slot int) protocol.ItemStack {
	if g, ok := b.gear(slot); ok {
		return g
	}
	items := b.ent.mob.Horse.Items
	if i := slot - inventory.HorseBaseSlots; i >= 0 && i < len(items) {
		return items[i]
	}
	return protocol.ItemStack{}
}

// Put replaces the slot's stack. Fitting a piece queues its equip sound,
// which inventory plays once the locks are released.
// vanilla: ArmorSlot.setByPlayer, LivingEntity.onEquipItem
func (b mountSlots) Put(slot int, stack protocol.ItemStack) {
	if g, ok := b.gear(slot); ok {
		if !stack.Empty() && !stack.Equal(g) {
			if item, ok := registry.StackEquippable(stack); ok {
				b.ent.equipSounds = append(b.ent.equipSounds, item.EquipSound)
			}
		}
		if slot == inventory.HorseArmorSlot {
			b.ent.mob.SetEquipment(6, stack)
			if !stack.Empty() {
				b.ent.mob.BodyDropChance = entity.GuaranteedDropChance
			}
		} else {
			b.ent.mob.SetMetadataHorseSaddle(stack)
			if !stack.Empty() {
				b.ent.mob.SaddleDropChance = entity.GuaranteedDropChance
			}
		}
		return
	}
	items := b.ent.mob.Horse.Items
	if i := slot - inventory.HorseBaseSlots; i >= 0 && i < len(items) {
		items[i] = stack
	}
}

// Fits reports whether the mount wears the stack in its saddle or armour
// slot; the chest slots take anything.
// vanilla: ArmorSlot.mayPlace
func (b mountSlots) Fits(slot int, stack protocol.ItemStack) bool {
	if slot < len(inventory.HorseSlotNames) {
		return b.ent.mob.Wears(inventory.HorseSlotNames[slot], stack)
	}
	return true
}

// openMountScreen opens a tame horse's screen: its saddle, armour and any
// chest. Nothing opens for an untamed horse or one somebody else rides.
// vanilla: AbstractHorse.openCustomInventoryScreen, ServerPlayer.openHorseInventory
func (c *client) openMountScreen(ent *mobEntity) error {
	s := c.srv
	s.mobsMu.Lock()
	m := ent.mob
	passengers := m.PassengerIDs()
	ok := s.mountLive(ent) && (m.Horse.Tame || entity.IsNautilus(m.Type) && m.Tamable.Tame) &&
		(len(passengers) == 0 || slices.Contains(passengers, c.player.EntityID))
	columns := m.ChestColumns()
	s.mobsMu.Unlock()
	if !ok {
		return nil
	}
	return c.openVehicleCargo(&openWindow{kind: windowHorse, w: ent.w, mount: ent, columns: columns})
}

// openRiddenMountScreen is the inventory key pressed while riding: it opens
// the ridden mount's screen or chest boat's cargo.
// vanilla: ServerGamePacketListenerImpl.handlePlayerCommand's OPEN_INVENTORY
func (c *client) openRiddenMountScreen() error {
	if it := c.vehicle.Load(); it != nil && entity.IsChestBoat(it.kind) {
		return c.openBoatCargo(it)
	}
	ent := c.mount.Load()
	if ent == nil || (!entity.IsEquine(ent.mob.Type) && !entity.IsNautilus(ent.mob.Type)) {
		return nil
	}
	return c.openMountScreen(ent)
}

// mountStillOpen reports whether the player may keep the mount's screen: the
// horse is alive in the player's world and in reach, and has neither gained
// nor lost its chest.
// vanilla: AbstractMountInventoryMenu.stillValid
func (c *client) mountStillOpen(window *openWindow) bool {
	s := c.srv
	w := c.theWorld()
	s.mobsMu.Lock()
	ent := window.mount
	valid := s.mountLive(ent) && ent.mob.ChestColumns() == window.columns && ent.w == w
	minX, minY, minZ, maxX, maxY, maxZ := ent.mob.BlastBox()
	s.mobsMu.Unlock()
	if !valid {
		return false
	}
	pos := c.player.Position()
	return rules.WithinEntityInteractionRangeBuffered(c.entityRange(),
		mountScreenRangeExtra, pos.X, pos.Y+c.dims(c.player.Appearance().Pose).EyeHeight, pos.Z,
		minX, minY, minZ, maxX, maxY, maxZ)
}

// broadcastMountSlot tells every other player with the mount's screen open
// that one of its slots changed; every screen over a mount numbers its
// slots alike.
func (s *Server) broadcastMountSlot(ent *mobEntity, slot int32, except *client) {
	s.mobsMu.Lock()
	stack := mountSlots{ent: ent}.Slot(int(slot))
	s.mobsMu.Unlock()
	s.mu.RLock()
	var viewers []*client
	for _, c := range s.clients {
		if window := c.openContainerWindow(); c != except && window != nil && window.mount == ent {
			viewers = append(viewers, c)
		}
	}
	s.mu.RUnlock()
	for _, c := range viewers {
		if window := c.openContainerWindow(); window != nil {
			c.queueContainerSlot(window.id, slot, stack)
		}
	}
}
