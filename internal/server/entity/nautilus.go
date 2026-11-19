package entity

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// TypeNautilus is minecraft:nautilus.
const TypeNautilus = registry.EntityTypeNautilus

func init() {
	// vanilla: EntityTypes.NAUTILUS, AbstractNautilus.createAttributes
	kinds[TypeNautilus] = &kind{
		name: "Nautilus", width: 0.875, height: float64(float32(0.95)), eyeHeight: float64(float32(0.2751)),
		babyWidth: 0.4375, babyHeight: float64(float32(0.475)), babyEye: float64(float32(0.13755)),
		babyable: true, maxHealth: 15, speed: 1, attackDamage: 3, knockbackResist: float64(float32(0.3)),
		temptTag: &registry.ItemTagIDNautilusFood, temptPace: 1.3, strollPace: 1,
		ambientInterval: 120,
		// Sounds picks the variant event; a non-empty Ambient lets the idle
		// roll in Mob.baseTick run.
		sounds: MobSounds{Ambient: registry.SoundEntityNautilusAmbient},
	}
	mobTracking[TypeNautilus] = tracking{rangeChunks: 10, updateInterval: 3}
}

type nautilusState struct {
	dashCooldown                   int
	dashing                        bool
	attackCooldown, chargeCooldown int
	angerUntil                     int
	hurtStamp                      int
	angryAt                        int32
	angryUUID                      uuid.UUID
	speed, up                      float64
}

// NautilusFood follows AbstractNautilus.isFood's tame/baby distinction.
func (m *Mob) NautilusFood(stack protocol.ItemStack) bool {
	tag := "minecraft:nautilus_food"
	if !m.Tamable.Tame && !m.IsBaby() {
		tag = "minecraft:nautilus_taming_items"
	}
	return !stack.Empty() && registry.Item(stack.ItemID).InTag(registry.ItemTagOf(tag))
}

// NautilusCanEquip is AbstractNautilus.canUseSlot for body and saddle.
func (m *Mob) NautilusCanEquip() bool {
	return IsNautilus(m.Type) && m.Alive() && !m.IsBaby() && m.Tamable.Tame
}

// TameNautilus attempts the one-in-three fish taming roll without a sit order.
func (m *Mob) TameNautilus(owner uuid.UUID) bool {
	if !m.TryToTame(owner) {
		return false
	}
	m.Tamable.OrderedToSit = false
	return true
}

// FeedNautilus heals an adult before trying the shared animal feeding.
func (m *Mob) FeedNautilus(stack protocol.ItemStack) bool {
	if !m.IsBaby() && m.Health < m.MaxHealth {
		m.feedHeal(stack, 2, 1)
		return true
	}
	return false
}

// NautilusSoundEvent selects one of a nautilus's age and underwater sounds.
type NautilusSoundEvent uint8

const (
	NautilusAmbient NautilusSoundEvent = iota
	NautilusHurt
	NautilusDeath
	NautilusEat
	NautilusDash
	NautilusDashReady
	NautilusSwim
)

// NautilusSound selects the age and underwater variant of a sound.
// vanilla: Nautilus, ZombieNautilus, AbstractNautilus
func (m *Mob) NautilusSound(event NautilusSoundEvent) registry.Sound {
	set := 0
	if m.Type == TypeZombieNautilus {
		set = 1
	}
	if m.IsBaby() && event != NautilusDash && event != NautilusDashReady {
		set = 2
	}
	if m.waterFluid.eyesIn {
		return nautilusSounds[set][event][0]
	}
	return nautilusSounds[set][event][1]
}

