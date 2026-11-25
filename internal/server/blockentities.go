package server

import (
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/inventory"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// Container blocks: opening, placing, breaking, lids and the furnace tick.
// Contents live in the chunk's saved block entity
// (internal/server/world/blockentity.go); windows are in containers.go.

// containerBlockKind reports which window right-clicking the block opens;
// block properties do not matter.
func containerBlockKind(block registry.Block) (windowKind, bool) {
	switch block {
	case registry.BlockCraftingTable:
		return windowCrafting, true
	case registry.BlockFurnace:
		return windowFurnace, true
	case registry.BlockBlastFurnace:
		return windowBlastFurnace, true
	case registry.BlockSmoker:
		return windowSmoker, true
	case registry.BlockBrewingStand:
		return windowBrewingStand, true
	case registry.BlockCrafter:
		return windowCrafter, true
	case registry.BlockEnchantingTable:
		return windowEnchant, true
	case registry.BlockBarrel:
		return windowBarrel, true
	case registry.BlockShulkerBox:
		return windowShulker, true
	case registry.BlockEnderChest:
		return windowEnderChest, true
	case registry.BlockDispenser:
		return windowDispenser, true
	case registry.BlockDropper:
		return windowDropper, true
	case registry.BlockHopper:
		return windowHopper, true
	case registry.BlockAnvil, registry.BlockChippedAnvil, registry.BlockDamagedAnvil:
		return windowAnvil, true
	case registry.BlockStonecutter:
		return windowStonecutter, true
	case registry.BlockSmithingTable:
		return windowSmithing, true
	case registry.BlockGrindstone:
		return windowGrindstone, true
	case registry.BlockLoom:
		return windowLoom, true
	case registry.BlockCartographyTable:
		return windowCartography, true
	case registry.BlockBeacon:
		return windowBeacon, true
	case registry.BlockLectern:
		return windowLectern, true
	}
	if world.IsChestBlock(block) {
		return windowChest, true
	}
	if block.IsShulkerBox() {
		return windowShulker, true
	}
	return windowPlayer, false
}

// isCookingBlock reports whether the block keeps a cooking block entity:
// furnace, blast furnace, smoker.
func isCookingBlock(block registry.Block) bool {
	switch block {
	case registry.BlockFurnace, registry.BlockBlastFurnace, registry.BlockSmoker:
		return true
	}
	return false
}

// spectatorUseBlock opens the window a spectator's use shows, reporting
// whether one opened. A spectator runs none of the block's own use; only
// what getMenuProvider returns opens. vanilla: ServerPlayerGameMode.useItemOn
func (c *client) spectatorUseBlock(x, y, z int32) (bool, error) {
	w := c.theWorld()
	state := w.BlockAt(x, y, z)
	kind, opens := containerBlockKind(registry.BlockOf(state))
	if !opens {
		return c.spectatorUsePortal(x, y, z, state)
	}
	// A lectern with no book returns no provider.
	// vanilla: LecternBlock.getMenuProvider
	if kind == windowLectern && !registry.StateBool(state, registry.PropHasBook) {
		return false, nil
	}
	// An ender chest's block entity names no menu provider, so nothing opens.
	// vanilla: BaseEntityBlock.getMenuProvider
	if kind == windowEnderChest {
		return false, nil
	}
	// A chest whose lid a conductor blocks returns no provider.
	// vanilla: ChestBlock.getMenuProvider
	if world.IsChestBlock(registry.BlockOf(state)) && w.ChestBlockedAt(x, y, z) {
		return false, nil
	}
	// A container still holding its placed loot table refuses a spectator, so
	// no menu is built; a chest's table may be on either half.
	// vanilla: RandomizableContainerBlockEntity.canOpen
	halves := [][3]int32{{x, y, z}}
	if world.IsChestBlock(registry.BlockOf(state)) {
		if first, second, paired := w.ChestPair(x, y, z); paired {
			halves = [][3]int32{first, second}
		}
	}
	for _, h := range halves {
		be := w.BlockEntity(h[0], h[1], h[2])
		if be == nil {
			continue
		}
		pending := false
		be.Use(func(b *world.BlockEntity) { pending = b.LootTable != "" })
		if pending {
			return false, nil
		}
	}
	return true, c.openContainer(kind, x, y, z)
}

// sendCommandBlockData tells this client the command block's update tag, which
// chunk data leaves empty, so its editor opens filled in.
// vanilla: ServerPlayer.openCommandBlock
func (c *client) sendCommandBlockData(x, y, z int32) {
	be := c.theWorld().BlockEntity(x, y, z)
	if be == nil || be.Kind != world.KindCommandBlock {
		return
	}
	var tag nbt.Compound
	be.Use(func(b *world.BlockEntity) { tag = b.UpdateTag() })
	c.send(protocol.IDPlayBlockEntityData, blockEntityData(x, y, z, be.TypeID(), tag))
}

// useBlock handles a right-click on a block other than placing against it,
// reporting whether the block was used. facing is the player's horizontal
// look, by which a fence gate swings; hand is the clicking hand, which the
// anchor checks for the other hand's glowstone.
func (c *client) useBlock(hand, x, y, z int32, ctx rules.PlacementContext) (used bool, err error) {
	// Handled uses default to SUCCESS; CONSUME and SUCCESS_SERVER branches override it.
	c.useSwing = swingPredicted
	defer func() {
		if !used {
			c.useSwing = swingNone
		}
	}()
	facing := rules.PlayerDirection(ctx.Yaw)
	state := c.theWorld().BlockAt(x, y, z)
	block := registry.BlockOf(state)
	// A head clicked onto a note block's top is placed as the item's use,
	// leaving the note untuned. vanilla: NoteBlock.useItemOn
	if block == registry.BlockNoteBlock && ctx.Face == protocol.FaceTop &&
		registry.Item(c.heldItem(hand).ItemID).InTag(registry.ItemTagIDNoteblockTopInstruments) {
		return false, nil
	}
	// A gamemaster's click opens the block's editor and is used, so the held
	// item is not placed. Only the main hand reaches useWithoutItem.
	// vanilla: ServerPlayerGameMode.useItemOn, CommandBlock.useWithoutItem
	if hand == protocol.HandMain && rules.OpensGameMasterScreen(block) && c.canUseGameMasterBlocks() {
		c.sendCommandBlockData(x, y, z)
		return true, nil
	}
	// Consume even a refused click so the held block is not placed.
	// vanilla: LightBlock.useWithoutItem (flags 2)
	if hand == protocol.HandMain && block == registry.BlockLight {
		c.useSwing = swingNone
		if c.canUseGameMasterBlocks() {
			c.useSwing = swingServer
			level, _ := registry.StateInt(state, registry.PropLevel)
			next, _ := registry.WithInt(state, registry.PropLevel, (level+1)%16)
			c.srv.writeBlock(c.theWorld(), x, y, z, next, nil, writeLight|writeLightAfter|writeNeighborShapes)
		}
		return true, nil
	}
	// vanilla: DragonEggBlock.useWithoutItem
	if block == registry.BlockDragonEgg {
		c.teleportDragonEgg(x, y, z)
		return true, nil
	}
	// Redstone first (levers, buttons, repeaters, wire, doors): the engine
	// changes the state, changes and click sounds go out here. A block event (a
	// note block's note) goes after the changes, as vanilla runs it at tick
	// end, so the particle uses the new note. vanilla: ServerLevel.runBlockEvents
	used, changes, effects, primes := c.srv.physicsFor(c.theWorld()).UseBlock(x, y, z, facing)
	if used {
		// A pressed button consumes the click without starting another swing.
		// vanilla: ButtonBlock.useWithoutItem
		if block.InGroup(registry.GroupButton) && registry.StateBool(state, registry.PropPowered) {
			c.useSwing = swingNone
		}
		if block == registry.BlockNoteBlock {
			c.awardStat(player.Custom(registry.CustomStatTuneNoteblock), 1)
		}
		tell := func(e world.Effect) {
			c.srv.tellEffectFrom(c.theWorld(), c, e)
		}
		for _, e := range effects {
			if !e.HasBlockAction {
				tell(e)
			}
		}
		c.srv.tellBlockChanges(c.theWorld(), changes)
		for _, e := range effects {
			if e.HasBlockAction {
				tell(e)
			}
		}
		for _, t := range primes {
			c.srv.primeTNT(c.theWorld(), t)
		}
		c.srv.relightAsync(c.theWorld(), x, z)
		return true, nil
	}
	// A lectern's window opens only on the main hand and only with a book.
	if block == registry.BlockLectern {
		return c.useLecternWithoutItem(hand, x, y, z, state)
	}
	// A bell rings for a click on its body (see useBell); one on its blind side
	// falls through to the held item.
	if c.useBell(x, y, z, ctx) {
		return true, nil
	}
	// A bed always consumes the click (see useBed).
	if registry.BlockOf(state).InGroup(registry.GroupBed) {
		c.useSwing = swingServer
		return c.useBed(c.theWorld(), x, y, z, state)
	}
	// A charged respawn anchor sets the spawn in the nether and explodes
	// elsewhere (see useRespawnAnchor).
	if block == registry.BlockRespawnAnchor {
		return c.useRespawnAnchor(hand, x, y, z)
	}
	// A held hanging sign clicked onto the underside of a ceiling hanging sign
	// unless it is waxed with click events is placed below it instead of opening the editor.
	// vanilla: CeilingHangingSignBlock.shouldTryToChainAnotherHangingSign
	if registry.BlockOf(state).Type() == registry.BlockTypeCeilingHangingSign && ctx.Face == protocol.FaceBottom &&
		c.heldHangingSign(hand) && !c.signHasClicks(x, y, z, state) {
		return false, nil
	}
	// A sign opens its editor, or a waxed one wobbles; a held item's
	// applicators come first, in useItemOnBlock.
	if _, isSign := world.SignEntityKind(block); isSign {
		return c.useSign(block, x, y, z)
	}
	if block == registry.BlockEnderChest {
		// A conductor on top blocks the lid, but the click is still used.
		// vanilla: EnderChestBlock.useWithoutItem
		if c.theWorld().EnderChestBlockedAt(x, y, z) {
			return true, nil
		}
	}
	// A conductor above a chest blocks it too (either half of a double blocks
	// both), the click still used. vanilla: ChestBlock.useWithoutItem
	if world.IsChestBlock(registry.BlockOf(state)) && c.theWorld().ChestBlockedAt(x, y, z) {
		return true, nil
	}
	kind, opens := containerBlockKind(registry.BlockOf(state))
	if !opens {
		return false, nil
	}
	// A closed box whose lid would hit a block stays shut, the click still
	// used; a box being viewed is already open. The lid's closing animation,
	// when vanilla also skips the test, is not modelled.
	// vanilla: ShulkerBoxBlock.canOpen
	if kind == windowShulker && c.srv.containerOpenerCount(c.theWorld(), x, y, z) == 0 &&
		c.theWorld().ShulkerLidBlockedAt(x, y, z, registry.StateValue(state, registry.PropFacing)) {
		return true, nil
	}
	err = c.openContainer(kind, x, y, z)
	c.awardOpenStat(block)
	if err == nil && registry.BlockOf(state).InTag(registry.TagIDGuardedByPiglins) {
		c.angerNearbyPiglins(true)
	}
	return true, err
}

// containerPlaced gives a placed container its block entity: the stack's
// block_entity_data first, then its components (contents, custom name)
// over it. Nothing is sent: clients create their own block entity, and
// contents go out on open. vanilla: BlockEntity.applyComponentsFromItemStack
func (c *client) containerPlaced(state uint16, x, y, z int32, stack protocol.ItemStack) {
	kind, ok := containerEntityKindOf(registry.BlockOf(state))
	if !ok {
		return
	}
	be := c.theWorld().EnsureBlockEntity(x, y, z, kind)
	// Bells, ender chests, daylight detectors and conduits keep block entities
	// that are no BaseContainerBlockEntity and take no components (bells and
	// ender chests open windows of their own; detectors and conduits have no
	// slot to fill), nor does the creaking heart, whose link no item carries.
	// vanilla: BellBlockEntity, EnderChestBlockEntity,
	// DaylightDetectorBlockEntity, ConduitBlockEntity, CreakingHeartBlockEntity
	if be == nil || kind == world.KindBell || kind == world.KindEnderChest ||
		kind == world.KindDaylightDetector || kind == world.KindConduit || kind == world.KindPotentSulfur ||
		kind == world.KindCreakingHeart || kind == world.KindSculkSensor ||
		kind == world.KindCalibratedSculkSensor || kind == world.KindSculkShrieker ||
		kind == world.KindSculkCatalyst {
		return
	}
	// Data before components, vanilla's updateCustomBlockEntityTag then
	// applyComponentsFromItemStack order, so components override it. A
	// gamemaster block takes data only from a gamemaster.
	// vanilla: BlockItem.updateCustomBlockEntityTag
	if typeID, tag, ok := stack.BlockEntityDataOf(); ok &&
		(!world.OpOnlyKind(kind) || c.canUseGameMasterBlocks()) {
		be.Use(func(b *world.BlockEntity) { b.LoadBlockEntityData(typeID, tag) })
		// Only the data key counts, flag 2 like vanilla's write.
		// vanilla: JukeboxBlock.setPlacedBy
		if _, has := tag["RecordItem"]; has && kind == world.KindJukebox {
			if next, ok := registry.WithBool(state, registry.PropHasRecord, true); ok {
				c.srv.writeBlock(c.theWorld(), x, y, z, next, nil, writeNeighborShapes)
			}
		}
	}
	be.Use(func(b *world.BlockEntity) { b.ApplyItemStack(stack) })
}

// brokenBlockEntity reads components before removal and retains the saved
// compound for loot functions, including fields the removal consumes.
// vanilla: BlockEntity.collectComponents, Block.dropResources
func brokenBlockEntity(w *world.World, x, y, z int32) (world.ItemComponents, func() nbt.Compound) {
	if be := w.BlockEntity(x, y, z); be != nil {
		var components world.ItemComponents
		var tag nbt.Compound
		be.Use(func(b *world.BlockEntity) {
			components.Lock = registry.LockTag(b.Lock)
			if len(b.Components) > 0 {
				components.Extra = nbt.Copy(b.Components).(nbt.Compound)
			}
			components.ContainerLoot = protocol.ContainerLoot{Table: b.LootTable, Seed: b.LootTableSeed}
			if b.LootTable != "" || b.RecipesUsed.Len() > 0 {
				tag = b.EncodeNBT()
			}
		})
		if tag != nil {
			return components, func() nbt.Compound { return tag }
		}
		return components, func() nbt.Compound {
			var tag nbt.Compound
			be.Use(func(b *world.BlockEntity) { tag = b.EncodeNBT() })
			return tag
		}
	}
	tag, ok := w.BlockEntityTag(x, y, z)
	if !ok {
		return world.ItemComponents{}, nil
	}
	return world.ImplicitComponentsOf(tag), func() nbt.Compound { return tag }
}

// carriedComponentsPlaced gives a placed banner, player head or beehive the
// raw block entity its item's components describe, containerPlaced's
// counterpart for windowless kinds. No collected component, or nothing in
// the stack for the kind, creates no entity. Clients render banner patterns
// and head profiles from it, so watchers are told at once rather than at the
// next chunk send; a beehive has no update packet.
// vanilla: BlockItem.updateBlockEntityComponents, BannerBlockEntity.getUpdatePacket,
// SkullBlockEntity.getUpdatePacket
func (c *client) carriedComponentsPlaced(w *world.World, b registry.Block, x, y, z int32, stack protocol.ItemStack) {
	id, ok := registry.BlockEntityTypes[b.Name()]
	if !ok {
		return
	}
	kind, _ := world.BlockEntityKindByName(id)
	tag, ok := w.PlaceBlockEntityComponents(x, y, z, kind, stack)
	if !ok {
		return
	}
	switch kind {
	case world.KindBanner, world.KindSkull:
		c.srv.broadcastBlockChunkExcept(w, nil, x, z, protocol.IDPlayBlockEntityData, blockEntityData(
			x, y, z, world.BlockEntityTypeID(id), tag))

	}
}

// containerEntityKindOf is a container block's block entity kind; false for
// the crafting table, enchanting table, anvil, stonecutter, grindstone and
// cartography table, whose slots are the window's. Some non-container
// blocks keep one too: the beacon (level and choices), bell (ring action),
// comparator (output signal), decorated pot (one stack), shelf (three),
// conduit (beam target) and creaking heart (creaking link).
// vanilla: DecoratedPotBlock.newBlockEntity, ShelfBlock.newBlockEntity
func containerEntityKindOf(block registry.Block) (world.BlockEntityKind, bool) {
	switch block {
	case registry.BlockCommandBlock, registry.BlockChainCommandBlock, registry.BlockRepeatingCommandBlock:
		// All three command blocks keep the one command block entity.
		// vanilla: CommandBlock.newBlockEntity
		return world.KindCommandBlock, true
	}
	if block == registry.BlockTrappedChest {
		return world.KindTrappedChest, true
	}
	if block.InTag(registry.TagIDCampfires) {
		// vanilla: CampfireBlock.newBlockEntity
		return world.KindCampfire, true
	}
	if block == registry.BlockBell {
		// vanilla: BellBlock.newBlockEntity
		return world.KindBell, true
	}
	if block == registry.BlockComparator {
		// The comparator's output signal lives in a block entity of its own.
		// vanilla: ComparatorBlock.newBlockEntity
		return world.KindComparator, true
	}
	if block == registry.BlockJukebox {
		// vanilla: JukeboxBlock.newBlockEntity
		return world.KindJukebox, true
	}
	if block == registry.BlockLectern {
		// vanilla: LecternBlock.newBlockEntity
		return world.KindLectern, true
	}
	if block == registry.BlockDaylightDetector {
		// vanilla: DaylightDetectorBlock.newBlockEntity
		return world.KindDaylightDetector, true
	}
	if block == registry.BlockDecoratedPot {
		// vanilla: DecoratedPotBlock.newBlockEntity
		return world.KindDecoratedPot, true
	}
	// Every wooden shelf shares the shelf block entity.
	// vanilla: ShelfBlock.newBlockEntity
	if block.Type() == registry.BlockTypeShelf {
		return world.KindShelf, true
	}
	if block == registry.BlockChiseledBookshelf {
		// vanilla: ChiseledBookShelfBlock.newBlockEntity
		return world.KindChiseledBookshelf, true
	}
	if block == registry.BlockConduit {
		// vanilla: ConduitBlock.newBlockEntity
		return world.KindConduit, true
	}
	if block.InGroup(registry.GroupCopperGolemStatue) {
		// vanilla: CopperGolemStatueBlock.newBlockEntity
		return world.KindCopperGolemStatue, true
	}
	if block == registry.BlockEnchantingTable {
		// vanilla: EnchantingTableBlock.newBlockEntity
		return world.KindEnchantingTable, true
	}
	if block == registry.BlockPotentSulfur {
		// vanilla: PotentSulfurBlock.newBlockEntity
		return world.KindPotentSulfur, true
	}
	if block == registry.BlockSculkSensor {
		// vanilla: SculkSensorBlock.newBlockEntity
		return world.KindSculkSensor, true
	}
	if block == registry.BlockCalibratedSculkSensor {
		// vanilla: CalibratedSculkSensorBlock.newBlockEntity
		return world.KindCalibratedSculkSensor, true
	}
	if block == registry.BlockSculkShrieker {
		// vanilla: SculkShriekerBlock.newBlockEntity
		return world.KindSculkShrieker, true
	}
	if block == registry.BlockSculkCatalyst {
		// vanilla: SculkCatalystBlock.newBlockEntity
		return world.KindSculkCatalyst, true
	}
	if block == registry.BlockCreakingHeart {
		// The heart keeps one for the creaking it may be linked to.
		// vanilla: CreakingHeartBlock.newBlockEntity
		return world.KindCreakingHeart, true
	}
	kind, _ := containerBlockKind(block)
	switch kind {
	case windowChest:
		return world.KindChest, true
	case windowFurnace:
		return world.KindFurnace, true
	case windowBlastFurnace:
		return world.KindBlastFurnace, true
	case windowSmoker:
		return world.KindSmoker, true
	case windowBrewingStand:
		return world.KindBrewingStand, true
	case windowCrafter:
		return world.KindCrafter, true
	case windowBarrel:
		return world.KindBarrel, true
	case windowShulker:
		return world.KindShulkerBox, true
	case windowEnderChest:
		return world.KindEnderChest, true
	case windowDispenser:
		return world.KindDispenser, true
	case windowDropper:
		return world.KindDropper, true
	case windowHopper:
		return world.KindHopper, true
	case windowBeacon:
		return world.KindBeacon, true
	}
	return world.KindNone, false
}

// removeBlockEntity removes a block entity with setRemoved's side effects:
// a jukebox's stop whatever its song (unlike an eject's or hopper pull's)
// and a beacon's deactivate. Explosions and block replacement use it.
// vanilla: JukeboxBlockEntity.setRemoved, BeaconBlockEntity.setRemoved
func (s *Server) removeBlockEntity(w *world.World, x, y, z int32) *world.BlockEntity {
	be := w.RemoveBlockEntity(x, y, z)
	if be == nil {
		return nil
	}
	switch be.Kind {
	case world.KindJukebox:
		s.broadcastLevelEvent(w,
			protocol.WorldEventJukeboxStop, x, y, z, 0)

	case world.KindCreakingHeart:
		// vanilla: CreakingHeartBlockEntity.preRemoveSideEffects
		s.creakingRemoveProtector(w, be, false)

	case world.KindBeacon:
		s.broadcastNear(w, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, soundRange(1),
			protocol.IDPlaySoundEffect, soundEffectIDAt(registry.SoundBlockBeaconDeactivate,
				protocol.SoundCategoryBlock,
				float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, 1.0, 1.0))
	}
	return be
}

// breakContainer spills a broken container as item entities, removes its
// block entity and closes its windows; a furnace pays its owed experience at
// the centre (vanilla: preRemoveSideEffects). It spills in creative too.
// A shulker box spills nothing (its loot's copy_components keeps the
// contents in the drop, so spilling would duplicate); a jukebox pops its
// disc above (see spawnJukeboxRecord). The returned call (nil for no spill)
// pays and drops once the cell is written, so nothing spawns inside the
// container's collision box. vanilla: LevelChunk.setBlockState
func (c *client) breakContainer(x, y, z int32, old uint16) func() {
	w := c.theWorld()
	var spill func()
	c.srv.breakBlockEntity(w, x, y, z, old, false, func(be *world.BlockEntity, spills []protocol.ItemStack) {
		spill = func() {
			if world.IsCooking(be.Kind) {
				c.srv.payFurnaceSmelts(be, w, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, nil)
			}
			c.srv.spillContainer(w, x, y, z, spills)
		}
	})
	return spill
}

// breakBlockEntity removes and empties a broken cell's block entity: a
// cooking block pays its experience at the centre first when payNow
// (vanilla: BlockEntity.preRemoveSideEffects; else the spill callback pays),
// the slots go to spill, and windows on it close. It returns the slots and
// name the loot copies onto the drop (copy_components). A shulker box
// spills nothing, its loot carrying the contents.
func (s *Server) breakBlockEntity(w *world.World, x, y, z int32, old uint16, payNow bool, spill func(*world.BlockEntity, []protocol.ItemStack)) (container []protocol.ItemStack, customName *protocol.Text) {
	be := s.removeBlockEntity(w, x, y, z)
	if be == nil {
		return nil, nil
	}
	s.shriekerRemoved(w, x, y, z, old, be)
	// Shulker boxes keep unresolved loot on their item; playerWillDestroy
	// already unpacks a survival player's box before rolling its drop.
	// vanilla: ShulkerBoxBlock.playerWillDestroy, Containers.dropContents
	if be.Kind != world.KindShulkerBox {
		be.Use(func(b *world.BlockEntity) { w.UnpackLoot(b) })
	}
	// Read the slots before they spill: a shulker box's copy_components puts
	// them on its drop.
	container, customName = containerOf(be), customNameOf(be)
	if payNow && world.IsCooking(be.Kind) {
		s.payFurnaceSmelts(be, w, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, nil)
	}
	var spills []protocol.ItemStack
	switch {
	case be.Kind == world.KindJukebox:
		// A jukebox pops its disc out above itself rather than spilling it.
		var disc protocol.ItemStack
		be.Use(func(b *world.BlockEntity) { disc = b.Slots[0] })
		if !disc.Empty() {
			s.spawnJukeboxRecord(w, x, y, z, disc)
		}
	case be.Kind == world.KindLectern:
		// The book is thrown on the lectern's facing side, only if the state says
		// it has one. vanilla: LecternBlockEntity.preRemoveSideEffects
		if registry.BlockOf(old) == registry.BlockLectern {
			var book protocol.ItemStack
			be.Use(func(b *world.BlockEntity) { book = b.Slots[0] })
			if registry.StateBool(old, registry.PropHasBook) && !book.Empty() {
				s.spawnLecternBook(w, x, y, z, old, book)
			}
		}
	case be.Kind != world.KindShulkerBox:
		be.Use(func(b *world.BlockEntity) {
			for _, stack := range b.Slots {
				if !stack.Empty() {
					spills = append(spills, stack)
				}
			}
		})
	}
	spill(be, spills)
	s.closeContainersAt(w, x, y, z)
	return container, customName
}

// payFurnaceSmelts takes the furnace's recipe-use counts (so nothing pays
// twice) and spawns each recipe's experience as orbs at x, y, z: the taker
// for a result take, the block centre for a break. One award per recipe
// (vanilla: ExperienceOrb.award), spawned after the block entity's lock
// since nothing is sent under a lock. It returns the recipes it found,
// sorted by id, each with the furnace's non-empty slots after
// the payout's take as its ingredients (vanilla passes this.items).
func (s *Server) payFurnaceSmelts(be *world.BlockEntity, w *world.World, x, y, z float64, extract func(item protocol.ItemStack, xp int) int) []inventory.RecipeCraft {
	var used map[string]int
	var slots []protocol.ItemStack
	be.Use(func(b *world.BlockEntity) {
		used = b.TakeRecipesUsed()
		for _, slot := range b.Slots {
			if !slot.Empty() {
				slots = append(slots, slot)
			}
		}
		// The click emptying the result marked the chunk before this, so a save in
		// between would keep the old counts. A broken furnace refuses the mark.
		w.TouchEntity(b)
	})
	var crafts []inventory.RecipeCraft
	var made protocol.ItemStack
	total := 0
	for id, count := range used {
		recipe, ok := registry.CookingRecipeByID(be.Kind.Name(), id)
		if !ok {
			// An unknown recipe (another data pack) awards nothing; vanilla's lookup
			// misses it too.
			continue
		}
		crafts = append(crafts, inventory.RecipeCraft{ID: id, Ingredients: slots})
		xp := rules.FurnaceExperience(count, recipe.Experience, float32(s.levelRng.Float64()))
		if extract == nil {
			s.spawnOrbs(w, x, y, z, xp)
			continue
		}
		// A listener sees one extraction: what the smelts made and all their
		// experience, which it may change.
		total += xp
		if made.Count == 0 {
			made = registry.CookingResult(recipe)
			made.Count = 0
		}
		made.Count += int32(count) * recipe.ResultCount
	}
	if extract != nil && made.Count > 0 {
		s.spawnOrbs(w, x, y, z, extract(made, total))
	}
	slices.SortFunc(crafts, func(a, b inventory.RecipeCraft) int { return strings.Compare(a.ID, b.ID) })
	return crafts
}

// lidBehavior is a container's lid: whether it animates by block action,
// whether that repeats on the five-tick recheck, the action's block, and
// the open and close sounds.
type lidBehavior struct {
	action     bool
	rechecked  bool
	block      int32
	openSound  registry.Sound
	closeSound registry.Sound
}

// lidOf returns the lid behaviour at a position. Chests, trapped chests and
// ender chests repeat their action on recheck; shulker boxes send one per
// open and close naming their colour's block; the barrel sends none
// (vanilla's openerCountChanged is empty for it), only sounds.
func lidOf(w *world.World, x, y, z int32) (lidBehavior, bool) {
	state := w.BlockAt(x, y, z)
	b := registry.BlockOf(state)
	// A copper chest's hinge sound follows its weathering stage (waxed sharing
	// the unwaxed); the action names its own block. vanilla: CopperChestBlock
	if b.InTag(registry.TagIDCopperChests) {
		openSound, closeSound := copperChestHingeSounds(b)
		return lidBehavior{
			action: true, rechecked: true, block: int32(b),
			openSound: openSound, closeSound: closeSound,
		}, true
	}
	switch b {
	case registry.BlockChest, registry.BlockTrappedChest:
		block := int32(protocol.BlockChest)
		if b == registry.BlockTrappedChest {
			block = protocol.BlockTrappedChest
		}
		return lidBehavior{
			action: true, rechecked: true, block: block,
			openSound: registry.SoundBlockChestOpen, closeSound: registry.SoundBlockChestClose,
		}, true
	case registry.BlockEnderChest:
		return lidBehavior{
			action: true, rechecked: true, block: protocol.BlockEnderChest,
			openSound: registry.SoundBlockEnderChestOpen, closeSound: registry.SoundBlockEnderChestClose,
		}, true
	case registry.BlockBarrel:
		return lidBehavior{
			openSound: registry.SoundBlockBarrelOpen, closeSound: registry.SoundBlockBarrelClose,
		}, true
	}
	// Each shulker colour is its own block with its own registry number.
	if b.IsShulkerBox() {
		return lidBehavior{
			action: true, block: int32(b),
			openSound: registry.SoundBlockShulkerBoxOpen, closeSound: registry.SoundBlockShulkerBoxClose,
		}, true
	}
	return lidBehavior{}, false
}

// copperChestHingeSounds is a copper chest's open and close sound: weathered
// and oxidized have their own, unaffected and exposed share the plain one.
// vanilla: CopperChestBlock.getHingeSound
func copperChestHingeSounds(b registry.Block) (open, close registry.Sound) {
	switch stage, _ := world.CopperChestStage(b); stage {
	case 2:
		return registry.SoundBlockCopperChestWeatheredOpen, registry.SoundBlockCopperChestWeatheredClose
	case 3:
		return registry.SoundBlockCopperChestOxidizedOpen, registry.SoundBlockCopperChestOxidizedClose
	default:
		return registry.SoundBlockCopperChestOpen, registry.SoundBlockCopperChestClose
	}
}

// sendLidAction sends the lid block action to the world; the parameter is
// the viewer count, open above zero and closed at zero.
func (s *Server) sendLidAction(w *world.World, x, y, z int32, viewers int, block int32) {
	var b protocol.Buffer
	b.PutPosition(x, y, z)
	b.PutU8(protocol.ChestActionLid)
	b.PutU8(uint8(viewers))
	b.PutVarInt(block)
	s.broadcastBlockEvent(w, x, y, z, b.Bytes())
}

// containerLidOpened sends the lid action with the current viewer count
// (from players' open windows; no counter is stored) and books the recheck.
// The open sound is containerOpenSound's, played while the window is built.
func (s *Server) containerLidOpened(w *world.World, x, y, z int32) {
	lid, isLid := lidOf(w, x, y, z)
	if !isLid {
		return
	}
	viewers := s.containerOpenerCount(w, x, y, z)
	if lid.action {
		s.sendLidAction(w, x, y, z, viewers, lid.block)
	}
	s.signalTrappedChest(w, x, y, z, viewers)
	s.moveShulkerLid(w, x, y, z, viewers)
	if lid.rechecked {
		s.scheduleLidRecheck(w, x, y, z)
	}
}

// containerOpenSound plays the open sound for the lid's first viewer:
// written straight to the opener so it precedes Open Screen and the contents
// (vanilla's order), and to the rest via the mailbox. A double chest's comes
// from its right half only. vanilla: ChestBlockEntity.startOpen
func (c *client) containerOpenSound(window *openWindow) error {
	for _, h := range window.halves() {
		lid, isLid := lidOf(window.w, h[0], h[1], h[2])
		if !isLid {
			continue
		}
		viewers := c.srv.containerOpenerCount(window.w, h[0], h[1], h[2])
		if viewers != 1 {
			continue
		}
		payload, plays := c.srv.containerSound(window.w, lid.openSound, h[0], h[1], h[2])
		if !plays {
			continue
		}
		c.srv.broadcastSoundPacketExcept(window.w, c, payload)
		if err := c.writePacket(protocol.IDPlaySoundEffect, "container open sound", payload); err != nil {
			return err
		}
	}
	return nil
}

// lockSoundEvent is a lock refusal's sound for any container.
// vanilla: SoundEvents.CHEST_LOCKED
const lockSoundEvent = registry.SoundBlockChestLocked

// containerLocked answers a use the held item does not unlock: the overlay
// message naming the container to the clicker, then the sound for the whole
// world. vanilla: BaseContainerBlockEntity.sendChestLockedNotifications
func (c *client) containerLocked(window *openWindow, name protocol.Text) error {
	payload := systemChatPayload(protocol.Text{Translate: "container.isLocked", With: []protocol.Text{name}}, true)
	if err := c.writePacket(protocol.IDPlaySystemChat, "locked container message", payload); err != nil {
		return err
	}
	sound := lockedContainerSound(window.w, window.kind, window.x, window.y, window.z)
	c.srv.broadcastSoundPacketExcept(window.w, nil, sound)
	return nil
}

// lockedContainerSound builds the lock refusal sound at the container's
// centre (between a double chest's halves: the right half's centre plus half
// the step to its partner) at the notification's volume and pitch.
// vanilla: BaseContainerBlockEntity.sendChestLockedNotifications
func lockedContainerSound(w *world.World, kind windowKind, x, y, z int32) []byte {
	dx, dz := 0.0, 0.0
	if kind == windowDouble {
		sx, sz := world.ChestPartnerStep(w.BlockAt(x, y, z))
		dx, dz = float64(sx)/2, float64(sz)/2
	}
	return soundEffectIDAt(lockSoundEvent, protocol.SoundCategoryBlock,
		float64(x)+0.5+dx, float64(y)+0.5, float64(z)+0.5+dz, 1, 1)
}

// containerLidClosed sends the lid action with the remaining viewers, and
// the close sound once none remain.
func (s *Server) containerLidClosed(w *world.World, x, y, z int32) {
	lid, isLid := lidOf(w, x, y, z)
	if !isLid {
		return
	}
	viewers := s.containerOpenerCount(w, x, y, z)
	if viewers == 0 {
		s.playContainerSound(w, lid.closeSound, x, y, z)
	}
	if lid.action {
		s.sendLidAction(w, x, y, z, viewers, lid.block)
	}
	s.signalTrappedChest(w, x, y, z, viewers)
	s.moveShulkerLid(w, x, y, z, viewers)
}

// moveShulkerLid passes a shulker box's viewer count to the engine, which
// runs the lid phases and their neighbour updates.
func (s *Server) moveShulkerLid(w *world.World, x, y, z int32, viewers int) {
	if registry.IsShulkerBoxState(w.BlockAt(x, y, z)) {
		s.physicsFor(w).ShulkerViewersChanged(x, y, z, viewers)
	}
}

// signalTrappedChest records a trapped chest's viewers as its signal and
// wakes what it reaches when it changed.
func (s *Server) signalTrappedChest(w *world.World, x, y, z int32, viewers int) {
	be := w.BlockEntity(x, y, z)
	if be == nil || be.Kind != world.KindTrappedChest {
		return
	}
	moved := false
	be.Use(func(b *world.BlockEntity) {
		moved = b.Openers != viewers
		b.Openers = viewers
	})
	if !moved {
		return
	}
	changes, effects, primes := s.physicsFor(w).TrappedChestOpenersChanged(x, y, z)
	for _, e := range effects {
		s.tellEffect(w, e)
	}
	for _, t := range primes {
		s.primeTNT(w, t)
	}
	s.tellBlockChanges(w, changes)
}

// containerSound builds a container's open or close sound, volume 0.5,
// pitch in [0.9, 1.0), and returns false for a double chest's left half,
// whose right half plays it. The barrel's comes from its mouth, half a block
// along its facing (vanilla: BarrelBlockEntity.playSound); a double chest's
// from between the halves (vanilla: ChestBlockEntity.playSound).
func (s *Server) containerSound(w *world.World, sound registry.Sound, x, y, z int32) ([]byte, bool) {
	dx, dy, dz := 0.5, 0.5, 0.5
	state := w.BlockAt(x, y, z)
	if sound == registry.SoundBlockBarrelOpen || sound == registry.SoundBlockBarrelClose {
		facing := registry.StateValue(state, registry.PropFacing)
		dx += barrelMouth[facing][0]
		dy += barrelMouth[facing][1]
		dz += barrelMouth[facing][2]
	}
	if world.IsChestBlock(registry.BlockOf(state)) {
		switch registry.StateValue(state, registry.PropType) {
		case "left":
			return nil, false
		case "right":
			sx, sz := world.ChestPartnerStep(state)
			dx += float64(sx) / 2
			dz += float64(sz) / 2
		}
	}
	return soundEffectIDAt(sound, protocol.SoundCategoryBlock,
		float64(x)+dx, float64(y)+dy, float64(z)+dz, 0.5, float32(0.9+s.soundRng.Float64()*0.1)), true
}

// playContainerSound plays containerSound to the whole world.
func (s *Server) playContainerSound(w *world.World, sound registry.Sound, x, y, z int32) {
	if payload, ok := s.containerSound(w, sound, x, y, z); ok {
		s.broadcastSoundPacketExcept(w, nil, payload)
	}
}

// barrelMouth is the barrel sound's offset from the block centre per facing.
var barrelMouth = map[string][3]float64{
	"north": {0, 0, -0.5},
	"south": {0, 0, 0.5},
	"west":  {-0.5, 0, 0},
	"east":  {0.5, 0, 0},
	"up":    {0, 0.5, 0},
	"down":  {0, -0.5, 0},
}

// lidRecheck keys a container's lid recheck.
type lidRecheck struct {
	w       *world.World
	x, y, z int32
}

// lidRecheckDelay is the lid recheck interval, so an open lid's action
// repeats every 5 ticks. vanilla: ContainerOpenersCounter.scheduleRecheck
const lidRecheckDelay = 5

// scheduleLidRecheck books the next recheck unless one is due.
// vanilla: LevelChunkTicks.schedule dedupes per position and block, so
// rebooking never moves a booked tick.
func (s *Server) scheduleLidRecheck(w *world.World, x, y, z int32) {
	key := lidRecheck{w: w, x: x, y: y, z: z}
	s.lidsMu.Lock()
	if _, booked := s.lids[key]; !booked {
		s.lids[key] = lidRecheckDelay
	}
	s.lidsMu.Unlock()
}

// tickLidRechecks runs due rechecks: recount, resend the lid action, rebook
// while anyone looks. Entries whose block no longer rechecks are dropped.
// With no viewers left it still sends the zero count once, as vanilla's
// already-booked recheck does, then stops.
func (s *Server) tickLidRechecks() {
	// Take the due entries in one pass under the lock; recount and broadcast
	// must not run under it.
	s.lidsMu.Lock()
	var due []lidRecheck
	for key, ticks := range s.lids {
		if ticks > 1 {
			s.lids[key] = ticks - 1
			continue
		}
		due = append(due, key)
		delete(s.lids, key)
	}
	s.lidsMu.Unlock()
	for _, key := range due {
		lid, isLid := lidOf(key.w, key.x, key.y, key.z)
		if !isLid || !lid.rechecked {
			continue
		}
		viewers := s.containerOpenerCount(key.w, key.x, key.y, key.z)
		s.sendLidAction(key.w, key.x, key.y, key.z, viewers, lid.block)
		s.signalTrappedChest(key.w, key.x, key.y, key.z, viewers)
		if viewers > 0 {
			s.scheduleLidRecheck(key.w, key.x, key.y, key.z)
		}
	}
}

// furnaceBurn is one furnace's tick: computed under the block entity's
// lock, acted on (lit state, window updates) after.
type furnaceBurn struct {
	be *world.BlockEntity
	// events asks the plugin listeners; nil when none of them is registered.
	events                             *blockEntityEvents
	x, y, z                            int32
	litWas, litNow                     bool
	inputStack, fuelStack, outputStack protocol.ItemStack
	// slotsMoved: a visible slot changed; progressed: fire or progress moved.
	// Either means viewers are told.
	slotsMoved, progressed bool
	// ran is false when Tick found the furnace gone: nothing was read and x,
	// y, z name nothing to act on.
	ran bool
}

// daylightDetectorCadence is how often a detector rereads the sky, the game
// time gate on its vanilla ticker. vanilla: DaylightDetectorBlock.tickEntity
const daylightDetectorCadence = 20

// tickBlockEntities ticks every loaded ticking block entity in every
// dimension, then the lid rechecks.
func (s *Server) tickBlockEntities() {
	age, _ := s.world.Time()
	gate := s.newEntityGate()
	for _, dim := range s.allDims() {
		ticks := gate.blockFilter(dim.w)
		host := &spawnerHost{s: s, dim: dim}
		s.tickTrialSpawners(dim, host, age, ticks)
		s.tickHives(dim.w, ticks)
		s.tickEndGateways(dim.w, ticks)
		s.tickVaults(dim, &trialHost{s: s, dim: dim, spawn: host, now: age}, age, ticks)
		old := len(dim.tickingBuf)
		dim.tickingBuf = dim.w.AppendTickingBlockEntities(dim.tickingBuf[:0])
		if n := len(dim.tickingBuf); n < old {
			// Drop the stale tail so removed entities are not kept alive.
			clear(dim.tickingBuf[n:old])
		}
		// Only a hopper's pull reads the item index; be.Kind is fixed for life.
		if slices.ContainsFunc(dim.tickingBuf, func(be *world.BlockEntity) bool { return be.Kind == world.KindHopper }) {
			s.indexItemColumns(dim.w)
			s.indexCartColumns(dim.w)
		} else {
			// The previous dimension's index must not answer for this one.
			s.dropItemColumns()
			s.dropCartColumns()
		}
		dim.sulfur.reset()
		var tickCache [8][8]struct {
			pos        world.ChunkPos
			generation uint64
			valid      bool
			ticks      bool
		}
		for _, be := range dim.tickingBuf {
			if seam := s.blockEntityTickSeam; seam != nil {
				seam(be)
			}
			if be.Sleeping() {
				continue
			}
			pos := blockChunk(be.X, be.Z)
			entry := &tickCache[pos.X&7][pos.Z&7]
			// A ticket change during the filter must invalidate this entry on the next turn.
			generation := dim.w.TicketGeneration()
			if !entry.valid || entry.pos != pos || entry.generation != generation {
				entry.pos = pos
				entry.generation = generation
				entry.ticks = ticks(pos)
				entry.valid = true
			}
			if !entry.ticks {
				continue
			}
			switch {
			case world.IsCooking(be.Kind):
				step := furnaceBurn{be: be, events: s.blockEntityEvents(dim.w)}
				// A break since the list was read already spilled the slots.
				be.Tick(step.advance)
				if !step.ran {
					continue
				}
				if step.litNow != step.litWas {
					s.setFurnaceLit(dim.w, step.x, step.y, step.z, step.litNow)
				}
				if step.slotsMoved {
					// Only a slot move is vanilla's setChanged; fire progress does not wake the
					// comparators, which read the slots only.
					dim.w.Touch(step.x, step.y, step.z)
				}
				if step.slotsMoved || step.progressed {
					s.tellFurnaceViewers(dim.w, step)
				}
			case be.Kind == world.KindBrewingStand:
				step := brewingStep{be: be, events: s.blockEntityEvents(dim.w)}
				be.Tick(step.advance)
				if !step.ran {
					continue
				}
				// Every brewing tick is a vanilla setChanged, waking the stand's
				// comparators.
				if step.slotsMoved || step.progressed {
					dim.w.Touch(step.x, step.y, step.z)
				}
				if step.brewed {
					s.broadcastLevelEvent(dim.w,
						protocol.WorldEventBrewing, step.x, step.y, step.z, 0)

				}
				if len(step.drops) > 0 {
					s.spillContainer(dim.w, step.x, step.y, step.z, step.drops)
				}
				if step.bottlesMoved {
					s.setBrewingBottles(dim.w, step.x, step.y, step.z, step.bottles)
				}
				if step.slotsMoved || step.progressed {
					s.tellBrewingViewers(dim.w, step)
				}
			case be.Kind == world.KindHopper:
				s.tickHopper(dim.w, be, age)
			case be.Kind == world.KindCrafter:
				s.tickCrafter(dim.w, be)
			case be.Kind == world.KindConduit:
				s.tickConduit(dim.w, be, age)
			case be.Kind == world.KindBell:
				s.tickBell(dim, be)
			case be.Kind == world.KindPotentSulfur:
				s.tickPotentSulfur(dim, be, age)
			case be.Kind == world.KindJukebox:
				s.tickJukebox(dim.w, be)
			case be.Kind == world.KindDaylightDetector:
				if age%daylightDetectorCadence == 0 {
					s.tickDaylightDetector(dim.w, be)
				}
			case be.Kind == world.KindCreakingHeart:
				s.tickCreakingHeart(dim.w, be)
			case be.Kind == world.KindCampfire:
				s.tickCampfire(dim.w, be)
			case be.Kind == world.KindBeacon:
				s.tickBeacon(dim.w, be, age)
			case be.Kind == world.KindSpawner:
				s.tickSpawner(host, be)
			case be.Kind == world.KindSign || be.Kind == world.KindHangingSign:
				s.clearFarSignEditor(dim.w, be)
			}
		}
	}
	s.dropItemColumns()
	s.dropCartColumns()
	s.tickLidRechecks()
}

// tickDaylightDetector recomputes a detector's power and tells the world;
// the use path broadcasts a click itself, so the tick must too.
func (s *Server) tickDaylightDetector(w *world.World, be *world.BlockEntity) {
	changes, effects, primes := s.physicsFor(w).DaylightDetectorUpdate(be.X, be.Y, be.Z)
	for _, e := range effects {
		s.tellEffect(w, e)
	}
	s.tellBlockChanges(w, changes)
	for _, t := range primes {
		s.primeTNT(w, t)
	}
}

// advance runs a furnace tick and reports whether it can sleep until a write.
func (b *furnaceBurn) advance(entity *world.BlockEntity) bool {
	b.x, b.y, b.z = entity.X, entity.Y, entity.Z
	b.ran = true

	input := entity.Slots[0]
	result := registry.CookingMatchCached(entity.Kind.Name(), input, &entity.CookCheck)
	// Smeltable: a recipe matches and the output is empty or the same item
	// with room for one more.
	var cooked protocol.ItemStack
	if result != nil {
		cooked = registry.CookingResult(result)
	}
	canSmelt := result != nil && (entity.Slots[2].Empty() ||
		(entity.Slots[2].Same(cooked) &&
			entity.Slots[2].Count+result.ResultCount <= registry.StackMaxStackSize(entity.Slots[2])))

	lit := entity.LitRemaining > 0
	b.litWas = lit
	if lit {
		entity.LitRemaining--
		b.progressed = true
		lit = entity.LitRemaining > 0
	}
	hasIngredient := !input.Empty()
	hasFuel := !entity.Slots[1].Empty()

	// A furnace cooks with fire and something to cook; otherwise progress
	// cools or stays. vanilla: AbstractFurnaceBlockEntity.serverTick
	if lit || (hasFuel && hasIngredient) {
		if !hasIngredient {
			b.resetCook(entity)
		} else if result != nil {
			if canSmelt {
				if !lit {
					burn, speed := fuelLot(entity.Kind, entity.Slots[1])
					if burn > 0 && b.events != nil {
						burn = b.events.burn(entity, entity.Slots[1], burn)
					}
					entity.LitTotal, entity.LitRemaining = burn, burn
					entity.SpeedMultiplier = speed
					// A cook under way keeps its share done at the new fuel's speed.
					if entity.CookingTotal > 0 && entity.CookingSpent < entity.CookingTotal {
						done := float32(entity.CookingSpent) / float32(entity.CookingTotal)
						entity.CookingTotal = entity.CookTotalOf(result)
						entity.CookingSpent = int32(math.Ceil(float64(done * float32(entity.CookingTotal))))
					}
					if burn > 0 {
						entity.Put(1, spentFuel(entity.Slots[1]))
						b.slotsMoved = true
						b.progressed = true
						lit = true
					}
				}
				if lit {
					// Keep a loaded total until completion, including a missing total of zero.
					// vanilla: AbstractFurnaceBlockEntity.serverTick
					entity.CookingSpent++
					b.progressed = true
					if entity.CookingSpent >= entity.CookingTotal {
						// Count the smelt toward owed experience, paid on result take or break.
						// vanilla: AbstractFurnaceBlockEntity.setRecipeUsed
						entity.CookingSpent = 0
						entity.CookingTotal = entity.CookTotalOf(result)
						produced, cooked := registry.CookingResult(result), true
						if b.events != nil {
							produced, cooked = b.events.cook(entity, input, produced)
						}
						if cooked {
							if entity.Slots[2].Empty() {
								entity.Put(2, produced)
							} else if entity.Slots[2].Same(produced) {
								output := entity.Slots[2]
								output.Count += produced.Count
								entity.Put(2, output)
							}
							// A dried sponge fills an empty bucket in the fuel slot.
							// vanilla: AbstractFurnaceBlockEntity.burn
							if fill, ok := wetSpongeBucketFill(input, entity.Slots[1]); ok {
								entity.Put(1, fill)
							}
							left := input
							left.Count = input.Count - 1
							if left.Count <= 0 {
								left = protocol.ItemStack{}
							}
							// The input shrinks in place, as vanilla's burn does, rather than a setItem
							// that would reset the cook total.
							entity.PutKeepingCook(0, left)
							entity.RecordRecipeUsed(result.ID)
							b.slotsMoved = true
						}
					}
				} else {
					b.resetCook(entity)
				}
			} else {
				b.resetCook(entity)
			}
		}
	} else if entity.CookingSpent > 0 {
		// An unlit furnace with nothing to cook cools by 2 a tick, clamped to 0 and
		// the total (a loaded timer may overshoot it).
		// vanilla: AbstractFurnaceBlockEntity.BURN_COOL_SPEED
		entity.CookingSpent = min(max(entity.CookingSpent-2, 0), entity.CookingTotal)
		b.progressed = true
	}

	b.litNow = entity.LitRemaining > 0
	b.inputStack = entity.Slots[0]
	b.fuelStack = entity.Slots[1]
	b.outputStack = entity.Slots[2]
	return !b.progressed && !b.slotsMoved && !b.litNow && entity.CookingSpent == 0
}

// fuelLot is the burn time and speed multiplier of one item of fuel in a
// cooking block. A stack's own cooking_fuel resolves against the block; an
// item default is the generated plain time, which a blast furnace and a smoker
// burn in half the time at twice the speed. No fuel burns for 0 at the plain
// speed.
// vanilla: CookingFuel, the cooking context providers, predicate block/fast_cooking
func fuelLot(kind world.BlockEntityKind, fuel protocol.ItemStack) (burn int32, speed float32) {
	fast := kind == world.KindBlastFurnace || kind == world.KindSmoker
	if count, mult, own, _ := registry.StackCookingFuel(fuel); own {
		block := registry.BlockFurnace
		switch kind {
		case world.KindBlastFurnace:
			block = registry.BlockBlastFurnace
		case world.KindSmoker:
			block = registry.BlockSmoker
		}
		return rules.ResolveFuel(count, mult, block.DefaultState())
	}
	if !registry.HasCookingFuel(fuel) {
		return 0, 1
	}
	burn = registry.FuelBurnTicks(fuel.ItemID)
	if burn > 0 && fast {
		return burn / 2, 2
	}
	return burn, 1
}

// resetCook zeroes progress when nothing can cook: no ingredient, no fire,
// or a blocked output.
func (b *furnaceBurn) resetCook(entity *world.BlockEntity) {
	if entity.CookingSpent != 0 {
		entity.CookingSpent = 0
		b.progressed = true
	}
}

// wetSpongeBucketFill is the water bucket replacing an empty bucket in the
// fuel slot when a wet sponge finishes; false otherwise.
func wetSpongeBucketFill(input, fuel protocol.ItemStack) (protocol.ItemStack, bool) {
	if name := registry.Item(input.ItemID); name != registry.ItemWetSponge {
		return protocol.ItemStack{}, false
	}
	if name := registry.Item(fuel.ItemID); name != registry.ItemBucket {
		return protocol.ItemStack{}, false
	}
	return protocol.ItemStack{ItemID: int32(registry.ItemWaterBucket), Count: 1}, true
}

// spentFuel is the fuel slot after one item burns; a lava bucket leaves an
// empty bucket.
func spentFuel(fuel protocol.ItemStack) protocol.ItemStack {
	if name := registry.Item(fuel.ItemID); name == registry.ItemLavaBucket {
		return protocol.ItemStack{ItemID: int32(registry.ItemBucket), Count: 1}
	}
	left := fuel
	left.Count = fuel.Count - 1
	if left.Count <= 0 {
		return protocol.ItemStack{}
	}
	return left
}

// setJukeboxHasRecord sets has_record and tells the world, waking
// neighbours so comparators and wire read it; it does not change light, so
// no relight. vanilla: JukeboxBlockEntity.notifyItemChangedInJukebox
func (s *Server) setJukeboxHasRecord(w *world.World, x, y, z int32, has bool) {
	state := w.BlockAt(x, y, z)
	if registry.BlockOf(state) != registry.BlockJukebox {
		return
	}
	if registry.StateBool(state, registry.PropHasRecord) == has {
		return
	}
	newState, ok := registry.WithBool(state, registry.PropHasRecord, has)
	if !ok {
		return
	}
	if w.SetBlock(x, y, z, newState) {
		s.markChanged(w, x, y, z)
		s.blockChanged(w, x, y, z, state)
	}
}

// setBookshelfState redraws a chiseled bookshelf from its slots: six
// occupied bits, plus the last-touched cell (kept under the entity's lock),
// and the block change wakes its comparators.
// vanilla: ChiseledBookShelfBlockEntity.updateState
func (s *Server) setBookshelfState(w *world.World, be *world.BlockEntity, last int) {
	state := w.BlockAt(be.X, be.Y, be.Z)
	if registry.BlockOf(state) != registry.BlockChiseledBookshelf {
		return
	}
	var values []registry.PropValue
	be.Use(func(b *world.BlockEntity) {
		b.LastInteractedSlot = last
		values = values[:0]
		for slot := range b.Slots {
			values = append(values, registry.PropValue{P: registry.BookshelfSlotProps[slot], V: strconv.FormatBool(!b.Slots[slot].Empty())})
		}
	})
	newState, ok := registry.WithProps(state, values...)
	if !ok {
		return
	}
	if newState != state {
		s.setBlockAndTell(w, be.X, be.Y, be.Z, newState)
	}
	s.gameEvent(w, rules.GameEventBlockChange, float64(be.X)+0.5, float64(be.Y)+0.5, float64(be.Z)+0.5, rules.GameEventContext{Affected: newState})
}

// syncBookshelfState reconciles a chiseled bookshelf after a hopper move:
// the flipped cell becomes the last one, and agreeing bits change nothing.
// Call it with no lock held.
func (s *Server) syncBookshelfState(w *world.World, be *world.BlockEntity) {
	state := w.BlockAt(be.X, be.Y, be.Z)
	if registry.BlockOf(state) != registry.BlockChiseledBookshelf {
		return
	}
	moved := -1
	be.Use(func(b *world.BlockEntity) {
		for slot := range b.Slots {
			if !b.Slots[slot].Empty() != registry.StateBool(state, registry.BookshelfSlotProps[slot]) {
				moved = slot
				return
			}
		}
	})
	if moved < 0 {
		return
	}
	s.setBookshelfState(w, be, moved)
}

// signalNeighborsChanged wakes what a block's signal reaches when it changed
// with no block change, as a jukebox's song ending on its own (has_record
// kept) or a test block's start.
func (s *Server) signalNeighborsChanged(w *world.World, x, y, z int32) {
	changes, effects, primes := s.physicsFor(w).SignalChanged(x, y, z)
	for _, e := range effects {
		s.tellEffect(w, e)
	}
	for _, t := range primes {
		s.primeTNT(w, t)
	}
	s.tellBlockChanges(w, changes)
}

// tickJukebox advances a song a tick, stops it once its length plus the end
// delay passes with the disc still in, and pops a note particle every 20
// ticks. The stop's level event and wake wait for the lock to drop.
// vanilla: JukeboxSongPlayer.tick
func (s *Server) tickJukebox(w *world.World, be *world.BlockEntity) {
	stopped, note, ran := false, false, false
	// UseLive: a break or unload since the list was read already stopped the
	// song and dropped the disc.
	be.UseLive(func(b *world.BlockEntity) {
		ran = true
		if _, ok := b.JukeboxSong(); !ok || b.TicksSinceSongStarted < 0 {
			return
		}
		if b.JukeboxFinished() {
			// The song ran out: vanilla clears it and sends the stop.
			b.TicksSinceSongStarted = -1
			stopped = true
			return
		}
		// The particle comes before the tick is counted, so a new song pops one at
		// once.
		note = b.TicksSinceSongStarted%20 == 0
		b.TicksSinceSongStarted++
	})
	if !ran {
		return
	}
	if !stopped {
		if note {
			// An allay that arrived after the start still hears the song.
			// vanilla: JukeboxSongPlayer.tick (JUKEBOX_PLAY every 20 ticks)
			s.allaysHearJukebox(w, be.X, be.Y, be.Z, true)
			// vanilla: JukeboxSongPlayer.spawnMusicParticles
			f := float32(s.levelRng.Intn(4)) / 24
			s.sendParticles(w, protocol.ParticleNote,
				float64(be.X)+0.5, float64(be.Y)+1.2, float64(be.Z)+0.5, f, 0, 0, 1.0, 0)
		}
		return
	}
	s.broadcastLevelEvent(w,
		protocol.WorldEventJukeboxStop, be.X, be.Y, be.Z, 0)

	s.signalNeighborsChanged(w, be.X, be.Y, be.Z)
	w.Touch(be.X, be.Y, be.Z)
}

// syncJukeboxRecord reconciles a jukebox after a hopper move: an arrived
// disc starts its song, a removed one stops it, has_record follows. Call it
// with no lock held.
func (s *Server) syncJukeboxRecord(w *world.World, be *world.BlockEntity) {
	state := w.BlockAt(be.X, be.Y, be.Z)
	if registry.BlockOf(state) != registry.BlockJukebox {
		return
	}
	hasRecord := registry.StateBool(state, registry.PropHasRecord)
	var disc protocol.ItemStack
	songSet := false
	song, playable := "", false
	be.Use(func(b *world.BlockEntity) {
		disc = b.Slots[0]
		song, playable = registry.JukeboxSongForStack(disc)
		// A song runs while its ticks are set, even with the disc gone (as a hopper
		// pull leaves it).
		songSet = b.TicksSinceSongStarted >= 0
	})
	if playable {
		if hasRecord {
			return
		}
		be.Use(func(b *world.BlockEntity) { b.TicksSinceSongStarted = 0 })
		s.setJukeboxHasRecord(w, be.X, be.Y, be.Z, true)
		s.broadcastLevelEvent(w,
			protocol.WorldEventJukeboxPlay, be.X, be.Y, be.Z, registry.JukeboxSongID(song))

		return
	}
	if hasRecord {
		be.Use(func(b *world.BlockEntity) { b.TicksSinceSongStarted = -1 })
		s.setJukeboxHasRecord(w, be.X, be.Y, be.Z, false)
		if songSet {
			s.broadcastLevelEvent(w,
				protocol.WorldEventJukeboxStop, be.X, be.Y, be.Z, 0)

		}
	}
}

// setFurnaceLit sets a cooking block's lit property, keeping the others,
// and tells the world.
func (s *Server) setFurnaceLit(w *world.World, x, y, z int32, lit bool) {
	state := w.BlockAt(x, y, z)
	if !isCookingBlock(registry.BlockOf(state)) {
		return
	}
	if registry.StateBool(state, registry.PropLit) == lit {
		return
	}
	newState, ok := registry.WithBool(state, registry.PropLit, lit)
	if !ok {
		return
	}
	s.setBlockAndTell(w, x, y, z, newState)
}

// tellFurnaceViewers sends a tick's changes to the furnace's window
// viewers: moved slots, plus fire and progress always.
func (s *Server) tellFurnaceViewers(w *world.World, step furnaceBurn) {
	s.containerViewers(w, step.x, step.y, step.z, nil, func(c *client) {
		window := c.openContainerWindow()
		if window == nil || !window.kind.Cooking() {
			return
		}
		if step.slotsMoved {
			c.queueContainerSlot(window.id, 0, step.inputStack)
			c.queueContainerSlot(window.id, 1, step.fuelStack)
			c.queueContainerSlot(window.id, 2, step.outputStack)
		}
		c.queueContainerProperty(window.id, protocol.FurnacePropertyFuelLeft, step.be.LitRemaining)
		c.queueContainerProperty(window.id, protocol.FurnacePropertyFuelTotal, step.be.LitTotal)
		c.queueContainerProperty(window.id, protocol.FurnacePropertyProgress, step.be.CookingSpent)
		c.queueContainerProperty(window.id, protocol.FurnacePropertyProgressTotal, step.be.CookingTotal)
	})
}

// brewingTicks is a brew's duration before it pours, at the plain speed.
// vanilla: BrewingStandBlockEntity.serverTick (400 = 20 seconds)
const brewingTicks = 400

// brewingStep is one brewing stand tick: computed under the block entity's
// lock, acted on (bottles drawn, sound, window) after.
type brewingStep struct {
	be *world.BlockEntity
	// events asks the plugin listeners; nil when none of them is registered.
	events  *blockEntityEvents
	x, y, z int32
	// slots are the five slots after the tick, sent to the window when any
	// moved; bottles is which bottle slots are filled (the has_bottle bits);
	// drops are remainders the ingredient slot could not keep.
	slots   [5]protocol.ItemStack
	bottles [3]bool
	drops   []protocol.ItemStack
	// brewed: the last tick poured. slotsMoved: a visible slot changed.
	// progressed: countdown or fuel moved. Either of the last two is a vanilla
	// setChanged. totalsMoved: a fuel loaded or a brew started, so a gauge's
	// full mark changed.
	brewed      bool
	slotsMoved  bool
	progressed  bool
	totalsMoved bool
	// bottlesMoved: the has_bottle bits changed, so a block update is due.
	// vanilla: BrewingStandBlockEntity.lastPotionCount
	bottlesMoved bool
	// ran is false when Tick found the stand gone: nothing was read and x,
	// y, z name nothing to act on.
	ran bool
}

// advance runs a brewing tick and reports whether it can sleep until a write.
// vanilla: BrewingStandBlockEntity.serverTick
func (step *brewingStep) advance(entity *world.BlockEntity) bool {
	step.x, step.y, step.z = entity.X, entity.Y, entity.Z
	step.ran = true

	// The fuel loads whatever the bottles hold.
	if entity.Fuel <= 0 {
		if fuel := entity.Slots[4]; inventory.IsBrewingFuel(fuel) {
			uses, speed := registry.BrewingFuelUses(fuel.ItemID), float32(1)
			// The generated fuel table carries only fuels of the plain speed.
			if count, mult, own, _ := registry.StackBrewingFuel(fuel); own {
				uses, speed = rules.ResolveFuel(count, mult, registry.BlockBrewingStand.DefaultState())
			}
			entity.Fuel = int(uses)
			entity.FuelTotal = entity.Fuel
			entity.SpeedMultiplier = speed
			entity.Put(4, oneFewer(fuel))
			step.slotsMoved, step.progressed, step.totalsMoved = true, true, true
		}
	}

	brewable := inventory.Brewable(entity.Slots)
	ingredient := entity.Slots[3]
	switch {
	case entity.BrewTime > 0:
		entity.BrewTime--
		if entity.BrewTime == 0 && brewable {
			// Pour into every bottle slot a mix fits, spending the ingredient.
			// vanilla: BrewingStandBlockEntity.doBrew
			var mixed [3]protocol.ItemStack
			for slot := range mixed {
				mixed[slot] = inventory.BrewingMix(ingredient, entity.Slots[slot])
			}
			poured := true
			if step.events != nil {
				mixed, poured = step.events.brew(entity, ingredient, mixed)
			}
			if poured {
				for slot, mix := range mixed {
					entity.Put(slot, mix)
				}
				slot, drop := spentIngredient(ingredient)
				entity.Put(3, slot)
				if !drop.Empty() {
					step.drops = append(step.drops, drop)
				}
				step.brewed = true
				step.slotsMoved = true
			}
		} else if !brewable || ingredient.ItemID != entity.BrewingIngredient {
			entity.BrewTime = 0
		}
		step.progressed = true
	case brewable && entity.Fuel > 0:
		entity.Fuel--
		entity.BrewTime = int(world.ScaledTicks(brewingTicks, entity.SpeedMultiplier))
		entity.BrewTotal = entity.BrewTime
		entity.BrewingIngredient = ingredient.ItemID
		step.progressed, step.totalsMoved = true, true
	}

	if step.slotsMoved || step.progressed {
		copy(step.slots[:], entity.Slots)
	}
	for slot := range entity.Slots[:3] {
		step.bottles[slot] = !entity.Slots[slot].Empty()
	}
	step.bottlesMoved = entity.BrewingBottles == nil || step.bottles != *entity.BrewingBottles
	if step.bottlesMoved {
		bottles := step.bottles
		entity.BrewingBottles = &bottles
	}
	return entity.BrewTime <= 0 && !step.progressed && !step.slotsMoved && !step.bottlesMoved
}

// oneFewer is the stack minus one, empty when it was the last.
func oneFewer(stack protocol.ItemStack) protocol.ItemStack {
	if stack.Count <= 1 {
		return protocol.ItemStack{}
	}
	stack.Count--
	return stack
}

// spentIngredient is the ingredient slot and drop after a brew spends one:
// one fewer, or the item's crafting remainder if that empties it. A
// remainder with ingredient left (only dragon breath has one) drops at the
// stand rather than stacking. vanilla: BrewingStandBlockEntity.doBrew
func spentIngredient(ingredient protocol.ItemStack) (slot, drop protocol.ItemStack) {
	remainder := registry.CraftingRemainder(ingredient.ItemID)
	if ingredient.Count <= 1 {
		return remainder, protocol.ItemStack{}
	}
	ingredient.Count--
	return ingredient, remainder
}

// setBrewingBottles sets has_bottle_0..2 from the three bottle slots. Only
// called when the bits changed (vanilla's lastPotionCount check); vanilla
// writes with flag 2, so the update goes out without waking neighbours.
// vanilla: BrewingStandBlockEntity.serverTick
func (s *Server) setBrewingBottles(w *world.World, x, y, z int32, has [3]bool) {
	state := w.BlockAt(x, y, z)
	if registry.BlockOf(state) != registry.BlockBrewingStand {
		return
	}
	values := make([]registry.PropValue, len(has))
	for slot, present := range has {
		values[slot] = registry.PropValue{P: registry.BrewingBottleProps[slot], V: strconv.FormatBool(present)}
	}
	newState, ok := registry.WithProps(state, values...)
	if !ok {
		return
	}
	s.writeBlock(w, x, y, z, newState, nil, writeNeighborShapes)
}

// tellBrewingViewers sends a tick's changes to the stand's window viewers:
// moved slots, plus the countdown and fuel always.
func (s *Server) tellBrewingViewers(w *world.World, step brewingStep) {
	s.containerViewers(w, step.x, step.y, step.z, nil, func(c *client) {
		window := c.openContainerWindow()
		if window == nil || window.kind != windowBrewingStand {
			return
		}
		if step.slotsMoved {
			for slot, stack := range step.slots {
				c.queueContainerSlot(window.id, int32(slot), stack)
			}
		}
		c.queueContainerProperty(window.id, protocol.BrewingPropertyBrewTime, int32(step.be.BrewTime))
		c.queueContainerProperty(window.id, protocol.BrewingPropertyFuel, int32(step.be.Fuel))
		if step.totalsMoved {
			c.queueContainerProperty(window.id, protocol.BrewingPropertyBrewTotal, int32(step.be.BrewTotal))
			c.queueContainerProperty(window.id, protocol.BrewingPropertyFuelTotal, int32(step.be.FuelTotal))
		}
	})
}

// teleportDragonEgg moves the egg within the world border and tells
// clients. vanilla: DragonEggBlock.teleport
func (c *client) teleportDragonEgg(x, y, z int32) {
	w := c.theWorld()
	changes, to := c.srv.physicsFor(w).DragonEggTeleport(x, y, z, c.dimension().border.ContainsBlock)
	if len(changes) == 0 {
		return
	}
	c.srv.broadcastLevelEvent(w, protocol.WorldEventDragonEggTeleport, x, y, z,
		protocol.PackPosDiff(x, y, z, to[0], to[1], to[2], 16, 8, 16))
	c.srv.tellBlockChanges(w, changes)
}

// useRedstoneOre lights clicked redstone ore, reporting whether the click
// ends there: a held block item goes on to be placed, any other hand ends it.
// vanilla: RedStoneOreBlock.useItemOn
func (c *client) useRedstoneOre(hand, x, y, z int32) bool {
	if !rules.IsRedstoneOre(c.theWorld().BlockAt(x, y, z)) {
		return false
	}
	c.lightRedstoneOre(x, y, z)
	held := c.heldItem(hand)
	if held.Count <= 0 {
		return true
	}
	_, isBlock := registry.Item(held.ItemID).Block()
	return !isBlock
}

// lightRedstoneOre lights the ore and sends the change and its light.
func (c *client) lightRedstoneOre(x, y, z int32) {
	c.srv.lightRedstoneOre(c.theWorld(), x, y, z)
}

// lightRedstoneOre lights the ore in w, if any, and sends the change and its
// light.
func (s *Server) lightRedstoneOre(w *world.World, x, y, z int32) {
	if changes := s.physicsFor(w).TouchRedstoneOre(x, y, z); len(changes) > 0 {
		s.tellBlockChanges(w, changes)
	}
}

// heldHangingSign reports whether hand holds a hanging sign item.
func (c *client) heldHangingSign(hand int32) bool {
	b, ok := registry.Item(c.heldItem(hand).ItemID).Block()
	return ok && b.InTag(registry.TagIDAllHangingSigns)
}

// signHasClicks reports whether the sign at x,y,z is waxed and the side
// facing the player has a click event on any line.
// vanilla: SignBlockEntity.canExecuteClickCommands
func (c *client) signHasClicks(x, y, z int32, state uint16) bool {
	be := c.theWorld().BlockEntity(x, y, z)
	if be == nil {
		return false
	}
	pos := c.player.Position()
	front := rules.SignFrontFacing(state, x, z, pos.X, pos.Z)
	var events [4]nbt.Compound
	var waxed bool
	be.Use(func(b *world.BlockEntity) { _, events = b.SignClicks(front); waxed = b.Waxed })
	if !waxed {
		return false
	}
	for _, e := range events {
		if _, ok := nbt.Get[nbt.String](e, "action"); ok {
			return true
		}
	}
	return false
}
