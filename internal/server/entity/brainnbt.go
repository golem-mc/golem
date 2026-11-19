package entity

import "github.com/golem-mc/golem/nbt"

// vanilla: Brain.Packed.CODEC
func brainTag(memories *memoryMap) *nbt.OrderedCompound {
	tag := nbt.NewOrderedCompound(1)
	tag.Set("memories", memories.encode())
	return tag
}

// vanilla: ExpirableValue.codec
func memoryTag(value nbt.Tag, ttl ...int64) *nbt.OrderedCompound {
	tag := nbt.NewOrderedCompound(2)
	tag.Set("value", value)
	if len(ttl) > 0 {
		tag.Set("ttl", nbt.Long(ttl[0]))
	}
	return tag
}

// vanilla: GlobalPos.MAP_CODEC
func globalPosTag(g GlobalPos) *nbt.OrderedCompound {
	tag := nbt.NewOrderedCompound(2)
	tag.Set("dimension", nbt.String(g.Dim))
	tag.Set("pos", nbt.IntArray{g.X, g.Y, g.Z})
	return tag
}
