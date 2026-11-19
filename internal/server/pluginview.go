package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/inventory"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/plugin"
	"slices"
)

// rawOutside is the raw slot of a click outside the window.
const rawOutside = -999

// pluginView is a player's open window. A window the player has since
// closed or replaced reads as empty. fixed is the window while it is being
// opened, before the player has it.
type pluginView struct {
	p     pluginPlayer
	id    int32
	fixed *windowView
}

func (c *client) pluginViewOf(cv windowView) pluginView {
	return pluginView{p: pluginPlayer{host: c.srv.pluginAdapter, c: c}, id: cv.id}
}

func (v pluginView) window() (windowView, bool) {
	if v.fixed != nil {
		return *v.fixed, true
	}
	cv := v.p.c.currentView()
	return cv, cv.id == v.id
}

func (v pluginView) Player() plugin.Player { return v.p }

func (v pluginView) Title() plugin.Text {
	cv, ok := v.window()
	if !ok || cv.kind == windowPlayer {
		return plugin.Text{}
	}
	text, _ := pluginText(cv.title())
	return text
}

func (v pluginView) SetProperty(id, value int) {
	if _, ok := v.window(); ok && v.id != 0 {
		v.p.c.queueContainerProperty(v.id, int32(id), int32(value))
	}
}

func (v pluginView) Close() {
	if _, ok := v.window(); ok && v.id != 0 {
		_ = v.p.c.closeWindowForPlugins()
	}
}

func (v pluginView) Bottom() plugin.PlayerInventory { return v.p.Inventory().(pluginPlayerInv) }

func (v pluginView) Top() plugin.Inventory {
	cv, _ := v.window()
	if cv.menu != nil {
		return cv.menu.inv
	}
	return &pluginInv{kind: pluginTopType(cv.kind), slots: topSlots{v}, holder: v.p, viewers: v.p.viewerList()}
}

var windowInventoryTypes = map[windowKind]plugin.InventoryType{
	windowChest: plugin.InvChest, windowDouble: plugin.InvChest, windowCrafting: plugin.InvCrafting,
	windowFurnace: plugin.InvFurnace, windowBlastFurnace: plugin.InvBlastFurnace, windowSmoker: plugin.InvSmoker,
	windowBrewingStand: plugin.InvBrewing, windowCrafter: plugin.InvCrafter, windowEnchant: plugin.InvEnchanting,
	windowBarrel: plugin.InvBarrel, windowShulker: plugin.InvShulkerBox, windowEnderChest: plugin.InvEnderChest,
	windowDispenser: plugin.InvDispenser, windowDropper: plugin.InvDropper, windowHopper: plugin.InvHopper,
	windowAnvil: plugin.InvAnvil, windowStonecutter: plugin.InvStonecutter, windowSmithing: plugin.InvSmithing,
	windowGrindstone: plugin.InvGrindstone, windowBeacon: plugin.InvBeacon, windowCartography: plugin.InvCartography,
	windowMerchant: plugin.InvMerchant, windowLectern: plugin.InvLectern, windowLoom: plugin.InvLoom,
	windowPlayer: plugin.InvCrafting,
}

func pluginTopType(kind windowKind) plugin.InventoryType { return windowInventoryTypes[kind] }

// topSlots are the window's own slots in the window's numbering, ahead of the
// player's things: the container's, or a menu's (the player's own window
// shows its result and 2x2 grid). Container slots can be written; a menu's
// are read-only.
type topSlots struct{ v pluginView }

func (s topSlots) size() int {
	cv, ok := s.v.window()
	switch {
	case !ok:
		return 0
	case cv.kind == windowPlayer:
		return 5
	}
	return int(cv.clickView(nil).InvBase())
}

func (s topSlots) get(i int) (out protocol.ItemStack) {
	cv, ok := s.v.window()
	if !ok {
		return out
	}
	cv.inventory(s.v.p.c, func(inv *player.Inventory) {
		cv.use(func() { out = cv.stack(inv, int32(i)) })
	})
	return out
}

func (s topSlots) set(i int, st protocol.ItemStack) {
	cv, ok := s.v.window()
	if !ok || cv.kind == windowPlayer || i < 0 || i >= s.size() {
		return
	}
	c := s.v.p.c
	type shown struct {
		slot  int32
		stack protocol.ItemStack
	}
	var announced []shown
	cv.inventory(c, func(inv *player.Inventory) {
		cv.use(func() {
			view := cv.clickView(inv)
			if view.Entity == nil && cv.w == nil {
				return
			}
			var outcome inventory.Outcome
			view.SetStack(inv, int32(i), st, &outcome)
			if cv.w != nil {
				// vanilla: AbstractContainerMenu.slotsChanged
				if cv.w.SlotsChanged(cv.kind, inv, &outcome, false) {
					c.recomputeEnchantOffers(cv)
				}
			}
			for _, slot := range outcome.Announce {
				announced = append(announced, shown{slot, cv.stack(inv, slot)})
			}
		})
	})
	for _, a := range announced {
		c.queueContainerSlot(cv.id, a.slot, a.stack)
	}
	if cv.w != nil && cv.be != nil {
		x, y, z := cv.be.X, cv.be.Y, cv.be.Z
		c.srv.containerViewers(cv.w.w, x, y, z, c, func(o *client) {
			if window := o.openContainerWindow(); window != nil {
				o.queueContainerSlot(window.id, window.slotBase(x, y, z)+int32(i), st)
			}
		})
	}
}

