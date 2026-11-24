package server

import (
	"cmp"
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/chatsecurity"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// Command adapters on the target player: each carries out the command and
// sends packets; package command decides who and how many. Source adapters
// are in command_adapters.go.

// playerClients returns every connection in player-list (join) order, the
// order targets resolve in. vanilla: PlayerList.getPlayers
func (s *Server) playerClients() []*client {
	s.mu.RLock()
	clients := make([]*client, 0, len(s.clients))
	for _, c := range s.clients {
		clients = append(clients, c)
	}
	s.mu.RUnlock()
	slices.SortFunc(clients, func(a, b *client) int {
		return cmp.Compare(a.joinSeq.Load(), b.joinSeq.Load())
	})
	return clients
}

// clientByUUID finds an online player's connection, nil if not in the
// world; a ban uses it to disconnect the banned player.
func (s *Server) clientByUUID(id uuid.UUID) *client {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.clients[id]
}

// playerClientsIn returns one dimension's connections in its own
// player-list order (a player who crossed in is last), the order a
// world-limited selector resolves in. vanilla: ServerLevel.players
func (s *Server) playerClientsIn(name string) []*client {
	s.mu.RLock()
	clients := make([]*client, 0, len(s.clients))
	for _, c := range s.clients {
		if c.dimension().name == name {
			clients = append(clients, c)
		}
	}
	s.mu.RUnlock()
	slices.SortFunc(clients, func(a, b *client) int {
		return cmp.Compare(a.dimSeq.Load(), b.dimSeq.Load())
	})
	return clients
}

// Profile is the player's profile, the operator list's key.
func (c *client) Profile() player.Profile { return c.player.Profile }

// Position is the player's position and facing.
func (c *client) Position() player.Position { return c.player.Position() }

// EyePosition is the position raised by the eye height of the player's pose
// and scale. vanilla: Entity.getEyePosition
func (c *client) EyePosition() player.Position {
	pos := c.player.Position()
	pos.Y += c.dims(c.player.Appearance().Pose).EyeHeight
	return pos
}

// DisplayName is the name in the player's team's prefix, suffix and colour,
// with the /tell click, show-entity hover and insertion on the whole.
// vanilla: Player.getDisplayName
func (c *client) DisplayName() protocol.Text {
	if name := c.pluginDisplayName.Load(); name != nil {
		return *name
	}
	name := c.player.Name
	text := command.TeamFormattedName(c.srv.scoreboard, name, protocol.Text{Text: name})
	text.Insertion = name
	text.ClickCommand = "/tell " + name + " "
	text.HoverEntity = &protocol.HoverEntity{
		Name: protocol.Text{Text: name},
		ID:   "minecraft:player",
		UUID: uuidWords(c.player.UUID),
	}
	return text
}

// PlainName is the player's name, which a selector's name option matches.
func (c *client) PlainName() string { return c.player.Name }

// EntityType is the player's minecraft:entity_type name.
func (c *client) EntityType() string { return "minecraft:player" }

// UUID is the player's UUID.
func (c *client) UUID() uuid.UUID { return c.player.UUID }

// EntityID is the player's protocol entity ID.
func (c *client) EntityID() int32 { return c.player.EntityID }

// Box is the player's box in their pose and scale.
func (c *client) Box() (minX, minY, minZ, maxX, maxY, maxZ float64) {
	b := c.bodyBox()
	return b[0], b[1], b[2], b[3], b[4], b[5]
}

// bodyBox is the player's bounding box as minX, minY, minZ, maxX, maxY, maxZ.
// It takes the player's lock. vanilla: Entity.getBoundingBox
func (c *client) bodyBox() [6]float64 {
	return c.bodyBoxAt(c.player.Position())
}

// bodyBoxAt is bodyBox for a position the caller already read.
func (c *client) bodyBoxAt(pos player.Position) [6]float64 {
	d := c.dims(c.player.Appearance().Pose)
	hw := d.Width / 2
	return [6]float64{pos.X - hw, pos.Y, pos.Z - hw, pos.X + hw, pos.Y + d.Height, pos.Z + hw}
}

// Alive is false for a dead player who has not respawned, so the @e and @n
// alive filters skip them. vanilla: LivingEntity.isAlive
func (c *client) Alive() bool { return !c.player.Vitals().Dead }

// Tags are the player's scoreboard tags, matched by the tag option and
// edited by /tag. vanilla: Entity.entityTags
func (c *client) Tags() []string { return c.player.Tags() }

// AddTag and RemoveTag change the player's scoreboard tags, answering as
// Entity.addTag and Entity.removeTag do: false if already held, not held,
// or at the cap.
func (c *client) AddTag(tag string) bool { return c.player.AddTag(tag) }

func (c *client) RemoveTag(tag string) bool { return c.player.RemoveTag(tag) }

// GameModeNumber is the player's game mode as the packets number it.
func (c *client) GameModeNumber() int32 { return c.player.GameMode() }

// SetGameMode changes the player's mode and flight state and sends the game
// event, abilities and tab list update; a switch to spectator also drops a
// use in progress and any seat. It returns false if already in mode.
// Packets go via the target's mailbox, as another connection runs the
// command.
func (c *client) SetGameMode(mode int32) bool {
	if c.player.GameMode() == mode || !c.pluginGameMode(mode) {
		return false
	}
	// Leaving spectator returns the view to the player's own eyes.
	// vanilla: ServerPlayer.setGameMode
	if mode != protocol.GameModeSpectator {
		c.clearCamera()
	}
	c.resetFlightGrant()
	c.player.UpdateVitals(func(v *player.Vitals) {
		v.PreviousGameMode, v.HasPreviousGameMode = v.GameMode, true
		v.GameMode = mode
		// vanilla: ServerPlayerGameMode.changeGameModeForPlayer
		if mode == protocol.GameModeCreative {
			rules.ResetImpulse(v)
		}
	})
	switch mode {
	case protocol.GameModeSpectator:
		c.removeEntitiesOnShoulder()
		c.flying.Store(true)
		// vanilla: ServerPlayer.setGameMode
		c.stopUsingItem()
		c.dismountBoat(true)
		c.dismountMinecart(true)
		c.dismountCushion(true)
	case protocol.GameModeCreative:
		// vanilla: ServerPlayerGameMode.changeGameModeForPlayer, isInRangeOfGround
		if c.flying.Load() && c.inRangeOfGround() {
			c.flying.Store(false)
		}
	default:
		c.flying.Store(false)
	}

	c.send(protocol.IDPlayGameEvent, gameEventPayload(protocol.GameEventChangeGameMode, float32(mode)))
	c.send(protocol.IDPlayAbilities, c.abilitiesPayload())
	c.srv.broadcast(protocol.IDPlayPlayerInfoUpdate, playerInfoGameMode(c.player))
	c.srv.updateSleepingPlayerList()
	// Not vanilla; the admin echo reports the change, so this is debug-only.
	c.log.Debug("game mode set", "mode", gameModeNames[mode])
	return true
}

// Notify queues a chat line through the player's mailbox.
func (c *client) Notify(text protocol.Text) {
	var b protocol.Buffer
	b.PutText(text)
	b.PutBool(false) // in the chat window, not over the hotbar
	c.send(protocol.IDPlaySystemChat, b.Bytes())
}

// ShowTitle sends the title, subtitle or action bar text.
func (c *client) ShowTitle(kind command.TitleKind, text protocol.Text) {
	id := int32(protocol.IDPlayTitleText)
	switch kind {
	case command.TitleSubtitle:
		id = protocol.IDPlaySubtitleText
	case command.TitleActionBar:
		id = protocol.IDPlayActionBarText
	}
	var b protocol.Buffer
	b.PutText(text)
	c.send(id, b.Bytes())
}

// SetTitleTimes sends the fade-in, stay and fade-out ticks.
func (c *client) SetTitleTimes(fadeIn, stay, fadeOut int32) {
	var b protocol.Buffer
	b.PutI32(fadeIn)
	b.PutI32(stay)
	b.PutI32(fadeOut)
	c.send(protocol.IDPlayTitlesAnimation, b.Bytes())
}

// ClearTitles sends Clear Titles.
func (c *client) ClearTitles(reset bool) {
	var b protocol.Buffer
	b.PutBool(reset)
	c.send(protocol.IDPlayClearTitles, b.Bytes())
}

// Teleport moves the player and sends Synchronize Player Position, after
// the portal path's full dimension change when dim is another dimension.
// Others are told as for a reported step, from where they last saw the
// player, and the client's later moves measure from the arrival.
func (c *client) Teleport(move command.Move, dim string, by string) {
	move, dim, ok := c.srv.pluginTeleport(c, move, dim, by)
	if !ok {
		return
	}
	// A teleport leaves any camera behind: the view returns to the player.
	c.clearCamera()
	if d := c.srv.findDimension(dim); d != nil && d != c.dimension() {
		// Only the points go absolute across a dimension; each relative axis's
		// delta bit stays, so look and velocity stay relative.
		// vanilla: TeleportCommand.getRelatives
		move.RelX, move.RelY, move.RelZ = false, false, false
		if err := c.enterDimension(d, move, respawnNone, false); err != nil {
			c.log.Warn("teleport across dimensions failed", "error", err)
			return
		}
	} else {
		c.leaveVehicleForTeleport()
		c.teleport(move)
		// The tracker reads the body's clamped pitch back, not the command's copy,
		// under the move path's hold, so a reported step cannot land between the
		// position and the telling.
		c.tp.mu.Lock()
		c.sendMovement(c.player.Position())
		c.tp.mu.Unlock()
	}
	// A facing clause aims the client with a Player Look At after the move, in
	// the entity form if it named one. vanilla: LookAt.perform
	if move.LookAt != nil {
		if move.LookAtEntity != 0 {
			c.send(protocol.IDPlayLookAt, playerLookAtEntityPayload(move.LookAtFromEyes,
				move.LookAt.X, move.LookAt.Y, move.LookAt.Z,
				move.LookAtEntity, move.LookAtEntityEyes))
		} else {
			c.send(protocol.IDPlayLookAt, playerLookAtPayload(move.LookAtFromEyes,
				move.LookAt.X, move.LookAt.Y, move.LookAt.Z))
		}
	}
	c.log.Info("player teleported",
		"x", move.Pos.X, "y", move.Pos.Y, "z", move.Pos.Z, "by", by)
}

// Rotate turns the player in place, storing the angles and sending a Player
// Rotation via the mailbox. vanilla: RotateCommand.rotate
func (c *client) Rotate(yaw, pitch float32) {
	pitch = clampBodyPitch(pitch)
	c.player.UpdatePosition(func(p *player.Position) {
		p.Yaw, p.Pitch = yaw, pitch
	})
	var b protocol.Buffer
	b.PutF32(yaw)
	b.PutBool(false) // absolute yaw
	b.PutF32(pitch)
	b.PutBool(false) // absolute pitch
	c.send(protocol.IDPlayPlayerRotation, b.Bytes())
}

// killDamage is past any health the game can hold, as vanilla's /kill.
const killDamage = 3.4e38

// Kill applies /kill. generic_kill bypasses invulnerability, so it reaches
// creative and spectator players. vanilla: LivingEntity.kill
func (c *client) Kill() { c.kill(nil) }

// kill is Kill with the cause the hurt carries.
func (c *client) kill(cause *damageCause) {
	// Clear the post-hit invulnerability window, which would absorb the kill.
	c.player.UpdateVitals(func(v *player.Vitals) {
		v.InvulnerableTicks = 0
	})
	c.hurt(killDamage, rules.DamageTypeGenericKill, cause)
}

// HasAttributes is true: every player has an attribute map.
func (c *client) HasAttributes() bool { return true }

// WithAttributes runs fn over the player's attribute map.
func (c *client) WithAttributes(fn func(a *player.Attributes)) {
	c.player.Attributes(func(a *player.Attributes) { fn(a) })
}

// Swing plays the player's arm swing for everyone, the player included.
// vanilla: LivingEntity.swing
func (c *client) Swing(offHand bool, anim registry.SwingAnimation) bool {
	c.srv.broadcastWorld(c.theWorld(), protocol.IDPlaySwingAnimation, swingAnimationWith(c.player.EntityID, handOf(offHand), anim))
	return true
}

// handOf names the hand a command picked.
func handOf(offHand bool) int32 {
	if offHand {
		return protocol.HandOff
	}
	return protocol.HandMain
}

// addToInventory adds a stack and sends the changed slots, returning what
// did not fit. A creative player's stack that fits nowhere counts as added.
// vanilla: Inventory.add (hasInfiniteMaterials)
func (c *client) addToInventory(stack protocol.ItemStack) int32 {
	var slotPackets [][]byte
	visible := false
	var left int32
	c.player.Inventory(func(inv *player.Inventory) {
		var changed []int
		left, changed = inv.Add(stack)
		for _, slot := range changed {
			slotPackets = append(slotPackets, inv.WindowSlot(playerWindowID, int32(slot), inv.Slots[slot]))
			visible = visible || player.VisibleSlot(inv, slot)
		}
	})
	for _, packet := range slotPackets {
		c.send(protocol.IDPlayContainerSlot, packet)
	}
	if visible {
		c.broadcastEquipment()
	}
	if left == stack.Count && c.player.GameMode() == protocol.GameModeCreative {
		return 0
	}
	return left
}

// GiveItem gives a stack one max stack at a time: a chunk that fits leaves
// the fake item for the client's pickup animation and a pickup sound; one
// that does not is dropped as the receiver's leftover, pickable at once.
// vanilla: GiveCommand.giveItem
func (c *client) GiveItem(stack protocol.ItemStack) {
	maxStack := registry.StackMaxStackSize(stack)
	for remaining := stack.Count; remaining > 0; {
		chunk := min(maxStack, remaining)
		remaining -= chunk
		piece := stack
		piece.Count = chunk
		if left := c.addToInventory(piece); left > 0 {
			leftover := stack
			leftover.Count = left
			c.dropLeftover(leftover)
			continue
		}
		fake := stack
		fake.Count = 1
		c.dropFakeItem(fake)
		c.playPickupSound()
		c.BroadcastItemChanges()
	}
}

// AddItem adds the stack one max stack at a time and reports whether any
// went in or the player is in creative; what does not fit is lost.
// vanilla: Inventory.add
func (c *client) AddItem(stack protocol.ItemStack) bool {
	maxStack := registry.StackMaxStackSize(stack)
	added := c.player.GameMode() == protocol.GameModeCreative
	for remaining := stack.Count; remaining > 0; {
		piece := stack
		piece.Count = min(maxStack, remaining)
		remaining -= piece.Count
		if c.addToInventory(piece) < piece.Count {
			added = true
		}
	}
	return added
}

// LootEntity is what a loot table asks about the player: burning, its
// scoreboard name, its profile and its saved compound. The compound is read
// when the roll asks, which takes the inventory lock, so a roll made under a
// lock ranked below it reads it first (lootOpener).
func (c *client) LootEntity() rules.LootEntity {
	v := c.player.Vitals()
	out := rules.LootEntity{Type: "minecraft:player", OnFire: rules.OnFire(&v), ScoreHolder: c.Name(), Profile: c.lootProfile(), Player: c.lootPlayer(),
		NBT: func() nbt.Compound {
			tag, ok := c.DataTag()
			if !ok {
				return nil
			}
			return tag.Entries()
		}}
	command.FillLootEntity(&out, c)
	out.Team, _ = c.srv.scoreboard.PlayerTeam(c.player.Name)
	return out
}

// lootNBT is the player's compound a loot table's copy_custom_data reads,
// nil for a joining player. It takes the player's locks, which come after the
// mob lock.
// vanilla: ContextNbtProvider, NbtPredicate.getEntityTagToCompare
func (c *client) lootNBT() nbt.Compound {
	tag, ok := c.DataTag()
	if !ok {
		return nil
	}
	return tag.Entries()
}

// lootProfile is the player's resolved profile with its session properties,
// which carry the skin.
// vanilla: ResolvableProfile.createResolved
func (c *client) lootProfile() *protocol.Profile {
	props := make([]protocol.ProfileProperty, len(c.player.Properties))
	for i, p := range c.player.Properties {
		props[i] = protocol.ProfileProperty{Name: p.Name, Value: p.Value, Signature: p.Signature, Signed: p.Signature != ""}
	}
	return &protocol.Profile{UUID: c.player.UUID, Name: c.player.Name, HasUUID: true, HasName: true, Properties: props}
}

// dropLeftover drops a chunk that did not fit as the receiver's own
// leftover, pickable at once.
func (c *client) dropLeftover(stack protocol.ItemStack) {
	owner := c.player.UUID
	c.dropItem(stack, &owner, false, 0, uuid.Nil, false)
}

// dropFakeItem shows the receiver the unpickable fake item for its pickup
// animation; it despawns next tick.
func (c *client) dropFakeItem(stack protocol.ItemStack) {
	c.dropItem(stack, nil, true, 0, uuid.Nil, false)
}

// playPickupSound plays a fitted chunk's pickup sound, with vanilla's volume
// and a pitch from two of the player's rolls. vanilla: GiveCommand.giveItem
func (c *client) playPickupSound() {
	pos := c.player.Position()
	dice := c.ownDice()
	pitch := ((dice.Float32()-dice.Float32())*0.7 + 1) * 2
	c.srv.broadcastNear(c.theWorld(), pos.X, pos.Y, pos.Z, soundRange(0.2), protocol.IDPlaySoundEffect,
		soundEffectIDAt(registry.SoundEntityItemPickup, protocol.SoundCategoryPlayer,
			pos.X, pos.Y, pos.Z, 0.2, pitch))
}

// ClearInventory runs /clear and returns how many items were removed. A nil
// filter empties everything; otherwise matching stacks go, up to count when
// hasCount (zero counts without removing). The walk order, cursor last, is
// vanilla's. vanilla: Inventory.clearOrCountMatchingItems
func (c *client) ClearInventory(filter command.ItemFilter, count int64, hasCount bool) int64 {
	countOnly := hasCount && count == 0
	var content []byte
	removed := int64(0)
	c.player.Inventory(func(inv *player.Inventory) {
		carried := &c.container.click.Carried
		if filter == nil {
			for _, stack := range inv.Slots {
				removed += int64(stack.Count)
			}
			removed += int64(inv.Body.Count + inv.Saddle.Count)
			inv.Clear()
			removed += int64(carried.Count)
			*carried = protocol.ItemStack{}
			content = inv.ContainerContentWith(*carried)
			return
		}
		remaining := count
		take := func(stack *protocol.ItemStack) int64 {
			if stack.Empty() || !filter(*stack) {
				return 0
			}
			n := int64(stack.Count)
			if countOnly {
				return n
			}
			if hasCount {
				if remaining <= 0 {
					return 0
				}
				n = min(n, remaining)
			}
			stack.Count -= int32(n)
			if stack.Empty() {
				*stack = protocol.ItemStack{}
			}
			remaining -= n
			return n
		}
		for _, i := range clearOrder {
			removed += take(&inv.Slots[i])
		}
		removed += take(&inv.Body) + take(&inv.Saddle)
		for i := 1; i < player.ArmorSlot; i++ { // the grid, not the crafting output
			removed += take(&inv.Slots[i])
		}
		removed += take(carried)
		if !countOnly {
			content = inv.ContainerContentWith(*carried)
		}
	})
	if removed > 0 && !countOnly {
		c.send(protocol.IDPlayContainerContent, content)
		c.broadcastEquipment()
	}
	return removed
}

// clearOrder is /clear's walk of the player's window: the 36 item slots
// hotbar first, armour feet first, off hand, the 2x2 grid, then the cursor.
// vanilla: Inventory.clearOrCountMatchingItems
var clearOrder = buildClearOrder()

func buildClearOrder() []int {
	slots := make([]int, 0, 45)
	for i := player.HotbarSlot; i < player.HotbarSlot+player.HotbarSlots; i++ {
		slots = append(slots, i)
	}
	for i := player.MainSlot; i < player.HotbarSlot; i++ {
		slots = append(slots, i)
	}
	for i := player.ArmorSlot + player.ArmorSlots - 1; i >= player.ArmorSlot; i-- {
		slots = append(slots, i)
	}
	slots = append(slots, player.OffHandSlot)
	return slots
}

// itemSlotKind maps a raw slot id to the player's thing it addresses and
// the index within it: the window, the selected hotbar slot (main hand),
// the ender chest or the cursor. Raw ids 0-8 are the hotbar and the next 27
// the main inventory; equipment, ender, cursor and crafting have their own
// anchors. Body and saddle are equipment outside the window. Bands the
// player has none of (machine slots past 35, mob and horse inventories)
// answer nothing. vanilla: SlotRanges, LivingEntity.getSlot
func itemSlotKind(raw command.ItemSlot) (kind itemSlotPlace, index int) {
	switch {
	case raw >= 0 && raw < player.HotbarSlots:
		return itemSlotHotbar, int(raw)
	case raw >= player.HotbarSlots && raw < player.HotbarSlot:
		return itemSlotWindow, int(raw)
	case raw == command.ItemSlotMainHand:
		return itemSlotSelected, 0
	case raw == command.ItemSlotOffHand:
		return itemSlotWindow, player.OffHandSlot
	case raw >= command.ItemSlotCraftBase && raw < command.ItemSlotCraftBase+4:
		// The 2x2 grid of the player's own window, behind its output.
		return itemSlotWindow, craftingGridSlot + int(raw-command.ItemSlotCraftBase)
	case raw >= command.ItemSlotFeet && raw < command.ItemSlotFeet+player.ArmorSlots:
		// The armor ids count from the feet, the window from the head.
		return itemSlotWindow, player.ArmorSlot + player.ArmorSlots - 1 - int(raw-command.ItemSlotFeet)
	case raw >= command.ItemSlotEnder && raw < command.ItemSlotEnder+player.EnderSlots:
		return itemSlotEnder, int(raw - command.ItemSlotEnder)
	case raw == command.ItemSlotCursor:
		return itemSlotCursor, 0
	case raw == command.ItemSlotBody:
		return itemSlotBody, 0
	case raw == command.ItemSlotSaddle:
		return itemSlotSaddle, 0
	}
	return itemSlotNone, 0
}

// The kinds of the player's things a raw slot id can address.
type itemSlotPlace uint8

const (
	itemSlotNone itemSlotPlace = iota
	itemSlotWindow
	itemSlotHotbar
	itemSlotSelected
	itemSlotEnder
	itemSlotCursor
	itemSlotBody
	itemSlotSaddle
)

// ItemIn returns the stack in the raw slot, for /item reading a target as
// source.
func (c *client) ItemIn(slot command.ItemSlot) (protocol.ItemStack, bool) {
	var stack protocol.ItemStack
	ok := c.UpdateItemIn(slot, func(held protocol.ItemStack) (protocol.ItemStack, bool) {
		stack = held
		return held, false
	})
	return stack, ok
}

// itemWindowSlot is the window slot a raw slot addresses; -1 for the ender
// chest, the cursor and slots this target lacks.
func itemWindowSlot(kind itemSlotPlace, index int, inv *player.Inventory) int {
	switch kind {
	case itemSlotWindow:
		return index
	case itemSlotHotbar:
		return player.HotbarSlot + index
	case itemSlotSelected:
		return player.HotbarSlot + int(inv.Selected)
	}
	return -1
}

// SetItemIn puts the stack in the raw slot as /item does, telling the client
// of the changed window slot and everyone of changed equipment (an ender
// chest window fills when next opened). A slot the player lacks, or a stack
// the slot refuses, is not written.
func (c *client) SetItemIn(slot command.ItemSlot, stack protocol.ItemStack) bool {
	if !itemSlotAccepts(slot, stack) {
		return false
	}
	return c.UpdateItemIn(slot, func(protocol.ItemStack) (protocol.ItemStack, bool) {
		return stack, true
	})
}

// UpdateItemIn hands the slot's stack to update and, if asked, writes back
// the result and tells the client and everyone, all in one hold of the
// player's lock, so a held slot moved, dropped or re-selected in between is
// never the one written.
func (c *client) UpdateItemIn(slot command.ItemSlot, update func(protocol.ItemStack) (protocol.ItemStack, bool)) bool {
	kind, index := itemSlotKind(slot)
	if kind == itemSlotNone {
		return false
	}
	var (
		stack   protocol.ItemStack
		old     protocol.ItemStack
		write   bool
		content []byte
		result  []byte
		visible bool
	)
	windowSlot := -1
	c.player.Inventory(func(inv *player.Inventory) {
		switch kind {
		case itemSlotEnder:
			stack = inv.Ender[index]
		case itemSlotCursor:
			stack = c.container.click.Carried
		case itemSlotBody:
			stack = inv.Body
		case itemSlotSaddle:
			stack = inv.Saddle
		default:
			stack = inv.Slots[itemWindowSlot(kind, index, inv)]
		}
		old = stack
		stack, write = update(stack)
		if !write {
			return
		}
		switch kind {
		case itemSlotEnder:
			enderSlots{inv: inv}.Put(index, stack)
		case itemSlotCursor:
			c.container.click.Carried = stack
		case itemSlotBody:
			inv.Body = stack
			delete(inv.Stored, player.BodyStored)
		case itemSlotSaddle:
			inv.Saddle = stack
			delete(inv.Stored, player.SaddleStored)
		default:
			windowSlot = itemWindowSlot(kind, index, inv)
		}
		if windowSlot >= 0 {
			inv.Set(windowSlot, stack)
			content = inv.WindowSlot(playerWindowID, int32(windowSlot), inv.Slots[windowSlot])
			visible = player.VisibleSlot(inv, windowSlot)
			// A grid write changes the 2x2's result, as vanilla's slotsChanged
			// recomputes.
			if inCraftingGrid(windowSlot) {
				_, crafted := inv.CraftMatch(inv.Slots[craftingGridSlot:craftingGridSlot+craftingGridSlots], craftingGridWidth)
				result = inv.WindowSlot(playerWindowID, 0, crafted)
			}
		}
	})
	if !write {
		return true
	}
	if windowSlot >= player.ArmorSlot && windowSlot < player.ArmorSlot+player.ArmorSlots {
		c.armorChanged(windowSlot, old, stack)
	}
	switch kind {
	case itemSlotWindow, itemSlotHotbar, itemSlotSelected:
		c.send(protocol.IDPlayContainerSlot, content)
		if len(result) > 0 {
			c.send(protocol.IDPlayContainerSlot, result)
		}
		if visible {
			c.broadcastEquipment()
		}
	case itemSlotBody, itemSlotSaddle:
		// No window slot shows these; other players draw them.
		c.broadcastEquipment()
	case itemSlotEnder:
		// An ender chest write reaches the player via the window open on it, or
		// the next one opened.
		if w := c.openContainerWindow(); w != nil && w.kind == windowEnderChest {
			c.queueContainerSlot(w.id, int32(index), stack)
		}
	case itemSlotCursor:
		// The cursor is the mouse's stack, not window 0's content, so an open
		// container window is left alone.
		if err := c.sendCursorItem(stack); err != nil {
			c.log.Debug("could not send cursor item", "error", err)
		}
	}
	return true
}

// inCraftingGrid reports whether a window slot is in the 2x2 grid, from
// which result slot 0 is recomputed.
func inCraftingGrid(windowSlot int) bool {
	return windowSlot >= craftingGridSlot && windowSlot < craftingGridSlot+craftingGridSlots
}

// itemSlotAccepts reports whether a slot takes the stack. Chest, legs, feet,
// body and saddle take empty or the piece that belongs there (the stack's own
// equippable component, else its item's), so they do not become extra
// pockets; head and hands take anything.
// vanilla: LivingEntity.createEquipmentSlotAccess
func itemSlotAccepts(slot command.ItemSlot, stack protocol.ItemStack) bool {
	if stack.Empty() {
		return true
	}
	want, ok := armorSlotName(slot)
	if !ok {
		return true
	}
	item, ok := registry.StackEquippable(stack)
	return ok && item.Slot == want
}

// armorSlotName is the equipment name of a raw armour slot, for the five
// the accessor validates; head and hands answer false, taking any stack.
func armorSlotName(slot command.ItemSlot) (string, bool) {
	switch slot {
	case command.ItemSlotFeet:
		return "feet", true
	case command.ItemSlotLegs:
		return "legs", true
	case command.ItemSlotChest:
		return "chest", true
	case command.ItemSlotBody:
		return "body", true
	case command.ItemSlotSaddle:
		return "saddle", true
	}
	return "", false
}

// The 2x2 grid in the player's window: first slot, cell count, and the
// width the recipe result is matched over.
const (
	craftingGridSlot  = 1
	craftingGridSlots = 4
	craftingGridWidth = 2
)

// SetSpawnPoint sets a forced respawn point in the command's dimension,
// saved in vanilla's respawn compound.
func (c *client) SetSpawnPoint(pos world.SpawnPos, yaw, pitch float32, dimension string) {
	c.setRespawn(pos, yaw, pitch, dimension, true)
}

// PlaySound sends the player a Sound Effect from the position, via the
// mailbox since another connection runs the command.
func (c *client) PlaySound(name string, source int32, x, y, z float64, volume, pitch float32, seed int64) {
	c.send(protocol.IDPlaySoundEffect, playSoundEffect(name, source, x, y, z, volume, pitch, seed))
}

// StopSound sends Stop Sound; its flags byte says which of source and name
// follow. It goes via the player's mailbox.
func (c *client) StopSound(source int32, sound string) {
	var b protocol.Buffer
	var flags byte
	if source >= 0 {
		flags |= 0x01
	}
	if sound != "" {
		flags |= 0x02
	}
	b.PutU8(flags)
	if source >= 0 {
		b.PutVarInt(source)
	}
	if sound != "" {
		b.PutString(sound)
	}
	c.send(protocol.IDPlayStopSound, b.Bytes())
}

// SendParticle sends a Level Particles packet, data being the bytes after
// the particle ID, via the player's mailbox.
func (c *client) SendParticle(id int32, data []byte, force bool, x, y, z float64, dx, dy, dz, speed float32, count int32) {
	c.send(protocol.IDPlayLevelParticles, particleDataPayload(id, data, force, x, y, z, dx, dy, dz, speed, count))
}

// Transfer sends /transfer's packet telling the client to reconnect to
// host:port, via the mailbox. Nothing here closes the connection; the
// client leaves.
func (c *client) Transfer(host string, port int32) {
	var b protocol.Buffer
	b.PutString(host)
	b.PutVarInt(port)
	c.send(protocol.IDPlayTransfer, b.Bytes())
}

// setRespawn stores the respawn point with vanilla's forced flag (true for
// /spawnpoint, false for a bed), the angles folded into [-180, 180) and the
// pitch clamped. vanilla: ServerPlayer.setRespawnPosition
func (c *client) setRespawn(pos world.SpawnPos, yaw, pitch float32, dimension string, forced bool) {
	c.spawnPoint.Store(&respawnPos{
		pos:    pos,
		dim:    dimension,
		yaw:    command.WrapDegrees(yaw),
		pitch:  command.ClampPitch(pitch),
		forced: forced,
	})
}

// IncomingMessage shows a direct message from another player.
// vanilla: MsgCommand, OutgoingChatMessage.create
func (c *client) IncomingMessage(from protocol.Text, message *chatsecurity.Message, resolved protocol.Text) {
	c.commandChat("minecraft:msg_command_incoming", from, message, nil, resolved)
}

// commandChat sends a command's player-chat line (a whisper or a team
// message) naming the target (nil for none), unless chat mode excludes
// player chat. A signed one goes as a Player Chat Message naming the sender
// (kept through as); one without signing context (console, command block,
// unsigned command) uses the Disguised Chat Message system form.
// vanilla: ServerPlayer.sendChatMessage, OutgoingChatMessage.create
func (c *client) commandChat(chatType string, from protocol.Text, message *chatsecurity.Message, to *protocol.Text, resolved protocol.Text) {
	if !c.acceptsChatMessages() {
		return
	}
	if c.srv.Config.PreventChatReports {
		c.sendConvertedChat(chatType, from, resolved, to)
		return
	}
	if message == nil {
		c.send(protocol.IDPlayDisguisedChat, disguisedChatPayload(chatType, from, resolved, to))
		return
	}
	c.srv.chatOrderMu.Lock()
	defer c.srv.chatOrderMu.Unlock()
	c.sendVerifiedChat(chatType, from, *message, resolved, to)
}

// IP is the address the player connected from, which the IP ban list keys by.
func (c *client) IP() string { return c.remoteIP() }

// Disconnect closes the connection with the reason, sent via the mailbox
// and closed once written, so the reason arrives first.
func (c *client) Disconnect(reason protocol.Text) { c.disconnectAfterDrain(reason) }

// PermLevel is the player's permission level. SetPermLevel changes it
// without telling the client; ResendPermissionState does that.
func (c *client) PermLevel() int         { return int(c.permLevel.Load()) }
func (c *client) SetPermLevel(level int) { c.permLevel.Store(int32(level)) }

// ResendPermissionState sends the command tree and op-level entity event
// for the player's level, via the mailbox as /op and /deop run elsewhere.
func (c *client) ResendPermissionState() {
	if r := c.srv.plugins; r != nil && (c.srv.pluginCommands.active.Load() || r.Has(pluginruntime.PlayerCommandSend)) {
		r.Post(func() {
			level := c.PermLevel()
			c.sendPermissionState(level, c.commandTreePacket(level))
		})
		return
	}
	level := c.PermLevel()
	c.sendPermissionState(level, command.Encode(c.srv.pluginCommandTree(c, level)))
}

func (c *client) sendPermissionState(level int, tree []byte) {
	c.send(protocol.IDPlayCommands, tree)
	c.send(protocol.IDPlayEntityEvent,
		entityEventPayload(c.player.EntityID, uint8(protocol.EntityStatusOpLevelBase+level)))
	c.log.Info("permission level changed", "level", level)
}

// ShowDialog opens the dialog: a registry entry goes as its ID plus one, an
// inline definition as 0 and the network NBT.
// vanilla: ClientboundShowDialogPacket, ByteBufCodecs.holder
func (c *client) ShowDialog(dialog command.DialogRef) {
	var b protocol.Buffer
	if dialog.Inline == nil {
		b.PutVarInt(int32(dialog.Index) + 1)
	} else {
		b.PutVarInt(0)
		data, err := nbt.EncodeNetwork(dialog.Inline)
		if err != nil {
			c.log.Warn("dialog did not encode", "err", err)
			return
		}
		b.PutRaw(data)
	}
	c.send(protocol.IDPlayShowDialog, b.Bytes())
}

// ClearDialog closes the dialog screen the player has open.
func (c *client) ClearDialog() { c.send(protocol.IDPlayClearDialog, nil) }

// advancementContext is the loot context a criterion's player conditions are
// tested in: THIS_ENTITY and ORIGIN are the player.
// vanilla: SimpleCriterionTrigger.trigger, EntityPredicate.createContext
func (c *client) advancementContext() (*rules.LootContext, rules.LootRand) {
	ctx := &rules.LootContext{This: c.LootEntity()}
	command.FillLootWorld(ctx, c, c.Position())
	return ctx, c.Random()
}