var nautilusSounds = [3][7][2]registry.Sound{
	{
		{registry.SoundEntityNautilusAmbient, registry.SoundEntityNautilusAmbientLand},
		{registry.SoundEntityNautilusHurt, registry.SoundEntityNautilusHurtLand},
		{registry.SoundEntityNautilusDeath, registry.SoundEntityNautilusDeathLand},
		{registry.SoundEntityNautilusEat, registry.SoundEntityNautilusEat},
		{registry.SoundEntityNautilusDash, registry.SoundEntityNautilusDashLand},
		{registry.SoundEntityNautilusDashReady, registry.SoundEntityNautilusDashReadyLand},
		{registry.SoundEntityNautilusSwim, registry.SoundEntityNautilusSwim},
	},
	{
		{registry.SoundEntityZombieNautilusAmbient, registry.SoundEntityZombieNautilusAmbientLand},
		{registry.SoundEntityZombieNautilusHurt, registry.SoundEntityZombieNautilusHurtLand},
		{registry.SoundEntityZombieNautilusDeath, registry.SoundEntityZombieNautilusDeathLand},
		{registry.SoundEntityZombieNautilusEat, registry.SoundEntityZombieNautilusEat},
		{registry.SoundEntityZombieNautilusDash, registry.SoundEntityZombieNautilusDashLand},
		{registry.SoundEntityZombieNautilusDashReady, registry.SoundEntityZombieNautilusDashReadyLand},
		{registry.SoundEntityZombieNautilusSwim, registry.SoundEntityZombieNautilusSwim},
	},
	{
		{registry.SoundEntityBabyNautilusAmbient, registry.SoundEntityBabyNautilusAmbientLand},
		{registry.SoundEntityBabyNautilusHurt, registry.SoundEntityBabyNautilusHurtLand},
		{registry.SoundEntityBabyNautilusDeath, registry.SoundEntityBabyNautilusDeathLand},
		{registry.SoundEntityBabyNautilusEat, registry.SoundEntityBabyNautilusEat},
		{registry.SoundEntityNautilusDash, registry.SoundEntityNautilusDashLand},
		{registry.SoundEntityNautilusDashReady, registry.SoundEntityNautilusDashReadyLand},
		{registry.SoundEntityBabyNautilusSwim, registry.SoundEntityBabyNautilusSwim},
	},
}

// NautilusDashing is AbstractNautilus.DASH.
func (m *Mob) NautilusDashing() bool { return m.nautilus.dashing }

func (m *Mob) nautilusStep(t *tickView) {
	n := &m.nautilus
	if n.dashing && n.dashCooldown < 35 {
		m.SetMetadataNautilusDashing(false)
	}
	if n.dashCooldown > 0 {
		n.dashCooldown--
		if n.dashCooldown == 0 {
			t.events = append(t.events, MobEvent{Mob: m, Kind: MobSounded, SoundID: m.NautilusSound(NautilusDashReady), Volume: 1, Pitch: m.VoicePitch()})
		}
	}
	if m.NoAI {
		return
	}
	if n.attackCooldown > 0 {
		n.attackCooldown--
	}
	if n.chargeCooldown > 0 {
		n.chargeCooldown--
	}
	if m.blowAttacker != 0 && (m.lastDamageStamp != n.hurtStamp || n.angryAt != m.blowAttacker) {
		n.hurtStamp, n.angryAt, n.angerUntil = m.lastDamageStamp, m.blowAttacker, m.Age+400
		n.angryUUID = uuid.Nil
		if p := m.targetByID(t, n.angryAt); p != nil {
			n.angryUUID = p.UUID
		}
		if o := t.mobByID(n.angryAt); o != nil && m.NautilusIdentities != nil {
			n.angryUUID = m.NautilusIdentities.MobUUID(o)
		}
	}
	if n.angerUntil <= m.Age {
		n.angryUUID = uuid.Nil
	}
	if n.angerUntil > m.Age && n.angryAt == 0 && n.angryUUID != uuid.Nil {
		for _, p := range t.players {
			if p.UUID == n.angryUUID {
				n.angryAt = p.EntityID
				break
			}
		}
		if m.NautilusIdentities != nil {
			for _, o := range t.others {
				if m.NautilusIdentities.MobUUID(o) == n.angryUUID {
					n.angryAt = o.ID
					break
				}
			}
		}
	}
	if m.Tamable.Tame && m.LeashHolderID == 0 && len(m.PassengerIDs()) == 0 {
		radius := int32(16)
		if !m.IsBaby() && m.Horse.Saddle.Empty() {
			radius = 32
		}
		x, y, z, r, ok := m.HomeRestriction()
		at := m.cell()
		if !ok || r != radius || Dist2(float64(x), float64(y), float64(z), float64(at.X), float64(at.Y), float64(at.Z)) >= float64((radius+8)*(radius+8)) {
			m.RestrictTo(at.X, at.Y, at.Z, radius)
		}
	}
}

