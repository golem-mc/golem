package inventory

import (
	"math/bits"
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
	// StackPtr is the slot's stack in place for reading, or nil if out of range
	// or unbacked; writes go through Put.
	StackPtr(slot int) *protocol.ItemStack
	// Filled is the mask of slots holding items: bit i is set exactly when
	// slot i's count is positive (at most 64 slots).
	Filled() uint64
	Kind() Kind
}

// Pair is two blocks as one, the first's slots then the second's: a
// double chest's 54, right half first. The halves keep their concrete type so
// value adapters need no heap box. vanilla: CompoundContainer
type Pair[B Block] struct {
	First, Second B
}

// Compound is a Pair of interface-typed halves, for window and interface callers.
type Compound = Pair[Block]

// Slot is what the given one of the pair's slots holds.
func (c Pair[B]) Slot(slot int) protocol.ItemStack {
	if n := c.First.Len(); slot >= n {
		return c.Second.Slot(slot - n)
	}
	return c.First.Slot(slot)
}

// Peek is the item and count of the given one of the pair's slots.
func (c Pair[B]) Peek(slot int) (itemID, count int32) {
	if n := c.First.Len(); slot >= n {
		return c.Second.Peek(slot - n)
	}
	return c.First.Peek(slot)
}

// StackPtr is the given slot's stack in place for reading.
func (c Pair[B]) StackPtr(slot int) *protocol.ItemStack {
	if n := c.First.Len(); slot >= n {
		return c.Second.StackPtr(slot - n)
	}
	return c.First.StackPtr(slot)
}

// Filled is the pair's masks joined, the second half above the first's slots.
func (c Pair[B]) Filled() uint64 { return c.First.Filled() | c.Second.Filled()<<c.First.Len() }

// Put replaces what the given one of the pair's slots holds.
func (c Pair[B]) Put(slot int, stack protocol.ItemStack) {
	if n := c.First.Len(); slot >= n {
		c.Second.Put(slot-n, stack)
		return
	}
	c.First.Put(slot, stack)
}

// Stacks is a copy of the pair's slots.
func (c Pair[B]) Stacks() []protocol.ItemStack {
	return append(slices.Clone(c.First.Stacks()), c.Second.Stacks()...)
}

// Len is how many slots the pair has: both halves'.
func (c Pair[B]) Len() int { return c.First.Len() + c.Second.Len() }

// Kind is the double chest's window.
func (c Pair[B]) Kind() Kind { return KindDoubleChest }

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
		return canPlaceThroughFace(b, &stack, slot)
	case KindJukebox:
		// vanilla: JukeboxBlockEntity.canPlaceItem
		_, ok := registry.JukeboxSongForStack(stack)
		_, count := b.Peek(slot)
		return ok && count <= 0
	case KindCrafter:
		return crafterCanPlace(b, slot)
	case KindShelf:
		// vanilla: ListBackedContainer.canPlaceItem
		if s, ok := any(b).(storedBlock); ok && s.Stored(slot) {
			return false
		}
		_, count := b.Peek(slot)
		return count <= 0 || count < registry.StackMaxStackSizePtr(&stack)
	case KindShulker, KindComposterInput, KindComposterOutput, KindComposterFull:
		return true
	}
	return canPlaceThroughFace(b, &stack, slot)
}

// crafterCanPlace: an enabled, non-full slot that is empty or has no later
// enabled slot empty or holding a smaller stack of the same item.
// vanilla: CrafterBlockEntity.canPlaceItem
func crafterCanPlace[B Block](b B, slot int) bool {
	if blockSlotDisabled(b, slot) {
		return false
	}
	in := b.StackPtr(slot)
	if in == nil || in.Count <= 0 {
		return true
	}
	if in.Count >= registry.StackMaxStackSizePtr(in) {
		return false
	}
	for i := slot + 1; i < b.Len() && i < 9; i++ {
		if blockSlotDisabled(b, i) {
			continue
		}
		other := b.StackPtr(i)
		if other == nil || other.Count <= 0 || (other.Count < in.Count && other.SamePtr(in)) {
			return false
		}
	}
	return true
}

// isPlainContainer reports a non-worldly container whose slots accept any stack
// through any face.
func isPlainContainer(kind Kind) bool {
	switch kind {
	case KindHopper, KindChest, KindDoubleChest, KindBarrel, KindDispenser, KindDropper:
		return true
	}
	return false
}

