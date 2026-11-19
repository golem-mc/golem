package server

import (
	"math/rand"
	"sync"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// vaultBook serialises the tick's and key uses' read-modify-write of vault
// compounds, and holds the insert-fail sound times vanilla keeps only in
// memory.
type vaultBook struct {
	mu   sync.Mutex
	fail map[vaultKey]int64
}

type vaultKey struct {
	w       *world.World
	x, y, z int32
}

// vaultHost is entity.VaultHost for one vault.
type vaultHost struct {
	base    *trialHost
	x, y, z int32
	// opener is the player using a key on the vault, read before the vault
	// lock is taken; the zero value on its tick.
	opener lootOpener
}

func (h *vaultHost) GameTime() int64 { return h.base.GameTime() }

func (h *vaultHost) Rand() *rand.Rand { return h.base.Rand() }

func (h *vaultHost) Players() []entity.TrialPlayer { return h.base.Players() }

// vanilla: VaultBlockEntity.Server.cycleDisplayItemFromLootTable
func (h *vaultHost) Loot(table string) []protocol.ItemStack {
	stacks, _ := h.base.s.rollLoot(h.base.dim.w, h.x, h.y, h.z, table, 0, lootOpener{}, protocol.ItemStack{})
	return stacks
}

// vanilla: VaultBlockEntity.Server.resolveItemsToEject
func (h *vaultHost) RewardLoot(table string, key protocol.ItemStack) []protocol.ItemStack {
	stacks, _ := h.base.s.rollLoot(h.base.dim.w, h.x, h.y, h.z, table, 0, h.opener, key)
	return stacks
}

func (h *vaultHost) DropItem(stack protocol.ItemStack, x, y, z, vx, vy, vz float64) {
	h.base.DropItem(stack, x, y, z, vx, vy, vz)
}

func (h *vaultHost) LevelEvent(event, x, y, z, data int32) { h.base.LevelEvent(event, x, y, z, data) }

func (h *vaultHost) Sound(sound registry.Sound, x, y, z int32, volume, pitch float32) {
	h.base.s.broadcastSoundID(h.base.dim.w, sound, protocol.SoundCategoryBlock,
		float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, volume, pitch)
}

// SetState moves the block to the vault_state and tells the clients.
// vanilla: VaultBlockEntity.Server.setVaultState
func (h *vaultHost) SetState(next string) {
	w := h.base.dim.w
	state := w.BlockAt(h.x, h.y, h.z)
	if registry.BlockOf(state) != registry.BlockVault {
		return
	}
	if next, ok := registry.WithProp(state, registry.PropVaultState, next); ok {
		h.base.s.setBlockAndTell(w, h.x, h.y, h.z, next)
	}
}

// tickVaults ticks the vaults of a dimension.
// vanilla: VaultBlockEntity.Server.tick
func (s *Server) tickVaults(dim *dimension, host *trialHost, now int64, ticks func(world.ChunkPos) bool) {
	vaults := dim.w.Vaults()
	if len(vaults) == 0 {
		return
	}
	s.vaults.mu.Lock()
	defer s.vaults.mu.Unlock()
	for _, at := range vaults {
		if ticks != nil && !ticks(blockChunk(at.X, at.Z)) {
			continue
		}
		if entity.VaultIdle(at.Tag, at.State, now) {
			continue
		}
		v := entity.ParseVault(at.Tag)
		key := vaultKey{dim.w, at.X, at.Y, at.Z}
		v.LastInsertFail = s.vaults.fail[key]
		v.Tick(&vaultHost{base: host, x: at.X, y: at.Y, z: at.Z}, at.X, at.Y, at.Z, at.State, at.Ominous)
		s.saveVault(dim.w, key, v, at.Tag)
	}
}

// saveVault writes a vault's changed data back, sending its update tag
// when the shared data changed.
func (s *Server) saveVault(w *world.World, key vaultKey, v *entity.Vault, tag nbt.Compound) {
	if v.LastInsertFail != 0 {
		if s.vaults.fail == nil {
			s.vaults.fail = make(map[vaultKey]int64)
		}
		s.vaults.fail[key] = v.LastInsertFail
	}
	if !v.Dirty && !v.SharedDirty {
		return
	}
	stored := v.Store(tag)
	if !w.ReplaceCarried(key.x, key.y, key.z, stored) {
		w.SetBlockEntityTag(key.x, key.y, key.z, "minecraft:vault", stored)
	}
	if v.SharedDirty {
		data := world.CarriedUpdateTag("minecraft:vault", stored, false)
		s.broadcastBlockChunkExcept(w, nil, key.x, key.z, protocol.IDPlayBlockEntityData,
			blockEntityData(key.x, key.y, key.z, protocol.BlockEntityVault, data))
	}
}

// useVault uses a held stack on an active vault: it takes the key or
// refuses, and the click is spent either way. vanilla: VaultBlock.useItemOn
func (c *client) useVault(hand, x, y, z int32) bool {
	w := c.theWorld()
	held := c.heldItem(hand)
	if held.Empty() {
		return false
	}
	dim := c.dimension()
	age, _ := c.srv.world.Time()
	host := &trialHost{s: c.srv, dim: dim, now: age}
	vh := &vaultHost{base: host, x: x, y: y, z: z, opener: c.lootOpener()}

	// Read state and tag under the lock so two simultaneous key uses cannot
	// both see ACTIVE and overwrite each other's data.
	c.srv.vaults.mu.Lock()
	state := w.BlockAt(x, y, z)
	if registry.BlockOf(state) != registry.BlockVault {
		c.srv.vaults.mu.Unlock()
		return false
	}
	vaultState := registry.StateValue(state, registry.PropVaultState)
	if vaultState != entity.VaultActive {
		c.srv.vaults.mu.Unlock()
		return false
	}
	tag, _ := w.BlockEntityTag(x, y, z)
	v := entity.ParseVault(tag)
	key := vaultKey{w, x, y, z}
	v.LastInsertFail = c.srv.vaults.fail[key]
	spent := v.TryInsertKey(vh, x, y, z, vaultState, registry.StateBool(state, registry.PropOminous), c.player.UUID, held)
	c.srv.saveVault(w, key, v, tag)
	c.srv.vaults.mu.Unlock()

	if spent > 0 {
		c.awardItemStat(registry.StatTypeUsed, held.ItemID, 1)
		for range spent {
			c.spendHeld(hand)
		}
	}
	c.useSwing = swingServer
	return true
}
