package entity

import (
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

type mobFieldPolicy uint16

const (
	mobOmitZero mobFieldPolicy = 1 << iota
	mobKeepAbsent
	mobOnlyTrue
	mobInvert
	mobOmitNonPositive
	mobTemper
	mobDefaultZero
	mobPositive
	mobStrictInt
	mobDefaultTrue
)

type mobCodec struct {
	tag  nbt.Compound
	load bool
	out  nbt.CompoundTag
}

// scalarFields declares each symmetric field once; stateful codecs keep their load order.
func (m *Mob) scalarFields(tag nbt.Compound, load bool) {
	c := mobCodec{tag: tag, load: load}
	m.entityFields(c)
	m.mobFields(c, nil)
	m.kindFields(c, nil)
}

// SaveEntityFields writes the body's Entity fields after its server-owned custom name.
func (m *Mob) SaveEntityFields(tag nbt.CompoundTag) {
	tag.Set("Air", nbt.Short(clampToShort(m.AirSupply)))
	tag.Set("fall_distance", nbt.Double(m.FallDistance))
	m.entityFields(mobCodec{tag: tag.Entries(), out: tag})
}

func (m *Mob) entityFields(c mobCodec) {
	c.flag("Invulnerable", &m.Invulnerable, 0)
	c.flag("Silent", &m.Silent, mobOmitZero)
	c.flag("NoGravity", &m.NoGravity, mobOmitZero)
	c.flag("Glowing", &m.Glowing, mobOmitZero)
	mobInteger(&c, "TicksFrozen", &m.TicksFrozen, mobOmitNonPositive)
	c.flag("HasVisualFire", &m.VisualFire, mobOmitZero)
	// vanilla: Entity.load, saveWithoutId: "data" is written when not empty
	if c.load {
		m.CustomData = nil
		if data, ok := nbt.Get[nbt.Compound](c.tag, "data"); ok && len(data) > 0 {
			m.CustomData = nbt.Copy(data).(nbt.Compound)
		}
	} else if len(m.CustomData) > 0 {
		c.set("data", nbt.Copy(m.CustomData))
	}
}

// vanilla: Mob.addAdditionalSaveData, readAdditionalSaveData
func (m *Mob) mobFields(c mobCodec, leash func()) {
	var pickupPolicy mobFieldPolicy
	if m.isPiglin() {
		// vanilla: AbstractPiglin.readAdditionalSaveData
		pickupPolicy = mobDefaultTrue
	}
	pickup := &m.CanPickUpLoot
	if !c.load {
		// vanilla: Mob.addAdditionalSaveData calls canPickUpLoot, not the raw flag.
		enabled := m.LootPickupEnabled()
		pickup = &enabled
	}
	c.flag("CanPickUpLoot", pickup, pickupPolicy)
	c.flag("PersistenceRequired", &m.Persistence, 0)
	if !c.load {
		if chances := m.DropChancesTag(); len(chances.Entries()) > 0 {
			c.set("drop_chances", chances)
		}
		if leash != nil {
			leash()
		}
		if x, y, z, radius, ok := m.HomeRestriction(); ok {
			c.set("home_radius", nbt.Int(radius))
			c.set("home_pos", nbt.IntArray{x, y, z})
		}
	}
	if c.load && m.Type == TypePillager {
		// vanilla: Pillager.readAdditionalSaveData
		m.CanPickUpLoot = true
	}
	c.flag("LeftHanded", &m.LeftHanded, 0)
	if !c.load {
		if m.DeathLootTable != "" {
			c.set("DeathLootTable", nbt.String(m.DeathLootTable))
		}
		if m.DeathLootTableSeed != 0 {
			c.set("DeathLootTableSeed", nbt.Long(m.DeathLootTableSeed))
		}
	}
	c.flag("NoAI", &m.NoAI, mobOmitZero)
}

func (m *Mob) kindFields(c mobCodec, raider func()) {
	load := c.load
	if m.Type == TypeWolf || m.Type == TypeCat || IsNautilus(m.Type) || m.Type == TypeParrot {
		// vanilla: TamableAnimal.addAdditionalSaveData, readAdditionalSaveData
		if !load && m.Tamable.Owner != uuid.Nil {
			c.set("Owner", nbt.UUIDToIntArray(m.Tamable.Owner))
		}
		c.flag("Sitting", &m.Tamable.OrderedToSit, 0)
		if load {
			m.Tamable.SittingPose = m.Tamable.OrderedToSit
		}
	}
	if isRaiderType(m.Type) {
		c.optionalPos("patrol_target", &m.PatrolTarget)
		c.flag("PatrolLeader", &m.PatrolLeader, 0)
		c.flag("Patrolling", &m.Patrolling, 0)
		if !load && raider != nil {
			raider()
		}
	}
	switch m.Type {
	case TypeSulfurCube:
		mobInteger(&c, "pickup_timer", &m.sulfur.pickupTimer, 0)
		if !load {
			c.set("from_bucket", boolTag(m.FromBucket))
		}
		fuse := -1
		if m.sulfur.primed {
			fuse = m.sulfur.fuse
		}
		mobInteger(&c, "fuse", &fuse, 0)
		m.sulfur.primed, m.sulfur.fuse = fuse >= 0, max(fuse, 0)
		if c.load && fuse >= 0 {
			m.sulfur.maxFuse = fuse // vanilla: readAdditionalSaveData sets MAX_FUSE
		}
	case TypeDolphin:
		if load {
			m.dolphin.moistness = dolphinMaxMoistness
		}
		c.flag("GotFish", &m.dolphin.gotFish, 0)
		mobInteger(&c, "Moistness", &m.dolphin.moistness, 0)
	case TypeSheep:
		c.flag("Sheared", &m.Sheared, mobKeepAbsent)
	case TypeCamel, TypeCamelHusk, TypeHorse, TypeDonkey, TypeMule, TypeSkeletonHorse, TypeZombieHorse, TypeLlama, TypeTraderLlama:
		c.flag("EatingHaystack", &m.Horse.Eating, 0)
		c.flag("Bred", &m.Horse.Bred, 0)
		mobInteger(&c, "Temper", &m.Horse.Temper, mobTemper)
		// vanilla: Camel.isTamed always returns true, even after loading Tame:0b.
		tame := m.Horse.Tame || IsCamel(m.Type)
		c.flag("Tame", &tame, 0)
		if load {
			m.Horse.Tame = tame || IsCamel(m.Type)
		}
		if !load && m.Horse.Owner != uuid.Nil {
			c.set("Owner", nbt.UUIDToIntArray(m.Horse.Owner))
		}
		if m.Chested() {
			c.flag("ChestedHorse", &m.Horse.Chest, 0)
		}
		if !load {
			registry.PutSlottedItems(c.output(), m.Horse.Items, false)
		}
		switch {
		case IsCamel(m.Type):
			m.camelFields(&c)
		case IsLlama(m.Type):
			// vanilla: Llama.addAdditionalSaveData; LoadFrom clamps both
			mobInteger(&c, "Variant", &m.Horse.Variant, 0)
			mobInteger(&c, "Strength", &m.Horse.Strength, 0)
		case m.Type == TypeHorse:
			mobInteger(&c, "Variant", &m.Horse.Variant, 0)
		case m.Type == TypeSkeletonHorse:
			// vanilla: SkeletonHorse.addAdditionalSaveData
			c.flag("SkeletonTrap", &m.Horse.Trap, 0)
			mobInteger(&c, "SkeletonTrapTime", &m.Horse.TrapTime, 0)
		}
		if m.Type == TypeTraderLlama {
			// vanilla: TraderLlama.addAdditionalSaveData
			mobInteger(&c, "DespawnDelay", &m.DespawnDelay, 0)
		}
	case TypeWither:
		mobInteger(&c, "Invul", &m.wither.invul, 0)
	case TypeHoglin:
		c.flag("IsImmuneToZombification", &m.ZombificationImmune, 0)
		mobInteger(&c, "TimeInOverworld", &m.hoglin.inOverworld, mobDefaultZero)
		c.flag("CannotBeHunted", &m.hoglin.CannotBeHunted, 0)
	case TypePiglin, TypePiglinBrute:
		c.flag("IsImmuneToZombification", &m.barter.Immune, 0)
		mobInteger(&c, "TimeInOverworld", &m.barter.inOverworld, mobDefaultZero)
		if m.Type == TypePiglin {
			if !load {
				c.set("IsBaby", boolTag(m.IsBaby()))
			}
			c.flag("CannotHunt", &m.barter.CannotHunt, 0)
		}
		m.piglinBrainFields(c.output(), load)
	case TypeGoat:
		c.flag("IsScreamingGoat", &m.Goat.Screaming, 0)
		m.goatBrainFields(c.output(), c.load)
		c.flag("HasLeftHorn", &m.Goat.NoLeftHorn, mobInvert|mobKeepAbsent)
		c.flag("HasRightHorn", &m.Goat.NoRightHorn, mobInvert|mobKeepAbsent)
	case TypeFox:
		if !load {
			trusted := nbt.List{Elem: nbt.TypeIntArray}
			for _, id := range m.Fox.Trusted {
				trusted.Items = append(trusted.Items, nbt.UUIDToIntArray(id))
			}
			c.set("Trusted", trusted)
		}
		c.flag("Sleeping", &m.Fox.Sleeping, 0)
		if !load {
			c.set("Type", nbt.String(FoxVariantName(m.Fox.Variant)))
		}
		c.flag("Sitting", &m.Fox.Sitting, 0)
		c.flag("Crouching", &m.Fox.Crouching, 0)
	case TypeOcelot:
		c.flag("Trusting", &m.Trusting, 0)
	case TypeArmadillo:
		m.armadilloFields(&c)
	case TypeAllay:
		m.allayFields(&c)
	case TypeHappyGhast:
		// vanilla: HappyGhast.addAdditionalSaveData, readAdditionalSaveData
		mobInteger(&c, "still_timeout", &m.stillTimeout, mobDefaultZero)
	case TypeCopperGolem:
		m.copperGolemFields(&c)
	case TypeChicken:
		c.flag("IsChickenJockey", &m.ChickenJockey, mobOnlyTrue)
		mobInteger(&c, "EggLayTime", &m.EggTimer, mobPositive)
	case TypeFrog:
		m.frogBrainFields(c.output(), c.load)
	case TypeTurtle:
		if !load {
			c.set("home_pos", nbt.IntArray{m.Home.X, m.Home.Y, m.Home.Z})
		}
		c.flag("has_egg", &m.HasEgg, 0)
	case TypeBogged:
		c.flag("sheared", &m.Sheared, 0)
	case TypeSnowGolem:
		c.flag("Pumpkin", &m.Sheared, mobInvert|mobKeepAbsent)
	case TypeIronGolem:
		c.flag("PlayerCreated", &m.PlayerCreated, 0)
	case TypeCreeper:
		c.flag("powered", &m.Powered, 0)
	case TypeRabbit:
		if !load {
			c.set("RabbitType", nbt.Int(m.RabbitType))
		}
		mobInteger(&c, "MoreCarrotTicks", &m.MoreCarrotTicks, 0)
	case TypeBat:
		// The whole DATA_ID_FLAGS byte, resting in bit 0x01; absent loads 0.
		// vanilla: Bat.readAdditionalSaveData
		if c.load {
			n, _ := nbt.ByteValue(c.tag["BatFlags"])
			m.Resting = n&1 != 0
		} else {
			c.flag("BatFlags", &m.Resting, 0)
		}
	case TypeGlowSquid:
		mobInteger(&c, "DarkTicksRemaining", &m.DarkTicks, mobDefaultZero)
	case TypeEndermite:
		mobInteger(&c, "Lifetime", &m.Life, mobDefaultZero)
	case TypeVillager:
		mobInteger(&c, "Xp", &m.VillagerXp, mobStrictInt)
	case TypeWanderingTrader:
		// vanilla: WanderingTrader.addAdditionalSaveData
		mobInteger(&c, "DespawnDelay", &m.DespawnDelay, mobDefaultZero)
		c.optionalPos("wander_target", &m.WanderTarget)
	case TypeWarden:
		m.wardenFields(c.output(), load)
	case TypeShulker:
		m.shulkerFields(c.output(), load)
	}
}

// optionalPos is a nullable BlockPos.CODEC field, an int array of three,
// absent for nil.
func (c *mobCodec) optionalPos(key string, v **Waypoint) {
	if !c.load {
		if *v != nil {
			c.set(key, nbt.IntArray{(*v).X, (*v).Y, (*v).Z})
		}
		return
	}
	*v = nil
	if p, ok := nbt.Get[nbt.IntArray](c.tag, key); ok && len(p) == 3 {
		*v = &Waypoint{p[0], p[1], p[2]}
	}
}

func (c *mobCodec) flag(key string, v *bool, policy mobFieldPolicy) {
	if c.load {
		n, ok := nbt.ByteValue(c.tag[key])
		if !ok && policy&mobKeepAbsent != 0 || n == 0 && policy&mobOnlyTrue != 0 {
			return
		}
		b := n != 0 || !ok && policy&mobDefaultTrue != 0
		if policy&mobInvert != 0 {
			b = !b
		}
		*v = b
	} else {
		b := *v
		if policy&mobInvert != 0 {
			b = !b
		}
		if b || policy&mobOmitZero == 0 {
			c.set(key, boolTag(b))
		}
	}
}

func mobInteger[T ~int | ~int32](c *mobCodec, key string, v *T, policy mobFieldPolicy) {
	if c.load {
		n, ok := nbt.IntField(c.tag, key)
		if policy&mobStrictInt != 0 {
			raw, exact := c.tag[key].(nbt.Int)
			n, ok = int32(raw), exact
		}
		if !ok && policy&mobDefaultZero == 0 || n <= 0 && policy&mobPositive != 0 {
			return
		}
		if policy&mobTemper != 0 {
			n = max(0, min(n, HorseMaxTemper))
		}
		*v = T(n)
	} else if policy&mobOmitNonPositive == 0 || *v > 0 {
		c.set(key, nbt.Int(int32(*v)))
	}
}

// set lets the shared scalar codecs write either compound representation.
func (c *mobCodec) set(key string, tag nbt.Tag) {
	if c.out != nil {
		c.out.Set(key, tag)
	} else {
		c.tag[key] = tag
	}
}

func (c *mobCodec) output() nbt.CompoundTag {
	if c.out != nil {
		return c.out
	}
	return c.tag
}
