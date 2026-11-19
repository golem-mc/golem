package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// invSlots is a fixed-size store of slots; pluginInv builds the whole
// Inventory on it. Each store takes its own locks and tells the viewers.
type invSlots interface {
	size() int
	get(i int) protocol.ItemStack
	set(i int, s protocol.ItemStack)
}

// pluginInv is a plugin.Inventory over a slot store.
type pluginInv struct {
	kind    plugin.InventoryType
	slots   invSlots
	holder  any
	viewers func() []plugin.Player
	// storage is how many leading slots Add and FirstEmpty use, 0 for all;
	// a player's 41 slots fill only the 36 of storage.
	// vanilla: Inventory.getFreeSlot, Inventory.add
	storage int
}

// room is the number of slots Add and FirstEmpty may fill.
func (i *pluginInv) room() int {
	if i.storage > 0 {
		return i.storage
	}
	return i.Size()
}

func (i *pluginInv) Type() plugin.InventoryType { return i.kind }
func (i *pluginInv) Size() int                  { return i.slots.size() }
func (i *pluginInv) Holder() any                { return i.holder }

func (i *pluginInv) Viewers() []plugin.Player {
	if i.viewers == nil {
		return nil
	}
	return i.viewers()
}

func (i *pluginInv) Item(slot int) plugin.ItemStack {
	if slot < 0 || slot >= i.Size() {
		return plugin.ItemStack{}
	}
	return pluginStack(i.slots.get(slot))
}

// SetItem leaves the slot alone for a stack the codec refuses.
func (i *pluginInv) SetItem(slot int, s plugin.ItemStack) {
	st, ok := pluginProtocolStack(s)
	if ok && slot >= 0 && slot < i.Size() {
		i.slots.set(slot, st)
	}
}

func (i *pluginInv) Contents() []plugin.ItemStack {
	out := make([]plugin.ItemStack, i.Size())
	for n := range out {
		out[n] = pluginStack(i.slots.get(n))
	}
	return out
}

// SetContents writes the stacks in order and empties the slots past them.
func (i *pluginInv) SetContents(stacks []plugin.ItemStack) {
	for n := range i.Size() {
		if n < len(stacks) {
			i.SetItem(n, stacks[n])
		} else {
			i.slots.set(n, protocol.ItemStack{})
		}
	}
}

// Add merges into similar stacks first, then fills empty slots, as
// Container.addItem does.
// vanilla: SimpleContainer.addItem
func (i *pluginInv) Add(stacks ...plugin.ItemStack) []plugin.ItemStack {
	var left []plugin.ItemStack
	for _, s := range stacks {
		if s.Empty() {
			continue
		}
		st, ok := pluginProtoCounted(s)
		if !ok {
			left = append(left, s)
			continue
		}
		limit := registry.StackMaxStackSize(st)
		for n := 0; n < i.room() && st.Count > 0; n++ {
			cur := i.slots.get(n)
			if cur.Empty() || !cur.Same(st) || cur.Count >= limit {
				continue
			}
			moved := min(limit-cur.Count, st.Count)
			cur.Count += moved
			st.Count -= moved
			i.slots.set(n, cur)
		}
		for n := 0; n < i.room() && st.Count > 0; n++ {
			if !i.slots.get(n).Empty() {
				continue
			}
			put := st
			put.Count = min(limit, st.Count)
			st.Count -= put.Count
			i.slots.set(n, put)
		}
		if st.Count > 0 {
			rest := s
			rest.Count = int(st.Count)
			left = append(left, rest)
		}
	}
	return left
}

// Remove takes the stacks' counts out of the similar stacks, last slot
// first-served in slot order, and answers what was missing.
// vanilla: SimpleContainer.removeItemType
func (i *pluginInv) Remove(stacks ...plugin.ItemStack) []plugin.ItemStack {
	var missing []plugin.ItemStack
	for _, s := range stacks {
		st, ok := pluginProtoCounted(s)
		if !ok {
			missing = append(missing, s)
			continue
		}
		need := st.Count
		for n := 0; n < i.Size() && need > 0; n++ {
			cur := i.slots.get(n)
			if cur.Empty() || !cur.Same(st) {
				continue
			}
			taken := min(cur.Count, need)
			cur.Count -= taken
			need -= taken
			if cur.Count <= 0 {
				cur = protocol.ItemStack{}
			}
			i.slots.set(n, cur)
		}
		if need > 0 {
			rest := s
			rest.Count = int(need)
			missing = append(missing, rest)
		}
	}
	return missing
}

