package server

import (
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/inventory"
	"slices"
	"sync"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/plugin"
)

// pluginMenu is a chest menu a plugin made: slots no block or entity holds,
// shown to any number of players. mu guards slots, and nests inside a
// player's inventory lock like a boat's cargo lock.
type pluginMenu struct {
	mu    sync.Mutex
	kind  windowKind // windowMenu for a chest, else the container kind whose layout it takes
	slots []protocol.ItemStack
	title protocol.Text
	inv   *pluginInv
	// mirror is non-nil for a menu showing another player's inventory.
	mirror *menuMirror
}

// menuSlots is a menu's slots as a window Container; the caller holds the
// menu lock through windowView.use.
type menuSlots struct{ m *pluginMenu }

func (s menuSlots) Slot(i int) protocol.ItemStack {
	if i < 0 || i >= len(s.m.slots) {
		return protocol.ItemStack{}
	}
	return s.m.slots[i]
}

func (s menuSlots) Put(i int, st protocol.ItemStack) {
	if i >= 0 && i < len(s.m.slots) {
		s.m.slots[i] = st
	}
}

// SlotDisabled reports a slot past the store a mirrored menu follows: the
// rows' spare cells, which an item put there would be lost from.
func (s menuSlots) SlotDisabled(i int) bool {
	return s.m.mirror != nil && i >= len(s.m.mirror.last)
}

// menuInvSlots is a menu's slot store for the plugin handle; a write reaches
// every viewer.
type menuInvSlots struct {
	s *Server
	m *pluginMenu
}

func (a menuInvSlots) size() int { return len(a.m.slots) }

func (a menuInvSlots) get(i int) protocol.ItemStack {
	a.m.mu.Lock()
	defer a.m.mu.Unlock()
	return menuSlots{a.m}.Slot(i)
}

func (a menuInvSlots) set(i int, st protocol.ItemStack) {
	a.m.mu.Lock()
	menuSlots{a.m}.Put(i, st)
	a.m.mu.Unlock()
	a.s.syncMenuViewers(a.m)
}

// stationKinds are the menu types with no block to hold their slots: the
// window keeps them for each viewer, as a crafting table's does.
var stationKinds = map[plugin.InventoryType]windowKind{
	plugin.InvCrafting: windowCrafting, plugin.InvAnvil: windowAnvil, plugin.InvGrindstone: windowGrindstone,
	plugin.InvLoom: windowLoom, plugin.InvSmithing: windowSmithing, plugin.InvStonecutter: windowStonecutter,
	plugin.InvCartography: windowCartography, plugin.InvEnchanting: windowEnchant, plugin.InvBeacon: windowBeacon,
	plugin.InvMerchant: windowMerchant,
}

// stationSlots is the inventory CreateInventory answers for a station: a
// template that OpenInventory turns into a window of its own for each viewer,
// whose slots are then the view's Top. It holds no stacks itself.
type stationSlots struct {
	kind  windowKind
	title protocol.Text
}

func (s stationSlots) size() int                 { return int(s.kind.Layout().InvBase) }
func (stationSlots) get(int) protocol.ItemStack  { return protocol.ItemStack{} }
func (stationSlots) set(int, protocol.ItemStack) {}

// containerMenuKinds are the container types a plugin menu can take the
// layout of: slots a plugin owns, with the kind's screen and shift-click rules.
var containerMenuKinds = map[plugin.InventoryType]struct {
	kind  windowKind
	slots int
}{
	plugin.InvHopper: {windowHopper, 5}, plugin.InvDispenser: {windowDispenser, 9}, plugin.InvDropper: {windowDropper, 9},
	plugin.InvBarrel: {windowBarrel, 27}, plugin.InvShulkerBox: {windowShulker, 27},
	plugin.InvFurnace: {windowFurnace, 3}, plugin.InvBlastFurnace: {windowBlastFurnace, 3}, plugin.InvSmoker: {windowSmoker, 3},
	plugin.InvBrewing: {windowBrewingStand, 5}, plugin.InvCrafter: {windowCrafter, 9}, plugin.InvLectern: {windowLectern, 1},
}

