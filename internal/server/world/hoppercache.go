package world

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// ContainerStamp identifies a loaded chunk and its latest block or container
// replacement. Including the chunk identity distinguishes unload followed by reload.
type ContainerStamp struct {
	chunk *loadedChunk
	gen   uint64
}

// ContainerStamp reads the change stamp without loading a missing chunk.
func (w *World) ContainerStamp(x, z int32) ContainerStamp {
	pos := ChunkPos{X: x >> 4, Z: z >> 4}
	l := w.nearAt(pos)
	if l == nil {
		w.mu.RLock()
		l = w.chunks[pos]
		w.mu.RUnlock()
	}
	if l == nil {
		return ContainerStamp{}
	}
	return ContainerStamp{chunk: l, gen: l.containerGen.Load()}
}

// InventoryVersion changes on slot writes and bulk loads, including writes
// before the caller marks the container's chunk dirty.
func (b *BlockEntity) InventoryVersion() uint64 { return b.inventoryVersion.Load() }

// HopperTargets caches a hopper's push and pull lookups; it is never saved.
type HopperTargets struct {
	Push, Pull HopperTargetCache
	// FullKnown, FullVersion and Full cache the hopper's own slots-full result per inventory version.
	FullKnown   bool
	FullVersion uint64
	Full        bool
}

// HopperTargetCache holds block-container identities, mouth obstruction and
// failed-transfer versions. A missing block container still searches the entity
// column on each attempt, preserving random vehicle selection and movement between ticks.
type HopperTargetCache struct {
	Known        bool
	Tags         uint64
	Position     [3]int32
	Stamp        ContainerStamp
	Partner      [2]int32
	PartnerStamp ContainerStamp
	Paired       bool
	Entities     [2]*BlockEntity
	Composter    bool
	MouthBlocked bool
	Failed       bool
	SelfVersion  uint64
	Versions     [2]uint64
	// VehicleSlots identifies the selected vehicle inventory, including NBT replacements.
	VehicleSlots *protocol.ItemStack
}

// HopperFull is vanilla's inventoryFull after ejectItems, reusing the last
// answer until the inventory version moves. The caller holds b's lock and has
// set HopperTargets.
func (b *BlockEntity) HopperFull() bool {
	if b.FilledSlots() != 0x1f {
		return false
	}
	c := b.HopperTargets
	if v := b.InventoryVersion(); !c.FullKnown || c.FullVersion != v {
		c.FullKnown, c.FullVersion, c.Full = true, v, slotsFull(b.Slots)
	}
	return c.Full
}

// vanilla: HopperBlockEntity.inventoryFull
func slotsFull(slots []protocol.ItemStack) bool {
	for i := range slots {
		if slots[i].Count <= 0 || slots[i].Count != registry.StackMaxStackSizePtr(&slots[i]) {
			return false
		}
	}
	return true
}
