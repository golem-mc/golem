package server

import (
	"slices"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// spawnAllowed puts a mob just added to EntitySpawnEvent listeners and
// discards it on a cancel, which the watchers in range see as an appearance
// and a removal in the same tick. The reason is what the caller named, else
// read off the spawn: a group is natural, a baby is an egg's chick.
// vanilla: ServerLevel.addFreshEntity
func (s *Server) spawnAllowed(ent *mobEntity, group *entity.SpawnGroup, baby bool) bool {
	if s.plugins == nil || !s.plugins.Has(pluginruntime.EntitySpawn) {
		return true
	}
	reason := plugin.SpawnDefault
	switch {
	case ent.spawnReason != 0:
		reason = plugin.SpawnReason(ent.spawnReason - 1)
	case group != nil:
		reason = plugin.SpawnNatural
	case baby:
		reason = plugin.SpawnEgg
	}
	e := &plugin.EntitySpawnEvent{Entity: pluginEntity{id: ent.uuid}, Reason: reason}
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
	if s.plugins == nil || !s.plugins.Has(pluginruntime.EntitySpawn) {
		return true
	}
	e := &plugin.EntitySpawnEvent{Entity: pluginEntity{id: id}, Reason: reason}
	s.askBody(func() { s.plugins.Fire(e) })
	if !e.Cancelled() {
		return true
	}
	removeSpawned(s, id)
	return false
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
