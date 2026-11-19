package world

import (
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// VaultAt is a carried vault and the state its block is in.
type VaultAt struct {
	X, Y, Z int32
	// State is the block's vault_state.
	State string
	// Ominous is the block's ominous property.
	Ominous bool
	// Tag is the carried compound. It is never modified in place: a change
	// replaces it (see ReplaceCarried).
	Tag nbt.Compound
}

// Vaults lists the vaults the loaded chunks carry whose block is still a
// vault. It walks only the chunks that carry a ticking block entity.
func (w *World) Vaults() []VaultAt {
	w.mu.RLock()
	defer w.mu.RUnlock()
	var out []VaultAt
	for pos := range w.tickingChunks {
		c := w.chunks[pos].chunk
		for _, item := range c.carriedBlockEntities.Items {
			compound, ok := nbt.CompoundView(item)
			if !ok {
				continue
			}
			if id, _ := nbt.Get[nbt.String](compound, "id"); id != "minecraft:vault" {
				continue
			}
			x, y, z, ok := carriedPosInTag(compound)
			if !ok {
				continue
			}
			state := c.BlockAt(x&0xF, y, z&0xF)
			if registry.BlockOf(state) != registry.BlockVault {
				continue
			}
			out = append(out, VaultAt{
				X: x, Y: y, Z: z,
				State:   registry.StateValue(state, registry.PropVaultState),
				Ominous: registry.StateBool(state, registry.PropOminous),
				Tag:     compound,
			})
		}
	}
	return out
}
