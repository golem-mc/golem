package entity

import (
	"maps"
	"math"
	"math/rand"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// Trial spawner states, the values of the block's trial_spawner_state.
// vanilla: TrialSpawnerState
const (
	TrialInactive               = "inactive"
	TrialWaitingForPlayers      = "waiting_for_players"
	TrialActive                 = "active"
	TrialWaitingForRewardEject  = "waiting_for_reward_ejection"
	TrialEjectingReward         = "ejecting_reward"
	TrialCooldown               = "cooldown"
	trialScanPeriod             = 20
	trialDetectSpawnBuffer      = 40
	trialMaxTrackingDistanceSqr = 47 * 47
	// trialShutterDelay is the ticks after the cooldown began before the
	// shutter opens.
	trialShutterDelay = 40
	// trialEjectionPeriod is the ticks between two ejections.
	trialEjectionPeriod = 30
	// trialEjectSpread is vanilla's 0.0172275 times the accuracy 2 of an
	// ejected item.
	trialEjectSpread = 0.0172275 * 2
)

// Level events a trial spawner broadcasts.
// vanilla: LevelEvent.PARTICLES_TRIAL_SPAWNER_SPAWN, ..._SPAWN_MOB_AT,
// ..._DETECT_PLAYER
const (
	TrialEventSpawn        = 3011
	TrialEventSpawnMobAt   = 3012
	TrialEventDetectPlayer = 3013
	TrialEventEjectItem    = 3014
	// vanilla: LevelEvent.PARTICLES_TRIAL_SPAWNER_DETECT_PLAYER_OMINOUS,
	// ..._BECOME_OMINOUS, ..._SPAWN_ITEM
	TrialEventDetectOminous = 3019
	TrialEventBecomeOminous = 3020
	TrialEventSpawnItem     = 3021
)

// trialConfig is the part of TrialSpawnerConfig the normal mode reads. The
// defaults are TrialSpawnerConfig.Builder's.
type trialConfig struct {
	spawnRange            int
	totalMobs             float32
	simultaneousMobs      float32
	totalPerPlayer        float32
	simultaneousPerPlayer float32
	ticksBetweenSpawn     int
	potentials            []trialPotential
	lootTables            []trialLoot
	// itemsToDrop is items_to_drop_when_ominous, the loot table the ominous
	// item spawners draw from.
	itemsToDrop string
}

// defaultTrialConfig is TrialSpawnerConfig.DEFAULT.
func defaultTrialConfig() trialConfig {
	return trialConfig{
		spawnRange: 4, totalMobs: 6, simultaneousMobs: 2,
		totalPerPlayer: 2, simultaneousPerPlayer: 1, ticksBetweenSpawn: 40,
		lootTables: []trialLoot{
			{1, "minecraft:spawners/trial_chamber/consumables"},
			{1, "minecraft:spawners/trial_chamber/key"},
		},
		itemsToDrop: "minecraft:spawners/trial_chamber/items_to_drop_when_ominous",
	}
}

// trialLoot is one loot table of loot_tables_to_eject.
type trialLoot struct {
	weight int
	table  string
}

type trialPotential struct {
	weight int
	data   nbt.Compound
}

// TrialSpawner is one trial spawner's saved data and both configurations,
// read from its block entity compound.
// vanilla: TrialSpawnerStateData, TrialSpawner.FullConfig
type TrialSpawner struct {
	normal, ominousCfg trialConfig
	cooldownLength     int
	requiredRange      int

	players          []uuid.UUID
	mobs             []uuid.UUID
	cooldownEndsAt   int64
	nextMobSpawnsAt  int64
	totalMobsSpawned int
	// nextSpawnData is the saved spawn_data, an empty compound for vanilla's
	// SpawnData(), nil when absent.
	nextSpawnData nbt.Compound
	// ejectingLoot is the loot table the running ejection draws from, kept
	// across the ejections of one reward.
	ejectingLoot string

	// Ominous is the block's ominous property; a tick that turns the spawner
	// ominous, or ends it, changes it for the caller to store in the block.
	// vanilla: TrialSpawner.isOminous
	Ominous bool
	// Updated is set when the update tag the clients hold changed.
	Updated bool
	// Dirty is set when the saved form changed.
	Dirty bool
}

// TrialHost is what a trial spawner's tick needs of the server around it.
type TrialHost interface {
	// GameTime is the dimension's game time.
	GameTime() int64
	Rand() *rand.Rand
	// CanSpawn is TrialSpawner.canSpawnInLevel.
	CanSpawn() bool
	// Players are the players of the spawner's dimension.
	Players() []TrialPlayer
	// Mob looks a spawned mob up by UUID.
	Mob(id uuid.UUID) (TrialMob, bool)
	// World is the dimension the spawner stands in.
	World() SpawnWorld
	// LineClear reports whether nothing blocks the line from the point to
	// the centre of the spawner block.
	LineClear(x, y, z int32, px, py, pz float64) bool
	// Bodies are the boxes that obstruct a spawn.
	Bodies() []SpawnBody
	// StandUp stands the mob up, reporting its UUID. equip, when set, dresses
	// the body before it is added.
	StandUp(t MobType, x, y, z float64, yaw float32, tag nbt.Compound, plain bool, equip func(*Mob)) (uuid.UUID, bool)
	LevelEvent(event, x, y, z, data int32)
	// Loot rolls a loot table with no context, nil for an unknown table.
	Loot(table string) []protocol.ItemStack
	// DropItem lays an item entity with a velocity, subject to the usual
	// pickup delay.
	DropItem(stack protocol.ItemStack, x, y, z, vx, vy, vz float64)
	// Sound plays a block-category sound at the spawner.
	Sound(sound registry.Sound, x, y, z int32)

	// BadOmenToTrialOmen swaps the player's bad omen for the trial omen it
	// turns into.
	// vanilla: TrialSpawnerStateData.transformBadOmenIntoTrialOmen
	BadOmenToTrialOmen(id uuid.UUID)
	// Discard removes a spawned mob without a death, dropping the equipment
	// it keeps, and reports where it stood.
	// vanilla: Mob.dropPreservedEquipment, Entity.remove(DISCARDED)
	Discard(id uuid.UUID) (x, y, z float64, ok bool)
	// OminousItems rolls a loot table with no context under a seed that
	// stands for the low-resolution position of the spawner.
	// vanilla: TrialSpawnerStateData.getDispensingItems
	OminousItems(table string, x, y, z int32) []protocol.ItemStack
	// ClipUp is the cell the line from the point straight up by dy ends in:
	// the block it hits, else the one holding its end.
	ClipUp(x, y, z, dy float64) (cx, cy, cz int32)
	// CollisionEmpty reports whether the block at the cell has no collision
	// shape.
	CollisionEmpty(x, y, z int32) bool
	// SpawnItemSpawner stands an ominous item spawner holding the item
	// at the position, which drops it after the ticks given.
	SpawnItemSpawner(stack protocol.ItemStack, x, y, z float64, afterTicks int)
	// SoundAt plays a block-category sound at a point.
	SoundAt(sound registry.Sound, x, y, z float64, volume, pitch float32)
}

// TrialPlayer is a player as a trial spawner's detector sees one.
type TrialPlayer struct {
	UUID uuid.UUID
	Mode int32
	// X, Y, Z is the feet position; Eye is the eye height above it and
	// Height the box's.
	X, Y, Z, Eye, Height float64
	Alive                bool
	// TrialOmen and BadOmen are the effects the player has.
	TrialOmen, BadOmen bool
}

// TrialMob is a spawned mob as the spawner tracks it.
type TrialMob struct {
	X, Y, Z float64
	// Height is the box's.
	Height float64
	Alive  bool
}

// trialTracked reports whether the mob still exists, lives, and stands within
// 47 blocks of the spawner.
// vanilla: TrialSpawner.shouldMobBeUntracked
func trialTracked(h TrialHost, id uuid.UUID, x, y, z int32) bool {
	m, ok := h.Mob(id)
	if !ok || !m.Alive {
		return false
	}
	dx := float64(int32(math.Floor(m.X)) - x)
	dy := float64(int32(math.Floor(m.Y)) - y)
	dz := float64(int32(math.Floor(m.Z)) - z)
	return dx*dx+dy*dy+dz*dz <= trialMaxTrackingDistanceSqr
}

// trialDetect lists the survival and adventure players within the range of
// the block position, in line of sight when asked.
// vanilla: PlayerDetector.NO_CREATIVE_PLAYERS
func trialDetect(h TrialHost, x, y, z int32, requiredRange int, lineOfSight bool) []uuid.UUID {
	var found []uuid.UUID
	limit := float64(requiredRange) * float64(requiredRange)
	for _, p := range h.Players() {
		if p.Mode == protocol.GameModeCreative || p.Mode == protocol.GameModeSpectator {
			continue
		}
		dx := float64(int32(math.Floor(p.X)) - x)
		dy := float64(int32(math.Floor(p.Y)) - y)
		dz := float64(int32(math.Floor(p.Z)) - z)
		if dx*dx+dy*dy+dz*dz >= limit {
			continue
		}
		if lineOfSight && !h.LineClear(x, y, z, p.X, p.Y+p.Eye, p.Z) {
			continue
		}
		found = append(found, p.UUID)
	}
	return found
}

// TrialIdle reports whether a tick of the spawner would change nothing, read
// off the saved compound without parsing it: states that wait on the player
// scan or the cooldown cost nothing on the other ticks.
func TrialIdle(tag nbt.Compound, state string, ominous bool, x, y, z int32, now int64, canSpawn bool) bool {
	if mobs, ok := nbt.Get[nbt.List](tag, "current_mobs"); ok && len(mobs.Items) > 0 {
		return false
	}
	scan := trialScanTick(x, y, z, now)
	switch state {
	case TrialInactive:
		// A spawner with no saved spawn_data still has to store the one it rolls.
		return trialHasSpawnData(tag) && !trialSpawnDataHasID(tag)
	case TrialWaitingForPlayers:
		return !scan && canSpawn && trialSpawnDataHasID(tag)
	case TrialCooldown:
		players, _ := nbt.Get[nbt.List](tag, "registered_players")
		ends, _ := nbt.LongValue(tag["cooldown_ends_at"])
		return !scan && len(players.Items) == 0 && now < ends
	}
	return false
}

func trialHasSpawnData(tag nbt.Compound) bool {
	data, _ := nbt.Get[nbt.Compound](tag, "spawn_data")
	_, ok := nbt.Get[nbt.Compound](data, "entity")
	return ok
}

func trialSpawnDataHasID(tag nbt.Compound) bool {
	data, _ := nbt.Get[nbt.Compound](tag, "spawn_data")
	entity, _ := nbt.Get[nbt.Compound](data, "entity")
	_, ok := nbt.Get[nbt.String](entity, "id")
	return ok
}

// trialSpawnerParsed is trialSpawnerConfigs parsed once. The compounds are
// shared: callers must not modify them.
var trialSpawnerParsed = sync.OnceValue(func() map[string]nbt.Compound {
	out := make(map[string]nbt.Compound, len(trialSpawnerConfigs))
	for name, snbt := range trialSpawnerConfigs {
		out[name], _ = nbt.ParseSNBT(snbt)
	}
	return out
})

var packTrialConfigs atomic.Pointer[map[string]nbt.Compound]

// ApplyPackTrialSpawnerConfigs installs the data packs' trial_spawner configs
// over the built-in ones; an entry that fails TrialSpawnerConfig.DIRECT_CODEC's
// ranges is skipped and named in the result. nil clears them.
// vanilla: RegistryDataLoader.WORLD_REGISTRIES, TrialSpawnerConfig.DIRECT_CODEC
func ApplyPackTrialSpawnerConfigs(packs map[string]nbt.Tag) (skipped []string) {
	out := make(map[string]nbt.Compound, len(packs))
	for name, tag := range packs {
		cfg, ok := nbt.CompoundView(tag)
		if !ok || !trialSettingsOf(cfg).Valid() {
			skipped = append(skipped, name)
			continue
		}
		out[name] = cfg
	}
	slices.Sort(skipped)
	if len(out) == 0 {
		packTrialConfigs.Store(nil)
	} else {
		packTrialConfigs.Store(&out)
	}
	return skipped
}

// trialSettingsOf reads a configuration's numeric fields as written, the
// defaults for an absent one.
func trialSettingsOf(cfg nbt.Compound) TrialSpawnerSettings {
	c := defaultTrialConfig()
	c.read(cfg)
	return TrialSpawnerSettings{c.spawnRange, c.totalMobs, c.simultaneousMobs, c.totalPerPlayer, c.simultaneousPerPlayer, c.ticksBetweenSpawn}
}

// trialConfigTag is the configuration saved under key: inline, or the registry
// name of a data pack config. An unknown name reads as no configuration.
// vanilla: TrialSpawner.Config, RegistryFileCodec
func trialConfigTag(tag nbt.Compound, key string) nbt.Compound {
	switch v := tag[key].(type) {
	case nbt.CompoundTag:
		compoundFields := v.Entries()
		return compoundFields
	case nbt.String:
		name := registry.Qualify(string(v))
		if m := packTrialConfigs.Load(); m != nil {
			if cfg, ok := (*m)[name]; ok {
				return cfg
			}
		}
		return trialSpawnerParsed()[name]
	}
	return nil
}

// trialScanTick is the throttle of the player scan: one tick in twenty per
// spawner, staggered by position.
// vanilla: TrialSpawnerStateData.tryDetectPlayers
func trialScanTick(x, y, z int32, now int64) bool {
	return (PackBlockPos(x, y, z)+now)%trialScanPeriod == 0
}

// ParseTrialSpawner reads the saved data and the normal configuration.
// vanilla: TrialSpawner.load
func ParseTrialSpawner(tag nbt.Compound) *TrialSpawner {
	t := &TrialSpawner{cooldownLength: 36000, requiredRange: 14,
		normal: defaultTrialConfig(), ominousCfg: defaultTrialConfig()}
	if v, ok := nbt.IntValue(tag["target_cooldown_length"]); ok && v >= 0 {
		t.cooldownLength = int(v)
	}
	if v, ok := nbt.IntValue(tag["required_player_range"]); ok && v >= 1 && v <= 128 {
		t.requiredRange = int(v)
	}
	if cfg := trialConfigTag(tag, "normal_config"); cfg != nil {
		t.normal.read(cfg)
	}
	if cfg := trialConfigTag(tag, "ominous_config"); cfg != nil {
		t.ominousCfg.read(cfg)
	}
	t.players = trialUUIDs(tag, "registered_players")
	t.mobs = trialUUIDs(tag, "current_mobs")
	t.cooldownEndsAt, _ = nbt.LongValue(tag["cooldown_ends_at"])
	t.nextMobSpawnsAt, _ = nbt.LongValue(tag["next_mob_spawns_at"])
	if v, ok := nbt.IntValue(tag["total_mobs_spawned"]); ok && v >= 0 {
		t.totalMobsSpawned = int(v)
	}
	loot, _ := nbt.Get[nbt.String](tag, "ejecting_loot_table")
	t.ejectingLoot = string(loot)
	if data, ok := nbt.Get[nbt.Compound](tag, "spawn_data"); ok {
		if _, ok := nbt.Get[nbt.Compound](data, "entity"); ok {
			t.nextSpawnData = data
		}
	}
	return t
}

func (c *trialConfig) read(cfg nbt.Compound) {
	if v, ok := nbt.IntValue(cfg["spawn_range"]); ok && v >= 1 && v <= 128 {
		c.spawnRange = int(v)
	}
	readFloat := func(key string, dst *float32) {
		switch v := cfg[key].(type) {
		case nbt.Float:
			if v >= 0 {
				*dst = float32(v)
			}
		case nbt.Double:
			if v >= 0 {
				*dst = float32(v)
			}
		default:
			if n, ok := nbt.IntValue(cfg[key]); ok && n >= 0 {
				*dst = float32(n)
			}
		}
	}
	readFloat("total_mobs", &c.totalMobs)
	readFloat("simultaneous_mobs", &c.simultaneousMobs)
	readFloat("total_mobs_added_per_player", &c.totalPerPlayer)
	readFloat("simultaneous_mobs_added_per_player", &c.simultaneousPerPlayer)
	if v, ok := nbt.IntValue(cfg["ticks_between_spawn"]); ok && v >= 0 {
		c.ticksBetweenSpawn = int(v)
	}
	if list, ok := nbt.Get[nbt.List](cfg, "loot_tables_to_eject"); ok {
		c.lootTables = nil
		for _, item := range list.Items {
			entry, ok := nbt.CompoundView(item)
			if !ok {
				continue
			}
			table, ok := nbt.Get[nbt.String](entry, "data")
			if !ok {
				continue
			}
			weight := 1
			if w, ok := nbt.IntValue(entry["weight"]); ok {
				weight = int(w)
			}
			if weight > 0 {
				c.lootTables = append(c.lootTables, trialLoot{weight, string(table)})
			}
		}
	}
	if table, ok := nbt.Get[nbt.String](cfg, "items_to_drop_when_ominous"); ok {
		c.itemsToDrop = registry.Qualify(string(table))
	}
	list, _ := nbt.Get[nbt.List](cfg, "spawn_potentials")
	for _, item := range list.Items {
		entry, ok := nbt.CompoundView(item)
		if !ok {
			continue
		}
		data, ok := nbt.Get[nbt.Compound](entry, "data")
		if !ok {
			continue
		}
		weight := 1
		if w, ok := nbt.IntValue(entry["weight"]); ok {
			weight = int(w)
		}
		if weight > 0 {
			c.potentials = append(c.potentials, trialPotential{weight, data})
		}
	}
}

func trialUUIDs(tag nbt.Compound, key string) []uuid.UUID {
	list, _ := nbt.Get[nbt.List](tag, key)
	var out []uuid.UUID
	for _, item := range list.Items {
		if words, ok := item.(nbt.IntArray); ok {
			if id, ok := nbt.UUIDFromIntArray(words); ok && !slices.Contains(out, id) {
				out = append(out, id)
			}
		}
	}
	return out
}

func trialUUIDList(ids []uuid.UUID) nbt.List {
	items := make([]nbt.Tag, len(ids))
	for i, id := range ids {
		items[i] = nbt.UUIDToIntArray(id)
	}
	return nbt.List{Elem: nbt.TypeIntArray, Items: items}
}

// Store returns the compound with the saved data written over it. The input
// is not modified: the world hands the same map to readers.
// vanilla: TrialSpawnerStateData.Packed.MAP_CODEC
func (t *TrialSpawner) Store(tag nbt.Compound) nbt.Compound {
	out := make(nbt.Compound, len(tag)+7)
	maps.Copy(out, tag)
	setUnless := func(key string, v nbt.Tag, isDefault bool) {
		if isDefault {
			delete(out, key)
		} else {
			out[key] = v
		}
	}
	setUnless("registered_players", trialUUIDList(t.players), len(t.players) == 0)
	setUnless("current_mobs", trialUUIDList(t.mobs), len(t.mobs) == 0)
	setUnless("cooldown_ends_at", nbt.Long(t.cooldownEndsAt), t.cooldownEndsAt == 0)
	setUnless("next_mob_spawns_at", nbt.Long(t.nextMobSpawnsAt), t.nextMobSpawnsAt == 0)
	setUnless("total_mobs_spawned", nbt.Int(t.totalMobsSpawned), t.totalMobsSpawned == 0)
	setUnless("ejecting_loot_table", nbt.String(t.ejectingLoot), t.ejectingLoot == "")
	setUnless("spawn_data", t.nextSpawnData, t.nextSpawnData == nil)
	// vanilla: TrialSpawner.FullConfig.MAP_CODEC, lenientOptionalFieldOf defaults
	setUnless("target_cooldown_length", nbt.Int(t.cooldownLength), t.cooldownLength == 36000)
	setUnless("required_player_range", nbt.Int(t.requiredRange), t.requiredRange == 14)
	storeTrialConfig(out, "normal_config", &t.normal)
	storeTrialConfig(out, "ominous_config", &t.ominousCfg)
	return out
}

// storeTrialConfig writes an inline configuration back through
// TrialSpawnerConfig.DIRECT_CODEC: the mob counts as floats, a field equal to
// TrialSpawnerConfig.DEFAULT left out. A registry name stays as it is.
func storeTrialConfig(out nbt.Compound, key string, c *trialConfig) {
	inline, ok := out[key].(nbt.CompoundTag)
	if !ok {
		return
	}
	def := defaultTrialConfig()
	cfg := maps.Clone(inline.Entries())
	for _, f := range [...]struct {
		key    string
		v, def float32
	}{
		{"total_mobs", c.totalMobs, def.totalMobs},
		{"simultaneous_mobs", c.simultaneousMobs, def.simultaneousMobs},
		{"total_mobs_added_per_player", c.totalPerPlayer, def.totalPerPlayer},
		{"simultaneous_mobs_added_per_player", c.simultaneousPerPlayer, def.simultaneousPerPlayer},
	} {
		if _, ok := cfg[f.key]; !ok {
			continue
		}
		if f.v == f.def {
			delete(cfg, f.key)
		} else {
			cfg[f.key] = nbt.Float(f.v)
		}
	}
	for _, f := range [...]struct {
		key    string
		v, def int
	}{
		{"spawn_range", c.spawnRange, def.spawnRange},
		{"ticks_between_spawn", c.ticksBetweenSpawn, def.ticksBetweenSpawn},
	} {
		if _, ok := cfg[f.key]; !ok {
			continue
		}
		if f.v == f.def {
			delete(cfg, f.key)
		} else {
			cfg[f.key] = nbt.Int(f.v)
		}
	}
	if list, ok := nbt.Get[nbt.List](cfg, "spawn_potentials"); ok && len(list.Items) == 0 {
		delete(cfg, "spawn_potentials")
	}
	if slices.Equal(c.lootTables, def.lootTables) {
		delete(cfg, "loot_tables_to_eject")
	}
	if c.itemsToDrop == def.itemsToDrop {
		delete(cfg, "items_to_drop_when_ominous")
	}
	out[key] = cfg
}

// OverrideTrialEntity is the saved form of a spawner told to spawn the type:
// the saved data cleared and both configurations spawning only that mob, with
// the entity_data fields (nil for none) in its entity tag.
// vanilla: TrialSpawner.overrideEntityToSpawn, TrialSpawnerConfig.withSpawning
func OverrideTrialEntity(tag nbt.Compound, t MobType, data nbt.Compound) nbt.Compound {
	out := ParseTrialSpawner(tag).reset().Store(tag)
	entityTag := nbt.Compound{}
	maps.Copy(entityTag, data)
	entityTag["id"] = nbt.String(t.Name())
	spawnData := nbt.Compound{"entity": entityTag}
	for _, key := range []string{"normal_config", "ominous_config"} {
		cfg := maps.Clone(trialConfigTag(tag, key))
		if cfg == nil {
			cfg = nbt.Compound{}
		}
		cfg["spawn_potentials"] = nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
			nbt.Compound{"data": spawnData, "weight": nbt.Int(1)},
		}}
		out[key] = cfg
	}
	return out
}

