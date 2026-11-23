package inventory

import (
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// Handless container-to-container moves: hopper eject and suck, dropper
// push, dispenser put-backs. Faces are protocol.Face* numbers.
// vanilla: HopperBlockEntity.addItem, tryMoveInItem

// Block is a container block entity as the moves see it: its slots, and the
// window kind it opens, which picks the face rules. Transfer functions keep
// concrete Block types as type parameters so value adapters need no heap box.
type Block interface {
	Container
	// Stacks is the block's slots, for reading; writes go through Put.
	Stacks() []protocol.ItemStack
	// Len is how many slots the block has.
	Len() int
	// Peek is the slot's item and count, read without copying the stack.
	Peek(slot int) (itemID, count int32)
	// Filled is the mask of slots holding items: bit i is set exactly when
	// slot i's count is positive (at most 64 slots).
	Filled() uint64
	Kind() Kind
}

// Compound is two blocks as one, the first's slots then the second's: a
// double chest's 54, right half first. vanilla: CompoundContainer
type Compound struct {
	First, Second Block
}

// Slot is what the given one of the pair's slots holds.
func (c Compound) Slot(slot int) protocol.ItemStack {
	if n := c.First.Len(); slot >= n {
		return c.Second.Slot(slot - n)
	}
	return c.First.Slot(slot)
}

// Peek is the item and count of the given one of the pair's slots.
func (c Compound) Peek(slot int) (itemID, count int32) {
	if n := c.First.Len(); slot >= n {
		return c.Second.Peek(slot - n)
	}
	return c.First.Peek(slot)
}

// Filled is the pair's masks joined, the second half above the first's slots.
func (c Compound) Filled() uint64 { return c.First.Filled() | c.Second.Filled()<<c.First.Len() }

// Put replaces what the given one of the pair's slots holds.
func (c Compound) Put(slot int, stack protocol.ItemStack) {
	if n := c.First.Len(); slot >= n {
		c.Second.Put(slot-n, stack)
		return
	}
	c.First.Put(slot, stack)
}

// Stacks is a copy of the pair's slots.
func (c Compound) Stacks() []protocol.ItemStack {
	return append(slices.Clone(c.First.Stacks()), c.Second.Stacks()...)
}

// Len is how many slots the pair has: both halves'.
func (c Compound) Len() int { return c.First.Len() + c.Second.Len() }

// Kind is the double chest's window.
func (c Compound) Kind() Kind { return KindDoubleChest }

// slotRange is 0..n-1 for every block size, shared and never written:
// vanilla's CACHED_SLOTS, so a move allocates no slot list.
var slotRange = func() []int {
	r := make([]int, 54)
	for i := range r {
		r[i] = i
	}
	return r
}()

// Slots per face. Furnace: vanilla's SLOTS_FOR_UP, _DOWN and _SIDES.
// Brewing stand: ingredient from the top, bottles and ingredient from
// below, bottles and fuel from the sides. Composter: the input's top and
// the output's bottom only.
var (
	furnaceTop    = []int{0}
	furnaceBottom = []int{2, 1}
	furnaceSides  = []int{1}

	brewingTop    = []int{3}
	brewingBottom = []int{0, 1, 2, 3}
	brewingSides  = []int{0, 1, 2, 4}

	composterTop    = []int{0}
	composterBottom = []int{0}
)

// allSlots is every slot of a block in order.
func allSlots[B Block](b B) []int {
	n := b.Len()
	if n <= len(slotRange) {
		return slotRange[:n]
	}
	slots := make([]int, n)
	for i := range slots {
		slots[i] = i
	}
	return slots
}

// isWorldly reports a face-aware container (vanilla: WorldlyContainer): the
// cooking containers, brewing stand, shulker box and composter, whose two
// views are all a move reaches it by.
func isWorldly(kind Kind) bool {
	return kind.Cooking() || kind == KindBrewingStand || kind == KindShulker ||
		kind == KindComposterInput || kind == KindComposterOutput || kind == KindComposterFull
}

// slotsForFace is the slots a face reaches (vanilla: getSlots); the full
// composter reaches none, non-worldly blocks every slot.
func slotsForFace[B Block](b B, face int32) []int {
	switch {
	case b.Kind().Cooking():
		switch face {
		case protocol.FaceTop:
			return furnaceTop
		case protocol.FaceBottom:
			return furnaceBottom
		default:
			return furnaceSides
		}
	case b.Kind() == KindBrewingStand:
		switch face {
		case protocol.FaceTop:
			return brewingTop
		case protocol.FaceBottom:
			return brewingBottom
		default:
			return brewingSides
		}
	case b.Kind() == KindComposterInput:
		if face == protocol.FaceTop {
			return composterTop
		}
		return nil
	case b.Kind() == KindComposterOutput:
		if face == protocol.FaceBottom {
			return composterBottom
		}
		return nil
	case b.Kind() == KindComposterFull:
		return nil
	}
	return allSlots(b)
}

// storedBlock is a Block whose slot may hold a compound kept whole, or a
// composter input that already took its item: Slot reads empty, yet the
// cell is occupied for a move.
type storedBlock interface {
	Stored(slot int) bool
}

// CanPlaceItem is a container's own slot rule without WorldlyContainer face
// rules, so a shulker box and the composter take anything.
// vanilla: Container.canPlaceItem
func CanPlaceItem[B Block](b B, stack protocol.ItemStack, slot int) bool {
	switch b.Kind() {
	case KindBrewingStand:
		// vanilla: BrewingStandBlockEntity.canPlaceItem
		return canPlaceThroughFace(b, stack, slot)
	case KindJukebox:
		// vanilla: JukeboxBlockEntity.canPlaceItem
		_, ok := registry.JukeboxSongForStack(stack)
		return ok && b.Slot(slot).Empty()
	case KindCrafter:
		return crafterCanPlace(b, slot)
	case KindShelf:
		// vanilla: ListBackedContainer.canPlaceItem
		if s, ok := any(b).(storedBlock); ok && s.Stored(slot) {
			return false
		}
		in := b.Slot(slot)
		return in.Empty() || in.Count < registry.StackMaxStackSize(stack)
	case KindShulker, KindComposterInput, KindComposterOutput, KindComposterFull:
		return true
	}
	return canPlaceThroughFace(b, stack, slot)
}

// crafterCanPlace: an enabled, non-full slot that is empty or has no later
// enabled slot empty or holding a smaller stack of the same item.
// vanilla: CrafterBlockEntity.canPlaceItem
func crafterCanPlace[B Block](b B, slot int) bool {
	if blockSlotDisabled(b, slot) {
		return false
	}
	in := b.Slot(slot)
	if in.Empty() {
		return true
	}
	if in.Count >= registry.StackMaxStackSize(in) {
		return false
	}
	for i := slot + 1; i < b.Len() && i < 9; i++ {
		if blockSlotDisabled(b, i) {
			continue
		}
		other := b.Slot(i)
		if other.Empty() || (other.Count < in.Count && other.Same(in)) {
			return false
		}
	}
	return true
}

// canPlaceThroughFace: furnace input anything, fuel slot fuel (an empty
// bucket only while it holds none), output nothing; brewing bottles a potion
// or empty bottle, ingredient and fuel theirs; a shulker box not its own
// kind; the composter input one compostable, its output nothing.
// vanilla: canPlaceItemInContainer
func canPlaceThroughFace[B Block](b B, stack protocol.ItemStack, slot int) bool {
	switch b.Kind() {
	case KindFurnace, KindBlastFurnace, KindSmoker:
		switch slot {
		case 0:
			return true
		case 1:
			if IsCookingFuel(stack) {
				return true
			}
			return registry.Item(stack.ItemID) == registry.ItemBucket && registry.Item(b.Slot(1).ItemID) != registry.ItemBucket
		default:
			return false
		}
	case KindBrewingStand:
		switch slot {
		case 0, 1, 2:
			// vanilla: BrewingStandBlockEntity.canPlaceItem
			return isPotionBottle(stack) && b.Slot(slot).Empty()
		case 3:
			return isBrewingIngredient(registry.Item(stack.ItemID))
		case 4:
			return IsBrewingFuel(stack)
		}
		return false
	case KindCrafter:
		return crafterAccepts(b, slot, stack)
	case KindShulker:
		return !isShulkerBoxItem(stack)
	case KindJukebox:
		// vanilla: JukeboxBlockEntity.canPlaceItem
		_, ok := registry.JukeboxSongForStack(stack)
		return ok
	case KindChiseledBookshelf:
		// Only shelf books (the item tag), and with a stack size of one only
		// into an empty cell. A compound kept whole reads empty through Slot,
		// so the cell's occupancy must be checked too.
		// vanilla: ChiseledBookShelfBlockEntity.acceptsItemType,
		// ListBackedContainer.canPlaceItem
		if s, ok := any(b).(storedBlock); ok && s.Stored(slot) {
			return false
		}
		return registry.Item(stack.ItemID).InTag(registry.ItemTagIDBookshelfBooks) && b.Slot(slot).Empty()
	case KindComposterInput:
		// The container takes one item and then refuses.
		// vanilla: ComposterBlock.InputContainer.canPlaceItemThroughFace
		if s, ok := any(b).(storedBlock); ok && s.Stored(slot) {
			return false
		}
		return isCompostable(stack)
	case KindComposterOutput, KindComposterFull:
		return false
	default:
		return true
	}
}

// crafterAccepts: a disabled slot takes nothing; a stack goes only into the
// slot its even spread picks, the first enabled empty cell, else the enabled
// cell with the smallest same-item stack.
// vanilla: CrafterBlockEntity.canPlaceItem
func crafterAccepts[B Block](b B, slot int, stack protocol.ItemStack) bool {
	return crafterTarget(b, stack) == slot
}

// crafterTarget is the cell a stack goes into, -1 when every enabled cell
// holds a different item.
func crafterTarget[B Block](b B, stack protocol.ItemStack) int {
	best := -1
	for i := 0; i < b.Len(); i++ {
		if blockSlotDisabled(b, i) {
			continue
		}
		in := b.Slot(i)
		if in.Empty() {
			return i
		}
		if sameStack(in, stack) && (best < 0 || in.Count < b.Slot(best).Count) {
			best = i
		}
	}
	return best
}

// blockSlotDisabled reports whether a block's slot is disabled by the
// container's own rule, the crafter's grid among them.
func blockSlotDisabled[B Block](b B, slot int) bool {
	if d, ok := any(b).(disabledContainer); ok {
		return d.SlotDisabled(slot)
	}
	return false
}

// canTakeThroughFace: from the bottom the furnace fuel slot gives only empty
// or water buckets, the brewing ingredient slot only leftover empty bottles,
// the composter output only bone meal. The composter input gives nothing.
// vanilla: canTakeItemFromContainer
func canTakeThroughFace[B Block](b B, stack protocol.ItemStack, slot int, face int32) bool {
	if b.Kind().Cooking() && face == protocol.FaceBottom && slot == 1 {
		return registry.Item(stack.ItemID) == registry.ItemWaterBucket || registry.Item(stack.ItemID) == registry.ItemBucket
	}
	if b.Kind() == KindBrewingStand && slot == 3 {
		return registry.Item(stack.ItemID) == registry.ItemGlassBottle
	}
	if b.Kind() == KindComposterOutput {
		return face == protocol.FaceBottom && registry.Item(stack.ItemID) == registry.ItemBoneMeal
	}
	if b.Kind() == KindComposterInput || b.Kind() == KindComposterFull {
		return false
	}
	return true
}

// isCompostable reports whether the stack has a compostable component.
// vanilla: ComposterBlock.InputContainer.canPlaceItemThroughFace
func isCompostable(stack protocol.ItemStack) bool {
	_, ok := registry.StackCompostable(stack)
	return ok
}

// canMergeItems is vanilla's canMergeItems.
func canMergeItems(a, b protocol.ItemStack) bool {
	if a.Count > registry.StackMaxStackSize(a) {
		return false
	}
	return a.Same(b)
}

// Full is vanilla's isFullContainer over the slots a face reads.
func Full[B Block](b B, face int32) bool {
	for _, slot := range slotsForFace(b, face) {
		if _, count := b.Peek(slot); count <= 0 || count < registry.StackMaxStackSize(b.Slot(slot)) {
			return false
		}
	}
	return true
}

// Empty tells whether a block holds nothing at all.
func Empty[B Block](b B) bool {
	for i := 0; i < b.Len(); i++ {
		if _, count := b.Peek(i); count > 0 {
			return false
		}
	}
	return true
}

// AddItem pours stack into the face's slots (all, unless throughFace and the
// block is worldly), merging then filling. It returns the leftover and
// whether an empty block received something, which starts a hopper's
// chained cooldown.
// vanilla: HopperBlockEntity.addItem
func AddItem[B Block](dest B, stack protocol.ItemStack, face int32, throughFace bool) (left protocol.ItemStack, intoEmpty bool) {
	slots := allSlots(dest)
	if throughFace && isWorldly(dest.Kind()) {
		slots = slotsForFace(dest, face)
	}
	wasEmpty := Empty(dest)
	changed := false
	for _, slot := range slots {
		if stack.Empty() {
			break
		}
		var moved bool
		stack, moved = tryMoveInItem(dest, stack, slot)
		changed = changed || moved
	}
	return stack, wasEmpty && changed
}

// tryMoveInItem is vanilla's tryMoveInItem: one slot's share of a pour.
func tryMoveInItem[B Block](dest B, stack protocol.ItemStack, slot int) (protocol.ItemStack, bool) {
	if !canPlaceThroughFace(dest, stack, slot) {
		return stack, false
	}
	id, count := dest.Peek(slot)
	if count <= 0 {
		dest.Put(slot, stack)
		return protocol.ItemStack{}, true
	}
	// Room and item first: a full or foreign slot is the common refusal and
	// needs neither the stack nor a component comparison.
	if id != stack.ItemID {
		return stack, false
	}
	item := dest.Slot(slot)
	move := min(registry.StackMaxStackSize(item)-item.Count, stack.Count)
	if move <= 0 || !canMergeItems(item, stack) {
		return stack, false
	}
	stack.Count -= move
	item.Count += move
	dest.Put(slot, item)
	return stack, true
}

// Gate lets a caller veto or replace the one item a hopper move is about to
// transfer. ok false ends the move without a transfer.
type Gate interface {
	Check(one protocol.ItemStack) (replacement protocol.ItemStack, ok bool)
}

// Eject is vanilla's ejectItems: the first stack of src that dest takes one
// item of through face gives that item up.
func Eject[S, D Block](src S, dest D, face int32) (moved, intoEmpty bool) {
	return EjectGated(src, dest, face, nil)
}

// EjectGated is Eject asking gate (when non-nil) about the first stack,
// before dest is tried.
func EjectGated[S, D Block](src S, dest D, face int32, gate Gate) (moved, intoEmpty bool) {
	if Full(dest, face) {
		return false, false
	}
	for i := 0; i < src.Len(); i++ {
		if _, count := src.Peek(i); count <= 0 {
			continue
		}
		stack := src.Slot(i)
		one := stack
		one.Count = 1
		if gate != nil {
			var ok bool
			if one, ok = gate.Check(one); !ok {
				return false, false
			}
		}
		left, into := AddItem(dest, one, face, true)
		if !left.Empty() {
			continue
		}
		src.Put(i, shrunk(stack))
		return true, into
	}
	return false, false
}

// Suck moves one item from the first slot of src's bottom face the hopper
// accepts, and reports whether the hopper was empty.
// vanilla: HopperBlockEntity.suckInItems
func Suck[S, D Block](src S, hopper D) (moved, intoEmpty bool) {
	return SuckGated(src, hopper, nil)
}

// SuckGated is Suck asking gate (when non-nil) about the first stack the
// hopper could take from, before the hopper is tried.
func SuckGated[S, D Block](src S, hopper D, gate Gate) (moved, intoEmpty bool) {
	filled := src.Filled()
	if filled == 0 {
		return false, false
	}
	for _, slot := range slotsForFace(src, protocol.FaceBottom) {
		if slot < 64 && filled&(1<<slot) == 0 {
			continue
		}
		stack := src.Slot(slot)
		if !canTakeThroughFace(src, stack, slot, protocol.FaceBottom) {
			continue
		}
		one := stack
		one.Count = 1
		if gate != nil {
			var ok bool
			if one, ok = gate.Check(one); !ok {
				return false, false
			}
		}
		left, into := AddItem(hopper, one, 0, false)
		if !left.Empty() {
			continue
		}
		src.Put(slot, shrunk(stack))
		return true, into
	}
	return false, false
}

// shrunk is a stack one item smaller, empty once it runs out.
func shrunk(stack protocol.ItemStack) protocol.ItemStack {
	stack.Count--
	if stack.Count <= 0 {
		return protocol.ItemStack{}
	}
	return stack
}
