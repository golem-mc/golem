package entity

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The piglin's and villager's eight-slot inventory, where a piglin keeps gold
// nuggets and loved items it has no hand or armour slot for, and a villager
// its food and seeds. A piglin's drops on death.
// vanilla: Piglin.inventory, InventoryCarrier

const (
	piglinInventorySize = 8
	// piglinEatCooldown is how long ATE_RECENTLY lasts, in ticks.
	// vanilla: PiglinAi.EAT_COOLDOWN
	piglinEatCooldown = 200
)

// isLoved is PiglinAi.isLovedItem.
func isLoved(stack protocol.ItemStack) bool {
	return !stack.Empty() && registry.Item(stack.ItemID).InTag(registry.ItemTagIDPiglinLoved)
}

// inventorySlots is the kind's SimpleContainer capacity.
func (m *Mob) inventorySlots() []protocol.ItemStack {
	switch m.Type {
	case TypeAllay:
		return m.barter.Inventory[:1]
	case TypePillager:
		return m.barter.Inventory[:5] // vanilla: Pillager.inventory
	}
	return m.barter.Inventory[:]
}

// canAddToInventory is SimpleContainer.canAddItem: a free slot or an
// unfilled stack of the same item.
func (m *Mob) canAddToInventory(stack protocol.ItemStack) bool {
	for _, held := range m.inventorySlots() {
		if held.Empty() || held.Same(stack) && held.Count < registry.StackMaxStackSize(held) {
			return true
		}
	}
	return false
}

// addToInventory is SimpleContainer.addItem: it fills same-item stacks, then
// empty slots, and returns what did not fit.
func (m *Mob) addToInventory(stack protocol.ItemStack) protocol.ItemStack {
	inv := m.inventorySlots()
	for i := range inv {
		if stack.Empty() {
			return stack
		}
		if held := &inv[i]; !held.Empty() && held.Same(stack) {
			n := min(stack.Count, registry.StackMaxStackSize(*held)-held.Count)
			if n > 0 {
				held.Count += n
				stack.Count -= n
			}
		}
	}
	for i := range inv {
		if stack.Empty() {
			break
		}
		if inv[i].Empty() {
			inv[i] = stack
			inv[i].Count = min(stack.Count, registry.StackMaxStackSize(stack))
			stack.Count -= inv[i].Count
		}
	}
	if stack.Empty() {
		return protocol.ItemStack{}
	}
	return stack
}

// stow puts the stack in the inventory and throws what does not fit.
// vanilla: PiglinAi.putInInventory
func (m *Mob) stow(t *tickView, stack protocol.ItemStack) {
	if rest := m.addToInventory(stack); !rest.Empty() {
		tx, ty, tz := m.randomThrowTarget()
		m.throwToward(t, rest, tx, ty, tz)
	}
}

// wearLoved equips a loved item that beats the slot's current piece, a
// loved one always beating an unloved one. The piece it replaces drops.
// vanilla: Mob.equipItemIfPossible, Piglin.canReplaceCurrentItem
func (m *Mob) wearLoved(t *tickView, stack protocol.ItemStack) bool {
	slot := lootSlotOf(stack)
	if !equippableInMobSlot(stack, slot) {
		return false
	}
	staged := stagedSlots{main: m.MainHand, off: m.OffHand, worn: m.Worn}
	current := staged.currentOf(slot)
	replace := replaceLoot(m, stack, current, slot)
	if isArmourSlot(slot) && !replace {
		slot = "mainhand"
		current = staged.currentOf(slot)
		replace = current.Empty()
	}
	if !replace {
		return false
	}
	if isArmourSlot(slot) {
		stack.Count = 1
	}
	i := lootSlotIndex(slot)
	if !current.Empty() {
		t.events = append(t.events, MobEvent{Mob: m, Kind: MobThrewItem, Stack: current, X: m.X, Y: m.Y, Z: m.Z, VY: 0.2})
	}
	m.setSlotStack(i, stack)
	m.DropChances[i] = guaranteedDropChance
	m.Persistence = true
	return true
}

// inventoryDrops empties the inventory for the death loot or the
// conversion; a villager's is lost with it.
// vanilla: Piglin.dropCustomDeathLoot, Piglin.finishConversion
func (m *Mob) inventoryDrops() []protocol.ItemStack {
	if m.Type == TypeVillager {
		return nil
	}
	var drops []protocol.ItemStack
	for i, stack := range m.barter.Inventory {
		if !stack.Empty() {
			drops = append(drops, stack)
		}
		m.barter.Inventory[i] = protocol.ItemStack{}
	}
	return drops
}

// putInventory writes the filled slots as the plain "Inventory" list.
// vanilla: InventoryCarrier.writeInventoryToTag
func (m *Mob) putInventory(out nbt.CompoundTag) {
	var items []nbt.Tag
	for _, stack := range m.barter.Inventory {
		if !stack.Empty() {
			items = append(items, registry.StackTag(stack))
		}
	}
	// Replace loaded items even after the last stack has left the inventory.
	out.Set("Inventory", nbt.List{Elem: nbt.TypeCompound, Items: items})
}

// loadInventory refills the inventory from "Inventory"; entries past the
// eight slots are lost.
// vanilla: InventoryCarrier.readInventoryFromTag, SimpleContainer.fromItemList
func (m *Mob) loadInventory(tag nbt.Compound) {
	list, ok := nbt.Get[nbt.List](tag, "Inventory")
	if !ok {
		return
	}
	m.barter.Inventory = [piglinInventorySize]protocol.ItemStack{}
	for _, entry := range list.Items {
		if item, ok := nbt.CompoundView(entry); ok {
			if stack, ok := registry.StoredStack(item); ok {
				m.addToInventory(stack)
			}
		}
	}
}

// CarriedInventory is the filled inventory slots, in slot order.
func (m *Mob) CarriedInventory() []protocol.ItemStack {
	var out []protocol.ItemStack
	for _, stack := range m.barter.Inventory {
		if !stack.Empty() {
			out = append(out, stack)
		}
	}
	return out
}
