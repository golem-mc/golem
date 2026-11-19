package server

import (
	"cmp"
	"math"
	"math/rand/v2"
	"slices"
	"strconv"
	"sync"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// airBlockState is air's global state ID, always 0 since air is the block
// registry's first entry.
const airBlockState uint16 = 0

// blockRange is the player's minecraft:block_interaction_range, creative
// modifier included. It takes the player's lock.
// vanilla: Player.blockInteractionRange
func (c *client) blockRange() float64 {
	var r float64
	c.player.Attributes(func(a *player.Attributes) {
		r = a.Value(registry.AttributeBlockInteractionRange)
	})
	return r
}

// entityRange is the player's minecraft:entity_interaction_range, creative
// modifier included. It takes the player's lock.
// vanilla: Player.entityInteractionRange
func (c *client) entityRange() float64 {
	var r float64
	c.player.Attributes(func(a *player.Attributes) {
		r = a.Value(registry.AttributeEntityInteractionRange)
	})
	return r
}

// withinAttackRange reports whether an attack packet's target box is in the
// reach of the player's eyes with the main-hand item. It takes the player's
// lock.
// vanilla: ServerGamePacketListenerImpl.handleAttack
func (c *client) withinAttackRange(minX, minY, minZ, maxX, maxY, maxZ float64) bool {
	pos := c.player.Position()
	return rules.WithinAttackRange(c.heldItem(protocol.HandMain), c.player.GameMode() == protocol.GameModeCreative, c.entityRange(),
		pos.X, pos.Y+c.dims(c.player.Appearance().Pose).EyeHeight, pos.Z, minX, minY, minZ, maxX, maxY, maxZ)
}

// blockUpdate builds Block Update for one block; state is a global block
// state ID, as in a chunk palette.
func blockUpdate(x, y, z int32, state uint16) []byte {
	var b protocol.Buffer
	b.PutPosition(x, y, z)
	b.PutVarInt(int32(state))
	return b.Bytes()
}

// sectionBlocks builds Update Section Blocks for one section: its position
// packed into a long, then a VarLong per entry of state and local cell.
func sectionBlocks(key [3]int32, entries []int64) []byte {
	var b protocol.Buffer
	// Absolute signed section coordinates in 22, 22 and 20 bits, Y last.
	b.PutI64(int64(uint64(key[0]&0x3FFFFF)<<42 | uint64(key[1]&0x3FFFFF)<<20 | uint64(key[2]&0xFFFFF)))
	b.PutVarInt(int32(len(entries)))
	for _, entry := range entries {
		b.PutVarLong(entry)
	}
	return b.Bytes()
}

// tellSectionBlocks sends a batch of block changes as one packet per
// section touched: Block Update for a single change, else Update Section
// Blocks. Each is followed by Block Entity Data for cells whose entity has an
// update packet. Sections go chunk by chunk (X, then Z), bottom-up; entries
// keep the batch's order. vanilla: ChunkHolder.broadcastChanges
func (s *Server) tellSectionBlocks(w *world.World, changes []world.BlockChange) {
	if len(changes) == 0 {
		return
	}
	sections := make(map[[3]int32][]world.BlockChange, len(changes))
	keys := make([][3]int32, 0, len(changes))
	for _, ch := range changes {
		key := [3]int32{ch.X >> 4, ch.Z >> 4, ch.Y >> 4}
		if _, ok := sections[key]; !ok {
			keys = append(keys, key)
		}
		sections[key] = append(sections[key], ch)
	}
	slices.SortFunc(keys, func(a, b [3]int32) int {
		return cmp.Or(cmp.Compare(a[0], b[0]), cmp.Compare(a[1], b[1]), cmp.Compare(a[2], b[2]))
	})
	for _, key := range keys {
		changed := sections[key]
		if len(changed) == 1 {
			ch := changed[0]
			s.broadcastBlockUpdate(w, ch.X, ch.Y, ch.Z, ch.State)
		} else {
			entries := make([]int64, len(changed))
			for i, ch := range changed {
				entries[i] = int64(ch.State)<<12 | int64(ch.X&15)<<8 | int64(ch.Z&15)<<4 | int64(ch.Y&15)
			}
			s.broadcastChunkExcept(w, nil, world.ChunkPos{X: key[0], Z: key[1]}, protocol.IDPlaySectionBlocks, sectionBlocks(key, entries))
		}
		for _, ch := range changed {
			s.broadcastBlockEntityIfNeeded(w, ch.X, ch.Y, ch.Z)
		}
	}
}

// broadcastBlockEntityIfNeeded follows a block update with the cell's Block
// Entity Data when its class sends one.
// vanilla: ChunkHolder.broadcastBlockEntityIfNeeded
func (s *Server) broadcastBlockEntityIfNeeded(w *world.World, x, y, z int32) {
	if be := w.BlockEntity(x, y, z); be != nil {
		if registry.UpdatePacketBlockEntityTypes[be.Kind.Name()] {
			s.tellBlockEntity(w, x, y, z, be.Kind.Name(), true)
		}
		return
	}
	if tag, ok := w.BlockEntityTag(x, y, z); ok {
		id, _ := nbt.Get[nbt.String](tag, "id")
		s.tellBlockEntity(w, x, y, z, string(id), false)
	}
}

// worldEvent builds World Event. Event 2001 (block break) carries the
// state ID, from which the client picks the sound and particles.
func worldEvent(event int32, x, y, z int32, data int32) []byte {
	var b protocol.Buffer
	b.PutI32(event)
	b.PutPosition(x, y, z)
	b.PutI32(data)
	b.PutBool(false) // do not disable relative volume
	return b.Bytes()
}

// blockOf is the block containing (x, y, z). vanilla: BlockPos.containing
func blockOf(x, y, z float64) (bx, by, bz int32) {
	return int32(math.Floor(x)), int32(math.Floor(y)), int32(math.Floor(z))
}

// worldEventGlobal builds a World Event heard at full volume everywhere
// (relative volume off), vanilla's globalLevelEvent, e.g. the end portal
// opening.
func worldEventGlobal(event int32, x, y, z int32, data int32) []byte {
	var b protocol.Buffer
	b.PutI32(event)
	b.PutPosition(x, y, z)
	b.PutI32(data)
	b.PutBool(true) // disable relative volume
	return b.Bytes()
}

// putSoundSeed writes the sound packets' trailing Seed, which picks among a
// sound's variants. Vanilla rolls a fresh long per sound (Level's
// thread-safe soundSeedGenerator feeding playSeededSound); the global
// rand.Int64 source is safe for the goroutines building packets.
func putSoundSeed(b *protocol.Buffer) {
	b.PutI64(rand.Int64())
}

// ackBlockChange builds Acknowledge Block Change for one of the client's
// block interactions. The client then adopts the server's block for that
// sequence over its prediction; without it a rejected placement leaves a
// ghost block until the chunk reloads.
func ackBlockChange(seq int32) []byte {
	var b protocol.Buffer
	b.PutVarInt(seq)
	return b.Bytes()
}

// changeBlock sets a block, tells every player and acknowledges the
// requester.
//
// The broadcast includes the requester: its Block Update reconciles the
// client's prediction with the server's decision, e.g. a placed block's
// orientation.
//
// An unchanged block (chunk not loaded, or already that state) is still
// acknowledged, so the client is not left waiting, but not broadcast.
//
// On a change, neighbouring fences, bars and walls reconnect and stairs
// take their corner shape. The physics neighbour update and portal
// revalidation are blockChanged's (physics.go).
// vanilla: Block.updateShape, StairBlock.updateShape
func (c *client) changeBlock(x, y, z int32, state uint16) error {
	c.setUsedBlock(c.theWorld(), x, y, z, state)
	return nil
}

// ackBlock records a block interaction's sequence (a dig, a placement, or a
// refusal's bare number) for the single ack the tick's end sends. Handlers
// record once, after everything the interaction queued. vanilla:
// ServerGamePacketListenerImpl.ackBlockChangesUpTo
//
// Recording keeps the ack behind the interaction's block updates: updates
// travel via the mailbox, sent as each lands, and an ack that overtook its
// update would have the client resolve its prediction before learning the
// result. The highest sequence wins, so several interactions in a tick get
// one packet; a sequence of -1 or less (no-ack calls, or a hostile negative)
// records nothing.
func (c *client) ackBlock(seq int32) {
	for {
		pending := c.pendingAck.Load()
		if seq <= pending || c.pendingAck.CompareAndSwap(pending, seq) {
			return
		}
	}
}

// digState is what a survival dig carries between its packets and ticks,
// as vanilla's ServerPlayerGameMode keeps: the block and tick a START began,
// and the block a STOP left for the ticks to finish when its progress was
// short of the 0.7 a finished dig breaks at. One dig at a time; the next
// START replaces it. The lock is held while the connection goroutine and
// the survival pass touch the fields, and is taken before the world's and
// the inventory's.
type digState struct {
	mu sync.Mutex

	// begun is whether a START put a dig on x, y, z, on tick startTick. It
	// stays set through the STOPs and ABORTs that end it until the next START:
	// vanilla's destroyPos, which a later STOP still names.
	begun     bool
	x, y, z   int32
	startTick int64

	// digging is whether a START is on the block with no STOP or ABORT yet:
	// vanilla's isDestroyingBlock, which decides whether the next START owes
	// the correction for the dig it displaces.
	digging bool

	// delayed is whether a STOP left dx, dy, dz for the ticks to finish, begun
	// on tick dstart: vanilla's hasDelayedDestroy.
	delayed    bool
	dx, dy, dz int32
	dstart     int64

	// lastStage is the crack stage last sent, -1 for none: vanilla's
	// lastSentState, so a stage is not sent twice.
	lastStage int32

	// face is the block face the last START or CHANGE_DESTROY_DIRECTION named:
	// vanilla's destroyDirection, the data of the progress level event.
	face int32
}

// destroyStage is a Set Block Destroy Stage to send after the dig lock.
type destroyStage struct {
	x, y, z, stage int32
}

// sendDestroyStages shows crack stages to players within
// rules.DestroyStageRange in this dimension, except the digger, after the
// dig lock is released. vanilla: ServerLevel.destroyBlockProgress
func (c *client) sendDestroyStages(stages []destroyStage) {
	w := c.theWorld()
	for _, s := range stages {
		var b protocol.Buffer
		b.PutVarInt(c.player.EntityID)
		b.PutPosition(s.x, s.y, s.z)
		b.PutU8(uint8(s.stage))
		c.srv.broadcastNearExcept(w, c, float64(s.x), float64(s.y), float64(s.z),
			rules.DestroyStageRange, protocol.IDPlayBlockDestruction, b.Bytes())
	}
}

// incrementDestroyProgress returns a dig's progress on state by this tick,
// queueing the crack stage if it changed. The caller holds the dig lock.
// vanilla: ServerPlayerGameMode.incrementDestroyProgress
func (c *client) incrementDestroyProgress(state uint16, x, y, z int32, startTick int64, out *[]destroyStage) float32 {
	progress := c.destroyProgress(state) * (float32(c.digTicksInTick()-startTick) + 1)
	if stage := rules.DestroyStage(progress); stage != c.dig.lastStage {
		*out = append(*out, destroyStage{x, y, z, stage})
		c.dig.lastStage = stage
	}
	return progress
}

// digTicksInTick is the tick count the tick's dig pass reads: finished
// main-loop ticks plus one, as the current tick is not over. Packets read
// the plain finished count, vanilla's gameTicks while they are handled.
func (c *client) digTicksInTick() int64 {
	return int64(c.srv.ticksRun.Load()) + 1
}

// startDig starts a survival dig on the block, begun on this packet's tick.
// A displaced dig's block is corrected to the client with vanilla's update,
// since the client's start of the new dig broke its prediction there.
// progress is the first tick's share, whose crack stage goes with the
// start.
// vanilla: ServerPlayerGameMode.handleBlockBreakAction START_DESTROY_BLOCK
func (c *client) startDig(x, y, z int32, progress float32) {
	d := &c.dig
	var out []destroyStage
	defer func() { c.sendDestroyStages(out) }()
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.digging {
		w := c.theWorld()
		c.send(protocol.IDPlayBlockUpdate, blockUpdate(d.x, d.y, d.z, w.BlockAt(d.x, d.y, d.z)))
	}
	d.begun = true
	d.digging = true
	d.x, d.y, d.z = x, y, z
	d.startTick = int64(c.srv.ticksRun.Load())
	d.lastStage = rules.DestroyStage(progress)
	out = append(out, destroyStage{x, y, z, d.lastStage})
}

// stopDigging ends the dig on the named block only; any other block, or
// one never STARTed, changes nothing. Progress at 0.7 of the whole breaks
// the block now; less leaves it for the ticks to finish (the delayed
// destroy), unless one already waits, in which case nothing happens. The
// delayed destroy keeps the START's tick.
// vanilla: ServerPlayerGameMode.handleBlockBreakAction STOP_DESTROY_BLOCK
func (c *client) stopDigging(x, y, z int32) {
	d := &c.dig
	var out []destroyStage
	breakNow := false
	defer func() { c.sendDestroyStages(out) }()
	// The break runs once the lock is released: its listeners may wait on
	// the tick goroutine, which takes the lock in tickDig.
	defer func() {
		if breakNow {
			c.destroyBlock(x, y, z, false)
		}
	}()
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.begun || d.x != x || d.y != y || d.z != z {
		return
	}
	w := c.theWorld()
	state := w.BlockAt(x, y, z)
	if state == airBlockState {
		// Nothing to finish: vanilla neither destroys nor delays on air.
		return
	}
	p := c.destroyProgress(state)
	if p*(float32(int64(c.srv.ticksRun.Load())-d.startTick)+1) >= 0.7 {
		d.digging = false
		out = append(out, destroyStage{x, y, z, -1})
		breakNow = true
		return
	}
	if !d.delayed {
		d.digging = false
		d.delayed = true
		d.dx, d.dy, d.dz = x, y, z
		d.dstart = d.startTick
	}
}

// isDigging reports whether a START's dig is still held.
// vanilla: ServerPlayerGameMode.isDestroyingBlock
func (c *client) isDigging() bool {
	c.dig.mu.Lock()
	defer c.dig.mu.Unlock()
	return c.dig.digging
}

// abortDig drops the released dig, keeping any delayed destroy: vanilla's
// abort clears only isDestroyingBlock, a real client re-STARTing once the
// ack reverts its ghost and aborting when the button comes up. Both the
// dig's block and the named block lose their crack.
// vanilla: ServerPlayerGameMode.handleBlockBreakAction ABORT_DESTROY_BLOCK
func (c *client) abortDig(x, y, z int32) {
	c.pluginBlockDamageAbort(x, y, z)
	d := &c.dig
	var out []destroyStage
	defer func() { c.sendDestroyStages(out) }()
	d.mu.Lock()
	defer d.mu.Unlock()
	d.digging = false
	if d.begun && (d.x != x || d.y != y || d.z != z) {
		out = append(out, destroyStage{d.x, d.y, d.z, -1})
	}
	out = append(out, destroyStage{x, y, z, -1})
}

// clearDigStage clears a current dig's crack for other players as the
// digger leaves.
func (c *client) clearDigStage() {
	d := &c.dig
	d.mu.Lock()
	var out []destroyStage
	if d.delayed {
		out = append(out, destroyStage{d.dx, d.dy, d.dz, -1})
	} else if d.digging {
		out = append(out, destroyStage{d.x, d.y, d.z, -1})
	}
	d.mu.Unlock()
	c.sendDestroyStages(out)
}

// resetDig clears every trace of a dig, the delayed destroy included.
// Vanilla's respawn builds a new body; a crossing keeps its game mode, and
// clearing ours keeps a delayed destroy's coordinates out of the new
// world's ticks (see enterDimension).
func (c *client) resetDig() {
	d := &c.dig
	d.mu.Lock()
	defer d.mu.Unlock()
	// Reset the fields, not the struct: zeroing it whole would copy an unlocked
	// mutex over the held one and the deferred unlock would panic.
	d.begun, d.x, d.y, d.z, d.startTick = false, 0, 0, 0, 0
	d.digging = false
	d.delayed, d.dx, d.dy, d.dz, d.dstart = false, 0, 0, 0, 0
	d.lastStage = -1
	d.face = 0
}

// tickDig is the survival pass's turn at the dig, before the end-of-tick
// ack flush: a STOP's delayed destroy finishes once progress reaches the
// whole block (its update ahead of the ack), and a held dig whose block
// vanished is dropped. Progress counts from the START's tick, one past what
// packets see. vanilla: ServerPlayerGameMode.tick
func (c *client) tickDig() {
	d := &c.dig
	var out []destroyStage
	// The held dig's progress event, sent to everyone in range, the digger
	// included, once the lock is released.
	var ev struct {
		x, y, z, event, data int32
		send                 bool
	}
	defer func() {
		c.sendDestroyStages(out)
		if ev.send {
			c.srv.broadcastLevelEvent(c.theWorld(), ev.event, ev.x, ev.y, ev.z, ev.data)
		}
	}()
	var breakAt *[3]int32
	defer func() {
		if breakAt != nil {
			c.destroyBlock(breakAt[0], breakAt[1], breakAt[2], true)
		}
	}()
	d.mu.Lock()
	defer d.mu.Unlock()
	w := c.theWorld()
	if d.delayed {
		state := w.BlockAt(d.dx, d.dy, d.dz)
		if state == airBlockState {
			d.delayed = false
			return
		}
		if c.incrementDestroyProgress(state, d.dx, d.dy, d.dz, d.dstart, &out) >= 1 {
			d.delayed = false
			breakAt = &[3]int32{d.dx, d.dy, d.dz}
		}
		return
	}
	if d.digging {
		state := w.BlockAt(d.x, d.y, d.z)
		if state == airBlockState {
			out = append(out, destroyStage{d.x, d.y, d.z, -1})
			d.lastStage = -1
			d.digging = false
		} else {
			c.incrementDestroyProgress(state, d.x, d.y, d.z, d.startTick, &out)
			// vanilla: ServerPlayerGameMode.tick (ticksSpentDestroying % 4)
			ev.event = protocol.WorldEventDestroyProgress
			if (c.digTicksInTick()-d.startTick)%4 == 0 {
				ev.event = protocol.WorldEventDestroyProgressSound
			}
			ev.x, ev.y, ev.z, ev.data, ev.send = d.x, d.y, d.z, d.face, true
		}
	}
}

// flushBlockAck sends the ack for the recorded sequences and clears them,
// at the tick's end: vanilla's handler tick is the only place its ack is
// sent, so every update the tick's interactions queued precedes it. Nothing
// recorded sends nothing. vanilla: ServerGamePacketListenerImpl.tick
func (c *client) flushBlockAck() {
	if seq := c.pendingAck.Swap(-1); seq >= 0 {
		if hook := c.srv.beforeAckHook; hook != nil {
			hook()
		}
		// A handler may have changed a block since the tick's flush; send it ahead
		// of the ack. vanilla: MinecraftServer.tickChildren (broadcastChangedChunks)
		c.srv.flushBlockChanges(c.theWorld())
		c.send(protocol.IDPlayAckBlockChange, ackBlockChange(seq))
	}
}

// useItemOnTail ends a Use Item On as vanilla's handleUseItemOn ends every
// interaction, refused or not: echo block updates for the clicked cell and
// the cell on the clicked face, read now. The modern client saved the old
// states it predicted over and reverts from these updates alone, so a
// refusal needs them. The packet's early refusals (client still loading,
// item not enabled, beyond reach or build limit) send only the recorded ack,
// like vanilla's early returns.
//
// After a successful placement the face cell's echo repeats the
// placement's broadcast; vanilla sends both too.
//
// Echoes go via the mailbox and the ack from the tick's end (see ackBlock),
// so the placer sees the interaction's broadcasts, the echoes, then the
// ack. Vanilla writes its echoes before its broadcast, ack still last; the
// client only resolves at the ack, so either order works. Each echo is
// tagged with the interaction's world, so a crossing in between drops it
// rather than planting an old-world block in the new world.
func (c *client) useItemOnTail(w *world.World, x, y, z int32, face int32) error {
	c.sendIn(w, protocol.IDPlayBlockUpdate, blockUpdate(x, y, z, w.BlockAt(x, y, z)))
	off := protocol.FaceOffset[face]
	c.sendIn(w, protocol.IDPlayBlockUpdate, blockUpdate(x+off[0], y+off[1], z+off[2],
		w.BlockAt(x+off[0], y+off[1], z+off[2])))
	return nil
}

// sendBuildLimit shows the build-limit message over the hotbar: the top
// limit when tooHigh, else the minimum.
// vanilla: ServerPlayer.sendBuildLimitMessage
func (c *client) sendBuildLimit(tooHigh bool, limit int32) {
	key := "build.tooLow"
	if tooHigh {
		key = "build.tooHigh"
	}
	c.send(protocol.IDPlaySystemChat, systemChatPayload(protocol.Text{
		Translate: key,
		With:      []protocol.Text{{Text: strconv.Itoa(int(limit))}},
		Color:     protocol.ColorRed,
	}, true))
}

// facingDelta is the block offset a facing property value points to, where
// placement and pair blocks' second halves stand.
var facingDelta = map[string][2]int32{
	"north": {0, -1}, "south": {0, 1}, "west": {-1, 0}, "east": {1, 0},
}

// facingDelta3 is facingDelta over all six directions, for vertical
// facings.
var facingDelta3 = map[string][3]int32{
	"down": {0, -1, 0}, "up": {0, 1, 0},
	"north": {0, 0, -1}, "south": {0, 0, 1}, "west": {-1, 0, 0}, "east": {1, 0, 0},
}

// pairPartner is the cell a broken block takes down with it without drops
// (the lower half under a door's or two-tall plant's upper half, a bed
// foot's head, an extended piston head's base) when breaksPartnerDropless
// says so. Otherwise the partner is left to shape updates, whose
// destruction drops the pair's one item.
// vanilla: DoublePlantBlock.preventDropFromBottomPart, DoorBlock/BedBlock/PistonHeadBlock.playerWillDestroy
func pairPartner(w *world.World, state uint16, x, y, z int32) (px, py, pz int32, ok bool) {
	if registry.BlockOf(state) == registry.BlockPistonHead {
		// PistonHeadBlock.playerWillDestroy takes the base down without drops when
		// the player prevents drops. isFittingBase: the head's own kind, extended,
		// same facing.
		facing := registry.StateValue(state, registry.PropFacing)
		d, known := facingDelta3[facing]
		if !known {
			return 0, 0, 0, false
		}
		bx, by, bz := x-d[0], y-d[1], z-d[2]
		baseState := w.BlockAt(bx, by, bz)
		want := registry.BlockPiston
		if registry.StateValue(state, registry.PropType) == "sticky" {
			want = registry.BlockStickyPiston
		}
		if registry.BlockOf(baseState) != want || !registry.StateBool(baseState, registry.PropExtended) ||
			registry.StateValue(baseState, registry.PropFacing) != facing {
			return 0, 0, 0, false
		}
		return bx, by, bz, true
	}
	if int(state) >= registry.StateCount {
		return 0, 0, 0, false
	}
	if registry.StateValue(state, registry.PropHalf) == "upper" && (registry.BlockOf(state).InGroup(registry.GroupDoor) || registry.BlockOf(state).IsDoublePlant()) {
		if !world.IsLowerHalfBelow(w, registry.BlockOf(state), x, y, z) {
			return 0, 0, 0, false
		}
		return x, y - 1, z, true
	}
	if registry.StateValue(state, registry.PropPart) == "foot" {
		d, known := facingDelta[registry.StateValue(state, registry.PropFacing)]
		if !known {
			return 0, 0, 0, false
		}
		return x + d[0], y, z + d[1], true
	}
	return 0, 0, 0, false
}

// breaksPartnerDropless reports whether this player's break takes the pair
// partner without drops: in creative, or a door the tool has not earned
// drops for. vanilla: DoorBlock.playerWillDestroy, Player.hasCorrectToolForDrops
func (c *client) breaksPartnerDropless(state uint16) bool {
	if c.player.GameMode() == protocol.GameModeCreative {
		return true
	}
	b := registry.BlockOf(state)
	return b.InGroup(registry.GroupDoor) && registry.RequiresCorrectTool(b) &&
		!rules.StackCorrectForDrops(c.heldItem(protocol.HandMain), b)
}

// cracksDecoratedPot reports whether the held item shatters a decorated
// pot: an item in #breaks_decorated_pots, unless it has silk touch's
// prevents_decorated_pot_shattering.
// vanilla: DecoratedPotBlock.playerWillDestroy, EnchantmentHelper.hasTag
func (c *client) cracksDecoratedPot() bool {
	held := c.heldItem(protocol.HandMain)
	if held.Empty() || !registry.Item(held.ItemID).InTag(registry.ItemTagIDBreaksDecoratedPots) {
		return false
	}
	for id := range held.Enchantments {
		if registry.EnchantmentInTag(id, "minecraft:prevents_decorated_pot_shattering") {
			return false
		}
	}
	return true
}

// cutsTripwireWithShears reports whether the main hand holds shears, which
// disarm a tripwire instead of firing it. vanilla: TripWireBlock.playerWillDestroy
func (c *client) cutsTripwireWithShears() bool {
	held := c.heldItem(protocol.HandMain)
	return !held.Empty() && held.ItemID == int32(registry.ItemShears)
}

// destroyProgress is one dig tick's share of the block; 1 or more breaks it
// on the starting click.
func (c *client) destroyProgress(state uint16) float32 {
	if _, _, ok := registry.BlockStateName(state); !ok {
		return 0
	}
	pos := c.player.Position()
	haste, fatigue := rules.DigEffects(c.player.Effects())
	d := rules.Digger{
		EyesInWater: c.habitat(pos).EyesInWater, OnGround: pos.OnGround,
		Haste: haste, MiningFatigue: fatigue,
	}
	c.player.Inventory(func(inv *player.Inventory) {
		d.Held = inv.Slots[player.HotbarSlot+int(inv.Selected)]
	})
	c.player.Attributes(func(a *player.Attributes) {
		d.MiningEfficiency = a.Value(registry.AttributeMiningEfficiency)
		d.BlockBreakSpeed = a.Value(registry.AttributeBlockBreakSpeed)
		d.SubmergedMiningSpeed = a.Value(registry.AttributeSubmergedMiningSpeed)
	})
	return rules.DestroyProgress(registry.BlockOf(state), d)
}

// destroyBlock breaks the block as a finished dig does: partner, sound and
// particles, drops, container spill, wear and exhaustion. The handler and
// the delayed destroy call it; reach and ack are the handler's, as vanilla's
// destroyBlock knows no sequence. vanilla: ServerPlayerGameMode.destroyBlock
func (c *client) destroyBlock(x, y, z int32, onTick bool) {
	w := c.theWorld()
	// The debug stick breaks nothing in any mode: its canDestroyBlock treats
	// the click as a property selection.
	// vanilla: DebugStickItem.canDestroyBlock, ServerPlayerGameMode.destroyAndAck
	if c.holdsDebugStick(protocol.HandMain) {
		c.debugStick(protocol.HandMain, x, y, z, false)
		c.sendIn(w, protocol.IDPlayBlockUpdate, blockUpdate(x, y, z, w.BlockAt(x, y, z)))
		return
	}
	// A tool with can_destroy_blocks_in_creative false (sword, trident, mace)
	// breaks nothing in creative.
	// vanilla: Item.canDestroyBlock, ServerPlayerGameMode.destroyAndAck
	if c.player.GameMode() == protocol.GameModeCreative &&
		!rules.StackCanDestroyInCreative(c.heldItem(protocol.HandMain)) {
		c.sendIn(w, protocol.IDPlayBlockUpdate, blockUpdate(x, y, z, w.BlockAt(x, y, z)))
		return
	}
	state := w.BlockAt(x, y, z)
	// A gamemaster block breaks only for players who can use gamemaster blocks.
	// Others' clients predicted the break, so the block goes back to everyone;
	// the other two refusals resend only the state.
	// vanilla: ServerPlayerGameMode.destroyBlock (level.sendBlockUpdated), GameMasterBlock
	if rules.IsGameMasterBlock(registry.BlockOf(state)) && !c.canUseGameMasterBlocks() {
		c.srv.markChanged(w, x, y, z)
		return
	}
	// A dig allowed when it began ends here if that no longer holds (mode or
	// held stack changed), resending the block instead of breaking it.
	// vanilla: ServerPlayerGameMode.destroyBlock
	if rules.BlockActionRestricted(c.player.GameMode(), c.heldItem(protocol.HandMain), adventureTarget(w, x, y, z, state)) {
		c.sendIn(w, protocol.IDPlayBlockUpdate, blockUpdate(x, y, z, state))
		return
	}
	held := c.heldItem(protocol.HandMain)
	cancelled, loot := c.pluginBlockBreak(w, x, y, z, state, held, onTick)
	if cancelled {
		c.srv.markChanged(w, x, y, z)
		return
	}
	var spawns []func()
	if state != airBlockState {
		// vanilla: playerWillDestroy, setBlock flags 35 and a 2001 to everyone but
		// the breaker
		if c.breaksPartnerDropless(state) {
			if px, py, pz, ok := pairPartner(w, state, x, y, z); ok {
				partner := w.BlockAt(px, py, pz)
				if partner != airBlockState {
					c.srv.broadcastLevelEventExcept(w, c,
						protocol.WorldEventBlockBreak, px, py, pz, int32(partner))

					replacement := world.RemovalLeaves(partner)
					if w.SetBlock(px, py, pz, replacement) {
						c.srv.markChanged(w, px, py, pz)
						c.srv.blockChanged(w, px, py, pz, partner)
					}
				}
			}
		}
		// playerWillDestroy's per-block part, before the break sound: unstable TNT
		// primes, and a decorated pot is cracked before the break event and drops
		// read it.
		name, _, known := registry.BlockStateName(state)
		block := registry.BlockOf(state)
		if known && block.InTag(registry.TagIDGuardedByPiglins) {
			c.angerNearbyPiglins(false)
		}
		if known && block == registry.BlockTnt && registry.StateBool(state, registry.PropUnstable) &&
			c.player.GameMode() != protocol.GameModeCreative {
			// vanilla: TntBlock.playerWillDestroy, TntBlock.prime
			c.srv.primeTNT(w, world.TNTPrime{X: x, Y: y, Z: z})
		}
		// A shulker box's pending table rolls before the drop copies the contents:
		// for a creative breaker isEmpty() rolls it with no player, so no luck and
		// no advancement.
		// vanilla: ShulkerBoxBlock.playerWillDestroy
		if be := w.BlockEntity(x, y, z); be != nil && be.Kind == world.KindShulkerBox {
			if c.player.GameMode() == protocol.GameModeCreative {
				be.Use(w.UnpackLoot)
			} else {
				c.unpackLootFor(w, be)
			}
		}
		if known && c.player.GameMode() == protocol.GameModeCreative {
			// vanilla: ShulkerBoxBlock.playerWillDestroy: the box is added before the
			// break event.
			c.dropCreativeShulkerBox(x, y, z, state)
			c.dropCreativeHive(x, y, z, state)
		}
		if known && block == registry.BlockTripwire && c.cutsTripwireWithShears() {
			// vanilla: TripWireBlock.playerWillDestroy: the cut silently marks the
			// string disarmed before the break, so the removal's wake never fires the
			// hooks.
			if disarmed, ok := registry.WithBool(state, registry.PropDisarmed, true); ok {
				w.SetBlock(x, y, z, disarmed)
				state = disarmed
			}
		}
		if known && block == registry.BlockCreakingHeart {
			c.srv.creakingHeartBroken(w, x, y, z, state, c.player.GameMode())
		}
		if known && block == registry.BlockDecoratedPot && c.cracksDecoratedPot() {
			// vanilla: DecoratedPotBlock.playerWillDestroy
			if cracked, ok := registry.WithBool(state, registry.PropCracked, true); ok {
				w.SetBlock(x, y, z, cracked)
				state = cracked
			}
		}
		// Break sound and particles go to all but the breaker, whose client
		// predicts them; sending World Event to them too would double the sound
		// (vanilla: Block.spawnDestroyParticles uses level.levelEvent(player,
		// 2001, ...), and ServerLevel.levelEvent's PlayerList broadcast excludes
		// the source player).
		//
		// Fire is the exception: BaseFireBlock.playerWillDestroy plays the
		// extinguish event 1009 to everyone, breaker included, and its
		// spawnDestroyParticles is empty, so no 2001.
		if rules.IsFire(state) {
			c.srv.broadcastLevelEvent(w,
				protocol.WorldEventFireExtinguish, x, y, z, 0)

		} else {
			c.srv.broadcastLevelEventExcept(w, c,
				protocol.WorldEventBlockBreak, x, y, z, int32(state))

		}
		// vanilla: Block.playerWillDestroy
		c.srv.gameEvent(w, rules.GameEventBlockDestroy, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5,
			c.gameEventContext(state))
		// Drops are rolled against the standing block and its block entity before
		// breakContainer removes it, and spawned at the cell centre once it is air;
		// a container's contents spill first the same way, and open windows on it
		// close. vanilla: LevelChunk.setBlockState (spill), Block.playerDestroy
		release := c.collectHive(x, y, z, state)
		// The tool takes wear (not in creative) before the drops roll, so a
		// table reading the breaker's SelectedItem sees the worn stack; the roll
		// itself is judged by held, the stack before the wear (vanilla's
		// destroyedWith). None for a zero-destroy-speed block such as a torch.
		// Shears are the exception: their mineBlock skips the speed test, so
		// every block but fire costs their damage per block.
		// vanilla: ServerPlayerGameMode.destroyBlock, Item.mineBlock, ShearsItem.mineBlock
		// A tool's mineBlock returns true regardless of block and damage, which is
		// the use ItemStack.mineBlock counts; creative returns before it.
		if c.player.GameMode() != protocol.GameModeCreative && rules.StackIsTool(held) {
			c.awardItemStat(registry.StatTypeUsed, held.ItemID, 1)
		}
		h, ok := registry.Hardness(registry.BlockOf(state))
		if (!rules.IsFire(state) && held.ItemID == int32(registry.ItemShears)) || (ok && h != 0) {
			c.wearHeld(protocol.HandMain, rules.StackDamagePerBlock(held))
		}
		drops := c.dropBrokenBlock(x, y, z, state, held, loot)
		if drops == nil && loot != nil && loot.experience > 0 {
			drops = func() { c.srv.spawnOrbs(w, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, loot.experience) }
		}
		spawns = append(spawns, c.breakContainer(x, y, z, state), drops, release)
		// The block counts as mined where it would drop, judged by the tool before
		// the break wears it. vanilla: ServerPlayerGameMode.destroyBlock, Block.playerDestroy
		harvests := rules.HasCorrectToolForDrops(held, registry.BlockOf(state))
		if known && c.player.GameMode() != protocol.GameModeCreative && harvests {
			c.awardStat(player.Stat{Type: registry.StatTypeMined, Key: registry.BlockIDs[name]}, 1)
		}
		// Food exhaustion rides Block.playerDestroy, which runs only for a
		// harvest, never in creative.
		if c.player.GameMode() != protocol.GameModeCreative && harvests {
			c.exhaust(rules.MineExhaustion)
		}
	}
	// The handler owns the ack (the delayed destroy has none), and the change
	// cannot fail.
	_ = c.changeBlock(x, y, z, world.RemovalLeaves(state))
	// A surviving player's break takes one egg: the block is put back with one
	// fewer after the removal.
	// vanilla: TurtleEggBlock.decreaseEggs
	if registry.BlockOf(state) == registry.BlockTurtleEgg && c.player.GameMode() != protocol.GameModeCreative {
		broke := c.srv.physicsFor(w).DecreaseTurtleEgg(x, y, z, state)
		c.srv.announceTurtleEggBreak(w, x, y, z, broke)
	}
	// vanilla: IceBlock.playerDestroy
	if registry.BlockOf(state) == registry.BlockIce && c.player.GameMode() != protocol.GameModeCreative &&
		rules.ToolOf(held).Enchantments["minecraft:silk_touch"] == 0 && !w.WaterEvaporates() {
		below := w.BlockAt(x, y-1, z)
		if registry.BlockOf(below).InTag(registry.TagIDIceMeltsWhenDestroyedAbove) || registry.BlockOf(below) == registry.BlockWater || registry.BlockOf(below) == registry.BlockLava {
			_ = c.changeBlock(x, y, z, registry.BlockWater.DefaultState())
		}
	}
	// Bodies spawn after the block change clearing their cell, so the queue
	// goes out first.
	if len(spawns) > 0 {
		c.srv.flushBlockChanges(w)
	}
	for _, spawn := range spawns {
		if spawn != nil {
			spawn()
		}
	}
}

// destroyBlockContents runs Level.destroyBlock's non-write half: a non-fire
// block's 2001 event, the container spilled with its block entity removed
// and windows closed, and the loot rolled. It reports whether the break's
// write would change the cell: false for air, and for a cell already
// holding its fluid's block. vanilla: Level.destroyBlock
func (s *Server) destroyBlockContents(w *world.World, x, y, z int32, old uint16) bool {
	return s.destroyBlockContentsBy(w, x, y, z, old, "")
}

// destroyBlockContentsBy is destroyBlockContents with the breaking body's
// entity type as the loot's this_entity ("" for none).
// vanilla: Level.destroyBlock
func (s *Server) destroyBlockContentsBy(w *world.World, x, y, z int32, old uint16, entityType string) bool {
	if old == airBlockState {
		return false
	}
	if world.IsAirState(old) {
		return false
	}
	if !rules.IsFire(old) {
		s.tellEffect(w, world.Effect{LevelEvent: 2001, LevelEventData: int32(old), X: x, Y: y, Z: z})
	}
	// A skull's compound holds its profile and name; read them before the
	// break removes it.
	ctx := s.brokenBlockLootContext(w, x, y, z, old, nil)
	ctx.This.Type = entityType
	container, customName := s.breakBlockEntity(w, x, y, z, old, true, func(_ *world.BlockEntity, spills []protocol.ItemStack) {
		s.spillContainer(w, x, y, z, spills)
	})
	ctx.Container = container
	if customName != nil {
		ctx.CustomName = customName
	}
	s.rollBlockLoot(w, x, y, z, old, &ctx)
	if rules.IsInfestedBlock(registry.BlockOf(old)) {
		s.spawnInfestation(w, x, y, z)
	}
	// The break leaves the block's fluid, so a cell already that fluid is
	// unchanged.
	return world.RemovalLeaves(old) != old
}

// placeBlock places the held item's block where clicked, broadcasting and
// playing the place sound; the handler acks the sequence afterwards (see
// ackBlock). Non-block items place nothing, and the tail's echoes revert the
// client's prediction.
//
// Adventure players' block uses still work, since vanilla gates only the
// item's useOn on mayBuild (ItemStack.useOn); the item's own uses run only
// where minecraft:can_place_on names the block. A spectator only gets a
// block's menu. A placement costs one item except in creative.
func (c *client) placeBlock(hand, x, y, z int32, ctx rules.PlacementContext) error {
	c.blockUseConsumed = false
	c.defaultBlockUsed = false
	c.itemUsedOn = false
	if c.player.GameMode() == protocol.GameModeSpectator {
		// vanilla: ServerPlayerGameMode.useItemOn
		opened, err := c.spectatorUseBlock(x, y, z)
		if err != nil {
			return err
		}
		// An opened menu is CONSUME, so no build-limit message follows.
		// vanilla: ServerPlayerGameMode.useItemOn
		c.blockUseConsumed = opened
		return c.useItemOnTail(c.theWorld(), x, y, z, ctx.Face)
	}
	face := ctx.Face
	if face < 0 || int(face) >= len(protocol.FaceOffset) {
		return c.useItemOnTail(c.theWorld(), x, y, z, ctx.Face)
	}
	w := c.theWorld()
	// Right-clicking a usable block uses it instead of building against it.
	// Sneaking skips these uses only while either hand holds something (a
	// bare-handed sneak still uses the block), which is how blocks get placed
	// against a chest. vanilla: ServerPlayerGameMode.useItemOn
	if !c.suppressUsingBlock() && !c.denyBlockUse {
		if c.useItemOnBlock(hand, x, y, z, ctx) {
			c.blockUseConsumed = true
			c.itemUsedOn = true
			return c.useItemOnTail(w, x, y, z, ctx.Face)
		}
		// TntBlock.useItemOn is not gated on mayBuild; TntBlock.prime
		// refuses adventure hands whose can_break does not match the TNT.
		if used, err := c.primeTntWithSteel(hand, x, y, z); used {
			c.useSwing = swingPredicted
			c.blockUseConsumed = true
			if err == nil {
				err = c.useItemOnTail(w, x, y, z, ctx.Face)
			}
			return err
		}
		// A fence's own use (vanilla: FenceBlock.useWithoutItem): the player's led
		// mobs around it move onto a knot there, for any hand including empty and
		// without the mayBuild gate item uses have. With no such mobs the click
		// falls through. Sneaking skips this but not the item's use, which ties
		// below. vanilla: FenceBlock.useWithoutItem, LeadItem.bindPlayerMobs
		if handled, err := c.useLeadOnFence(hand, x, y, z); handled {
			c.blockUseConsumed = true
			c.defaultBlockUsed = hand == protocol.HandMain
			if err != nil {
				return err
			}
			return c.useItemOnTail(w, x, y, z, ctx.Face)
		}
		// Block uses vanilla gates on mayBuild (repeater, comparator, redstone wire
		// cycling) answer an adventure hand with the echoes.
		// vanilla: RepeaterBlock.useWithoutItem, ComparatorBlock.useWithoutItem,
		// RedStoneWireBlock.useWithoutItem
		if !rules.ModeMayBuild(c.player.GameMode()) && blockUseNeedsMayBuild(w.BlockAt(x, y, z)) {
			return c.useItemOnTail(w, x, y, z, ctx.Face)
		}
		if used, err := c.useBlock(hand, x, y, z, ctx); used {
			c.blockUseConsumed = true
			c.defaultBlockUsed = hand == protocol.HandMain
			if err == nil {
				err = c.useItemOnTail(w, x, y, z, ctx.Face)
			}
			return err
		}
	}
	if c.denyItemUse {
		return c.useItemOnTail(w, x, y, z, ctx.Face)
	}
	// The item's useOn is behind vanilla's mayBuild gate: in adventure it runs
	// only when minecraft:can_place_on names the clicked block, else the
	// echoes refuse it.
	// vanilla: ItemStack.useOn, ItemStack.canPlaceOnBlockInAdventureMode
	if !rules.ModeMayBuild(c.player.GameMode()) &&
		!rules.CanPlaceOnInAdventureMode(c.heldItem(hand), adventureTarget(w, x, y, z, w.BlockAt(x, y, z))) {
		return c.useItemOnTail(w, x, y, z, ctx.Face)
	}
	// The held item's useOn (itemUsesOn), else its block form.
	u := useOnClick{hand: hand, x: x, y: y, z: z, ctx: ctx}
	if used, err := c.useHeldItemOn(u); used {
		if err != nil {
			return err
		}
		return c.useItemOnTail(w, x, y, z, ctx.Face)
	}
	item := registry.Item(c.heldItem(hand).ItemID)
	consumed, err := c.placeHeldBlock(w, hand, x, y, z, ctx)
	if err != nil {
		return err
	}
	// BlockItem calls Item.useOn only when placement did not consume the action.
	// vanilla: BlockItem.useOn
	if _, block := item.Block(); !consumed && block {
		c.useSwing = swingPredicted
		consumed, err = blockTransformerUseOn.use(c, u)
		if err != nil {
			return err
		}
		if !consumed {
			c.useSwing = swingNone
		}
	}
	// SolidBucketItem wraps both placement and the transformer fallback.
	// vanilla: SolidBucketItem.useOn, BucketItem.getEmptySuccessItem
	if consumed && item == registry.ItemPowderSnowBucket && c.player.GameMode() != protocol.GameModeCreative {
		c.filledResult(hand, protocol.ItemStack{ItemID: int32(registry.ItemBucket), Count: 1}, false)
	}
	return c.useItemOnTail(w, x, y, z, ctx.Face)
}

// placeHeldBlock places the held item's block for a click no use handled,
// reporting whether it placed.
//
// The target is vanilla BlockPlaceContext's: the clicked cell if the item
// can replace it (air, water, grass, or a slab, snow, candle the item adds
// to), else the neighbour on the clicked face. A non-replaceable block or a
// living body (player or mob) there refuses it (vanilla rejects both); the
// refusal's echoes revert the prediction, the tick end's ack behind them.
//
// Orientation comes from the placement context (logs stand or lie,
// furnaces face the player); fence, bars and stair shapes come from the
// four neighbours.
// vanilla: FenceBlock.updateShape, IronBarsBlock.updateShape, StairBlock.getStairsShape
//
// The place sound is the block's SoundType sound event, generated from the
// decompiled client jar (tools/gen blocksounds), so it is exact.
func (c *client) placeHeldBlock(w *world.World, hand, x, y, z int32, ctx rules.PlacementContext) (placed bool, err error) {
	stack := c.heldItem(hand)
	if stack.Empty() {
		return false, nil
	}
	block, ok := registry.Item(stack.ItemID).Block()
	if !ok {
		return false, nil
	}
	defaultState := block.DefaultState()
	// vanilla: GameMasterBlockItem.getPlacementState
	if rules.IsGameMasterBlock(registry.BlockOf(defaultState)) && !c.canUseGameMasterBlocks() {
		return false, nil
	}
	plan, fits := rules.PlanBlockPlacement(w, c.srv.physicsFor(w), block, defaultState, ctx, x, y, z, rand.IntN)
	if plan.TooHigh {
		c.sendBuildLimit(true, w.MinY()+w.Height()-1)
	}
	if !fits {
		return false, nil
	}
	placedBlock, state := plan.Block, plan.State
	nx, ny, nz := plan.X, plan.Y, plan.Z
	upper, triggered := plan.Upper, plan.Triggered
	if c.srv.obstructed(w, nx, ny, nz, state) {
		return false, nil
	}
	// Item components below still override the heart's environment-derived state.
	// vanilla: CreakingHeartBlock.getStateForPlacement
	if registry.BlockOf(state) == registry.BlockCreakingHeart {
		state = world.CreakingHeartPlacedState(w, state, nx, ny, nz)
	}
	// vanilla: PotentSulfurBlock.getStateForPlacement
	if registry.BlockOf(state) == registry.BlockPotentSulfur {
		state, _ = world.PotentSulfurState(w.BlockAt, state, nx, ny, nz)
	}
	// Components apply after validation; partners and wakes see the final state.
	// vanilla: BlockItem.updateBlockStateFromTag
	if props, ok := stack.BlockStatePropertiesOf(); ok {
		state = rules.BlockStateFromItemComponent(state, props)
		// The upper half copies the component-modified facing.
		// vanilla: SmallDripleafBlock.setPlacedBy
		if registry.BlockOf(state).IsDoublePlant() {
			upper = rules.DoublePlantUpper(w, state, nx, ny, nz)
		}
	}
	old := w.BlockAt(nx, ny, nz)
	if c.pluginBlockPlace(hand, nx, ny, nz, state, [3]int32{x, y, z}, stack, upper) {
		return false, nil
	}
	policy := writeLight | writeLightAfter
	if triggered {
		policy |= writeCrafter
	}
	if c.srv.writeBlock(w, nx, ny, nz, state, nil, policy) {
		placed = true
		// vanilla: DoorBlock.setPlacedBy
		if registry.BlockOf(state).InGroup(registry.GroupDoor) {
			if upper, ok := registry.WithProp(state, registry.PropHalf, "upper"); ok {
				c.srv.placePartner(w, nx, ny+1, nz, upper, airBlockState)
			}
		}
		// vanilla: DoublePlantBlock.setPlacedBy
		if upper != 0 {
			c.srv.placePartner(w, nx, ny+1, nz, upper, w.BlockAt(nx, ny+1, nz))
		}
		// vanilla: BedBlock.setPlacedBy
		if registry.BlockOf(state).InGroup(registry.GroupBed) {
			if hd, known := facingDelta[registry.StateValue(state, registry.PropFacing)]; known {
				if head, ok := registry.WithProp(state, registry.PropPart, "head"); ok {
					hx, hy, hz := nx+hd[0], ny, nz+hd[1]
					c.srv.placePartner(w, hx, hy, hz, head, airBlockState)
				}
			}
		}
		// vanilla: MossyCarpetBlock.setPlacedBy
		if registry.BlockOf(state) == registry.BlockPaleMossCarpet {
			if topper, ok := world.MossyCarpetTopper(w, nx, ny, nz, func() bool { return c.srv.levelRng.Intn(2) == 1 }); ok {
				c.srv.placePartner(w, nx, ny+1, nz, topper, w.BlockAt(nx, ny+1, nz))
				c.srv.updateNeighborShapes(w, nx, ny+1, nz)
			}
		}
		c.containerPlaced(state, nx, ny, nz, stack)
		c.structureBlockPlaced(w, placedBlock, nx, ny, nz)
		c.commandBlockPlaced(w, placedBlock, nx, ny, nz, stack)
		c.signPlaced(placedBlock, nx, ny, nz, stack)
		w.EnsureBlockEntityForBlock(nx, ny, nz)
		c.carriedComponentsPlaced(w, placedBlock, nx, ny, nz, stack)
		// vanilla: BlockEntity.applyComponentsFromItemStack
		if be := w.BlockEntity(nx, ny, nz); be != nil {
			be.Use(func(b *world.BlockEntity) { b.ApplyLeftoverComponents(stack) })
		}
		solidBucket := c.finishBlockPlacement(w, registry.Item(stack.ItemID), state, nx, ny, nz, old)
		// vanilla: ItemStack.useOn
		c.awardItemStat(registry.StatTypeUsed, stack.ItemID, 1)
		// Confirm the client's predicted spend, including adventure placements.
		if !solidBucket && c.player.GameMode() != protocol.GameModeCreative {
			if err := c.consumeHeld(hand); err != nil {
				return true, err
			}
		}
	}
	return placed, nil
}

// blockUseNeedsMayBuild reports whether the block's use requires a hand
// that may build. These are blocks whose own use vanilla gates on mayBuild like
// the item's: repeater, comparator, redstone wire and daylight detector.
// vanilla: RepeaterBlock.useWithoutItem, ComparatorBlock.useWithoutItem,
// RedStoneWireBlock.useWithoutItem, DaylightDetectorBlock.useWithoutItem
func blockUseNeedsMayBuild(state uint16) bool {
	switch registry.BlockOf(state) {
	case registry.BlockRepeater, registry.BlockComparator, registry.BlockRedstoneWire, registry.BlockDaylightDetector:
		return true
	}
	return false
}

// suppressUsingBlock gates the block's own uses: sneaking skips them only
// while either hand holds something. vanilla: ServerPlayerGameMode.useItemOn
func (c *client) suppressUsingBlock() bool {
	if !c.player.Appearance().Sneaking {
		return false
	}
	return !c.heldItem(protocol.HandMain).Empty() || !c.heldItem(protocol.HandOff).Empty()
}

// heldItem returns the hand's stack, empty for a nonexistent hand.
func (c *client) heldItem(hand int32) protocol.ItemStack {
	var stack protocol.ItemStack
	c.player.Inventory(func(inv *player.Inventory) {
		switch hand {
		case protocol.HandMain:
			stack = inv.Slots[player.HotbarSlot+int(inv.Selected)]
		case protocol.HandOff:
			stack = inv.Slots[player.OffHandSlot]
		}
	})
	return stack
}

// wornArmour returns the worn pieces in a mob's Worn order: feet, legs,
// chest, head.
func (c *client) wornArmour() [4]protocol.ItemStack {
	var worn [4]protocol.ItemStack
	c.player.Inventory(func(inv *player.Inventory) {
		eq := inv.Equipment()
		copy(worn[:], eq[len(eq)-player.ArmorSlots:])
	})
	return worn
}

// placedBlockTail ends every player placement: the place sound for all but
// the placer (who predicts it), neighbour shape updates, the wake, and the
// block_place game event.
func (c *client) placedBlockTail(w *world.World, tool int32, sound registry.Sound, volume, pitch float32, x, y, z int32, old uint16) {
	c.srv.broadcastSoundIDExcept(w, c,
		sound, protocol.SoundCategoryBlock,
		float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, volume, pitch)

	// Neighbouring fences, bars and walls connect, and stairs take their corner
	// shape. vanilla: Block.updateShape, StairBlock.updateShape
	c.srv.updateNeighborShapes(w, x, y, z)
	c.srv.blockChanged(w, x, y, z, old)
	c.itemUsedOn = true
	c.useSwing = swingPredicted
	c.placedBlockAdvancements(w, x, y, z, tool)
	// vanilla: BlockItem.place
	c.srv.gameEvent(w, rules.GameEventBlockPlace, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5,
		c.gameEventContext(w.BlockAt(x, y, z)))
}

func (s *Server) placePartner(w *world.World, x, y, z int32, state, old uint16) {
	if s.writeBlock(w, x, y, z, state, nil, 0) {
		s.blockChanged(w, x, y, z, old)
	}
}

func (c *client) finishBlockPlacement(w *world.World, item registry.Item, state uint16, nx, ny, nz int32, old uint16) bool {
	// The dried ghast's SoundType has an empty place event, so the broadcast is
	// silent; its setPlacedBy plays its own arrival sound (wet or dry), to the
	// placer too. vanilla: DriedGhastBlock.setPlacedBy
	if registry.BlockOf(state) == registry.BlockDriedGhast {
		event := registry.SoundBlockDriedGhastPlace
		if registry.StateBool(state, registry.PropWaterlogged) {
			event = registry.SoundBlockDriedGhastPlaceInWater
		}
		c.srv.broadcastSoundID(w,
			event, protocol.SoundCategoryBlock,
			float64(nx)+0.5, float64(ny)+0.5, float64(nz)+0.5, 1.0, 1.0)

	}
	placeSound := rules.BlockPlaceSound(registry.BlockOf(state))
	bucketSound, solidBucket := rules.SolidBucketPlaceSound(item)
	if solidBucket {
		// BlockItem.place plays the override at pitch*0.8 like any block.
		// vanilla: BlockItem.place, SolidBucketItem.getPlaceSound
		placeSound = bucketSound
	}
	volume, pitch := rules.BlockPlaceSoundLevels(registry.BlockOf(state))
	c.placedBlockTail(w, int32(item), placeSound, volume, pitch, nx, ny, nz, old)
	// vanilla: DiodeBlock.setPlacedBy
	if block := registry.BlockOf(state); block == registry.BlockRepeater || block == registry.BlockComparator {
		c.srv.physicsFor(w).DiodePlaced(nx, ny, nz)
	}
	// A player-placed tripwire hook scans its line at once, after the
	// placement's wake, in vanilla's setPlacedBy order; /setblock never runs it.
	// vanilla: TripWireHookBlock.setPlacedBy
	if registry.BlockOf(state) == registry.BlockTripwireHook {
		changes, effects, primes := c.srv.physicsFor(w).TripwireHookPlaced(nx, ny, nz)
		c.srv.tellBlockChanges(w, changes)
		for _, e := range effects {
			c.srv.tellEffect(w, e)
		}
		for _, t := range primes {
			c.srv.primeTNT(w, t)
		}
	}
	// vanilla: WitherSkullBlock.setPlacedBy
	if block := registry.BlockOf(state); block == registry.BlockWitherSkeletonSkull || block == registry.BlockWitherSkeletonWallSkull {
		c.srv.trySummonWither(w, nx, ny, nz)
	}
	return solidBucket
}
