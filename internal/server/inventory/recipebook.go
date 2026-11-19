package inventory

import (
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// Recipe-book placement: filling the crafting grid from the player's
// inventory within its stack counts.
// vanilla: ServerPlaceRecipe, RecipeBookMenu.handlePlacement

// Recipe is a craftable recipe in the terms the placement needs: which items
// each of its squares accepts, and how the squares are laid out.
type Recipe struct {
	Shaped bool
	Width  int32
	Height int32

	// Squares are the recipe's squares, each the item IDs that satisfy it:
	// row-major Width*Height for shaped (nil for a gap), one per ingredient
	// for shapeless.
	// vanilla: PlacementInfo.slotsToIngredientIndex with its ingredients
	Squares []registry.Ingredient
}

// PlaceRecipe fills the open window's grid with recipe from the player's
// inventory: one per square, or as many as items and stack sizes allow with
// makeAll. The grid is first handed back, dropping overflow; for a survival
// player whose grid cannot all be handed back nothing happens, so a
// placement never spills it.
//
// It reports false when one craft's items are missing (the caller sends the
// ghost recipe). The caller holds the inventory's lock.
// vanilla: ServerPlaceRecipe.placeRecipe, tryPlaceRecipe
func PlaceRecipe(view View, inv *player.Inventory, recipe Recipe, makeAll, creative bool, outcome *Outcome) bool {
	cells, width, ok := view.gridOf(inv)
	if !ok {
		// A window without a crafting part; the menu kinds a placement
		// runs in are the caller's gate.
		return true
	}
	if !creative && !gridCanBeGivenBack(inv, cells) {
		return true
	}
	tally := newStackTally()
	for slot := range inventoryOrder() {
		tally.account(inv.Slots[slot])
	}
	for _, cell := range cells {
		tally.account(cell)
	}
	if !newRecipePicker(tally, recipe).tryPick(1, nil) {
		clearGrid(view, inv, cells, outcome, nil)
		return false
	}
	fillGrid(view, inv, cells, width, recipe, tally, makeAll, outcome, nil)
	return true
}

// PlaceFurnaceRecipe fills a furnace window's input slot with one of the
// ingredient's items from the inventory, handing the input and the result
// slot back first. It reports false when no item is held (the caller sends
// the ghost recipe). The caller holds the inventory's and the furnace's locks.
// vanilla: AbstractFurnaceMenu.handlePlacement over ServerPlaceRecipe
func PlaceFurnaceRecipe(view View, inv *player.Inventory, ingredient registry.Ingredient, makeAll, creative bool, outcome *Outcome) bool {
	if !view.Kind.Cooking() || view.Entity == nil {
		return true
	}
	cells := []protocol.ItemStack{view.Entity.Slot(furnaceInputSlot)}
	if !creative && !gridCanBeGivenBack(inv, cells) {
		return true
	}
	tally := newStackTally()
	for slot := range inventoryOrder() {
		tally.account(inv.Slots[slot])
	}
	// The furnace's fuel and result count among the available items too.
	for slot := range furnaceSlots {
		tally.account(view.Entity.Slot(slot))
	}
	recipe := Recipe{Shaped: true, Width: 1, Height: 1, Squares: []registry.Ingredient{ingredient}}
	// Only the input slot is a placement cell; the result slot is handed back
	// whenever the input is.
	handBackResult := func() {
		result := view.Entity.Slot(furnaceResultSlot)
		if result.Empty() {
			return
		}
		view.Entity.Put(furnaceResultSlot, protocol.ItemStack{})
		back := &Handback{}
		back.give(inv, result)
		for _, slot := range back.Slots {
			outcome.touchedInv(view, slot)
		}
		outcome.touched(furnaceResultSlot)
		outcome.Dropped = append(outcome.Dropped, back.Dropped...)
		outcome.Visible = outcome.Visible || back.Visible
	}
	placed := true
	if !newRecipePicker(tally, recipe).tryPick(1, nil) {
		clearGrid(view, inv, cells, outcome, handBackResult)
		placed = false
	} else {
		fillGrid(view, inv, cells, 1, recipe, tally, makeAll, outcome, handBackResult)
	}
	view.Entity.Put(furnaceInputSlot, cells[0])
	return placed
}

// The furnace's input slot and slot count; the fuel is slot 1.
const (
	furnaceInputSlot = 0
	furnaceSlots     = 3
)

// fillGrid lays the recipe in the grid. The grid counts among the items, so
// clicking again places one more over a filled grid, unless the most the
// items could craft is fewer than the stacks it holds; then nothing moves.
// vanilla: ServerPlaceRecipe.placeRecipe
func fillGrid(view View, inv *player.Inventory, cells []protocol.ItemStack, gridWidth int32,
	recipe Recipe, tally *stackTally, makeAll bool, outcome *Outcome, alsoClear func()) {
	biggestCraftable := newRecipePicker(tally, recipe).tryPickAll()
	matched := recipeFits(recipe, cells, gridWidth)
	if matched {
		for _, cell := range cells {
			if cell.Empty() {
				continue
			}
			if min(biggestCraftable, stackMax(cell)) < cell.Count+1 {
				return
			}
		}
	}
	amount := int32(1)
	if makeAll {
		amount = biggestCraftable
	} else if matched {
		smallest := int32(0x7FFFFFFF)
		for _, cell := range cells {
			if !cell.Empty() && cell.Count < smallest {
				smallest = cell.Count
			}
		}
		// A grid the recipe matched holds at least one item, so this runs
		// to at least the two of one more.
		amount = smallest + 1
	}
	picker := newRecipePicker(tally, recipe)
	items := picker.tryPickCollect(amount)
	if items == nil {
		return
	}
	// The smallest max stack size among the picked items caps the craft.
	// vanilla: ServerPlaceRecipe.clampToMaxStackSize
	if clamped := min(amount, stackMaxOf(items)); clamped != amount {
		amount = clamped
		if items = picker.tryPickCollect(amount); items == nil {
			return
		}
	}

	gridHeld := false
	for _, cell := range cells {
		if !cell.Empty() {
			gridHeld = true
			break
		}
	}
	clearGrid(view, inv, cells, outcome, alsoClear)
	touched := false
	for _, entry := range placeInGrid(recipe, gridWidth, int32(len(cells))/gridWidth) {
		if entry.demand < 0 || entry.cell >= int32(len(cells)) {
			continue
		}
		moveToGrid(inv, cells, entry.cell, items[entry.demand], amount, view, outcome)
		touched = true
	}
	// The result slot follows whatever the placement left the grid worth;
	// a grid clearGrid emptied already announced it.
	if touched && !gridHeld {
		outcome.touched(0)
	}
}

// clearGrid hands the grid back to the inventory as a close does, dropping
// overflow, and announces the emptied cells and filled slots. It also
// announces the result slot, failing path included: vanilla's
// finishPlacingRecipe runs in a finally.
// alsoClear, when set, hands back slots that are no grid cell (the furnace's
// result).
// vanilla: ServerPlaceRecipe.clearGrid, AbstractCraftingMenu.handlePlacement
func clearGrid(view View, inv *player.Inventory, cells []protocol.ItemStack, outcome *Outcome, alsoClear func()) {
	back := &Handback{}
	cleared := false
	for i, cell := range cells {
		if cell.Empty() {
			continue
		}
		cleared = true
		cells[i] = protocol.ItemStack{}
		back.give(inv, cell)
		outcome.touched(gridSlotOf(view, int32(i)))
	}
	seen := make(map[int]bool)
	for _, slot := range back.Slots {
		if seen[slot] {
			continue
		}
		seen[slot] = true
		outcome.touchedInv(view, slot)
	}
	outcome.Dropped = append(outcome.Dropped, back.Dropped...)
	outcome.Visible = outcome.Visible || back.Visible
	if alsoClear != nil {
		alsoClear()
	}
	if cleared {
		outcome.touched(0)
	}
}

// moveToGrid moves count of item from as many inventory slots as it takes
// into the cell, whole stacks with their components: worn, enchanted or
// renamed stacks are never spent, and an occupied cell is fed only by its
// own stack. Source slots and cell are announced.
// vanilla: ServerPlaceRecipe.moveItemToGrid, Inventory.findSlotMatchingCraftingIngredient
func moveToGrid(inv *player.Inventory, cells []protocol.ItemStack, cell, item, count int32, view View, outcome *Outcome) {
	remaining := count
	target := &cells[cell]
	for remaining > 0 {
		from := -1
		stack := protocol.ItemStack{}
		for slot := range inventoryOrder() {
			candidate := inv.Slots[slot]
			if candidate.ItemID != item || !usableForCrafting(candidate) ||
				(!target.Empty() && !sameStack(*target, candidate)) {
				continue
			}
			from, stack = slot, candidate
			break
		}
		if from < 0 {
			return
		}
		moved := min(remaining, stack.Count)
		if target.Empty() {
			taken := stack
			taken.Count = moved
			*target = taken
		} else {
			target.Count += moved
		}
		stack.Count -= moved
		if stack.Count <= 0 {
			stack = protocol.ItemStack{}
		}
		inv.Set(from, stack)
		remaining -= moved
		outcome.touchedInv(view, from)
	}
	outcome.touched(gridSlotOf(view, cell))
}

// usableForCrafting reports whether placement may count and spend a stack:
// not worn, enchanted or renamed.
// vanilla: Inventory.isUsableForCrafting
func usableForCrafting(stack protocol.ItemStack) bool {
	return stack.Damage == 0 && len(stack.Enchantments) == 0 && stack.CustomName == nil
}

// gridSlotOf is the window slot a crafting grid cell's index addresses.
func gridSlotOf(view View, index int32) int32 {
	if view.Kind.Cooking() {
		return furnaceInputSlot
	}
	if view.Kind == KindPlayer {
		return playerCraftingGrid + index
	}
	return craftingTableGrid + index
}

// stackMaxOf is the smallest stack size among the items.
func stackMaxOf(items []int32) int32 {
	smallest := int32(0x7FFFFFFF)
	for _, item := range items {
		smallest = min(smallest, registry.MaxStackSize(item))
	}
	return smallest
}

// recipeFits reports whether the grid already holds the recipe.
// vanilla: RecipeBookMenu's recipeMatches, through RecipeManager.getRecipeFor
func recipeFits(recipe Recipe, cells []protocol.ItemStack, gridWidth int32) bool {
	if recipe.Shaped {
		return registry.ShapedPatternFits(recipe.Width, recipe.Height, recipe.Squares, cells, gridWidth)
	}
	return registry.ShapelessIngredientsFit(recipe.Squares, cells)
}

// gridCanBeGivenBack simulates handing the grid back: each stack onto a
// partial same-item stack or into an empty main slot, merging as it goes. A
// grid that cannot is not cleared and no placement runs.
// vanilla: ServerPlaceRecipe.testClearGrid
func gridCanBeGivenBack(inv *player.Inventory, cells []protocol.ItemStack) bool {
	free := 0
	for slot := range inventoryOrder() {
		if inv.Slots[slot].Empty() {
			free++
		}
	}
	var laid []protocol.ItemStack
	for _, cell := range cells {
		if cell.Empty() {
			continue
		}
		if slotWithRemainingSpace(inv, cell) >= 0 {
			continue
		}
		rest := cell.Count
		for i := range laid {
			if laid[i].ItemID != cell.ItemID || laid[i].Count >= stackMax(laid[i]) ||
				laid[i].Count+rest > stackMax(laid[i]) {
				continue
			}
			laid[i].Count += rest
			rest = 0
			break
		}
		if rest == 0 {
			continue
		}
		if free == 0 {
			return false
		}
		free--
		laid = append(laid, protocol.ItemStack{ItemID: cell.ItemID, Count: rest})
	}
	return true
}

// slotWithRemainingSpace is the first slot with room for more of the stack -
// the held one, the off hand, then the hotbar and the main rows - or -1 when
// none. vanilla: Inventory.getSlotWithRemainingSpace
func slotWithRemainingSpace(inv *player.Inventory, stack protocol.ItemStack) int {
	slots := []int{player.HotbarSlot + int(inv.Selected), player.OffHandSlot}
	for slot := range inventoryOrder() {
		slots = append(slots, slot)
	}
	for _, slot := range slots {
		if into := inv.Slots[slot]; !into.Empty() && sameStack(into, stack) && into.Count < stackMax(into) {
			return slot
		}
	}
	return -1
}

// stackTally is the per-item counts of the inventory plus the grid, in read
// order, which is the order the pick tries them.
// vanilla: StackedItemContents over StackedContents
type stackTally struct {
	counts map[int32]int32
	order  []int32
}

func newStackTally() *stackTally {
	return &stackTally{counts: make(map[int32]int32)}
}

// account adds a stack to the counts, skipping empty, worn, enchanted and
// renamed ones.
// vanilla: StackedItemContents.accountSimpleStack
func (t *stackTally) account(stack protocol.ItemStack) {
	if stack.Empty() || !usableForCrafting(stack) {
		return
	}
	if t.counts[stack.ItemID] == 0 {
		t.order = append(t.order, stack.ItemID)
	}
	t.counts[stack.ItemID] += stack.Count
}

// recipePicker assigns each square an accepted item from the tally, an item
// covering as many squares as its count; a square whose item cannot be
// spared is re-routed by an alternating item/square path search.
// vanilla: StackedContents.RecipePicker
type recipePicker struct {
	tally   *stackTally
	demands []registry.Ingredient
	items   []int32
}

// newRecipePicker counts the squares the recipe fills, one option list a
// square, and the tally's items any of them accepts.
func newRecipePicker(tally *stackTally, recipe Recipe) *recipePicker {
	p := &recipePicker{tally: tally, demands: demandsOf(recipe)}
	for _, id := range tally.order {
		if tally.counts[id] <= 0 {
			continue
		}
		for _, demand := range p.demands {
			if slices.Contains(demand, id) {
				p.items = append(p.items, id)
				break
			}
		}
	}
	return p
}

// demandsOf lists the recipe's squares in order, the ones it leaves out
// skipped.
func demandsOf(recipe Recipe) []registry.Ingredient {
	var demands []registry.Ingredient
	for _, square := range recipe.Squares {
		if square != nil {
			demands = append(demands, square)
		}
	}
	return demands
}

// tryPick gives every square amount of an accepted item within the tally,
// reporting each square's item to out in order, only on success. The counts
// are restored on return, even after a partial pick, so the next pick sees
// the same tally.
// vanilla: StackedContents.RecipePicker.tryPick
func (p *recipePicker) tryPick(amount int32, out func(item int32)) bool {
	if amount <= 0 {
		return true
	}
	assigned := make([][]bool, len(p.items))
	for i := range assigned {
		assigned[i] = make([]bool, len(p.demands))
	}
	satisfied := make([]bool, len(p.demands))
	given := 0
	for given < len(p.demands) {
		path, ok := p.assignPath(amount, assigned, satisfied)
		if !ok {
			break
		}
		p.tally.counts[p.items[path[0]]] -= amount
		satisfied[path[len(path)-1]] = true
		given++
		// Apply the path: even steps give an item to the next square, odd steps
		// take it back from the previous, freeing a square's item for another.
		for step := 0; step+1 < len(path); step++ {
			if step%2 == 0 {
				assigned[path[step]][path[step+1]] = true
			} else {
				assigned[path[step+1]][path[step]] = false
			}
		}
	}
	full := given == len(p.demands)
	for demand := range p.demands {
		for i := range p.items {
			if !assigned[i][demand] {
				continue
			}
			assigned[i][demand] = false
			p.tally.counts[p.items[i]] += amount
			if full && out != nil {
				out(p.items[i])
			}
			break
		}
	}
	return full
}

// tryPickCollect is tryPick gathering the item each square was given, nil
// when the amount cannot be covered.
func (p *recipePicker) tryPickCollect(amount int32) []int32 {
	if amount <= 0 {
		// The one-a-square craft no recipe asks for at zero; vanilla's
		// search answers nothing rather than none.
		return nil
	}
	var items []int32
	if !p.tryPick(amount, func(item int32) { items = append(items, item) }) {
		return nil
	}
	return items
}

// tryPickAll is the largest amount every square can get at once, by the
// tally's upper bound and a halving search.
// vanilla: StackedContents.RecipePicker.tryPickAll, getResultUpperBound
func (p *recipePicker) tryPickAll() int32 {
	upper := int32(0x7FFFFFFF)
	for _, demand := range p.demands {
		best := int32(0)
		for _, item := range p.items {
			if slices.Contains(demand, item) && p.tally.counts[item] > best {
				best = p.tally.counts[item]
			}
		}
		upper = min(upper, best)
	}
	low, high := int32(0), upper+1
	for high-low > 1 {
		if p.tryPick((low+high)/2, nil) {
			low = (low + high) / 2
		} else {
			high = (low + high) / 2
		}
	}
	return low
}

// assignPath finds a re-routing that satisfies one more square: from each
// tally item with amount available, alternate item and square until an
// unassigned square is reached. Visited state is shared across starts,
// as vanilla's search shares its own.
// vanilla: StackedContents.RecipePicker.tryAssigningNewItem
func (p *recipePicker) assignPath(amount int32, assigned [][]bool, satisfied []bool) ([]int32, bool) {
	visitedItem := make([]bool, len(p.items))
	visitedDemand := make([]bool, len(p.demands))
	for start := range p.items {
		if p.tally.counts[p.items[start]] < amount {
			continue
		}
		visitedItem[start] = true
		path := []int32{int32(start)}
		for len(path) > 0 {
			step := len(path)
			switch step % 2 {
			case 1: // an item's turn: to the first square it fits
				item := path[step-1]
				for demand := range p.demands {
					if visitedDemand[demand] || assigned[item][demand] ||
						!slices.Contains(p.demands[demand], p.items[item]) {
						continue
					}
					visitedDemand[demand] = true
					path = append(path, int32(demand))
					break
				}
			default: // a square's turn: through the item it was given
				demand := path[step-1]
				if !satisfied[demand] {
					return path, true
				}
				for item := range p.items {
					if visitedItem[item] || !assigned[item][demand] {
						continue
					}
					visitedItem[item] = true
					path = append(path, int32(item))
					break
				}
			}
			if len(path) == step {
				path = path[:step-1] // a dead end; back up one
			}
		}
	}
	return nil, false
}

// gridPlacement is one recipe square in the grid: its ingredient index (-1
// for a gap) and grid cell.
type gridPlacement struct {
	demand int32
	cell   int32
}

// placeInGrid lays the squares over the grid as the client's book draws
// them: shaped at the top left, centred in a dimension it fills less than
// half of; shapeless one per cell from the top left. Squares past the grid
// never land.
// vanilla: PlaceRecipeHelper.placeRecipe
func placeInGrid(recipe Recipe, gridWidth, gridHeight int32) []gridPlacement {
	recipeWidth, recipeHeight := gridWidth, gridHeight
	if recipe.Shaped {
		recipeWidth, recipeHeight = recipe.Width, recipe.Height
	}
	// Each square's ingredient index, gaps excluded, counted from the
	// first (vanilla: slotsToIngredientIndex).
	demand := make([]int32, len(recipe.Squares))
	next := int32(-1)
	for i := range demand {
		if recipe.Squares[i] != nil {
			next++
		}
		demand[i] = next
	}

	var out []gridPlacement
	square := 0
	gridIndex := int32(0)
	for y := int32(0); y < gridHeight; y++ {
		// A recipe less than half the grid tall starts a row in, the rows
		// above given a skip.
		if 2*recipeHeight < gridHeight && (gridHeight-recipeHeight)/2 > y {
			gridIndex += gridWidth
			y++
		}
		for x := range gridWidth {
			if square == len(recipe.Squares) {
				return out
			}
			centre := 2*recipeWidth < gridWidth
			start := (gridWidth - recipeWidth) / 2
			add := x < recipeWidth
			width := recipeWidth
			if centre {
				width = start + recipeWidth
				add = start <= x && x < start+recipeWidth
			}
			if add {
				out = append(out, gridPlacement{demand: demand[square], cell: gridIndex})
				square++
			} else if width == x {
				// The recipe's squares for this row are placed; the rest
				// of the row is not walked.
				gridIndex += gridWidth - x
				break
			}
			gridIndex++
		}
	}
	return out
}