// reset is TrialSpawnerStateData.reset.
func (t *TrialSpawner) reset() *TrialSpawner {
	t.mobs, t.nextSpawnData = nil, nil
	t.resetStatistics()
	return t
}

// resetStatistics is TrialSpawnerStateData.resetStatistics.
func (t *TrialSpawner) resetStatistics() {
	if len(t.players) > 0 || t.totalMobsSpawned != 0 || t.nextMobSpawnsAt != 0 || t.cooldownEndsAt != 0 {
		t.Dirty = true
	}
	t.players, t.totalMobsSpawned, t.nextMobSpawnsAt, t.cooldownEndsAt = nil, 0, 0, 0
}

// rollSpawnData is getOrCreateNextSpawnData: the saved next spawn, or a
// weighted pick from the potentials.
func (t *TrialSpawner) rollSpawnData(rng *rand.Rand) nbt.Compound {
	if t.nextSpawnData != nil {
		return t.nextSpawnData
	}
	t.nextSpawnData = nbt.Compound{"entity": nbt.Compound{}}
	if pick := t.active().pick(rng); pick != nil {
		t.nextSpawnData = nbt.Copy(pick).(nbt.Compound)
	}
	t.Dirty, t.Updated = true, true
	return t.nextSpawnData
}

// pick is WeightedList.getRandom over the potentials.
func (c *trialConfig) pick(rng *rand.Rand) nbt.Compound {
	total := 0
	for _, p := range c.potentials {
		total += p.weight
	}
	if total <= 0 {
		return nil
	}
	n := rng.Intn(total)
	for _, p := range c.potentials {
		if n -= p.weight; n < 0 {
			return p.data
		}
	}
	return nil
}

