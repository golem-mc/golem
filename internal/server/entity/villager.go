package entity

import (
	"math/rand"

	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/registry"
)

// VillagerData is vanilla's VillagerData: type and profession by name,
// level, and whether the type was finalized from the birth biome.
type VillagerData struct {
	Type, Profession string
	Level            int
	Finalized        bool
}

// villagerTypes and villagerProfessions are the minecraft:villager_type and
// minecraft:villager_profession registries in ID order (registries
// report); the client has them built in.
var (
	villagerTypes = []string{
		"minecraft:desert", "minecraft:jungle", "minecraft:plains", "minecraft:savanna",
		"minecraft:snow", "minecraft:swamp", "minecraft:taiga",
	}
	villagerProfessions = []string{
		"minecraft:none", "minecraft:armorer", "minecraft:butcher", "minecraft:cartographer",
		"minecraft:cleric", "minecraft:farmer", "minecraft:fisherman", "minecraft:fletcher",
		"minecraft:leatherworker", "minecraft:librarian", "minecraft:mason", "minecraft:nitwit",
		"minecraft:shepherd", "minecraft:toolsmith", "minecraft:weaponsmith",
	}
)

// villagerTypeOfBiome is VillagerType.BY_BIOME; every other biome is plains.
var villagerTypeOfBiome = map[registry.Biome]string{
	registry.BiomeBadlands:               "minecraft:desert",
	registry.BiomeDesert:                 "minecraft:desert",
	registry.BiomeErodedBadlands:         "minecraft:desert",
	registry.BiomeWoodedBadlands:         "minecraft:desert",
	registry.BiomeBambooJungle:           "minecraft:jungle",
	registry.BiomeJungle:                 "minecraft:jungle",
	registry.BiomeSparseJungle:           "minecraft:jungle",
	registry.BiomeSavannaPlateau:         "minecraft:savanna",
	registry.BiomeSavanna:                "minecraft:savanna",
	registry.BiomeWindsweptSavanna:       "minecraft:savanna",
	registry.BiomeDeepFrozenOcean:        "minecraft:snow",
	registry.BiomeFrozenOcean:            "minecraft:snow",
	registry.BiomeFrozenRiver:            "minecraft:snow",
	registry.BiomeIceSpikes:              "minecraft:snow",
	registry.BiomeSnowyBeach:             "minecraft:snow",
	registry.BiomeSnowyTaiga:             "minecraft:snow",
	registry.BiomeSnowyPlains:            "minecraft:snow",
	registry.BiomeGrove:                  "minecraft:snow",
	registry.BiomeSnowySlopes:            "minecraft:snow",
	registry.BiomeFrozenPeaks:            "minecraft:snow",
	registry.BiomeJaggedPeaks:            "minecraft:snow",
	registry.BiomeSwamp:                  "minecraft:swamp",
	registry.BiomeMangroveSwamp:          "minecraft:swamp",
	registry.BiomeOldGrowthSpruceTaiga:   "minecraft:taiga",
	registry.BiomeOldGrowthPineTaiga:     "minecraft:taiga",
	registry.BiomeWindsweptGravellyHills: "minecraft:taiga",
	registry.BiomeWindsweptHills:         "minecraft:taiga",
	registry.BiomeTaiga:                  "minecraft:taiga",
	registry.BiomeWindsweptForest:        "minecraft:taiga",
}

// VillagerTypeByBiome is VillagerType.byBiome.
func VillagerTypeByBiome(biome registry.Biome) string {
	if t, ok := villagerTypeOfBiome[biome]; ok {
		return t
	}
	return "minecraft:plains"
}

// newZombieVillagerData is ZombieVillager.initializeZombieVillagerData:
// plains, level one, and a profession from the whole registry (none and
// nitwit included).
func newZombieVillagerData(rng *rand.Rand) VillagerData {
	return VillagerData{
		Type:       "minecraft:plains",
		Profession: villagerProfessions[rng.Intn(len(villagerProfessions))],
		Level:      1,
	}
}

// IDs is the data as the Villager Data metadata carries it: the type's and
// the profession's registry IDs, ok false for a name neither registry has.
func (v VillagerData) IDs() (typ, profession int32, ok bool) {
	typ, profession = -1, -1
	for i, name := range villagerTypes {
		if name == v.Type {
			typ = int32(i)
		}
	}
	for i, name := range villagerProfessions {
		if name == v.Profession {
			profession = int32(i)
		}
	}
	return typ, profession, typ >= 0 && profession >= 0
}

// The villager's speed modifiers over its 0.5 movement speed.
// vanilla: Villager.SPEED_MODIFIER, VillagerGoalPackages.getPanicPackage
const (
	villagerWalkPace  = 0.5
	villagerPanicPace = 0.75
)

