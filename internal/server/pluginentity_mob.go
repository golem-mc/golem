package server

import (
	"maps"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// The mob-family handle types sit on pluginEntity like the living ones; the
// stub one level deeper answers the methods not adapted yet.
type (

	// The per-interface method sets are separate types so a handle has
	// exactly the methods of its kind's interfaces: a method promoted from
	// pluginEntity would make every handle satisfy every interface.
	mobOps  struct{ e pluginEntity }
	ageOps  struct{ e pluginEntity }
	loveOps struct{ e pluginEntity }
	tameOps struct{ e pluginEntity }
	orbOps  struct{ e pluginEntity }

	pluginMobEntity struct {
		pluginEntity
		livingOps
		mobOps
	}
	pluginAgeableEntity struct {
		pluginEntity
		livingOps
		mobOps
		ageOps
	}
	pluginBreedableEntity struct {
		pluginEntity
		livingOps
		mobOps
		ageOps
		loveOps
	}
	pluginTameableEntity struct {
		pluginEntity
		livingOps
		mobOps
		ageOps
		loveOps
		tameOps
	}
	pluginOrbEntity struct {
		pluginEntity
		orbOps
	}
)

// pluginKindHandle picks the handle type a body's saved form calls for: the
// keys a kind always writes tell which interfaces it has. A zombie has no
// Age but is an Ageable through IsBaby.
// vanilla: Mob.addAdditionalSaveData, AgeableMob, Animal, TamableAnimal
func pluginKindHandle(base pluginEntity, saved nbt.Compound) plugin.Entity {
	has := func(k string) bool { _, ok := saved[k]; return ok }
	if h := pluginVariantOf(base, saved); h != nil && has("PersistenceRequired") {
		return h
	}
	switch {
	case !has("PersistenceRequired"):
		return pluginLivingEntity{pluginEntity: base, livingOps: livingOps{base}}
	case has("Sitting") && base.Type() != registry.EntityTypeFox, has("Tame"):
		return pluginTameableEntity{pluginEntity: base, livingOps: livingOps{base}, mobOps: mobOps{base}, ageOps: ageOps{base}, loveOps: loveOps{base}, tameOps: tameOps{base}}
	case has("InLove"):
		return pluginBreedableEntity{pluginEntity: base, livingOps: livingOps{base}, mobOps: mobOps{base}, ageOps: ageOps{base}, loveOps: loveOps{base}}
	case has("Age") || has("IsBaby"):
		return pluginAgeableEntity{pluginEntity: base, livingOps: livingOps{base}, mobOps: mobOps{base}, ageOps: ageOps{base}}
	}
	return pluginMobEntity{pluginEntity: base, livingOps: livingOps{base}, mobOps: mobOps{base}}
}

func (e pluginEntity) uuidField(key string) (plugin.UUID, bool) {
	id, ok := nbt.UUIDField(e.saved(), key)
	return plugin.UUID(id), ok && id != uuid.Nil
}

// setUUID writes the identity at key, or drops the key for the zero one.
func (e pluginEntity) setUUID(key string, id plugin.UUID) {
	if id == (plugin.UUID{}) {
		e.drop(key)
		return
	}
	e.merge(nbt.Compound{key: nbt.UUIDToIntArray(uuid.UUID(id))})
}

func uuidOf(id plugin.UUID) uuid.UUID { return uuid.UUID(id) }

func (e mobOps) AI() bool     { return !e.e.flag("NoAI") }
func (e mobOps) SetAI(v bool) { e.e.setFlag("NoAI", !v) }
func (e mobOps) SavedData() nbt.Compound {
	return maps.Clone(e.e.saved())
}
func (e mobOps) MergeData(fields nbt.Compound) bool { return e.e.merge(fields) }
func (e mobOps) Persistent() bool                   { return e.e.flag("PersistenceRequired") }
func (e mobOps) SetPersistent(v bool)               { e.e.setFlag("PersistenceRequired", v) }
func (e mobOps) LeftHanded() bool                   { return e.e.flag("LeftHanded") }
func (e mobOps) SetLeftHanded(v bool)               { e.e.setFlag("LeftHanded", v) }

func (e mobOps) Loot() (plugin.Key, int64) {
	saved := e.e.saved()
	name, _ := nbt.Get[nbt.String](saved, "DeathLootTable")
	k, _ := plugin.ParseKey(string(name))
	seed, _ := nbt.Get[nbt.Long](saved, "DeathLootTableSeed")
	return k, int64(seed)
}
func (e mobOps) SetLoot(table plugin.Key, seed int64) {
	e.e.merge(nbt.Compound{"DeathLootTable": nbt.String(table.String()), "DeathLootTableSeed": nbt.Long(seed)})
}

// Age is the ticks to grow up while negative; a zombie has none of its own.
func (e ageOps) Age() int { n, _ := nbt.IntField(e.e.saved(), "Age"); return int(n) }
func (e ageOps) SetAge(n int) {
	e.e.merge(nbt.Compound{"Age": nbt.Int(int32(n))})
}

// Baby is the form a young animal or a baby zombie is in.
// vanilla: AgeableMob.isBaby, Zombie.isBaby
func (e ageOps) Baby() bool {
	saved := e.e.saved()
	if _, ok := saved["IsBaby"]; ok {
		return nbt.BoolField(saved, "IsBaby")
	}
	n, _ := nbt.IntField(saved, "Age")
	return n < 0
}

// SetBaby ages an animal to -24000 ticks or to adult, as AgeableMob.setBaby does.
// vanilla: AgeableMob.setBaby
func (e ageOps) SetBaby(v bool) {
	if _, ok := e.e.saved()["IsBaby"]; ok {
		e.e.setFlag("IsBaby", v)
		return
	}
	age := int32(0)
	if v {
		age = -24000
	}
	e.e.merge(nbt.Compound{"Age": nbt.Int(age)})
}

func (e loveOps) LoveTicks() int { n, _ := nbt.IntField(e.e.saved(), "InLove"); return int(n) }
func (e loveOps) SetLoveTicks(n int) {
	e.e.merge(nbt.Compound{"InLove": nbt.Int(int32(max(n, 0)))})
}

// Tamed is a horse's Tame flag, else a TamableAnimal's live flag, which an
// owner implies but does not need.
// vanilla: AbstractHorse.isTamed, TamableAnimal.isTame
func (e tameOps) Tamed() bool {
	if _, horse := e.e.saved()["Tame"]; horse {
		return e.e.flag("Tame")
	}
	if m, ok := e.e.mobBody(); ok {
		srv := e.e.host.srv
		srv.mobsMu.Lock()
		defer srv.mobsMu.Unlock()
		return m.mob.Tamable.Tame
	}
	_, ok := e.e.uuidField("Owner")
	return ok
}
func (e tameOps) Owner() (plugin.UUID, bool) { return e.e.uuidField("Owner") }
func (e tameOps) SetOwner(id plugin.UUID) {
	if _, horse := e.e.saved()["Tame"]; horse {
		e.e.setFlag("Tame", id != plugin.UUID{})
	}
	e.e.setUUID("Owner", id)
}

// SetTamed flips the live flag; it is not saved without an owner.
// vanilla: TamableAnimal.setTame
func (e tameOps) SetTamed(v bool) {
	if _, horse := e.e.saved()["Tame"]; horse {
		e.e.setFlag("Tame", v)
		return
	}
	m, ok := e.e.mobBody()
	if !ok {
		return
	}
	srv := e.e.host.srv
	srv.mobsMu.Lock()
	m.mob.SetMetadataTamableTame(v)
	srv.mobsMu.Unlock()
}
func (e tameOps) Sitting() bool { return e.e.flag("Sitting") }

// SetSitting is ignored by a horse, which cannot sit.
func (e tameOps) SetSitting(v bool) {
	if _, horse := e.e.saved()["Tame"]; !horse {
		e.e.setFlag("Sitting", v)
	}
}

func (e orbOps) Value() int {
	n, _ := nbt.ShortField(e.e.saved(), "Value")
	return int(n)
}
func (e orbOps) SetValue(n int) {
	e.e.merge(nbt.Compound{"Value": nbt.Short(clampInt16(max(n, 0)))})
}

// Target is the player the mob hunts; other kinds of target are held by the
// goals themselves.
// vanilla: Mob.getTarget
func (e mobOps) Target() (plugin.LivingEntity, bool) {
	m, ok := e.e.mobBody()
	if !ok {
		return nil, false
	}
	srv := e.e.host.srv
	srv.mobsMu.Lock()
	id, w := m.mob.TargetID, m.w
	srv.mobsMu.Unlock()
	rel := srv.relatedByID(w, id)
	if rel == nil {
		return nil, false
	}
	t, ok := e.e.host.pluginEntityOf(rel).(plugin.LivingEntity)
	return t, ok
}

// SetTarget points a mob at a player, or drops its target for nil; a
// target of another kind is refused, as the goals hunt players only.
// vanilla: Mob.setTarget
func (e mobOps) SetTarget(t plugin.LivingEntity) {
	m, ok := e.e.mobBody()
	if !ok {
		return
	}
	var id int32
	if t != nil {
		b, ok := e.e.host.entityOfHandle(t)
		if !ok {
			return
		}
		if _, isPlayer := b.(*client); !isPlayer {
			return
		}
		id = b.EntityID()
	}
	if srv := e.e.host.srv; srv.targetListened() {
		ev := &plugin.EntityTargetEvent{Entity: pendingMob{id: m.uuid}, Target: t, Reason: plugin.TargetPlugin}
		if t == nil {
			ev.Reason = plugin.TargetForgot
		}
		srv.firePluginEvent(ev)
		if ev.Cancelled() {
			return
		}
	}
	e.e.host.srv.mobsMu.Lock()
	m.mob.SetMetadataTargetID(id)
	e.e.host.srv.mobsMu.Unlock()
}

// MoveTo plans the path now, so the result says whether one exists.
// vanilla: PathNavigation.moveTo
func (e mobOps) MoveTo(pos plugin.Vec3, speed float64) bool {
	m, ok := e.e.mobBody()
	if !ok {
		return false
	}
	e.e.host.srv.mobsMu.Lock()
	defer e.e.host.srv.mobsMu.Unlock()
	return m.mob.PlanMoveTo(m.w, pos.X, pos.Y, pos.Z, speed)
}

func (e mobOps) StopMoving() {
	if m, ok := e.e.mobBody(); ok {
		e.e.host.srv.mobsMu.Lock()
		m.mob.StopMoving()
		e.e.host.srv.mobsMu.Unlock()
	}
}

func (e mobOps) LookAt(pos plugin.Vec3) {
	if m, ok := e.e.mobBody(); ok {
		e.e.host.srv.mobsMu.Lock()
		m.mob.LookAtPoint(pos.X, pos.Y, pos.Z)
		e.e.host.srv.mobsMu.Unlock()
	}
}

// Leash is the entity holding the lead; a knot has no handle.
// vanilla: Leashable.getLeashHolder
func (e mobOps) Leash() (plugin.Entity, bool) {
	c, _ := nbt.CompoundView(e.e.saved()["leash"])
	id, ok := nbt.UUIDField(c, "UUID")
	if !ok {
		return nil, false
	}
	b, found := e.e.host.entityByUUID(id, "")
	if !found {
		return nil, false
	}
	return e.e.host.pluginEntityOf(b), true
}

// SetLeash ties the lead to holder, or cuts it for nil.
// vanilla: Leashable.setLeashedTo, dropLeash
func (e mobOps) SetLeash(holder plugin.Entity) {
	if holder == nil {
		e.e.drop("leash")
		return
	}
	if b, ok := e.e.host.entityOfHandle(holder); ok {
		e.e.merge(nbt.Compound{"leash": nbt.Compound{"UUID": nbt.UUIDToIntArray(b.UUID())}})
	}
}

// IsBreedingItem is whether feeding the stack sends the animal into love.
// vanilla: Animal.isFood
func (e loveOps) IsBreedingItem(s plugin.ItemStack) bool {
	m, ok := e.e.mobBody()
	if !ok {
		return false
	}
	stack, ok := pluginProtocolStack(s)
	if !ok {
		return false
	}
	e.e.host.srv.mobsMu.Lock()
	defer e.e.host.srv.mobsMu.Unlock()
	return m.mob.FedBy(stack)
}
