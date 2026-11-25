package server

import (
	"math"
	"reflect"
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/advancement"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// pluginToggle fires one of the sneak, sprint and flight toggle events and
// reports whether the change may go ahead.
func (c *client) pluginToggle(hook int, on bool) bool {
	if !c.pluginListening(hook) {
		return true
	}
	var e plugin.Event
	var cancelled func() bool
	switch hook {
	case pluginruntime.PlayerToggleSneak:
		ev := &plugin.PlayerToggleSneakEvent{Player: c.pluginSelf(), On: on}
		e, cancelled = ev, ev.Cancelled
	case pluginruntime.PlayerToggleSprint:
		ev := &plugin.PlayerToggleSprintEvent{Player: c.pluginSelf(), On: on}
		e, cancelled = ev, ev.Cancelled
	default:
		ev := &plugin.PlayerToggleFlightEvent{Player: c.pluginSelf(), On: on}
		e, cancelled = ev, ev.Cancelled
	}
	return !c.pluginFire(e, false) || !cancelled()
}

// pluginItemHeld fires PlayerItemHeldEvent for a hotbar change and reports
// whether it may go ahead.
// vanilla: ServerGamePacketListenerImpl.handleSetCarriedItem
func (c *client) pluginItemHeld(previous, selected int32) bool {
	if !c.pluginListening(pluginruntime.PlayerItemHeld) {
		return true
	}
	e := &plugin.PlayerItemHeldEvent{Player: c.pluginSelf(), Previous: int(previous), New: int(selected)}
	return !c.pluginFire(e, false) || !e.Cancelled()
}

// pluginSwapHands fires PlayerSwapHandItemsEvent with the stacks as they will
// stand after the swap. It returns the stacks to put in the main and off hand,
// false when the swap is cancelled, and whether a listener changed them.
// vanilla: ServerGamePacketListenerImpl.handlePlayerAction SWAP_ITEM_WITH_OFFHAND
func (c *client) pluginSwapHands() (main, off protocol.ItemStack, ok, edited bool) {
	main, off = c.heldItem(protocol.HandOff), c.heldItem(protocol.HandMain)
	if !c.pluginListening(pluginruntime.PlayerSwapHandItems) {
		return main, off, true, false
	}
	e := &plugin.PlayerSwapHandItemsEvent{Player: c.pluginSelf(), MainHand: pluginStack(main), OffHand: pluginStack(off)}
	if !c.pluginFire(e, false) {
		return main, off, true, false
	}
	if e.Cancelled() {
		return main, off, false, false
	}
	if m, valid := pluginProtocolStack(e.MainHand); valid {
		main, edited = m, true
	}
	if o, valid := pluginProtocolStack(e.OffHand); valid {
		off, edited = o, true
	}
	return main, off, true, edited
}

// pluginInteract fires PlayerInteractEvent for a click on the block at pos, or
// the air when pos is nil. It returns nil when nobody listens.
// vanilla: ServerGamePacketListenerImpl.handleUseItem, handleUseItemOn
func (c *client) pluginInteract(action plugin.InteractAction, hand int32, pos *[3]int32, face int32, cursor plugin.Vec3) *plugin.PlayerInteractEvent {
	if !c.pluginListening(pluginruntime.PlayerInteract) {
		return nil
	}
	e := &plugin.PlayerInteractEvent{Player: c.pluginSelf(), Action: action, Hand: plugin.Hand(hand),
		Item: pluginStack(c.heldItem(hand)), Face: plugin.BlockFace(face), Cursor: cursor}
	if pos != nil {
		b := c.pluginBlockAt(pos[0], pos[1], pos[2])
		e.Block = &b
	}
	if !c.pluginFire(e, false) {
		return nil
	}
	return e
}

// fire runs the listeners wherever the caller is: a command or plugin call is
// already on the tick goroutine, a connection is not.
func (s *Server) pluginFireAny(e plugin.Event) bool {
	if s.plugins.OnTick() {
		s.plugins.Fire(e)
		return true
	}
	return s.plugins.Call(func() { s.plugins.Fire(e) })
}

// pluginMove fires PlayerMoveEvent for a reported step to pos. It returns the
// position to apply, or false when the player is sent back to where they were
// (cancelled) or to the listener's To instead.
// vanilla: ServerGamePacketListenerImpl.handleMovePlayer
func (c *client) pluginMove(pos player.Position) (player.Position, bool) {
	if !c.pluginListening(pluginruntime.PlayerMove) {
		return pos, true
	}
	from := c.player.Position()
	if from.X == pos.X && from.Y == pos.Y && from.Z == pos.Z && from.Yaw == pos.Yaw && from.Pitch == pos.Pitch {
		return pos, true
	}
	c.tp.mu.Lock()
	waiting := c.tp.pending
	c.tp.mu.Unlock()
	if waiting {
		return pos, true
	}
	h := c.srv.pluginAdapter
	w := pluginWorld{srv: c.srv, dim: c.dimension(), host: h}
	loc := func(p player.Position) plugin.Location {
		return plugin.Location{World: w, Pos: plugin.Vec3{X: p.X, Y: p.Y, Z: p.Z}, Yaw: p.Yaw, Pitch: p.Pitch}
	}
	e := &plugin.PlayerMoveEvent{Player: c.pluginSelf(), From: loc(from), To: loc(pos)}
	if !c.pluginFire(e, false) {
		return pos, true
	}
	if e.Cancelled() {
		c.resetPosition()
		return pos, false
	}
	if to := e.To; to.Pos != (plugin.Vec3{X: pos.X, Y: pos.Y, Z: pos.Z}) || to.Yaw != pos.Yaw || to.Pitch != pos.Pitch {
		pos.X, pos.Y, pos.Z, pos.Yaw, pos.Pitch = to.Pos.X, to.Pos.Y, to.Pos.Z, to.Yaw, to.Pitch
		c.teleport(command.Move{Pos: pos})
		return pos, false
	}
	return pos, true
}

// pluginTeleport fires EntityTeleportEvent for a teleport of b; it returns the
// move and dimension to carry out, or false when cancelled.
// vanilla: Entity.teleportTo
func (s *Server) pluginTeleport(b command.Entity, move command.Move, dim, by string) (command.Move, string, bool) {
	cause := plugin.TeleportCommand
	switch by {
	case pluginTeleportBy:
		// The plugin's own call already fired the event.
		return move, dim, true
	case "teleport menu":
		cause = plugin.TeleportSpectate
	}
	return s.pluginTeleportCause(b, move, dim, cause)
}

// pluginTeleportBy is the `by` of a teleport whose EntityTeleportEvent the
// caller fired itself.
const pluginTeleportBy = "plugin"

// pluginTeleportCause is pluginTeleport with the cause known.
func (s *Server) pluginTeleportCause(b command.Entity, move command.Move, dim string, cause plugin.TeleportCause) (command.Move, string, bool) {
	if s.plugins == nil || !s.plugins.Has(pluginruntime.EntityTeleport) {
		return move, dim, true
	}
	h := s.pluginAdapter
	toDim := s.dimensionByName(dim)
	if toDim == nil {
		toDim = s.dimensionByName(b.DimensionName())
	}
	p := move.Pos
	e := &plugin.EntityTeleportEvent{Entity: h.pluginEntityOf(b), From: h.entityLocation(b), Cause: cause,
		To: plugin.Location{World: pluginWorld{srv: s, dim: toDim, host: h}, Pos: plugin.Vec3{X: p.X, Y: p.Y, Z: p.Z}, Yaw: p.Yaw, Pitch: p.Pitch}}
	if !s.pluginFireAny(e) {
		return move, dim, true
	}
	if e.Cancelled() {
		return move, dim, false
	}
	to := e.To
	if w, ok := to.World.(pluginWorld); ok && w.srv == s && w.dim != nil {
		dim = w.dim.name
	}
	if to.Pos != (plugin.Vec3{X: p.X, Y: p.Y, Z: p.Z}) || to.Yaw != p.Yaw || to.Pitch != p.Pitch {
		// A listener's destination is absolute.
		move.Pos = player.Position{X: to.Pos.X, Y: to.Pos.Y, Z: to.Pos.Z, Yaw: to.Yaw, Pitch: to.Pitch, OnGround: p.OnGround}
		move.RelX, move.RelY, move.RelZ, move.RelYaw, move.RelPitch = false, false, false, false, false
		if to.Yaw != p.Yaw || to.Pitch != p.Pitch {
			// The listener's angles replace a facing clause's.
			move.LookAt = nil
		}
	}
	return move, dim, true
}

// pluginAttack fires PlayerAttackEvent; false cancels the blow. Only players,
// mobs and leash knots are known to plugins.
// vanilla: ServerGamePacketListenerImpl.handleAttack
func (c *client) pluginAttack(target int32) bool {
	if !c.pluginListening(pluginruntime.PlayerAttack) {
		return true
	}
	t := c.pluginTargetByID(target)
	if t == nil {
		return true
	}
	e := &plugin.PlayerAttackEvent{Player: c.pluginSelf(), Target: c.srv.pluginAdapter.pluginEntityOf(t)}
	return !c.pluginFire(e, false) || !e.Cancelled()
}

// pluginTargetByID is the entity of any kind a client names by ID in its
// world, nil for none.
func (c *client) pluginTargetByID(id int32) command.Entity {
	if t := c.srv.relatedByID(c.theWorld(), id); t != nil {
		return t
	}
	found := (worldScope{srv: c.srv}).Entities(command.EntityQuery{
		Dimension: c.dimension().name, Limit: 1,
		Accept: func(e command.Entity) bool { return e.EntityID() == id },
	})
	if len(found) == 0 {
		return nil
	}
	return found[0]
}

// pluginInteractEntity fires PlayerInteractEntityEvent; false cancels the use.
// vanilla: ServerGamePacketListenerImpl.handleInteract
func (c *client) pluginInteractEntity(target, hand int32, ox, oy, oz float64) bool {
	if !c.pluginListening(pluginruntime.PlayerInteractEntity) {
		return true
	}
	t := c.pluginTargetByID(target)
	if t == nil {
		return true
	}
	e := &plugin.PlayerInteractEntityEvent{Player: c.pluginSelf(), Target: c.srv.pluginAdapter.pluginEntityOf(t),
		Hand: plugin.Hand(hand), Pos: plugin.Vec3{X: ox, Y: oy, Z: oz}}
	return !c.pluginFire(e, false) || !e.Cancelled()
}

// pluginGameMode fires PlayerGameModeChangeEvent; false cancels the change.
func (c *client) pluginGameMode(mode int32) bool {
	if c.srv.plugins == nil || !c.srv.plugins.Has(pluginruntime.PlayerGameModeChange) {
		return true
	}
	e := &plugin.PlayerGameModeChangeEvent{Player: c.pluginSelf(), New: plugin.GameMode(mode)}
	return !c.srv.pluginFireAny(e) || !e.Cancelled()
}

// pluginRespawn fires PlayerRespawnEvent and returns where the player respawns,
// as the listener left it.
// vanilla: PlayerList.respawn
func (c *client) pluginRespawn(pos player.Position, dest *dimension, point *respawnPos, missing bool) (player.Position, *dimension) {
	w := pluginWorld{srv: c.srv, dim: dest, host: c.srv.pluginAdapter}
	var bed, anchor bool
	if point != nil && !missing {
		if pointDim := c.srv.findDimension(point.dim); pointDim != nil {
			anchor = pointDim.w.RespawnAnchorWorks()
			bed = !anchor
		}
	}
	e := &plugin.PlayerRespawnEvent{Player: c.pluginSelf(),
		Location: plugin.Location{World: w, Pos: plugin.Vec3{X: pos.X, Y: pos.Y, Z: pos.Z}, Yaw: pos.Yaw, Pitch: pos.Pitch},
		Bed:      bed,
		Anchor:   anchor}
	if !c.pluginFire(e, false) {
		return pos, dest
	}
	l := e.Location
	if lw, ok := l.World.(pluginWorld); ok && lw.srv == c.srv && lw.dim != nil {
		dest = lw.dim
	}
	return player.Position{X: l.Pos.X, Y: l.Pos.Y, Z: l.Pos.Z, Yaw: l.Yaw, Pitch: l.Pitch}, dest
}

// pluginConsume fires PlayerItemConsumeEvent for the meal or drink in hand
// that just finished and returns the stack its effects use, which a listener
// may have replaced; false when cancelled.
// vanilla: ServerPlayer.completeUsingItem
func (c *client) pluginConsume(hand int32) (protocol.ItemStack, bool) {
	held := c.heldItem(hand)
	if !c.pluginListening(pluginruntime.PlayerItemConsume) {
		return held, true
	}
	e := &plugin.PlayerItemConsumeEvent{Player: c.pluginSelf(), Item: pluginStack(held), Hand: plugin.Hand(hand)}
	if !c.srv.pluginFireAny(e) {
		return held, true
	}
	if e.Cancelled() {
		return held, false
	}
	if item, valid := pluginProtocolStack(e.Item); valid && !item.Empty() {
		return item, true
	}
	return held, true
}

// pluginBucket fires PlayerBucketEvent for a scoop or pour at the block x, y, z
// and returns the bucket the hand ends up with, which a listener may have
// changed; false when cancelled, with the hand resent.
// vanilla: BucketItem.use
func (c *client) pluginBucket(hand int32, fill bool, x, y, z, face int32, held, result protocol.ItemStack) (protocol.ItemStack, bool) {
	if !c.pluginListening(pluginruntime.PlayerBucket) {
		return result, true
	}
	e := &plugin.PlayerBucketEvent{Player: c.pluginSelf(), Block: c.pluginBlockAt(x, y, z), Face: plugin.BlockFace(face),
		Fill: fill, Item: pluginStack(held), Result: pluginStack(result)}
	if !c.pluginFire(e, false) {
		return result, true
	}
	if e.Cancelled() {
		c.resyncHand(hand)
		return result, false
	}
	if item, valid := pluginProtocolStack(e.Result); valid && !item.Empty() {
		return item, true
	}
	return result, true
}

// pluginBedLeave fires PlayerBedLeaveEvent for a sleeper about to stand up.
func (c *client) pluginBedLeave() {
	if !c.pluginListening(pluginruntime.PlayerBedLeave) {
		return
	}
	sl := c.player.Sleep()
	if !sl.Lying {
		return
	}
	c.srv.pluginFireAny(&plugin.PlayerBedLeaveEvent{Player: c.pluginSelf(), Bed: c.pluginBlockAt(sl.X, sl.Y, sl.Z)})
}

// pluginBedEnter fires PlayerBedEnterEvent with vanilla's answer to the sleep
// attempt and returns the answer to act on; false when cancelled.
// vanilla: ServerPlayer.startSleepInBed
func (c *client) pluginBedEnter(x, y, z int32, result plugin.BedResult) (plugin.BedResult, bool) {
	if !c.pluginListening(pluginruntime.PlayerBedEnter) {
		return result, true
	}
	e := &plugin.PlayerBedEnterEvent{Player: c.pluginSelf(), Bed: c.pluginBlockAt(x, y, z), Result: result}
	if !c.pluginFire(e, false) {
		return result, true
	}
	return e.Result, !e.Cancelled()
}

// resyncHand resends the hand's slot and what others see of it.
func (c *client) resyncHand(hand int32) {
	var payload []byte
	c.player.Inventory(func(inv *player.Inventory) {
		slot := rules.SlotOfHand(inv, hand)
		payload = inv.WindowSlot(playerWindowID, int32(slot), inv.Slots[slot])
	})
	c.send(protocol.IDPlayContainerSlot, payload)
	c.broadcastEquipment()
}

// pluginExpChange fires PlayerExpChangeEvent for points of experience about
// to be gained and returns the amount to give, which a listener may change.
func (c *client) pluginExpChange(points int) int {
	if !c.pluginListening(pluginruntime.PlayerExpChange) {
		return points
	}
	e := &plugin.PlayerExpChangeEvent{Player: c.pluginSelf(), Amount: points}
	c.srv.pluginFireAny(e)
	return max(e.Amount, 0) // a listener's negative amount is no loss
}

// pluginLevelChange fires PlayerLevelChangeEvent after the level moved.
func (c *client) pluginLevelChange(was, now int) {
	if was == now || !c.pluginListening(pluginruntime.PlayerLevelChange) {
		return
	}
	c.srv.pluginFireAny(&plugin.PlayerLevelChangeEvent{Player: c.pluginSelf(), Old: was, New: now})
}

// pluginCriterionVeto fires PlayerAdvancementCriterionGrantEvent and reports
// whether a listener cancelled the grant. It runs under the progress's lock.
func (c *client) pluginCriterionVeto(n *advancement.Node, criterion string) bool {
	if !c.pluginListening(pluginruntime.PlayerAdvancementCriterionGrant) {
		return false
	}
	e := &plugin.PlayerAdvancementCriterionGrantEvent{Player: c.pluginSelf(), Advancement: pluginAdvancement{n}, Criterion: criterion}
	return c.srv.pluginFireAny(e) && e.Cancelled()
}

// pluginAdvancementDone fires PlayerAdvancementDoneEvent for a completed
// advancement and returns the chat announcement to send: msg, which a
// listener may replace or clear. msg is nil when nothing is announced.
func (c *client) pluginAdvancementDone(n *advancement.Node, msg *protocol.Text) *protocol.Text {
	if !c.pluginListening(pluginruntime.PlayerAdvancementDone) {
		return msg
	}
	e := &plugin.PlayerAdvancementDoneEvent{Player: c.pluginSelf(), Advancement: pluginAdvancement{n}}
	var shown plugin.Text
	// A message the API cannot represent is passed on as it is unless the
	// listener sets one.
	opaque := false
	if msg != nil {
		if t, ok := pluginText(*msg); ok {
			shown = t
			e.Message = &t
		} else {
			opaque = true
		}
	}
	if !c.srv.pluginFireAny(e) {
		return msg
	}
	switch {
	case e.Message == nil && opaque:
		return msg
	case e.Message == nil:
		return nil
	case msg != nil && reflect.DeepEqual(*e.Message, shown):
		return msg
	}
	t := pluginProtocolText(*e.Message)
	return &t
}

// pluginLeftClickAir fires PlayerInteractEvent LeftClickAir for a swing that
// reaches no block. The swing goes ahead whatever the listener says.
// vanilla: Item.getPlayerPOVHitResult
func (c *client) pluginLeftClickAir() {
	if !c.pluginListening(pluginruntime.PlayerInteract) || c.player.GameMode() == protocol.GameModeSpectator {
		return
	}
	pos := c.player.Position()
	fx, fy, fz := pos.X, pos.Y+c.dims(c.player.Appearance().Pose).EyeHeight, pos.Z
	dx, dy, dz := entity.LookVector(pos.Yaw, pos.Pitch)
	reach := c.blockRange()
	if _, hit := c.theWorld().ClipBlocks(fx, fy, fz, dx*reach, dy*reach, dz*reach, nil); !hit {
		c.pluginInteract(plugin.LeftClickAir, protocol.HandMain, nil, 0, plugin.Vec3{})
	}
}

// pluginDrop fires PlayerDropItemEvent for the item entity a player just threw
// and reports whether the drop stands.
// vanilla: ServerPlayer.drop
func (c *client) pluginDrop(it *itemEntity) bool {
	if !c.pluginListening(pluginruntime.PlayerDropItem) {
		return true
	}
	e := &plugin.PlayerDropItemEvent{Player: c.pluginSelf(), Item: pluginItemEntity{pluginEntity{host: c.srv.pluginAdapter, id: it.uuid}}}
	ran := c.srv.pluginFireAny(e)
	return !ran || !e.Cancelled()
}

// pluginEditBook fires PlayerEditBookEvent and returns the pages and title to
// write, which a listener may have changed; false when cancelled. The title
// is read by the event only when the book is signed.
// vanilla: ServerGamePacketListenerImpl.handleEditBook
func (c *client) pluginEditBook(pages []string, title string, signing bool) ([]string, string, bool) {
	if !c.pluginListening(pluginruntime.PlayerEditBook) {
		return pages, title, true
	}
	e := &plugin.PlayerEditBookEvent{Player: c.pluginSelf(), Pages: slices.Clone(pages)}
	if signing {
		e.Title = &title
	}
	if !c.srv.pluginFireAny(e) {
		return pages, title, true
	}
	if e.Cancelled() {
		return pages, title, false
	}
	if signing && e.Title != nil {
		title = *e.Title
	}
	return e.Pages, title, true
}

// pluginChangedWorld fires PlayerChangedWorldEvent once the player stands in
// the dimension they crossed into, with the writer released.
func (c *client) pluginChangedWorld(from *dimension) {
	if !c.pluginListening(pluginruntime.PlayerChangedWorld) {
		return
	}
	c.srv.pluginFireAny(&plugin.PlayerChangedWorldEvent{Player: c.pluginSelf(),
		From: pluginWorld{srv: c.srv, dim: from, host: c.srv.pluginAdapter}})
}

// pluginShear is the ask for a player's shear: nil when nobody listens.
// vanilla: Shearable.shear
func (c *client) pluginShear(ent *mobEntity) func([]protocol.ItemStack) ([]protocol.ItemStack, bool) {
	if !c.pluginListening(pluginruntime.PlayerShearEntity) {
		return nil
	}
	return c.srv.pluginShearAsk(c.pluginSelf(), ent)
}

// pluginDispenserShear is the ask for a dispenser's shear, with no player.
func (s *Server) pluginDispenserShear(ent *mobEntity) func([]protocol.ItemStack) ([]protocol.ItemStack, bool) {
	if s.plugins == nil || !s.plugins.Has(pluginruntime.PlayerShearEntity) {
		return nil
	}
	return s.pluginShearAsk(nil, ent)
}

func (s *Server) pluginShearAsk(p plugin.Player, ent *mobEntity) func([]protocol.ItemStack) ([]protocol.ItemStack, bool) {
	return func(drops []protocol.ItemStack) ([]protocol.ItemStack, bool) {
		e := &plugin.PlayerShearEntityEvent{Player: p, Target: pluginEntity{host: s.pluginAdapter, id: ent.uuid}}
		for _, d := range drops {
			e.Drops = append(e.Drops, pluginStack(d))
		}
		if !s.pluginFireAny(e) {
			return drops, true
		}
		if e.Cancelled() {
			return nil, false
		}
		out := make([]protocol.ItemStack, 0, len(e.Drops))
		for _, d := range e.Drops {
			if st, ok := pluginProtocolStack(d); ok && !st.Empty() {
				out = append(out, st)
			}
		}
		return out, true
	}
}

// pluginPickup fires EntityPickupItemEvent for a player about to collect an
// item entity. It returns how many the player may take, 0 for all of them,
// and false when the pickup is cancelled.
// vanilla: ItemEntity.playerTouch
func (c *client) pluginPickup(it *itemEntity) (take int32, ok bool) {
	if !c.pluginListening(pluginruntime.EntityPickupItem) {
		return 0, true
	}
	s := c.srv
	s.itemsMu.Lock()
	count := it.item.Stack.Count
	s.itemsMu.Unlock()
	e := &plugin.EntityPickupItemEvent{Entity: c.pluginSelf(), Count: int(count),
		Item: pluginItemEntity{pluginEntity{host: s.pluginAdapter, id: it.uuid}}}
	if !s.pluginFireAny(e) {
		return 0, true
	}
	if e.Cancelled() || e.Count <= 0 {
		return 0, false
	}
	return int32(min(e.Count, int(count))), true
}

// pluginItemBreak fires PlayerItemBreakEvent for an item that wore out; the
// stack is gone by now, so the event names the item alone.
func (c *client) pluginItemBreak(item int32) {
	if !c.pluginListening(pluginruntime.PlayerItemBreak) {
		return
	}
	c.srv.pluginFireAny(&plugin.PlayerItemBreakEvent{Player: c.pluginSelf(),
		Item: plugin.ItemStack{Item: registry.Item(item), Count: 1}})
}

// pluginStandManipulate fires PlayerArmorStandManipulateEvent for a swap
// between the held stack and a stand's slot, and reports whether it may go
// ahead. The wire slots 0-5 are the plugin slots in the same order.
// vanilla: ArmorStand.interact, swapItem
func (c *client) pluginStandManipulate(id uuid.UUID, slot int, held, onStand protocol.ItemStack) bool {
	base := pluginEntity{host: c.srv.pluginAdapter, id: id}
	stand, ok := pluginTypedHandle(base, registry.EntityTypeArmorStand).(plugin.ArmorStand)
	if !ok {
		return true
	}
	e := &plugin.PlayerArmorStandManipulateEvent{Player: c.pluginSelf(), Stand: stand,
		Slot: plugin.EquipmentSlot(slot), PlayerItem: pluginStack(held), StandItem: pluginStack(onStand)}
	return !c.pluginFire(e, false) || !e.Cancelled()
}

// pluginItemDamage fires PlayerItemDamageEvent for wear about to land on stack
// and returns the wear to apply, 0 when cancelled. It runs before unbreaking,
// on whichever goroutine the wear lands.
func (c *client) pluginItemDamage(stack protocol.ItemStack, n int32) int32 {
	if !c.pluginListening(pluginruntime.PlayerItemDamage) {
		return n
	}
	e := &plugin.PlayerItemDamageEvent{Player: c.pluginSelf(), Item: pluginStack(stack), Damage: int(n)}
	if !c.srv.pluginFireAny(e) {
		return n
	}
	if e.Cancelled() || e.Damage <= 0 {
		return 0
	}
	return int32(e.Damage)
}

// pluginItemDamageHeld is pluginItemDamage for the stack in hand, read only
// when someone listens.
func (c *client) pluginItemDamageHeld(hand, n int32) int32 {
	if !c.pluginListening(pluginruntime.PlayerItemDamage) {
		return n
	}
	return c.pluginItemDamage(c.heldItem(hand), n)
}

// askWear fires PlayerItemDamageEvent for n wear on each of the inventory
// slots whose stack skip does not exclude, and returns the wear per slot, nil
// when nobody listens (every slot then takes n). Call it outside the
// inventory lock.
func (c *client) askWear(n int32, skip func(protocol.ItemStack) bool, slots ...int) []int32 {
	if !c.pluginListening(pluginruntime.PlayerItemDamage) {
		return nil
	}
	stacks := make([]protocol.ItemStack, len(slots))
	c.player.Inventory(func(inv *player.Inventory) {
		for i, slot := range slots {
			stacks[i] = inv.Slots[slot]
		}
	})
	out := make([]int32, len(slots))
	for i, stack := range stacks {
		out[i] = n
		if !stack.Empty() && (skip == nil || !skip(stack)) {
			out[i] = c.pluginItemDamage(stack, n)
		}
	}
	return out
}

// pluginFish fires PlayerFishEvent for one stage of fishing and reports
// whether it may go ahead. caught is the catch (an item not yet in the world,
// or the hooked body) and xp
// the orb's value, which a listener may change.
// vanilla: FishingHook.retrieve, FishingRodItem.use
func (c *client) pluginFish(state plugin.FishState, it *bobberEntity, caught plugin.Entity, xp *int) bool {
	if !c.pluginListening(pluginruntime.PlayerFish) {
		return true
	}
	e := &plugin.PlayerFishEvent{Player: c.pluginSelf(), State: state,
		Hook: pluginEntity{host: c.srv.pluginAdapter, id: it.uuid}}
	e.Caught = caught
	if xp != nil {
		e.Experience = *xp
	}
	if !c.srv.pluginFireAny(e) {
		return true
	}
	if e.Cancelled() {
		return false
	}
	if xp != nil {
		*xp = max(e.Experience, 0)
	}
	return true
}

// pluginPortal fires EntityPortalEvent for a body about to cross an end portal to
// spot in to, and returns the destination to use: a listener's To names the
// block the body is put at, in its world. It reports false when cancelled.
// vanilla: Entity.teleportCrossDimension, Entity.handlePortal
func (s *Server) pluginPortal(who command.Entity, pos player.Position, to *dimension, spot world.SpawnPos, cause plugin.TeleportCause) (*dimension, world.SpawnPos, bool) {
	if who == nil || s.plugins == nil || !s.plugins.Has(pluginruntime.EntityPortal) {
		return to, spot, true
	}
	h := s.pluginAdapter
	at := standingAt(spot)
	dest := plugin.Location{World: pluginWorld{srv: s, dim: to, host: h},
		Pos: plugin.Vec3{X: at.X, Y: at.Y, Z: at.Z}, Yaw: pos.Yaw, Pitch: pos.Pitch}
	e := &plugin.EntityPortalEvent{Entity: h.pluginEntityOf(who), From: h.entityLocation(who), To: dest, Cause: cause}
	if !s.pluginFireAny(e) {
		return to, spot, true
	}
	if e.Cancelled() {
		return to, spot, false
	}
	if e.To.Pos != dest.Pos {
		spot = world.SpawnPos{X: int32(math.Floor(e.To.Pos.X)), Y: int32(math.Floor(e.To.Pos.Y)), Z: int32(math.Floor(e.To.Pos.Z))}
	}
	if w, ok := e.To.World.(pluginWorld); ok && w.srv == s && w.dim != nil {
		to = w.dim
	}
	return to, spot, true
}

// pluginNetherPortal fires EntityPortalEvent for a body about to cross a
// nether portal, before the exit is searched for. at is the scaled target in
// to; it returns the target, radii and whether a missing exit may be built
// as the listeners left them, false when cancelled.
// vanilla: Entity.handlePortal, PortalForcer.findClosestPortalPosition
func (s *Server) pluginNetherPortal(who command.Entity, pos player.Position, to *dimension, at world.SpawnPos, search, create int32) (*dimension, world.SpawnPos, int32, int32, bool, bool) {
	if s.plugins == nil || !s.plugins.Has(pluginruntime.EntityPortal) {
		return to, at, search, create, true, true
	}
	h := s.pluginAdapter
	dest := plugin.Location{World: pluginWorld{srv: s, dim: to, host: h},
		Pos: plugin.Vec3{X: float64(at.X), Y: float64(at.Y), Z: float64(at.Z)}, Yaw: pos.Yaw, Pitch: pos.Pitch}
	e := &plugin.EntityPortalEvent{Entity: h.pluginEntityOf(who), From: h.entityLocation(who), To: dest, Cause: plugin.TeleportNetherPortal,
		SearchRadius: int(search), CreationRadius: int(create), CanCreatePortal: true}
	if !s.pluginFireAny(e) {
		return to, at, search, create, true, true
	}
	if e.Cancelled() {
		return to, at, search, create, false, false
	}
	if e.To.Pos != dest.Pos {
		at = world.SpawnPos{X: int32(math.Floor(e.To.Pos.X)), Y: int32(math.Floor(e.To.Pos.Y)), Z: int32(math.Floor(e.To.Pos.Z))}
	}
	if w, ok := e.To.World.(pluginWorld); ok && w.srv == s && w.dim != nil {
		to = w.dim
	}
	return to, at, int32(max(e.SearchRadius, 0)), int32(max(e.CreationRadius, 0)), e.CanCreatePortal, true
}