// pickLoot is WeightedList.getRandom over loot_tables_to_eject, empty for an
// empty list.
func (c *trialConfig) pickLoot(rng *rand.Rand) string {
	total := 0
	for _, l := range c.lootTables {
		total += l.weight
	}
	if total <= 0 {
		return ""
	}
	n := rng.Intn(total)
	for _, l := range c.lootTables {
		if n -= l.weight; n < 0 {
			return l.table
		}
	}
	return ""
}

// eject rolls the ejecting loot table and throws its items up out of the
// spawner.
// vanilla: TrialSpawner.ejectReward, DefaultDispenseItemBehavior.spawnItem
func (t *TrialSpawner) eject(h TrialHost, rng *rand.Rand, x, y, z int32) {
	stacks := h.Loot(t.ejectingLoot)
	if len(stacks) == 0 {
		return
	}
	for _, stack := range stacks {
		px, py, pz, vx, vy, vz := ejectUp(rng, x, y, z)
		h.DropItem(stack, px, py, pz, vx, vy, vz)
	}
	h.LevelEvent(TrialEventEjectItem, x, y, z, 0)
}

// ejectUp is where and how fast an item thrown up out of the block at x, y, z
// starts: 1.2 above the block's bottom centre, the item's y being its bottom,
// with the motion of an accuracy 2 dispense.
// vanilla: DefaultDispenseItemBehavior.spawnItem
func ejectUp(rng *rand.Rand, x, y, z int32) (px, py, pz, vx, vy, vz float64) {
	px, py, pz = float64(x)+0.5, float64(y)+1.2-0.125, float64(z)+0.5
	// The draw for the x and z push, which the Up face scales to zero.
	rng.Float64()
	triangle := func(mean float64) float64 {
		return mean + trialEjectSpread*(rng.Float64()-rng.Float64())
	}
	vx = triangle(0)
	vy = triangle(0.2)
	vz = triangle(0)
	return
}