// pluginProtoCounted converts a stack that may exceed one slot's size: the
// codec reads a count past the item's maximum as 1, so the count is set after.
func pluginProtoCounted(s plugin.ItemStack) (protocol.ItemStack, bool) {
	count := s.Count
	s.Count = min(s.Count, 1)
	st, ok := pluginProtocolStack(s)
	if ok && !st.Empty() {
		st.Count = int32(count)
	}
	return st, ok && !st.Empty()
}

func (i *pluginInv) Count(item registry.Item) int {
	total := 0
	for n := range i.Size() {
		if cur := i.slots.get(n); !cur.Empty() && cur.ItemID == int32(item) {
			total += int(cur.Count)
		}
	}
	return total
}

func (i *pluginInv) First(s plugin.ItemStack) int {
	st, ok := pluginProtocolStack(s)
	if !ok || st.Empty() {
		return -1
	}
	for n := range i.Size() {
		if cur := i.slots.get(n); !cur.Empty() && cur.Same(st) {
			return n
		}
	}
	return -1
}

func (i *pluginInv) FirstEmpty() int {
	for n := range i.room() {
		if i.slots.get(n).Empty() {
			return n
		}
	}
	return -1
}

func (i *pluginInv) Clear() {
	for n := range i.Size() {
		if !i.slots.get(n).Empty() {
			i.slots.set(n, protocol.ItemStack{})
		}
	}
}

// playerSlots is a player's slots addressed as /item does: the 36 of the
// inventory with the hotbar first, or the 27 of the ender chest.
type playerSlots struct {
	c     *client
	base  command.ItemSlot
	count int
}

func (s playerSlots) size() int { return s.count }
func (s playerSlots) get(i int) protocol.ItemStack {
	stack, _ := s.c.ItemIn(s.base + command.ItemSlot(i))
	return stack
}
func (s playerSlots) set(i int, st protocol.ItemStack) { s.c.SetItemIn(s.base+command.ItemSlot(i), st) }

func (p pluginPlayer) viewerList() func() []plugin.Player {
	return func() []plugin.Player {
		if p.IsValid() {
			return []plugin.Player{p}
		}
		return nil
	}
}

// pluginPlayerInv is a player's inventory with its equipment and held slot.
type pluginPlayerInv struct {
	*pluginInv
	pluginPlayerEquipment
	p pluginPlayer
}

func (p pluginPlayer) Inventory() plugin.PlayerInventory {
	return pluginPlayerInv{&pluginInv{kind: plugin.InvPlayer, slots: invseeSlots{p.c},
		holder: p, viewers: p.viewerList(), storage: 36}, pluginPlayerEquipment{p.c}, p}
}

func (p pluginPlayer) EnderChest() plugin.Inventory {
	return &pluginInv{kind: plugin.InvEnderChest, slots: playerSlots{c: p.c, base: command.ItemSlotEnder, count: player.EnderSlots},
		holder: p, viewers: p.viewerList()}
}

func (i pluginPlayerInv) HeldSlot() int {
	var selected int32
	i.p.c.player.Inventory(func(inv *player.Inventory) { selected = inv.Selected })
	return int(selected)
}

func (i pluginPlayerInv) SetHeldSlot(slot int) {
	if slot < 0 || slot >= player.HotbarSlots {
		return
	}
	c := i.p.c
	c.player.Inventory(func(inv *player.Inventory) { inv.Selected = int32(slot) })
	_ = c.sendHeldItem()
	c.broadcastEquipment()
}

