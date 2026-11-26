package server

import (
	"slices"

	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// spawnReasonNBTKey is the NBT tag key Paper saves an entity's spawn reason
// under; omitted when SpawnDefault so unmodified saves stay vanilla-clean.
const spawnReasonNBTKey = "Paper.SpawnReason"

// originNBTKey is the NBT tag key Paper saves an entity's spawn origin under;
// omitted on /data get (empty id) so commands stay vanilla-clean.
const originNBTKey = "Paper.Origin"

func putOrigin(tag nbt.CompoundTag, id string, o entityOrigin) {
	if id == "" || !o.set {
		return
	}
	tag.Set(originNBTKey, nbt.NewList([]nbt.Tag{nbt.Double(o.x), nbt.Double(o.y), nbt.Double(o.z)}))
}

func readOrigin(dst *entityOrigin, dim string, compound nbt.Compound) {
	list, ok := nbt.Get[nbt.List](compound, originNBTKey)
	if !ok || len(list.Items) != 3 {
		return
	}
	x, okX := list.Items[0].(nbt.Double)
	y, okY := list.Items[1].(nbt.Double)
	z, okZ := list.Items[2].(nbt.Double)
	if okX && okY && okZ {
		*dst = entityOrigin{set: true, dim: dim, x: float64(x), y: float64(y), z: float64(z)}
	}
}

func spawnReasonCode(r plugin.SpawnReason) uint8 { return uint8(r) + 1 }

func spawnReasonOf(code uint8) plugin.SpawnReason {
	if code == 0 {
		return plugin.SpawnDefault
	}
	return plugin.SpawnReason(code - 1)
}

func putSpawnReason(tag nbt.CompoundTag, id string, code uint8) {
	if id == "" || code == 0 {
		return
	}
	if r := spawnReasonOf(code); r != plugin.SpawnDefault && int(r) < len(spawnReasonNames) && spawnReasonNames[r] != "" {
		tag.Set(spawnReasonNBTKey, nbt.String(spawnReasonNames[r]))
	}
}

func readSpawnReason(dst *uint8, compound nbt.Compound) {
	if raw, ok := nbt.Get[nbt.String](compound, spawnReasonNBTKey); ok {
		if r, known := spawnReasonByName[string(raw)]; known {
			*dst = spawnReasonCode(r)
		}
	}
}

var spawnReasonNames = [...]string{
	plugin.SpawnNatural:         "NATURAL",
	plugin.SpawnChunkGeneration: "CHUNK_GEN",
	plugin.SpawnSpawner:         "SPAWNER",
	plugin.SpawnTrialSpawner:    "TRIAL_SPAWNER",
	plugin.SpawnEgg:             "EGG",
	plugin.SpawnBreeding:        "BREEDING",
	plugin.SpawnCommand:         "COMMAND",
	plugin.SpawnPlugin:          "CUSTOM",
	plugin.SpawnDispenser:       "DISPENSE_EGG",
	plugin.SpawnStructure:       "STRUCTURE",
	plugin.SpawnPatrol:          "PATROL",
	plugin.SpawnRaid:            "RAID",
	plugin.SpawnReinforcements:  "REINFORCEMENTS",
	plugin.SpawnConversion:      "CONVERSION",
	plugin.SpawnBucket:          "BUCKET",
	plugin.SpawnLightning:       "LIGHTNING",
	plugin.SpawnBuildIronGolem:  "BUILD_IRONGOLEM",
	plugin.SpawnBuildSnowGolem:  "BUILD_SNOWMAN",
	plugin.SpawnBuildWither:     "BUILD_WITHER",
	plugin.SpawnVillageDefense:  "VILLAGE_DEFENSE",
	plugin.SpawnJockey:          "JOCKEY",
	plugin.SpawnTriggered:       "TRIGGERED",
	plugin.SpawnDefault:         "DEFAULT",
	plugin.SpawnSpawnerEgg:      "SPAWNER_EGG",
}

var spawnReasonByName = func() map[string]plugin.SpawnReason {
	m := make(map[string]plugin.SpawnReason, len(spawnReasonNames))
	for r, name := range spawnReasonNames {
		if name != "" {
			m[name] = plugin.SpawnReason(r)
		}
	}
	return m
}()

// spawnAllowed puts a mob just added to EntitySpawnEvent listeners and
// discards it on a cancel, which the watchers in range see as an appearance
// and a removal in the same tick.
// vanilla: ServerLevel.addFreshEntity
func (s *Server) spawnAllowed(ent *mobEntity) bool {
	if s.plugins == nil || !s.plugins.Has(pluginruntime.EntitySpawn) {
		return true
	}
	e := &plugin.EntitySpawnEvent{Entity: pluginEntity{id: ent.uuid}, Reason: spawnReasonOf(ent.spawnReason)}
	s.firePluginEvent(e)
	if !e.Cancelled() {
		return true
	}
	s.discardSpawned(ent)
	return false
}

// discardSpawned removes a refused spawn.
func (s *Server) discardSpawned(ent *mobEntity) {
	s.discardMob(ent)
	s.flushDestroyed()
	s.dropEntityData(ent.uuid.String())
}

// mobMayInteract puts a mob's landing on farmland to EntityInteractEvent
// listeners, outside any lock, and reports whether the trample may go on. It
// is asked only where the roll can turn the farmland to dirt (a fall over half
// a block), before the roll, so a cancel leaves the roll unspent; pressure
// plates and tripwires are not asked.
// vanilla: FarmlandBlock.fallOn
func (s *Server) mobMayInteract(w *world.World, id uuid.UUID, x, y, z int32, fall float64) bool {
	if s.plugins == nil || !s.plugins.Has(pluginruntime.EntityInteract) || fall <= 0.5 ||
		registry.BlockOf(w.BlockAt(x, y, z)) != registry.BlockFarmland {
		return true
	}
	e := &plugin.EntityInteractEvent{
		Entity: pluginEntity{id: id},
		Block:  plugin.Block{World: pluginWorld{srv: s, dim: s.dimensionOfWorld(w), host: s.pluginAdapter}, Pos: plugin.BlockPos{X: int(x), Y: int(y), Z: int(z)}},
	}
	s.firePluginEvent(e)
	return !e.Cancelled()
}

// registerFreshItem puts a new item entity in the world and asks the spawn
// listeners about it.
func (s *Server) registerFreshItem(it *itemEntity) {
	s.registerItem(it)
	s.itemsSpawned(plugin.SpawnDefault, it)
}

// itemsSpawned asks EntitySpawnEvent about items just registered.
func (s *Server) itemsSpawned(reason plugin.SpawnReason, its ...*itemEntity) {
	if s.plugins == nil || !s.plugins.Has(pluginruntime.EntitySpawn) {
		return
	}
	for _, it := range its {
		s.bodySpawned(it.uuid, reason)
	}
}

// bodySpawned fires EntitySpawnEvent for a non-mob body (item, projectile,
// vehicle) just put in the world and discards it on a cancel, reporting
// whether it stays. Call it with no lock of the body's kind held.
// vanilla: ServerLevel.addFreshEntity
func (s *Server) bodySpawned(id uuid.UUID, reason plugin.SpawnReason) bool {
	if s.bodySpawnAllowed(id, reason) {
		return true
	}
	removeSpawned(s, id)
	return false
}

// bodySpawnAllowed puts the spawn of a body to EntitySpawnEvent listeners and
// leaves a refused one to the caller.
func (s *Server) bodySpawnAllowed(id uuid.UUID, reason plugin.SpawnReason) bool {
	if s.plugins == nil || !s.plugins.Has(pluginruntime.EntitySpawn) {
		return true
	}
	e := &plugin.EntitySpawnEvent{Entity: pluginEntity{id: id}, Reason: reason}
	s.askBody(func() { s.plugins.Fire(e) })
	return !e.Cancelled()
}

// removeSpawned discards a refused body. It is set in init: the entity
// operations reach every spawn site, so naming it in a declaration would be an
// initialization cycle.
var removeSpawned func(s *Server, id uuid.UUID)

func init() {
	removeSpawned = func(s *Server, id uuid.UUID) {
		bindPluginEntity(pluginEntity{id: id, host: s.pluginAdapter}, s.pluginAdapter).Remove()
	}
}

// askBody runs fn on the tick goroutine with no tick lock held. Unlike
// askUnlocked it never touches mobsMu: a spawn or a hurt of a non-mob body is
// not asked from a mob's hurt, and a client or command goroutine must not
// release a lock the tick holds.
func (s *Server) askBody(fn func()) {
	if !s.plugins.OnTick() {
		s.plugins.Call(fn)
		return
	}
	held := slices.Clone(s.tickLocks)
	for _, mu := range slices.Backward(held) {
		mu.Unlock()
	}
	s.tickLocks = s.tickLocks[:0]
	defer func() {
		for _, mu := range held {
			mu.Lock()
		}
		s.tickLocks = append(s.tickLocks, held...)
	}()
	fn()
}