// hasMobToSpawn is TrialSpawnerStateData.hasMobToSpawn.
func (t *TrialSpawner) hasMobToSpawn(rng *rand.Rand) bool {
	data := t.rollSpawnData(rng)
	entity, _ := nbt.Get[nbt.Compound](data, "entity")
	_, hasID := nbt.Get[nbt.String](entity, "id")
	return hasID || len(t.active().potentials) > 0
}

// active is the configuration of the spawner's current mode.
// vanilla: TrialSpawner.activeConfig
func (t *TrialSpawner) active() *trialConfig {
	if t.Ominous {
		return &t.ominousCfg
	}
	return &t.normal
}

// totalTarget is TrialSpawnerConfig.calculateTargetTotalMobs.
func (c *trialConfig) totalTarget(extra int) int {
	return int(math.Floor(float64(c.totalMobs + c.totalPerPlayer*float32(extra))))
}

// simultaneousTarget is TrialSpawnerConfig.calculateTargetSimultaneousMobs.
func (c *trialConfig) simultaneousTarget(extra int) int {
	return int(math.Floor(float64(c.simultaneousMobs + c.simultaneousPerPlayer*float32(extra))))
}

// TickTrialSpawner runs one server tick in the mode Ominous names and
// returns the next state (the same when it stays). The caller stores the
// data when Dirty, updates clients when Updated, and stores Ominous in the
// block when it changed.
// vanilla: TrialSpawner.tickServer, TrialSpawnerState.tickAndGetNext
func TickTrialSpawner(h TrialHost, t *TrialSpawner, x, y, z int32, state string) string {
	now := h.GameTime()
	if before := len(t.mobs); before > 0 {
		t.mobs = slices.DeleteFunc(t.mobs, func(id uuid.UUID) bool { return !trialTracked(h, id, x, y, z) })
		if len(t.mobs) != before {
			t.nextMobSpawnsAt = now + int64(t.active().ticksBetweenSpawn)
			t.Dirty = true
		}
	}
	rng := h.Rand()
	// The configuration is read once: a spawner that turns ominous in this
	// tick finishes it on the normal one.
	cfg := t.active()
	switch state {
	case TrialInactive:
		data := t.rollSpawnData(rng)
		entity, _ := nbt.Get[nbt.Compound](data, "entity")
		if _, ok := nbt.Get[nbt.String](entity, "id"); ok {
			return TrialWaitingForPlayers
		}
	case TrialWaitingForPlayers:
		if !h.CanSpawn() {
			t.resetStatistics()
			return state
		}
		if !t.hasMobToSpawn(rng) {
			return TrialInactive
		}
		t.detect(h, x, y, z, state)
		if len(t.players) > 0 {
			return TrialActive
		}
	case TrialActive:
		if !h.CanSpawn() {
			t.resetStatistics()
			return TrialWaitingForPlayers
		}
		if !t.hasMobToSpawn(rng) {
			return TrialInactive
		}
		extra := max(0, len(t.players)-1)
		t.detect(h, x, y, z, state)
		if t.Ominous {
			t.spawnOminousItemSpawner(h, x, y, z, cfg)
		}
		if t.totalMobsSpawned >= cfg.totalTarget(extra) {
			if len(t.mobs) == 0 {
				t.cooldownEndsAt = now + int64(t.cooldownLength)
				t.totalMobsSpawned, t.nextMobSpawnsAt = 0, 0
				t.Dirty = true
				return TrialWaitingForRewardEject
			}
		} else if now >= t.nextMobSpawnsAt && len(t.mobs) < cfg.simultaneousTarget(extra) {
			t.spawnMob(h, x, y, z, now, cfg)
		}
	case TrialWaitingForRewardEject:
		if float32(now) >= float32(t.cooldownEndsAt-int64(t.cooldownLength))+trialShutterDelay {
			h.Sound(registry.SoundBlockTrialSpawnerOpenShutter, x, y, z)
			return TrialEjectingReward
		}
	case TrialEjectingReward:
		started := t.cooldownEndsAt - int64(t.cooldownLength)
		if (now-started)%trialEjectionPeriod != 0 {
			return state
		}
		if len(t.players) == 0 {
			h.Sound(registry.SoundBlockTrialSpawnerCloseShutter, x, y, z)
			t.ejectingLoot = ""
			t.Dirty = true
			return TrialCooldown
		}
		if t.ejectingLoot == "" {
			t.ejectingLoot = cfg.pickLoot(rng)
		}
		if t.ejectingLoot != "" {
			t.eject(h, rng, x, y, z)
		}
		t.players = t.players[1:]
		t.Dirty = true
	case TrialCooldown:
		t.detect(h, x, y, z, state)
		if len(t.players) > 0 {
			t.totalMobsSpawned, t.nextMobSpawnsAt = 0, 0
			t.Dirty = true
			return TrialActive
		}
		if now >= t.cooldownEndsAt {
			t.Ominous = false
			t.reset()
			return TrialWaitingForPlayers
		}
	}
	return state
}

