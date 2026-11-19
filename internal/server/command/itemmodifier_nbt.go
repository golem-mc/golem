package command

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func init() { rules.SetCustomDataCopy(copyCustomData) }

// itemModifierContext is the roll context an item modifier runs under, its
// hooks reaching the command's world.
// vanilla: ItemCommands.applyModifier
func itemModifierContext(src Source) *rules.LootContext {
	ctx := &rules.LootContext{
		// Only this, the command's own entity, is in play here; its
		// selectors resolve through the command's source.
		ResolveText: func(entity string, t protocol.Text) protocol.Text {
			if entity != "this" || src.SelfEntity() == nil {
				return t
			}
			return ResolveText(src, t)
		},
	}
	fillLootWorld(ctx, src, src.Position())
	if self := src.SelfEntity(); self != nil {
		ctx.This = lootEntityOf(self)
	}
	return ctx
}

// copyCustomData runs the operations over a copy of the stack's custom
// data; an operation whose path fails is skipped, and the data is set once
// any operation asked for it.
// vanilla: CopyCustomDataFunction.run
func copyCustomData(source nbt.Compound, ops []registry.LootCopyOp, stack *protocol.ItemStack) {
	var target nbt.CompoundTag
	for _, op := range ops {
		from, perr := parseNBTPath(op.Source)
		to, terr := parseNBTPath(op.Target)
		if perr != nil || terr != nil {
			continue
		}
		values, err := from.get(source)
		if err != nil || len(values) == 0 {
			continue
		}
		if target == nil {
			target = &nbt.OrderedCompound{}
			if current := stack.CustomDataTag(); current != nil {
				target = nbt.Copy(current).(nbt.CompoundTag)
			}
		}
		mergeStrategy(op.Op, to, target, values)
	}
	if target != nil {
		stack.SetCustomDataTag(nbt.Copy(target).(nbt.CompoundTag))
	}
}

// mergeStrategy lays the values at the path of the target.
// vanilla: CopyCustomDataFunction.MergeStrategy
func mergeStrategy(op string, path nbtPath, target nbt.CompoundTag, values []nbt.Tag) {
	switch op {
	case "replace":
		_, _ = path.set(target, values[len(values)-1])
	case "append":
		_, _ = path.insert(-1, target, values)
	case "merge":
		places, err := path.placesCreated(target, func() nbt.Tag { return &nbt.OrderedCompound{} })
		if err != nil {
			return
		}
		for _, place := range places {
			if compound, ok := place.get.(nbt.CompoundTag); ok {
				for _, value := range values {
					if from, ok := value.(nbt.CompoundTag); ok {
						mergeCompound(compound, from)
					}
				}
			}
		}
	}
}