func (m *Mob) nautilusAirStep(events []MobEvent) []MobEvent {
	// vanilla: Nautilus.handleAirSupply; rain does not rehydrate it.
	if m.NoAI {
		return events
	}
	if m.InWater() || !m.Alive() {
		m.SetMetadataAirSupply(300)
		return events
	}
	m.SetMetadataAirSupply(m.AirSupply - 1)
	if m.AirSupply <= -20 {
		m.SetMetadataAirSupply(0)
		if landed, soft := m.plainHurt(2, registry.DamageTypeDryOut, false); landed {
			m.noteLastDamage(registry.DamageTypeDryOut)
			events = append(events, MobEvent{Mob: m, Kind: MobBurned, DamageType: registry.DamageTypeDryOut, Softened: soft, NoBubble: true})
		}
	}
	return events
}

func nautilusGoals(m *Mob) []Goal {
	if m.Type == TypeZombieNautilus {
		return []Goal{&temptGoal{m: m, priority: 2, stopAt: 3.5}, &nautilusChargeGoal{m: m}, &swimStrollGoal{m: m, interval: 40}}
	}
	return []Goal{
		// vanilla: NautilusAi.initCoreActivity AnimalPanic(1.6f)
		&panicGoal{m: m, pace: 1.6, brain: true}, &breedGoal{m: m, priority: 1},
		&temptGoal{m: m, priority: 2, stopAt: 3.5},
		&nautilusChargeGoal{m: m}, &swimStrollGoal{m: m, interval: 40},
	}
}

func (m *Mob) nautilusMoveControl(w World) {
	// vanilla: SmoothSwimmingMoveControl(85,10,0.011,0,true)
	if m.InWater() {
		m.VY += 0.005
	}
	m.nautilus.speed, m.nautilus.up = m.smoothSwimMoveControl(w, float64(float32(0.011)), 0)
}

func (m *Mob) nautilusTravel(w World) {
	speed := m.nautilus.speed
	if m.NoAI {
		speed = 0
	}
	m.smoothSwimTravel(w, speed, m.nautilus.up)
}

// NautilusJump starts the client-simulated dash; the client supplies its impulse.
// vanilla: AbstractNautilus.handleStartJump, onSyncedDataUpdated
func (m *Mob) NautilusJump(power int32) bool {
	if power <= 0 || !m.Steered() || m.nautilus.dashCooldown > 0 {
		return false
	}
	m.SetMetadataNautilusDashing(true)
	m.nautilus.dashCooldown = 40
	return true
}

func nautilusSpawnOk(w SpawnWorld, x, y, z int32) bool {
	sea := w.SeaLevel()
	return y >= sea-25 && y <= sea-5 && rules.IsWater(w.BlockAt(x, y, z)) && rules.IsWater(w.BlockAt(x, y-1, z)) && registry.BlockOf(w.BlockAt(x, y+1, z)) == registry.BlockWater
}

// nautilusFields keeps the brain's codec-backed memories across unloading.
// vanilla: Brain.Packed, MemoryModuleType
func (m *Mob) nautilusFields(out nbt.CompoundTag, load bool) {
	tag := out.Entries()
	n := &m.nautilus
	memories := brainMemories(tag)
	saved := m.newMemoryMap()
	for _, key := range []struct {
		name  string
		ticks *int
	}{
		{"minecraft:attack_target_cooldown", &n.attackCooldown},
		{"minecraft:charge_cooldown_ticks", &n.chargeCooldown},
	} {
		if load {
			c, _ := nbt.Get[nbt.Compound](memories, key.name)
			value, _ := nbt.IntValue(c["value"])
			*key.ticks = max(0, int(value))
		} else if *key.ticks > 0 {
			saved.Set(key.name, memoryTag(nbt.Int(*key.ticks)))
		}
	}
	if load {
		anger, _ := nbt.Get[nbt.Compound](memories, "minecraft:angry_at")
		n.angryUUID, _ = nbt.UUIDField(anger, "value")
		ttl, _ := nbt.LongValue(anger["ttl"])
		n.angerUntil, n.angryAt = m.Age+int(max(0, ttl)), 0
		return
	}
	if n.angerUntil > m.Age && n.angryUUID != uuid.Nil {
		saved.Set("minecraft:angry_at", memoryTag(nbt.UUIDToIntArray(n.angryUUID), int64(n.angerUntil-m.Age)))
	}
	out.Set("Brain", brainTag(saved))
}