// detect is TrialSpawnerStateData.tryDetectPlayers.
func (t *TrialSpawner) detect(h TrialHost, x, y, z int32, state string) {
	now := h.GameTime()
	if !trialScanTick(x, y, z, now) || (state == TrialCooldown && t.Ominous) {
		return
	}
	inSight := trialDetect(h, x, y, z, t.requiredRange, true)
	becameOminous := false
	if !t.Ominous && len(inSight) > 0 {
		if player, ok := trialOmenPlayer(h, inSight); ok {
			if !player.TrialOmen {
				h.BadOmenToTrialOmen(player.UUID)
			}
			// vanilla: BlockPos.containing(player.getEyePosition())
			h.LevelEvent(TrialEventBecomeOminous, int32(math.Floor(player.X)),
				int32(math.Floor(player.Y+player.Eye)), int32(math.Floor(player.Z)), 0)
			t.applyOminous(h, x, y, z)
			becameOminous = true
		}
	}
	if state == TrialCooldown && !becameOminous {
		return
	}
	found := inSight
	if len(t.players) > 0 {
		// Only the first player must be in line of sight.
		found = trialDetect(h, x, y, z, t.requiredRange, false)
	}
	added := false
	for _, id := range found {
		if !slices.Contains(t.players, id) {
			t.players = append(t.players, id)
			added = true
		}
	}
	if added {
		t.nextMobSpawnsAt = max(now+trialDetectSpawnBuffer, t.nextMobSpawnsAt)
		t.Dirty = true
		if !becameOminous {
			event := int32(TrialEventDetectPlayer)
			if t.Ominous {
				event = TrialEventDetectOminous
			}
			h.LevelEvent(event, x, y, z, int32(len(t.players)))
		}
	}
}

