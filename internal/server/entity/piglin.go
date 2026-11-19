package entity

import (
	"math"
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The piglin and piglin brute. The piglin hunts players without
// piglin-safe armour and barters for gold; the brute hunts every player. The
// hoglin hunt and retreat are in piglinhunt.go.
// vanilla: Piglin, PiglinBrute, PiglinAi, PiglinBruteAi, AbstractPiglin

const (
	TypePiglin      = registry.EntityTypePiglin
	TypePiglinBrute = registry.EntityTypePiglinBrute
)

func init() {
	// vanilla: EntityTypes.PIGLIN and Piglin.createAttributes
	kinds[TypePiglin] = &kind{
		fireAverse: true,
		name:       "Piglin", width: 0.6, height: 1.95, eyeHeight: 1.79,
		babyWidth: 0.49, babyHeight: 0.98, babyEye: 0.78, babyable: true,
		maxHealth: 16, speed: float64(float32(0.35)), hostile: true, livesInPeaceful: true, attackDamage: 5,
		sounds: MobSounds{
			Ambient: registry.SoundEntityPiglinAmbient,
			Hurt:    registry.SoundEntityPiglinHurt,
			Death:   registry.SoundEntityPiglinDeath,
			Step:    registry.SoundEntityPiglinStep,
		},
	}
	// vanilla: EntityTypes.PIGLIN_BRUTE and PiglinBrute.createAttributes
	kinds[TypePiglinBrute] = &kind{
		fireAverse: true,
		name:       "Piglin Brute", width: 0.6, height: 1.95, eyeHeight: 1.79,
		maxHealth: 50, speed: float64(float32(0.35)), hostile: true, attackDamage: 7, followRange: 12,
		sounds: MobSounds{
			Ambient: registry.SoundEntityPiglinBruteAmbient,
			Hurt:    registry.SoundEntityPiglinBruteHurt,
			Death:   registry.SoundEntityPiglinBruteDeath,
			Step:    registry.SoundEntityPiglinBruteStep,
		},
	}
	// vanilla: EntityTypes.PIGLIN, PIGLIN_BRUTE clientTrackingRange(8)
	mobTracking[TypePiglin] = tracking{rangeChunks: 8, updateInterval: 3}
	mobTracking[TypePiglinBrute] = tracking{rangeChunks: 8, updateInterval: 3}
}

const (
	// piglinAdmireTicks is the ADMIRING_ITEM memory's expiry.
	// vanilla: PiglinAi.admireGoldItem
	piglinAdmireTicks = 119
	// piglinWalkRange is how near a loved item must be to walk to it, and
	// piglinReachTicks how long the walk may last before it is given up for as
	// long again; piglinScanRange and piglinScanTicks are the sensor's.
	piglinWalkRange  = 9
	piglinReachTicks = 200
	piglinScanRange  = 32
	piglinScanTicks  = 20
	// piglinZombifyAfter is the ticks in a dimension that is not piglin_safe
	// after which the conversion runs; it needs more than this many.
	// vanilla: AbstractPiglin.customServerAiStep
	piglinZombifyAfter = 300
	// piglinThrowSpeed and piglinThrowDrop are the barter throw's velocity per
	// axis and how far under the eyes the item leaves the hand.
	// vanilla: BehaviorUtils.throwItem
	piglinThrowSpeed = 0.3
	piglinThrowDrop  = 0.3
	// piglinThrowRange is the reach of the nearest player the barter is
	// thrown to; a piglin sees no further than its follow range.
	piglinThrowRange = 16.0
	// piglinArmourOdds is the chance of each golden armour piece at spawn.
	// vanilla: Piglin.maybeWearArmor
	piglinArmourOdds = 0.1
	// piglinBabyOdds is the chance of a baby at spawn.
	// vanilla: Piglin.PROBABILITY_OF_SPAWNING_AS_BABY
	piglinBabyOdds = 0.2
	// piglinAdmireOffTicks is how long a player's blow disables admiring.
	// vanilla: PiglinAi.HIT_BY_PLAYER_MEMORY_TIMEOUT
	piglinAdmireOffTicks = 400
)

// barterState is the piglins' bartering and conversion counters.
type barterState struct {
	// admiring counts the ticks left of the gold the piglin holds up in its
	// off hand; 0 when it holds none.
	admiring int
	// wanted is NEAREST_VISIBLE_WANTED_ITEM, the item the last sensor scan
	// found; hasWanted is false when it found none.
	wanted    LootItem
	hasWanted bool
	// scanIn counts down to the next sensor scan.
	scanIn int
	// reachTime is TIME_TRYING_TO_REACH_ADMIRE_ITEM and walkOff counts down
	// DISABLE_WALK_TO_ADMIRE_ITEM.
	reachTime, walkOff int
	// ate counts down ATE_RECENTLY; while it runs the piglin takes no food.
	ate int
	// hurtBy is the attacker queued for the next step; admireOff counts down
	// ADMIRING_DISABLED.
	hurtBy    int32
	admireOff int
	// inOverworld is AbstractPiglin.timeInOverworld.
	inOverworld int
	// Immune is IsImmuneToZombification.
	Immune bool
	// CannotHunt keeps a piglin from hunting hoglins.
	// vanilla: Piglin.cannotHunt
	CannotHunt bool
	// home is the brute's HOME memory; nil when it has none.
	home *GlobalPos
	// Inventory is the eight-slot store; see piglininv.go.
	Inventory [piglinInventorySize]protocol.ItemStack
	huntState
}

var (
	goldenArmourItems = [4]registry.Item{
		wornFeet: registry.ItemGoldenBoots, wornLegs: registry.ItemGoldenLeggings,
		wornChest: registry.ItemGoldenChestplate, wornHead: registry.ItemGoldenHelmet,
	}
)

func (m *Mob) isPiglin() bool { return piglinKind(m.Type) }

// piglinIdle reports whether no higher-priority activity is active.
// vanilla: AbstractPiglin.playAmbientSound, PiglinAi.isIdle
func (m *Mob) piglinIdle() bool {
	b := &m.barter
	return m.TargetID == 0 && b.admiring == 0 && b.retreat == 0 && b.celebrate == 0 && b.ride == 0
}

func piglinKind(t MobType) bool { return t == TypePiglin || t == TypePiglinBrute }

// piglinSafeArmour is PiglinAi.isWearingSafeArmor: any worn piece in the
// minecraft:piglin_safe_armor tag.
func piglinSafeArmour(p *Player) bool {
	for _, piece := range p.Worn {
		if !piece.Empty() && registry.Item(piece.ItemID).InTag(registry.ItemTagIDPiglinSafeArmor) {
			return true
		}
	}
	return false
}

// piglinMindGoals stands in for the brain's fight and idle activities:
// admiring first, then grudge, the nearest player it will not leave alone,
// the swing and idle looks.
// vanilla: PiglinAi.findNearestValidAttackTarget, PiglinBruteAi
func piglinMindGoals(m *Mob) []Goal {
	m.CanPickUpLoot = true
	accept := func(t *tickView, p *Player) bool {
		return !m.IsBaby() && (m.Type == TypePiglinBrute || !piglinSafeArmour(p))
	}
	var stroll Goal = &randomStroll{m: m, priority: hostileStroll}
	if m.Type == TypePiglinBrute {
		// vanilla: PiglinBruteAi.createIdleMovementBehaviors
		home := func(t *tickView) (Waypoint, bool) {
			g := m.barter.home
			if g == nil || g.Dim != t.w.DimensionName() {
				return Waypoint{}, false
			}
			return g.at(), true
		}
		// The walk home stops within 2 blocks.
		// vanilla: PiglinBruteAi.HOME_CLOSE_ENOUGH_DISTANCE
		stroll = &runOneGoal{m: m, priority: hostileStroll, choices: []Goal{
			&randomStroll{m: m, priority: hostileStroll, pace: 0.6},
			&poiStrollGoal{m: m, poi: home, pace: 0.6, reach: 2, maxDist: 100, cooldown: 80, toPoi: true},
			&poiStrollGoal{m: m, poi: home, pace: 0.6, reach: navReach, maxDist: 5, cooldown: 180},
		}}
	}
	goals := []Goal{
		&admireGoal{m: m},
		&retreatGoal{m: m},
		// vanilla: PiglinAi.wasHurtBy ignores a piglin's blow,
		// PiglinBruteAi.wasHurtBy any piglin's
		&piglinHurtByGoal{&hurtByGoal{
			m: m, priority: priorityHurtBy, brain: true,
			ignore: func(t MobType) bool {
				return t == TypePiglin || m.Type == TypePiglinBrute && t == TypePiglinBrute
			},
		}},
		&huntHoglinGoal{m: m, priority: priorityNearest},
		&huntNemesisGoal{m: m, priority: priorityNearest},
		&nearestTargetGoal{m: m, priority: priorityNearest, accept: accept, recheck: true,
			gate: func(t *tickView) bool { return !m.nearZombified(t) }},
		newPiglinCrossbowGoal(m, priorityMelee),
		&spearGoal{m: m, priority: priorityMelee},
		// vanilla: MeleeAttack is not run with a usable crossbow or spear,
		// Piglin.canUseNonMeleeWeapon
		&meleeAttackGoal{m: m, priority: priorityMelee, brain: true, refuse: func() bool {
			return m.MainHand.ItemID == int32(registry.ItemCrossbow) && !m.MainHand.Empty() || m.holdsSpear()
		}},
		// vanilla: PiglinAi.initCoreActivity, PiglinBruteAi.initCoreActivity (InteractWithDoor)
		newInteractWithDoor(m),
		&celebrateGoal{m: m},
		&rideGoal{m: m},
		stroll,
		&lookAtPlayer{m: m, priority: hostileLookAt},
		&randomLookAround{m: m, priority: hostileLookAround},
	}
	if m.Type != TypePiglin {
		return goals
	}
	// vanilla: PiglinAi.initIdleActivity, initCelebrateActivity, initRideHoglinActivity
	look := &gatedGoal{
		Goal: &lookAtPlayer{m: m, priority: hostileLookAt - 1, reach: 14, chance: 1, brain: true,
			accept: func(p *Player) bool {
				return isPlayerHoldingLovedItem(p) && (m.barter.ride == 0 || m.celebrating() || m.distanceToPlayer(p) <= 8)
			}},
		enabled: func() bool { return m.TargetID == 0 && m.barter.admiring == 0 && m.barter.retreat == 0 },
	}
	i := slices.IndexFunc(goals, func(g Goal) bool { _, ok := g.(*lookAtPlayer); return ok })
	goals = slices.Insert(goals, i, Goal(look))
	// vanilla: PiglinAi.avoidRepellent is in the idle and celebrate
	// activities only, and does nothing while a walk target exists.
	i = slices.IndexFunc(goals, func(g Goal) bool { _, ok := g.(*celebrateGoal); return ok })
	repel := &hoglinRepelGoal{m: m, priority: priorityCelebrate - 1, pace: 1.0,
		source: func() (Waypoint, bool) { return m.barter.repellent, m.barter.repelSeen },
		idle: func() bool {
			return m.TargetID == 0 && m.barter.admiring == 0 && m.barter.retreat == 0 && m.barter.ride == 0 &&
				len(m.navOf().nav.path) == 0
		}}
	return slices.Insert(goals, i, Goal(repel))
}

// admireGoal holds the piglin still while it admires its gold.
// vanilla: Activity.ADMIRE_ITEM outranks Activity.FIGHT in PiglinAi.updateActivity
type admireGoal struct{ m *Mob }

func (g *admireGoal) Priority() int                 { return priorityFloat }
func (g *admireGoal) Flags() GoalFlags              { return GoalMove | GoalLook | GoalJump }
func (g *admireGoal) CanStart(*tickView) bool       { return g.m.barter.admiring > 0 }
func (g *admireGoal) Start(*tickView)               { g.m.stopSeeking() }
func (g *admireGoal) ShouldContinue(*tickView) bool { return g.m.barter.admiring > 0 }
func (g *admireGoal) Stop(*tickView)                {}
func (g *admireGoal) RequiresEveryTick() bool       { return true }

// Tick walks an admiring piglin with an empty off hand to the loved item it
// saw, giving up when the item is out of reach range or the walk drags on.
// vanilla: GoToWantedItem, StopAdmiringIfItemTooFarAway, StopAdmiringIfTiredOfTryingToReachItem
func (g *admireGoal) Tick(t *tickView) {
	m := g.m
	b := &m.barter
	if !m.OffHand.Empty() {
		return
	}
	if !b.hasWanted || sqDist3(m.X, m.Y, m.Z, b.wanted.X, b.wanted.Y, b.wanted.Z) >= piglinWalkRange*piglinWalkRange {
		b.admiring, b.reachTime = 0, 0
		return
	}
	if b.reachTime > piglinReachTicks {
		b.admiring, b.reachTime, b.walkOff = 0, 0, piglinReachTicks
		return
	}
	b.reachTime++
	m.seekTo(t.w, b.wanted.X, b.wanted.Y, b.wanted.Z, 1, 0)
}

// startWalkAdmire is StartAdmiringItemIfSeen: a loved item in sight makes the
// piglin admire it, which walks it over.
func (m *Mob) startWalkAdmire() {
	b := &m.barter
	if b.walkOff > 0 {
		b.walkOff--
	}
	if m.Type == TypePiglin && b.admiring == 0 && b.walkOff == 0 && b.admireOff == 0 && b.hasWanted && isLoved(b.wanted.Stack) {
		b.admiring = piglinAdmireTicks
	}
}

// SenseWantedItems is NearestItemSensor for piglins and villagers: every twenty ticks each
// finds the nearest item it would pick up, within 32 blocks and 16 vertically,
// that it has line of sight to.
// vanilla: NearestItemSensor.doTick
func (mobs Mobs) SenseWantedItems(w World, items []LootItem) {
	for _, m := range mobs {
		if m.Type == TypeSulfurCube {
			m.senseSulfurItems(items)
			continue
		}
		if m.Type == TypeAllay {
			// vanilla: Allay.wantsToPickUp holds regardless of the pickup cooldown
			if !w.MobGriefing() || !m.Alive() || !m.AllayHolding() {
				m.barter.hasWanted = false
				continue
			}
		} else if !w.MobGriefing() || m.Type != TypePiglin && m.Type != TypeVillager || !m.Alive() || !m.CanPickUpLoot {
			continue
		}
		b := &m.barter
		if b.scanIn--; b.scanIn > 0 {
			continue
		}
		b.scanIn = piglinScanTicks
		b.hasWanted = false
		staged := stagedSlots{main: m.MainHand, off: m.OffHand, worn: m.Worn}
		best := float64(piglinScanRange * piglinScanRange)
		for _, it := range items {
			dx, dy, dz := it.X-m.X, it.Y-m.Y, it.Z-m.Z
			if it.Stack.Empty() || dx > 32 || dx < -32 || dy > 16 || dy < -16 || dz > 32 || dz < -32 {
				continue
			}
			d := dx*dx + dy*dy + dz*dz
			if d >= best || !rayClear(w, m.X, m.Y+m.eyeHeight(), m.Z, it.X, it.Y+itemEyeHeight, it.Z) {
				continue
			}
			if _, _, ok := staged.take(m, it.Stack); ok {
				best, b.wanted, b.hasWanted = d, it, true
			}
		}
	}
}

// InitBruteHome sets a brute's HOME to its block position in dim.
// vanilla: PiglinBruteAi.initMemories
func (m *Mob) InitBruteHome(dim string) {
	if m.Type == TypePiglinBrute {
		m.barter.home = &GlobalPos{dim, rules.BlockOf(m.X), rules.BlockOf(m.Y), rules.BlockOf(m.Z)}
	}
}

// piglinBrainFields saves and loads the piglin's HUNTED_RECENTLY and the
// brute's HOME memory.
// vanilla: Brain.serialize
func (m *Mob) piglinBrainFields(out nbt.CompoundTag, load bool) {
	tag := out.Entries()
	b := &m.barter
	if load {
		memories := brainMemories(tag)
		b.home, b.huntRest = nil, 0
		if m.Type == TypePiglinBrute {
			b.home = brainGlobalPos(memories, "minecraft:home")
			return
		}
		hunted, _ := nbt.Get[nbt.Compound](memories, "minecraft:hunted_recently")
		if nbt.BoolField(hunted, "value") {
			ttl, _ := nbt.LongValue(hunted["ttl"])
			b.huntRest = int(max(ttl, 0))
		}
		return
	}
	memories := m.newMemoryMap()
	if m.Type == TypePiglinBrute {
		putBrainGlobalPos(memories, "minecraft:home", b.home)
	}
	if b.huntRest > 0 && m.Type == TypePiglin {
		memories.Set("minecraft:hunted_recently", memoryTag(nbt.Byte(1), int64(b.huntRest)))
	}
	out.Set("Brain", brainTag(memories))
}

// finalizePiglin equips a spawned piglin (golden armour on a tenth of the
// pieces, createSpawnWeapon's weapon) or brute (golden axe). A crossbow or
// spear in hand only melee-hits.
// vanilla: Piglin.finalizeSpawn, Piglin.populateDefaultEquipmentSlots,
// PiglinBrute.populateDefaultEquipmentSlots
func (m *Mob) finalizePiglin(dice Dice, local rules.LocalDifficulty, structure bool) {
	if m.Type == TypePiglinBrute {
		m.SetEquipment(protocol.EquipMainHand, protocol.ItemStack{ItemID: int32(registry.ItemGoldenAxe), Count: 1})
		return
	}
	if structure {
		// vanilla: Piglin.finalizeSpawn(STRUCTURE) keeps the template's age
		// and weapon and draws PiglinAi.initMemories, then the armour, then
		// the enchantments from the region's random.
		m.barter.huntRest = piglinHuntRestMin + int(dice.NextIntN(piglinHuntRestMax-piglinHuntRestMin+1))
		m.wearPiglinArmour(dice.NextFloat)
		m.rollSpawnEnchantments(dice, local)
		return
	}
	rnd := m.equipmentRand()
	// vanilla: Piglin.finalizeSpawn: a baby rolls no weapon and wears no armour
	if rnd.Float32() < piglinBabyOdds {
		m.SetBaby(true)
	} else {
		// vanilla: Piglin.createSpawnWeapon
		weapon := int32(registry.ItemGoldenSword)
		if rnd.Float64() < 0.5 {
			weapon = int32(registry.ItemCrossbow)
		} else if rnd.Intn(10) == 0 {
			weapon = int32(registry.ItemGoldenSpear)
		}
		m.SetEquipment(protocol.EquipMainHand, protocol.ItemStack{ItemID: weapon, Count: 1})
	}
	m.wearPiglinArmour(rnd.Float32)
	m.rollSpawnEnchantments(GoDice{rnd}, local)
	// vanilla: PiglinAi.initMemories; the dice keep the mob's own stream where
	// it was
	m.barter.huntRest = piglinHuntRestMin + rnd.Intn(piglinHuntRestMax-piglinHuntRestMin+1)
}

// wearPiglinArmour rolls each golden armour piece, head to feet, for an adult.
// vanilla: Piglin.populateDefaultEquipmentSlots, maybeWearArmor
func (m *Mob) wearPiglinArmour(roll func() float32) {
	if m.IsBaby() {
		return
	}
	for _, slot := range populationOrder {
		if roll() < piglinArmourOdds {
			m.SetEquipment(protocol.EquipFeet+slot, protocol.ItemStack{ItemID: int32(goldenArmourItems[slot]), Count: 1})
		}
	}
}

// takePiglin is Piglin.wantsToPickup for an item lying within reach: food
// is eaten (one item) while the piglin has not eaten recently, a barter ingot or loved item goes into the off hand to be admired while no
// loved item is held there, a gold nugget goes whole into the inventory, and
// anything else is worn, one item, in its own slot when it beats what is
// there.
// vanilla: PiglinAi.wantsToPickup, pickUpItem, isBarterCurrency,
// Mob.equipItemIfPossible
func (s *stagedSlots) takePiglin(m *Mob, stack protocol.ItemStack) (slot string, count int32, ok bool) {
	if m.Type != TypePiglin || !m.Alive() || m.barter.admireOff > 0 && m.TargetID != 0 ||
		registry.Item(stack.ItemID).InTag(registry.ItemTagIDPiglinRepellents) ||
		m.IsBaby() && registry.Item(stack.ItemID).InTag(registry.ItemTagIDIgnoredByPiglinBabies) {
		return "", 0, false
	}
	if stack.ItemID == int32(registry.ItemGoldIngot) {
		// vanilla: PiglinAi.isBarterCurrency, isNotHoldingLovedItemInOffHand
		if isLoved(s.off) {
			return "", 0, false
		}
		s.off = stack
		s.off.Count = 1
		return "offhand", 1, true
	}
	if registry.Item(stack.ItemID) == registry.ItemGoldNugget {
		return "inventory", stack.Count, m.canAddToInventory(stack)
	}
	if registry.Item(stack.ItemID).InTag(registry.ItemTagIDPiglinFood) {
		return "eat", 1, m.barter.ate == 0 && m.canAddToInventory(stack)
	}
	if isLoved(stack) {
		if isLoved(s.off) || !m.canAddToInventory(stack) {
			return "", 0, false
		}
		s.off = stack
		s.off.Count = 1
		return "offhand", 1, true
	}
	slot = lootSlotOf(stack)
	if !replaceLoot(m, stack, s.currentOf(slot), slot) {
		return "", 0, false
	}
	if !equippableInMobSlot(stack, slot) {
		// A body piece or saddle beats the piglin's always-empty slot but
		// cannot be worn, so it goes to the inventory. Unlike vanilla, a full
		// inventory leaves it lying instead of taking and throwing it.
		return "inventory", 1, m.canAddToInventory(stack)
	}
	equipped := stack
	equipped.Count = 1
	s.set(slot, equipped)
	return slot, 1, true
}

// PiglinImmune reports the piglin's IsImmuneToZombification flag.
func (m *Mob) PiglinImmune() bool { return m.isPiglin() && m.barter.Immune }

func (m *Mob) canBarter() bool { return m.Type == TypePiglin && m.Alive() && !m.IsBaby() }

// AdmireGold is PiglinAi.mobInteract with a held gold ingot: the piglin takes
// one into its off hand and admires it. It reports whether it took one, for
// the caller to spend.
func (m *Mob) AdmireGold(stack protocol.ItemStack) bool {
	if stack.ItemID != int32(registry.ItemGoldIngot) || !m.canBarter() || m.barter.admireOff > 0 || m.barter.admiring > 0 {
		return false
	}
	// vanilla: PiglinAi.holdInOffhand, Entity.spawnAtLocation
	if !m.OffHand.Empty() {
		vx, vy, vz := PopVelocity(m.Rand)
		m.queued = append(m.queued, MobEvent{
			Mob: m, Kind: MobThrewItem, Stack: m.OffHand, X: m.X, Y: m.Y, Z: m.Z,
			VX: vx, VY: vy, VZ: vz,
		})
	}
	m.SetEquipment(protocol.EquipOffHand, protocol.ItemStack{ItemID: int32(registry.ItemGoldIngot), Count: 1})
	m.DropChances[1] = guaranteedDropChance
	m.barter.admiring = piglinAdmireTicks
	return true
}

// piglinStep runs the admiring countdown (bartering at its end) and the
// zombification count outside the Nether. It returns the type to convert
// into, or "".
// vanilla: StopHoldingItemIfNoLongerAdmiring, AbstractPiglin.customServerAiStep
func (m *Mob) piglinStep(t *tickView) MobType {
	if m.barter.provoker != 0 {
		m.takeProvocation(t)
	}
	if m.barter.ate > 0 {
		m.barter.ate--
	}
	if m.barter.admireOff > 0 {
		m.barter.admireOff--
	}
	m.celebrateStep(t)
	if m.Type == TypePiglin && (m.Age+int(m.ID))%piglinSenseInterval == 0 {
		m.barter.repellent, m.barter.repelSeen = nearestRepellent(t.w, m.cell(), isPiglinRepellent)
	}
	m.piglinSounds(t)
	if id := m.barter.hurtBy; id != 0 {
		m.barter.hurtBy = 0
		m.piglinWasHurt(t, id)
	}
	m.startWalkAdmire()
	if m.barter.admiring > 0 {
		m.barter.admiring--
		if m.barter.admiring == 0 {
			m.finishAdmiring(t, true)
		}
	}
	if !t.w.PiglinsZombify() || m.barter.Immune {
		m.barter.inOverworld = 0
		return NoMobType
	}
	m.barter.inOverworld++
	if m.barter.inOverworld > piglinZombifyAfter && m.mayConvert(TypeZombifiedPiglin) {
		m.dropAdmired(t)
		for _, stack := range m.inventoryDrops() {
			m.throwOnConversion(t, stack)
		}
		return TypeZombifiedPiglin
	}
	return NoMobType
}

// dropAdmired lets go of the ingot held up when the conversion runs.
// vanilla: PiglinAi.cancelAdmiring
func (m *Mob) dropAdmired(t *tickView) {
	if m.barter.admiring == 0 || m.OffHand.Empty() {
		return
	}
	m.throwOnConversion(t, m.OffHand)
	m.SetEquipment(protocol.EquipOffHand, protocol.ItemStack{})
	m.barter.admiring = 0
}

// throwOnConversion drops stack where the converting piglin stands.
// vanilla: Piglin.finishConversion, Entity.spawnAtLocation
func (m *Mob) throwOnConversion(t *tickView, stack protocol.ItemStack) {
	t.events = append(t.events, MobEvent{
		Mob: m, Kind: MobThrewItem, Stack: stack, X: m.X, Y: m.Y, Z: m.Z,
		VX: m.Rand.Float64()*0.2 - 0.1, VY: 0.2, VZ: m.Rand.Float64()*0.2 - 0.1,
	})
}

// piglinWasHurt is the admiring half of PiglinAi.wasHurtBy: the held item is
// let go without bartering, the admiring memory is erased and a player's
// blow disables admiring for a while. A blow from a piglin changes nothing.
// vanilla: PiglinAi.wasHurtBy
func (m *Mob) piglinWasHurt(t *tickView, id int32) {
	if o := t.mobByID(id); o != nil && o.Type == TypePiglin {
		return
	}
	if !m.OffHand.Empty() {
		m.finishAdmiring(t, false)
	}
	m.barter.admiring, m.barter.reachTime = 0, 0
	if t.playerByID(id) != nil {
		m.barter.admireOff = piglinAdmireOffTicks
	}
}

// finishAdmiring empties the off hand and, for gold, throws what the
// bartering table gives; any other loved item is worn if it beats what the
// piglin has, else stowed in its inventory. A baby instead holds it in the
// main hand, putting away what was there.
// vanilla: PiglinAi.stopHoldingOffHandItem
func (m *Mob) finishAdmiring(t *tickView, barter bool) {
	held := m.OffHand
	m.SetEquipment(protocol.EquipOffHand, protocol.ItemStack{})
	switch {
	case held.Empty():
		return
	case m.IsBaby():
		if !m.wearLoved(t, held) {
			m.releaseMainHand(t)
			m.SetEquipment(protocol.EquipMainHand, held)
			m.Persistence = true // vanilla: Piglin.holdInMainHand
		}
		return
	case held.ItemID != int32(registry.ItemGoldIngot):
		if !m.wearLoved(t, held) {
			m.stow(t, held)
		}
		return
	}
	if !barter {
		return
	}
	tx, ty, tz := m.randomThrowTarget()
	best := piglinThrowRange * piglinThrowRange
	for i := range t.players {
		p := &t.players[i]
		if p.Spectator || p.Dead {
			continue
		}
		dx, dz := p.X-m.X, p.Z-m.Z
		if d := dx*dx + (p.Y-m.Y)*(p.Y-m.Y) + dz*dz; d < best && m.seesTarget(t.w, p) {
			best, tx, ty, tz = d, p.X, p.Y, p.Z
		}
	}
	thrown := rules.RollLoot("minecraft:gameplay/piglin_bartering", m.giftContext(t.w), m.Rand)
	if m.Barter != nil && m.Barter.Listening() {
		thrown = m.Barter.Ask(m, held, thrown)
	}
	if len(thrown) > 0 {
		t.events = append(t.events, MobEvent{Mob: m, Kind: MobSwungOffHand})
	}
	for _, stack := range thrown {
		m.throwToward(t, stack, tx, ty, tz)
	}
}

// releaseMainHand stows a loved main-hand item or throws any other, leaving
// the hand empty.
// vanilla: PiglinAi.stopHoldingOffHandItem
func (m *Mob) releaseMainHand(t *tickView) {
	main := m.MainHand
	m.SetEquipment(protocol.EquipMainHand, protocol.ItemStack{})
	if main.Empty() {
		return
	}
	if isLoved(main) {
		m.stow(t, main)
		return
	}
	tx, ty, tz := m.randomThrowTarget()
	t.events = append(t.events, MobEvent{Mob: m, Kind: MobSwungOffHand})
	m.throwToward(t, main, tx, ty, tz)
}

// randomThrowTarget is a point within four blocks of the feet.
// vanilla: PiglinAi.throwItemsTowardRandomPos
func (m *Mob) randomThrowTarget() (x, y, z float64) {
	return m.X + float64(m.Rand.Intn(9)-4), m.Y, m.Z + float64(m.Rand.Intn(9)-4)
}

// throwToward throws the stack from the eyes towards a block over the
// target's feet.
// vanilla: PiglinAi.throwItemsTowardPos
func (m *Mob) throwToward(t *tickView, stack protocol.ItemStack, tx, ty, tz float64) {
	m.throwAt(t, stack, tx, ty+1, tz)
}

// throwAt throws the stack from 0.3 below the eyes towards the point.
// vanilla: BehaviorUtils.throwItem
func (m *Mob) throwAt(t *tickView, stack protocol.ItemStack, tx, ty, tz float64) {
	t.events = append(t.events, m.throwEvent(stack, tx, ty, tz, piglinThrowSpeed, piglinThrowSpeed, piglinThrowDrop))
}

// throwEvent is a throw from drop below the eyes towards the point, the
// velocity's horizontal and vertical scales applied to the unit direction; a
// point at the feet gives no velocity.
// vanilla: BehaviorUtils.throwItem (the overload taking the velocity and the hand's drop)
func (m *Mob) throwEvent(stack protocol.ItemStack, tx, ty, tz, horizontal, vertical, drop float64) MobEvent {
	dx, dy, dz := tx-m.X, ty-m.Y, tz-m.Z
	norm := math.Sqrt(dx*dx + dy*dy + dz*dz)
	if norm == 0 {
		norm = 1
	}
	return MobEvent{
		Mob: m, Kind: MobThrewItem, Stack: stack, Owned: true,
		X: m.X, Y: m.Y + m.eyeHeight() - drop, Z: m.Z,
		VX: dx / norm * horizontal, VY: dy / norm * vertical, VZ: dz / norm * horizontal,
	}
}

// sqDist3 is the squared distance between two points.
func sqDist3(x0, y0, z0, x1, y1, z1 float64) float64 {
	dx, dy, dz := x1-x0, y1-y0, z1-z0
	return dx*dx + dy*dy + dz*dz
}
