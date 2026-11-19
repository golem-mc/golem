package inventory

import (
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
)

// The paint drag: marks accumulate in the session and the stack moves only
// when it ends. The wire shape is internal/server/containers.go's.

// The drag types, the quickcraft mask's bits 2-3. vanilla:
// AbstractContainerMenu.getQuickcraftType
const (
	dragSplit  = 0 // left button: an even share of the cursor
	dragSingle = 1 // right button: one each
	dragFill   = 2 // middle button, creative only: a full stack each
)

// BeginDrag starts a drag with the mask's button type (0, 4 or 8); nothing
// on the cursor, or middle button outside creative, starts none.
// vanilla: AbstractContainerMenu.isValidQuickcraftType
func (s *Session) BeginDrag(button int32, creative bool) {
	if s.Carried.Empty() {
		return
	}
	kind := button >> 2 & 3
	if kind == dragFill && !creative {
		return
	}
	s.dragging = true
	s.dragType = kind
	s.dragSlots = nil
}

// MarkDrag adds a slot once, keeping marks within the cursor's count so each
// share is at least one.
// vanilla: AbstractContainerMenu.doClick QUICK_CRAFT add step.
func (s *Session) MarkDrag(view View, inv *player.Inventory, slot int32) {
	if !s.dragging || slot < 0 || slot >= view.Slots() {
		return
	}
	if slices.Contains(s.dragSlots, slot) {
		return
	}
	held := s.Carried
	inSlot := view.Stack(inv, slot)
	takes := inSlot.Empty() || sameStack(inSlot, held)
	if !takes || !view.placeable(slot, held) {
		return
	}
	if s.dragType != dragFill && held.Count <= int32(len(s.dragSlots)) {
		return
	}
	s.dragSlots = append(s.dragSlots, slot)
}

// CancelDrag ends the drag without dividing anything out, what any
// click that is not part of the paint does - the vanilla server's rule.
func (s *Session) CancelDrag() {
	s.dragging = false
}

// EndDrag ends the drag: one marked slot runs as a plain pickup, more split
// the cursor's stack. It reports false when no drag was in progress.
// vanilla: AbstractContainerMenu.doClick QUICK_CRAFT end step.
func (s *Session) EndDrag(view View, inv *player.Inventory, creative bool, xpLevel int32, outcome *Outcome) bool {
	if !s.dragging {
		return false
	}
	if len(s.dragSlots) == 1 {
		s.dragging = false
		// A one-slot drag is a pickup whose button is the drag type (0 left, 1
		// right); the fill type's 2 is no pickup button, so nothing happens.
		if s.dragType != dragFill {
			clickPickup(s, view, inv, s.dragType, s.dragSlots[0], creative, xpLevel, outcome)
		}
		return true
	}
	s.finishDrag(view, inv, outcome)
	s.dragging = false
	return true
}

// DragPlan is what ending the drag would write: each marked slot's new stack
// in mark order, and what stays held. Nil when no multi-slot drag is in
// progress.
type DragPlan struct {
	Slots  []int32
	Stacks []protocol.ItemStack
	Held   protocol.ItemStack
	Right  bool
}

// PlanDrag computes the multi-slot drag's result without applying it.
func (s *Session) PlanDrag(view View, inv *player.Inventory) *DragPlan {
	if !s.dragging || len(s.dragSlots) < 2 || s.Carried.Empty() {
		return nil
	}
	held := s.Carried
	marks := int32(len(s.dragSlots))
	carried := held.Count
	share := int32(1)
	switch s.dragType {
	case dragSplit:
		share = carried / marks
	case dragFill:
		share = stackMax(held)
	}
	plan := &DragPlan{Right: s.dragType == dragSingle}
	for _, slot := range s.dragSlots {
		inSlot := view.Stack(inv, slot)
		// A mark can turn ineligible before the end (another viewer, a hopper),
		// so it is rechecked; the count is the original, not the shrinking
		// remainder.
		if (!inSlot.Empty() && !sameStack(inSlot, held)) ||
			!view.placeable(slot, held) || (s.dragType != dragFill && carried < marks) {
			continue
		}
		n := min(share+inSlot.Count, view.slotLimit(slot, held)) - inSlot.Count
		if n <= 0 {
			continue
		}
		split := held
		split.Count = inSlot.Count + n
		plan.Slots = append(plan.Slots, slot)
		plan.Stacks = append(plan.Stacks, split)
		held.Count -= n
	}
	// The remainder is the carried stack with its count cut down, so the
	// components ride with it.
	plan.Held = held
	if held.Count <= 0 {
		plan.Held = protocol.ItemStack{}
	}
	return plan
}

// finishDrag splits the held stack over the marks: left gives each the
// floor of the share, right one each, the rest staying held. Fill gives each
// a full stack, counting past the cursor's size, so the cursor ends empty.
// vanilla: AbstractContainerMenu.doClick QUICK_CRAFT end step.
func (s *Session) finishDrag(view View, inv *player.Inventory, outcome *Outcome) {
	plan := s.PlanDrag(view, inv)
	if plan == nil {
		return
	}
	if s.DragEdit != nil {
		plan, s.DragEdit = s.DragEdit, nil
	}
	for i, slot := range plan.Slots {
		setStack(view, inv, slot, plan.Stacks[i], outcome)
	}
	s.Carried = plan.Held
}
