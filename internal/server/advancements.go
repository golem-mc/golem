package server

import (
	"math"
	"time"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/advancement"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/inventory"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// BroadcastItemChanges fires inventory criteria and announces their rewards
// before the command that changed the slots answers.
// vanilla: EntityItemAccessor.setItems, ServerPlayer.containerListener
func (c *client) BroadcastItemChanges() {
	progress := c.advancements.Load()
	if progress == nil {
		return
	}
	c.inventoryChangedAdvancements(progress)
	c.AnnounceAdvancements()
}

// tickInventoryAdvancements fires inventory_changed when the inventory
// differs from the last broadcast, and enter_block for a water cell the
// body is inside. Vanilla's menus fire it off slots changed since their last
// broadcast; one whole-inventory read a tick catches the same arrivals.
// vanilla: InventoryChangeTrigger at the menus' broadcastChanges,
// ServerPlayer.onInsideBlock
func (c *client) tickInventoryAdvancements() {
	progress := c.advancements.Load()
	if progress == nil {
		return
	}
	c.inventoryChangedAdvancements(progress)
	// vanilla: Entity.isAffectedByBlocks
	if c.player.Vitals().Dead || c.player.GameMode() == protocol.GameModeSpectator {
		return
	}
	if x, y, z, ok := c.waterInside(); ok {
		c.enteredBlockAdvancements(x, y, z)
	}
}

// inventoryChangedAdvancements shares the slot-change gate between commands
// and the tick, excluding storage, the cursor and the crafting grid.
// vanilla: AbstractContainerMenu.triggerSlotListeners, ServerPlayer.containerListener
func (c *client) inventoryChangedAdvancements(progress *advancement.Progress) {
	c.inventoryAdvancementMu.Lock()
	defer c.inventoryAdvancementMu.Unlock()
	var inv advancement.Inventory
	changed := false
	c.player.Inventory(func(pi *player.Inventory) {
		slots := pi.Slots[player.ArmorSlot:]
		changedItems := c.changedScratch[:0]
		changed = len(slots) != len(c.lastInventory)
		for i, stack := range slots {
			if i < len(c.lastInventory) {
				previous := c.lastInventory[i]
				if stack.Empty() && previous.Empty() || stack.Equal(previous) {
					continue
				}
			}
			changed = true
			if !stack.Empty() {
				changedItems = append(changedItems, stack.ItemID)
			}
		}
		if changed {
			c.lastInventory = append(c.lastInventory[:0], slots...)
			inv = advancement.InventoryOf(slots, c.heldScratch)
			inv.Changed = changedItems
			c.heldScratch = inv.Held
			c.changedScratch = inv.Changed
		}
	})
	if changed {
		progress.InventoryChanged(inv, time.Now())
	}
}

// waterInside finds a water block among the cells the body's box overlaps,
// the only block besides the end gateway that vanilla's criteria name.
// vanilla: Entity.checkInsideBlocks over the box
func (c *client) waterInside() (x, y, z int32, ok bool) {
	pos := c.player.Position()
	dims := c.dims(c.player.Appearance().Pose)
	w := c.theWorld()
	halfWidth := dims.Width / 2
	minX, maxX := rules.CellRange(pos.X-halfWidth, pos.X+halfWidth)
	minY, maxY := rules.CellRange(pos.Y, pos.Y+dims.Height)
	minZ, maxZ := rules.CellRange(pos.Z-halfWidth, pos.Z+halfWidth)
	for y := minY; y <= maxY; y++ {
		for z := minZ; z <= maxZ; z++ {
			for x := minX; x <= maxX; x++ {
				if rules.IsWaterBlock(w.BlockAt(x, y, z)) {
					return x, y, z, true
				}
			}
		}
	}
	return 0, 0, 0, false
}

// changedDimensionAdvancements fires changed_dimension for a crossing.
func (c *client) changedDimensionAdvancements(from, to string) {
	if progress := c.advancements.Load(); progress != nil {
		progress.ChangedDimension(from, to, time.Now())
	}
}

// advancementEntity names a mob or player for entity predicates: type and
// dimension.
func (s *Server) advancementEntity(kind string, w *world.World) advancement.Entity {
	return advancement.Entity{Type: kind, Dimension: s.dimensionOfWorld(w).name}
}

// killedEntityAdvancements fires player_killed_entity for a kill credited to
// the player. vanilla: ServerPlayer.awardKillScore
func (c *client) killedEntityAdvancements(victim advancement.Entity) {
	if progress := c.advancements.Load(); progress != nil {
		progress.PlayerKilledEntity(victim, time.Now())
	}
}

// generatedLootAdvancements fires player_generates_container_loot for a table
// the player's open or brush rolled; an empty table (none pending) is no roll.
// vanilla: RandomizableContainer.unpackLootTable
func (c *client) generatedLootAdvancements(table string) {
	if table == "" {
		return
	}
	if progress := c.advancements.Load(); progress != nil {
		progress.GeneratedContainerLoot(table, time.Now())
	}
}

// heroOfTheVillageAdvancements fires hero_of_the_village for a player a won
// raid rewards. vanilla: Raid.tick
func (c *client) heroOfTheVillageAdvancements() {
	if progress := c.advancements.Load(); progress != nil {
		progress.HeroOfTheVillage(time.Now())
	}
}

// raidOmenAdvancements fires voluntary_exile for a player whose raid omen a
// raid absorbed before its first wave. vanilla: Raid.absorbRaidOmen
func (c *client) raidOmenAdvancements() {
	if progress := c.advancements.Load(); progress != nil {
		progress.RaidOmen(time.Now())
	}
}

// usedEnderEyeAdvancements fires used_ender_eye for an eye thrown toward a
// structure dx, dz blocks from the player. vanilla: EnderEyeItem.use
func (c *client) usedEnderEyeAdvancements(dx, dz float64) {
	if progress := c.advancements.Load(); progress != nil {
		progress.UsedEnderEye(dx*dx+dz*dz, time.Now())
	}
}

// beeNestDestroyedAdvancements fires bee_nest_destroyed for a hive block the
// player broke, with the tool's enchantments and the bees stored inside.
// vanilla: BeehiveBlock.playerDestroy
func (c *client) beeNestDestroyedAdvancements(block string, enchantments map[int32]int32, bees int) {
	if progress := c.advancements.Load(); progress != nil {
		progress.BeeNestDestroyed(block, enchantments, bees, time.Now())
	}
}

// avoidVibrationAdvancements fires avoid_vibration for a sneaking player
// whose step a sculk sensor in range would have heard.
// vanilla: VibrationSystem.User.isValidVibration
func (c *client) avoidVibrationAdvancements() {
	if progress := c.advancements.Load(); progress != nil {
		progress.AvoidVibration(time.Now())
	}
}

// honeySlideAdvancements fires slide_down_block on every 20th game tick a
// falling player's box overlaps a honey block's side below its top.
// The fall speed is the step's drop; vanilla reads the server's delta movement.
// vanilla: HoneyBlock.isSlidingDown, HoneyBlock.maybeDoSlideAchievement
func (c *client) honeySlideAdvancements(was, now player.Position) {
	progress := c.advancements.Load()
	if progress == nil || !rules.HoneyFalling(now.Y-was.Y, now.OnGround) {
		return
	}
	if gameTime, _ := c.srv.world.Time(); gameTime%20 != 0 {
		return
	}
	dims := c.dims(c.player.Appearance().Pose)
	half := dims.Width / 2
	w := c.theWorld()
	minX, maxX := rules.CellRange(now.X-half, now.X+half)
	minY, maxY := rules.CellRange(now.Y, now.Y+dims.Height)
	minZ, maxZ := rules.CellRange(now.Z-half, now.Z+half)
	for y := minY; y <= maxY; y++ {
		for z := minZ; z <= maxZ; z++ {
			for x := minX; x <= maxX; x++ {
				if rules.IsHoneyBlock(w.BlockAt(x, y, z)) &&
					rules.HoneySliding(x, y, z, now.X, now.Y, now.Z, now.Y-was.Y, dims.Width, now.OnGround) {
					progress.SlideDownBlock(time.Now())
					return
				}
			}
		}
	}
}

// killedNearSculkCatalystAdvancements fires kill_mob_near_sculk_catalyst
// for the player who last hurt a catalyst-fed mob.
// vanilla: CatalystListener.tryAwardItSpreadsAdvancement
func (c *client) killedNearSculkCatalystAdvancements() {
	if progress := c.advancements.Load(); progress != nil {
		progress.KillMobNearSculkCatalyst(time.Now())
	}
}

// killedByEntityAdvancements fires entity_killed_player for a death credited
// to an entity. vanilla: Entity.awardKillScore
func (c *client) killedByEntityAdvancements() {
	if progress := c.advancements.Load(); progress != nil {
		progress.EntityKilledPlayer(time.Now())
	}
}

// tickLocationAdvancements fires tick every tick, levitation every tick from where the
// Levitation took hold, ride_entity_in_lava from where a ride entered lava,
// and location once a second for the living player's
// biome and structure pieces. vanilla: ServerPlayer.tick, doTick
func (c *client) tickLocationAdvancements() {
	progress := c.advancements.Load()
	if progress == nil {
		return
	}
	v := c.player.Vitals()
	pos := c.player.Position()
	progress.Tick(time.Now())
	if v.LevitationStart != nil {
		progress.Moved(advancement.TriggerLevitation, *v.LevitationStart, point(pos), time.Now())
	}
	if v.LavaRideStart != nil || c.mount.Load() != nil {
		c.lavaRideAdvancements(progress, v, pos)
	}
	if v.Dead || v.AgeTicks%20 != 0 {
		return
	}
	w := c.theWorld()
	x, y, z := rules.BlockOf(pos.X), rules.BlockOf(pos.Y), rules.BlockOf(pos.Z)
	dims := c.dims(c.player.Appearance().Pose)
	on := w.BlockAt(rules.StandingBlock(w, pos.X, pos.Y, pos.Z, dims.Width/2))
	standing := advancement.Standing{Feet: c.boots().ItemID, OnGround: pos.OnGround, On: on}
	progress.Location(w.BiomeAt(x, y, z), func(ids []string) bool {
		return w.StructureHasPieceAt(x, y, z, ids)
	}, standing, time.Now())
}

// consumedItemAdvancements fires consume_item for a finished eat or drink.
// vanilla: Consumable.onConsume
func (c *client) consumedItemAdvancements(item int32) {
	if progress := c.advancements.Load(); progress != nil {
		progress.ConsumeItem(item, time.Now())
	}
}

// enchantedItemAdvancements fires enchanted_item for a successful table
// press. vanilla: EnchantmentMenu.clickMenuButton
func (c *client) enchantedItemAdvancements() {
	if progress := c.advancements.Load(); progress != nil {
		progress.EnchantedItem(time.Now())
	}
}

// bredAnimalsAdvancements fires bred_animals for the player whose feeding
// caused love. vanilla: Animal.finalizeSpawnChildFromBreeding
func (c *client) bredAnimalsAdvancements(parent, partner, child string) {
	if progress := c.advancements.Load(); progress != nil {
		progress.BredAnimals(parent, partner, child, time.Now())
	}
}

// blocksAround reads the blocks around a position for a location condition.
func blocksAround(w *world.World, x, y, z int32) advancement.Blocks {
	return func(dx, dy, dz int32) uint16 { return w.BlockAt(x+dx, y+dy, z+dz) }
}

// placedBlockAdvancements fires placed_block for a block the player placed.
// vanilla: BlockItem.place
func (c *client) placedBlockAdvancements(w *world.World, x, y, z, tool int32) {
	if progress := c.advancements.Load(); progress != nil {
		progress.PlacedBlock(blocksAround(w, x, y, z), tool, time.Now())
	}
}

// itemUsedOnBlockAdvancements fires item_used_on_block for a successful
// use, with the item held at its start. vanilla: ServerPlayerGameMode.useItemOn
func (c *client) itemUsedOnBlockAdvancements(w *world.World, x, y, z, tool int32) {
	if progress := c.advancements.Load(); progress != nil {
		progress.ItemUsedOnBlock(blocksAround(w, x, y, z), tool, w.BiomeAt(x, y, z), time.Now())
	}
}

// blockUseAdvancements fires default_block_use for a click the block's own
// use answered, then any_block_use for one any use consumed, with the hand's
// item as the use left it.
// vanilla: ServerPlayerGameMode.useItemOn, ServerGamePacketListenerImpl.handleUseItemOn
func (c *client) blockUseAdvancements(w *world.World, x, y, z, tool int32) {
	progress := c.advancements.Load()
	if progress == nil {
		return
	}
	if c.defaultBlockUsed {
		progress.DefaultBlockUse(blocksAround(w, x, y, z), time.Now())
	}
	if c.itemUsedOn || c.blockUseConsumed {
		progress.AnyBlockUse(blocksAround(w, x, y, z), tool, time.Now())
	}
}

// recipeUnlockedAdvancements fires recipe_unlocked for each recipe newly
// added to the book. vanilla: ServerRecipeBook.addRecipes
func (c *client) recipeUnlockedAdvancements(names []string) {
	if progress := c.advancements.Load(); progress != nil {
		for _, name := range names {
			progress.RecipeUnlocked(name, time.Now())
		}
	}
}

// tameAnimalAdvancements fires tame_animal; variant is a cat's or wolf's,
// empty otherwise. vanilla: TamableAnimal.tame, AbstractHorse.tameWithName
func (c *client) tameAnimalAdvancements(animal, variant string) {
	if progress := c.advancements.Load(); progress != nil {
		progress.TameAnimal(animal, variant, time.Now())
	}
}

// interactedWithEntityAdvancements fires player_interacted_with_entity;
// held is the item the interaction used, 0 for none.
// vanilla: ServerGamePacketListenerImpl.handleInteract
func (c *client) interactedWithEntityAdvancements(e advancement.EntityInteraction, held int32) {
	if progress := c.advancements.Load(); progress != nil {
		progress.InteractedWithEntity(e, held, c.wornArmourIDs(), time.Now())
	}
}

// shearedEquipmentAdvancements fires player_sheared_equipment for the item
// taken off. vanilla: Mob.shearItem
func (c *client) shearedEquipmentAdvancements(e advancement.EntityInteraction, item int32) {
	if progress := c.advancements.Load(); progress != nil {
		progress.ShearedEquipment(e, item, c.wornArmourIDs(), time.Now())
	}
}

// thrownItemPickedUpAdvancements fires the pickup of an item thrown by the
// entity thrower in w: thrown_item_picked_up_by_entity for a throwing player,
// with collector who picked it up, then thrown_item_picked_up_by_player for
// collecting, the collector when a player (nil for a mob). No lock may be
// held. vanilla: LivingEntity.onItemPickup, ServerPlayer.onItemPickup
func (s *Server) thrownItemPickedUpAdvancements(w *world.World, thrower uuid.UUID, item int32, collecting *client, collector advancement.Entity) {
	if thrower == uuid.Nil {
		return
	}
	var by advancement.Entity
	throwing := s.clientByUUID(thrower)
	if throwing != nil && throwing.theWorld() == w {
		by.Type = playerEntityType
		throwing.pickedUpAdvancements(true, item, collector)
	} else {
		throwing = nil
		s.mobsMu.Lock()
		if m := s.dimensionOfWorld(w).mobs.byUUID[thrower]; m != nil {
			by = advancement.Entity{Type: m.Type.Name(), Baby: m.IsBaby()}
		}
		s.mobsMu.Unlock()
	}
	// vanilla resolves the thrower among the level's entities; one gone or
	// elsewhere fires nothing.
	if collecting != nil && (throwing != nil || by.Type != "") {
		collecting.pickedUpAdvancements(false, item, by)
	}
}

// playerEntityType is the player's entity type name.
const playerEntityType = "minecraft:player"

// pickedUpAdvancements fires thrown_item_picked_up_by_entity (byEntity) or
// thrown_item_picked_up_by_player with the other side of the pickup.
func (c *client) pickedUpAdvancements(byEntity bool, item int32, other advancement.Entity) {
	progress := c.advancements.Load()
	if progress == nil {
		return
	}
	progress.ThrownItemPickedUp(byEntity, item, other, c.wornArmourIDs(), time.Now())
}

// wornArmourIDs is the item IDs of the player's worn armour pieces.
func (c *client) wornArmourIDs() []int32 {
	var worn []int32
	for _, piece := range c.wornArmour() {
		if !piece.Empty() {
			worn = append(worn, piece.ItemID)
		}
	}
	return worn
}

// allayDropAdvancements fires allay_drop_item_on_block for the player an
// allay's delivery throw is for, while in survival or creative. No lock may
// be held. vanilla: AllayAi.throwItem, getLikedPlayer
func (s *Server) allayDropAdvancements(w *world.World, ev entity.MobEvent) {
	c := s.clientByUUID(ev.Liked)
	if c == nil || c.theWorld() != w {
		return
	}
	if mode := c.player.GameMode(); mode != protocol.GameModeSurvival && mode != protocol.GameModeCreative {
		return
	}
	if progress := c.advancements.Load(); progress != nil {
		progress.AllayDropItemOnBlock(blocksAround(w, ev.DropOn[0], ev.DropOn[1], ev.DropOn[2]), ev.Stack.ItemID, time.Now())
	}
}

// filledBucketAdvancements fires filled_bucket for the bucket a scoop gave.
// vanilla: BucketItem.use
func (c *client) filledBucketAdvancements(item int32) {
	if progress := c.advancements.Load(); progress != nil {
		progress.FilledBucket(item, time.Now())
	}
}

// effectsChangedAdvancements fires effects_changed with the player's
// effects and their source after a change. vanilla: ServerPlayer.onEffectAdded
func (c *client) effectsChangedAdvancements(source string) {
	progress := c.advancements.Load()
	if progress == nil {
		return
	}
	active := c.player.Effects()
	names := make([]string, len(active))
	for i, in := range active {
		names[i] = registry.MobEffectName(in.ID)
	}
	progress.EffectsChangedFrom(names, source, time.Now())
}

// summonedEntityAdvancements fires summoned_entity for a golem the player's
// block built. vanilla: CarvedPumpkinBlock.trySpawnGolem
func (c *client) summonedEntityAdvancements(kind string) {
	if progress := c.advancements.Load(); progress != nil {
		progress.SummonedEntity(kind, time.Now())
	}
}

// sleptInBedAdvancements fires slept_in_bed. vanilla: ServerPlayer.startSleepInBed
func (c *client) sleptInBedAdvancements() {
	if progress := c.advancements.Load(); progress != nil {
		progress.SleptInBed(time.Now())
	}
}

// shotCrossbowAdvancements fires shot_crossbow for a crossbow's shot.
// vanilla: CrossbowItem.performShooting
func (c *client) shotCrossbowAdvancements(item int32) {
	if progress := c.advancements.Load(); progress != nil {
		progress.ShotCrossbow(item, time.Now())
	}
}

// villagerTradeAdvancements fires villager_trade for a taken trade result.
// vanilla: AbstractVillager.notifyTrade
func (c *client) villagerTradeAdvancements() {
	if progress := c.advancements.Load(); progress != nil {
		progress.VillagerTrade(c.player.Position().Y, time.Now())
	}
}

// constructBeaconAdvancements fires construct_beacon for a beacon newly
// active at level. vanilla: BeaconBlockEntity.tick
func (c *client) constructBeaconAdvancements(level int32) {
	if progress := c.advancements.Load(); progress != nil {
		progress.ConstructBeacon(level, time.Now())
	}
}

// brewedPotionAdvancements fires brewed_potion for potions taken from a
// stand. vanilla: BrewingStandMenu.PotionSlot.onTake
func (c *client) brewedPotionAdvancements() {
	if progress := c.advancements.Load(); progress != nil {
		progress.BrewedPotion(time.Now())
	}
}

// fishingRodHookedAdvancements fires fishing_rod_hooked for a reel's catch.
// vanilla: FishingHook.retrieve
func (c *client) fishingRodHookedAdvancements(caught []int32) {
	if progress := c.advancements.Load(); progress != nil {
		progress.FishingRodHooked(caught, time.Now())
	}
}

// flushAdvancements sends advancement changes since last tick; the first
// after join resets the client's tree.
// vanilla: ServerPlayer.tick's PlayerAdvancements.flushDirty
func (c *client) flushAdvancements() {
	progress := c.advancements.Load()
	if progress == nil {
		return
	}
	c.AnnounceAdvancements()
	if update, ok := progress.FlushTick(); ok {
		c.send(protocol.IDPlayUpdateAdvancements, updateAdvancementsPayload(update))
	}
}

// sendAdvancementUpdate announces newly done advancements, then flushes
// progress and sends the packet.
func (c *client) sendAdvancementUpdate(progress *advancement.Progress, show bool) {
	c.AnnounceAdvancements()
	update, ok := progress.FlushShow(show)
	if !ok {
		return
	}
	c.send(protocol.IDPlayUpdateAdvancements, updateAdvancementsPayload(update))
}

// AnnounceAdvancements rewards and announces newly done advancements in
// chat, before the update showing them. vanilla: PlayerAdvancements.award
func (c *client) AnnounceAdvancements() {
	// A caller must wait for completions another caller already drained to
	// finish dispatching before it sends command feedback or progress.
	c.advancementAnnounceMu.Lock()
	defer c.advancementAnnounceMu.Unlock()
	progress := c.advancements.Load()
	if progress == nil {
		return
	}
	for {
		completed := progress.TakeCompleted()
		if len(completed) == 0 {
			return
		}
		var recipes []string
		for _, n := range completed {
			if n.Experience > 0 {
				c.GiveExperiencePoints(int(n.Experience))
			}
			for _, name := range n.Recipes {
				if _, ok := registry.RecipeDisplayIDOfName(name); ok {
					recipes = append(recipes, name)
				}
			}
			var msg *protocol.Text
			if n.Display != nil && n.Display.AnnounceChat && c.srv.world.BoolRule(world.RuleShowAdvancementMsgs) {
				msg = &protocol.Text{
					Translate: "chat.type.advancement." + advancementFrames[n.Display.Frame],
					With:      []protocol.Text{c.DisplayName(), command.AdvancementName(n)},
				}
			}
			if msg = c.pluginAdvancementDone(n, msg); msg != nil {
				c.srv.broadcastSystemMessage(*msg)
			}
		}
		// Recipe rewards can complete recipe_unlocked criteria. Drain those
		// here too, without recursively taking the dispatch lock.
		if len(recipes) > 0 {
			c.giveRecipes(recipes, true)
		}
	}
}

// advancementFrames names AdvancementType by ordinal.
var advancementFrames = [...]string{"task", "challenge", "goal"}

// handleSeenAdvancements handles the advancement screen opening a tab: a
// displayed root's tab other than the last selected is echoed back in
// Select Advancements Tab. Closing, or an unknown advancement, changes
// nothing. vanilla: ServerGamePacketListenerImpl.handleSeenAdvancements
func (c *client) handleSeenAdvancements(r *protocol.Reader) error {
	// vanilla: ServerboundSeenAdvancementsPacket.Action
	const openedTab = 0
	action := r.ReadVarInt()
	var tab string
	if action == openedTab {
		tab = registry.Qualify(r.ReadString(32767))
	}
	if err := r.Err(); err != nil {
		return c.decodeFailure("seen_advancements", err)
	}
	progress := c.advancements.Load()
	if action != openedTab || progress == nil {
		return nil
	}
	n := advancement.Current().Get(tab)
	if n == nil {
		return nil
	}
	if root, changed := progress.SelectTab(n); changed {
		c.send(protocol.IDPlaySelectAdvancementsTab, selectAdvancementsTabPayload(root))
	}
	return nil
}

// selectAdvancementsTabPayload names the tab's root, or none.
func selectAdvancementsTabPayload(root *advancement.Node) []byte {
	var b protocol.Buffer
	b.PutBool(root != nil)
	if root != nil {
		b.PutString(root.ID)
	}
	return b.Bytes()
}

// updateAdvancementsPayload builds Update Advancements; show advancements
// is clear only for /advancement grant everything's final flush.
// vanilla: ClientboundUpdateAdvancementsPacket.write
func updateAdvancementsPayload(u advancement.Update) []byte {
	var b protocol.Buffer
	b.PutBool(u.Reset)
	b.PutVarInt(int32(len(u.Added)))
	for _, n := range u.Added {
		b.PutString(n.ID)
		putAdvancement(&b, n)
		// vanilla: ClientboundUpdateAdvancementsPacket.PositionedAdvancement
		b.PutF32(n.X)
		b.PutF32(n.Y)
	}
	b.PutVarInt(int32(len(u.Removed)))
	for _, id := range u.Removed {
		b.PutString(id)
	}
	b.PutVarInt(int32(len(u.Progress)))
	for _, p := range u.Progress {
		b.PutString(p.Node.ID)
		b.PutVarInt(int32(len(p.Criteria)))
		for _, criterion := range p.Criteria {
			b.PutString(criterion.Name)
			obtained := !criterion.Obtained.IsZero()
			b.PutBool(obtained)
			if obtained {
				b.PutI64(criterion.Obtained.UnixMilli())
			}
		}
	}
	b.PutBool(u.Show)
	return b.Bytes()
}

// putAdvancement writes one advancement's network form.
// vanilla: Advancement.write, DisplayInfo.serializeToNetwork
func putAdvancement(b *protocol.Buffer, n *advancement.Node) {
	b.PutBool(n.Parent != "")
	if n.Parent != "" {
		b.PutString(n.Parent)
	}
	d := n.Display
	b.PutBool(d != nil)
	if d != nil {
		b.PutText(command.DisplayText(d.Title, d.TitleLiteral))
		b.PutText(command.DisplayText(d.Description, d.DescriptionLiteral))
		// The icon is an ItemStackTemplate: item, count, then an empty component
		// patch's two counts.
		b.PutVarInt(d.Icon)
		b.PutVarInt(d.IconCount)
		b.PutVarInt(0)
		b.PutVarInt(0)
		b.PutVarInt(d.Frame)
		var flags int32
		if d.Background != "" {
			flags |= 0x01
		}
		if d.ShowToast {
			flags |= 0x02
		}
		if d.Hidden {
			flags |= 0x04
		}
		b.PutI32(flags)
		if d.Background != "" {
			b.PutString(d.Background)
		}
	}
	b.PutVarInt(int32(len(n.Requirements)))
	for _, group := range n.Requirements {
		b.PutVarInt(int32(len(group)))
		for _, name := range group {
			b.PutString(name)
		}
	}
	b.PutBool(n.Telemetry)
}

// point is a position's coordinates.
func point(p player.Position) [3]float64 { return [3]float64{p.X, p.Y, p.Z} }

// fallBegan returns where an explosion hit the player and its cause when a
// fall begins at or above that point, and clears both once a fall begins.
// vanilla: ServerPlayer.trackStartFallingPosition
func fallBegan(v *player.Vitals, now player.Position) (impact [3]float64, cause string, ok bool) {
	if v.FallDistance <= 0 || v.FallStart != nil {
		return impact, "", false
	}
	if v.ExplosionImpact != nil && v.ExplosionImpact[1] <= now.Y {
		impact, cause, ok = *v.ExplosionImpact, v.ExplosionCause, true
	}
	v.ExplosionImpact, v.ExplosionCause = nil, ""
	return impact, cause, ok
}

// fellAfterExplosion fires fall_after_explosion for a fall that began at now.
func (c *client) fellAfterExplosion(impact [3]float64, cause string, now player.Position) {
	if progress := c.advancements.Load(); progress != nil {
		progress.FallAfterExplosion(impact, point(now), cause, time.Now())
	}
}

// fallEnded keeps where a fall began and returns it once the fall has
// ended, nil otherwise. vanilla: ServerPlayer.trackStartFallingPosition
func fallEnded(v *player.Vitals, now player.Position) *[3]float64 {
	switch {
	case v.FallDistance > 0 && v.FallStart == nil:
		v.FallStart = &[3]float64{now.X, now.Y, now.Z}
	case v.FallDistance == 0 && v.FallStart != nil:
		start := v.FallStart
		v.FallStart = nil
		return start
	}
	return nil
}

// fellAdvancements fires fall_from_height for a fall from start that ended
// at now, read after the landing's damage: a fall that kills fires none.
// vanilla: ServerPlayer.resetFallDistance
func (c *client) fellAdvancements(start [3]float64, now player.Position) {
	progress := c.advancements.Load()
	if v := c.player.Vitals(); progress == nil || v.Dead || v.Health <= 0 {
		return
	}
	progress.Moved(advancement.TriggerFallFromHeight, start, point(now), time.Now())
}

// levitationStarted keeps where Levitation took hold, or forgets it when it
// ended. vanilla: ServerPlayer.onEffectAdded, onEffectsRemoved
func (c *client) levitationStarted(started bool) {
	pos := point(c.player.Position())
	c.player.UpdateVitals(func(v *player.Vitals) {
		v.LevitationStart = nil
		if started {
			v.LevitationStart = &pos
		}
	})
}

// netherTravelAdvancements keeps where a crossing into the nether left the
// overworld and fires nether_travel from there on the way back; left is
// where the crossing left from.
// vanilla: ServerPlayer.teleport, triggerDimensionChangeTriggers
func (c *client) netherTravelAdvancements(from, to string, left [3]float64) {
	var entered *[3]float64
	c.player.UpdateVitals(func(v *player.Vitals) {
		entered = v.EnteredNether
		switch {
		case from == world.DimensionName && to == world.NetherName:
			v.EnteredNether = &left
		case to != world.NetherName:
			v.EnteredNether = nil
		}
	})
	if progress := c.advancements.Load(); progress != nil && entered != nil &&
		from == world.NetherName && to == world.DimensionName {
		progress.Moved(advancement.TriggerNetherTravel, *entered, point(c.player.Position()), time.Now())
	}
}

// enteredBlockAdvancements fires enter_block for the block at x, y, z, one
// the player's body is inside. Only an end gateway and water report it:
// vanilla's own criteria name no other block. vanilla: ServerPlayer.onInsideBlock
func (c *client) enteredBlockAdvancements(x, y, z int32) {
	if progress := c.advancements.Load(); progress != nil {
		progress.EnterBlock(blocksAround(c.theWorld(), x, y, z), time.Now())
	}
}

// playersAboard is the players riding the mob, read under the mob lock.
func (ent *mobEntity) playersAboard() []*client {
	if ent.rider == nil {
		return ent.extra
	}
	return append([]*client{ent.rider}, ent.extra...)
}

// advancementRider reads the player's dimension, vehicle and the vehicle's
// passengers for a player condition. The boat, cart and mob locks are
// taken one after the other, never together; a mob's type never changes,
// so a boat's mob passengers are named without the mob lock.
func (c *client) advancementRider() advancement.Rider {
	s := c.srv
	who := advancement.Rider{Dimension: s.dimensionOfWorld(c.theWorld()).name}
	if it := c.vehicle.Load(); it != nil {
		s.boatsMu.Lock()
		who.Vehicle = it.entityType()
		for _, p := range it.passengers {
			if p.c != nil {
				who.Passengers = append(who.Passengers, "minecraft:player")
			} else {
				who.Passengers = append(who.Passengers, p.ent.mob.Type.Name())
			}
		}
		s.boatsMu.Unlock()
	} else if it := c.minecart.Load(); it != nil {
		s.minecartsMu.Lock()
		who.Vehicle = it.kind.Name()
		for range it.passengers {
			who.Passengers = append(who.Passengers, "minecraft:player")
		}
		s.minecartsMu.Unlock()
	} else if ent := c.mount.Load(); ent != nil {
		s.mobsMu.Lock()
		who.Vehicle = ent.mob.Type.Name()
		for range ent.playersAboard() {
			who.Passengers = append(who.Passengers, "minecraft:player")
		}
		for _, m := range ent.mob.Passengers {
			who.Passengers = append(who.Passengers, m.Type.Name())
		}
		s.mobsMu.Unlock()
	} else if ent := c.cushion.Load(); ent != nil {
		s.fixturesMu.Lock()
		who.Vehicle = ent.kind
		for range ent.riders {
			who.Passengers = append(who.Passengers, "minecraft:player")
		}
		s.fixturesMu.Unlock()
	}
	return who
}

// startedRidingAdvancements fires started_riding for each player aboard a
// vehicle that just took a passenger. Call it with no lock held.
// vanilla: Entity.startRiding
func startedRidingAdvancements(riders []*client) {
	for _, c := range riders {
		if progress := c.advancements.Load(); progress != nil {
			progress.StartedRiding(c.advancementRider(), time.Now())
		}
	}
}

// usedTotemAdvancements fires used_totem for the item that saved the
// player. vanilla: LivingEntity.checkTotemDeathProtection
func (c *client) usedTotemAdvancements(item int32) {
	if progress := c.advancements.Load(); progress != nil {
		progress.UsedTotem(item, time.Now())
	}
}

// hurtAdvancements fires entity_hurt_player for a hurt the player took or a
// shield blocked. vanilla: LivingEntity.hurtServer
func (c *client) hurtAdvancements(damageType registry.DamageType, blocked bool) {
	if progress := c.advancements.Load(); progress != nil {
		progress.EntityHurtPlayer(damageType.InTag(registry.DamageTypeTagIDIsProjectile), blocked, time.Now())
	}
}

// targetHitAdvancements fires target_hit for the player's projectile that
// hit a target block at hit for signal. vanilla: TargetBlock.onProjectileHit
func (c *client) targetHitAdvancements(signal int32, hit [3]float64) {
	if progress := c.advancements.Load(); progress != nil {
		progress.TargetHit(signal, point(c.player.Position()), hit, time.Now())
	}
}

// woreItemAdvancements fires item_durability_changed for wear put on the
// player's item. Call it with no lock held.
// vanilla: ItemStack.applyDamage
func (c *client) woreItemAdvancements(item int32) {
	if progress := c.advancements.Load(); progress != nil {
		progress.ItemDurabilityChanged(item, c.advancementRider(), time.Now())
	}
}

// lavaRideAdvancements keeps where the player's mob vehicle entered lava
// and fires ride_entity_in_lava from there each tick it stays in; a boat or
// cart in lava is not tracked. vanilla: ServerPlayer.trackEnteredOrExitedLavaOnVehicle
func (c *client) lavaRideAdvancements(progress *advancement.Progress, v player.Vitals, pos player.Position) {
	inLava := false
	if ent := c.mount.Load(); ent != nil {
		c.srv.mobsMu.Lock()
		inLava = ent.mob.InLava()
		c.srv.mobsMu.Unlock()
	}
	switch {
	case inLava && v.LavaRideStart != nil:
		progress.RodeInLava(c.advancementRider(), *v.LavaRideStart, point(pos), time.Now())
	case inLava:
		start := point(pos)
		c.player.UpdateVitals(func(v *player.Vitals) { v.LavaRideStart = &start })
	case v.LavaRideStart != nil:
		c.player.UpdateVitals(func(v *player.Vitals) { v.LavaRideStart = nil })
	}
}

// recipeCraftedAdvancements fires recipe_crafted for each recipe a click took
// the result of. vanilla: ServerPlayer.triggerRecipeCrafted
func (c *client) recipeCraftedAdvancements(crafts []inventory.RecipeCraft) {
	if progress := c.advancements.Load(); progress != nil {
		for _, craft := range crafts {
			progress.RecipeCrafted(craft.ID, craft.Ingredients, time.Now())
		}
	}
}

// crafterRecipeAdvancements fires crafter_recipe_crafted for the non-spectator
// players whose bounding box meets the 17-block box around a crafter that
// dropped its product. vanilla: CrafterBlock.dispenseFrom
func (s *Server) crafterRecipeAdvancements(w *world.World, x, y, z int32, craft inventory.RecipeCraft) {
	cx, cy, cz := float64(x)+0.5, float64(y)+0.5, float64(z)+0.5
	s.mu.RLock()
	var near []*client
	for _, o := range s.clients {
		if o.theWorld() != w || o.player.GameMode() == protocol.GameModeSpectator {
			continue
		}
		pos := o.player.Position()
		dims := o.dims(o.player.Appearance().Pose)
		half, height := dims.Width/2, dims.Height
		if math.Abs(pos.X-cx) <= 8.5+half && math.Abs(pos.Z-cz) <= 8.5+half &&
			pos.Y <= cy+8.5 && pos.Y+height >= cy-8.5 {
			near = append(near, o)
		}
	}
	s.mu.RUnlock()
	for _, o := range near {
		if progress := o.advancements.Load(); progress != nil {
			progress.CrafterRecipeCrafted(craft.ID, craft.Ingredients, time.Now())
		}
	}
}

// usingItemAdvancements fires using_item for a tick of using item.
// vanilla: LivingEntity.updatingUsingItem
func (c *client) usingItemAdvancements(item int32) {
	if progress := c.advancements.Load(); progress != nil {
		progress.UsingItem(item, c.lookedAtType, time.Now())
	}
}

// lookedAtType is the type of the nearest live mob on the player's view ray
// within 100 blocks, empty for none; players on the ray are not considered.
// vanilla: PlayerPredicate.matches (looking_at), ProjectileUtil.getEntityHitResult
func (c *client) lookedAtType() string {
	c.srv.mobsMu.Lock()
	defer c.srv.mobsMu.Unlock()
	if m := c.lookedAtMobLocked(); m != nil {
		return m.Type.Name()
	}
	return ""
}

// lookingAtRange is the length of the view ray a looking_at predicate casts.
// vanilla: PlayerPredicate.LOOKING_AT_RANGE
const lookingAtRange = 100