// spawnMob is TrialSpawner.spawnMob plus the bookkeeping of the active tick,
// which reads the configuration cfg the tick began with.
func (t *TrialSpawner) spawnMob(h TrialHost, x, y, z int32, now int64, cfg *trialConfig) {
	rng := h.Rand()
	data := t.rollSpawnData(rng)
	toSpawn, _ := nbt.Get[nbt.Compound](data, "entity")
	id, _ := nbt.Get[nbt.String](toSpawn, "id")
	kind := MobTypeOf(string(id))
	// A type no mob here is cannot be loaded, vanilla's missing entity type.
	if id == "" || !SpawnableKind(kind) {
		return
	}
	var fx, fy, fz float64
	if pos, ok := nbt.Get[nbt.List](toSpawn, "Pos"); ok && len(pos.Items) == 3 {
		fx, fy, fz = listDouble(pos, 0), listDouble(pos, 1), listDouble(pos, 2)
	} else {
		fx, fy, fz = SpawnerSpawnPos(rng, x, y, z, t.active().spawnRange)
	}
	w := h.World()
	if !TrialSpawnerAllows(w, rng, kind, fx, fy, fz) {
		return
	}
	if !h.LineClear(x, y, z, fx, fy, fz) {
		return
	}
	if !customSpawnRulesAllow(data, w, int32(math.Floor(fx)), int32(math.Floor(fy)), int32(math.Floor(fz))) {
		return
	}
	yaw := rng.Float32() * 360
	if !TrialMobFits(w, kind, fx, fy, fz, h.Bodies()) {
		return
	}
	plain := len(toSpawn) == 1
	var tag nbt.Compound
	if !plain {
		tag = maps.Clone(toSpawn)
		delete(tag, "Pos")
	}
	mobID, ok := h.StandUp(kind, fx, fy, fz, yaw, tag, plain, spawnEquipment(h.Loot, data))
	if !ok {
		return
	}
	bx, by, bz := int32(math.Floor(fx)), int32(math.Floor(fy)), int32(math.Floor(fz))
	flame := int32(0)
	if t.Ominous {
		flame = 1
	}
	h.LevelEvent(TrialEventSpawn, x, y, z, flame)
	h.LevelEvent(TrialEventSpawnMobAt, bx, by, bz, flame)
	t.mobs = append(t.mobs, mobID)
	t.totalMobsSpawned++
	t.nextMobSpawnsAt = now + int64(cfg.ticksBetweenSpawn)
	t.Dirty = true
	if next := cfg.pick(rng); next != nil {
		t.nextSpawnData = nbt.Copy(next).(nbt.Compound)
		t.Updated = true
	}
}