func (p pluginPlayer) OpenView() (plugin.InventoryView, bool) {
	cv := p.c.currentView()
	if cv.kind == windowPlayer {
		return nil, false
	}
	return p.c.pluginViewOf(cv), true
}

func (p pluginPlayer) CloseInventory() {
	_ = p.c.closeWindowForPlugins()
}

// pluginOpen fires InventoryOpenEvent for a window about to open and reports
// whether a listener cancelled it. It runs on the connection goroutine and
// waits for the tick goroutine.
func (c *client) pluginOpen(cv windowView) (cancelled bool) {
	r := c.srv.plugins
	if r == nil || !r.Has(pluginruntime.InventoryOpen) {
		return false
	}
	fire := func() {
		view := c.pluginViewOf(cv)
		view.fixed = &cv
		e := &plugin.InventoryOpenEvent{Player: view.p, View: view}
		r.Fire(e)
		cancelled = e.Cancelled()
	}
	if r.OnTick() {
		fire()
	} else {
		r.Call(fire)
	}
	return cancelled
}

// pluginClose fires InventoryCloseEvent for the window the client closes,
// while it is still open.
func (c *client) pluginClose() {
	r := c.srv.plugins
	cv := c.currentView()
	if r == nil || cv.kind == windowPlayer || !r.Has(pluginruntime.InventoryClose) {
		return
	}
	fire := func() {
		view := c.pluginViewOf(cv)
		r.Fire(&plugin.InventoryCloseEvent{Player: view.p, View: view})
	}
	if r.OnTick() {
		fire()
	} else {
		r.Call(fire)
	}
}

// clickTypes maps a Click Container packet's mode and button to the API.
// vanilla: ClickType
func pluginClickType(mode int32, button int8, slot int32) (plugin.ClickType, int) {
	switch mode {
	case protocol.ClickModePickup:
		switch {
		case slot == rawOutside && button == 0:
			return plugin.ClickWindowBorderLeft, 0
		case slot == rawOutside:
			return plugin.ClickWindowBorderRight, 0
		case button == 0:
			return plugin.ClickLeft, 0
		}
		return plugin.ClickRight, 0
	case protocol.ClickModeQuickMove:
		if button == 0 {
			return plugin.ClickShiftLeft, 0
		}
		return plugin.ClickShiftRight, 0
	case protocol.ClickModeSwap:
		if button == 40 {
			return plugin.ClickSwapOffhand, 0
		}
		return plugin.ClickNumberKey, int(button)
	case protocol.ClickModeClone:
		return plugin.ClickMiddle, 0
	case protocol.ClickModeThrow:
		if button == 0 {
			return plugin.ClickDrop, 0
		}
		return plugin.ClickControlDrop, 0
	}
	return plugin.ClickDouble, 0
}

// pluginClick fires InventoryClickEvent for a click before it runs. A
// listener's change to the clicked stack or the cursor is written first, and
// cancelled reports that the click must not run.
// vanilla: ServerGamePacketListenerImpl.handleContainerClick
func (c *client) pluginClick(cv windowView, slot int32, button int8, mode int32) (cancelled bool) {
	r := c.srv.plugins
	if r == nil {
		return false
	}
	if want, ok := resultSlots[cv.kind]; !r.Has(pluginruntime.InventoryClick) && (!ok || slot != want || !r.Has(pluginruntime.CraftItem)) {
		return false
	}
	var current protocol.ItemStack
	var craftID string
	r.Call(func() {
		view := c.pluginViewOf(cv)
		top := topSlots{view}
		topSize := int32(top.size())
		e := &plugin.InventoryClickEvent{Player: view.p, View: view, RawSlot: int(slot), Slot: -1, Button: 0}
		e.Click, e.Button = pluginClickType(mode, button, slot)
		inWindow := slot >= 0 && slot < cv.slots()
		var cursor protocol.ItemStack
		cv.inventory(c, func(inv *player.Inventory) {
			cv.use(func() {
				if inWindow {
					current = cv.stack(inv, slot)
				}
				if cells, width, ok := cv.clickView(inv).GridOf(inv); ok && slot == 0 {
					craftID, current = inv.CraftMatch(cells, width)
				}
			})
			cursor = c.container.click.Carried
		})
		e.Current, e.Cursor = pluginStack(current), pluginStack(cursor)
		switch {
		case !inWindow:
			e.Inventory = view.Bottom()
		case cv.kind == windowPlayer || slot >= topSize:
			e.Inventory = view.Bottom()
			e.Slot = pluginBottomIndex(cv, slot)
		default:
			e.Inventory, e.Slot = view.Top(), int(slot)
		}
		r.Fire(e)
		if cancelled = e.Cancelled(); cancelled {
			return
		}
		if st, ok := pluginProtocolStack(e.Cursor); ok && !sameCount(st, cursor) {
			c.SetItemIn(command.ItemSlotCursor, st)
		}
		if st, ok := pluginProtocolStack(e.Current); ok && inWindow && !sameCount(st, current) {
			c.pluginWriteWindowSlot(cv, slot, st)
		}
	})
	if want, ok := resultSlots[cv.kind]; ok && !cancelled && slot == want && !current.Empty() &&
		mode != protocol.ClickModeClone && mode != protocol.ClickModeCollect {
		return c.pluginCraft(cv, current, craftID, mode == protocol.ClickModeQuickMove)
	}
	return cancelled
}

