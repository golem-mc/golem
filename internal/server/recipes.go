package server

import (
	"fmt"
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/inventory"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// Recipe book packets: the join's, the handlers that change it, and unlock
// grants, which come from the recipe advancements' rewards. Tables are in
// registry/recipebook_gen.go (from the vanilla data pack); state, saving and
// grid placement are package inventory's (internal/server/inventory/book.go and
// recipebook.go).

// Slot display types from minecraft:slot_display, built into the client
// and not sent, so these numbers are version-pinned.
const (
	slotDisplayEmpty     = 0 // a square the recipe leaves out
	slotDisplayPotion    = 2
	slotDisplayComponent = 3
	slotDisplayDyed      = 7
	slotDisplayComposite = 10
	slotDisplayAnyFuel   = 1 // whatever burns
	slotDisplayItem      = 4 // one item, no count
	slotDisplayItemStack = 5 // item, count and component patch
	slotDisplayTrim      = 8 // trim demo: base, material and pattern
	slotDisplayTag       = 6 // a HolderSet of items: a tag or an inline list
)

// Recipe display types from minecraft:recipe_display, built in like the
// slot displays.
const (
	recipeDisplayShapeless   = 0
	recipeDisplayShaped      = 1
	recipeDisplayFurnace     = 2
	recipeDisplayStonecutter = 3
	recipeDisplaySmithing    = 4
)

// Recipe Book Add entry flags: the toast for a grant (per the recipe's
// show_notification) and the highlight for one not yet seen. The join's
// book has no toasts. vanilla: ClientboundRecipeBookAddPacket's Entry
const (
	recipeBookEntryNotify = 0x01
	recipeBookEntryNew    = 0x02
)

// recipeBookEntryReplace replaces the client's known recipes; vanilla sends
// true for the join's book, false for later grants.
const recipeBookEntryReplace = true

// recipeBookState is the player's book, or a fresh one before the join
// stores it.
func (c *client) recipeBookState() *inventory.RecipeBook {
	if book := c.recipeBook.Load(); book != nil {
		return book
	}
	return inventory.FreshRecipeBook()
}

// updateRecipeBook replaces the book with change's result, under the
// book's lock.
func (c *client) updateRecipeBook(change func(book *inventory.RecipeBook)) {
	c.bookMu.Lock()
	defer c.bookMu.Unlock()
	next := c.recipeBookState().Clone()
	change(next)
	c.recipeBook.Store(next)
}

// The join's packets.

// sendUpdateRecipes sends property sets and stonecutter recipes; crafting
// recipes go as Recipe Book Add.
func (c *client) sendUpdateRecipes() error {
	return c.writePacket(protocol.IDPlayUpdateRecipes, "update recipes", updateRecipesPayload())
}

func updateRecipesPayload() []byte {
	var b protocol.Buffer
	sets := registry.RecipePropertySetsNow()
	b.PutVarInt(int32(len(sets)))
	for _, set := range sets {
		b.PutString(set.Name)
		b.PutVarInt(int32(len(set.Items)))
		for _, id := range set.Items {
			b.PutVarInt(id)
		}
	}
	cuts := registry.StonecutterRecipesNow()
	b.PutVarInt(int32(len(cuts)))
	for _, recipe := range cuts {
		// ID Set type 2: an ad-hoc set of exactly one.
		b.PutVarInt(2)
		b.PutVarInt(recipe.Input)
		b.PutVarInt(slotDisplayItemStack)
		b.PutVarInt(recipe.ResultID)
		b.PutVarInt(recipe.ResultCount)
		b.PutVarInt(0) // no added components
		b.PutVarInt(0) // no removed components
	}
	return b.Bytes()
}

func (c *client) sendRecipeBookSettings() error {
	return c.writePacket(protocol.IDPlayRecipeBookSettings, "recipe book settings",
		recipeBookSettingsPayload(c.recipeBookState()))
}

// recipeBookSettingsPayload writes the four books' open and filter flags in
// client order.
// vanilla: ClientboundRecipeBookSettingsPacket, RecipeBookSettings's codec
func recipeBookSettingsPayload(book *inventory.RecipeBook) []byte {
	var b protocol.Buffer
	for i := range book.Settings.Open {
		b.PutBool(book.Settings.Open[i])
		b.PutBool(book.Settings.Filtering[i])
	}
	return b.Bytes()
}

// sendRecipeBookAdd sends the player's book. Deliberate difference from
// vanilla, which sends an empty book at join and grants fresh recipes via
// the recipe-unlock advancements the tick after: this join sends the fresh
// book whole. Later unlocks are the same either way (the recipe
// advancements' rewards).
func (c *client) sendRecipeBookAdd() error {
	return c.writePacket(protocol.IDPlayRecipeBookAdd, "recipe book add",
		c.recipeBookAddPayload())
}

// recipeBookAddPayload writes the book's entries: display, group and
// category, crafting requirements, and the highlight for never-seen
// entries.
// vanilla: ClientboundRecipeBookAddPacket over
// RecipeBookServer.sendInitialRecipeBook
func (c *client) recipeBookAddPayload() []byte {
	book := c.recipeBookState()
	var entries protocol.Buffer
	count := 0
	for _, name := range book.Known {
		// Unsupported displays stay in the file until the server can encode them.
		count += putNamedRecipeBookEntry(&entries, name, false, slices.Contains(book.ToBeDisplayed, name))
	}
	var b protocol.Buffer
	b.PutVarInt(int32(count))
	b.PutRaw(entries.Bytes())
	b.PutBool(recipeBookEntryReplace)
	return b.Bytes()
}

// putNamedRecipeBookEntry writes a recipe's displays and returns their count.
func putNamedRecipeBookEntry(b *protocol.Buffer, name string, notify, highlight bool) int {
	if furnace := registry.FurnaceDisplayOfName(name); furnace != nil {
		putFurnaceRecipeBookEntry(b, furnace, notify, highlight)
		return 1
	}
	if displays := registry.CraftingDisplaysOfName(name); len(displays) > 0 {
		for i := range displays {
			display := &displays[i]
			putRecipeBookEntry(b, display, notify && display.Notify, highlight)
		}
		return len(displays)
	}
	if d := registry.StonecutterDisplayOfName(name); d != nil {
		b.PutVarInt(d.ID)
		b.PutVarInt(recipeDisplayStonecutter)
		putSquareDisplay(b, d.Ingredient)
		putBookResult(b, d.ResultID, d.ResultCount)
		putBookStation(b, registry.ItemStonecutter)
		putEntryTail(b, d.Group, d.Category, []registry.BookIngredient{d.Ingredient}, notify, highlight)
		return 1
	}
	if d := registry.SmithingDisplayOfName(name); d != nil {
		b.PutVarInt(d.ID)
		b.PutVarInt(recipeDisplaySmithing)
		putSquareDisplay(b, d.Template)
		putSquareDisplay(b, d.Base)
		putSquareDisplay(b, d.Addition)
		// vanilla: SlotDisplay.SmithingTrimDemoSlotDisplay.STREAM_CODEC
		if d.Trim {
			b.PutVarInt(slotDisplayTrim)
			putSquareDisplay(b, d.Base)
			putSquareDisplay(b, d.Addition)
			b.PutVarInt(d.Pattern + 1)
		} else {
			putBookResult(b, d.ResultID, d.ResultCount)
		}
		putBookStation(b, registry.ItemSmithingTable)
		putEntryTail(b, d.Group, d.Category, []registry.BookIngredient{d.Template, d.Base, d.Addition}, notify, highlight)
		return 1
	}
	return 0
}

// putRecipeBookEntry writes one Recipe Book Add entry: display, group and
// category, requirements, flags. The group is an ID vanilla's recipe scan
// assigns; none is sent as 0.
// vanilla: ClientboundRecipeBookAddPacket's Entry
func putRecipeBookEntry(b *protocol.Buffer, display *registry.CraftingDisplay, notify, highlight bool) {
	b.PutVarInt(display.ID)
	putCraftingDisplay(b, display)
	putEntryTail(b, display.Group, display.Category, display.Squares, notify, highlight)
}

// putFurnaceRecipeBookEntry writes a cooking recipe's Recipe Book Add entry.
// vanilla: ClientboundRecipeBookAddPacket's Entry
func putFurnaceRecipeBookEntry(b *protocol.Buffer, display *registry.FurnaceDisplay, notify, highlight bool) {
	b.PutVarInt(display.ID)
	putFurnaceDisplay(b, display)
	putEntryTail(b, display.Group, display.Category, []registry.BookIngredient{display.Ingredient}, notify, highlight)
}

// putEntryTail writes what follows an entry's display: group, category,
// requirements and flags.
func putEntryTail(b *protocol.Buffer, group, category int32, squares []registry.BookIngredient, notify, highlight bool) {
	b.PutVarInt(group + 1)
	b.PutVarInt(category)
	putCraftingRequirements(b, squares)
	flags := uint8(0)
	if notify {
		flags |= recipeBookEntryNotify
	}
	if highlight {
		flags |= recipeBookEntryNew
	}
	b.PutU8(flags)
}

// putCraftingDisplay writes a crafting recipe's display: shape and squares,
// one craft's output, and the station.
// vanilla: ShapedCraftingRecipeDisplay, ShapelessCraftingRecipeDisplay
func putCraftingDisplay(b *protocol.Buffer, display *registry.CraftingDisplay) {
	if display.Shaped {
		b.PutVarInt(recipeDisplayShaped)
		b.PutVarInt(display.Width)
		b.PutVarInt(display.Height)
	} else {
		b.PutVarInt(recipeDisplayShapeless)
	}
	if slots := display.Slots; slots != nil {
		b.PutVarInt(int32(len(slots.Squares)))
		for _, square := range slots.Squares {
			putBookSlot(b, square)
		}
		putBookSlot(b, slots.Result)
	} else {
		b.PutVarInt(int32(len(display.Squares)))
		for _, square := range display.Squares {
			putSquareDisplay(b, square)
		}
		putBookResult(b, display.ResultID, display.ResultCount, display.ResultComponents...)
	}
	putBookStation(b, registry.ItemCraftingTable)
}

// putBookSlot encodes the recursive SlotDisplay.STREAM_CODEC alternatives.
func putBookSlot(b *protocol.Buffer, slot registry.BookSlot) {
	switch slot.Kind {
	case registry.BookSlotIngredient:
		putSquareDisplay(b, slot.Ingredient)
	case registry.BookSlotStack:
		putBookResult(b, slot.ItemID, slot.Count)
	case registry.BookSlotPotion:
		b.PutVarInt(slotDisplayPotion)
		putBookSlot(b, slot.Children[0])
	case registry.BookSlotComponent:
		b.PutVarInt(slotDisplayComponent)
		putBookSlot(b, slot.Children[0])
		b.PutVarInt(slot.Component)
	case registry.BookSlotDyed:
		b.PutVarInt(slotDisplayDyed)
		putBookSlot(b, slot.Children[0])
		putBookSlot(b, slot.Children[1])
	case registry.BookSlotComposite:
		b.PutVarInt(slotDisplayComposite)
		b.PutVarInt(int32(len(slot.Children)))
		for _, child := range slot.Children {
			putBookSlot(b, child)
		}
	}
}

// putSquareDisplay writes one ingredient square's slot display: the
// ingredient's HolderSet, tag or item list alike, with no crafting remainder.
// vanilla: Ingredient.display
func putSquareDisplay(b *protocol.Buffer, square registry.BookIngredient) {
	if square.Tag == "" && len(square.Items) == 0 {
		b.PutVarInt(slotDisplayEmpty)
		return
	}
	b.PutVarInt(slotDisplayTag)
	putItemHolderSet(b, square)
}

// putItemHolderSet writes a square's items as a HolderSet: 0 and a tag name
// the client resolves, or n+1 and an inline list of n IDs.
// vanilla: ByteBufCodecs.holderSet
func putItemHolderSet(b *protocol.Buffer, square registry.BookIngredient) {
	if square.Tag != "" {
		b.PutVarInt(0)
		b.PutString(square.Tag)
		return
	}
	b.PutVarInt(int32(len(square.Items)) + 1)
	for _, id := range square.Items {
		b.PutVarInt(id)
	}
}

// putFurnaceDisplay writes a cooking recipe's display: the ingredient, any
// fuel, one smelt's output, the station that cooks it, and its duration and
// experience.
// vanilla: FurnaceRecipeDisplay's STREAM_CODEC
func putFurnaceDisplay(b *protocol.Buffer, display *registry.FurnaceDisplay) {
	b.PutVarInt(recipeDisplayFurnace)
	putSquareDisplay(b, display.Ingredient)
	b.PutVarInt(slotDisplayAnyFuel)
	putBookResult(b, display.ResultID, display.ResultCount)
	putBookStation(b, furnaceStations[display.Kind])
	b.PutVarInt(display.Duration)
	b.PutF32(display.Experience)
}

// furnaceStations is the crafting station of each cooking kind's display.
// vanilla: SmeltingRecipe.furnaceIcon and its siblings
var furnaceStations = [...]registry.Item{
	registry.CookingSmelting: registry.ItemFurnace,
	registry.CookingBlasting: registry.ItemBlastFurnace,
	registry.CookingSmoking:  registry.ItemSmoker,
	registry.CookingCampfire: registry.ItemCampfire,
}

// putCraftingRequirements writes what one craft takes, each square's
// HolderSet. Omitted squares need nothing.
// vanilla: RecipeDisplayEntry's craftingRequirements
func putCraftingRequirements(b *protocol.Buffer, squares []registry.BookIngredient) {
	var present []registry.BookIngredient
	for _, square := range squares {
		if square.Tag != "" || len(square.Items) > 0 {
			present = append(present, square)
		}
	}
	b.PutBool(true)
	b.PutVarInt(int32(len(present)))
	for _, square := range present {
		putItemHolderSet(b, square)
	}
}

// The packets that change the book.

// handlePlaceRecipe fills the named window's crafting grid with the
// recipe's items from the inventory (a recipe book click). A spectator, an
// unopened window or an unknown recipe moves nothing; missing items get the
// ghost recipe instead.
// vanilla: ServerGamePacketListenerImpl.handlePlaceRecipe, through
// RecipeBookMenu.handlePlacement and ServerPlaceRecipe
func (c *client) handlePlaceRecipe(r *protocol.Reader) error {
	windowID := r.ReadVarInt()
	recipe := r.ReadVarInt()
	makeAll := r.ReadBool()
	if err := r.Err(); err != nil {
		return c.decodeFailure("place_recipe", err)
	}
	if c.player.GameMode() == protocol.GameModeSpectator {
		return nil
	}
	view := c.currentView()
	if windowID != view.id {
		c.log.Debug("ignoring a recipe placed in a closed window", "window", windowID)
		return nil
	}
	// The game mode is read before the lock is taken, as the click does.
	creative := c.player.GameMode() == protocol.GameModeCreative
	var name string
	var place func(inv *player.Inventory, outcome *inventory.Outcome) bool
	var ghost []byte
	if furnace := registry.FurnaceDisplayOf(recipe); furnace != nil {
		// vanilla: AbstractFurnaceMenu is the one menu kind of each cooking recipe.
		name = furnace.Name
		var ghostBuf protocol.Buffer
		ghostBuf.PutVarInt(windowID)
		putFurnaceDisplay(&ghostBuf, furnace)
		ghost = ghostBuf.Bytes()
		place = func(inv *player.Inventory, outcome *inventory.Outcome) bool {
			if !view.kind.Cooking() {
				return true
			}
			return inventory.PlaceFurnaceRecipe(view.clickView(inv), inv, furnace.Matches, makeAll, creative, outcome)
		}
	} else if display := registry.CraftingDisplayOf(recipe); display != nil {
		name = display.Name
		ghost = placeGhostPayload(windowID, display)
		place = func(inv *player.Inventory, outcome *inventory.Outcome) bool {
			if view.kind != windowPlayer && view.kind != windowCrafting {
				return true
			}
			return inventory.PlaceRecipe(view.clickView(inv), inv, inventory.Recipe{
				Shaped:  display.Shaped,
				Width:   display.Width,
				Height:  display.Height,
				Squares: display.Items,
			}, makeAll, creative, outcome)
		}
	} else {
		c.log.Debug("ignoring a recipe no display names", "recipe", recipe)
		return nil
	}
	if !c.recipeBookState().Knows(name) {
		c.log.Debug("ignoring a recipe the player does not have", "recipe", name)
		return nil
	}
	outcome := inventory.Outcome{}
	var placed bool
	view.inventory(c, func(inv *player.Inventory) {
		// The window can close under this lock, so placement runs like clicks,
		// through the view's useLive.
		view.useLive(func() { placed = place(inv, &outcome) })
	})
	var sendErr error
	view.inventory(c, func(inv *player.Inventory) {
		view.use(func() {
			for _, slot := range outcome.Announce {
				if slot < 0 || slot >= view.slots() {
					continue
				}
				if err := c.sendContainerSlot(inv, view, slot, view.stack(inv, slot)); err != nil {
					sendErr = err
					return
				}
			}
		})
	})
	if sendErr != nil {
		return sendErr
	}
	if outcome.Visible {
		c.broadcastEquipment()
	}
	// No lock is held here: the throws spawn item entities.
	for _, stack := range outcome.Dropped {
		c.throwItem(stack)
	}
	if !placed {
		if err := c.writePacket(protocol.IDPlayPlaceGhostRecipe, "place ghost recipe",
			ghost); err != nil {
			return err
		}
	}
	return nil
}

// placeGhostPayload builds Place Ghost Recipe: the window and the requested
// recipe's display. vanilla: ClientboundPlaceGhostRecipePacket
func placeGhostPayload(windowID int32, display *registry.CraftingDisplay) []byte {
	var b protocol.Buffer
	b.PutVarInt(windowID)
	putCraftingDisplay(&b, display)
	return b.Bytes()
}

// handleRecipeBookSeenRecipe clears a recipe's new highlight when the client
// first draws it; an unknown display changes nothing. vanilla:
// ServerGamePacketListenerImpl.handleRecipeBookSeenRecipePacket
func (c *client) handleRecipeBookSeenRecipe(r *protocol.Reader) error {
	recipe := r.ReadVarInt()
	if err := r.Err(); err != nil {
		return c.decodeFailure("recipe_book_seen_recipe", err)
	}
	name := ""
	if display := registry.CraftingDisplayOf(recipe); display != nil {
		name = display.Name
	} else if display := registry.FurnaceDisplayOf(recipe); display != nil {
		name = display.Name
	} else if display := registry.StonecutterDisplayOf(recipe); display != nil {
		name = display.Name
	} else if display := registry.SmithingDisplayOf(recipe); display != nil {
		name = display.Name
	} else {
		return nil
	}
	c.updateRecipeBook(func(book *inventory.RecipeBook) {
		book.MarkSeen(name)
	})
	return nil
}

// handleRecipeBookChangeSettings stores one of the four books' open and
// filter state, which the player file keeps.
// vanilla: ServerGamePacketListenerImpl.handleRecipeBookChangeSettingsPacket
func (c *client) handleRecipeBookChangeSettings(r *protocol.Reader) error {
	book := r.ReadVarInt()
	open := r.ReadBool()
	filtering := r.ReadBool()
	if err := r.Err(); err != nil {
		return c.decodeFailure("recipe_book_change_settings", err)
	}
	// An out-of-range book fails vanilla's decode.
	// vanilla: FriendlyByteBuf.readEnum
	if book < 0 || book > 3 {
		return c.decodeFailure("recipe_book_change_settings",
			fmt.Errorf("recipe book change settings names book %d, which is not a book", book))
	}
	c.updateRecipeBook(func(next *inventory.RecipeBook) {
		next.Settings.Open[book] = open
		next.Settings.Filtering[book] = filtering
	})
	return nil
}

// GiveRecipes adds named recipes to the book, sending Recipe Book
// Add for new ones, and returns how many were new.
// vanilla: ServerPlayer.awardRecipes
func (c *client) GiveRecipes(names []string) int {
	// The recipe advancements the grant completes give their rewards once the
	// book is unlocked, not at the tick's flush: a take before that flush
	// would get the recipe back. vanilla: PlayerAdvancements.award
	defer c.AnnounceAdvancements()
	return c.giveRecipes(names)
}

// giveRecipes unlocks recipes without dispatching advancement rewards; the
// caller must drain completions after it returns and releases bookMu.
func (c *client) giveRecipes(names []string) int {
	c.bookMu.Lock()
	defer c.bookMu.Unlock()
	next, added := c.recipeBookState().Award(names)
	if next == nil {
		return 0
	}
	c.recipeBook.Store(next)
	c.recipeUnlockedAdvancements(added)
	var entries protocol.Buffer
	count := 0
	for _, name := range added {
		count += putNamedRecipeBookEntry(&entries, name, true, true)
	}
	var b protocol.Buffer
	b.PutVarInt(int32(count))
	b.PutRaw(entries.Bytes())
	b.PutBool(false)
	c.send(protocol.IDPlayRecipeBookAdd, b.Bytes())
	c.refreshLimitedCraftingResult()
	return count
}

// TakeRecipes removes named crafting recipes from the book, sending Recipe
// Book Remove for those held, and returns how many.
// vanilla: ServerPlayer.resetRecipes
func (c *client) TakeRecipes(names []string) int {
	c.bookMu.Lock()
	defer c.bookMu.Unlock()
	next, removed := c.recipeBookState().Revoke(names)
	if next == nil {
		return 0
	}
	c.recipeBook.Store(next)
	var ids []int32
	for _, name := range removed {
		if displays := registry.CraftingDisplaysOfName(name); len(displays) > 0 {
			for _, d := range displays {
				ids = append(ids, d.ID)
			}
		} else {
			id, _ := registry.RecipeDisplayIDOfName(name)
			ids = append(ids, id)
		}
	}
	var b protocol.Buffer
	b.PutVarInt(int32(len(ids)))
	for _, id := range ids {
		b.PutVarInt(id)
	}
	c.send(protocol.IDPlayRecipeBookRemove, b.Bytes())
	c.refreshLimitedCraftingResult()
	return len(ids)
}

// canCraftRecipe reads the live rule and immutable recipe book snapshot.
func (c *client) canCraftRecipe(id string) bool {
	return !c.theWorld().BoolRule(world.RuleLimitedCrafting) || c.recipeBookState().Knows(id)
}

// refreshLimitedCraftingResult is refreshCraftingResult after the book
// changed, which gates crafting only under limitedCrafting.
func (c *client) refreshLimitedCraftingResult() {
	if c.theWorld().BoolRule(world.RuleLimitedCrafting) {
		c.refreshCraftingResult()
	}
}

// refreshCraftingResult corrects an open player grid after its permissions change.
func (c *client) refreshCraftingResult() {
	view := c.currentView()
	if view.kind != windowPlayer && view.kind != windowCrafting {
		return
	}
	c.player.Inventory(func(inv *player.Inventory) {
		if err := c.sendContainerSlot(inv, view, 0, view.stack(inv, 0)); err != nil {
			c.log.Debug("could not refresh crafting result", "error", err)
		}
	})
}

// putBookResult writes an item-stack slot display.
func putBookResult(b *protocol.Buffer, item, count int32, components ...protocol.RawComponent) {
	b.PutVarInt(slotDisplayItemStack)
	b.PutItemTemplate(protocol.ItemStack{ItemID: item, Count: count, Raw: components})
}

// putBookStation writes the station's item slot display.
func putBookStation(b *protocol.Buffer, item registry.Item) {
	b.PutVarInt(slotDisplayItem)
	b.PutVarInt(int32(item))
}