// villagerGoals wires float, flight from danger, the activity-gated goals of
// the schedule (walking to a POI, the job, the stroll about it, sleeping),
// farming, the walk to a wanted item, breeding, the chat that trades gossip and food,
// child tag and play choices, the idle wander and weighted looks.
// vanilla: VillagerGoalPackages
func villagerGoals(m *Mob) []Goal {
	walk := func(slot memSlot, act villagerActivity, close, far int32, giveUp int64) Goal {
		return &poiWalkGoal{m: m, slot: slot, act: act, pace: villagerWalkPace, close: close, far: far, giveUp: giveUp}
	}
	m.vb.doors = newInteractWithDoor(m)
	return []Goal{
		newFloatGoal(m),
		&villagerPanicGoal{m: m},
		&tradingPlayerGoal{m: m},
		&villagerRaidGoal{m: m},
		&potentialJobGoal{m: m},
		walk(slotJob, actWork, 9, 100, 1200),
		walk(slotMeeting, actMeet, 6, 100, 200),
		walk(slotHome, actRest, 1, 150, 1200),
		&sleepInBedGoal{m: m},
		&giveGiftGoal{m: m, timeLeft: reducedTickDelay(giftFirstDelay)},
		&workAtPoiGoal{m: m},
		// vanilla: VillagerGoalPackages.getCorePackage (InteractWithDoor)
		m.vb.doors,
		&farmGoal{m: m, action: FarmHarvest},
		&farmGoal{m: m, action: FarmBonemeal},
		&farmlandStrollGoal{m: m},
		&poiStrollGoal{m: m, poi: villagerPoi(m, slotJob, actWork), pace: 0.4, reach: navReach, maxDist: 4, cooldown: 180},
		&poiStrollGoal{m: m, poi: villagerPoi(m, slotJob, actWork), pace: 0.4, reach: navReach, maxDist: 10, cooldown: 80, toPoi: true},
		&poiStrollGoal{m: m, poi: villagerPoi(m, slotMeeting, actMeet), pace: 0.4, reach: navReach, maxDist: 40, cooldown: 180},
		&kidPlayGoal{m: m},
		&villagerFetchGoal{m: m},
		&villagerBreedGoal{m: m},
		&villagerChatGoal{m: m},
		&showTradesGoal{m: m},
		&jumpOnBedGoal{m: m},
		&homeSearchGoal{m: m},
		&lookWalkGoal{m: m},
		&restWanderGoal{m: m},
		&randomStroll{m: m, pace: villagerWalkPace, blocked: func() bool {
			return m.vb.activity != actIdle
		}, pick: func(t *tickView) (Waypoint, bool) { return m.villageBoundPos(t, 10, 7) }},
		&villagerLookGoal{m: m},
		&villagerLookSink{m: m},
	}
}

// villagerLevelXp is the trade experience each level needs to reach the next,
// indexed by level - 1. vanilla: VillagerData.NEXT_LEVEL_XP_THRESHOLDS
var villagerLevelXp = [...]int32{0, 10, 70, 150, 250}

// Villager trade reward and level-up amounts. vanilla: Villager.rewardTradeXp
const (
	villagerLevelUpRegen    = 200 // ticks of regeneration the level-up gives
	villagerTradeOrbBase    = 3   // plus 0-3, the orb a trade drops
	villagerLevelUpOrbBonus = 5
)

// RewardTrade adds a trade's experience and returns the orb value it drops,
// zero for none. Reaching the next level's threshold raises the level at once
// (levelled: the caller draws that level's trades) and gives regeneration,
// whose change regen reports. A wandering trader gains nothing and rolls only
// for an orb it drops.
// vanilla: Villager.rewardTradeXp, Villager.shouldIncreaseLevel,
// Villager.increaseMerchantCareer, WanderingTrader.rewardTradeXp
func (m *Mob) RewardTrade(xp int32, rewardExp bool) (orb int, levelled bool, regen []effect.Change) {
	if m.Type == TypeWanderingTrader && !rewardExp {
		return 0, false, nil
	}
	orb = villagerTradeOrbBase + m.Rand.Intn(4)
	if m.Type == TypeWanderingTrader {
		return orb, false, nil
	}
	m.VillagerXp += xp
	if level := m.Villager.Level; level >= 1 && level < len(villagerLevelXp) && m.VillagerXp >= villagerLevelXp[level] {
		m.SetMetadataVillagerLevel(m.Villager.Level + 1)
		levelled = true
		if change, added := m.AddEffect(effect.New(mobEffectRegeneration, villagerLevelUpRegen, 0, false, true, true)); added {
			regen = []effect.Change{change}
		}
		orb += villagerLevelUpOrbBonus
	}
	if !rewardExp {
		orb = 0
	}
	return orb, levelled, regen
}
