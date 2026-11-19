package server

import (
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// itemAsks are the despawns and merges ItemDespawnEvent and ItemMergeEvent
// listeners decide, collected under the items lock and put to them once it is
// down: releasing it mid-tick would lose the items added meanwhile.
// A nil *itemAsks, when nothing listens, asks nothing.
type itemAsks struct {
	despawns  []*itemEntity
	merges    []itemMergeAsk
	byBody    map[*entity.Item]*itemEntity
	onDespawn bool
	onMerge   bool
	onHurt    bool
	// hurts are the hurts the step let land tentatively, per item, for
	// EntityDamageEvent; a lethal one keeps its item alive until resolved.
	hurts map[*itemEntity][]itemHurtAsk
}

type itemHurtAsk struct {
	damage float32
	dt     registry.DamageType
	// health is the item's health before this hurt.
	health int
}

type itemMergeAsk struct {
	survivor, victim *itemEntity
	// counts are the stacks as the ask saw them; a merge applies only if they
	// still stand.
	survivorCount, victimCount int32
}

// newItemAsks is nil when no listener wants an item's despawn or merge.
func (s *Server) newItemAsks() *itemAsks {
	if s.plugins == nil {
		return nil
	}
	a := &itemAsks{onDespawn: s.plugins.Has(pluginruntime.ItemDespawn), onMerge: s.plugins.Has(pluginruntime.ItemMerge),
		onHurt: s.plugins.Has(pluginruntime.EntityDamage)}
	if !a.onDespawn && !a.onMerge && !a.onHurt {
		return nil
	}
	a.byBody = make(map[*entity.Item]*itemEntity)
	a.hurts = make(map[*itemEntity][]itemHurtAsk)
	return a
}

// hasHurts reports whether the step's hurts of it await their event.
func (a *itemAsks) hasHurts(it *itemEntity) bool { return a != nil && len(a.hurts[it]) > 0 }

// hearHurts has the step report it's hurts; clear drops the hook again.
func (a *itemAsks) hearHurts(it *itemEntity) {
	if a == nil || !a.onHurt {
		return
	}
	it.item.HurtAsk = func(damage float32, dt registry.DamageType, health int) {
		a.hurts[it] = append(a.hurts[it], itemHurtAsk{damage, dt, health})
	}
}

// watch hooks one live item's merges, or unhooks them.
func (a *itemAsks) watch(it *itemEntity) {
	if a == nil || !a.onMerge {
		if it.item.MergeAsk != nil {
			it.item.MergeAsk = nil // a listener went away
		}
		return
	}
	a.byBody[it.item] = it
	it.item.MergeAsk = a.askMerge
}

func (a *itemAsks) askMerge(survivor, victim *entity.Item) bool {
	sv, vi := a.byBody[survivor], a.byBody[victim]
	if sv == nil || vi == nil {
		return false
	}
	for _, m := range a.merges {
		if m.survivor == sv && m.victim == vi {
			return true
		}
	}
	a.merges = append(a.merges, itemMergeAsk{sv, vi, survivor.Stack.Count, victim.Stack.Count})
	return true
}

// despawn records an expiring item to ask about and reports whether it did.
func (a *itemAsks) despawn(it *itemEntity) bool {
	if a == nil || !a.onDespawn {
		return false
	}
	a.despawns = append(a.despawns, it)
	return true
}

// askItemHurts puts each tentative hurt of the step to EntityDamageEvent
// listeners, with no lock held, and answers each hurt item's health as the
// listeners leave it: a cancelled hurt spends nothing, an edited amount
// spends that.
// vanilla: ItemEntity.hurtServer
func (s *Server) askItemHurts(a *itemAsks) map[*itemEntity]int {
	if len(a.hurts) == 0 {
		return nil
	}
	out := make(map[*itemEntity]int, len(a.hurts))
	for it, hurts := range a.hurts {
		health := hurts[0].health
		for _, h := range hurts {
			e := &plugin.EntityDamageEvent{Entity: pluginEntity{id: it.uuid}, Source: s.pluginSource(it.w, h.dt, nil), Amount: h.damage, Final: h.damage}
			s.plugins.Fire(e)
			if !e.Cancelled() && e.Amount > 0 {
				health = int(float32(health) - e.Amount)
			}
		}
		out[it] = health
	}
	return out
}

// resolveItemAsks fires the collected events with no lock held and applies
// the outcome: a cancelled despawn restarts the item's age, a refused or
// stale merge is dropped.
// vanilla: ItemEntity.tick, ItemEntity.tryToMerge
func (s *Server) resolveItemAsks(a *itemAsks, despawns map[*world.World][]int32, merges *[]itemMetadataUpdate, spills *[]itemSpill) {
	if a == nil {
		return
	}
	hurt := s.askItemHurts(a)
	handle := func(it *itemEntity) plugin.ItemEntity { return pluginItemEntity{pluginEntity{id: it.uuid}} }
	keep := make([]bool, len(a.despawns))
	for i, it := range a.despawns {
		e := &plugin.ItemDespawnEvent{Item: handle(it)}
		s.plugins.Fire(e)
		keep[i] = e.Cancelled()
	}
	allowed := make([]bool, len(a.merges))
	for i, m := range a.merges {
		e := &plugin.ItemMergeEvent{Item: handle(m.victim), Target: handle(m.survivor)}
		s.plugins.Fire(e)
		allowed[i] = !e.Cancelled()
	}
	s.lockTick(&s.itemsMu)
	defer s.unlockTick(&s.itemsMu)
	for it, health := range hurt {
		wasDead := it.item.Dead
		it.item.Health = health
		switch {
		case health > 0 && wasDead:
			it.item.Dead = false
		case health <= 0 && wasDead:
			despawns[it.w] = append(despawns[it.w], it.item.ID)
			s.entityDestroyed(it.uuid)
			*spills = append(*spills, spillOf(it))
		}
	}
	for i, it := range a.despawns {
		switch {
		case it.item.Dead:
		case keep[i]:
			it.item.Age = 0
		default:
			it.item.Dead = true
			despawns[it.w] = append(despawns[it.w], it.item.ID)
			s.entityDestroyedBy(it.uuid, plugin.RemoveDespawn)
		}
	}
	for i, m := range a.merges {
		sv, vi := m.survivor, m.victim
		if !allowed[i] || sv.item.Dead || vi.item.Dead || sv.item.Stack.Count != m.survivorCount || vi.item.Stack.Count != m.victimCount {
			continue
		}
		entity.MergeStacks(sv.item, vi.item)
		if vi.item.Dead {
			despawns[vi.w] = append(despawns[vi.w], vi.item.ID)
			s.entityDestroyedBy(vi.uuid, plugin.RemoveMerge)
		}
		if sv.item.TakeMerge() {
			update := itemMetadataUpdate{it: sv, metadata: itemMetadataPayload(sv)}
			for id := range sv.tracked.all() {
				update.watchers = append(update.watchers, id)
			}
			*merges = append(*merges, update)
		}
	}
}

// askMobDrop fires EntityDropItemEvent for an item a living mob just dropped
// by itself and removes the item when a listener cancels. A zero mob (a
// death's drop) asks nothing. Runs with no lock held.
// vanilla: Entity.spawnAtLocation
func (s *Server) askMobDrop(mob uuid.UUID, it *itemEntity) {
	r := s.plugins
	if r == nil || mob == (uuid.UUID{}) || !r.Has(pluginruntime.EntityDropItem) {
		return
	}
	item := pluginItemEntity{pluginEntity{host: s.pluginAdapter, id: it.uuid}}
	e := &plugin.EntityDropItemEvent{Entity: pendingMob{id: mob}, Item: item}
	r.Fire(e)
	if e.Cancelled() {
		s.killItem(it)
	}
}