// spawnEquipment rolls the spawn data's equipment table with loot and returns
// what dresses the mob with it, nil for spawn data without one.
// vanilla: Mob.equip(EquipmentTable)
func spawnEquipment(loot func(table string) []protocol.ItemStack, data nbt.Compound) func(*Mob) {
	equipment, ok := nbt.Get[nbt.Compound](data, "equipment")
	if !ok {
		return nil
	}
	table, ok := nbt.Get[nbt.String](equipment, "loot_table")
	if !ok {
		return nil
	}
	stacks := loot(registry.Qualify(string(table)))
	chances := map[string]float32{}
	switch v := equipment["slot_drop_chances"].(type) {
	case nbt.CompoundTag:
		compoundFields := v.Entries()
		for slot, chance := range compoundFields {
			if f, ok := floatValue(chance); ok && f >= 0 {
				chances[slot] = f
			}
		}
	default:
		// a single number is every slot's chance
		if f, ok := floatValue(v); ok && f >= 0 {
			for _, slot := range equipmentSlotNames {
				chances[slot] = f
			}
		}
	}
	return func(m *Mob) { m.EquipRolled(stacks, chances) }
}

// equipmentSlotNames are the names of EquipmentSlot, which a drop chance
// given as one number covers.
var equipmentSlotNames = []string{"mainhand", "offhand", "feet", "legs", "chest", "head", "body", "saddle"}

