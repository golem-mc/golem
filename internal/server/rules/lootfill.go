package rules

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// FillLoot rolls a container table into the empty slots, leaving the
// container alone for an unknown table (reported false). Stacks beyond the
// free slots are lost. rng serves the roll, the splits and both shuffles.
// vanilla: LootTable.fill
func FillLoot(name string, ctx *LootContext, rng LootRand, slots []protocol.ItemStack) bool {
	return FillLootReviewed(name, ctx, rng, slots, nil)
}

// FillLootReviewed is FillLoot with review seeing the rolled stacks before
// they are split and placed; it returns the stacks to place, none to place
// nothing, the table still counting as rolled.
func FillLootReviewed(name string, ctx *LootContext, rng LootRand, slots []protocol.ItemStack, review func([]protocol.ItemStack) []protocol.ItemStack) bool {
	table, ok := registry.FindLootTable(name)
	if !ok {
		return false
	}
	rng = ctx.lootRandom(table.RandomSequence, rng)
	stacks := rollLootTable(name, table, ctx, rng)
	if review != nil {
		stacks = review(stacks)
	}
	var free []int
	for i := range slots {
		if slots[i].Empty() {
			free = append(free, i)
		}
	}
	shuffle(rng, len(free), func(i, j int) { free[i], free[j] = free[j], free[i] })
	stacks = shuffleAndSplit(stacks, len(free), rng)
	for _, stack := range stacks {
		if len(free) == 0 {
			break
		}
		slots[free[len(free)-1]] = stack
		free = free[:len(free)-1]
	}
	return true
}

// shuffleAndSplit splits the stacks of more than one item at random while
// the container has free slots to spare, then shuffles all of them.
// vanilla: LootTable.shuffleAndSplitItems
func shuffleAndSplit(stacks []protocol.ItemStack, free int, rng LootRand) []protocol.ItemStack {
	var result, splittable []protocol.ItemStack
	for _, s := range stacks {
		switch {
		case s.Empty():
		case s.Count <= 1:
			result = append(result, s)
		default:
			splittable = append(splittable, s)
		}
	}
	for free-len(result)-len(splittable) > 0 && len(splittable) > 0 {
		i := nextIntBetween(rng, 0, len(splittable)-1)
		stack := splittable[i]
		splittable = append(splittable[:i], splittable[i+1:]...)
		take := int32(nextIntBetween(rng, 1, int(stack.Count)/2))
		part := stack
		part.Count = take
		stack.Count -= take
		if stack.Count > 1 && nextBoolean(rng) {
			splittable = append(splittable, stack)
		} else {
			result = append(result, stack)
		}
		if part.Count > 1 && nextBoolean(rng) {
			splittable = append(splittable, part)
		} else {
			result = append(result, part)
		}
	}
	result = append(result, splittable...)
	shuffle(rng, len(result), func(i, j int) { result[i], result[j] = result[j], result[i] })
	return result
}

// nextIntBetween is a uniform integer in [lo, hi], lo when the range is empty.
// vanilla: Mth.nextInt
func nextIntBetween(rng LootRand, lo, hi int) int {
	if lo >= hi {
		return lo
	}
	return lo + rng.Intn(hi-lo+1)
}

// shuffle permutes n items from the end, as vanilla's list shuffle does.
// vanilla: Util.shuffle
func shuffle(rng LootRand, n int, swap func(i, j int)) {
	for i := n; i > 1; i-- {
		swap(i-1, rng.Intn(i))
	}
}
