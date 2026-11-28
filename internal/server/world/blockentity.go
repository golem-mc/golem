package world

import (
	"github.com/golem-mc/golem/internal/server/commandblock"
	"maps"
	"math"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// IsCooking reports the furnace, blast furnace and smoker, which share slots,
// tick and progress fields.
func IsCooking(kind BlockEntityKind) bool {
	return kind == KindFurnace || kind == KindBlastFurnace || kind == KindSmoker
}

// furnaceBurnTimeVersion is the data version of BlockEntityFurnaceBurnTimeFix,
// which renames CookTime/CookTimeTotal/BurnTime to the current
// cooking_time_spent/cooking_total_time/lit_time_remaining.
// vanilla: DataFixers
const furnaceBurnTimeVersion = 4181

// fixFurnaceBurnTime renames pre-1.21.5 progress fields to the current names as
// the fixer does on load, old BurnTime becoming both the remaining burn and the
// total.
// vanilla: BlockEntityFurnaceBurnTimeFix.fixBurnTime
func fixFurnaceBurnTime(entry nbt.CompoundTag) {
	id, _ := nbt.Get[nbt.String](entry.Entries(), "id")
	if k, _ := BlockEntityKindByName(string(id)); !IsCooking(k) {
		return
	}
	renameBlockEntityField(entry, "CookTime", "cooking_time_spent")
	renameBlockEntityField(entry, "CookTimeTotal", "cooking_total_time")
	renameBlockEntityField(entry, "BurnTime", "lit_time_remaining")
	if remaining, ok := entry.Entries()["lit_time_remaining"]; ok {
		entry.Set("lit_total_time", remaining)
	}
}

// unfixFurnaceBurnTime writes progress under a pre-fixer file's names
// (BurnTime, CookTime, CookTimeTotal) so its fixers do not run over the current
// names. lit_total_time has no old name; the fixer rebuilds it from BurnTime.
func unfixFurnaceBurnTime(entry nbt.CompoundTag) {
	id, _ := nbt.Get[nbt.String](entry.Entries(), "id")
	if k, _ := BlockEntityKindByName(string(id)); !IsCooking(k) {
		return
	}
	renameBlockEntityField(entry, "cooking_time_spent", "CookTime")
	renameBlockEntityField(entry, "cooking_total_time", "CookTimeTotal")
	renameBlockEntityField(entry, "lit_time_remaining", "BurnTime")
	entry.Delete("lit_total_time")
}

// renameBlockEntityField moves a field, leaving the entry alone without the old
// name (Dynamic.renameField).
func renameBlockEntityField(entry nbt.CompoundTag, from, to string) {
	if value, ok := entry.Entries()[from]; ok {
		entry.Delete(from)
		entry.Set(to, value)
	}
}

// ContainerSize is the kind's container slot count, zero for none, which /item
// reads as no container.
// vanilla: Container.getContainerSize
func ContainerSize(kind BlockEntityKind) int { return int(containerSize[kind]) }

// slotCount is the stacks an entity of the kind holds: the container size, plus
// the lectern's book and the campfire's four slots, which no container reaches.
// vanilla: CampfireBlockEntity.NUM_SLOTS
func slotCount(kind BlockEntityKind) int {
	switch kind {
	case KindLectern:
		return 1
	case KindCampfire:
		return CampfireSlots
	}
	return int(containerSize[kind])
}

// containerSize is each kind's slot count, numbered as its window numbers them:
// chest 27, furnace 3 (input, fuel, output), dispenser and dropper 9, hopper 5,
// jukebox, decorated pot 1, shelf 3, ender chest none.
var containerSize = [len(kindNames)]uint8{
	KindChest:        27,
	KindTrappedChest: 27,
	KindFurnace:      3,
	KindBlastFurnace: 3,
	KindSmoker:       3,
	// The brewing stand's three bottles, its ingredient and its fuel.
	KindBrewingStand: 5,
	// The crafter's 3x3 grid; its result is computed, not stored.
	KindCrafter:               9,
	KindBarrel:                27,
	KindShulkerBox:            27,
	KindEnderChest:            0,
	KindBell:                  0,
	KindConduit:               0,
	KindPotentSulfur:          0,
	KindCopperGolemStatue:     0,
	KindSculkSensor:           0,
	KindCalibratedSculkSensor: 0,
	KindSculkShrieker:         0,
	KindSculkCatalyst:         0,
	KindEnchantingTable:       0,
	KindDispenser:             9,
	KindDropper:               9,
	KindHopper:                5,
	// The jukebox's one slot, its disc; vanilla JukeboxBlockEntity.
	KindJukebox: 1,
	// The pot's one slot, vanilla ContainerSingleItem.
	KindDecoratedPot: 1,
	// The shelf's three, vanilla ShelfBlockEntity.MAX_ITEMS.
	KindShelf: 3,
	// The bookshelf's six books, one per cell
	// (ChiseledBookShelfBlockEntity.MAX_BOOKS_IN_STORAGE).
	KindChiseledBookshelf: 6,
}

// BlockEntity is what a block keeps besides its state: its slots and its kind's
// progress, what its window shows, what the chunk saves under "block_entities",
// and what a broken container drops.
//
// Everything goes through Use, whose lock keeps clicks and the tick from
// racing. Lock order: inventory, then block entity, then world (see World.mu).
// A click marks the chunk dirty through the world's write lock with the
// entity's held, so an entity's lock is never taken while the world's is held:
// chunk packets, saves and the tag reads and writes reach entity data with the
// world lock released.
type BlockEntity struct {
	// Kind is one of the Kind names, and X, Y, Z its block.
	Kind    BlockEntityKind
	X, Y, Z int32

	// Every block-entity tick reads these fields, so they share a cache line with Kind.
	// Cooldown is the hopper's ticks until it may move again: above zero it
	// does nothing; at zero it moves and resets to 8 if it moved something.
	// Saved under the same name; a never-ticked hopper saves its initial -1,
	// which reads as idle like zero.
	Cooldown int32

	// TickedGameTime is the hopper's last tick time
	// (HopperBlockEntity.tickedGameTime), read by the chained-hopper cooldown:
	// a hopper whose own tick ran at or after the giver's takes one tick less.
	// Not saved.
	TickedGameTime int64
	// tickOrder is immutable after registration, for a sleeping hopper's
	// chained cooldown to account for the no-op turns it did not execute.
	tickOrder uint64
	// hopperSlept records no-op turns until a real turn or transfer accounts for them.
	hopperSlept bool

	// removed is set by RemoveBlockEntity and chunk unload, read by UseLive.
	// Atomic rather than under mu: an unload flags entities while holding the
	// world's lock, and taking mu there would invert the lock order (entity
	// before world) and deadlock.
	removed atomic.Bool

	// ticker is a wake generation, with bit 0 set while the ticker is a no-op.
	// World writes wake without mu to preserve the entity-before-world lock order.
	ticker atomic.Uint64

	mu sync.Mutex

	// pluginData is private saved data, excluded from client update tags.
	pluginData nbt.Compound

	// Components are the placed stack's components no implicit reader of the
	// kind consumed (lore, custom_data, ...), by name as in an item's patch.
	// vanilla: BlockEntity.components
	Components nbt.Compound

	// HopperTargets holds runtime lookup and failed-transfer stamps, under mu.
	HopperTargets    *HopperTargets
	inventoryVersion atomic.Uint64

	// Slots is the contents, indexed as the NBT and window number them. Stored
	// holds the file's compound for a slot this server cannot show
	// (player.Inventory.Stored), so vanilla-filled enchanted gear survives
	// being opened here; such a slot reads empty and a write replaces it.
	// Writes to Slots go through Put (or PutKeepingCook), which keep the
	// filled mask current.
	Slots  []protocol.ItemStack
	filled uint64 // bit i set exactly when Slots[i].Count > 0; containers have at most 64 slots
	Stored map[int]nbt.Compound

	// CustomName is the window title replacing the default, from a placed
	// item's minecraft:custom_name; nil when placed bare. Saved under the
	// vanilla key.
	CustomName *protocol.Text

	// Lock is the container's LockCode, an item predicate under the vanilla
	// key; nil when unlocked. Kept as read so a save writes it back whole;
	// UnlockedBy reads the fields ItemPredicate owns and drops those its codec
	// rejects.
	// vanilla: BaseContainerBlockEntity.loadAdditional, LockCode
	Lock nbt.CompoundTag

	// LootTable and its seed are RandomizableContainerBlockEntity's fields,
	// empty for a container keeping its own items. World.UnpackLoot rolls them
	// into the slots on the first touch; a save writes them back while pending.
	// Saved under the vanilla keys.
	LootTable     string
	LootTableSeed int64

	// Furnace progress in ticks: the current smelt's progress and length, and
	// the current fuel lot's remaining and total burn. Zero for a chest.
	CookingSpent, CookingTotal int32
	// CookCheck is the id of the cooking recipe that last matched, which wins
	// over an earlier recipe taking the same input; not saved.
	// vanilla: AbstractFurnaceBlockEntity.quickCheck, CampfireBlock.getTicker
	CookCheck              string
	cookReset              atomic.Bool // a state write asks the campfire to drop CookCheck
	LitRemaining, LitTotal int32
	// SpeedMultiplier is what the fuel last lit (a furnace) or loaded (a
	// brewing stand) divides the recipe's time by; nought counts as 1.
	// vanilla: AbstractFurnaceBlockEntity.speedMultiplier
	SpeedMultiplier float32

	// CampfireProgress and CampfireTotals are each slot's cooking ticks and
	// cook time, saved as CookingTimes and CookingTotalTimes.
	// vanilla: CampfireBlockEntity.cookingProgress, cookingTime
	CampfireProgress, CampfireTotals [CampfireSlots]int32

	// RecipesUsed counts finished smelts per recipe id since their experience
	// was last paid (as orbs when the result is taken or the furnace broken),
	// saved under vanilla's name. Nil for a chest.
	RecipesUsed recipeHistory

	// The brewing stand's ticks left of the current brew and fuel left, saved
	// under vanilla's names.
	// vanilla: BrewingStandBlockEntity
	BrewTime, Fuel int
	// BrewTotal and FuelTotal are the length of the current brew and the uses
	// the last fuel loaded, for the window's gauges.
	BrewTotal, FuelTotal int
	// BrewingIngredient is the item ID the current brew started from, zero for
	// none. Not saved: vanilla restores it from the slot on load, and the tick
	// cancels the brew when the slot's item changes.
	// vanilla: BrewingStandBlockEntity.ingredient
	BrewingIngredient int32
	// BrewingBottles is which bottle slots held anything at the last tick
	// (lastPotionCount). Nil until the first tick, as vanilla's field is, so
	// that tick writes the bits even with all slots empty and corrects a block
	// with stale bits; an unchanged tick writes no block update. Not saved.
	// vanilla: BrewingStandBlockEntity.lastPotionCount
	BrewingBottles *[3]bool

	// The crafter's disabled slots and whether a pulse has it mid-craft, saved
	// as disabled_slots (an int array of indexes) and triggered.
	DisabledSlots [9]bool
	Triggered     bool
	// CraftingTicks is the crafting animation's ticks left, saved as
	// crafting_ticks_remaining.
	// vanilla: CrafterBlockEntity.craftingTicksRemaining
	CraftingTicks int

	// Output is the comparator's output signal (ComparatorBlockEntity): its
	// last tick's comparison, delivered out the back until the next. Saved
	// under vanilla's name, so a reload keeps the strength.
	Output int

	// SculkFrequency is a sculk sensor's last vibration frequency, saved as
	// last_vibration_frequency.
	SculkFrequency int
	// WarningLevel is a shrieker's last warning level, saved as warning_level.
	WarningLevel int
	// Countdown is a potent sulfur geyser's seconds to its next swap, -1 until
	// rolled. Saved as countdown.
	Countdown int
	// cursors is a sculk catalyst's spreader, saved as cursors; Physics
	// accesses it under Use.
	cursors []chargeCursor
	// hasCursors lets idle catalyst adoption skip the entity lock.
	hasCursors atomic.Bool
	// listener is a sensor's or shrieker's vibration listener, saved as
	// listener; Physics copies its live one in under Use (saveListener).
	// listenerLoaded is set by each load, and Physics takes the loaded
	// listener up when it clears it (adoptListeners).
	listener       vibrationListener
	listenerLoaded atomic.Bool

	// Openers is how many players a trapped chest last counted, its signal
	// strength.
	// vanilla: ContainerOpenersCounter
	Openers int

	// TicksSinceSongStarted is how long a jukebox's disc (slot 0, written as
	// the RecordItem compound) has played; at or past the song's length the
	// song is over with the disc still in.
	// vanilla: JukeboxSongPlayer.ticksSinceSongStarted
	TicksSinceSongStarted int64

	// Page is a lectern book's open page.
	// vanilla: LecternBlockEntity.page
	Page int

	// The beacon's pyramid level from its 80-tick update, and its primary and
	// secondary effects as mob effect IDs (-1 unset). The effects save under
	// vanilla's names and the level as Levels, though a load recounts the level
	// rather than reading it.
	// vanilla: BeaconBlockEntity.primaryPower, secondaryPower, levels
	BeaconLevels                   int
	BeaconPrimary, BeaconSecondary int32

	// The spawner's BaseSpawner numbers: the next spawn's delay and the bounds
	// it is rolled between, plus count, neighbourhood and ranges. Saved as
	// vanilla's shorts; a fresh spawner has BaseSpawner's defaults.
	Delay, MinSpawnDelay, MaxSpawnDelay int
	SpawnCount, MaxNearbyEntities       int
	RequiredPlayerRange, SpawnRange     int

	// SpawnData is BaseSpawner.nextSpawnData as its codec writes it: the
	// entity's compound under "entity" and anything else the file had beside
	// it. Nil is a fresh spawner's absent data.
	SpawnData nbt.CompoundTag

	// SpawnPotentials is the weighted spawn list, defaulted on load to one
	// entry containing SpawnData when the list is absent.
	// vanilla: BaseSpawner.load
	SpawnPotentials nbt.List

	// The sign's two sides and wax (SignBlockEntity's front_text, back_text,
	// is_waxed).
	SignFront, SignBack SignText
	Waxed               bool
	// AllowOpFeatures lets a sign's click events run.
	// vanilla: SignBlockEntity.allowOpFeatures
	AllowOpFeatures bool

	// The decorated pot's sherds as written (an object of optional item
	// templates), saved back whole, and the shelf's align-to-bottom flag.
	// vanilla: PotDecorations, ShelfBlockEntity.ALIGN_ITEMS_TO_BOTTOM_TAG
	Sherds        nbt.CompoundTag
	BottomAligned bool

	// LastInteractedSlot is the chiseled bookshelf's last cell a book moved
	// through; one past it is the comparator's read from any side, -1 until a
	// book moves. Saved under vanilla's name.
	// vanilla: ChiseledBookShelfBlockEntity.lastInteractedSlot
	LastInteractedSlot int

	// Editor is the UUID of the player an open editor still belongs to, zero
	// for none. Not saved: playerWhoMayEdit lasts until its holder writes or
	// walks out of reach.
	// vanilla: SignBlockEntity.playerWhoMayEdit
	Editor uuid.UUID

	// Target is the body a conduit's beam strikes, by the chunk format's
	// four-int UUID; uuid.Nil while resting. Saved under the vanilla key.
	// vanilla: ConduitBlockEntity.destroyTarget
	Target uuid.UUID

	// ConduitActive is whether the frame last read active. Not saved: each
	// load's first refresh recomputes it.
	// vanilla: ConduitBlockEntity.isActive
	ConduitActive bool

	// Bell is a bell's swing and resonance counters. Not saved.
	// vanilla: BellBlockEntity
	Bell BellState

	// ConduitNextAmbient is when the next short ambient is due, 60-99 ticks
	// after the last. Not saved: a fresh load starts due.
	// vanilla: ConduitBlockEntity.nextAmbientSoundActivation
	ConduitNextAmbient int64

	// Creaking is the heart's linked protector (four-int UUID, nil for none),
	// which keeps a linked heart from uprooting. CreakingTicker is the state
	// switch's countdown, rolled 20-24 each firing; not saved, so a loaded
	// heart rereads one tick out. CreakingEmitter counts down the hundred
	// ticks of the particle trail a hurt creaking starts, CreakingExisted the
	// ticks since the link was made or the heart loaded, and CreakingOutput is
	// the comparator strength; none of them is saved.
	// vanilla: CreakingHeartBlockEntity.creakingInfo, ticker, emitter,
	// ticksExisted, outputSignal
	Creaking        uuid.UUID
	CreakingTicker  int32
	CreakingEmitter int32
	CreakingExisted int64
	CreakingOutput  int
	// CreakingTarget is the creaking's centre when the trail last aimed.
	CreakingTarget [3]float64

	// The command block's BaseCommandBlock (command, success count read by
	// comparators, output, last run) and its CommandBlockEntity fields: its
	// power, last met condition and always-active flag.
	commandblock.State
	Powered      bool
	ConditionMet bool
	Automatic    bool
}

// SignText is one sign side: four lines, the dye colour name and glow. Fresh
// sides are empty and black.
// vanilla: SignText
type SignText struct {
	Messages [4]string
	Color    string
	Glowing  bool

	// raw holds each non-plain line's component as read (a styled or translated
	// vanilla line Messages' string cannot carry); nil marks a line this server
	// may replace. encode writes raw back where set.
	raw [4]nbt.Tag
	// nil means filtered lines follow Messages and raw.
	filtered *[4]nbt.Tag
}

// ClickEvents is each line's root click_event compound, nil for a line with
// none; a list line's root is its first element. vanilla: Style.getClickEvent
func (t SignText) ClickEvents() [4]nbt.Compound {
	var events [4]nbt.Compound
	for i, raw := range t.raw {
		if list, ok := raw.(nbt.List); ok && len(list.Items) > 0 {
			raw = nbt.ListElement(list.Items[0])
		}
		if c, ok := raw.(nbt.CompoundTag); ok {
			events[i], _ = nbt.Get[nbt.Compound](c.Entries(), "click_event")
		}
	}
	return events
}

// SetMessages replaces the four lines with an editor's plain text. A line
// whose loaded component is a literal keeps its style keys, and a line that was
// anything else becomes plain.
// vanilla: SignBlockEntity.updateMessages
func (t *SignText) SetMessages(messages [4]string) {
	old := t.raw
	t.Messages = messages
	t.raw = [4]nbt.Tag{}
	t.filtered = nil
	for i, tag := range old {
		compound, ok := tag.(nbt.CompoundTag)
		if !ok {
			continue
		}
		if _, literal := signLiteral(compound); !literal {
			continue
		}
		styled := nbt.Compound{}
		for _, key := range signStyleKeys {
			if v, ok := compound.Entries()[key]; ok {
				styled[key] = v
			}
		}
		if len(styled) == 0 {
			continue
		}
		styled["text"] = nbt.String(messages[i])
		t.raw[i] = styled
	}
}

// Editable reports whether every line's root is a replaceable literal (styled
// or empty counts; translated, keybind, score, selector or NBT does not), what
// SignBlock.hasEditableText checks before opening the editor.
// vanilla: SignBlock.hasEditableText
func (t SignText) Editable() bool {
	for _, raw := range t.raw {
		if raw == nil {
			continue
		}
		if _, plain := signLiteral(raw); !plain {
			return false
		}
	}
	return true
}

// signLine is a saved line's plain text and whether it is a bare literal the
// codec writes as such (a string, or a compound with only "text" or the empty
// key). A styled literal has its text here but is not bare, so a save keeps its
// whole component.
func signLine(component nbt.Tag) (string, bool) {
	text, plain := signLiteral(component)
	if !plain {
		return "", false
	}
	if c, ok := nbt.CompoundView(component); ok {
		return text, len(c) == 1
	}
	return text, true
}

// signStyleKeys are the keys of a component root that make up its Style.
// vanilla: Style.Serializer.MAP_CODEC
var signStyleKeys = [...]string{
	"color", "font", "bold", "italic", "underlined", "strikethrough",
	"obfuscated", "shadow_color", "insertion", "click_event", "hover_event",
}

// signLiteral is a root's literal text and whether the root is a literal (a
// string, or a compound with a "text" or empty key, style keys allowed).
func signLiteral(component nbt.Tag) (string, bool) {
	switch v := component.(type) {
	case nbt.String:
		return string(v), true
	case nbt.CompoundTag:
		if text, ok := nbt.Get[nbt.String](v.Entries(), "text"); ok {
			return string(text), true
		}
		if text, ok := nbt.Get[nbt.String](v.Entries(), ""); ok {
			return string(text), true
		}
	}
	return "", false
}

// HasMessage reports whether any line says anything, which dyes and ink sacs
// check before touching a side. A translated, keybind, score, selector or NBT
// root counts even without literal text.
// vanilla: SignText.hasMessage
func (t SignText) HasMessage() bool {
	for i := range t.Messages {
		if t.Messages[i] != "" {
			return true
		}
		if raw := t.raw[i]; raw != nil {
			if _, plain := signLiteral(raw); !plain {
				return true
			}
		}
	}
	return false
}

// LootFill rolls the named loot table into the container at x, y, z's empty
// slots, seeded by seed (0 for none), and reports whether the table was
// known. The place is the location checks' origin.
type LootFill func(w *World, x, y, z int32, table string, seed int64, slots []protocol.ItemStack) bool

// SetLootFill sets the roll UnpackLoot uses; call it before the world is
// shared.
func (w *World) SetLootFill(fill LootFill) { w.lootFill = fill }

// UnpackLoot rolls the container's pending loot table into its slots and clears
// it, so it rolls once; an unknown table stays pending. The caller holds b's
// lock.
// vanilla: RandomizableContainer.unpackLootTable
func (w *World) UnpackLoot(b *BlockEntity) { w.UnpackLootWith(b, w.lootFill) }

// UnpackLootWith is UnpackLoot rolled by fill, for a roll that knows what the
// world does not: the player opening the container.
func (w *World) UnpackLootWith(b *BlockEntity, fill LootFill) {
	if b.LootTable == "" || fill == nil {
		return
	}
	if !fill(w, b.X, b.Y, b.Z, b.LootTable, b.LootTableSeed, b.Slots) {
		return
	}
	b.recomputeFilled()
	b.LootTable, b.LootTableSeed = "", 0
	w.Touch(b.X, b.Y, b.Z)
}

// LootReads are the server-wide stores a loot roll in a level reads: the
// scoreboard and the command storages (rules.LevelReads).
type LootReads interface {
	Score(holder, objective string) (int32, bool)
	StorageNumber(storage, path string) (nbt.Tag, bool)
	Storage(id string) nbt.Compound
}

// SetLootReads sets the stores LootReads returns; call it before the world is
// shared.
func (w *World) SetLootReads(r LootReads) { w.lootReads = r }

// LootReads is the stores a loot roll in this level reads; nil before
// SetLootReads.
func (w *World) LootReads() LootReads { return w.lootReads }

// UnpackBrushLoot rolls a brushable block's pending loot table into its hidden
// item with roll, saving the item in the table's place, and returns it with the
// table it rolled (empty if none was rolled). roll is seeded by seed (0 for
// none) and reports false for an unknown table, which stays pending.
// vanilla: BrushableBlockEntity.unpackLootTable
func (w *World) UnpackBrushLoot(x, y, z int32, roll func(table string, seed int64) ([]protocol.ItemStack, bool)) (protocol.ItemStack, string) {
	tag, ok := w.BlockEntityTag(x, y, z)
	if !ok {
		return protocol.ItemStack{}, ""
	}
	if table, ok := nbt.Get[nbt.String](tag, "LootTable"); ok {
		seed, _ := nbt.LongValue(tag["LootTableSeed"])
		stacks, known := roll(string(table), seed)
		if !known {
			return protocol.ItemStack{}, ""
		}
		var item protocol.ItemStack
		if len(stacks) > 0 {
			item = stacks[0]
		}
		next := nbt.Compound{}
		for k, v := range tag {
			if k != "LootTable" && k != "LootTableSeed" {
				next[k] = v
			}
		}
		if !item.Empty() {
			next["item"] = registry.StackTag(item)
		}
		w.SetBlockEntityTag(x, y, z, KindBrushable.Name(), next)
		return item, string(table)
	}
	if entry, ok := nbt.Get[nbt.Compound](tag, "item"); ok {
		item, _ := registry.StoredStack(entry)
		return item, ""
	}
	return protocol.ItemStack{}, ""
}

// Use runs fn with exclusive access to the entity; one lock guards clicks, the
// furnace tick and encoding, which all act on the same slots.
func (b *BlockEntity) Use(fn func(*BlockEntity)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.Wake()
	fn(b)
}

// UseLive is Use for a caller that found the entity by lookup before locking:
// fn runs only if the entity is still in the world, so a move cannot land in a
// container a blast, break or command removed meanwhile. A caller that removed
// the entity itself uses Use, since its read of the contents must still happen.
func (b *BlockEntity) UseLive(fn func(*BlockEntity)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.removed.Load() {
		return
	}
	b.Wake()
	fn(b)
}

// Unlocked runs fn with the entity's lock released, for a plugin listener that
// may read the entity. Call it only from inside Tick or Use; the caller
// re-reads the slots it acted on afterwards, as another goroutine may have
// written them.
func (b *BlockEntity) Unlocked(fn func()) {
	b.mu.Unlock()
	defer b.mu.Lock()
	fn()
}

// Removed reports whether the entity has left the world.
func (b *BlockEntity) Removed() bool { return b.removed.Load() }

// Put replaces a slot's stack, dropping any compound the file left there, as
// player.Inventory.Set does.
func (b *BlockEntity) Put(slot int, stack protocol.ItemStack) {
	if slot < 0 || slot >= len(b.Slots) {
		return
	}
	// A changed ingredient restarts the cook (setItem zeroes the timer and
	// recomputes the total); emptying the slot is a change, the same item at
	// another count is not.
	// vanilla: AbstractFurnaceBlockEntity.setItem
	if slot == 0 && IsCooking(b.Kind) && (stack.Empty() || !stack.SamePtr(&b.Slots[0])) {
		b.CookingTotal = b.CookTotalOf(registry.CookingMatchCached(b.Kind.Name(), stack, &b.CookCheck))
		b.CookingSpent = 0
	}
	// A write into a disabled grid slot enables it first, even an empty one.
	// vanilla: CrafterBlockEntity.setItem
	if b.Kind == KindCrafter && b.SlotDisabled(slot) {
		b.SetCrafterSlotState(slot, true)
	}
	b.PutKeepingCook(slot, stack)
}

// PutKeepingCook replaces a slot's stack without restarting a cook, for an
// input that shrinks in place.
func (b *BlockEntity) PutKeepingCook(slot int, stack protocol.ItemStack) {
	if slot < 0 || slot >= len(b.Slots) {
		return
	}
	b.Wake()
	b.inventoryVersion.Add(1)
	b.Slots[slot] = stack
	if len(b.Stored) > 0 {
		delete(b.Stored, slot)
	}
	b.setFilled(slot, stack.Count > 0)
}

// FilledSlots is the mask of slots holding items: bit i is set exactly when
// Slots[i].Count > 0.
func (b *BlockEntity) FilledSlots() uint64 { return b.filled }

func (b *BlockEntity) setFilled(slot int, on bool) {
	if slot >= 64 {
		return
	}
	if on {
		b.filled |= 1 << slot
	} else {
		b.filled &^= 1 << slot
	}
}

// recomputeFilled rebuilds the mask from Slots after a bulk write.
func (b *BlockEntity) recomputeFilled() {
	b.Wake()
	b.inventoryVersion.Add(1)
	b.filled = 0
	for i := range min(len(b.Slots), 64) {
		if b.Slots[i].Count > 0 {
			b.filled |= 1 << i
		}
	}
}

// CookTotalOf is the recipe's cook time at the entity's speed, rounded up, or
// the standard 200 for no recipe.
// vanilla: AbstractFurnaceBlockEntity.getTotalCookTime
func (b *BlockEntity) CookTotalOf(recipe *registry.CookingRecipe) int32 {
	if recipe == nil {
		return 200
	}
	return ScaledTicks(recipe.CookTime, b.SpeedMultiplier)
}

// ScaledTicks is a duration divided by a fuel's speed multiplier, rounded up;
// a speed that is not positive leaves it whole. The arithmetic is vanilla's
// own, in float32.
// vanilla: AbstractFurnaceBlockEntity.getTotalCookTime, BrewingStandBlockEntity.serverTick
func ScaledTicks(ticks int32, speed float32) int32 {
	if speed <= 0 {
		return ticks
	}
	return int32(math.Ceil(float64(float32(ticks) / speed)))
}

// Len is the entity's slot count, which its window lays over too.
func (b *BlockEntity) Len() int { return len(b.Slots) }

// ApplyItemStack copies a placed item's components onto the entity
// (BlockEntity.applyComponentsFromItemStack): minecraft:container slots,
// minecraft:custom_name, the pot's minecraft:pot_decorations and a
// randomizable container's minecraft:container_loot. A lockable container
// takes minecraft:lock, unlocked when absent. Slot entries past the entity's
// slots are dropped.
func (b *BlockEntity) ApplyItemStack(stack protocol.ItemStack) {
	for i, entry := range stack.Container {
		if i >= len(b.Slots) {
			break
		}
		b.Put(i, entry)
	}
	if stack.CustomName != nil {
		b.CustomName = stack.CustomName
	}
	// A stack without the component unlocks the container, over any lock
	// block_entity_data set.
	// vanilla: BaseContainerBlockEntity.applyImplicitComponents
	if lockableKind(b.Kind) {
		b.Lock = nil
		if stack.Lock != nil {
			b.Lock = saveLock(stack.Lock)
		}
	}
	// The pot rolls a LootTable too, but is no RandomizableContainerBlockEntity.
	// vanilla: RandomizableContainerBlockEntity.applyImplicitComponents
	if stack.ContainerLoot.Table != "" && lootTableKind(b.Kind) && b.Kind != KindDecoratedPot {
		b.LootTable, b.LootTableSeed = stack.ContainerLoot.Table, stack.ContainerLoot.Seed
	}
	// A stack without the component places the item's default, four bricks;
	// sherds stays nil for a pot with no side, which saves no key.
	// vanilla: DecoratedPotBlockEntity.applyImplicitComponents
	if b.Kind == KindDecoratedPot {
		sides, ok := stack.PotDecorationsOf()
		if !ok {
			sides = registry.PotAllBrick()
		}
		b.Sherds = blockRecord(registry.PotDecorationsTag(sides), "back", "left", "right", "front")
		if len(b.Sherds.Entries()) == 0 {
			b.Sherds = nil
		}
	}
}

// ApplyLeftoverComponents keeps the components of a placed stack that the
// kind's implicit reader did not consume, minus block_entity_data and
// block_state, and replaces what the entity held before. Removals in the patch
// are not kept.
// vanilla: BlockEntity.applyComponents
func (b *BlockEntity) ApplyLeftoverComponents(stack protocol.ItemStack) {
	b.Components = leftoverComponents(b.Kind, stack)
}

func leftoverComponents(kind BlockEntityKind, stack protocol.ItemStack) nbt.Compound {
	var left nbt.Compound
	implicit := implicitComponentNames(kind)
	for name, value := range registry.StackComponents(stack) {
		if strings.HasPrefix(name, "!") || name == "minecraft:block_entity_data" || name == "minecraft:block_state" {
			continue
		}
		// The statue's custom_name is written from CustomName.
		if kind == KindCopperGolemStatue && name == "minecraft:custom_name" {
			continue
		}
		if slices.Contains(implicit, name) {
			continue
		}
		if left == nil {
			left = nbt.Compound{}
		}
		left[name] = nbt.Copy(value)
	}
	return left
}

// implicitComponentNames are the components a kind's applyImplicitComponents
// reads, which stay out of the entity's own components.
// vanilla: the kinds' applyImplicitComponents overrides
func implicitComponentNames(kind BlockEntityKind) []string {
	const (
		name      = "minecraft:custom_name"
		lock      = "minecraft:lock"
		container = "minecraft:container"
		loot      = "minecraft:container_loot"
	)
	switch kind {
	case KindFurnace, KindBlastFurnace, KindSmoker, KindBrewingStand:
		return []string{name, lock, container}
	case KindChest, KindTrappedChest, KindBarrel, KindShulkerBox, KindCrafter,
		KindHopper, KindDispenser, KindDropper:
		return []string{name, lock, container, loot}
	case KindBeacon:
		return []string{name, lock}
	case KindEnchantingTable:
		return []string{name}
	case KindSkull:
		return []string{"minecraft:profile", "minecraft:note_block_sound", name}
	case KindCampfire, KindShelf, KindChiseledBookshelf:
		return []string{container}
	case KindDecoratedPot:
		return []string{"minecraft:pot_decorations", container}
	case KindSign, KindHangingSign:
		return []string{"minecraft:sign_text_front", "minecraft:sign_text_back", "minecraft:waxed"}
	case KindBeehive:
		return []string{"minecraft:bees"}
	case KindBanner:
		return []string{"minecraft:banner_patterns", name}
	case KindCommandBlock:
		return []string{name}
	}
	return nil
}

// PotDecorationsPatch is the minecraft:pot_decorations a pot whose "sherds"
// compound this is lays on its item: nil for four bricks, the item's default,
// which the patch drops. Absent or unreadable sherds are EMPTY, a patch.
// vanilla: DecoratedPotBlockEntity.collectImplicitComponents, PatchedDataComponentMap.set
func PotDecorationsPatch(sherds nbt.Compound) *[4]protocol.ItemStack {
	var sides [4]protocol.ItemStack
	if read, ok := registry.PotDecorationsOf(sherds); ok {
		sides = read
	}
	if registry.IsPotAllBrick(sides) {
		return nil
	}
	return &sides
}

// LoadBlockEntityData lays a stack's minecraft:block_entity_data on the entity
// (BlockItem.updateCustomBlockEntityTag, which refuses data naming another
// type), the placement counterpart of ItemComponentsAt. Components applied
// afterwards override it.
// vanilla: BlockEntity.loadWithComponents, TypedEntityData.loadInto
func (b *BlockEntity) LoadBlockEntityData(typeID int32, tag nbt.Compound) {
	if b.TypeID() == typeID {
		b.loadFrom(tag)
	}
}

// OpOnlyKind reports a type that takes block_entity_data only from a
// gamemaster.
// vanilla: BlockEntityTypes.OP_ONLY_CUSTOM_DATA, BlockItem.updateCustomBlockEntityTag
func OpOnlyKind(kind BlockEntityKind) bool {
	switch kind {
	case KindCommandBlock, KindSpawner, KindSign, KindHangingSign,
		KindLectern, KindTrialSpawner:
		return true
	}
	return false
}

// SlotDisabled reports a disabled crafter slot, which hopper inserts and player
// clicks check before filling.
func (b *BlockEntity) SlotDisabled(slot int) bool {
	return slot >= 0 && slot < len(b.DisabledSlots) && b.DisabledSlots[slot]
}

// SetCrafterSlotState enables or disables a grid slot; only an empty slot
// changes, and it reports whether the slot could. Grid slot i is Slots[i].
// vanilla: CrafterBlockEntity.setSlotState, slotCanBeDisabled
func (b *BlockEntity) SetCrafterSlotState(slot int, enabled bool) bool {
	if !b.crafterSlotCanBeDisabled(slot) {
		return false
	}
	b.DisabledSlots[slot] = !enabled
	b.Wake()
	b.inventoryVersion.Add(1)
	return true
}

func (b *BlockEntity) crafterSlotCanBeDisabled(slot int) bool {
	return slot >= 0 && slot < len(b.DisabledSlots) && slot < len(b.Slots) && b.Slots[slot].Count <= 0
}

// UnlockedBy reports whether the held stack satisfies the lock: nil opens for
// anyone. Call it inside Use.
// vanilla: LockCode.unlocksWith, ItemPredicate.test
func (b *BlockEntity) UnlockedBy(hand protocol.ItemStack) bool {
	if b.Lock == nil {
		return true
	}
	return registry.DecodeItemPredicate(b.Lock.Entries()).Matches(hand)
}

// RecordRecipeUsed counts one more finished smelt of the recipe id toward
// unpaid experience, the tick's setRecipeUsed.
func (b *BlockEntity) RecordRecipeUsed(id string) {
	b.RecipesUsed.add(id, 1)
}

// TakeRecipesUsed returns and clears the smelting history once its experience
// is paid; the caller turns it into orbs under the same lock so nothing is paid
// twice. Nil when nothing finished since the last payout.
func (b *BlockEntity) TakeRecipesUsed() map[string]int {
	used := b.RecipesUsed.counts
	b.RecipesUsed = recipeHistory{}
	return used
}

// JukeboxSong returns the minecraft:jukebox_song of the jukebox's disc, false
// without a playable stack. Call it inside Use.
func (b *BlockEntity) JukeboxSong() (string, bool) {
	if b.Kind != KindJukebox || b.Slots[0].Empty() {
		return "", false
	}
	return registry.JukeboxSongForStack(b.Slots[0])
}

// jukeboxSongEndDelay is how long past a song's length it counts as finished.
// vanilla: JukeboxSong.hasFinished
const jukeboxSongEndDelay int64 = 20

// JukeboxPlaying reports a song set (a disc and started ticks), what its signal
// answers; true through the end delay until the tick that stops it. Call it
// inside Use.
// vanilla: JukeboxSongPlayer.isPlaying
func (b *BlockEntity) JukeboxPlaying() bool {
	if _, ok := b.JukeboxSong(); !ok {
		return false
	}
	return b.TicksSinceSongStarted >= 0
}

// JukeboxFinished reports a set song past its length plus the end delay, which
// the next tick stops. Call it inside Use.
// vanilla: JukeboxSong.hasFinished
func (b *BlockEntity) JukeboxFinished() bool {
	song, ok := b.JukeboxSong()
	if !ok {
		return true
	}
	entry, ok := registry.JukeboxSongOf(song)
	if !ok {
		return true
	}
	return b.TicksSinceSongStarted >= entry.LengthTicks+jukeboxSongEndDelay
}

// JukeboxComparatorOutput is the stored disc's song output, which does not fall
// when the song ends. Call it inside Use.
// vanilla: JukeboxBlockEntity.getComparatorOutput
func (b *BlockEntity) JukeboxComparatorOutput() int {
	song, ok := b.JukeboxSong()
	if !ok {
		return 0
	}
	entry, _ := registry.JukeboxSongOf(song)
	return entry.Comparator
}

// blockEntityID is a kind's minecraft:block_entity_type wire ID; the registry
// is built into the client, so these are this version's constants.
func blockEntityID(kind BlockEntityKind) int32 {
	if int(kind) < len(blockEntityIDByKind) {
		return blockEntityIDByKind[kind]
	}
	return -1
}

// blockEntityIDByKind is indexed by kind; unmodelled slots are -1.
var blockEntityIDByKind = func() [len(kindNames)]int32 {
	t := [len(kindNames)]int32{}
	for i := range t {
		t[i] = -1
	}
	t[KindChest] = protocol.BlockEntityChest
	t[KindTrappedChest] = protocol.BlockEntityTrappedChest
	t[KindFurnace] = protocol.BlockEntityFurnace
	t[KindBlastFurnace] = protocol.BlockEntityBlastFurnace
	t[KindSmoker] = protocol.BlockEntitySmoker
	t[KindBrewingStand] = protocol.BlockEntityBrewingStand
	t[KindCrafter] = protocol.BlockEntityCrafter
	t[KindBarrel] = protocol.BlockEntityBarrel
	t[KindShulkerBox] = protocol.BlockEntityShulkerBox
	t[KindEnderChest] = protocol.BlockEntityEnderChest
	t[KindDispenser] = protocol.BlockEntityDispenser
	t[KindDropper] = protocol.BlockEntityDropper
	t[KindHopper] = protocol.BlockEntityHopper
	t[KindComparator] = protocol.BlockEntityComparator
	t[KindPiston] = protocol.BlockEntityPiston
	t[KindSpawner] = protocol.BlockEntitySpawner
	t[KindCommandBlock] = protocol.BlockEntityCommandBlock
	t[KindBell] = protocol.BlockEntityBell
	t[KindJukebox] = protocol.BlockEntityJukebox
	t[KindLectern] = protocol.BlockEntityLectern
	t[KindDaylightDetector] = protocol.BlockEntityDaylightDetector
	t[KindBeacon] = protocol.BlockEntityBeacon
	t[KindSign] = protocol.BlockEntitySign
	t[KindHangingSign] = protocol.BlockEntityHangingSign
	t[KindDecoratedPot] = protocol.BlockEntityDecoratedPot
	t[KindShelf] = protocol.BlockEntityShelf
	t[KindChiseledBookshelf] = protocol.BlockEntityChiseledBookshelf
	t[KindConduit] = protocol.BlockEntityConduit
	t[KindPotentSulfur] = protocol.BlockEntityPotentSulfur
	t[KindCopperGolemStatue] = protocol.BlockEntityCopperGolemStatue
	t[KindEnchantingTable] = protocol.BlockEntityEnchantingTable
	t[KindCampfire] = protocol.BlockEntityCampfire
	t[KindCreakingHeart] = protocol.BlockEntityCreakingHeart
	t[KindSculkSensor] = protocol.BlockEntitySculkSensor
	t[KindCalibratedSculkSensor] = protocol.BlockEntityCalibratedSculkSensor
	t[KindSculkShrieker] = protocol.BlockEntitySculkShrieker
	t[KindSculkCatalyst] = protocol.BlockEntitySculkCatalyst
	return t
}()

// TypeID is the entity's minecraft:block_entity_type ID.
func (b *BlockEntity) TypeID() int32 { return blockEntityID(b.Kind) }

// newBlockEntity creates a just-placed block's empty entity with its kind's
// slots. A hopper starts off cooldown, which its field holds as the -1 a save
// carries.
func newBlockEntity(kind BlockEntityKind, x, y, z int32) *BlockEntity {
	be := &BlockEntity{
		Cooldown: -1,
		Kind:     kind,
		X:        x,
		Y:        y,
		Z:        z,
		Slots:    make([]protocol.ItemStack, slotCount(kind)),
	}
	if kind != KindHopper {
		be.Cooldown = 0
	}
	if IsCooking(kind) || kind == KindBrewingStand {
		be.SpeedMultiplier = 1
	}
	if kind == KindSpawner {
		// BaseSpawner's initial field values.
		be.Delay, be.MinSpawnDelay, be.MaxSpawnDelay = 20, 200, 800
		be.SpawnCount, be.MaxNearbyEntities = 4, 6
		be.RequiredPlayerRange, be.SpawnRange = 16, 4
	}
	if kind == KindSign || kind == KindHangingSign {
		// Both sides start empty and black; an empty colour would write one the
		// client cannot name.
		be.SignFront.Color, be.SignBack.Color = "black", "black"
	}
	if kind == KindPotentSulfur {
		be.Countdown = -1
	}
	if kind == KindBeacon {
		be.BeaconPrimary, be.BeaconSecondary = -1, -1
	}
	if kind == KindCommandBlock {
		// A fresh command block tracks output and has the same-tick gate on,
		// with no run yet.
		be.State = commandblock.New()
	}
	if kind == KindChiseledBookshelf {
		// No cell touched yet: DEFAULT_LAST_INTERACTED_SLOT.
		be.LastInteractedSlot = -1
	}
	return be
}

// posKey identifies a block entity by the world position of its block.
type posKey = [3]int32

// entityKey packs a block position into Chunk.entities' key: y in the high
// bits, the in-chunk z and x in the low byte.
func entityKey(x, y, z int32) int64 {
	return int64(y)<<8 | int64(z&15)<<4 | int64(x&15)
}

// setBlockEntity adds an entity to the chunk, replacing any at its position;
// called from placement (or opening a container that had none) and chunk load.
func (c *Chunk) setBlockEntity(b *BlockEntity) {
	if c.entities == nil {
		c.entities = make(map[int64]*BlockEntity)
	}
	key := entityKey(b.X, b.Y, b.Z)
	// A replacement goes to the end of the chunk's order: the old entity is
	// removed and the new one registered last (removeBlockEntity then
	// addBlockEntityTicker).
	if _, replaced := c.entities[key]; replaced {
		c.dropEntityOrder(key)
	}
	c.entityOrder = append(c.entityOrder, key)
	c.entities[key] = b
}

// dropEntityOrder removes a position from the chunk's order, leaving the map.
func (c *Chunk) dropEntityOrder(key int64) {
	for i, k := range c.entityOrder {
		if k == key {
			c.entityOrder = append(c.entityOrder[:i], c.entityOrder[i+1:]...)
			return
		}
	}
}

// blockEntity returns the chunk's entity at a position, or nil.
func (c *Chunk) blockEntity(x, y, z int32) *BlockEntity {
	return c.entities[entityKey(x, y, z)]
}

// carriedPos reads a block entity entry's position, which every carried entry
// has.
func carriedPos(entry nbt.Compound) (int32, int32, int32) {
	x, _ := nbt.Get[nbt.Int](entry, "x")
	y, _ := nbt.Get[nbt.Int](entry, "y")
	z, _ := nbt.Get[nbt.Int](entry, "z")
	return int32(x), int32(y), int32(z)
}

// setCarriedBlockEntity keeps an unmodelled block entity's compound, written
// back as it came by the next save; one per position, a new one replacing the
// old.
func (c *Chunk) setCarriedBlockEntity(entry nbt.Compound) {
	x, y, z := carriedPos(entry)
	key := entityKey(x, y, z)
	handle := c.carriedHandles[key]
	c.removeCarriedBlockEntity(x, y, z)
	// Loading new data into the same kind keeps the live entity's identity.
	if handle != nil && entry["id"] == nbt.String(handle.kind) {
		c.carriedHandles[key] = handle
	}
	c.carriedBlockEntities = nbt.List{Elem: nbt.TypeCompound, Items: append(c.carriedBlockEntities.Items, entry)}
	if isCarriedTicking(entry) {
		c.tickingCarried++
	}
}

// isCarriedTicking reports a trial spawner, vault, beehive or end gateway, the
// carried types the server ticks.
func isCarriedTicking(entry nbt.Compound) bool {
	id, _ := nbt.Get[nbt.String](entry, "id")
	return id == "minecraft:trial_spawner" || id == "minecraft:vault" || id == "minecraft:beehive" ||
		id == "minecraft:end_gateway"
}

// countTickingCarried recounts the carried trial spawners and vaults of the chunk.
func (c *Chunk) countTickingCarried() {
	c.tickingCarried = 0
	for _, item := range c.carriedBlockEntities.Items {
		if compound, ok := nbt.CompoundView(item); ok && isCarriedTicking(compound) {
			c.tickingCarried++
		}
	}
}

// removeCarriedBlockEntity drops the position's carried compound (its block
// having gone) and returns how many, usually none.
func (c *Chunk) removeCarriedBlockEntity(x, y, z int32) int {
	delete(c.carriedHandles, entityKey(x, y, z))
	delete(c.hiveTicks, [3]int32{x, y, z})
	kept := c.carriedBlockEntities.Items[:0]
	dropped := 0
	for _, item := range c.carriedBlockEntities.Items {
		compound, ok := nbt.CompoundView(item)
		if ok {
			cx, cy, cz := carriedPos(compound)
			if cx == x && cy == y && cz == z {
				dropped++
				if isCarriedTicking(compound) {
					c.tickingCarried--
				}
				continue
			}
		}
		kept = append(kept, item)
	}
	c.carriedBlockEntities.Items = kept
	if len(kept) == 0 {
		c.carriedBlockEntities = nbt.List{}
	}
	return dropped
}

// carriedBlockEntityAt returns the carried compound at the position, an
// unmodelled kind's data.
func (c *Chunk) carriedBlockEntityAt(x, y, z int32) (nbt.Compound, bool) {
	for _, item := range c.carriedBlockEntities.Items {
		compound, ok := nbt.CompoundView(item)
		if !ok {
			continue
		}
		cx, cy, cz := carriedPos(compound)
		if cx == x && cy == y && cz == z {
			return c.foldHiveTicks(compound), true
		}
	}
	return nil, false
}

// foldHiveTicks returns the compound with the hive's pending clock written
// into its bees, the compound itself when none is pending.
func (c *Chunk) foldHiveTicks(entry nbt.Compound) nbt.Compound {
	if len(c.hiveTicks) == 0 {
		return entry
	}
	x, y, z := carriedPos(entry)
	n := c.hiveTicks[[3]int32{x, y, z}]
	if n == 0 {
		return entry
	}
	list, _ := nbt.Get[nbt.List](entry, "bees")
	bees := make([]nbt.Tag, len(list.Items))
	for i, bee := range list.Items {
		occupant, ok := nbt.CompoundView(bee)
		if !ok {
			bees[i] = bee
			continue
		}
		var next nbt.CompoundTag
		if ordered, ok := bee.(*nbt.OrderedCompound); ok {
			next = ordered.Clone()
		} else {
			next = nbt.ShallowCopyCompound(bee.(nbt.CompoundTag))
		}
		ticks, _ := nbt.IntValue(occupant["ticks_in_hive"])
		next.Set("ticks_in_hive", nbt.Int(ticks+n))
		bees[i] = next
	}
	out := maps.Clone(entry)
	list.Items = bees
	out["bees"] = list
	return out
}

// carriedEntries lists the carried compounds with pending hive clocks folded
// in; the stored slice itself when there are none.
func (c *Chunk) carriedEntries() []nbt.Tag {
	items := c.carriedBlockEntities.Items
	if len(c.hiveTicks) == 0 {
		return items
	}
	out := slices.Clone(items)
	for i, item := range out {
		if entry, ok := nbt.CompoundView(item); ok {
			out[i] = c.foldHiveTicks(entry)
		}
	}
	return out
}

// removeBlockEntity removes and returns the chunk's entity at the position, nil
// if none.
func (c *Chunk) removeBlockEntity(x, y, z int32) *BlockEntity {
	key := entityKey(x, y, z)
	b, ok := c.entities[key]
	if !ok {
		return nil
	}
	delete(c.entities, key)
	c.dropEntityOrder(key)
	return b
}

// blockEntities lists the chunk's entities in insertion order, vanilla's ticker
// order and a deterministic save order so two saves give the same bytes.
func (c *Chunk) blockEntities() []*BlockEntity {
	return c.appendBlockEntities(nil)
}

// appendBlockEntities appends the entities in blockEntities' order to out,
// growing it once for all of them.
func (c *Chunk) appendBlockEntities(out []*BlockEntity) []*BlockEntity {
	out = slices.Grow(out, len(c.entityOrder))
	for _, key := range c.entityOrder {
		if b := c.entities[key]; b != nil {
			out = append(out, b)
		}
	}
	return out
}

// BlockEntity returns the entity at the position, nil if none or not loaded.
func (w *World) BlockEntity(x, y, z int32) *BlockEntity {
	pos := ChunkPos{X: x >> 4, Z: z >> 4}
	w.mu.RLock()
	defer w.mu.RUnlock()
	loaded, ok := w.loadedAt(pos)
	if !ok {
		return nil
	}
	return loaded.chunk.blockEntity(x, y, z)
}

// EnsureBlockEntity returns the entity at the position, creating an empty one
// of kind in a loaded chunk without one: what placement and opening a window
// need. An entity of another kind (a swapped container) is replaced. Creating
// one marks the chunk dirty so it is saved even if nothing else changes.
func (w *World) EnsureBlockEntity(x, y, z int32, kind BlockEntityKind) *BlockEntity {
	pos := ChunkPos{X: x >> 4, Z: z >> 4}
	w.mu.Lock()
	defer w.mu.Unlock()
	loaded, ok := w.chunks[pos]
	if !ok {
		return nil
	}
	existing := loaded.chunk.blockEntity(x, y, z)
	if existing != nil && existing.Kind == kind {
		return existing
	}
	if existing != nil {
		// A swapped block: the old entity goes and the new one is registered
		// last, as vanilla removes the ticker before adding the replacement's.
		// The old one is flagged removed so a UseLive turn holding it skips.
		w.removeBlockEntityOrder(existing)
		existing.removed.Store(true)
	}
	created := newBlockEntity(kind, x, y, z)
	if kind == KindCrafter {
		// vanilla: CrafterBlock.newBlockEntity
		created.Triggered = registry.StateBool(loaded.chunk.BlockAt(x&15, y, z&15), registry.PropTriggered)
	}
	loaded.chunk.removePistonRide(x, y, z)
	loaded.chunk.setBlockEntity(created)
	loaded.containerGen.Add(1)
	w.appendBlockEntityOrder(created)
	loaded.markDirty()
	return created
}

// EnsureBlockEntityForBlock gives the block now at the position the block
// entity vanilla gives every block of its type, when it has none: a fresh
// spawner, or a carried compound with the type's default data for a type this
// server does not model (the client draws banners, heads and the enchanting
// table's book only from one). The other modelled kinds are the callers' to
// create; a piston's is its moving block's.
// vanilla: LevelChunk.setBlockState (EntityBlock.newBlockEntity)
func (w *World) EnsureBlockEntityForBlock(x, y, z int32) {
	pos := ChunkPos{X: x >> 4, Z: z >> 4}
	w.mu.RLock()
	loaded, ok := w.chunks[pos]
	if !ok {
		w.mu.RUnlock()
		return
	}
	state := loaded.chunk.BlockAt(x&15, y, z&15)
	block := registry.BlockOf(state)
	id, has := registry.BlockEntityTypes[block.Name()]
	_, carried := loaded.chunk.carriedBlockEntityAt(x, y, z)
	_, ride := loaded.chunk.pistonRide(x, y, z)
	live := loaded.chunk.blockEntity(x, y, z) != nil || ride
	w.mu.RUnlock()
	if !has || carried || live {
		return
	}
	kind, _ := BlockEntityKindByName(id)
	switch {
	case kind == KindSpawner:
		w.EnsureBlockEntity(x, y, z, kind)
	case containerKind(kind) || kind == KindPiston:
	default:
		w.SetBlockEntityTag(x, y, z, id, freshCarriedTag(block, state))
	}
}

// blockEntityTicks reports whether a kind has a per-tick or per-load pass: the
// kinds Server.tickBlockEntities switches on, the sculk listeners and the
// catalysts. vanilla: BlockState.getTicker non-null.
// sculkBlockEntityKind reports whether the kind is kept in sculkBlockEntities.
func sculkBlockEntityKind(kind BlockEntityKind) bool {
	return kind == KindSculkCatalyst || listenerKind(kind)
}

func blockEntityTicks(kind BlockEntityKind) bool {
	switch kind {
	case KindBrewingStand, KindHopper, KindCrafter, KindConduit, KindJukebox,
		KindDaylightDetector, KindCreakingHeart, KindCampfire, KindBeacon,
		KindSpawner, KindSign, KindHangingSign, KindSculkCatalyst, KindPotentSulfur, KindBell:
		return true
	}
	return IsCooking(kind) || listenerKind(kind)
}

// appendBlockEntityOrder adds entities to blockEntityOrder and, for the kinds
// that tick, to tickingBlockEntities; call it under mu.
func (w *World) appendBlockEntityOrder(bes ...*BlockEntity) {
	for _, be := range bes {
		w.nextEntityTickOrder++
		be.tickOrder = w.nextEntityTickOrder
		w.blockEntityOrder = append(w.blockEntityOrder, be)
		if blockEntityTicks(be.Kind) {
			w.tickingBlockEntities = append(w.tickingBlockEntities, be)
		}
		if sculkBlockEntityKind(be.Kind) {
			w.sculkBlockEntities = append(w.sculkBlockEntities, be)
		}
	}
}

// removeBlockEntityOrder drops one entity from the world's tick order; call it
// under mu.
func (w *World) removeBlockEntityOrder(be *BlockEntity) {
	w.blockEntityOrder = removeOrdered(w.blockEntityOrder, be)
	if blockEntityTicks(be.Kind) {
		w.tickingBlockEntities = removeOrdered(w.tickingBlockEntities, be)
	}
	if sculkBlockEntityKind(be.Kind) {
		w.sculkBlockEntities = removeOrdered(w.sculkBlockEntities, be)
	}
}

func removeOrdered(list []*BlockEntity, be *BlockEntity) []*BlockEntity {
	if i := slices.Index(list, be); i >= 0 {
		return slices.Delete(list, i, i+1)
	}
	return list
}

// removeChunkBlockEntities drops an unloading chunk's entities from the tick
// order and flags each removed, so a UseLive turn holding an old pointer skips.
// Call it under mu; the flag is atomic, so no entity lock is taken.
func (w *World) removeChunkBlockEntities(c *Chunk) {
	bes := c.blockEntities()
	if len(bes) == 0 {
		return
	}
	gone := make(map[*BlockEntity]bool, len(bes))
	for _, be := range bes {
		gone[be] = true
		be.removed.Store(true)
	}
	// One pass over the world's order, not one per entity.
	w.blockEntityOrder = slices.DeleteFunc(w.blockEntityOrder, func(b *BlockEntity) bool { return gone[b] })
	w.tickingBlockEntities = slices.DeleteFunc(w.tickingBlockEntities, func(b *BlockEntity) bool { return gone[b] })
	w.sculkBlockEntities = slices.DeleteFunc(w.sculkBlockEntities, func(b *BlockEntity) bool { return gone[b] })
}

// RemoveBlockEntity removes and returns the entity at the position, its block
// being broken, so the caller can drop its contents. The carried compound goes
// too, and the chunk is marked dirty so neither returns. The removed flag is an
// atomic store, safe under the world's lock; later uses are refused.
func (w *World) RemoveBlockEntity(x, y, z int32) *BlockEntity {
	pos := ChunkPos{X: x >> 4, Z: z >> 4}
	w.mu.Lock()
	loaded, ok := w.chunks[pos]
	if !ok {
		w.mu.Unlock()
		return nil
	}
	b := loaded.chunk.removeBlockEntity(x, y, z)
	if b != nil {
		loaded.containerGen.Add(1)
	}
	ride := loaded.chunk.removePistonRide(x, y, z)
	carried := loaded.chunk.removeCarriedBlockEntity(x, y, z) > 0
	if carried {
		w.syncTickingChunk(pos, loaded.chunk)
	}
	if b != nil || ride || carried {
		loaded.markDirty()
	}
	if b != nil {
		w.removeBlockEntityOrder(b)
		b.removed.Store(true)
	}
	w.mu.Unlock()
	return b
}

// TouchEntity marks the entity's chunk dirty (Touch for contents), but only if
// be is still the world's entity at its position; a broken or unloaded one
// returns false. Call it inside Use: the check and the mark are one step, so a
// change under the same lock always rides the generation it marks and a release
// save that read the chunk earlier cannot drop it unwritten. The position also
// waits for the next physics tick's comparator wake.
func (w *World) TouchEntity(be *BlockEntity) bool {
	// Read the tick before mu so the two locks never nest.
	tick, _ := w.Time()
	pos := ChunkPos{X: be.X >> 4, Z: be.Z >> 4}
	w.mu.Lock()
	defer w.mu.Unlock()
	loaded, ok := w.loadedAt(pos)
	if !ok || loaded.chunk.blockEntity(be.X, be.Y, be.Z) != be {
		return false
	}
	loaded.markDirty()
	w.containerChanges = append(w.containerChanges, containerChange{pos: posKey{be.X, be.Y, be.Z}, tick: tick})
	return true
}

// SetBlockEntityTag gives the position's entity a command's data for its block.
// The entry is built here in chunk-format shape, so the tag alone never moves
// the entity. A modelled kind is stood up from it, replacing what was there,
// except the block's own entity, which takes the tag in place so an open window
// keeps reading it; an unmodelled kind is carried raw. It returns the kind and
// whether it is modelled (whether clients are told). An unloaded chunk takes
// nothing. A moving piston's entity must already exist from piston movement.
// vanilla: BlockInput.place
func (w *World) SetBlockEntityTag(x, y, z int32, entityType string, data nbt.CompoundTag) (BlockEntityKind, bool) {
	tag, _ := nbt.CompoundView(data)
	if kind, ok := BlockEntityKindByName(entityType); ok && IsSignKind(kind) {
		tag = w.ResolveSignTag(x, y, z, tag)
	}
	entry := make(nbt.Compound, len(tag)+4)
	maps.Copy(entry, tag)
	entry["id"] = nbt.String(entityType)
	if bees, ok := nbt.Get[nbt.List](entry, "bees"); ok && entityType == "minecraft:beehive" {
		entry["bees"] = qualifiedOccupants(bees)
	}
	entry["x"] = nbt.Int(x)
	entry["y"] = nbt.Int(y)
	entry["z"] = nbt.Int(z)
	pos := ChunkPos{X: x >> 4, Z: z >> 4}

	// Load into the block's own entity in place under its lock, the world lock
	// released first (entity before world; see mu). TouchEntity under that hold
	// confirms it is still the world's entity and marks the chunk. If it
	// vanished meanwhile, fall through to a fresh one, reset rather than
	// merged: unnamed fields take the new entity's defaults.
	w.mu.RLock()
	loaded, ok := w.chunks[pos]
	var existing *BlockEntity
	if ok {
		if be := loaded.chunk.blockEntity(x, y, z); be != nil && be.Kind.Name() == entityType {
			existing = be
		}
	}
	w.mu.RUnlock()
	if existing != nil {
		took := false
		existing.UseLive(func(b *BlockEntity) {
			b.loadFrom(entry)
			took = w.TouchEntity(b)
		})
		if took {
			return existing.Kind, true
		}
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	loaded, ok = w.chunks[pos]
	if !ok {
		return KindNone, false
	}
	// vanilla: MovingPistonBlock.newBlockEntity returns null; only piston
	// movement creates its entity, though commands can update an existing ride.
	if entityType == KindPiston.Name() {
		if r, ok := loaded.chunk.pistonRide(x, y, z); ok {
			r.loadFrom(entry)
			loaded.chunk.setPistonRide(r)
			loaded.markDirty()
			return KindPiston, true
		}
		return KindNone, false
	}
	if be, ok := decodeBlockEntity(entry); ok {
		// The replaced entity leaves the tick order and the new one ticks last,
		// as in EnsureBlockEntity.
		if old := loaded.chunk.blockEntity(x, y, z); old != nil {
			w.removeBlockEntityOrder(old)
			old.removed.Store(true)
		}
		loaded.chunk.removePistonRide(x, y, z)
		loaded.chunk.setBlockEntity(be)
		loaded.containerGen.Add(1)
		w.appendBlockEntityOrder(be)
		loaded.markDirty()
		return be.Kind, true
	}
	if BlockEntityTypeID(entityType) >= 0 {
		readCarried(loaded.chunk, entry)
		entry = asSaved(entry)
	}
	loaded.chunk.setCarriedBlockEntity(entry)
	w.syncTickingChunk(pos, loaded.chunk)
	loaded.markDirty()
	return KindNone, false
}

// BlockEntityTag returns the chunk's data for the position's entity: the
// modelled entity's compound (read under its lock) or an unmodelled kind's
// carried compound; false when neither exists or the chunk is not loaded.
func (w *World) BlockEntityTag(x, y, z int32) (nbt.Compound, bool) {
	tag, _, ok := w.savedBlockEntity(x, y, z)
	if !ok {
		return nil, false
	}
	return tag.Entries(), true
}

// BlockEntityData reads a block entity's saved compound without erasing its
// history, a carried one in the order its type's codec writes it.
func (w *World) BlockEntityData(x, y, z int32) (nbt.CompoundTag, bool) {
	tag, carried, ok := w.savedBlockEntity(x, y, z)
	if carried {
		return orderedCarried(tag.Entries()), true
	}
	return tag, ok
}

// savedBlockEntity is the position's saved compound; carried reports an
// unmodelled kind's.
func (w *World) savedBlockEntity(x, y, z int32) (tag nbt.CompoundTag, carried, ok bool) {
	pos := ChunkPos{X: x >> 4, Z: z >> 4}
	w.mu.RLock()
	loaded, ok := w.chunks[pos]
	if !ok {
		w.mu.RUnlock()
		return nil, false, false
	}
	be := loaded.chunk.blockEntity(x, y, z)
	if be == nil {
		if r, found := loaded.chunk.pistonRide(x, y, z); found {
			w.mu.RUnlock()
			tag := r.saveTag()
			// vanilla: PistonMovingBlockEntity.saveAdditional writes progressO.
			tag.Set("progress", nbt.Float(r.rideProgress))
			return tag, false, true
		}
		compound, found := loaded.chunk.carriedBlockEntityAt(x, y, z)
		w.mu.RUnlock()
		return compound, found, found
	}
	w.mu.RUnlock()
	// Read under the entity's lock with the world's released (entity before
	// world; see mu), so a tick holding this entity can mark its chunk without
	// waiting on this reader.
	var compound nbt.CompoundTag
	be.Use(func(b *BlockEntity) { compound = b.SaveTag() })
	return compound, false, true
}

// ItemComponents are what a ctrl-pick copies from a block entity onto its item:
// slots, name, the kind's keys collectImplicitComponents turns into components
// (banner patterns, a head's profile and note block sound, beehive bees, pot
// sherds, a sign's text and wax), and block_entity_data plus type for the rest.
// A nil field, empty string or false is absent.
type ItemComponents struct {
	Container       []protocol.ItemStack
	CustomName      *protocol.Text
	BannerPatterns  []protocol.BannerPatternLayer
	Profile         *protocol.Profile
	NoteBlockSound  string
	Bees            []protocol.Bee
	PotDecorations  *[4]protocol.ItemStack
	SignFront       *protocol.SignText
	SignBack        *protocol.SignText
	Waxed           bool
	Lock            nbt.CompoundTag
	ContainerLoot   protocol.ContainerLoot
	BlockEntityType int32
	BlockEntityData nbt.Compound
	// Extra are the entity's own components, by name: what the placed stack
	// carried that no kind reader consumed.
	Extra nbt.Compound
}

// ItemComponentsAt returns what addBlockDataToItem copies from the position's
// entity onto a picked stack; keys a component consumed do not also travel in
// block_entity_data.
// vanilla: BlockEntity.collectComponents, BlockEntity.saveCustomOnly
func (w *World) ItemComponentsAt(x, y, z int32) (ItemComponents, bool) {
	tag, ok := w.BlockEntityTag(x, y, z)
	if !ok {
		return ItemComponents{}, false
	}
	kind, _ := nbt.Get[nbt.String](tag, "id")
	typeID := BlockEntityTypeID(registry.Qualify(string(kind)))
	if typeID < 0 {
		return ItemComponents{}, false
	}
	components, drop := collectImplicitComponents(kindOfID(string(kind)), tag)
	drop["id"] = true
	drop["x"] = true
	drop["y"] = true
	drop["z"] = true
	drop["plugins"] = true
	// The components travel as the item's own, not in block_entity_data.
	// vanilla: BlockEntity.collectComponents
	drop["components"] = true
	if extra, ok := nbt.Get[nbt.Compound](tag, "components"); ok && len(extra) > 0 {
		components.Extra = extra
	}
	rest := make(nbt.Compound, len(tag))
	for name, value := range tag {
		if drop[name] {
			continue
		}
		rest[name] = value
	}
	if len(rest) > 0 {
		rest["id"] = nbt.String(kind)
		components.BlockEntityType = typeID
		components.BlockEntityData = rest
	}
	return components, true
}

// ImplicitComponentsOf is a block-entity tag's implicit components, its kind
// taken from the tag's "id"; block_entity_data is left unset.
func ImplicitComponentsOf(tag nbt.Compound) ItemComponents {
	kind, _ := nbt.Get[nbt.String](tag, "id")
	components, _ := collectImplicitComponents(kindOfID(string(kind)), tag)
	return components
}

// collectImplicitComponents turns a tag's keys into its kind's components and
// names the keys consumed so they stay out of block_entity_data. Slots and
// custom name are every kind's; each kind adds its own.
// vanilla: BlockEntity.collectImplicitComponents, the kinds' overrides
func collectImplicitComponents(kind BlockEntityKind, tag nbt.Compound) (ItemComponents, map[string]bool) {
	drop := make(map[string]bool)
	components := ItemComponents{}
	if containerKey := itemContainerKey(kind); containerKey != "" {
		if container := containerFromItemTag(containerKey, tag); container != nil {
			components.Container = container
			drop[containerKey] = true
		}
	}
	if name, key := customNameOf(tag); name != nil {
		components.CustomName = name
		drop[key] = true
	}
	// The lock leaves the tag as the minecraft:lock component.
	// vanilla: BaseContainerBlockEntity.collectImplicitComponents, BeaconBlockEntity.collectImplicitComponents
	if lockableKind(kind) {
		if lock, ok := nbt.Get[nbt.CompoundTag](tag, "lock"); ok {
			components.Lock = registry.LockTag(lock)
			drop["lock"] = true
		}
	}
	// vanilla: RandomizableContainerBlockEntity.collectImplicitComponents,
	// RandomizableContainerBlockEntity.removeComponentsFromTag
	if lootTableKind(kind) && kind != KindDecoratedPot {
		if table, ok := nbt.Get[nbt.String](tag, "LootTable"); ok && table != "" {
			seed, _ := nbt.LongValue(tag["LootTableSeed"])
			components.ContainerLoot = protocol.ContainerLoot{Table: string(table), Seed: seed}
		}
		drop["LootTable"] = true
		drop["LootTableSeed"] = true
	}
	switch kind {
	case KindBanner:
		if layers, ok := bannerPatternsFromTag(tag); ok {
			components.BannerPatterns = layers
			drop["patterns"] = true
		}
	case KindSkull:
		if profile, ok := profileFromTag(tag); ok {
			components.Profile = profile
			drop["profile"] = true
		}
		if sound, ok := nbt.Get[nbt.String](tag, "note_block_sound"); ok {
			components.NoteBlockSound = string(sound)
			drop["note_block_sound"] = true
		}
	case KindBeehive:
		if list, ok := nbt.Get[nbt.List](tag, "bees"); ok {
			if bees, ok := registry.BeesOf(list); ok {
				components.Bees = bees
				drop["bees"] = true
			}
		}
	case KindDecoratedPot:
		// "sherds" is consumed either way.
		sherds, _ := nbt.Get[nbt.Compound](tag, "sherds")
		components.PotDecorations = PotDecorationsPatch(sherds)
		drop["sherds"] = true
	case KindSign, KindHangingSign:
		// A side equal to the item's default, the empty one, is no patch;
		// the three keys leave the tag either way.
		// vanilla: SignBlockEntity.collectImplicitComponents,
		// SignBlockEntity.removeComponentsFromTag, PatchedDataComponentMap.set
		components.SignFront = signTextComponentOf(tag, "front_text")
		components.SignBack = signTextComponentOf(tag, "back_text")
		components.Waxed = nbt.BoolField(tag, "is_waxed")
		drop["front_text"] = true
		drop["back_text"] = true
		drop["is_waxed"] = true
	case KindCommandBlock:
		// The command block's flag and condition are runtime state, dropped
		// rather than copied.
		// vanilla: CommandBlockEntity.removeComponentsFromTag
		drop["conditionMet"] = true
		drop["powered"] = true
	}
	return components, drop
}

// signTextComponentOf reads a sign side as its item component, nil when it
// is absent, refused or empty black unlit text, the sign items' default.
// vanilla: SignText.EMPTY
func signTextComponentOf(tag nbt.Compound, key string) *protocol.SignText {
	side, ok := nbt.Get[nbt.Compound](tag, key)
	if !ok {
		return nil
	}
	text, ok := registry.SignTextOf(side)
	if !ok {
		return nil
	}
	empty := text.Filtered == nil && !text.Glowing && registry.DyeNames[text.Dye] == "black"
	for _, line := range text.Messages {
		if plain, ok := line.(nbt.String); !ok || plain != "" {
			empty = false
		}
	}
	if empty {
		return nil
	}
	return &text
}

// ApplySignComponents lays a placed sign item's text and wax on the sign, over
// any block_entity_data: a side the stack lacks is the item's default, empty.
// vanilla: SignBlockEntity.applyImplicitComponents
func (b *BlockEntity) ApplySignComponents(stack protocol.ItemStack) {
	b.SignFront = signTextFromStack(stack, true)
	b.SignBack = signTextFromStack(stack, false)
	b.Waxed = stack.Waxed()
}

// signTextFromStack is a stack's side, or the fresh empty one.
func signTextFromStack(stack protocol.ItemStack, front bool) SignText {
	if text, ok := stack.SignTextOf(front); ok {
		return decodeSignText(registry.SignTextTag(text).Entries())
	}
	return SignText{Color: "black"}
}

// carriedComponentKind reports a carried kind whose class applies implicit
// components, which a placement must stand up an entity for: banner (patterns,
// name), player head (profile, note block sound, name), beehive (bees).
// vanilla: BannerBlockEntity, SkullBlockEntity, BeehiveBlockEntity
func carriedComponentKind(kind BlockEntityKind) bool {
	switch kind {
	case KindBanner, KindSkull, KindBeehive:
		return true
	}
	return false
}

// PlaceBlockEntityComponents stands up the carried entity a banner, player head
// or beehive placement needs: the stack's block_entity_data first, then its
// kind's components over it (BlockItem.updateCustomBlockEntityTag, then
// applyComponentsFromItemStack). Each key a component feeds is replaced,
// cleared if the stack lacks it, so components win rather than merge. It
// returns the tag, or false (block untouched) for other kinds or a bare stack,
// so ordinary placements make no empty entity.
// vanilla: BlockItem.updateBlockEntityComponents
func (w *World) PlaceBlockEntityComponents(x, y, z int32, kind BlockEntityKind, stack protocol.ItemStack) (nbt.Compound, bool) {
	if !carriedComponentKind(kind) {
		return nil, false
	}
	tag := nbt.Compound{}
	if typeID, data, ok := stack.BlockEntityDataOf(); ok && typeID == BlockEntityTypeID(kind.Name()) {
		maps.Copy(tag, data)
	}
	// A missing component clears its key rather than keeping the data's.
	// vanilla: BlockEntity.applyImplicitComponents
	for _, key := range implicitComponentKeys(kind) {
		delete(tag, key)
	}
	if components, ok := implicitComponentsTag(kind, stack); ok {
		maps.Copy(tag, components)
	}
	if len(tag) == 0 && len(leftoverComponents(kind, stack)) == 0 {
		return nil, false
	}
	w.SetBlockEntityTag(x, y, z, kind.Name(), tag)
	return tag, true
}

// implicitComponentKeys are the keys a kind's class reads from item components,
// which a placement replaces wholesale instead of merging over
// block_entity_data.
// vanilla: BannerBlockEntity.applyImplicitComponents, SkullBlockEntity's,
// BeehiveBlockEntity's
func implicitComponentKeys(kind BlockEntityKind) []string {
	switch kind {
	case KindBanner:
		return []string{"patterns", "CustomName"}
	case KindSkull:
		return []string{"profile", "note_block_sound", "custom_name"}
	case KindBeehive:
		return []string{"bees"}
	}
	return nil
}

// implicitComponentsTag writes the keys a stack's implicit components carry,
// inverting collectImplicitComponents: banner "patterns" and "CustomName", head
// "profile", "note_block_sound" and "custom_name", beehive "bees". False for a
// kind collecting none or a stack carrying none of them.
// vanilla: BlockEntity.applyImplicitComponents, the kinds' overrides
func implicitComponentsTag(kind BlockEntityKind, stack protocol.ItemStack) (nbt.Compound, bool) {
	tag := nbt.Compound{}
	switch kind {
	case KindBanner:
		if layers, ok := stack.BannerPatternsOf(); ok {
			tag["patterns"] = registry.BannerPatternsTag(layers)
		}
		if stack.CustomName != nil {
			tag["CustomName"] = protocol.TextTag(*stack.CustomName)
		}
	case KindSkull:
		if profile, ok := stack.ProfileTag(); ok {
			tag["profile"] = profile
		}
		if sound, ok := stack.NoteBlockSoundOf(); ok {
			tag["note_block_sound"] = nbt.String(sound)
		}
		if stack.CustomName != nil {
			tag["custom_name"] = protocol.TextTag(*stack.CustomName)
		}
	case KindBeehive:
		if bees, ok := stack.BeesOf(); ok {
			tag["bees"] = registry.BeesTag(bees)
		}
	}
	if len(tag) == 0 {
		return nil, false
	}
	return tag, true
}

// bannerPatternsFromTag reads a banner's "patterns" as banner_patterns layers.
func bannerPatternsFromTag(tag nbt.Compound) ([]protocol.BannerPatternLayer, bool) {
	patterns, ok := nbt.Get[nbt.List](tag, "patterns")
	if !ok {
		return nil, false
	}
	return registry.BannerPatternsOf(patterns)
}

// profileFromTag reads a head's "profile" (a name string, or a compound of
// optional name, optional four-int UUID and properties) as a profile component;
// name and UUID together are the full shape, either alone the partial one.
// vanilla: ResolvableProfile.CODEC, ResolvableProfile.Partial.CODEC, ExtraCodecs.PROPERTY_MAP
func profileFromTag(tag nbt.Compound) (*protocol.Profile, bool) {
	if name, ok := nbt.Get[nbt.String](tag, "profile"); ok {
		return &protocol.Profile{Name: string(name), HasName: true}, true
	}
	raw, ok := nbt.Get[nbt.Compound](tag, "profile")
	if !ok {
		return nil, false
	}
	profile := &protocol.Profile{}
	if name, ok := nbt.Get[nbt.String](raw, "name"); ok {
		profile.Name = string(name)
		profile.HasName = true
	}
	if ids, ok := nbt.Get[nbt.IntArray](raw, "id"); ok {
		if id, ok := nbt.UUIDFromIntArray(ids); ok {
			profile.UUID = id
			profile.HasUUID = true
		}
	}
	if properties, ok := nbt.Get[nbt.List](raw, "properties"); ok {
		for _, item := range properties.Items {
			entry, ok := nbt.CompoundView(item)
			if !ok {
				continue
			}
			name, _ := nbt.Get[nbt.String](entry, "name")
			value, _ := nbt.Get[nbt.String](entry, "value")
			property := protocol.ProfileProperty{Name: string(name), Value: string(value)}
			if signature, ok := nbt.Get[nbt.String](entry, "signature"); ok {
				property.Signature = string(signature)
				property.Signed = true
			}
			profile.Properties = append(profile.Properties, property)
		}
	}
	// The skin patch fields sit beside name and UUID; an unknown model reads as
	// absent.
	// vanilla: PlayerSkin.Patch.MAP_CODEC
	if texture, ok := nbt.Get[nbt.String](raw, "texture"); ok {
		profile.Skin.Body = string(texture)
	}
	if cape, ok := nbt.Get[nbt.String](raw, "cape"); ok {
		profile.Skin.Cape = string(cape)
	}
	if elytra, ok := nbt.Get[nbt.String](raw, "elytra"); ok {
		profile.Skin.Elytra = string(elytra)
	}
	if model, ok := nbt.Get[nbt.String](raw, "model"); ok && (model == "slim" || model == "wide") {
		profile.Skin.Model = string(model)
	}
	return profile, true
}

// itemContainerKey is the tag key a kind's slots are collected from, "" for
// none: "Items" for containers, campfire, shelf and chiseled bookshelf, "item"
// for the decorated pot. A brushable block's "item" is its looted stack, not a
// container.
// vanilla: BaseContainerBlockEntity.collectImplicitComponents,
// DecoratedPotBlockEntity.collectImplicitComponents
func itemContainerKey(kind BlockEntityKind) string {
	switch kind {
	case KindChest, KindTrappedChest, KindBarrel, KindShulkerBox, KindDispenser,
		KindDropper, KindHopper, KindFurnace, KindBlastFurnace, KindSmoker,
		KindBrewingStand, KindCrafter, KindShelf, KindChiseledBookshelf,
		KindCampfire:
		return "Items"
	case KindDecoratedPot:
		return "item"
	}
	return ""
}

// containerFromItemTag reads the slots under key ("Items" or the pot's single
// "item") as container slots.
func containerFromItemTag(key string, tag nbt.Compound) []protocol.ItemStack {
	switch key {
	case "":
		return nil
	case "Items":
		return itemsFromTag(tag)
	default:
		if item, ok := nbt.Get[nbt.Compound](tag, "item"); ok {
			if stack, ok := registry.StoredStack(item); ok && !stack.Empty() {
				return []protocol.ItemStack{stack}
			}
		}
		return nil
	}
}

// itemsFromTag reads an "Items" list, placed by each entry's "Slot", trimmed of
// trailing empties.
func itemsFromTag(tag nbt.Compound) []protocol.ItemStack {
	items, ok := nbt.Get[nbt.List](tag, "Items")
	if !ok {
		return nil
	}
	var slots []protocol.ItemStack
	for _, entry := range items.Items {
		item, ok := nbt.CompoundView(entry)
		if !ok {
			continue
		}
		slot, ok := nbt.IntValue(item["Slot"])
		if !ok {
			continue
		}
		stack, ok := registry.StoredStack(item)
		if !ok {
			continue
		}
		for len(slots) <= int(slot) {
			slots = append(slots, protocol.ItemStack{})
		}
		slots[slot] = stack
	}
	for len(slots) > 0 && slots[len(slots)-1].Empty() {
		slots = slots[:len(slots)-1]
	}
	return slots
}

// customNameOf reads a tag's custom name ("CustomName" for containers,
// "custom_name" for skulls) and returns the key read so the caller can drop it.
// vanilla: BlockEntity.collectImplicitComponents
func customNameOf(tag nbt.Compound) (*protocol.Text, string) {
	for _, key := range []string{"CustomName", "custom_name"} {
		if value, ok := tag[key]; ok {
			if name, ok := protocol.StyledTextOf(value); ok {
				return &name, key
			}
		}
	}
	return nil, ""
}

// BlockEntityTypeID is a kind's wire type number for the block entity data
// packet, -1 if the client's registry lacks it. It covers the whole registry,
// since the packet may name a carried type.
func BlockEntityTypeID(kind string) int32 {
	if id, ok := protocol.BlockEntityTypeID(kind); ok {
		return id
	}
	return -1
}

// TickingBlockEntities returns every live block entity of a kind that ticks
// (blockEntityTicks) in loaded chunks, in world insertion order (Level.blockEntityTickers' single
// list), so a chain across a chunk border ticks in placement order rather than
// map order. Locks are not held; the tick takes each one.
func (w *World) TickingBlockEntities() []*BlockEntity {
	return w.AppendTickingBlockEntities(nil)
}

// AppendTickingBlockEntities appends TickingBlockEntities' list to dst.
func (w *World) AppendTickingBlockEntities(dst []*BlockEntity) []*BlockEntity {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return append(dst, w.tickingBlockEntities...)
}

// BlockEntitiesOfKind returns every live block entity of kind in loaded
// chunks, in insertion order. Locks are not held.
func (w *World) BlockEntitiesOfKind(kind BlockEntityKind) []*BlockEntity {
	w.mu.RLock()
	defer w.mu.RUnlock()
	var out []*BlockEntity
	for _, be := range w.blockEntityOrder {
		if be.Kind == kind {
			out = append(out, be)
		}
	}
	return out
}

// AppendChunkBlockEntities appends the block entities of the given kinds in
// one loaded chunk to dst, in the chunk's insertion order; an unloaded chunk
// adds none. Locks are not held.
func (w *World) AppendChunkBlockEntities(dst []*BlockEntity, pos ChunkPos, kinds ...BlockEntityKind) []*BlockEntity {
	w.mu.RLock()
	defer w.mu.RUnlock()
	loaded, ok := w.loadedAt(pos)
	if !ok {
		return dst
	}
	c := loaded.chunk
	for _, key := range c.entityOrder {
		if b := c.entities[key]; b != nil && slices.Contains(kinds, b.Kind) {
			dst = append(dst, b)
		}
	}
	return dst
}

// SculkCatalystEntities returns the catalysts' entities in insertion order.
func (w *World) SculkCatalystEntities() []*BlockEntity {
	w.mu.RLock()
	defer w.mu.RUnlock()
	var out []*BlockEntity
	for _, be := range w.sculkBlockEntities {
		if be.Kind == KindSculkCatalyst {
			out = append(out, be)
		}
	}
	return out
}

// storedStack reads a block entity's stack, allowing safe book partial values.
// A stack with unmodelled components comes back whole for the caller to keep.
func storedStack(entry nbt.Compound) (stack protocol.ItemStack, stored nbt.Compound, ok bool) {
	if stack, ok := registry.StoredContainerStack(entry); ok {
		return stack, nil, true
	}
	return protocol.ItemStack{}, entry, true
}

// decodeBlockEntity reads one "block_entities" entry, false for an unmodelled
// kind, whose compound the caller carries.
func decodeBlockEntity(entry nbt.Compound) (*BlockEntity, bool) {
	id, _ := nbt.Get[nbt.String](entry, "id")
	kind, _ := BlockEntityKindByName(string(id))
	if !containerKind(kind) {
		return nil, false
	}
	x, y, z := carriedPos(entry)
	be := newBlockEntity(kind, x, y, z)
	be.loadFrom(entry)
	return be, true
}

// loadFrom fills the entity from a chunk-format entry, for chunk loads and
// command tags alike: what the entry says stands, what it omits takes the fresh
// entity's values, so a tag naming only a cooldown resets the rest. The fields
// are those codec declares. Called with the entity's lock held.
func (b *BlockEntity) loadFrom(entry nbt.Compound) {
	b.Wake()
	b.inventoryVersion.Add(1)
	b.HopperTargets = nil
	b.Slots = make([]protocol.ItemStack, slotCount(b.Kind))
	b.filled = 0
	b.Stored = nil
	// The runtime state no save carries starts over.
	b.BrewingIngredient, b.BrewingBottles = 0, nil
	b.TickedGameTime, b.Openers = 0, 0
	b.hopperSlept = false
	b.ConduitActive, b.ConduitNextAmbient, b.CreakingTicker = false, 0, 0
	b.Bell = BellState{}
	b.CreakingEmitter, b.CreakingExisted, b.CreakingOutput = 0, 0, 0
	b.CreakingTarget = [3]float64{}
	c := beCodec{load: true, in: entry}
	b.codec(&c)
}

// containerKind reports a modelled kind, one a chunk's block_entities may carry
// that the world takes over from vanilla's compounds.
func containerKind(kind BlockEntityKind) bool {
	switch kind {
	case KindChest, KindTrappedChest, KindFurnace, KindBlastFurnace, KindSmoker,
		KindBrewingStand, KindCrafter, KindBarrel, KindShulkerBox, KindEnderChest,
		KindDispenser, KindDropper, KindHopper, KindComparator,
		KindSpawner, KindCommandBlock, KindBell, KindSign, KindHangingSign,
		KindJukebox, KindDaylightDetector, KindBeacon, KindDecoratedPot, KindShelf,
		KindChiseledBookshelf, KindConduit, KindCreakingHeart, KindLectern,
		KindCampfire, KindSculkSensor, KindCalibratedSculkSensor, KindSculkShrieker,
		KindSculkCatalyst, KindPotentSulfur, KindCopperGolemStatue, KindEnchantingTable:
		return true
	}
	return false
}

// lootTableKind reports a RandomizableContainerBlockEntity kind: chests,
// barrel, shulker box, crafter, hopper, dispenser, dropper, decorated pot.
// Furnaces and brewing stands hold their own items, and a table on one is
// dropped.
// vanilla: RandomizableContainerBlockEntity
func lootTableKind(kind BlockEntityKind) bool {
	switch kind {
	case KindChest, KindTrappedChest, KindBarrel, KindShulkerBox,
		KindCrafter, KindHopper, KindDispenser, KindDropper, KindDecoratedPot:
		return true
	}
	return false
}

// lockableKind reports a BaseContainerBlockEntity kind, or the beacon, which
// keeps the same lock: those whose menu a lock gates and whose save carries
// one. The ender chest, jukebox, pot and shelf are not BaseContainer,
// so a lock on one is dropped.
// vanilla: BaseContainerBlockEntity and its subclasses
func lockableKind(kind BlockEntityKind) bool {
	switch kind {
	case KindChest, KindTrappedChest, KindFurnace, KindBlastFurnace,
		KindSmoker, KindBrewingStand, KindCrafter, KindBarrel, KindShulkerBox,
		KindDispenser, KindDropper, KindHopper, KindBeacon:
		return true
	}
	return false
}

// decodeSignText reads one side: four messages, colour, glow. A side the codec
// refuses (other than four messages, a message that is no component, a colour
// that is no dye, a glow that is no number) gives a fresh empty side; a
// non-plain line (styled or translated) is kept raw so a save writes it back.
// vanilla: SignText.DIRECT_CODEC, SignBlockEntity.loadAdditional
func decodeSignText(entry nbt.Compound) SignText {
	fresh := SignText{Color: "black"}
	text := fresh
	messages, ok := nbt.Get[nbt.List](entry, "messages")
	if !ok || len(messages.Items) != 4 {
		return fresh
	}
	for i, item := range messages.Items {
		item = protocol.CleanStyle(nbt.ListElement(item))
		if !componentShaped(item) {
			return fresh
		}
		if line, bare := signLine(item); bare {
			text.Messages[i] = line
			continue
		}
		text.raw[i] = item
		text.Messages[i], _ = signLiteral(item)
	}
	// A malformed optional list falls back to the raw messages, without
	// refusing the side. vanilla: SignText.CODEC lenientOptionalFieldOf
	if filtered, ok := nbt.Get[nbt.List](entry, "filtered_messages"); ok && len(filtered.Items) == 4 {
		var lines [4]nbt.Tag
		valid := true
		for i, item := range filtered.Items {
			item = protocol.CleanStyle(nbt.ListElement(item))
			if !componentShaped(item) {
				valid = false
				break
			}
			if line, bare := signLine(item); bare {
				item = nbt.String(line)
			}
			lines[i] = item
		}
		if valid {
			text.filtered = &lines
		}
	}
	if tag, ok := entry["color"]; ok {
		color, ok := tag.(nbt.String)
		if !ok || !slices.Contains(registry.DyeNames, string(color)) {
			return fresh
		}
		text.Color = string(color)
	}
	if tag, ok := entry["has_glowing_text"]; ok {
		// Codec.BOOL reads any number, true when not nought.
		// vanilla: NbtOps.getBooleanValue
		if _, ok := nbt.LongValue(tag); !ok {
			return fresh
		}
		text.Glowing = nbt.DoubleFieldOr(entry, "has_glowing_text", 0) != 0
	}
	return text
}

// componentShaped reports whether a tag has a text component's shape: a
// string, a non-empty list of components, or a compound (one holding only an
// "" key is a list element's wrapper, read through). A compound's fields are
// not checked further.
// vanilla: ComponentSerialization.createCodec
func componentShaped(tag nbt.Tag) bool {
	switch v := tag.(type) {
	case nbt.String:
		return true
	case nbt.List:
		for _, item := range v.Items {
			if !componentShaped(item) {
				return false
			}
		}
		return len(v.Items) > 0
	case nbt.CompoundTag:
		if inner, ok := v.Entries()[""]; ok && len(v.Entries()) == 1 {
			return componentShaped(inner)
		}
		return true
	}
	return false
}

// encode writes one side: four lines plus colour and glow, the last two always
// even at defaults. A side with a raw line writes every line as a compound,
// since an NBT list has one element type; a line that is no compound (a
// component in list form) goes in the "" wrapper. Call it inside Use.
// vanilla: SignText.DIRECT_CODEC
func (t SignText) encode() nbt.CompoundTag {
	styled := false
	for _, raw := range t.raw {
		if raw != nil {
			styled = true
			break
		}
	}
	elem := nbt.TypeString
	if styled {
		elem = nbt.TypeCompound
	}
	lines := make([]nbt.Tag, 4)
	for i, m := range t.Messages {
		_, compound := t.raw[i].(nbt.CompoundTag)
		switch {
		case compound:
			lines[i] = t.raw[i]
		case t.raw[i] != nil:
			lines[i] = nbt.Compound{"": t.raw[i]}
		case styled:
			lines[i] = nbt.Compound{"": nbt.String(m)}
		default:
			lines[i] = nbt.String(m)
		}
	}
	var glow nbt.Byte
	if t.Glowing {
		glow = 1
	}
	// vanilla: SignText.CODEC
	tag := nbt.NewOrderedCompound(4)
	tag.Set("messages", nbt.List{Elem: elem, Items: lines})
	if t.filtered != nil {
		var messages [4]nbt.Tag
		for i, line := range lines {
			messages[i] = nbt.ListElement(line)
		}
		// vanilla: SignText.filteredMessagesForSerialization
		if !reflect.DeepEqual(messages, *t.filtered) {
			tag.Set("filtered_messages", nbt.NewList(t.filtered[:]))
		}
	}
	tag.Set("color", nbt.String(t.Color))
	tag.Set("has_glowing_text", glow)
	return tag
}

// SpawnEntityID is the "id" in SpawnData's entity compound, empty if none. Call
// it inside Use.
func (b *BlockEntity) SpawnEntityID() string {
	if b.SpawnData == nil {
		return ""
	}
	entity, _ := nbt.Get[nbt.Compound](b.SpawnData.Entries(), "entity")
	id, _ := nbt.Get[nbt.String](entity, "id")
	return string(id)
}

// SetSpawnEntityID writes the id into the next spawn's entity compound,
// creating the compound if needed. Call it inside Use.
// vanilla: BaseSpawner.setEntityId
func (b *BlockEntity) SetSpawnEntityID(id string) {
	if b.SpawnData == nil {
		b.SpawnData = &nbt.OrderedCompound{}
	}
	if entity, ok := nbt.Get[nbt.CompoundTag](b.SpawnData.Entries(), "entity"); ok {
		entity.Set("id", nbt.String(id))
		return
	}
	entity := &nbt.OrderedCompound{}
	entity.Set("id", nbt.String(id))
	b.SpawnData.Set("entity", entity)
}

// RollSpawnPotential puts a weighted pick of SpawnPotentials in SpawnData (intn
// is the level random's nextInt) and reports whether it picked. With no
// potentials or all-zero weights nothing is drawn and the data stays; load
// defaults only an omitted list to one entry. Call it inside Use.
// vanilla: WeightedList.getRandom, BaseSpawner.load
func (b *BlockEntity) RollSpawnPotential(intn func(int) int) bool {
	total := 0
	for _, item := range b.SpawnPotentials.Items {
		total += spawnPotentialWeight(item)
	}
	if total <= 0 {
		return false
	}
	pick := intn(total)
	for _, item := range b.SpawnPotentials.Items {
		pick -= spawnPotentialWeight(item)
		if pick < 0 {
			if data, ok := nbt.Get[nbt.CompoundTag](item.(nbt.CompoundTag).Entries(), "data"); ok {
				b.SpawnData = data
			}
			return true
		}
	}
	return false
}

// spawnPotentialWeight is an entry's weight, 0 for a non-compound or no weight.
func spawnPotentialWeight(item nbt.Tag) int {
	entry, ok := item.(nbt.CompoundTag)
	if !ok {
		return 0
	}
	weight, _ := nbt.Get[nbt.Int](entry.Entries(), "weight")
	return max(int(weight), 0)
}

// EncodeNBT builds the entity's chunk-format compound: id, position and the
// fields codec declares. It is one "block_entities" entry, and a packet sends
// it minus the position.
//
// It reads unlocked: call it inside Use.
func (b *BlockEntity) EncodeNBT() nbt.Compound { return b.SaveTag().Entries() }

// SaveTag builds the saved compound, retaining the writer's insertion history.
// It reads unlocked: call it inside Use.
// vanilla: BlockEntity.saveWithFullMetadata
func (b *BlockEntity) SaveTag() *nbt.OrderedCompound {
	tag := nbt.NewOrderedCompound(8)
	c := beCodec{out: tag}
	b.codec(&c)
	if b.Kind == KindCopperGolemStatue && len(b.Components) > 0 {
		merged := nbt.Compound{}
		if named, ok := nbt.Get[nbt.Compound](tag.Entries(), "components"); ok {
			maps.Copy(merged, named)
		}
		maps.Copy(merged, nbt.Copy(b.Components).(nbt.Compound))
		tag.Set("components", merged)
	}
	if _, ok := tag.Entries()["components"]; !ok {
		tag.Set("components", nbt.Compound{})
	}
	tag.Set("id", nbt.String(b.Kind.Name()))
	tag.Set("x", nbt.Int(b.X))
	tag.Set("y", nbt.Int(b.Y))
	tag.Set("z", nbt.Int(b.Z))
	return tag
}

// NewSpawnerState is a spawner's state held outside any chunk, for an entity
// that carries a BaseSpawner; it has BaseSpawner's initial values.
func NewSpawnerState() *BlockEntity { return newBlockEntity(KindSpawner, 0, 0, 0) }

// LoadSpawnerTag reads the spawner fields of an entity's compound.
// vanilla: BaseSpawner.load
func (b *BlockEntity) LoadSpawnerTag(tag nbt.Compound) {
	b.Use(func(b *BlockEntity) { b.loadFrom(tag) })
}

// SaveSpawnerTo writes BaseSpawner fields into an entity's compound.
func (b *BlockEntity) SaveSpawnerTo(tag nbt.CompoundTag) {
	b.Use(func(b *BlockEntity) { b.codec(&beCodec{out: tag}) })
}

// SpawnerTag is the spawner fields an entity's compound carries.
// vanilla: BaseSpawner.save
func (b *BlockEntity) SpawnerTag() nbt.Compound {
	var c nbt.Compound
	b.Use(func(b *BlockEntity) { c = b.EncodeNBT() })
	for _, k := range []string{"id", "x", "y", "z", "components"} {
		delete(c, k)
	}
	return c
}

// UpdateTag is the compound Block Entity Data or a Chunk Data entry carries:
// getUpdateTag's saveCustomOnly, so no id or position. The spawner omits its
// potentials, the campfire its cooking times. It reads unlocked: call it inside
// Use.
// vanilla: SpawnerBlockEntity.getUpdateTag, CampfireBlockEntity.getUpdateTag
func (b *BlockEntity) UpdateTag() nbt.Compound {
	c := nbt.Compound{}
	b.codec(&beCodec{out: c, updateTag: true})
	// vanilla: BlockEntity.saveCustomOnly leaves the components out
	delete(c, "components")
	if b.Kind == KindSpawner {
		delete(c, "SpawnPotentials")
	}
	if b.Kind == KindCampfire {
		delete(c, "CookingTimes")
		delete(c, "CookingTotalTimes")
	}
	return c
}

// putBlockEntities writes the chunk's block entity list: a count, then per
// entity its position (X and Z section-relative nibbles, Y world height),
// client registry type and update tag. Modelled and carried entities are all
// listed; an empty update tag (most containers) is written as a single end
// byte, so contents never travel.
//
// own was collected under the world's read lock and carried snapshotted under
// it, with active naming carried trial spawners whose block state is ACTIVE;
// each modelled entity is read under its own lock, never taken with the world's
// held (see World.mu).
// vanilla: BlockEntity.getUpdateTag
func putBlockEntities(b *protocol.Buffer, own []*BlockEntity, rides []pistonRide, carried []nbt.Tag, active map[posKey]bool) {
	type entry struct {
		packed uint8
		y      int16
		typeID int32
		tag    nbt.Compound
	}
	// Filter before writing the count, so a type the client's registry cannot
	// number is left out of both.
	var entryBuf [16]entry
	entries := entryBuf[:0]
	if n := len(own) + len(rides) + len(carried); n > len(entryBuf) {
		entries = make([]entry, 0, n)
	}
	for _, be := range own {
		id := blockEntityID(be.Kind)
		if id < 0 {
			continue
		}
		e := entry{packed: uint8(be.X&0xF)<<4 | uint8(be.Z&0xF), y: int16(be.Y), typeID: id}
		if registry.UpdateTagBlockEntityTypes[be.Kind.Name()] {
			be.Use(func(b *BlockEntity) { e.tag = b.UpdateTag() })
		}
		entries = append(entries, e)
	}
	for i := range rides {
		r := &rides[i]
		entries = append(entries, entry{
			packed: uint8(r.x&0xF)<<4 | uint8(r.z&0xF),
			y:      int16(r.y),
			typeID: protocol.BlockEntityPiston,
			tag:    r.updateTag(),
		})
	}
	for _, item := range carried {
		compound, ok := nbt.CompoundView(item)
		if !ok {
			continue
		}
		x, y, z, ok := carriedPosInTag(compound)
		if !ok {
			continue
		}
		id, _ := nbt.Get[nbt.String](compound, "id")
		typeID := BlockEntityTypeID(registry.Qualify(string(id)))
		if typeID < 0 {
			continue
		}
		e := entry{packed: uint8(x&0xF)<<4 | uint8(z&0xF), y: int16(y), typeID: typeID}
		e.tag = CarriedUpdateTag(string(id), compound, active[posKey{x, y, z}])
		entries = append(entries, e)
	}

	b.PutVarInt(int32(len(entries)))
	for _, e := range entries {
		b.PutU8(e.packed)
		b.PutI16(e.y)
		b.PutVarInt(e.typeID)
		putUpdateTag(b, e.tag)
	}
}

// CarriedUpdateTag builds a carried entity's update tag per its class's
// getUpdateTag: usually the stored compound minus the chunk format's keys, but
// chosen fields for the vault, trial spawner and brushable block, and empty for
// a type without the override. active is whether a trial spawner's block is
// ACTIVE, read only by that type.
func CarriedUpdateTag(kind string, stored nbt.Compound, active bool) nbt.Compound {
	tag := nbt.Compound{}
	if !registry.UpdateTagBlockEntityTypes[kind] {
		return tag
	}
	switch k := kindOfID(kind); k {
	case KindBanner:
		// The banner's tag keeps components, an item component's own data.
		copyTagExcept(tag, stored, "id", "x", "y", "z", "keepPacked")
	case KindVault:
		copyTagKey(tag, stored, "shared_data")
	case KindTrialSpawner:
		copyTagKey(tag, stored, "spawn_data")
		// next_mob_spawns_at is sent while the block is ACTIVE regardless of
		// value: it can stay non-zero after the spawner goes inactive.
		if active {
			copyTagKey(tag, stored, "next_mob_spawns_at")
		}
	case KindBrushable:
		copyTagKey(tag, stored, "hit_direction")
		copyTagKey(tag, stored, "item")
	default:
		copyTagExcept(tag, stored, "id", "x", "y", "z", "keepPacked", "components")
	}
	delete(tag, "plugins")
	return tag
}

// copyTagExcept copies every key of src into dst but the named ones.
func copyTagExcept(dst, src nbt.Compound, drop ...string) {
	maps.Copy(dst, src)
	for _, name := range drop {
		delete(dst, name)
	}
}

// copyTagKey copies one key of src into dst when it is there.
func copyTagKey(dst, src nbt.Compound, name string) {
	if value, ok := src[name]; ok {
		dst[name] = value
	}
}

// TrialSpawnerActive reports whether the block state is a trial spawner in the active state.
func TrialSpawnerActive(state uint16) bool {
	return registry.BlockOf(state) == registry.BlockTrialSpawner &&
		registry.StateValue(state, registry.PropTrialState) == "active"
}

// trialSpawnerBlock reads a trial spawner's state and its "ominous" property;
// the map is nil for other blocks.
func trialSpawnerBlock(state uint16) (string, map[string]string) {
	if registry.BlockOf(state) != registry.BlockTrialSpawner {
		return "", nil
	}
	return registry.StateValue(state, registry.PropTrialState),
		map[string]string{"ominous": registry.StateValue(state, registry.PropOminous)}
}

// activeTrialSpawners returns the carried trial spawners in an ACTIVE block:
// next_mob_spawns_at is sent only then, and the state is in the block, so the
// caller holds the world's read lock.
func (c *Chunk) activeTrialSpawners(carried []nbt.Tag) map[posKey]bool {
	var active map[posKey]bool
	for _, item := range carried {
		compound, ok := nbt.CompoundView(item)
		if !ok {
			continue
		}
		id, _ := nbt.Get[nbt.String](compound, "id")
		if id != "minecraft:trial_spawner" {
			continue
		}
		x, y, z, ok := carriedPosInTag(compound)
		if !ok {
			continue
		}
		if TrialSpawnerActive(c.BlockAt(x&0xF, y, z&0xF)) {
			if active == nil {
				active = make(map[posKey]bool)
			}
			active[posKey{x, y, z}] = true
		}
	}
	return active
}

// carriedPosInTag reads a carried compound's position, false unless all of x, y
// and z are present (vanilla would not list it).
func carriedPosInTag(entry nbt.Compound) (int32, int32, int32, bool) {
	x, xok := nbt.Get[nbt.Int](entry, "x")
	y, yok := nbt.Get[nbt.Int](entry, "y")
	z, zok := nbt.Get[nbt.Int](entry, "z")
	return int32(x), int32(y), int32(z), xok && yok && zok
}

// putUpdateTag writes an update tag, or the single end byte for an empty one.
func putUpdateTag(b *protocol.Buffer, tag nbt.Compound) {
	if len(tag) == 0 {
		b.PutU8(0)
		return
	}
	data, err := nbt.EncodeNetwork(tag)
	if err != nil {
		b.PutU8(0)
		return
	}
	b.PutRaw(data)
}

// encodeBlockEntities reads each live entity's compound under its own lock with
// no world lock held: the save's snapshot, taken before the world's read lock
// is taken for the sections (see saveChunk).
func encodeBlockEntities(own []*BlockEntity) []nbt.Tag {
	out := make([]nbt.Tag, len(own))
	for i, be := range own {
		be.Use(func(b *BlockEntity) { out[i] = b.SaveTag() })
	}
	return out
}

// HasBook reports whether a lectern holds a book. Call it from inside Use.
// vanilla: LecternBlockEntity.hasBook
func (b *BlockEntity) HasBook() bool {
	if b.Kind != KindLectern {
		return false
	}
	_, ok := b.book()
	return ok
}

// book is the lectern's book as a page count, false for a non-book. A writable
// book carries writable_book_content even when its stack names none.
// vanilla: Items.WRITABLE_BOOK, LecternBlockEntity.hasBook
func (b *BlockEntity) book() (int, bool) {
	if stored, ok := b.Stored[0]; ok {
		if n, ok := bookPagesOf(stored); ok {
			return n, true
		}
		id, _ := nbt.Get[nbt.String](stored, "id")
		return 0, string(id) == "minecraft:writable_book"
	}
	if n, ok := b.Slots[0].BookPages(); ok {
		return n, true
	}
	return 0, !b.Slots[0].Empty() && b.Slots[0].ItemID == int32(registry.ItemWritableBook)
}

// BookPages is the lectern book's page count, zero without one. Call it inside
// Use.
// vanilla: LecternBlockEntity.getPageCount
func (b *BlockEntity) BookPages() int {
	if b.Kind != KindLectern {
		return 0
	}
	n, _ := b.book()
	return n
}

// LecternSignal is a lectern's comparator strength: the page's share of the
// book, 1 to 15 with a book. Call it inside Use.
// vanilla: LecternBlockEntity.getRedstoneSignal
func (b *BlockEntity) LecternSignal() int {
	pages := b.BookPages()
	progress := float32(1)
	if pages > 1 {
		progress = float32(b.Page) / float32(pages-1)
	}
	signal := int(progress * 14)
	if b.HasBook() {
		signal++
	}
	return signal
}

// SetLecternPage turns to the page, clamped to the book, reporting whether it
// moved. Call it inside Use.
// vanilla: LecternBlockEntity.setPage
func (b *BlockEntity) SetLecternPage(page int) bool {
	page = clampPage(page, b.BookPages())
	if page == b.Page {
		return false
	}
	b.Page = page
	return true
}

// clampPage keeps a page inside a book of the given page count.
func clampPage(page, pages int) int {
	return max(0, min(page, pages-1))
}

// bookPagesOf counts the pages of a book kept as a file's item compound.
func bookPagesOf(stack nbt.Compound) (int, bool) {
	components, _ := nbt.Get[nbt.Compound](stack, "components")
	for _, name := range []string{"minecraft:written_book_content", "minecraft:writable_book_content"} {
		content, ok := nbt.Get[nbt.Compound](components, name)
		if !ok {
			continue
		}
		pages, _ := nbt.Get[nbt.List](content, "pages")
		return len(pages.Items), true
	}
	return 0, false
}

// kindOfID is the modelled kind with the chunk-format id, KindNone otherwise.
func kindOfID(id string) BlockEntityKind {
	k, _ := BlockEntityKindByName(id)
	return k
}

// BellState is a bell's swing and resonance counters.
// vanilla: BellBlockEntity
type BellState struct {
	// Ticks counts the swing; Shaking ends it at 50.
	Ticks   int
	Shaking bool
	// Resonating counts ResonationTicks up to 40, when the raiders glow.
	Resonating      bool
	ResonationTicks int
	// Raiders are the raiders in the box searched at SearchedAt (game time);
	// nil before the first search. Positions are read live.
	Raiders    []uuid.UUID
	SearchedAt int64
}

// qualifiedOccupants writes each stored bee's entity id as an Identifier.
// vanilla: BeehiveBlockEntity.Occupant.CODEC, TypedEntityData.codec
func qualifiedOccupants(bees nbt.List) nbt.List {
	items := make([]nbt.Tag, len(bees.Items))
	for i, item := range bees.Items {
		items[i] = item
		occupant, ok := nbt.CompoundView(item)
		if !ok {
			continue
		}
		data, ok := nbt.Get[nbt.Compound](occupant, "entity_data")
		if !ok {
			continue
		}
		if id, ok := nbt.Get[nbt.String](data, "id"); ok && registry.Qualify(string(id)) != string(id) {
			nextData := nbt.ShallowCopyCompound(occupant["entity_data"].(nbt.CompoundTag))
			next := nbt.ShallowCopyCompound(item.(nbt.CompoundTag))
			nextData.Set("id", nbt.String(registry.Qualify(string(id))))
			next.Set("entity_data", nextData)
			items[i] = next
		}
	}
	bees.Items = items
	return bees
}