// floatValue is a numeric tag as a float.
func floatValue(tag nbt.Tag) (float32, bool) {
	switch v := tag.(type) {
	case nbt.Float:
		return float32(v), true
	case nbt.Double:
		return float32(v), true
	}
	i, ok := nbt.IntValue(tag)
	return float32(i), ok
}

// customSpawnRulesAllow is SpawnData.CustomSpawnRules.isValidPosition: the
// block light and the sky light less the sky darkening of the cell, floored at
// 0, must lie in the data's limits, each 0..15 when absent.
// vanilla: LevelReader.getEffectiveSkyBrightness
func customSpawnRulesAllow(data nbt.Compound, w SpawnWorld, x, y, z int32) bool {
	limits, ok := nbt.Get[nbt.Compound](data, "custom_spawn_rules")
	if !ok {
		return true
	}
	blockLo, blockHi := lightLimit(limits["block_light_limit"])
	skyLo, skyHi := lightLimit(limits["sky_light_limit"])
	block := int(w.BlockLightAt(x, y, z))
	sky := max(int(w.SkyLightAt(x, y, z))-w.SkyDarken(), 0)
	return block >= blockLo && block <= blockHi && sky >= skyLo && sky <= skyHi
}

// lightLimit reads an InclusiveRange of light levels: a single number, a
// two-element list or a min_inclusive / max_inclusive compound. A range that
// is malformed or outside 0..15 reads as the whole range, where vanilla
// refuses the whole spawn data.
func lightLimit(tag nbt.Tag) (lo, hi int) {
	lo, hi = 0, 15
	var a, b int32
	var ok bool
	switch v := tag.(type) {
	case nbt.CompoundTag:
		compoundFields := v.Entries()
		var okA, okB bool
		a, okA = nbt.IntValue(compoundFields["min_inclusive"])
		b, okB = nbt.IntValue(compoundFields["max_inclusive"])
		ok = okA && okB
	case nbt.List:
		if len(v.Items) == 2 {
			var okA, okB bool
			a, okA = nbt.IntValue(v.Items[0])
			b, okB = nbt.IntValue(v.Items[1])
			ok = okA && okB
		}
	default:
		a, ok = nbt.IntValue(tag)
		b = a
	}
	if !ok || a < 0 || b > 15 || a > b {
		return lo, hi
	}
	return int(a), int(b)
}

// TrialSpawnerAllows is SpawnerAllows for the TRIAL_SPAWNER reason, which
// does not ask a monster for darkness.
// vanilla: Monster.checkMonsterSpawnRules, EntitySpawnReason.ignoresLightRequirements
func TrialSpawnerAllows(w SpawnWorld, rng *rand.Rand, t MobType, x, y, z float64) bool {
	if boxCollidesBlocks(w, t, x, y, z) || !AllowedAt(t, w.Difficulty()) {
		return false
	}
	bx, by, bz := int32(math.Floor(x)), int32(math.Floor(y)), int32(math.Floor(z))
	return spawnerSpawnRules(w, rng, t, bx, by, bz, true)
}

// TrialMobFits is the spawn obstruction the made mob must pass: no liquid in
// the box for the kinds that keep out of it, no body in the way and the
// type's ground restrictions.
// vanilla: Mob.checkSpawnObstruction, Ocelot.checkSpawnObstruction
func TrialMobFits(w SpawnWorld, t MobType, x, y, z float64, bodies []SpawnBody) bool {
	if t == TypeOcelot && !ocelotSpawnGroundOk(w, int32(math.Floor(x)), int32(math.Floor(y)), int32(math.Floor(z))) {
		return false
	}
	if !leavesLiquidOut(t) && boxInLiquid(w, t, x, y, z) {
		return false
	}
	return !boxOverlapsBody(bodies, spawnBox(t, x, y, z))
}