// CreateInventory answers a chest menu of 1 to 6 rows, or a station menu
// (crafting table, anvil, grindstone, loom, smithing table, stonecutter,
// cartography table, enchanting table, beacon, merchant), or a furnace, brewing stand, crafter, lectern, hopper, dispenser, dropper, barrel or shulker box
// whose slots the plugin holds; any other kind or row count answers nil.
func (h *pluginHost) CreateInventory(kind plugin.InventoryType, rows int, title plugin.Text, holder any) plugin.Inventory {
	if wk, ok := stationKinds[kind]; ok {
		return &pluginInv{kind: kind, slots: stationSlots{kind: wk, title: pluginProtocolText(title)}, holder: holder}
	}
	if ck, ok := containerMenuKinds[kind]; ok {
		inv := h.newMenu(make([]protocol.ItemStack, ck.slots), pluginProtocolText(title), holder, nil)
		inv.kind = kind
		inv.slots.(menuInvSlots).m.kind = ck.kind
		return inv
	}
	if kind != plugin.InvChest || rows < 1 || rows > 6 {
		return nil
	}
	return h.newMenu(make([]protocol.ItemStack, 9*rows), pluginProtocolText(title), holder, nil)
}

func (h *pluginHost) newMenu(slots []protocol.ItemStack, title protocol.Text, holder any, mirror *menuMirror) *pluginInv {
	m := &pluginMenu{kind: windowMenu, slots: slots, title: title, mirror: mirror}
	m.inv = &pluginInv{kind: plugin.InvChest, slots: menuInvSlots{h.srv, m}, holder: holder,
		viewers: func() (out []plugin.Player) {
			h.srv.menuViewers(m, func(c *client) { out = append(out, pluginPlayer{host: h, c: c}) })
			return out
		}}
	return m.inv
}

// menuMirror ties a menu to a live store (another player's inventory or
// ender chest): last is what the store and the menu agreed on, so a sync
// copies a change on either side to the other. The store is read and written
// with no menu lock held, since it takes the owner's inventory lock.
type menuMirror struct {
	src  invSlots
	live func() bool
	last []protocol.ItemStack
}

// invseeSlots is a player's inventory as /invsee shows it and Bukkit numbers
// it: the 36 slots with the hotbar first, then boots, leggings, chestplate,
// helmet and the offhand.
type invseeSlots struct{ c *client }

var invseeExtra = [...]command.ItemSlot{command.ItemSlotFeet, command.ItemSlotLegs, command.ItemSlotChest, command.ItemSlotHead, command.ItemSlotOffHand}

func (s invseeSlots) size() int { return 36 + len(invseeExtra) }

func (s invseeSlots) slot(i int) command.ItemSlot {
	if i < 36 {
		return command.ItemSlot(i)
	}
	return invseeExtra[i-36]
}

func (s invseeSlots) get(i int) protocol.ItemStack {
	stack, _ := s.c.ItemIn(s.slot(i))
	return stack
}
func (s invseeSlots) set(i int, st protocol.ItemStack) { s.c.SetItemIn(s.slot(i), st) }

// syncMenuMirror copies changes between a mirrored menu and its store, in
// both directions; the menu's side wins when both changed the same slot.
func (s *Server) syncMenuMirror(m *pluginMenu) {
	mir := m.mirror
	n := mir.src.size()
	m.mu.Lock()
	cur := slices.Clone(m.slots[:n])
	last := slices.Clone(mir.last)
	m.mu.Unlock()
	pulled := false
	for i := range n {
		if !cur[i].Equal(last[i]) {
			mir.src.set(i, cur[i])
			last[i] = cur[i]
			continue
		}
		if live := mir.src.get(i); !live.Equal(last[i]) {
			cur[i], last[i], pulled = live, live, true
		}
	}
	m.mu.Lock()
	for i := range n {
		// A click since the snapshot keeps its slot for the next sync.
		if pulled && m.slots[i].Equal(mir.last[i]) {
			m.slots[i] = cur[i]
		}
	}
	mir.last = last
	m.mu.Unlock()
	if pulled {
		s.syncMenuViewers(m)
	}
}

