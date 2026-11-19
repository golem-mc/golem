package entity

import (
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// FarmAction is a villager's deferred crop or workstation use.
type FarmAction uint8

const (
	FarmHarvest FarmAction = iota // harvest a ripe crop, or plant an empty cell
	FarmBonemeal
	FarmCompost
)

// FarmResult is the block edit, drops and effects of a validated farm action.
type FarmResult struct {
	State                    uint16
	Changed, Break, Bonemeal bool
	Composted, Emptied       bool
	Fill                     int32
	Drops                    []protocol.ItemStack
}

// cropAge models CropBlock subclasses, not every block in #crops (pitchers
// and stems are not CropBlocks). vanilla: HarvestFarmland.validPos
func cropAge(state uint16) (age, maxAge int, ok bool) {
	switch registry.BlockOf(state) {
	case registry.BlockWheat, registry.BlockCarrots, registry.BlockPotatoes:
		maxAge = 7
	case registry.BlockBeetroots:
		maxAge = 3
	case registry.BlockTorchflowerCrop:
		maxAge = 2
	default:
		return 0, 0, false
	}
	age, _ = registry.StateInt(state, registry.PropAge)
	return age, maxAge, true
}

// farmlandStep is SecondaryPoiSensor: only farmers have secondary POIs.
func (m *Mob) farmlandStep(t *tickView) {
	v := &m.vb
	if m.Villager.Profession != "minecraft:farmer" || m.Village == nil {
		v.farmland = nil
		return
	}
	if dim := m.Village.Dimension(); dim != v.farmDim {
		v.farmland, v.farmScan, v.farmDim = nil, 0, dim
	}
	if v.farmScan--; v.farmScan > 0 {
		return
	}
	v.farmScan, v.farmland = 40, v.farmland[:0]
	c := m.cell()
	for x := c.X - 4; x <= c.X+4; x++ {
		for y := c.Y - 2; y <= c.Y+2; y++ {
			for z := c.Z - 4; z <= c.Z+4; z++ {
				if registry.BlockOf(t.w.BlockAt(x, y, z)) == registry.BlockFarmland {
					v.farmland = append(v.farmland, Waypoint{x, y, z})
				}
			}
		}
	}
}

func (m *Mob) farmEvent(t *tickView, action FarmAction, p Waypoint) {
	t.events = append(t.events, MobEvent{Mob: m, Kind: MobFarm, Farm: action,
		X: float64(p.X), Y: float64(p.Y), Z: float64(p.Z)})
}

// Farm rechecks live state and spends inventory only when the action can
// apply. The server calls it under the mob lock during the effects phase;
// two villagers reaching the same cell cannot both spend a seed or meal.
func (m *Mob) Farm(w World, action FarmAction, p Waypoint) (r FarmResult) {
	if m.Type != TypeVillager || !m.Alive() {
		return r
	}
	s := w.BlockAt(p.X, p.Y, p.Z)
	age, maxAge, crop := cropAge(s)
	switch action {
	case FarmHarvest:
		if m.Villager.Profession != "minecraft:farmer" || !w.MobGriefing() {
			return r
		}
		if crop && age >= maxAge {
			r.Break = true
		} else if w.IsAirAt(p.X, p.Y, p.Z) && registry.BlockOf(w.BlockAt(p.X, p.Y-1, p.Z)) == registry.BlockFarmland {
			for i, stack := range m.barter.Inventory {
				if stack.Empty() || !registry.Item(stack.ItemID).InTag(registry.ItemTagIDVillagerPlantableSeeds) {
					continue
				}
				if b, ok := registry.Item(stack.ItemID).Block(); ok {
					r.State, r.Changed = b.DefaultState(), true
					m.spendFarmItem(i)
					break
				}
			}
		}
	case FarmBonemeal:
		if crop && age < maxAge {
			for i, stack := range m.barter.Inventory {
				if !stack.Empty() && registry.Item(stack.ItemID) == registry.ItemBoneMeal {
					m.spendFarmItem(i)
					r.Bonemeal = true
					break
				}
			}
		}
	case FarmCompost:
		if m.Villager.Profession == "minecraft:farmer" && registry.BlockOf(s) == registry.BlockComposter {
			ctx := rules.LootContext{Level: rules.LevelAt(w, w.LootReads(), float64(p.X)+0.5, float64(p.Y)+0.5, float64(p.Z)+0.5), Interacting: m.LootEntity(),
				BlockAt: func(dx, dy, dz int32) uint16 { return w.BlockAt(p.X+dx, p.Y+dy, p.Z+dz) }}
			r = m.workComposter(s, &ctx)
		}
	}
	return r
}

func (m *Mob) spendFarmItem(i int) {
	s := &m.barter.Inventory[i]
	s.Count--
	if s.Count <= 0 {
		*s = protocol.ItemStack{}
	}
}

// workComposter is WorkAtComposter.useWorkstation. It does not depend on
// mob_griefing; only HarvestFarmland checks that rule in vanilla.
func (m *Mob) workComposter(state uint16, ctx *rules.LootContext) (r FarmResult) {
	if m.countItem(breadItem) <= 36 {
		n := min(int32(3), m.countItem(wheatItem)/3)
		left := n * 3
		for i := range m.barter.Inventory {
			s := &m.barter.Inventory[i]
			if s.ItemID == wheatItem {
				used := min(left, s.Count)
				s.Count, left = s.Count-used, left-used
				if s.Count == 0 {
					*s = protocol.ItemStack{}
				}
			}
		}
		if n > 0 {
			if rest := m.addToInventory(protocol.ItemStack{ItemID: breadItem, Count: n}); !rest.Empty() {
				r.Drops = append(r.Drops, rest)
			}
		}
	}
	r.State, r.Composted = state, true
	if rules.ComposterReady(state) {
		r.State, _ = registry.WithInt(state, registry.PropLevel, 0)
		r.Emptied = true
		r.Drops = append(r.Drops, protocol.ItemStack{ItemID: int32(registry.ItemBoneMeal), Count: 1})
	}
	initial := r.State
	seen := [2]int32{}
	budget := int32(20)
	for i := len(m.barter.Inventory) - 1; i >= 0 && budget > 0; i-- {
		s := &m.barter.Inventory[i]
		index := slices.Index(farmerRequests[1:3], s.ItemID)
		if s.Empty() || index < 0 {
			continue
		}
		seen[index] += s.Count
		n := min(seen[index]-10, budget, s.Count)
		budget -= max(n, 0)
		for ; n > 0; n-- {
			level, _ := registry.StateInt(r.State, registry.PropLevel)
			if level < 7 {
				after, inserted, _ := rules.ComposterInsert(r.State, *s, ctx, m.Rand)
				if inserted {
					r.State = after
					m.spendFarmItem(i)
				}
			}
			if level, _ = registry.StateInt(r.State, registry.PropLevel); level == 7 {
				break
			}
		}
		if level, _ := registry.StateInt(r.State, registry.PropLevel); level == 7 {
			break
		}
	}
	r.Changed = r.State != state
	if r.State != initial {
		r.Fill = 1
	}
	return r
}

// farmGoal ports HarvestFarmland and UseBonemeal's nearby targets, work
// cycles and session cooldowns. Start gates adapt the work RunOne weights
// to the goal selector, as the other villager interaction goals do.
type farmGoal struct {
	m                   *Mob
	action              FarmAction
	target              *Waypoint
	candidates          []Waypoint
	next, end           int64
	lastSession, worked int
	repick              bool
}

func (g *farmGoal) Priority() int           { return priorityPoiStrol }
func (g *farmGoal) Flags() GoalFlags        { return GoalMove | GoalLook }
func (g *farmGoal) RequiresEveryTick() bool { return true }

func (g *farmGoal) CanStart(t *tickView) bool {
	m := g.m
	if m.vb.activity != actWork || m.Sleeping || m.Trading {
		return false
	}
	weight, total := 2, 25
	if g.action == FarmHarvest {
		if m.Villager.Profession != "minecraft:farmer" || len(m.vb.farmland) == 0 || !t.w.MobGriefing() {
			return false
		}
	} else {
		weight = 4
		if m.Villager.Profession != "minecraft:farmer" {
			weight, total = 7, 31
		}
		// Selection alternates with the entity ID's parity; a two-tick
		// window gives either parity one check per ten ticks.
		if m.Age%10 > 1 || g.lastSession != 0 && m.Age < g.lastSession+160 || m.countItem(int32(registry.ItemBoneMeal)) == 0 {
			return false
		}
	}
	if m.Rand.Intn(total) >= weight {
		return false
	}
	g.pick(t)
	return g.target != nil
}

func (g *farmGoal) pick(t *tickView) {
	g.candidates = g.candidates[:0]
	c := g.m.cell()
	for x := c.X - 1; x <= c.X+1; x++ {
		for y := c.Y - 1; y <= c.Y+1; y++ {
			for z := c.Z - 1; z <= c.Z+1; z++ {
				age, maxAge, crop := cropAge(t.w.BlockAt(x, y, z))
				valid := crop && age < maxAge
				if g.action == FarmHarvest {
					valid = crop && age >= maxAge || t.w.IsAirAt(x, y, z) && registry.BlockOf(t.w.BlockAt(x, y-1, z)) == registry.BlockFarmland
				}
				if valid {
					g.candidates = append(g.candidates, Waypoint{x, y, z})
				}
			}
		}
	}
	g.choose()
}

func (g *farmGoal) choose() {
	g.target = nil
	if len(g.candidates) > 0 {
		p := g.candidates[g.m.Rand.Intn(len(g.candidates))]
		g.target = &p
	}
}

func (g *farmGoal) walk(t *tickView) {
	if p := g.target; p != nil {
		// MoveToTargetSink.tryComputePath uses reach 0; stopping at an
		// adjacent path node would leave the crop outside the use radius.
		g.m.seekTo(t.w, float64(p.X)+0.5, float64(p.Y), float64(p.Z)+0.5, villagerWalkPace, 0)
	}
}

func (g *farmGoal) Start(t *tickView) {
	g.worked, g.repick, g.end = 0, false, g.m.GameTime+60 // Behavior.DEFAULT_DURATION
	if g.action == FarmBonemeal {
		g.m.SetEquipment(protocol.EquipMainHand, protocol.ItemStack{ItemID: int32(registry.ItemBoneMeal), Count: 1})
		g.next = g.m.GameTime
	}
	if g.action == FarmBonemeal || g.m.GameTime > g.next {
		g.walk(t)
	}
}

func (g *farmGoal) ShouldContinue(*tickView) bool {
	limit := 200
	if g.action == FarmBonemeal {
		limit = 80
	}
	return g.m.vb.activity == actWork && !g.m.Sleeping && !g.m.Trading && g.m.GameTime <= g.end && g.worked < limit && g.target != nil
}

func (g *farmGoal) Tick(t *tickView) {
	m := g.m
	if g.repick {
		g.pick(t)
		g.walk(t)
		g.repick = false
	}
	p := g.target
	if p == nil {
		return
	}
	m.lookAtPoint(float64(p.X)+0.5, float64(p.Y)+0.5, float64(p.Z)+0.5)
	if !m.nearCenter(*p, 1) {
		return
	}
	m.stopSeeking()
	if m.GameTime > g.next || g.action == FarmBonemeal && m.GameTime == g.next {
		m.farmEvent(t, g.action, *p)
		if g.action == FarmBonemeal {
			g.next, g.repick = m.GameTime+40, true
		} else if age, maxAge, crop := cropAge(t.w.BlockAt(p.X, p.Y, p.Z)); crop && age < maxAge {
			g.candidates = slices.DeleteFunc(g.candidates, func(c Waypoint) bool { return c == *p })
			g.choose()
			g.next = m.GameTime + 20
			g.walk(t)
		}
	}
	g.worked++
}

func (g *farmGoal) Stop(*tickView) {
	if g.action == FarmBonemeal {
		g.m.SetEquipment(protocol.EquipMainHand, protocol.ItemStack{})
		g.lastSession = g.m.Age
	} else {
		g.next = g.m.GameTime + 40
	}
	g.m.stopSeeking()
}

// farmlandStrollGoal is StrollToPoiList(SECONDARY_JOB_SITE, 0.5, 1, 6, JOB_SITE).
type farmlandStrollGoal struct {
	m    *Mob
	next int64
	to   Waypoint
}

func (g *farmlandStrollGoal) Priority() int           { return priorityPoiStrol }
func (g *farmlandStrollGoal) Flags() GoalFlags        { return GoalMove }
func (g *farmlandStrollGoal) RequiresEveryTick() bool { return false }
func (g *farmlandStrollGoal) CanStart(*tickView) bool {
	m := g.m
	if m.vb.activity != actWork || m.Sleeping || m.Trading || !m.memHere(m.vb.jobSite) || !m.nearCenter(m.vb.jobSite.at(), 6) || len(m.vb.farmland) == 0 || m.GameTime <= g.next || m.Rand.Intn(5) != 0 {
		return false
	}
	g.to = m.vb.farmland[m.Rand.Intn(len(m.vb.farmland))]
	return true
}
func (g *farmlandStrollGoal) Start(t *tickView) {
	g.next = g.m.GameTime + 100
	g.m.seekTo(t.w, float64(g.to.X)+0.5, float64(g.to.Y), float64(g.to.Z)+0.5, villagerWalkPace, 0)
}
func (g *farmlandStrollGoal) ShouldContinue(*tickView) bool {
	return g.m.vb.activity == actWork && !g.m.Sleeping && !g.m.Trading && len(g.m.navOf().nav.path) > 0
}
func (g *farmlandStrollGoal) Tick(*tickView) {
	if manhattan3(g.m.cell(), g.to) <= 1 {
		g.m.stopSeeking()
	}
}
func (g *farmlandStrollGoal) Stop(*tickView) { g.m.stopSeeking() }
