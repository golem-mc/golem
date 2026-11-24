package server

import (
	"fmt"
	"slices"
	"sync"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/inventory"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// Container windows and their packets. The window model (slot numbering,
// click modes, crafting) is package inventory's, which runs a click once
// decoded here.
//
// The client sends its predicted result with each click; the server runs
// the click itself and corrects every disagreeing slot. The server alone
// decides what a window holds, the cursor stack included, so the client's
// view is only a hint. A click with a stale state ID still runs against the
// server's slots; the stale state only changes the answer from the changed
// slots to the whole window.
// vanilla: ServerGamePacketListenerImpl.handleContainerClick

// windowKind is the open window's kind, as package inventory numbers them.
type windowKind = inventory.Kind

const (
	windowPlayer       = inventory.KindPlayer
	windowChest        = inventory.KindChest
	windowCrafting     = inventory.KindCrafting
	windowFurnace      = inventory.KindFurnace
	windowBlastFurnace = inventory.KindBlastFurnace
	windowSmoker       = inventory.KindSmoker
	windowBrewingStand = inventory.KindBrewingStand
	windowCrafter      = inventory.KindCrafter
	windowEnchant      = inventory.KindEnchant
	windowBarrel       = inventory.KindBarrel
	windowShulker      = inventory.KindShulker
	windowEnderChest   = inventory.KindEnderChest
	windowDispenser    = inventory.KindDispenser
	windowDropper      = inventory.KindDropper
	windowHopper       = inventory.KindHopper
	windowAnvil        = inventory.KindAnvil
	windowDouble       = inventory.KindDoubleChest
	windowJukebox      = inventory.KindJukebox
	windowStonecutter  = inventory.KindStonecutter
	windowSmithing     = inventory.KindSmithing
	windowGrindstone   = inventory.KindGrindstone
	windowBeacon       = inventory.KindBeacon
	windowCartography  = inventory.KindCartography
	windowMerchant     = inventory.KindMerchant
	windowDecoratedPot = inventory.KindDecoratedPot
	windowShelf        = inventory.KindShelf
	windowBookshelf    = inventory.KindChiseledBookshelf
	windowLectern      = inventory.KindLectern
	windowLoom         = inventory.KindLoom
	windowHorse        = inventory.KindHorse
	windowMenu         = inventory.KindMenu
)

// openWindow is a container window an Open Screen put in front of the
// player: its protocol ID, the container's location (its world included,
// since after a dimension change the player's world and the container's
// differ), and inventory.Window.
//
// A chest boat's window has no block entity: its slots are the boat's
// cargo and boat names the hull. It closes once the boat is out of reach or
// gone, and has no lid or block to tell neighbours about.
//
// A beacon's window has a block entity its slots are not in: the payment
// is in the window's menu (like the enchanting table's lapis), while the
// pyramid level and chosen effects are in the beacon's block entity.
type openWindow struct {
	id      int32
	kind    windowKind
	w       *world.World
	x, y, z int32

	// second is a double chest's left half; x, y, z is then the right half,
	// whose slots come first.
	second [3]int32

	// boat is the chest boat a windowChest reads; nil for block windows.
	boat *boatEntity
	// cart is the container minecart a window reads; nil for block and boat
	// windows.
	cart *minecartEntity

	// trader is the merchant a window of kind windowMerchant trades with.
	trader *merchant

	// menu is the plugin chest a windowMenu shows.
	menu *pluginMenu

	// virtual is a station (crafting table, anvil, ...) a plugin opened with
	// no block: x, y, z mean nothing, and nothing happens there (no sound, no
	// anvil wear), as with vanilla's ContainerLevelAccess.NULL. title is the
	// plugin's name for it.
	virtual bool
	title   *protocol.Text

	// mount is the horse whose screen a windowHorse shows, and columns its
	// chest columns when the screen opened; a chest coming or going closes it.
	mount   *mobEntity
	columns int32

	// Window is guarded by this player's inventory lock, like every window
	// packet.
	inventory.Window
}

// setSent records what the player was last told slot holds. The caller
// holds the inventory lock, which guards Sent.
func (o *openWindow) setSent(slot int32, stack protocol.ItemStack) {
	if o == nil {
		return
	}
	o.SetSent(slot, stack)
}

// containerSession is the state of this player's windows. All but the
// state ID is written by the connection's reader goroutine (the only one
// that opens, closes and clicks) or the player's save path, both under the
// inventory lock, which keeps a click from racing the player's departure.
//
// The state ID lives in player.Inventory, bumped by every window packet;
// the furnace tick sends those too from the main loop, so every builder
// runs under the inventory lock. The window pointer has its own small mutex
// because other connections read it (the furnace tick finding watchers, a
// chest click reaching others at the same chest), as a leaf, never while
// holding an inventory or block entity lock.
type containerSession struct {
	mu     sync.Mutex
	window *openWindow

	// click is package inventory's session (cursor stack, paint drag, window ID
	// counter), read under the inventory lock.
	click inventory.Session
}

// openContainerWindow returns the open container window, nil when only the
// player's own is.
func (c *client) openContainerWindow() *openWindow {
	c.container.mu.Lock()
	defer c.container.mu.Unlock()
	return c.container.window
}

// setOpenWindow records the open container window, or clears it for nil.
// The small mutex lets the server read who has what open; it is never held
// across an inventory or block entity lock.
func (c *client) setOpenWindow(w *openWindow) {
	c.container.mu.Lock()
	defer c.container.mu.Unlock()
	if c.container.window == nil && w != nil {
		c.srv.openContainerWindows.Add(1)
	} else if c.container.window != nil && w == nil {
		c.srv.openContainerWindows.Add(-1)
	}
	c.container.window = w
}

// doubleChestHalf is how many slots each half of a double chest holds.
const doubleChestHalf = 27

// halves are the container blocks the window shows, in its slot order.
func (o *openWindow) halves() [][3]int32 {
	if o.kind == windowDouble {
		return [][3]int32{{o.x, o.y, o.z}, o.second}
	}
	return [][3]int32{{o.x, o.y, o.z}}
}

// covers reports whether the window shows the container at the position;
// a chest boat's covers none.
func (o *openWindow) covers(w *world.World, x, y, z int32) bool {
	return o.boat == nil && o.cart == nil && o.menu == nil && !o.virtual && o.w == w && slices.Contains(o.halves(), [3]int32{x, y, z})
}

// slotBase is where the container at the position starts in the window's
// numbering.
func (o *openWindow) slotBase(x, y, z int32) int32 {
	if o.kind == windowDouble && o.second == [3]int32{x, y, z} {
		return doubleChestHalf
	}
	return 0
}

// windowView is the open window in the click code's terms: kind, client ID,
// the open window (for the crafting grid), and the block entity holding the
// container's slots, plus be2 for a double chest's left half. A chest boat's
// window has boat and no block entity; srv holds the hull so a click can
// take the cargo lock as a leaf.
type windowView struct {
	kind windowKind
	id   int32
	w    *openWindow
	srv  *Server
	be   *world.BlockEntity
	be2  *world.BlockEntity
	boat *boatEntity
	cart *minecartEntity
	menu *pluginMenu
	// mount is a windowHorse's horse, whose slots the mob lock guards.
	mount *mobEntity
}

// use runs fn holding the window's block entity locks, a double chest's
// right half before its left, the order every path uses. A chest boat holds
// the boat lock instead, so a click reads and writes its cargo as one move.
func (v windowView) use(fn func()) {
	switch {
	case v.mount != nil:
		// The mob lock is already held, taken by inventory.
		fn()
	case v.menu != nil:
		v.menu.mu.Lock()
		fn()
		v.menu.mu.Unlock()
	case v.boat != nil:
		v.srv.boatsMu.Lock()
		fn()
		v.srv.boatsMu.Unlock()
	case v.cart != nil:
		v.srv.minecartsMu.Lock()
		fn()
		v.srv.minecartsMu.Unlock()
	case v.be == nil:
		fn()
	case v.be2 == nil:
		v.be.Use(func(*world.BlockEntity) { fn() })
	default:
		v.be.Use(func(*world.BlockEntity) {
			v.be2.Use(func(*world.BlockEntity) { fn() })
		})
	}
}

// useLive is use for clicks: fn runs only while the container is still in
// the world, so a click that found the block entity before a blast, break
// or command removed it is refused rather than acting on slots about to
// spill. It reports whether fn ran.
func (v windowView) useLive(fn func()) bool {
	ran := false
	run := func() {
		ran = true
		fn()
	}
	switch {
	case v.mount != nil:
		if v.srv.mountLive(v.mount) {
			run()
		}
	case v.boat != nil || v.cart != nil || v.menu != nil:
		// The entity's tick writes the cargo too (a hopper cart's pull), so the
		// click holds the same lock use takes.
		v.use(run)
	case v.be == nil:
		run()
	case v.be2 == nil:
		v.be.UseLive(func(*world.BlockEntity) { run() })
	default:
		v.be.UseLive(func(*world.BlockEntity) {
			v.be2.UseLive(func(*world.BlockEntity) { run() })
		})
	}
	return ran
}

// currentView describes the open window: the player's own when no
// container is open, since the client can always click its inventory. A
// container whose block entity vanished (chunk unloaded) is treated as the
// player's own; the client's next click learns the difference.
func (c *client) currentView() windowView {
	w := c.openContainerWindow()
	if w == nil {
		return windowView{kind: windowPlayer}
	}
	view := windowView{kind: w.kind, id: w.id, w: w, srv: c.srv, boat: w.boat, cart: w.cart, menu: w.menu, mount: w.mount}
	if w.kind.EntitySlots() && w.boat == nil && w.cart == nil && w.menu == nil {
		// Look up the block entity in the window's world, where it was opened.
		view.be = w.w.BlockEntity(w.x, w.y, w.z)
		if view.be == nil {
			return windowView{kind: windowPlayer}
		}
		if w.kind == windowDouble {
			view.be2 = w.w.BlockEntity(w.second[0], w.second[1], w.second[2])
			if view.be2 == nil {
				return windowView{kind: windowPlayer}
			}
		}
	}
	if w.kind == windowBeacon {
		// A beacon window has no entity slots, but its numbers and name are in the
		// block entity.
		view.be = w.w.BlockEntity(w.x, w.y, w.z)
	}
	return view
}

// layout is the part of the window's shape the protocol fixes.
func (v windowView) layout() inventory.Layout { return v.kind.Layout() }

// title is Open Screen's window name: the first custom name of the block
// entities (a double chest's right half, then left), else the kind's
// translated default. A beacon's view has no block entity, so its own name
// is used. vanilla: ChestBlock (a pair takes the first named half, then the second),
// BeaconBlockEntity.getDisplayName
func (v windowView) title() protocol.Text {
	for _, be := range []*world.BlockEntity{v.be, v.be2} {
		if be == nil {
			continue
		}
		var name *protocol.Text
		be.Use(func(b *world.BlockEntity) { name = b.CustomName })
		if name != nil {
			return *name
		}
	}
	if v.menu != nil {
		return v.menu.title
	}
	if v.w != nil && v.w.title != nil {
		return *v.w.title
	}
	if v.boat != nil {
		// vanilla: AbstractChestBoat.interact opens with the entity as
		// MenuProvider, so the title is Entity.getDisplayName.
		v.srv.boatsMu.Lock()
		tag := v.boat.custom.tag()
		v.srv.boatsMu.Unlock()
		return entityDisplayName(v.srv.scoreboard, v.boat.kind.Name(), v.boat.uuid, customNameText(tag))
	}
	if v.cart != nil {
		// vanilla: AbstractMinecartContainer titles the menu with getDisplayName.
		v.srv.minecartsMu.Lock()
		stored := v.cart.custom
		v.srv.minecartsMu.Unlock()
		return entityDisplayName(v.srv.scoreboard, v.cart.kind.Name(), v.cart.uuid, customNameText(stored.tag()))
	}
	if v.kind == windowMerchant && v.w != nil && v.w.trader != nil {
		return v.w.trader.title
	}
	if (v.kind == windowBeacon || v.kind == windowEnchant) && v.w != nil {
		if be := v.w.w.BlockEntity(v.w.x, v.w.y, v.w.z); be != nil {
			var name *protocol.Text
			be.Use(func(b *world.BlockEntity) { name = b.CustomName })
			if name != nil {
				return *name
			}
		}
	}
	return protocol.Text{Translate: v.layout().Title}
}

// slots is how many slot numbers this window addresses.
func (v windowView) slots() int32 { return v.clickView(nil).Slots() }

// stack returns a window slot's current stack as package inventory reads
// it: player slots from the inventory, container slots from the block
// entity, the crafting result from the grid.
func (v windowView) stack(inv *player.Inventory, slot int32) protocol.ItemStack {
	return v.clickView(inv).Stack(inv, slot)
}

// clickView is the open window in package inventory's terms: kind, packet
// ID and the stores of its slots. It takes no locks; callers hold them. inv
// is for the ender chest, whose slots are the player's; it may be nil for
// calls that only need the window's shape.
func (v windowView) clickView(inv *player.Inventory) inventory.View {
	view := inventory.View{Kind: v.kind, ID: v.id}
	if v.w != nil {
		view = v.w.View(v.kind, v.id)
	}
	if v.menu != nil {
		switch v.kind {
		case windowMenu:
			view.Rows = int32(len(v.menu.slots) / 9)
		case windowLectern:
			view.Menu = v.menu.slots[:1]
			return view
		case windowCrafter:
			view.Grid = v.menu.slots[:inventory.CraftingGridCells]
		}
		view.Entity = menuSlots{v.menu}
		return view
	}
	switch v.kind {
	case windowLectern:
		// The book is the window's one slot, read through the block entity.
		if inv != nil && v.be != nil && len(v.be.Slots) > 0 {
			view.Menu = []protocol.ItemStack{v.be.Slots[0]}
		}
	case windowCrafter:
		// The crafter's 3x3 is the block entity's first nine slots, its result
		// computed like a crafting grid's. vanilla: CrafterMenu
		if v.be != nil {
			view.Entity = containerSlots(v.be, v.w.w)
			if grid := v.be.Slots; len(grid) >= inventory.CraftingGridCells {
				view.Grid = grid[:inventory.CraftingGridCells]
			}
		}
	case windowChest, windowFurnace, windowBlastFurnace, windowSmoker,
		windowBrewingStand, windowBarrel, windowShulker,
		windowDispenser, windowDropper, windowHopper:
		if v.be != nil {
			view.Entity = containerSlots(v.be, v.w.w)
		} else if v.boat != nil {
			view.Entity = boatCargo{it: v.boat}
		} else if v.cart != nil {
			view.Entity = cartCargo{it: v.cart}
		}
	case windowDouble:
		if v.be != nil && v.be2 != nil {
			view.Entity = inventory.Compound{First: containerSlots(v.be, v.w.w), Second: containerSlots(v.be2, v.w.w)}
		}
	case windowEnderChest:
		if inv != nil {
			view.Entity = enderSlots{inv: inv}
		}
	case windowHorse:
		if v.mount != nil {
			view.Columns = v.w.columns
			view.Entity = mountSlots{ent: v.mount}
		}
	}
	return view
}

// enderSlots is the player's ender chest slots as a window Container. An
// ender chest has no block entity (its contents are the player's, in their
// inventory and file), so this stands in for the block entity adapter,
// under the inventory lock clicks already hold.
type enderSlots struct {
	inv *player.Inventory
}

// Slot is the ender slot's stack, empty beyond them.
func (e enderSlots) Slot(slot int) protocol.ItemStack {
	if slot >= 0 && slot < player.EnderSlots {
		return e.inv.Ender[slot]
	}
	return protocol.ItemStack{}
}

// Put replaces the ender slot's stack, dropping any compound the file left,
// as every store's writes do.
func (e enderSlots) Put(slot int, stack protocol.ItemStack) {
	if slot >= 0 && slot < player.EnderSlots {
		e.inv.Ender[slot] = stack
		delete(e.inv.EnderStored, slot)
	}
}

// boatCargo is a chest boat's slots as a window Container, read and written
// directly; the caller holds the boat lock.
type boatCargo struct {
	it *boatEntity
}

// Slot is the cargo slot's stack; a hull with no cargo (broken) reads empty.
func (b boatCargo) Slot(slot int) protocol.ItemStack {
	if slot < 0 || slot >= len(b.it.cargo) {
		return protocol.ItemStack{}
	}
	return b.it.cargo[slot]
}

// Put replaces the cargo slot's stack, a no-op once the hull has none.
func (b boatCargo) Put(slot int, stack protocol.ItemStack) {
	if slot < 0 || slot >= len(b.it.cargo) {
		return
	}
	b.it.cargo[slot] = stack
	b.it.cargoVersion++
}

// Peek reads the cargo slot without copying its components.
func (b boatCargo) Peek(slot int) (itemID, count int32) {
	if slot < 0 || slot >= len(b.it.cargo) {
		return 0, 0
	}
	return b.it.cargo[slot].ItemID, b.it.cargo[slot].Count
}

// StackPtr is the cargo slot's stack in place for reading.
func (b boatCargo) StackPtr(slot int) *protocol.ItemStack {
	if slot < 0 || slot >= len(b.it.cargo) {
		return nil
	}
	return &b.it.cargo[slot]
}

// Filled is the cargo's occupied-slot mask.
func (b boatCargo) Filled() uint64 { return filledOf(b.it.cargo) }

// Stacks is the cargo, read while the boat lock is held.
func (b boatCargo) Stacks() []protocol.ItemStack { return b.it.cargo }

// Len is the cargo's slot count.
func (b boatCargo) Len() int { return len(b.it.cargo) }

// Kind gives chest boats the chest's unrestricted transfer rules.
func (b boatCargo) Kind() windowKind { return windowChest }

// cartCargo is a container cart's slots as a window Container; the caller
// holds the cart lock via windowView.use.
type cartCargo struct {
	it *minecartEntity
}

// Slot is what the given one of the cart's slots holds.
func (b cartCargo) Slot(slot int) protocol.ItemStack {
	if slot < 0 || slot >= len(b.it.cargo.slots) {
		return protocol.ItemStack{}
	}
	return b.it.cargo.slots[slot]
}

// Put replaces the cart slot's stack, a no-op once the cart has none.
func (b cartCargo) Put(slot int, stack protocol.ItemStack) {
	if slot < 0 || slot >= len(b.it.cargo.slots) {
		return
	}
	b.it.cargo.slots[slot] = stack
	b.it.cargo.version++
}

// blockEntitySlots is a container block entity as package inventory's
// Container. The entity's lock stays the caller's (click paths hold its
// Use). The world lets a write mark the chunk dirty so changes reach the
// region file. vanilla: BlockEntity.setChanged
type blockEntitySlots struct {
	be   *world.BlockEntity
	w    *world.World
	kind windowKind
}

// containerSlots adapts a container block entity; a nil world marks nothing
// dirty. A non-nil world also rolls a pending loot table, so a caller
// outside the entity's lock must already have cleared it (an open does,
// before any click).
func containerSlots(be *world.BlockEntity, w *world.World) blockEntitySlots {
	kind, _ := containerWindow(be.Kind)
	if w != nil {
		w.UnpackLoot(be)
	}
	return blockEntitySlots{be: be, w: w, kind: kind}
}

// Slot is the container slot's stack, empty for one it lacks.
func (b blockEntitySlots) Slot(slot int) protocol.ItemStack {
	if slot >= 0 && slot < b.be.Len() {
		return b.be.Slots[slot]
	}
	return protocol.ItemStack{}
}

// Filled is the entity's filled-slot mask.
func (b blockEntitySlots) Filled() uint64 { return b.be.FilledSlots() }

// Peek is the slot's item and count, read in place.
func (b blockEntitySlots) Peek(slot int) (itemID, count int32) {
	if slot >= 0 && slot < len(b.be.Slots) {
		s := &b.be.Slots[slot]
		return s.ItemID, s.Count
	}
	return 0, 0
}

// StackPtr is the slot's stack in place for reading, nil for one it lacks.
func (b blockEntitySlots) StackPtr(slot int) *protocol.ItemStack {
	if slot >= 0 && slot < len(b.be.Slots) {
		return &b.be.Slots[slot]
	}
	return nil
}

// Put replaces the container slot's stack, dropping any compound the file
// left, and marks the chunk dirty.
func (b blockEntitySlots) Put(slot int, stack protocol.ItemStack) {
	b.be.Put(slot, stack)
	if b.w != nil {
		b.w.Touch(b.be.X, b.be.Y, b.be.Z)
	}
}

// Stacks is the container's slots, read in place.
func (b blockEntitySlots) Stacks() []protocol.ItemStack { return b.be.Slots }

// Stored reports whether the slot holds a compound kept whole in place of
// an item, which reads empty through Slot but still occupies it. Call under
// the entity's lock, like Slot.
func (b blockEntitySlots) Stored(slot int) bool {
	_, ok := b.be.Stored[slot]
	return ok
}

// Len is how many slots the container has.
func (b blockEntitySlots) Len() int { return b.be.Len() }

// Kind is the window the container opens, which picks its move rules.
func (b blockEntitySlots) Kind() windowKind { return b.kind }

// SlotDisabled reports whether a crafter disabled the slot; other
// containers have none.
func (b blockEntitySlots) SlotDisabled(slot int) bool { return b.be.SlotDisabled(slot) }

// containerWindow is the window a block entity's slots open in, false for
// none. The jukebox, decorated pot, shelf and chiseled bookshelf kinds only
// name move rules; nothing opens them.
func containerWindow(entityKind world.BlockEntityKind) (windowKind, bool) {
	switch entityKind {
	case world.KindChest, world.KindTrappedChest:
		return windowChest, true
	case world.KindFurnace:
		return windowFurnace, true
	case world.KindBlastFurnace:
		return windowBlastFurnace, true
	case world.KindSmoker:
		return windowSmoker, true
	case world.KindBrewingStand:
		return windowBrewingStand, true
	case world.KindCrafter:
		return windowCrafter, true
	case world.KindBarrel:
		return windowBarrel, true
	case world.KindShulkerBox:
		return windowShulker, true
	case world.KindDispenser:
		return windowDispenser, true
	case world.KindDropper:
		return windowDropper, true
	case world.KindHopper:
		return windowHopper, true
	case world.KindJukebox:
		return windowJukebox, true
	case world.KindDecoratedPot:
		return windowDecoratedPot, true
	case world.KindShelf:
		return windowShelf, true
	case world.KindChiseledBookshelf:
		return windowBookshelf, true
	}
	return windowPlayer, false
}

// Packets. Each carries a fresh state ID the next click echoes back, which
// is what makes the echo worth checking.

// sendContainerSlot corrects one window slot. The inventory lock must be
// held: the packet takes a state ID from it (see
// player.Inventory.WindowSlot).
func (c *client) sendContainerSlot(inv *player.Inventory, view windowView, slot int32, stack protocol.ItemStack) error {
	view.w.setSent(slot, stack)
	payload := inv.WindowSlot(view.id, slot, stack)
	return c.writePacket(protocol.IDPlayContainerSlot, "set container slot", payload)
}

// sendCursorItem tells the player what the mouse is holding now.
func (c *client) sendCursorItem(stack protocol.ItemStack) error {
	var b protocol.Buffer
	b.PutSlot(stack)
	return c.writePacket(protocol.IDPlaySetCursorItem, "set cursor item", b.Bytes())
}

// openScreen sends Open Screen: the window ID later packets use, the menu
// type from the client registry, and the title (custom name, else the
// translated default).
func (c *client) openScreen(view windowView) error {
	if view.kind == windowHorse {
		return c.write(protocol.IDPlayMountScreenOpen, "mount screen open", func(b *protocol.Buffer) {
			b.PutVarInt(view.id)
			b.PutVarInt(view.w.columns)
			b.PutI32(view.mount.mob.ID)
		})
	}
	layout := view.layout()
	if view.menu != nil && view.kind == windowMenu {
		layout.Menu = int32(len(view.menu.slots)/9 - 1) // generic_9x1 to generic_9x6
	}
	return c.write(protocol.IDPlayOpenScreen, "open screen", func(b *protocol.Buffer) {
		b.PutVarInt(view.id)
		b.PutVarInt(layout.Menu)
		b.PutText(view.title())
	})
}

// sendContainerContent sends the whole open window, every slot plus cursor,
// as on open and as the answer to a stale click. The inventory lock must be
// held.
func (c *client) sendContainerContent(view windowView, inv *player.Inventory) error {
	stacks := make([]protocol.ItemStack, 0, view.slots())
	for slot := int32(0); slot < view.slots(); slot++ {
		stacks = append(stacks, view.stack(inv, slot))
	}
	if view.w != nil {
		view.w.Sent = slices.Clone(stacks)
	}
	payload := inv.WindowContent(view.id, stacks, c.container.click.Carried)
	return c.writePacket(protocol.IDPlayContainerContent, "set container content", payload)
}

// resyncWindow sends the whole open window (state, cursor, data slots) when
// a click is not answered with a correction: a stale state, or a click
// refused before it ran.
func (c *client) resyncWindow(view windowView) error {
	var err error
	view.inventory(c, func(inv *player.Inventory) {
		view.use(func() {
			if err = c.sendContainerContent(view, inv); err != nil {
				return
			}
			err = c.sendWindowData(view)
		})
	})
	return err
}

// sendWindowData sends every data slot the window draws with, as on first
// fill (furnace fire and progress, anvil cost, enchanting offers, beacon
// level), which a full resync owes. The caller holds the inventory lock and
// any block entity lock.
// vanilla: AbstractContainerMenu.sendAllDataToRemote
func (c *client) sendWindowData(view windowView) error {
	switch {
	case view.kind.Cooking():
		if view.be != nil {
			return c.sendFurnaceProgress(view, view.be)
		}
	case view.kind == windowBrewingStand:
		if view.be != nil {
			return c.sendBrewingProgress(view, view.be)
		}
	case view.kind == windowCrafter:
		if view.be != nil {
			return c.sendCrafterProperties(view, view.be)
		}
	case view.kind == windowEnchant:
		if view.w != nil {
			return c.sendEnchantProperties(view, view.w.Props)
		}
	case view.kind == windowBeacon:
		if view.be != nil {
			return c.sendBeaconProperties(view,
				rules.BeaconProps(view.be.BeaconLevels, view.be.BeaconPrimary, view.be.BeaconSecondary), true)
		}
	case view.kind == windowLectern:
		if view.be != nil {
			return c.sendContainerProperty(view, lecternPageProperty, int32(view.be.Page))
		}
	case view.kind == windowAnvil:
		if view.w != nil {
			view.w.SentCost = view.w.Cost
			return c.sendContainerProperty(view, AnvilCostProperty, view.w.Cost)
		}
	case view.kind == windowLoom:
		if view.w != nil {
			view.w.SentLoomSelected = view.w.LoomSelected
			return c.sendContainerProperty(view, LoomSelectedProperty, view.w.LoomSelected)
		}
	case view.kind == windowStonecutter:
		if view.w != nil {
			view.w.SentRecipe = view.w.Recipe
			return c.sendContainerProperty(view, StonecutterSelectedProperty, view.w.Recipe)
		}
	}
	return nil
}

// sendClickAnswer answers a click: the whole window for a stale state,
// otherwise the changed slots and cursor. Corrections are only built and
// sorted when sent.
func (c *client) sendClickAnswer(view windowView, slots map[int32]protocol.ItemStack, carried protocol.ItemStack, fullResync bool) error {
	if fullResync {
		return c.resyncWindow(view)
	}
	ordered := make([]int32, 0, len(slots))
	for slot := range slots {
		ordered = append(ordered, slot)
	}
	slices.Sort(ordered)
	var sendErr error
	c.player.Inventory(func(inv *player.Inventory) {
		for _, slot := range ordered {
			if err := c.sendContainerSlot(inv, view, slot, slots[slot]); err != nil {
				sendErr = err
				return
			}
		}
	})
	if sendErr != nil {
		return sendErr
	}
	return c.sendCursorItem(carried)
}

// sendContainerChanges tells this player the window slots changed since
// last told, in their window's numbering, one state ID per packet. slots
// were read by the caller before taking any lock.
// vanilla: AbstractContainerMenu.broadcastChanges
func (c *client) sendContainerChanges(window *openWindow, base int32, slots []protocol.ItemStack) {
	c.player.Inventory(func(inv *player.Inventory) {
		// A close under this lock already dropped the window; nothing to correct.
		if c.openContainerWindow() != window {
			return
		}
		for i, stack := range slots {
			slot := base + int32(i)
			if slot < 0 || int(slot) >= len(window.Sent) || window.Sent[slot].Equal(stack) {
				continue
			}
			window.setSent(slot, stack)
			c.send(protocol.IDPlayContainerSlot, inv.WindowSlot(window.id, slot, stack))
		}
	})
}

// containerPropertyPayload builds Set Container Property.
func containerPropertyPayload(windowID, property, value int32) []byte {
	var b protocol.Buffer
	b.PutVarInt(windowID)
	b.PutI16(int16(property))
	b.PutI16(int16(value))
	return b.Bytes()
}

// sendContainerProperty sets one window number (a furnace's fire or
// progress).
func (c *client) sendContainerProperty(view windowView, property, value int32) error {
	return c.writePacket(protocol.IDPlayContainerProperty, "set container property",
		containerPropertyPayload(view.id, property, value))
}

// queueContainerSlot corrects one slot from outside this connection's
// goroutine (the furnace tick), via the mailbox rather than writing under
// the connection. It builds under the inventory lock like every window
// packet, which also assigns its state ID.
func (c *client) queueContainerSlot(windowID, slot int32, stack protocol.ItemStack) {
	c.player.Inventory(func(inv *player.Inventory) {
		if window := c.openContainerWindow(); window != nil && window.id == windowID {
			window.setSent(slot, stack)
		}
		c.send(protocol.IDPlayContainerSlot, inv.WindowSlot(windowID, slot, stack))
	})
}

// queueContainerProperty is the mailbox way of sending one window property.
func (c *client) queueContainerProperty(windowID, property, value int32) {
	c.send(protocol.IDPlayContainerProperty, containerPropertyPayload(windowID, property, value))
}

// Clicks. Click Container is the client's account of a click (slot,
// button, mode) plus its predicted changed slots and cursor; the server runs
// the click against its own state and corrects disagreements.

// handleBundleItemSelected reads Bundle Item Selected: a bundle's window
// slot and the index scrolled to. The selection is click state only (not
// on the wire stack), kept in the click session until the next window
// change; an out-of-window slot is ignored.
// vanilla: ServerGamePacketListenerImpl.handleBundleItemSelectedPacket
func (c *client) handleBundleItemSelected(r *protocol.Reader) error {
	slot := r.ReadVarInt()
	index := r.ReadVarInt()
	if err := r.Err(); err != nil {
		return c.decodeFailure("bundle_item_selected", err)
	}
	view := c.currentView()
	if slot < 0 || slot >= view.slots() {
		return nil
	}
	view.inventory(c, func(inv *player.Inventory) {
		entries := int32(len(view.stack(inv, slot).BundleContents()))
		c.container.click.SelectBundle(slot, index, entries)
	})
	return nil
}

// armorSlots is how many armour slots follow player.ArmorSlot in the window.
const armorSlots = 4

// handleClickContainer runs the client's click in its open window and
// answers with the authoritative changes. The click runs under the
// inventory lock and, for a container, the block entity's; packets go out
// after, so no thrown stack spawns and no equipment broadcast is delivered
// while a lock is held.
func (c *client) handleClickContainer(r *protocol.Reader) error {
	windowID := r.ReadVarInt()
	clientState := r.ReadVarInt()
	slot := r.ReadI16()
	button := r.ReadI8()
	mode := r.ReadVarInt()
	claimCount := r.ReadVarInt()
	if claimCount < 0 || claimCount > protocol.ClickClaimLimit {
		// vanilla's changed-slots codec is bounded at 128, failing the decode past
		// it.
		return c.decodeFailure("container_click",
			fmt.Errorf("click container claims %d changed slots, at most %d", claimCount, protocol.ClickClaimLimit))
	}
	claims := make([]inventory.SlotClaim, 0, claimCount)
	for range claimCount {
		claimed := int32(r.ReadI16())
		claim, comparable := r.ReadHashedSlot()
		claims = append(claims, inventory.SlotClaim{Slot: claimed, Claim: claim, Comparable: comparable})
	}
	_, _ = r.ReadHashedSlot() // the cursor, which the server is the authority on
	if err := r.Err(); err != nil {
		return c.decodeFailure("container_click", err)
	}

	view := c.currentView()
	if windowID != view.id {
		// A click on a window this player does not have open (closed, or the
		// player inventory while a container is open); vanilla drops these too.
		c.log.Debug("ignoring click on a closed window", "window", windowID)
		return nil
	}

	// A spectator or a body on its death screen runs none of the click; the
	// whole window is resent, as for a stale state.
	// vanilla: ServerGamePacketListenerImpl.handleContainerClick
	if c.player.GameMode() == protocol.GameModeSpectator || c.player.Vitals().Dead {
		c.log.Debug("refusing a click from a spectator or a dead player", "window", windowID)
		return c.resyncWindow(view)
	}

	// The state check runs under the inventory lock, where the state lives. A
	// stale client's click still runs against the server's slots; only the
	// answer becomes the whole window.
	// vanilla: ServerGamePacketListenerImpl.handleContainerClick
	var fullResync bool
	c.player.Inventory(func(inv *player.Inventory) {
		if clientState != inv.StateID(windowID) {
			fullResync = true
		}
	})

	if seam := c.srv.clickSeam; seam != nil {
		seam(view.be)
	}

	// A plugin sees the click before it runs; a cancelled one is answered with
	// the whole window and the cursor, as a refused one is.
	// vanilla: AbstractContainerMenu.sendAllDataToRemote
	if mode != protocol.ClickModeDrag && (c.pluginClick(view, int32(slot), button, mode) || c.pluginTrade(view, int32(slot), mode)) {
		return c.resyncWindow(view)
	}

	// Mode 5 is the paint drag, handled below; every other click ends any drag
	// in progress inside package inventory, since paint packets come as start,
	// marks, end with nothing between.
	if mode == protocol.ClickModeDrag {
		return c.handleDrag(view, int32(slot), int32(button), fullResync)
	}

	// Lock order: the inventory lock, then the block entity's; the block entity
	// lock is never held across anything taking either, so the furnace tick
	// cannot deadlock with a click.
	//
	// The game mode is read before locking: the inventory callback runs under
	// the player's lock, so reading the player inside (as the clone click does)
	// would deadlock.
	creative := c.player.GameMode() == protocol.GameModeCreative
	xpLevel := int32(c.player.Vitals().ExperienceLevel)
	outcome := inventory.Outcome{}
	var known, refused, resultValid bool
	var armorBefore [armorSlots]protocol.ItemStack
	var armorAfter [armorSlots]protocol.ItemStack
	view.inventory(c, func(inv *player.Inventory) {
		copy(armorBefore[:], inv.Slots[player.ArmorSlot:])
		defer func() { copy(armorAfter[:], inv.Slots[player.ArmorSlot:]) }()
		// The window can close and the entity vanish under the inventory lock
		// (shutdown, a disconnect's save racing this handler), so the lookups are
		// redone here.
		refused = !view.useLive(func() {
			known = c.container.click.Click(view.clickView(inv), inv,
				mode, int32(button), int32(slot), creative, xpLevel, &outcome)
		})
		if refused {
			return
		}
		outcome.Carried = c.container.click.Carried
		// The inputs changed, so the outputs are recomputed, as vanilla menus do on
		// any container change; enchanting offers are remade under the click's lock
		// before a resync reads them.
		if view.w != nil && view.w.SlotsChanged(view.kind, inv, &outcome, creative) {
			c.recomputeEnchantOffers(view)
		}
		if outcome.MerchantUpdated && view.w != nil {
			resultValid = !view.w.Menu[inventory.MerchantResultSlot].Empty()
		}
	})
	if refused {
		// The container left the world between the lookup and the lock, so the
		// click must not touch slots a blast is about to spill; the menu closes, as
		// vanilla's failed stillValid does. vanilla: ServerPlayer.doTick
		return c.closeWindowForPlugins()
	}
	// A listener's crafting or anvil result goes out with the click's answer.
	outcome.Announce = append(outcome.Announce, c.pluginPrepare(view, outcome.Announce)...)
	for i := range armorSlots {
		c.armorChanged(player.ArmorSlot+i, armorBefore[i], armorAfter[i])
	}
	for _, sound := range outcome.BundleSounds {
		volume, pitch := float32(1), float32(1)
		if sound != registry.SoundItemBundleInsertFail {
			volume, pitch = 0.8, float32(0.8+c.srv.soundRng.Float64()*0.4)
		}
		pos := c.player.Position()
		c.srv.broadcastSoundIDExcept(c.theWorld(), c, sound, protocol.SoundCategoryPlayer,
			pos.X, pos.Y, pos.Z, volume, pitch)
	}
	if !known {
		// No window handles this mode; the click is dropped but the cursor is still
		// restated below.
		c.log.Debug("ignoring click mode", "mode", mode)
	}

	// Corrections: the client's claimed slots are compared with the real ones,
	// and everything the click announced is sent regardless. A stale click gets
	// the whole window instead, so these are built only for a fresh one.
	var slots map[int32]protocol.ItemStack
	if !fullResync {
		view.inventory(c, func(inv *player.Inventory) {
			view.use(func() {
				slots = inventory.Corrections(claims, outcome.Announce, view.slots(),
					func(slot int32) protocol.ItemStack { return view.stack(inv, slot) })
			})
		})
	}

	if view.kind == windowAnvil && outcome.AnvilTaken {
		// vanilla's onTake plays the anvil's sounds and rolls its wear before the
		// corrections are broadcast; a creative take never wears it.
		if creative {
			if err := c.anvilEventAt(view, protocol.WorldEventAnvilUse); err != nil {
				return err
			}
		} else if err := c.damageAnvilAt(view); err != nil {
			return err
		}
	}
	// A table's take sound plays once per click however many takes, queued
	// after the click's direct writes (see tableTakeSound).
	var takeSound registry.Sound
	if outcome.StonecutterTaken {
		takeSound = registry.SoundUiStonecutterTakeResult
	}
	if outcome.LoomTaken {
		takeSound = registry.SoundUiLoomTakeResult
	}
	if outcome.SmithingTaken {
		// vanilla's onTake plays its use event before the corrections.
		if err := c.smithingTakeEvent(view); err != nil {
			return err
		}
	}
	if outcome.GrindstoneTaken && view.kind == windowGrindstone {
		// The take's level event plays at the grindstone, queued like the anvil's.
		if err := c.grindstoneEventAt(view); err != nil {
			return err
		}
	}
	if outcome.CartographyTaken {
		takeSound = registry.SoundUiCartographyTableTakeResult
	}

	if traded := outcome.MerchantTraded; len(traded) > 0 {
		// vanilla: MerchantResultSlot.onTake notifies each trade before the stat
		for range traded {
			c.villagerTradeAdvancements()
		}
		c.awardStat(player.Custom(registry.CustomStatTradedWithVillager), int32(len(traded)))
		if view.w != nil && view.w.trader != nil {
			trader := view.w.trader
			// vanilla: Slot.setByPlayer changes the result before onTake resets the voice timer.
			if trader.tradeUpdated != nil {
				trader.tradeUpdated(true)
			}
			var offers []inventory.Offer
			c.player.Inventory(func(*player.Inventory) {
				for _, o := range traded {
					trader.xp += o.XP
				}
				offers = slices.Clone(trader.offers)
			})
			if trader.persist != nil {
				trader.persist(offers, traded)
			}
		}
	}
	if outcome.MerchantUpdated && view.w != nil && view.w.trader != nil && view.w.trader.tradeUpdated != nil {
		view.w.trader.tradeUpdated(resultValid)
	}
	if len(outcome.Recipes) > 0 {
		c.recipeCraftedAdvancements(outcome.Recipes)
		// vanilla: RecipeCraftingHolder.awardUsedRecipes
		var names []string
		for _, craft := range outcome.Recipes {
			if _, ok := registry.RecipeDisplayIDOfName(craft.ID); ok && !slices.Contains(names, craft.ID) {
				names = append(names, craft.ID)
			}
		}
		if len(names) > 0 {
			c.GiveRecipes(names)
		}
	}
	if len(outcome.BrewedPotions) > 0 {
		c.brewedPotionAdvancements(outcome.BrewedPotions)
	}

	if fullResync {
		// The client missed an update, so its prediction concerns a window that no
		// longer exists. The click ran against the server's slots; the whole window
		// and cursor go out instead of an untrustworthy correction.
		c.log.Debug("resynchronizing a desynchronized window", "window", windowID,
			"clientState", clientState)
	}
	if err := c.sendClickAnswer(view, slots, outcome.Carried, fullResync); err != nil {
		return err
	}
	if outcome.Visible {
		c.broadcastEquipment()
	}
	for i, stack := range outcome.Dropped {
		if slices.Contains(outcome.Thrown, i) {
			c.throwFromHand(stack, nil)
		} else {
			c.throwItem(stack)
		}
	}
	for _, stack := range outcome.Crafted {
		// vanilla: ResultSlot.checkTakeAchievements, ItemStack.onCraftedBy
		c.awardItemStat(registry.StatTypeCrafted, stack.ItemID, stack.Count)
	}
	if view.kind == windowAnvil {
		// The anvil's result or inputs changed, so its cost goes out if it moved,
		// as vanilla broadcasts a data slot only when it differs.
		if err := c.sendAnvilCost(view); err != nil {
			return err
		}
		if outcome.AnvilTaken {
			// The take spends the cost from the player's levels (none in creative) and
			// updates the experience bar. The anvil's take response already went out
			// before the slots above, as vanilla's onTake precedes its broadcast.
			if !creative {
				c.player.UpdateVitals(func(v *player.Vitals) {
					rules.GiveExperienceLevels(v, -int(outcome.AnvilCost))
				})
				if err := c.sendExperience(); err != nil {
					return err
				}
			}
		}
	}
	if view.kind == windowEnchant &&
		(slices.Contains(outcome.Announce, 0) || slices.Contains(outcome.Announce, 1)) {
		// The item or lapis changed, so the offers are remade and changed
		// properties sent, as vanilla recomputes on any container change.
		if err := c.updateEnchantOffers(view); err != nil {
			return err
		}
	}
	if view.kind == windowLoom {
		// The selection moved with the inputs, or with a take that spent one.
		if err := c.sendLoomSelected(view); err != nil {
			return err
		}
	}
	if view.kind == windowStonecutter {
		// The selection changed with the input (its change, or a take spending its
		// last piece).
		if err := c.sendStonecutterSelected(view); err != nil {
			return err
		}
	}
	if view.kind == windowSmithing {
		// The error state changed with the inputs (their change, or a take).
		if err := c.sendSmithingError(view); err != nil {
			return err
		}
	}

	// Changes to the container's own slots reach others at the same container:
	// slots are numbered the same in every window over it.
	for _, entitySlot := range outcome.EntitySlots {
		c.srv.broadcastEntitySlot(view, entitySlot, c)
	}
	// Taking a furnace result pays out its stored experience as orbs at the
	// taker, unlocks the recipes and then fires recipe_crafted for each. All
	// locks are released here: the payout reads the block entity under its own
	// lock and adds the orbs from there.
	// vanilla: AbstractFurnaceBlockEntity.awardUsedRecipesAndPopExperience
	if outcome.ResultTaken && view.kind.Cooking() && view.be != nil {
		pos := c.player.Position()
		if crafts := c.srv.payFurnaceSmelts(view.be, c.theWorld(), pos.X, pos.Y, pos.Z, c.pluginFurnaceExtract(view)); len(crafts) > 0 {
			names := make([]string, len(crafts))
			for i, craft := range crafts {
				names[i] = craft.ID
			}
			c.GiveRecipes(names)
			c.recipeCraftedAdvancements(crafts)
		}
	}
	if view.kind == windowGrindstone && outcome.GrindstoneTaken && view.w != nil && !view.w.virtual {
		// The take's award as orbs at the grindstone's centre: half the stripped
		// worth plus as many points as the level's roll gives below it.
		// vanilla: GrindstoneMenu$3.onTake, ExperienceOrb.award
		total := rules.GrindstoneAward(outcome.GrindstoneWorth, c.srv.levelRng.Float64())
		c.srv.spawnOrbs(view.w.w, float64(view.w.x)+0.5, float64(view.w.y)+0.5,
			float64(view.w.z)+0.5, total)
	}
	if takeSound != registry.SoundNone {
		c.tableTakeSound(view, takeSound)
	}
	return nil
}

// handleDrag is mode 5, the paint: a drag starts (slot -999, the button
// naming the mouse button), marks slots, and ends, when the held stack is
// divided. The packets between are bookkeeping; the stack moves at the end,
// per package inventory's record of the marks. fullResync answers like a
// stale click: the whole window.
func (c *client) handleDrag(view windowView, slot, button int32, fullResync bool) error {
	switch {
	case slot == -999 && (button == 0 || button == 4 || button == 8):
		// Start: 0 the left button, 4 the right, 8 the middle (creative).
		creative := c.player.GameMode() == protocol.GameModeCreative
		c.player.Inventory(func(inv *player.Inventory) {
			c.container.click.BeginDrag(button, creative)
		})
		if fullResync {
			return c.resyncWindow(view)
		}
		return nil

	case slot == -999 && (button == 2 || button == 6 || button == 10):
		// End: 2 the left button's, 6 the right button's, 10 the middle's.
		// A cancelled drag writes nothing and is answered with the whole window
		// and the cursor.
		if c.pluginDrag(view) {
			c.player.Inventory(func(*player.Inventory) { c.container.click.CancelDrag() })
			return c.resyncWindow(view)
		}
		outcome := inventory.Outcome{}
		// Read before the callback: both accessors take the player's lock, which
		// the callback holds, so reading inside would deadlock.
		creative := c.player.GameMode() == protocol.GameModeCreative
		xpLevel := int32(c.player.Vitals().ExperienceLevel)
		var slots map[int32]protocol.ItemStack
		if !fullResync {
			slots = make(map[int32]protocol.ItemStack)
		}
		var ended, refused bool
		var armorBefore, armorAfter [armorSlots]protocol.ItemStack
		view.inventory(c, func(inv *player.Inventory) {
			copy(armorBefore[:], inv.Slots[player.ArmorSlot:])
			defer func() { copy(armorAfter[:], inv.Slots[player.ArmorSlot:]) }()
			refused = !view.useLive(func() {
				ended = c.container.click.EndDrag(view.clickView(inv), inv, creative, xpLevel, &outcome)
			})
			if refused {
				return
			}
			outcome.Carried = c.container.click.Carried
			if ended {
				// A drag into the inputs recomputes their outputs, as a click would.
				if view.w != nil && view.w.SlotsChanged(view.kind, inv, &outcome, creative) {
					c.recomputeEnchantOffers(view)
				}
				if !fullResync {
					for _, announced := range outcome.Announce {
						if announced >= 0 && announced < view.slots() {
							slots[announced] = view.stack(inv, announced)
						}
					}
				}
			}
		})
		if refused {
			return c.closeWindowForPlugins()
		}
		if ended {
			for _, changed := range c.pluginPrepare(view, outcome.Announce) {
				if !fullResync {
					view.inventory(c, func(inv *player.Inventory) {
						view.use(func() { slots[changed] = view.stack(inv, changed) })
					})
				}
			}
		}
		for i := range armorSlots {
			c.armorChanged(player.ArmorSlot+i, armorBefore[i], armorAfter[i])
		}
		if !ended {
			// No drag was in progress, so nothing to correct; a stale state still gets
			// the whole window and cursor.
			if fullResync {
				return c.resyncWindow(view)
			}
			return nil
		}

		if err := c.sendClickAnswer(view, slots, outcome.Carried, fullResync); err != nil {
			return err
		}
		if view.kind == windowAnvil {
			// The drag's recompute may have moved the anvil cost.
			if err := c.sendAnvilCost(view); err != nil {
				return err
			}
		}
		if view.kind == windowLoom {
			if err := c.sendLoomSelected(view); err != nil {
				return err
			}
		}
		if view.kind == windowStonecutter {
			// The drag's input change moved the selection with it.
			if err := c.sendStonecutterSelected(view); err != nil {
				return err
			}
		}
		if view.kind == windowSmithing {
			// The drag's input change moved the error state with it.
			if err := c.sendSmithingError(view); err != nil {
				return err
			}
		}
		// From here as for a click: watched slots are broadcast, and container
		// slots reach others with it open.
		if outcome.Visible {
			c.broadcastEquipment()
		}
		if view.kind == windowEnchant &&
			(slices.Contains(outcome.Announce, 0) || slices.Contains(outcome.Announce, 1)) {
			// A drag onto the table remakes the offers, as a click would.
			if err := c.updateEnchantOffers(view); err != nil {
				return err
			}
		}
		for _, entitySlot := range outcome.EntitySlots {
			c.srv.broadcastEntitySlot(view, entitySlot, c)
		}
		return nil

	default:
		// A mark: button 1 is left, 5 right, 9 middle.
		if button == 1 || button == 5 || button == 9 {
			view.inventory(c, func(inv *player.Inventory) {
				c.container.click.MarkDrag(view.clickView(inv), inv, slot)
			})
		}
		if fullResync {
			return c.resyncWindow(view)
		}
		return nil
	}
}

// Opening and closing. A client opens a container by right-clicking its
// block (blocks.go) and closes it with Close Container, by walking away or
// by disconnecting; the server closes one (the block broken) with the
// clientbound Close Container.

// unpackLootFor rolls the pending table of each non-nil block entity for the
// client and fires player_generates_container_loot for each table the roll
// consumes; the opener is read before any block entity lock is taken.
// vanilla: RandomizableContainer.unpackLootTable
func (c *client) unpackLootFor(w *world.World, bes ...*world.BlockEntity) {
	fill := c.srv.lootFillFor(c.lootOpener())
	for _, be := range bes {
		if be == nil {
			continue
		}
		be.Use(func(b *world.BlockEntity) {
			table := b.LootTable
			w.UnpackLootWith(b, fill)
			if b.LootTable == "" {
				c.generatedLootAdvancements(table)
			}
		})
	}
}

// openContainer opens the container at the position: crafting table 3x3,
// chest 27, furnace three plus fire. Any open window closes first, as
// vanilla's opening another does.
func (c *client) openContainer(kind windowKind, x, y, z int32) error {
	// Whatever the player had open goes away first, its grid handed back.
	if err := c.closeCurrentWindow(false); err != nil {
		return err
	}

	// The window ID is a counter from 1 that wraps as vanilla's does, assigned
	// under the inventory lock with the rest of the session so a disconnect's
	// save never sees a half-open window.
	window := &openWindow{kind: kind, w: c.theWorld(), x: x, y: y, z: z}
	if kind == windowChest {
		// Either half opens the whole double chest. vanilla: ChestBlock.getContainer
		if first, second, ok := window.w.ChestPair(x, y, z); ok {
			window.kind = windowDouble
			window.x, window.y, window.z = first[0], first[1], first[2]
			window.second = second
		}
	}
	c.player.Inventory(func(*player.Inventory) {
		window.id = c.container.click.OpenWindow()
	})
	view := windowView{kind: window.kind, id: window.id, w: window}
	if window.kind.EntitySlots() {
		// The slots are in the block entity the placement created; one whose chunk
		// lost it gets it back, and one that never had one gets an empty one rather
		// than a window over nothing.
		halves := window.halves()
		view.be = ensureContainerEntity(window.w, halves[0])
		if view.be == nil {
			return nil
		}
		if len(halves) > 1 {
			if view.be2 = ensureContainerEntity(window.w, halves[1]); view.be2 == nil {
				return nil
			}
		}
	}
	if window.kind == windowBeacon {
		// A beacon's numbers are its block entity's, and no menu opens without one;
		// placement and setblock both create it.
		if ensureContainerEntity(window.w, [3]int32{window.x, window.y, window.z}) == nil {
			return nil
		}
	}
	// A lock the held item does not match refuses the open before anything is
	// sent (no lid, no viewer count), showing the overlay message and locked
	// sound instead. All container windows share the gate, a double chest
	// checking both halves; a spectator passes, the one main hand vanilla's
	// canUnlock reads.
	// vanilla: BaseContainerBlockEntity.createMenu, BeaconBlockEntity.createMenu,
	// LockCode.canUnlock
	if (window.kind.EntitySlots() || window.kind == windowBeacon) && c.player.GameMode() != protocol.GameModeSpectator {
		hand := c.heldItem(protocol.HandMain)
		locked := false
		gated := []*world.BlockEntity{view.be, view.be2}
		if window.kind == windowBeacon {
			gated = []*world.BlockEntity{window.w.BlockEntity(window.x, window.y, window.z)}
		}
		for _, be := range gated {
			if be == nil {
				continue
			}
			be.Use(func(b *world.BlockEntity) {
				if !b.UnlockedBy(hand) {
					locked = true
				}
			})
		}
		if locked {
			return c.containerLocked(window, view.title())
		}
	}
	if c.pluginOpen(view) {
		return nil
	}
	c.unpackLootFor(window.w, view.be, view.be2)
	c.setOpenWindow(window)

	// The lid sound goes out before Open Screen and the contents, so it is
	// written straight to this client. A spectator is not a viewer, so the lid
	// never creaks for one.
	if window.kind.HasLid() && c.player.GameMode() != protocol.GameModeSpectator {
		if err := c.containerOpenSound(window); err != nil {
			return err
		}
	}

	// The contents go out right after the window, so it opens filled; no Block
	// Entity Data, as items travel in Set Container Content.
	// vanilla: ServerPlayer.openMenu
	if err := c.openScreen(view); err != nil {
		return err
	}
	var err error
	view.inventory(c, func(inv *player.Inventory) {
		view.use(func() { err = c.sendContainerContent(view, inv) })
	})
	if err != nil {
		return err
	}
	if view.kind.Cooking() {
		// The furnace's fire and progress go with the content so the window opens
		// showing them.
		view.be.Use(func(b *world.BlockEntity) {
			err = c.sendFurnaceProgress(view, b)
		})
		if err != nil {
			return err
		}
	}
	if view.kind == windowBrewingStand {
		// The brew countdown and fuel go with the content so the window opens
		// showing them.
		view.be.Use(func(b *world.BlockEntity) {
			err = c.sendBrewingProgress(view, b)
		})
		if err != nil {
			return err
		}
	}
	if view.kind == windowCrafter {
		// The nine slot states and the triggered flag the window draws with.
		view.inventory(c, func(*player.Inventory) {
			view.be.Use(func(b *world.BlockEntity) {
				err = c.sendCrafterProperties(view, b)
			})
		})
		if err != nil {
			return err
		}
	}
	if view.kind == windowEnchant {
		// The enchanting window opens as vanilla starts it: no costs or hovers,
		// empty item slot, and the player's enchanting seed, from which the client
		// guesses its glyphs.
		start := inventory.EnchantStartProps(c.xpSeed.Load())
		view.w.Props = start
		if err := c.sendEnchantProperties(view, start); err != nil {
			return err
		}
	}
	if view.kind == windowBeacon {
		// The window opens showing the beacon's last level and its three choices.
		// Sent under the inventory lock, which guards sentBeacon against the tick's
		// level updates.
		c.player.Inventory(func(*player.Inventory) {
			err = c.sendBeaconProperties(view, beaconPropsAt(view), true)
		})
		if err != nil {
			return err
		}
	}
	if view.kind == windowLectern {
		// The window opens at the page the book is on.
		view.be.Use(func(b *world.BlockEntity) {
			err = c.sendContainerProperty(view, lecternPageProperty, int32(b.Page))
		})
		if err != nil {
			return err
		}
	}
	if view.kind == windowAnvil {
		// The anvil opens at cost 0: empty inputs, name field cleared with the
		// window.
		if err := c.sendContainerProperty(view, AnvilCostProperty, 0); err != nil {
			return err
		}
	}
	if view.kind == windowLoom {
		if err := c.sendContainerProperty(view, LoomSelectedProperty, 0); err != nil {
			return err
		}
	}
	if view.kind == windowStonecutter {
		// The window opens with selection 0, no recipe picked.
		if err := c.sendContainerProperty(view, StonecutterSelectedProperty, 0); err != nil {
			return err
		}
	}
	if view.kind == windowSmithing {
		// The smithing window opens with no error: empty inputs.
		if err := c.sendContainerProperty(view, SmithingRecipeErrorProperty, 0); err != nil {
			return err
		}
	}
	if view.kind.HasLid() && c.player.GameMode() != protocol.GameModeSpectator {
		// The first viewer opens the lid: block-action containers carry the viewer
		// count, and those whose vanilla counter rechecks itself book a recheck five
		// ticks out, so the lid follows the viewers while anyone looks in. A double
		// chest's halves count separately, right first. A spectator moves no lid.
		// vanilla: CompoundContainer.startOpen
		for _, h := range window.halves() {
			c.srv.containerLidOpened(window.w, h[0], h[1], h[2])
			c.containerEvent(rules.GameEventContainerOpen, window.w, h, 1)
		}
	}
	return nil
}

// openBoatCargo opens a chest boat's cargo in the chest block's 9x3 window,
// titled with the boat's display name, over the hull's slots. It closes once
// the boat is out of reach or gone; a passenger may open their own boat.
// vanilla: AbstractChestBoat.interact, openCustomInventoryScreen
func (c *client) openBoatCargo(it *boatEntity) error {
	// The loot table is rolled as the menu is built.
	// vanilla: AbstractChestBoat.createMenu
	opener := c.lootOpener()
	c.srv.boatsMu.Lock()
	table := c.srv.unpackBoatLoot(it, opener)
	c.srv.boatsMu.Unlock()
	c.generatedLootAdvancements(table)
	err := c.openVehicleCargo(&openWindow{kind: windowChest, w: it.w, boat: it})
	if err == nil {
		c.postVehicleContainerEvent(rules.GameEventContainerOpen, &openWindow{w: it.w, boat: it})
	}
	return c.angerPiglinsOnVehicleOpen(err)
}

// openCartCargo opens a container cart's contents in its kind's menu,
// titled with the cart's display name, over its slots; it closes once the
// cart is out of reach or gone. Only the chest cart angers piglins; the hopper
// cart does not.
// vanilla: AbstractMinecartContainer.createMenu, MinecartChest.interact
func (c *client) openCartCargo(it *minecartEntity) error {
	// The loot table is rolled as the menu is built.
	// vanilla: AbstractMinecartContainer.createMenu
	opener := c.lootOpener()
	c.srv.minecartsMu.Lock()
	table := c.srv.unpackVehicleLoot(it.w, it.cart.X, it.cart.Y, it.cart.Z, &it.cargo, opener)
	c.srv.minecartsMu.Unlock()
	c.generatedLootAdvancements(table)
	err := c.openVehicleCargo(&openWindow{kind: it.windowKind(), w: it.w, cart: it})
	if it.kind == registry.EntityTypeHopperMinecart {
		return err
	}
	if err == nil {
		c.postVehicleContainerEvent(rules.GameEventContainerOpen, &openWindow{w: it.w, cart: it})
	}
	return c.angerPiglinsOnVehicleOpen(err)
}

// postVehicleContainerEvent posts a container open or close at a chest boat's
// or chest cart's position; a spectator posts none, the hopper cart none.
// vanilla: AbstractChestBoat.stopOpen, MinecartChest.stopOpen
func (c *client) postVehicleContainerEvent(ev rules.GameEvent, window *openWindow) {
	if c.player.GameMode() == protocol.GameModeSpectator {
		return
	}
	var x, y, z float64
	switch {
	case window.boat != nil:
		c.srv.boatsMu.Lock()
		x, y, z = window.boat.boat.X, window.boat.boat.Y, window.boat.boat.Z
		c.srv.boatsMu.Unlock()
	case window.cart != nil && window.cart.kind != registry.EntityTypeHopperMinecart:
		c.srv.minecartsMu.Lock()
		x, y, z = window.cart.cart.X, window.cart.cart.Y, window.cart.cart.Z
		c.srv.minecartsMu.Unlock()
	default:
		return
	}
	c.srv.gameEvent(window.w, ev, x, y, z, c.gameEventContext(0))
}

// angerPiglinsOnVehicleOpen passes err through, angering the piglins in
// sight when the cargo opened.
// vanilla: ContainerEntity.interactWithContainerVehicle
func (c *client) angerPiglinsOnVehicleOpen(err error) error {
	if err == nil {
		c.angerNearbyPiglins(true)
	}
	return err
}

// angerPiglinsOnChestVehicleBreak angers the piglins in sight of a player
// whose melee hit destroyed a container vehicle, unless entity_drops is off.
// vanilla: ContainerEntity.chestVehicleDestroyed
func (c *client) angerPiglinsOnChestVehicleBreak(w *world.World) {
	if w.BoolRule(world.RuleEntityDrops) {
		c.angerNearbyPiglins(true)
	}
}

// openVehicleCargo installs a vehicle's menu after returning the old window's
// grid, then sends Open Screen before its contents. No cargo lock may be held:
// closing the old window can acquire another vehicle's lock.
func (c *client) openVehicleCargo(window *openWindow) error {
	if err := c.closeCurrentWindow(false); err != nil {
		return err
	}
	c.player.Inventory(func(*player.Inventory) {
		window.id = c.container.click.OpenWindow()
	})
	c.setOpenWindow(window)

	view := windowView{kind: window.kind, id: window.id, w: window, srv: c.srv, boat: window.boat, cart: window.cart, menu: window.menu, mount: window.mount}
	if err := c.openScreen(view); err != nil {
		return err
	}
	var err error
	view.inventory(c, func(inv *player.Inventory) {
		view.use(func() { err = c.sendContainerContent(view, inv) })
	})
	return err
}

// ensureContainerEntity is the container block's block entity, created
// empty if missing; nil if the block keeps no slots or its chunk is gone.
func ensureContainerEntity(w *world.World, pos [3]int32) *world.BlockEntity {
	kind, ok := containerEntityKindOf(registry.BlockOf(w.BlockAt(pos[0], pos[1], pos[2])))
	if !ok {
		return nil
	}
	return w.EnsureBlockEntity(pos[0], pos[1], pos[2], kind)
}

// sendFurnaceProgress sends the furnace window's four numbers: fuel burning
// and total, smelt progress and total. The block entity's lock must be held.
func (c *client) sendFurnaceProgress(view windowView, b *world.BlockEntity) error {
	properties := [][2]int32{
		{protocol.FurnacePropertyFuelLeft, b.LitRemaining},
		{protocol.FurnacePropertyFuelTotal, b.LitTotal},
		{protocol.FurnacePropertyProgress, b.CookingSpent},
		{protocol.FurnacePropertyProgressTotal, b.CookingTotal},
	}
	for _, p := range properties {
		if err := c.sendContainerProperty(view, p[0], p[1]); err != nil {
			return err
		}
	}
	return nil
}

// sendBrewingProgress sends a brewing stand window's arrow and fuel gauge,
// each with its full mark. The block entity's lock must be held.
func (c *client) sendBrewingProgress(view windowView, b *world.BlockEntity) error {
	properties := [][2]int32{
		{protocol.BrewingPropertyBrewTime, int32(b.BrewTime)},
		{protocol.BrewingPropertyFuel, int32(b.Fuel)},
		{protocol.BrewingPropertyBrewTotal, int32(b.BrewTotal)},
		{protocol.BrewingPropertyFuelTotal, int32(b.FuelTotal)},
	}
	for _, p := range properties {
		if err := c.sendContainerProperty(view, p[0], p[1]); err != nil {
			return err
		}
	}
	return nil
}

// sendCrafterProperties sends a crafter window's nine disabled-slot states
// and triggered flag. The caller holds the inventory lock and the block
// entity's lock.
func (c *client) sendCrafterProperties(view windowView, b *world.BlockEntity) error {
	for property, value := range crafterProperties(view.w, b) {
		view.w.SentCrafter[property] = value
		if err := c.sendContainerProperty(view, int32(property), value); err != nil {
			return err
		}
	}
	return nil
}

// crafterProperties reads a crafter window's ten numbers: the disabled slots
// from the block entity, whose lock the caller holds, and triggered from the
// block state, which the power reaction sets.
// vanilla: CrafterBlockEntity.containerData, CrafterBlock.setBlockEntityTriggered
func crafterProperties(window *openWindow, b *world.BlockEntity) [protocol.CrafterPropertyTriggered + 1]int32 {
	var props [protocol.CrafterPropertyTriggered + 1]int32
	for slot := range inventory.CraftingGridCells {
		if b.SlotDisabled(slot) {
			props[protocol.CrafterPropertySlots+slot] = 1
		}
	}
	if registry.StateBool(window.w.BlockAt(window.x, window.y, window.z), registry.PropTriggered) {
		props[protocol.CrafterPropertyTriggered] = 1
	}
	return props
}

// syncCrafterProperties tells the viewer each crafter number that changed
// since last told, as the menu's data slot broadcast does every tick.
// vanilla: AbstractContainerMenu.broadcastChanges
func (c *client) syncCrafterProperties(window *openWindow) {
	be := window.w.BlockEntity(window.x, window.y, window.z)
	if be == nil {
		return
	}
	var props [protocol.CrafterPropertyTriggered + 1]int32
	be.Use(func(b *world.BlockEntity) { props = crafterProperties(window, b) })
	c.player.Inventory(func(*player.Inventory) {
		if c.openContainerWindow() != window {
			return
		}
		for property, value := range props {
			if window.SentCrafter[property] == value {
				continue
			}
			window.SentCrafter[property] = value
			c.send(protocol.IDPlayContainerProperty, containerPropertyPayload(window.id, int32(property), value))
		}
	})
}

// AnvilCostProperty is the anvil window's one property, the level cost,
// from which "Too Expensive!" and the price tag are drawn.
const AnvilCostProperty = 0

// sendAnvilCost sends the anvil's cost when it changed, as vanilla's data
// slot broadcast does; the cost is the last recompute's, stored beside its
// result stack.
func (c *client) sendAnvilCost(view windowView) error {
	if view.kind != windowAnvil || view.w == nil {
		return nil
	}
	cost, moved := view.w.AnvilCostMoved()
	if !moved {
		return nil
	}
	return c.sendContainerProperty(view, AnvilCostProperty, cost)
}

// damageAnvilAt rolls an anvil's wear for a survival take (never creative):
// on a failed roll it becomes chipped or damaged in place, or breaks with
// the broken sound instead of the used one; otherwise just the used sound.
// The chip is a clients-only write waking no neighbours; the removal is a
// flag-3 write that does.
// vanilla: AnvilMenu.onTake, Level.removeBlock. The taker's world event goes
// out written straight to their connection, ahead of the slot updates; the
// block change reaches them with everyone's at the tick's flush.
func (c *client) damageAnvilAt(view windowView) error {
	win := view.w
	if win.virtual {
		return nil
	}
	if !rules.AnvilWears(c.srv.levelRng.Float64()) {
		return c.anvilEventAt(view, protocol.WorldEventAnvilUse)
	}
	next, gone := registry.DamageAnvilState(win.w.BlockAt(win.x, win.y, win.z))
	if gone {
		change := world.BlockChange{X: win.x, Y: win.y, Z: win.z, State: airBlockState}
		old := win.w.BlockAt(win.x, win.y, win.z)
		if win.w.SetBlock(win.x, win.y, win.z, airBlockState) {
			c.srv.tellBlockChanges(win.w, []world.BlockChange{change})
			c.srv.updateNeighborShapes(win.w, win.x, win.y, win.z)
			c.srv.blockChanged(win.w, win.x, win.y, win.z, old)
		}
		return c.anvilEventAt(view, protocol.WorldEventAnvilBroken)
	}
	if next != win.w.BlockAt(win.x, win.y, win.z) {
		c.srv.writeBlock(win.w, win.x, win.y, win.z, next, nil, writeNeighborShapes)
	}
	return c.anvilEventAt(view, protocol.WorldEventAnvilUse)
}

// anvilEventAt plays an anvil level event at the anvil, for the taker and
// everyone near, since the taker's client predicts none of it.
func (c *client) anvilEventAt(view windowView, event int32) error {
	if view.w.virtual {
		return nil
	}
	payload := worldEvent(event, view.w.x, view.w.y, view.w.z, 0)
	c.sendIn(view.w.w, protocol.IDPlayWorldEvent, payload)
	c.srv.broadcastNearExcept(view.w.w, c, float64(view.w.x), float64(view.w.y), float64(view.w.z), levelEventRange,
		protocol.IDPlayWorldEvent, payload)
	return nil
}

// handleRenameItem handles the anvil name field's per-keystroke packet. The
// window keeps the filtered name, as vanilla's menu stores it; a name it
// already has changes nothing (no recompute, no send), and one too long for
// the field is ignored.
func (c *client) handleRenameItem(r *protocol.Reader) error {
	name := r.ReadString(32767)
	if err := r.Err(); err != nil {
		return c.decodeFailure("rename_item", err)
	}
	view := c.currentView()
	if view.kind != windowAnvil || view.w == nil {
		// A rename heard outside an anvil's window is none.
		return nil
	}
	validated, ok := rules.AnvilValidateName(name)
	if !ok || validated == view.w.Rename {
		return nil
	}
	creative := c.player.GameMode() == protocol.GameModeCreative
	var payload []byte
	c.player.Inventory(func(inv *player.Inventory) {
		view.w.SetRename(validated, creative)
		payload = inv.WindowSlot(view.id, inventory.AnvilResultSlot, view.w.Menu[2])
	})
	if payload == nil {
		return nil
	}
	if c.pluginPrepare(view, []int32{inventory.AnvilResultSlot}) != nil {
		c.player.Inventory(func(inv *player.Inventory) {
			payload = inv.WindowSlot(view.id, inventory.AnvilResultSlot, view.w.Menu[2])
		})
	}
	if err := c.writePacket(protocol.IDPlayContainerSlot, "set container slot", payload); err != nil {
		return err
	}
	return c.sendAnvilCost(view)
}

// closeCurrentWindow closes the open container window, returning the
// cursor and the window's own stacks (crafting grid, enchanting item and
// lapis, anvil inputs) to the inventory, dropping what does not fit. A
// chest lid falls when its last viewer leaves; a furnace keeps working.
// tellClient is whether to tell the client, which already knows if it
// closed the window itself.
func (c *client) closeCurrentWindow(tellClient bool) error {
	return c.closeWindows(tellClient, false, false)
}

// closeWindowForPlugins closes the open window on the server's initiative,
// firing InventoryCloseEvent first, and tells the client.
func (c *client) closeWindowForPlugins() error {
	c.pluginClose()
	return c.closeCurrentWindow(true)
}

// closeWindows is closeCurrentWindow that with own also closes the player's
// window, returning its 2x2 grid first (vanilla's Player.remove runs
// InventoryMenu.removed before the container's). With drop, everything
// returned drops instead: the player is leaving.
func (c *client) closeWindows(tellClient, own, drop bool) error {
	window := c.openContainerWindow()
	if window == nil && !own {
		return nil
	}
	if window != nil && window.menu != nil && window.menu.mirror != nil {
		c.srv.syncMenuMirror(window.menu)
	}
	view := inventory.View{Kind: inventory.KindPlayer}
	if window != nil {
		view = inventory.View{Kind: window.kind, Grid: window.Grid[:], Menu: window.Menu[:]}
	}
	var back inventory.Handback
	var closeError error
	var trader *merchant
	defer func() {
		if trader != nil && trader.release != nil {
			trader.release()
		}
	}()
	c.player.Inventory(func(inv *player.Inventory) {
		if window != nil {
			// Closed under the inventory lock, so a click in flight finds no window.
			// Its state count goes with it, the player's own window keeps its own, and
			// the ID is never reused.
			c.setOpenWindow(nil)
			inv.ForgetState(window.id)
			trader = window.trader
		}
		c.container.click.ClearBundleSelection()
		back = c.container.click.Close(view, inv, own, drop)
		if window != nil && tellClient {
			var b protocol.Buffer
			b.PutVarInt(window.id)
			if err := c.writePacket(protocol.IDPlayCloseContainer, "close container", b.Bytes()); err != nil {
				closeError = err
				return
			}
		}
		// A container close corrects only the player slots the handback changed;
		// closing the player's own window resends its whole content.
		if !drop {
			if window == nil {
				if back.Changed() {
					closeError = c.sendWindowZero(inv)
				}
			} else {
				for _, slot := range back.Slots {
					if err := c.sendContainerSlot(inv, windowView{kind: windowPlayer}, int32(slot), inv.Slots[slot]); err != nil {
						closeError = err
						break
					}
				}
			}
		}
	})
	// No lock held: throws spawn item entities and broadcasts walk the player
	// list.
	for _, stack := range back.Dropped {
		c.throwItem(stack)
	}
	if back.Visible {
		c.broadcastEquipment()
	}
	if window != nil && window.kind.HasLid() && window.boat == nil && window.cart == nil &&
		c.player.GameMode() != protocol.GameModeSpectator {
		for _, h := range window.halves() {
			c.srv.containerLidClosed(window.w, h[0], h[1], h[2])
			c.containerEvent(rules.GameEventContainerClose, window.w, h, 0)
		}
	}
	if window != nil && (window.boat != nil || window.cart != nil) {
		c.postVehicleContainerEvent(rules.GameEventContainerClose, window)
	}
	return closeError
}

// sendWindowZero fills the player's own window, the one that never closes:
// everything carried, the 2x2 grid's result, an empty cursor. A join sends
// it, and closing the player's own window leaves it showing.
func (c *client) sendWindowZero(inv *player.Inventory) error {
	// The inventory's builder computes the 2x2 result (four of its slots); the
	// cursor is empty after the close above returned it.
	return c.writePacket(protocol.IDPlayContainerContent, "set container content", inv.ContainerContent())
}

// handleCloseContainer closes the open container or, with none, the
// player's own window, returning grid and cursor. The reported window ID is
// not checked; vanilla's doCloseContainer closes whatever is open.
func (c *client) handleCloseContainer(r *protocol.Reader) error {
	r.ReadVarInt() // the window ID
	if err := r.Err(); err != nil {
		return c.decodeFailure("container_close", err)
	}
	c.pluginClose()
	return c.closeWindows(false, c.openContainerWindow() == nil, false)
}

// handleContainerSlotStateChanged toggles a crafter grid slot: the block
// entity records the disabled slots and the window is told, so the client
// draws the locked cell.
// vanilla: ServerboundContainerSlotStateChangedPacket
func (c *client) handleContainerSlotStateChanged(r *protocol.Reader) error {
	slot := r.ReadVarInt()
	windowID := r.ReadVarInt()
	state := r.ReadBool()
	if r.Err() != nil {
		return nil
	}
	if c.player.GameMode() == protocol.GameModeSpectator {
		return nil
	}
	view := c.currentView()
	if view.kind != windowCrafter || view.id != windowID || view.be == nil ||
		slot < 0 || int(slot) >= inventory.CraftingGridCells {
		return nil
	}
	changed := false
	view.be.Use(func(b *world.BlockEntity) {
		if changed = b.SetCrafterSlotState(int(slot), state); changed {
			c.theWorld().Touch(b.X, b.Y, b.Z)
		}
	})
	if !changed {
		return nil
	}
	c.syncCrafterProperties(view.w)
	return nil
}

// tickOpenWindow closes the open window, returning its stacks, once its
// container is gone or out of reach; a crafter window still open is told its
// changed numbers.
// vanilla: ServerPlayer.tick, AbstractContainerMenu.stillValid
func (c *client) tickOpenWindow() {
	window := c.openContainerWindow()
	if window == nil {
		return
	}
	if window.menu != nil && window.menu.mirror != nil {
		c.srv.syncMenuMirror(window.menu)
	}
	if c.containerStillOpen(window) {
		if window.kind == windowCrafter {
			c.syncCrafterProperties(window)
		}
		return
	}
	if err := c.closeWindowForPlugins(); err != nil {
		c.log.Debug("closing a window whose container is gone failed", "error", err)
	}
}

// containerStillOpen reports whether the holder can still use the window's
// container: for a block window, the block still opens this kind with its
// block entity, in reach of the eyes; for a chest boat, the hull is still
// listed and in reach. Deliberate difference from vanilla: a block entity
// replaced by another of the same kind keeps the window open, as the window
// holds a position, not the entity.
// vanilla: AbstractContainerMenu.stillValid, Container.stillValidBlockEntity
func (c *client) containerStillOpen(window *openWindow) bool {
	if window.virtual {
		return true
	}
	if window.menu != nil {
		return window.menu.mirror == nil || window.menu.mirror.live()
	}
	if window.boat != nil {
		s := c.srv
		s.boatsMu.Lock()
		it := window.boat
		valid := !it.boat.Dead && slices.Contains(s.boats, it)
		minX, minY, minZ, maxX, maxY, maxZ := it.boat.Box()
		w := it.w
		s.boatsMu.Unlock()
		if !valid || w != c.theWorld() {
			return false
		}
		pos := c.player.Position()
		// vanilla: ContainerEntity.isChestVehicleStillValid
		return rules.WithinEntityInteractionRangeBuffered(c.entityRange(),
			rules.ContainerRangeExtra, pos.X,
			pos.Y+c.dims(c.player.Appearance().Pose).EyeHeight, pos.Z,
			minX, minY, minZ, maxX, maxY, maxZ)
	}
	if window.cart != nil {
		s := c.srv
		s.minecartsMu.Lock()
		it := window.cart
		valid := !it.cart.Dead && slices.Contains(s.minecarts, it)
		minX, minY, minZ, maxX, maxY, maxZ := it.cart.Box()
		w := it.w
		s.minecartsMu.Unlock()
		if !valid || w != c.theWorld() {
			return false
		}
		pos := c.player.Position()
		return rules.WithinEntityInteractionRangeBuffered(c.entityRange(),
			rules.ContainerRangeExtra, pos.X,
			pos.Y+c.dims(c.player.Appearance().Pose).EyeHeight, pos.Z,
			minX, minY, minZ, maxX, maxY, maxZ)
	}
	if window.mount != nil {
		return c.mountStillOpen(window)
	}
	if window.trader != nil {
		// A merchant stands in no block; its own check decides.
		if window.trader.stillValid == nil {
			return true
		}
		pos := c.player.Position()
		return window.trader.stillValid(c.theWorld(), c.entityRange(), pos.X,
			pos.Y+c.dims(c.player.Appearance().Pose).EyeHeight, pos.Z)
	}
	pos := c.player.Position()
	eye := c.dims(c.player.Appearance().Pose).EyeHeight
	rng := c.blockRange()
	block := registry.BlockOf(window.w.BlockAt(window.x, window.y, window.z))
	kind, opens := containerBlockKind(block)
	if !opens {
		return false
	}
	// A double chest window is anchored on its right half, whose block alone
	// opens a plain chest window.
	if window.kind == windowDouble && kind != windowChest {
		return false
	}
	if window.kind != windowDouble && kind != window.kind {
		return false
	}
	// A lectern whose book was taken has nothing to show.
	// vanilla: LecternBlockEntity.bookAccess.stillValid
	if window.kind == windowLectern && !lecternHasBookAt(window.w, window.x, window.y, window.z) {
		return false
	}
	switch window.kind {
	case windowChest, windowFurnace, windowBlastFurnace, windowSmoker,
		windowBrewingStand, windowCrafter, windowBarrel, windowShulker,
		windowDispenser, windowDropper, windowHopper, windowLectern:
		if window.w.BlockEntity(window.x, window.y, window.z) == nil {
			return false
		}
	case windowDouble:
		// Each half must still be the same paired chest with its block entity, in
		// reach. vanilla: CompoundContainer.stillValid
		sx, sy, sz := window.second[0], window.second[1], window.second[2]
		second := window.w.BlockAt(sx, sy, sz)
		if registry.BlockOf(second) != block || registry.StateValue(second, registry.PropType) == "single" ||
			window.w.BlockEntity(window.x, window.y, window.z) == nil ||
			window.w.BlockEntity(sx, sy, sz) == nil {
			return false
		}
		if !rules.WithinContainerRange(rng, pos.X, pos.Y+eye, pos.Z, float64(sx), float64(sy), float64(sz)) {
			return false
		}
	}
	return rules.WithinContainerRange(rng,
		pos.X, pos.Y+eye, pos.Z, float64(window.x), float64(window.y), float64(window.z))
}

// closeContainersAt closes, and tells, every player viewing the container
// at the position, which is gone (a player broke it). The caller empties the
// block entity onto the ground.
func (s *Server) closeContainersAt(w *world.World, x, y, z int32) {
	s.containerViewers(w, x, y, z, nil, func(c *client) {
		if err := c.closeWindowForPlugins(); err != nil {
			c.log.Debug("closing the window of a broken container failed", "error", err)
		}
	})
}

// containerViewers runs fn, outside the player list lock, on every player
// but except whose window shows the container at the position in w (the
// same position in another dimension is another chest; a double chest's
// window shows both halves).
func (s *Server) containerViewers(w *world.World, x, y, z int32, except *client, fn func(*client)) {
	if s.openContainerWindows.Load() == 0 {
		return
	}

	var buf [8]*client
	recipients := buf[:0]
	s.mu.RLock()
	for _, c := range s.clients {
		if c == except {
			continue
		}
		window := c.openContainerWindow()
		if window == nil || !window.covers(w, x, y, z) {
			continue
		}
		recipients = append(recipients, c)
	}
	s.mu.RUnlock()
	for _, c := range recipients {
		fn(c)
	}
}

// containerOpenerCount counts the non-spectators viewing the container.
// vanilla: ContainerOpenersCounter.hasContainerOpen
func (s *Server) containerOpenerCount(w *world.World, x, y, z int32) int {
	golems := s.golemOpen.at(w, x, y, z)
	if s.openContainerWindows.Load() == 0 {
		return golems
	}

	s.mu.RLock()
	clients := s.clientsLocked()
	s.mu.RUnlock()
	return golems + openersAmong(clients, w, x, y, z)
}

// openersAmong counts the non-spectators among clients whose window shows
// the container.
func openersAmong(clients []*client, w *world.World, x, y, z int32) int {
	openers := 0
	for _, c := range clients {
		if window := c.openContainerWindow(); window != nil && window.covers(w, x, y, z) &&
			c.player.GameMode() != protocol.GameModeSpectator {
			openers++
		}
	}
	return openers
}

// chestWatch answers a cat's chest seat from the tick's players, whose list
// is read before the mob lock: taking s.mu under mobsMu would invert the lock
// order.
type chestWatch struct {
	w       *world.World
	clients []*client
}

// open reports whether anyone has the chest at the cell open.
func (c *chestWatch) open(x, y, z int32) bool { return openersAmong(c.clients, c.w, x, y, z) > 0 }

// broadcastEntitySlot tells every other player at the container that one of
// its slots changed, in their window's numbering (a double chest's left
// half starts 27 in). The stack is read where it lives, not taken from the
// click, so the correction is current.
func (s *Server) broadcastEntitySlot(view windowView, entitySlot int32, except *client) {
	if view.menu != nil {
		s.syncMenuViewers(view.menu)
		return
	}
	if view.cart != nil {
		s.syncCartViewers(view.cart)
		return
	}
	if view.boat != nil {
		s.syncBoatViewers(view.boat)
		return
	}
	if view.mount != nil {
		s.broadcastMountSlot(view.mount, entitySlot, except)
		return
	}
	if view.w == nil || view.be == nil {
		return
	}
	be, local := view.be, entitySlot
	if view.be2 != nil && entitySlot >= doubleChestHalf {
		be, local = view.be2, entitySlot-doubleChestHalf
	}
	var stack protocol.ItemStack
	be.Use(func(b *world.BlockEntity) {
		if local >= 0 && local < int32(b.Len()) {
			stack = b.Slots[local]
		}
	})
	s.containerViewers(view.w.w, be.X, be.Y, be.Z, except, func(c *client) {
		window := c.openContainerWindow()
		if window == nil {
			return
		}
		c.queueContainerSlot(window.id, window.slotBase(be.X, be.Y, be.Z)+local, stack)
	})
}

// playerWindowID is the player's own window, 0, which the client always has
// open and accepts updates for at any time.
const playerWindowID = 0

// playerWindowSlotOf builds Set Container Slot for a player window slot,
// straight from the inventory, for paths outside the containers (a pickup,
// a placement's cost). It runs under the inventory lock like every window
// packet, carrying the inventory's state ID.
func playerWindowSlotOf(inv *player.Inventory, invSlot int) []byte {
	return inv.WindowSlot(playerWindowID, int32(invSlot), inv.Slots[invSlot])
}

// tableTakeSound plays a table's take sound at its block once per click
// however many takes. The taker's copy is queued in the table's world like
// everyone's, so it follows the click's direct writes; vanilla sends it
// before the slot updates, which would put the click's answers on the
// mailbox too.
// vanilla: StonecutterMenu$2.onTake, LoomMenu$4.onTake, CartographyTableMenu$5.onTake
func (c *client) tableTakeSound(view windowView, sound registry.Sound) {
	if view.w.virtual {
		return
	}
	x, y, z := float64(view.w.x)+0.5, float64(view.w.y)+0.5, float64(view.w.z)+0.5
	payload := soundEffectIDAt(sound, protocol.SoundCategoryBlock, x, y, z, 1.0, 1.0)
	c.srv.broadcastNearExcept(view.w.w, c, x, y, z, soundRange(1.0), protocol.IDPlaySoundEffect, payload)
	c.sendIn(view.w.w, protocol.IDPlaySoundEffect, payload)
}

// sendWindowSelection sends a selection property when it changed.
func (c *client) sendWindowSelection(view windowView, property int32, selected int32, moved bool) error {
	if !moved {
		return nil
	}
	return c.sendContainerProperty(view, property, selected)
}