// menuViewers calls fn for every player with the menu open.
func (s *Server) menuViewers(m *pluginMenu, fn func(*client)) {
	s.mu.RLock()
	clients := slices.Collect(func(yield func(*client) bool) {
		for _, c := range s.clients {
			if !yield(c) {
				return
			}
		}
	})
	s.mu.RUnlock()
	for _, c := range clients {
		if w := c.openContainerWindow(); w != nil && w.menu == m {
			fn(c)
		}
	}
}

// syncMenuViewers sends the menu's slots to its viewers after the menu lock
// is released.
func (s *Server) syncMenuViewers(m *pluginMenu) {
	m.mu.Lock()
	slots := slices.Clone(m.slots)
	m.mu.Unlock()
	s.menuViewers(m, func(c *client) { c.sendContainerChanges(c.openContainerWindow(), 0, slots) })
}

// OpenInventory shows a menu made by CreateInventory, or another player's
// live inventory or ender chest (/invsee, /enderchest); any other inventory
// answers nil, as does a cancelled InventoryOpenEvent.
func (p pluginPlayer) OpenInventory(inv plugin.Inventory) plugin.InventoryView {
	var pi *pluginInv
	switch v := inv.(type) {
	case *pluginInv:
		pi = v
	case pluginPlayerInv:
		pi = v.pluginInv
	}
	if pi == nil {
		return nil
	}
	c := p.c
	switch slots := pi.slots.(type) {
	case menuInvSlots:
		return p.openWindow(&openWindow{kind: slots.m.kind, w: c.theWorld(), menu: slots.m})
	case stationSlots:
		window := &openWindow{kind: slots.kind, w: c.theWorld(), virtual: true, title: &slots.title}
		if slots.kind == windowEnchant {
			// No table to count shelves around, so no offers (vanilla: ContainerLevelAccess.NULL).
			window.Props = inventory.EnchantStartProps(c.xpSeed.Load())
		}
		return p.openWindow(window)
	case invseeSlots:
		return p.openMirror(slots.c, slots, protocol.Text{Text: slots.c.player.Name}, plugin.InvPlayer)
	case playerSlots:
		return p.openMirror(slots.c, slots, protocol.Text{Translate: "container.enderchest"}, plugin.InvEnderChest)
	}
	return nil
}

// openMirror shows a menu that follows a store of owner's, closed when the
// owner leaves; kind is the type its top inventory reports.
func (p pluginPlayer) openMirror(owner *client, src invSlots, title protocol.Text, kind plugin.InventoryType) plugin.InventoryView {
	h := p.host
	live := func() bool { return !owner.leaving.Load() && h.srv.clientByUUID(owner.profile.UUID) == owner }
	if !live() {
		return nil
	}
	size := src.size()
	slots := make([]protocol.ItemStack, (size+8)/9*9)
	last := make([]protocol.ItemStack, size)
	for i := range size {
		slots[i] = src.get(i)
		last[i] = slots[i]
	}
	menu := h.newMenu(slots, title, pluginPlayer{host: h, c: owner}, &menuMirror{src: src, live: live, last: last})
	menu.kind = kind
	return p.openWindow(&openWindow{kind: windowMenu, w: p.c.theWorld(), menu: menu.slots.(menuInvSlots).m})
}

// openWindow shows a window to the player, firing InventoryOpenEvent.
func (p pluginPlayer) openWindow(window *openWindow) plugin.InventoryView {
	c := p.c
	view := windowView{kind: window.kind, w: window, srv: c.srv, menu: window.menu}
	if c.pluginOpen(view) {
		return nil
	}
	c.pluginClose()
	if c.openVehicleCargo(window) != nil {
		return nil
	}
	return c.pluginViewOf(windowView{id: window.id})
}
