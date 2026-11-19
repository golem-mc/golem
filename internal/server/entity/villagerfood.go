package entity

import (
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The villager's food: what it eats from its inventory, sharing it and the
// seeds it does not need with other villagers, and breeding on it. The
// inventory is the piglin's eight slots (piglininv.go).

var (
	breadItem = registry.MustItem("minecraft:bread")
	wheatItem = registry.MustItem("minecraft:wheat")
)

// farmerRequests is the farmer's requestedItems; every other profession's is
// empty. vanilla: VillagerProfession.FARMER
var farmerRequests = []int32{
	wheatItem, registry.MustItem("minecraft:wheat_seeds"),
	registry.MustItem("minecraft:beetroot_seeds"), registry.MustItem("minecraft:bone_meal"),
}

func requestedItems(profession string) []int32 {
	if profession == "minecraft:farmer" {
		return farmerRequests
	}
	return nil
}

// Villager food and breeding numbers. vanilla: Villager.BREEDING_FOOD_THRESHOLD,
// hasExcessFood, VillagerMakeLove, VillagerMakeLove.breed
const (
	villagerBreedFood   = 12
	villagerExcessFood  = 24
	villagerBirthMin    = 275 // ticks of courtship, plus 0-49
	villagerBirthSpread = 50
	villagerParentAge   = 6000
	villagerBedScan     = 48
	villagerBreedScan   = 64 // squared; vanilla: InteractWith maxDist 8
	villagerKeepCount   = 24 // vanilla: TradeWithVillager.throwHalfStack
)

// foodPoints is Villager.countFoodPointsInInventory.
func (m *Mob) foodPoints() int32 {
	var n int32
	for _, s := range m.barter.Inventory {
		points, _ := registry.StackVillagerFood(s)
		n += s.Count * points
	}
	return n
}

// villagerCanBreed is Villager.canBreed.
func (m *Mob) villagerCanBreed() bool {
	return m.FoodLevel+m.foodPoints() >= villagerBreedFood && !m.Sleeping && m.BabyAge == 0
}

// eatAndDigest eats until the food level reaches 12, then spends 12.
// vanilla: Villager.eatAndDigestFood, eatUntilFull
func (m *Mob) eatAndDigest() {
	inv := &m.barter.Inventory
	for i := range inv {
		if m.FoodLevel >= villagerBreedFood {
			break
		}
		v, _ := registry.StackVillagerFood(inv[i])
		for v > 0 && inv[i].Count > 0 && m.FoodLevel < villagerBreedFood {
			m.FoodLevel += v
			inv[i].Count--
		}
		if inv[i].Count <= 0 {
			inv[i] = protocol.ItemStack{}
		}
	}
	m.FoodLevel -= villagerBreedFood
}

// takeVillager is Villager.wantsToPickUp and InventoryCarrier.pickUpItem: a
// wanted item goes into the inventory, as much as fits.
func (s *stagedSlots) takeVillager(m *Mob, stack protocol.ItemStack) (slot string, count int32, ok bool) {
	if _, food := registry.StackVillagerFood(stack); !food && !registry.Item(stack.ItemID).InTag(registry.ItemTagIDVillagerPicksUp) &&
		!slices.Contains(requestedItems(m.Villager.Profession), stack.ItemID) {
		return "", 0, false
	}
	n := m.inventoryRoom(stack)
	return "inventory", n, n > 0
}

// inventoryRoom is how many of the stack the inventory takes.
func (m *Mob) inventoryRoom(stack protocol.ItemStack) int32 {
	var n int32
	for _, held := range m.inventorySlots() {
		if held.Empty() {
			n += registry.StackMaxStackSize(stack)
		} else if held.Same(stack) {
			n += max(registry.StackMaxStackSize(held)-held.Count, 0)
		}
	}
	return min(n, stack.Count)
}

// throwHalfStack throws part of the first stack the match takes: half of a
// stack over half full, else what is over 24.
// vanilla: TradeWithVillager.throwHalfStack
func (m *Mob) throwHalfStack(t *tickView, match func(protocol.ItemStack) bool, to *Mob) {
	inv := &m.barter.Inventory
	for i := range inv {
		s := inv[i]
		if s.Empty() || !match(s) {
			continue
		}
		var n int32
		switch {
		case s.Count > registry.StackMaxStackSize(s)/2:
			n = s.Count / 2
		case s.Count > villagerKeepCount:
			n = s.Count - villagerKeepCount
		default:
			continue
		}
		if inv[i].Count -= n; inv[i].Count == 0 {
			inv[i] = protocol.ItemStack{}
		}
		m.throwAt(t, protocol.ItemStack{ItemID: s.ItemID, Count: n}, to.X, to.Y, to.Z)
		return
	}
}

// shareWith throws food to a hungry villager (any villager, for a farmer),
// a farmer's surplus wheat, and the items the other's profession asks for and
// this one's does not.
// vanilla: TradeWithVillager.tick, figureOutWhatIAmWillingToTrade
func (m *Mob) shareWith(t *tickView, o *Mob) {
	farmer := m.Villager.Profession == "minecraft:farmer"
	if m.foodPoints() >= villagerExcessFood && (farmer || o.foodPoints() < villagerBreedFood) {
		m.throwHalfStack(t, func(s protocol.ItemStack) bool { _, food := registry.StackVillagerFood(s); return food }, o)
	}
	if farmer && m.countItem(wheatItem) > registry.MaxStackSize(wheatItem)/2 {
		m.throwHalfStack(t, func(s protocol.ItemStack) bool { return s.ItemID == wheatItem }, o)
	}
	mine := requestedItems(m.Villager.Profession)
	theirs := requestedItems(o.Villager.Profession)
	m.throwHalfStack(t, func(s protocol.ItemStack) bool {
		return slices.Contains(theirs, s.ItemID) && !slices.Contains(mine, s.ItemID)
	}, o)
}

func (m *Mob) countItem(item int32) int32 {
	var n int32
	for _, s := range m.barter.Inventory {
		if s.ItemID == item {
			n += s.Count
		}
	}
	return n
}

// villagerFetchGoal walks to the wanted item the sensor saw within four
// blocks. vanilla: GoToWantedItem.create(0.5, false, 4)
type villagerFetchGoal struct{ m *Mob }

func (g *villagerFetchGoal) Priority() int           { return priorityPoiStrol }
func (g *villagerFetchGoal) Flags() GoalFlags        { return GoalMove | GoalLook }
func (g *villagerFetchGoal) RequiresEveryTick() bool { return false }

func (g *villagerFetchGoal) CanStart(*tickView) bool {
	m, b := g.m, &g.m.barter
	return b.hasWanted && m.CanPickUpLoot && !m.Sleeping && Dist2(m.X, m.Y, m.Z, b.wanted.X, b.wanted.Y, b.wanted.Z) < 16
}

func (g *villagerFetchGoal) Start(t *tickView) {
	it := g.m.barter.wanted
	g.m.seekTo(t.w, it.X, it.Y, it.Z, villagerWalkPace, 0)
}

func (g *villagerFetchGoal) ShouldContinue(*tickView) bool { return len(g.m.navOf().nav.path) > 0 }
func (g *villagerFetchGoal) Tick(*tickView)                {}

// Stop forgets the item until the next scan, so a stale one is not walked to
// again.
func (g *villagerFetchGoal) Stop(*tickView) {
	g.m.barter.hasWanted = false
	g.m.stopSeeking()
}

// villagerBreedGoal courts a villager in view that can breed too and, after
// 275-324 ticks side by side, has a child if a bed is free for it.
// vanilla: InteractWith (BREED_TARGET), VillagerMakeLove
type villagerBreedGoal struct {
	m      *Mob
	target *Mob
	birth  int64
	recalc int
}

func (g *villagerBreedGoal) Priority() int           { return priorityPoiStrol }
func (g *villagerBreedGoal) Flags() GoalFlags        { return GoalMove | GoalLook }
func (g *villagerBreedGoal) RequiresEveryTick() bool { return true }

// CanStart picks the courtship one time in eight, InteractWith's weight in
// the idle RunOne.
func (g *villagerBreedGoal) CanStart(t *tickView) bool {
	m := g.m
	if m.Village == nil || m.vb.activity != actIdle || m.Rand.Intn(8) != 0 || !m.villagerCanBreed() {
		return false
	}
	g.target = nil
	best := float64(villagerBreedScan)
	for _, o := range t.others {
		if o == m || o.Type != TypeVillager || !o.Alive() || !o.villagerCanBreed() {
			continue
		}
		if d := Dist2(m.X, m.Y, m.Z, o.X, o.Y, o.Z); d <= best && m.sees(t, o) {
			g.target, best = o, d
		}
	}
	return g.target != nil
}

func (g *villagerBreedGoal) Start(t *tickView) {
	m := g.m
	g.birth = m.GameTime + villagerBirthMin + int64(m.Rand.Intn(villagerBirthSpread))
	g.recalc = 0
	t.events = append(t.events,
		MobEvent{Mob: g.target, Kind: MobStatus, Status: protocol.EntityStatusLoveHearts},
		MobEvent{Mob: m, Kind: MobStatus, Status: protocol.EntityStatusLoveHearts})
}

func (g *villagerBreedGoal) ShouldContinue(t *tickView) bool {
	tg := g.target
	return g.m.GameTime <= g.birth && g.m.vb.activity == actIdle && tg != nil && tg.Alive() && !tg.gone &&
		g.m.sees(t, tg) && g.m.villagerCanBreed() && tg.villagerCanBreed()
}

func (g *villagerBreedGoal) Tick(t *tickView) {
	m, tg := g.m, g.target
	m.lookAtMob(tg)
	if g.recalc--; g.recalc <= 0 {
		g.recalc = reducedTickDelay(followReplan)
		m.seekTo(t.w, tg.X, tg.Y, tg.Z, villagerWalkPace, 2)
	}
	if Dist2(m.X, m.Y, m.Z, tg.X, tg.Y, tg.Z) > gossipTalkDist2 {
		return
	}
	if m.GameTime >= g.birth {
		m.eatAndDigest()
		tg.eatAndDigest()
		m.giveBirth(t, tg)
	} else if m.Rand.Intn(35) == 0 {
		t.events = append(t.events,
			MobEvent{Mob: tg, Kind: MobStatus, Status: protocol.EntityStatusVillagerHearts},
			MobEvent{Mob: m, Kind: MobStatus, Status: protocol.EntityStatusVillagerHearts})
	}
}

func (g *villagerBreedGoal) Stop(*tickView) {
	g.target = nil
	g.m.stopSeeking()
}

// giveBirth claims a free bed the villager can walk to for the child and
// asks for the child; with no bed both parents are angry.
// vanilla: VillagerMakeLove.tryToGiveBirth, takeVacantBed
func (m *Mob) giveBirth(t *tickView, partner *Mob) {
	c := m.cell()
	for _, p := range m.Village.POIs(isHome, c.X, c.Y, c.Z, villagerBedScan) {
		if !isHome(rules.POIKindOf(t.w.BlockAt(p.X, p.Y, p.Z))) || m.poiTaken(t, p) > 0 || !m.pathReaches(t.w, p, 1) {
			continue
		}
		// Held on the parent until the child spawns, so the bed counts as
		// taken meanwhile.
		m.vb.childHome = &GlobalPos{Dim: m.Village.Dimension(), X: p.X, Y: p.Y, Z: p.Z}
		m.SetMetadataBabyAge(villagerParentAge)
		partner.SetMetadataBabyAge(villagerParentAge)
		t.events = append(t.events, MobEvent{Mob: m, Kind: MobBred, Partner: partner})
		return
	}
	t.events = append(t.events,
		MobEvent{Mob: partner, Kind: MobStatus, Status: protocol.EntityStatusVillagerAngry},
		MobEvent{Mob: m, Kind: MobStatus, Status: protocol.EntityStatusVillagerAngry})
}

// BreedVillager makes the child a jobless villager of the biome's type (half
// the time) or a parent's, and gives it the bed giveBirth claimed.
// vanilla: Villager.getBreedOffspring, VillagerMakeLove.giveBedToChild
func (m *Mob) BreedVillager(baby, partner *Mob, biome registry.Biome) {
	typ := partner.Villager.Type
	switch roll := m.Rand.Float64(); {
	case roll < 0.5:
		typ = VillagerTypeByBiome(biome)
	case roll < 0.75:
		typ = m.Villager.Type
	}
	baby.Villager = VillagerData{Type: typ, Profession: "minecraft:none", Level: 1, Finalized: true}
	baby.InvalidateMetadataVillager()
	baby.vb.home, m.vb.childHome = m.vb.childHome, nil
}

// saveFood writes the villager food level.
// vanilla: Villager.addAdditionalSaveData
func (m *Mob) saveFood(out nbt.CompoundTag) {
	out.Set("FoodLevel", nbt.Byte(int8(m.FoodLevel)))
}

// loadFood reads them back. vanilla: Villager.readAdditionalSaveData
func (m *Mob) loadFood(c nbt.Compound) {
	if v, ok := nbt.ByteField(c, "FoodLevel"); ok {
		m.FoodLevel = int32(v)
	}
	m.loadInventory(c)
}
