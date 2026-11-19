package entity

import (
	"maps"
	"math"
	"math/rand"
	"slices"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// Vault states, the values of the block's vault_state.
// vanilla: VaultState
const (
	VaultInactive  = "inactive"
	VaultActive    = "active"
	VaultUnlocking = "unlocking"
	VaultEjecting  = "ejecting"

	// vaultUnlockingDelay is the ticks the unlocking state lasts.
	vaultUnlockingDelay = 14
	// vaultDisplayCycle is the ticks between two display item draws while active.
	vaultDisplayCycle = 20
	// vaultPeriod is the pause after a player scan and after each ejection.
	vaultPeriod = 20
	// vaultInsertFailBuffer is the ticks between two insert-fail sounds.
	vaultInsertFailBuffer = 15
	// vaultMaxRewarded is how many rewarded players a vault remembers.
	vaultMaxRewarded = 128

	defaultVaultLoot         = "minecraft:chests/trial_chambers/reward"
	defaultVaultActivation   = 4.0
	defaultVaultDeactivation = 4.5
)

// Level events a vault broadcasts.
// vanilla: LevelEvent.ANIMATION_VAULT_ACTIVATE, ..._DEACTIVATE, ..._EJECT_ITEM
const (
	VaultEventActivate   = 3015
	VaultEventDeactivate = 3016
	VaultEventEject      = 3017
)

// VaultHost is what a vault's tick and key use need of the server around it.
type VaultHost interface {
	// GameTime is the dimension's game time.
	GameTime() int64
	Rand() *rand.Rand
	// Players are the players of the vault's dimension.
	Players() []TrialPlayer
	// Loot rolls a loot table, nil for an unknown table.
	Loot(table string) []protocol.ItemStack
	// RewardLoot is Loot for the player inserting key: rolled with their
	// luck, their body as THIS and key as the tool.
	RewardLoot(table string, key protocol.ItemStack) []protocol.ItemStack
	// DropItem lays an item entity with a velocity, subject to the usual
	// pickup delay.
	DropItem(stack protocol.ItemStack, x, y, z, vx, vy, vz float64)
	LevelEvent(event, x, y, z, data int32)
	// Sound plays a block-category sound at the centre of the block.
	Sound(sound registry.Sound, x, y, z int32, volume, pitch float32)
	// SetState moves the vault block to a vault_state.
	SetState(state string)
}

// Vault is one vault's configuration and saved data, read from its block
// entity compound.
// vanilla: VaultConfig, VaultServerData, VaultSharedData
type Vault struct {
	lootTable    string
	activation   float64
	deactivation float64
	// key is the key item, empty for a vault that takes none.
	key      protocol.ItemStack
	override string

	rewarded       []uuid.UUID
	resumesAt      int64
	toEject        []protocol.ItemStack
	totalEjections int32

	display        protocol.ItemStack
	connected      []uuid.UUID
	particlesRange float64

	// LastInsertFail is the game time of the last insert-fail sound; the
	// vanilla block entity keeps it in memory only.
	LastInsertFail int64

	// Dirty is set when the saved form changed.
	Dirty bool
	// SharedDirty is set when the update tag the clients hold changed.
	SharedDirty bool
}

// ParseVault reads the config, the server data and the shared data. A config
// whose activation range exceeds its deactivation range reads as the default.
// vanilla: VaultBlockEntity.loadAdditional, VaultConfig.validate
func ParseVault(tag nbt.Compound) *Vault {
	v := &Vault{particlesRange: defaultVaultDeactivation}
	v.setDefaultConfig()
	if cfg, ok := nbt.Get[nbt.Compound](tag, "config"); ok {
		v.readConfig(cfg)
	}
	if data, ok := nbt.Get[nbt.Compound](tag, "server_data"); ok {
		v.rewarded = trialUUIDs(data, "rewarded_players")
		v.resumesAt, _ = nbt.LongValue(data["state_updating_resumes_at"])
		v.toEject = vaultStacks(data, "items_to_eject")
		v.totalEjections, _ = nbt.IntValue(data["total_ejections_needed"])
	}
	if data, ok := nbt.Get[nbt.Compound](tag, "shared_data"); ok {
		v.display = vaultStack(data, "display_item")
		v.connected = trialUUIDs(data, "connected_players")
		if r, ok := nbt.Get[nbt.Double](data, "connected_particles_range"); ok {
			v.particlesRange = float64(r)
		}
	}
	return v
}

func (v *Vault) setDefaultConfig() {
	v.lootTable = defaultVaultLoot
	v.activation, v.deactivation = defaultVaultActivation, defaultVaultDeactivation
	v.key = protocol.ItemStack{ItemID: registry.MustItem("minecraft:trial_key"), Count: 1}
	v.override = ""
}

func (v *Vault) readConfig(cfg nbt.Compound) {
	loot := defaultVaultLoot
	if s, ok := nbt.Get[nbt.String](cfg, "loot_table"); ok {
		loot = string(s)
	}
	activation, deactivation := defaultVaultActivation, defaultVaultDeactivation
	if d, ok := nbt.Get[nbt.Double](cfg, "activation_range"); ok {
		activation = float64(d)
	}
	if d, ok := nbt.Get[nbt.Double](cfg, "deactivation_range"); ok {
		deactivation = float64(d)
	}
	if activation > deactivation {
		return
	}
	v.lootTable, v.activation, v.deactivation = loot, activation, deactivation
	v.key = vaultStack(cfg, "key_item")
	if s, ok := nbt.Get[nbt.String](cfg, "override_loot_table_to_display"); ok {
		v.override = string(s)
	}
}

// vaultStack reads a stack stored under key, empty when absent or unreadable.
func vaultStack(tag nbt.Compound, key string) protocol.ItemStack {
	entry, ok := nbt.Get[nbt.Compound](tag, key)
	if !ok {
		return protocol.ItemStack{}
	}
	stack, ok := registry.StoredStack(entry)
	if !ok {
		return protocol.ItemStack{}
	}
	return stack
}

func vaultStacks(tag nbt.Compound, key string) []protocol.ItemStack {
	list, _ := nbt.Get[nbt.List](tag, key)
	var out []protocol.ItemStack
	for _, item := range list.Items {
		if entry, ok := nbt.CompoundView(item); ok {
			if stack, ok := registry.StoredStack(entry); ok {
				out = append(out, stack)
			}
		}
	}
	return out
}

// Store returns the compound with the server data and the shared data written
// over it. The input is not modified: the world hands the same map to readers.
// vanilla: VaultServerData.CODEC, VaultSharedData.CODEC
func (v *Vault) Store(tag nbt.Compound) nbt.Compound {
	out := make(nbt.Compound, len(tag)+2)
	maps.Copy(out, tag)
	server := nbt.Compound{}
	if len(v.rewarded) > 0 {
		server["rewarded_players"] = trialUUIDList(v.rewarded)
	}
	if v.resumesAt != 0 {
		server["state_updating_resumes_at"] = nbt.Long(v.resumesAt)
	}
	if len(v.toEject) > 0 {
		items := make([]nbt.Tag, len(v.toEject))
		for i, stack := range v.toEject {
			items[i] = registry.StackTag(stack)
		}
		server["items_to_eject"] = nbt.List{Elem: nbt.TypeCompound, Items: items}
	}
	if v.totalEjections != 0 {
		server["total_ejections_needed"] = nbt.Int(v.totalEjections)
	}
	out["server_data"] = server
	shared := vaultSharedTag(v)
	out["shared_data"] = shared
	return out
}

func vaultSharedTag(v *Vault) nbt.Compound {
	shared := nbt.Compound{}
	if !v.display.Empty() {
		shared["display_item"] = registry.StackTag(v.display)
	}
	if len(v.connected) > 0 {
		shared["connected_players"] = trialUUIDList(v.connected)
	}
	if v.particlesRange != defaultVaultDeactivation {
		shared["connected_particles_range"] = nbt.Double(v.particlesRange)
	}
	return shared
}

// VaultIdle reports whether a tick of the vault would change nothing, read off
// the saved compound: between two scans an inactive vault does nothing, and an
// active one only redraws its display item every twenty ticks.
func VaultIdle(tag nbt.Compound, state string, now int64) bool {
	data, _ := nbt.Get[nbt.Compound](tag, "server_data")
	resumes, _ := nbt.LongValue(data["state_updating_resumes_at"])
	if now >= resumes {
		return false
	}
	return state != VaultActive || now%vaultDisplayCycle != 0
}

// Tick runs one tick of the vault and answers the state its block should be
// in afterwards; the host has been told to move there when it changed.
// vanilla: VaultBlockEntity.Server.tick
func (v *Vault) Tick(h VaultHost, x, y, z int32, state string, ominous bool) string {
	now := h.GameTime()
	if now%vaultDisplayCycle == 0 && state == VaultActive {
		v.cycleDisplay(h, state)
	}
	next := state
	if now >= v.resumesAt {
		next = v.tickAndGetNext(h, x, y, z, state)
		if next != state {
			h.SetState(next)
			v.transition(h, x, y, z, state, next, ominous)
		}
	}
	return next
}

// TryInsertKey is the key use on an active vault. It answers how many items
// of the held stack the use spent.
// vanilla: VaultBlockEntity.Server.tryInsertKey
func (v *Vault) TryInsertKey(h VaultHost, x, y, z int32, state string, ominous bool, player uuid.UUID, held protocol.ItemStack) int32 {
	if !v.canEject(state) {
		return 0
	}
	if !held.Same(v.key) || held.Count < v.key.Count {
		v.insertFail(h, x, y, z, registry.SoundBlockVaultInsertItemFail)
		return 0
	}
	if slices.Contains(v.rewarded, player) {
		v.insertFail(h, x, y, z, registry.SoundBlockVaultRejectRewardedPlayer)
		return 0
	}
	items := h.RewardLoot(v.lootTable, held)
	if len(items) == 0 {
		return 0
	}
	v.toEject = slices.Clone(items)
	v.totalEjections = int32(len(items))
	v.Dirty = true
	v.setDisplay(v.nextToEject())
	v.pause(h.GameTime() + vaultUnlockingDelay)
	h.SetState(VaultUnlocking)
	v.transition(h, x, y, z, state, VaultUnlocking, ominous)

	v.rewarded = append(slices.Clone(v.rewarded), player)
	if len(v.rewarded) > vaultMaxRewarded {
		v.rewarded = v.rewarded[1:]
	}
	v.Dirty = true
	v.updateConnected(h, x, y, z, v.deactivation)
	return v.key.Count
}

func (v *Vault) canEject(state string) bool {
	return !v.key.Empty() && state != VaultInactive
}

func (v *Vault) insertFail(h VaultHost, x, y, z int32, sound registry.Sound) {
	if now := h.GameTime(); now >= v.LastInsertFail+vaultInsertFailBuffer {
		h.Sound(sound, x, y, z, 1, 1)
		v.LastInsertFail = now
	}
}

func (v *Vault) pause(until int64) {
	v.resumesAt = until
	v.Dirty = true
}

func (v *Vault) nextToEject() protocol.ItemStack {
	if len(v.toEject) == 0 {
		return protocol.ItemStack{}
	}
	return v.toEject[len(v.toEject)-1]
}

func (v *Vault) setDisplay(stack protocol.ItemStack) {
	if v.display.Empty() && stack.Empty() || v.display.Equal(stack) {
		return
	}
	v.display = stack
	v.SharedDirty = true
}

// cycleDisplay draws the display item from the loot table, a random one of a
// roll's results.
// vanilla: VaultBlockEntity.Server.cycleDisplayItemFromLootTable
func (v *Vault) cycleDisplay(h VaultHost, state string) {
	if !v.canEject(state) {
		v.setDisplay(protocol.ItemStack{})
		return
	}
	table := v.lootTable
	if v.override != "" {
		table = v.override
	}
	results := h.Loot(table)
	if len(results) == 0 {
		v.setDisplay(protocol.ItemStack{})
		return
	}
	v.setDisplay(results[h.Rand().Intn(len(results))])
}

// tickAndGetNext is VaultState.tickAndGetNext.
func (v *Vault) tickAndGetNext(h VaultHost, x, y, z int32, state string) string {
	now := h.GameTime()
	switch state {
	case VaultInactive:
		return v.scan(h, x, y, z, v.activation)
	case VaultActive:
		return v.scan(h, x, y, z, v.deactivation)
	case VaultUnlocking:
		v.pause(now + vaultPeriod)
		return VaultEjecting
	}
	if len(v.toEject) == 0 {
		v.totalEjections = 0
		v.Dirty = true
		return v.scan(h, x, y, z, v.deactivation)
	}
	progress := v.ejectionProgress()
	stack := v.toEject[len(v.toEject)-1]
	v.toEject = v.toEject[:len(v.toEject)-1]
	v.Dirty = true
	px, py, pz, vx, vy, vz := ejectUp(h.Rand(), x, y, z)
	h.DropItem(stack, px, py, pz, vx, vy, vz)
	h.LevelEvent(VaultEventEject, x, y, z, 0)
	h.Sound(registry.SoundBlockVaultEjectItem, x, y, z, 1, 0.8+0.4*progress)
	v.setDisplay(v.nextToEject())
	v.pause(now + vaultPeriod)
	return VaultEjecting
}

// ejectionProgress is how far through its items the ejection is, 1 for the
// last one, which sets the pitch of the sound.
// vanilla: VaultServerData.ejectionProgress
func (v *Vault) ejectionProgress() float32 {
	if v.totalEjections == 1 {
		return 1
	}
	size := float32(len(v.toEject))
	return 1 - (size-1)/(float32(v.totalEjections)-1)
}

// scan is VaultState.updateStateForConnectedPlayers.
func (v *Vault) scan(h VaultHost, x, y, z int32, limit float64) string {
	v.updateConnected(h, x, y, z, limit)
	v.pause(h.GameTime() + vaultPeriod)
	if len(v.connected) > 0 {
		return VaultActive
	}
	return VaultInactive
}

// updateConnected sets the connected players to the players within limit of
// the vault that it has not rewarded.
// vanilla: VaultSharedData.updateConnectedPlayersWithinRange
func (v *Vault) updateConnected(h VaultHost, x, y, z int32, limit float64) {
	var found []uuid.UUID
	for _, p := range h.Players() {
		if p.Mode == protocol.GameModeSpectator || slices.Contains(v.rewarded, p.UUID) {
			continue
		}
		dx := float64(int32(math.Floor(p.X)) - x)
		dy := float64(int32(math.Floor(p.Y)) - y)
		dz := float64(int32(math.Floor(p.Z)) - z)
		if dx*dx+dy*dy+dz*dz >= limit*limit {
			continue
		}
		found = append(found, p.UUID)
	}
	same := len(found) == len(v.connected)
	for _, id := range found {
		same = same && slices.Contains(v.connected, id)
	}
	if !same {
		v.connected = found
		v.SharedDirty = true
	}
}

// transition runs the exit of the state left and the entry of the state
// entered.
// vanilla: VaultState.onTransition
func (v *Vault) transition(h VaultHost, x, y, z int32, from, to string, ominous bool) {
	if from == VaultEjecting {
		h.Sound(registry.SoundBlockVaultCloseShutter, x, y, z, 1, 1)
	}
	data := int32(0)
	if ominous {
		data = 1
	}
	switch to {
	case VaultInactive:
		v.setDisplay(protocol.ItemStack{})
		h.LevelEvent(VaultEventDeactivate, x, y, z, data)
	case VaultActive:
		if v.display.Empty() {
			v.cycleDisplay(h, to)
		}
		h.LevelEvent(VaultEventActivate, x, y, z, data)
	case VaultUnlocking:
		h.Sound(registry.SoundBlockVaultInsertItem, x, y, z, 1, 1)
	case VaultEjecting:
		h.Sound(registry.SoundBlockVaultOpenShutter, x, y, z, 1, 1)
	}
}