// pluginPlayerEquipment is a player's worn and held items. A player drops
// all of it on death, so drop chances read 0 (the unused value) and ignore
// writes.
type pluginPlayerEquipment struct{ c *client }

func (p pluginPlayer) Equipment() plugin.Equipment { return pluginPlayerEquipment{p.c} }

func (e pluginPlayerEquipment) Get(s plugin.EquipmentSlot) plugin.ItemStack {
	if int(s) >= len(equipmentSlots) {
		return plugin.ItemStack{}
	}
	stack, _ := e.c.ItemIn(equipmentSlots[s])
	return pluginStack(stack)
}

func (e pluginPlayerEquipment) Set(s plugin.EquipmentSlot, item plugin.ItemStack) {
	if st, ok := pluginProtocolStack(item); ok && int(s) < len(equipmentSlots) {
		e.c.SetItemIn(equipmentSlots[s], st)
	}
}
func (pluginPlayerEquipment) DropChance(plugin.EquipmentSlot) float32     { return 0 }
func (pluginPlayerEquipment) SetDropChance(plugin.EquipmentSlot, float32) {}

func (p pluginPlayer) Cursor() plugin.ItemStack {
	stack, _ := p.c.ItemIn(command.ItemSlotCursor)
	return pluginStack(stack)
}

func (p pluginPlayer) SetCursor(s plugin.ItemStack) {
	if st, ok := pluginProtocolStack(s); ok {
		p.c.SetItemIn(command.ItemSlotCursor, st)
	}
}

// blockSlots is a container block entity's slots. A write reaches every
// window open on the container.
type blockSlots struct{ b pluginBlockEntity }

func (s blockSlots) size() int {
	return pluginBlockEntityRead(s.b, func(e *world.BlockEntity) int { return e.Len() })
}

func (s blockSlots) get(i int) protocol.ItemStack {
	return pluginBlockEntityRead(s.b, func(e *world.BlockEntity) protocol.ItemStack {
		return containerSlots(e, s.b.world.dim.w).Slot(i)
	})
}

func (s blockSlots) set(i int, st protocol.ItemStack) {
	w := s.b.world.dim.w
	if !s.b.mutate(func(e *world.BlockEntity) { containerSlots(e, w).Put(i, st) }) {
		return
	}
	x, y, z := s.b.entity.X, s.b.entity.Y, s.b.entity.Z
	// Menu packets take connection locks, so they go after the entity's is released.
	s.b.world.srv.containerViewers(w, x, y, z, nil, func(c *client) {
		if window := c.openContainerWindow(); window != nil {
			c.queueContainerSlot(window.id, window.slotBase(x, y, z)+int32(i), st)
		}
	})
}

// containerInventoryTypes maps container kinds to the API's kinds.
var containerInventoryTypes = map[world.BlockEntityKind]plugin.InventoryType{
	world.KindChest: plugin.InvChest, world.KindTrappedChest: plugin.InvChest, world.KindBarrel: plugin.InvBarrel,
	world.KindShulkerBox: plugin.InvShulkerBox, world.KindHopper: plugin.InvHopper,
	world.KindDispenser: plugin.InvDispenser, world.KindDropper: plugin.InvDropper,
	world.KindCrafter: plugin.InvCrafter, world.KindBrewingStand: plugin.InvBrewing,
	world.KindFurnace: plugin.InvFurnace, world.KindBlastFurnace: plugin.InvBlastFurnace, world.KindSmoker: plugin.InvSmoker,
}

// Inventory is the container's own slots; a double chest's halves are two
// inventories.
func (b pluginContainer) Inventory() plugin.Inventory {
	return &pluginInv{
		kind:   containerInventoryTypes[b.entity.Kind],
		slots:  blockSlots{b.pluginBlockEntity},
		holder: pluginBlockEntityHandle(b.world, b.entity),
		viewers: func() (out []plugin.Player) {
			e := b.entity
			b.world.srv.containerViewers(b.world.dim.w, e.X, e.Y, e.Z, nil, func(c *client) {
				out = append(out, pluginPlayer{host: b.world.srv.pluginAdapter, c: c})
			})
			return out
		},
	}
}