// canPlaceThroughFace: furnace input anything, fuel slot fuel (an empty
// bucket only while it holds none), output nothing; brewing bottles a potion
// or empty bottle, ingredient and fuel theirs; a shulker box not its own
// kind; the composter input one compostable, its output nothing.
// vanilla: canPlaceItemInContainer
func canPlaceThroughFace[B Block](b B, stack *protocol.ItemStack, slot int) bool {
	if isPlainContainer(b.Kind()) {
		return true
	}
	switch b.Kind() {
	case KindFurnace, KindBlastFurnace, KindSmoker:
		switch slot {
		case 0:
			return true
		case 1:
			if IsCookingFuel(*stack) {
				return true
			}
			id, _ := b.Peek(1)
			return registry.Item(stack.ItemID) == registry.ItemBucket && registry.Item(id) != registry.ItemBucket
		default:
			return false
		}
	case KindBrewingStand:
		switch slot {
		case 0, 1, 2:
			// vanilla: BrewingStandBlockEntity.canPlaceItem
			_, count := b.Peek(slot)
			return isPotionBottle(*stack) && count <= 0
		case 3:
			return isBrewingIngredient(registry.Item(stack.ItemID))
		case 4:
			return IsBrewingFuel(*stack)
		}
		return false
	case KindCrafter:
		return crafterAccepts(b, slot, stack)
	case KindShulker:
		return !isShulkerBoxItem(*stack)
	case KindJukebox:
		// vanilla: JukeboxBlockEntity.canPlaceItem
		_, ok := registry.JukeboxSongForStack(*stack)
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
		_, count := b.Peek(slot)
		return registry.Item(stack.ItemID).InTag(registry.ItemTagIDBookshelfBooks) && count <= 0
	case KindComposterInput:
		// The container takes one item and then refuses.
		// vanilla: ComposterBlock.InputContainer.canPlaceItemThroughFace
		if s, ok := any(b).(storedBlock); ok && s.Stored(slot) {
			return false
		}
		return isCompostable(*stack)
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
func crafterAccepts[B Block](b B, slot int, stack *protocol.ItemStack) bool {
	return crafterTarget(b, stack) == slot
}

// crafterTarget is the cell a stack goes into, -1 when every enabled cell
// holds a different item.
func crafterTarget[B Block](b B, stack *protocol.ItemStack) int {
	best := -1
	var bestCount int32
	for i := 0; i < b.Len(); i++ {
		if blockSlotDisabled(b, i) {
			continue
		}
		in := b.StackPtr(i)
		if in == nil || in.Count <= 0 {
			return i
		}
		if in.SamePtr(stack) && (best < 0 || in.Count < bestCount) {
			best = i
			bestCount = in.Count
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
func canTakeThroughFace[B Block](b B, stack *protocol.ItemStack, slot int, face int32) bool {
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

// Full is vanilla's isFullContainer over the slots a face reads.
func Full[B Block](b B, face int32) bool {
	if !isWorldly(b.Kind()) {
		if n := b.Len(); n > 0 && n <= 64 {
			allMask := ^uint64(0) >> (64 - n)
			if b.Filled()&allMask != allMask {
				return false
			}
		}
	}
	for _, slot := range slotsForFace(b, face) {
		s := b.StackPtr(slot)
		if s == nil || s.Count <= 0 || s.Count < registry.StackMaxStackSizePtr(s) {
			return false
		}
	}
	return true
}

// Empty tells whether a block holds nothing at all.
func Empty[B Block](b B) bool {
	if b.Len() <= 64 {
		return b.Filled() == 0
	}
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
	intoEmpty = addItemPtr(dest, &stack, face, throughFace)
	return stack, intoEmpty
}

// addItemPtr is AddItem mutating *stack in place.
func addItemPtr[B Block](dest B, stack *protocol.ItemStack, face int32, throughFace bool) (intoEmpty bool) {
	slots := allSlots(dest)
	if throughFace && isWorldly(dest.Kind()) {
		slots = slotsForFace(dest, face)
	}
	wasEmpty := Empty(dest)
	plain := isPlainContainer(dest.Kind())
	changed := false
	for _, slot := range slots {
		if stack.Count <= 0 {
			break
		}
		if tryMoveInItem(dest, stack, slot, plain) {
			changed = true
		}
	}
	return wasEmpty && changed
}

// addOneItem pours a single item of stack into dest's face slots without
// mutating or copying stack unless an empty slot accepts it.
func addOneItem[B Block](dest B, stack *protocol.ItemStack, face int32, throughFace bool) (moved, intoEmpty bool) {
	kind := dest.Kind()
	slots := allSlots(dest)
	if throughFace && isWorldly(kind) {
		slots = slotsForFace(dest, face)
	}
	checkPlacement := !isPlainContainer(kind)
	if kind == KindCrafter {
		target := crafterTarget(dest, stack)
		if target < 0 {
			return false, false
		}
		// Crafters expose the whole grid in slot order on every face.
		slots = slots[target : target+1]
		checkPlacement = false
	}
	wasEmpty := Empty(dest)
	for _, slot := range slots {
		if checkPlacement && !canPlaceThroughFace(dest, stack, slot) {
			continue
		}
		item := dest.StackPtr(slot)
		if item == nil || item.Count <= 0 {
			one := *stack
			one.Count = 1
			dest.Put(slot, one)
			return true, wasEmpty
		}
		// Room and item first: a full or foreign slot is the common refusal and
		// needs neither the stack nor a component comparison.
		if item.ItemID != stack.ItemID {
			continue
		}
		if item.Count >= registry.StackMaxStackSizePtr(item) || !item.SamePtr(stack) {
			continue
		}
		updated := *item
		updated.Count++
		dest.Put(slot, updated)
		return true, wasEmpty
	}
	return false, false
}

// tryMoveInItem is vanilla's tryMoveInItem: one slot's share of a pour. It
// takes what the slot accepts out of *stack and reports whether it moved any.
func tryMoveInItem[B Block](dest B, stack *protocol.ItemStack, slot int, plain bool) bool {
	if !plain && !canPlaceThroughFace(dest, stack, slot) {
		return false
	}
	item := dest.StackPtr(slot)
	if item == nil || item.Count <= 0 {
		dest.Put(slot, *stack)
		*stack = protocol.ItemStack{}
		return true
	}
	// Room and item first: a full or foreign slot is the common refusal and
	// needs neither the stack nor a component comparison.
	if item.ItemID != stack.ItemID {
		return false
	}
	move := min(registry.StackMaxStackSizePtr(item)-item.Count, stack.Count)
	// move > 0 means item.Count is under its maximum, vanilla's canMergeItems
	// capacity check.
	if move <= 0 || !item.SamePtr(stack) {
		return false
	}
	stack.Count -= move
	updated := *item
	updated.Count += move
	dest.Put(slot, updated)
	return true
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
	filled, n := src.Filled(), src.Len()
	if filled == 0 && n <= 64 {
		return false, false
	}
	// A single candidate slot on a plain destination: addOneItem checks every
	// slot's room and components itself, so the Full walk would only repeat it.
	// A gate is asked before dest is tried, so it keeps the preflight.
	single := gate == nil && n <= 64 && bits.OnesCount64(filled) == 1 && isPlainContainer(dest.Kind())
	if !single && Full(dest, face) {
		return false, false
	}
	for i := 0; i < src.Len(); i++ {
		if i < 64 && filled&(1<<i) == 0 {
			continue
		}
		stack := src.StackPtr(i)
		if stack == nil || stack.Count <= 0 {
			continue
		}
		var ok, into bool
		if gate != nil {
			one := *stack
			one.Count = 1
			if one, ok = gate.Check(one); !ok {
				return false, false
			}
			into = addItemPtr(dest, &one, face, true)
			ok = one.Count <= 0
		} else {
			ok, into = addOneItem(dest, stack, face, true)
		}
		if !ok {
			continue
		}
		shrinkSlot(src, i, stack)
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
	if filled == 0 && src.Len() <= 64 {
		return false, false
	}
	for _, slot := range slotsForFace(src, protocol.FaceBottom) {
		if slot < 64 && filled&(1<<slot) == 0 {
			continue
		}
		stack := src.StackPtr(slot)
		if stack == nil || stack.Count <= 0 {
			continue
		}
		if !canTakeThroughFace(src, stack, slot, protocol.FaceBottom) {
			continue
		}
		var ok, into bool
		if gate != nil {
			one := *stack
			one.Count = 1
			if one, ok = gate.Check(one); !ok {
				return false, false
			}
			into = addItemPtr(hopper, &one, 0, false)
			ok = one.Count <= 0
		} else {
			ok, into = addOneItem(hopper, stack, 0, false)
		}
		if !ok {
			continue
		}
		shrinkSlot(src, slot, stack)
		return true, into
	}
	return false, false
}

// shrinkSlot decrements slot's stack by one, clearing it when it runs out.
func shrinkSlot[B Block](b B, slot int, stack *protocol.ItemStack) {
	if stack.Count <= 1 {
		b.Put(slot, protocol.ItemStack{})
		return
	}
	updated := *stack
	updated.Count--
	b.Put(slot, updated)
}