// pluginDrag fires InventoryDragEvent as a multi-slot drag ends, before it
// writes anything, and reports whether a listener cancelled it. The event's
// Slots map is the plan's result for each marked slot; a listener's change to
// it is not applied.
// vanilla: AbstractContainerMenu.doClick QUICK_CRAFT end step
func (c *client) pluginDrag(cv windowView) (cancelled bool) {
	var edit *inventory.DragPlan
	r := c.srv.plugins
	if r == nil || !r.Has(pluginruntime.InventoryDrag) {
		return false
	}
	var plan *inventory.DragPlan
	cv.inventory(c, func(inv *player.Inventory) {
		cv.use(func() { plan = c.container.click.PlanDrag(cv.clickView(inv), inv) })
	})
	if plan == nil {
		return false
	}
	r.Call(func() {
		e := &plugin.InventoryDragEvent{Player: pluginPlayer{host: c.srv.pluginAdapter, c: c},
			View: c.pluginViewOf(cv), Slots: make(map[int]plugin.ItemStack, len(plan.Slots)),
			Cursor: pluginStack(plan.Held), Right: plan.Right}
		for i, slot := range plan.Slots {
			e.Slots[int(slot)] = pluginStack(plan.Stacks[i])
		}
		r.Fire(e)
		cancelled = e.Cancelled()
		if !cancelled {
			edit = editedDrag(plan, e)
		}
	})
	if edit != nil {
		cv.inventory(c, func(*player.Inventory) { c.container.click.DragEdit = edit })
	}
	return cancelled
}

// editedDrag is plan with the listener's changes, nil when it made none.
func editedDrag(plan *inventory.DragPlan, e *plugin.InventoryDragEvent) *inventory.DragPlan {
	out := &inventory.DragPlan{Slots: plan.Slots, Stacks: slices.Clone(plan.Stacks), Held: plan.Held, Right: plan.Right}
	changed := false
	for i, slot := range plan.Slots {
		if st, ok := pluginProtocolStack(e.Slots[int(slot)]); ok && !sameCount(st, plan.Stacks[i]) {
			out.Stacks[i], changed = st, true
		}
	}
	if st, ok := pluginProtocolStack(e.Cursor); ok && !sameCount(st, plan.Held) {
		out.Held, changed = st, true
	}
	if !changed {
		return nil
	}
	return out
}

func sameCount(a, b protocol.ItemStack) bool {
	if a.Empty() || b.Empty() {
		return a.Empty() && b.Empty()
	}
	return a.Count == b.Count && a.Same(b)
}

// pluginBottomIndex is the player-inventory handle index (hotbar first) of a
// window slot, -1 for a slot the handle lacks (off hand, crafting). The
// player window's armour slots are 36 (feet) to 39 (head), Bukkit's
// player-inventory slots for them.
const pluginArmorBase = 36

func pluginBottomIndex(cv windowView, slot int32) int {
	if cv.kind == windowPlayer {
		switch {
		case slot >= player.MainSlot && slot < player.HotbarSlot:
			return int(slot)
		case slot >= player.HotbarSlot && slot < player.HotbarSlot+player.HotbarSlots:
			return int(slot - player.HotbarSlot)
		case slot >= player.ArmorSlot && slot < player.ArmorSlot+player.ArmorSlots:
			return pluginArmorBase + int(player.ArmorSlot+player.ArmorSlots-1-slot)
		}
		return -1
	}
	n := slot - cv.clickView(nil).InvBase()
	if n < 0 || n >= player.MainInvSlots {
		return -1
	}
	if n < player.MainInvSlots-player.HotbarSlots {
		return int(n) + player.HotbarSlots
	}
	return int(n) - (player.MainInvSlots - player.HotbarSlots)
}

// pluginWriteWindowSlot writes a listener's replacement for the clicked
// slot: a container slot or one of the player's.
func (c *client) pluginWriteWindowSlot(cv windowView, slot int32, st protocol.ItemStack) {
	view := c.pluginViewOf(cv)
	if slot < int32(topSlots{view}.size()) && cv.kind != windowPlayer {
		topSlots{view}.set(int(slot), st)
		return
	}
	if idx := pluginBottomIndex(cv, slot); idx >= pluginArmorBase {
		c.SetItemIn(command.ItemSlotFeet+command.ItemSlot(idx-pluginArmorBase), st)
	} else if idx >= 0 {
		c.SetItemIn(command.ItemSlot(idx), st)
	}
}
