package entity

import (
	"sort"
	"testing"

	"github.com/golem-mc/golem/internal/server/inventory"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

type testOfferUpdater struct{}

func (testOfferUpdater) UpdateOfferState(offers nbt.Compound, update func(uses, maxUses, demand int32) (int32, int32)) nbt.Compound {
	return inventory.UpdateOfferState(offers, update)
}

// fakeVillage lists the POIs a test placed, closest first.
type fakeVillage struct {
	w    testWorld
	pois []Waypoint
}

func (v fakeVillage) Dimension() string { return "minecraft:overworld" }

func (v fakeVillage) POIs(match func(rules.POIKind) bool, x, y, z int32, radius int) []Waypoint {
	var out []Waypoint
	for _, p := range v.pois {
		if match(rules.POIKindOf(v.w.BlockAt(p.X, p.Y, p.Z))) {
			out = append(out, p)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return manhattan3(out[i], Waypoint{x, y, z}) < manhattan3(out[j], Waypoint{x, y, z})
	})
	return out
}

func (v fakeVillage) IsBed(x, y, z int32) bool {
	return registry.BlockOf(v.w.BlockAt(x, y, z)).InGroup(registry.GroupBed)
}

func (v fakeVillage) BedStandUp(x, y, z int32, _ float32) (float64, float64, float64) {
	return float64(x) + 1.5, float64(y), float64(z) + 0.5
}

func bedHead(t *testing.T) uint16 {
	t.Helper()
	state, ok := registry.FindState("minecraft:red_bed", map[string]string{"part": "head", "facing": "north", "occupied": "false"})
	if !ok {
		t.Fatal("no red bed head state")
	}
	return state
}

func TestTheVillagerScheduleKeyframesAndWrap(t *testing.T) {
	cases := []struct {
		time int64
		baby bool
		want villagerActivity
	}{
		{0, false, actRest}, {9, false, actRest}, {10, false, actIdle}, {1999, false, actIdle},
		{2000, false, actWork}, {8999, false, actWork}, {9000, false, actMeet}, {10999, false, actMeet},
		{11000, false, actIdle}, {11999, false, actIdle}, {12000, false, actRest}, {23999, false, actRest},
		{24000 + 2500, false, actWork},
		{5, true, actRest}, {10, true, actIdle}, {2999, true, actIdle}, {3000, true, actPlay}, {5999, true, actPlay}, {6000, true, actIdle}, {9999, true, actIdle}, {10000, true, actPlay}, {11999, true, actPlay}, {12000, true, actRest},
	}
	for _, c := range cases {
		if got := villagerSchedule(c.time, c.baby); got != c.want {
			t.Errorf("villagerSchedule(%d, baby=%v) = %d, want %d", c.time, c.baby, got, c.want)
		}
	}
}

func TestTwoVillagersShareOneBedByOneClaiming(t *testing.T) {
	w := newTestWorld()
	floor(w, -6, -6, 6, 6, 63, defaultState(t, "minecraft:stone"))
	head := bedHead(t)
	w.set(1, 64, 0, head)
	w.set(4, 64, 0, head)
	view := fakeVillage{w: w, pois: []Waypoint{{1, 64, 0}, {4, 64, 0}}}
	a := NewMob(TypeVillager, 1, 0.5, 64, 0.5, 1)
	b := NewMob(TypeVillager, 2, 0.5, 64, 2.5, 2)
	a.Village, b.Village = view, view
	for tick := int64(1); tick <= 80 && (a.vb.home == nil || b.vb.home == nil); tick++ {
		for _, m := range []*Mob{a, b} {
			m.GameTime = tick
			other := a
			if m == a {
				other = b
			}
			m.villagerBrainStep(&tickView{w: w, others: []*Mob{other}})
		}
	}
	if a.vb.home == nil || b.vb.home == nil {
		t.Fatalf("homes %v and %v, want both claimed", a.vb.home, b.vb.home)
	}
	if *a.vb.home == *b.vb.home {
		t.Errorf("both claimed %v", a.vb.home)
	}
	if first := (Waypoint{1, 64, 0}); a.vb.home.at() != first && b.vb.home.at() != first {
		t.Errorf("homes %v and %v, neither the closest bed", a.vb.home, b.vb.home)
	}
}

func TestAJobSiteGivesItsProfession(t *testing.T) {
	cases := map[string]string{
		"minecraft:blast_furnace": "minecraft:armorer", "minecraft:smoker": "minecraft:butcher",
		"minecraft:cartography_table": "minecraft:cartographer", "minecraft:brewing_stand": "minecraft:cleric",
		"minecraft:composter": "minecraft:farmer", "minecraft:barrel": "minecraft:fisherman",
		"minecraft:fletching_table": "minecraft:fletcher", "minecraft:cauldron": "minecraft:leatherworker",
		"minecraft:water_cauldron": "minecraft:leatherworker", "minecraft:lava_cauldron": "minecraft:leatherworker",
		"minecraft:powder_snow_cauldron": "minecraft:leatherworker", "minecraft:lectern": "minecraft:librarian",
		"minecraft:stonecutter": "minecraft:mason", "minecraft:loom": "minecraft:shepherd",
		"minecraft:smithing_table": "minecraft:toolsmith", "minecraft:grindstone": "minecraft:weaponsmith",
	}
	for block, want := range cases {
		w := newTestWorld()
		w.set(1, 64, 0, defaultState(t, block))
		m := NewMob(TypeVillager, 1, 0.5, 64, 0.5, 1)
		m.Village = fakeVillage{w: w}
		m.vb.potentialJobSite = &GlobalPos{Dim: "minecraft:overworld", X: 1, Y: 64, Z: 0}
		m.assignProfession(&tickView{w: w})
		if m.Villager.Profession != want || m.vb.jobSite == nil || m.vb.potentialJobSite != nil {
			t.Errorf("%s: profession %q, job %v, potential %v; want %q", block, m.Villager.Profession, m.vb.jobSite, m.vb.potentialJobSite, want)
		}
		// A villager that holds a profession keeps it at another site.
		if !acquirableJobSite(want, rules.POIKindOf(defaultState(t, block))) || acquirableJobSite("minecraft:nitwit", rules.POIKindOf(defaultState(t, block))) {
			t.Errorf("%s: acquirable by %s or by a nitwit wrongly", block, want)
		}
	}
	m := NewMob(TypeVillager, 1, 0.5, 64, 0.5, 1)
	m.Villager.Profession = "minecraft:librarian"
	m.Offers = nbt.Compound{"Recipes": nbt.List{Elem: nbt.TypeCompound}}
	m.resetProfession()
	if m.Villager.Profession != "minecraft:none" || m.Offers != nil {
		t.Errorf("a librarian with no job site is %q with offers %v", m.Villager.Profession, m.Offers)
	}
	m.Villager.Profession, m.VillagerXp = "minecraft:librarian", 1
	m.resetProfession()
	if m.Villager.Profession != "minecraft:librarian" {
		t.Error("a villager with experience lost its profession")
	}
}

func recipe(uses, maxUses, demand int32) nbt.Compound {
	return nbt.Compound{"uses": nbt.Int(uses), "maxUses": nbt.Int(maxUses), "demand": nbt.Int(demand)}
}

func recipeOf(m *Mob, i int) (uses, demand int32) {
	list, _ := nbt.Get[nbt.List](m.Offers, "Recipes")
	r := list.Items[i].(nbt.Compound)
	uses, _ = nbt.IntField(r, "uses")
	demand, _ = nbt.IntField(r, "demand")
	return uses, demand
}

func TestARestockRefillsUsesAndMovesDemand(t *testing.T) {
	m := NewMob(TypeVillager, 1, 0.5, 64, 0.5, 1)
	m.OfferUpdater = testOfferUpdater{}
	list := nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{recipe(3, 12, 1), recipe(0, 12, 5)}}
	m.Offers = nbt.Compound{"Recipes": list}
	held := m.Offers
	m.GameTime = 500
	m.restock()
	if u, d := recipeOf(m, 0); u != 0 || d != 1+3-(12-3) {
		t.Errorf("used recipe: uses %d demand %d, want 0 and %d", u, d, 1+3-9)
	}
	if _, d := recipeOf(m, 1); d != 5-12 {
		t.Errorf("unused recipe demand %d, want %d", d, 5-12)
	}
	if m.vb.lastRestock != 500 || m.vb.restocksToday != 1 {
		t.Errorf("lastRestock %d, restocks %d", m.vb.lastRestock, m.vb.restocksToday)
	}
	if u := held["Recipes"].(nbt.List).Items[0].(nbt.Compound)["uses"]; u != nbt.Int(3) {
		t.Errorf("the old offers were written to: uses %v", u)
	}
}

func TestACatchUpDemandResetsUsesBeforeTheDemand(t *testing.T) {
	m := NewMob(TypeVillager, 1, 0.5, 64, 0.5, 1)
	m.OfferUpdater = testOfferUpdater{}
	m.Offers = nbt.Compound{"Recipes": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{recipe(4, 6, 10)}}}
	m.vb.restocksToday = 0
	m.catchUpDemand()
	// Two missed updates over zero uses, each taking maxUses off.
	if u, d := recipeOf(m, 0); u != 0 || d != 10-2*6 {
		t.Errorf("after two missed updates: uses %d demand %d, want 0 and %d", u, d, 10-12)
	}
	m.Offers = nbt.Compound{"Recipes": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{recipe(4, 6, 10)}}}
	m.vb.restocksToday = 1
	m.catchUpDemand()
	if u, d := recipeOf(m, 0); u != 0 || d != 10-6 {
		t.Errorf("after one missed update: uses %d demand %d, want 0 and 4", u, d)
	}
	m.Offers = nbt.Compound{"Recipes": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{recipe(4, 6, 10)}}}
	m.vb.restocksToday = 2
	m.catchUpDemand()
	if u, d := recipeOf(m, 0); u != 4 || d != 10 {
		t.Errorf("after none missed: uses %d demand %d, want 4 and 10", u, d)
	}
}

func TestShouldRestockAllowsTwoADayAWhileApart(t *testing.T) {
	m := NewMob(TypeVillager, 1, 0.5, 64, 0.5, 1)
	m.OfferUpdater = testOfferUpdater{}
	m.Offers = nbt.Compound{"Recipes": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{recipe(1, 12, 0)}}}
	m.vb.lastRestock = 10000
	m.GameTime, m.DayTime = 13000, 13000
	if !m.shouldRestock() {
		t.Fatal("no first restock with a used offer")
	}
	m.restock()
	m.Offers = nbt.Compound{"Recipes": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{recipe(1, 12, 0)}}}
	m.GameTime += 100
	if m.shouldRestock() {
		t.Error("a second restock 100 ticks after the first")
	}
	m.GameTime += 2400
	if !m.shouldRestock() {
		t.Error("no second restock after 2500 ticks")
	}
	m.restock()
	m.GameTime += 2500
	if m.shouldRestock() {
		t.Error("a third restock in a day")
	}
}

func TestAVillagersBrainRoundTripsAndKeepsWhatItDoesNotModel(t *testing.T) {
	m := NewMob(TypeVillager, 1, 0.5, 64, 0.5, 1)
	m.vb.home = &GlobalPos{Dim: "minecraft:overworld", X: 1, Y: 64, Z: -3}
	m.vb.jobSite = &GlobalPos{Dim: "minecraft:the_nether", X: 5, Y: 70, Z: 9}
	m.vb.lastSlept, m.vb.lastRestock, m.vb.restocksToday = 77, 1234, 2
	tag := nbt.Compound{}
	m.SaveTo(tag, func() int64 { return 0 })
	brain := tag["Brain"].(nbt.Compound)
	mems := brain["memories"].(nbt.Compound)
	mems["minecraft:golem_detected_recently"] = nbt.Compound{"value": nbt.Byte(1), "ttl": nbt.Long(5)}
	home := mems["minecraft:home"].(nbt.Compound)["value"].(nbt.Compound)
	if home["dimension"] != nbt.String("minecraft:overworld") || len(home["pos"].(nbt.IntArray)) != 3 {
		t.Fatalf("home saved as %v", home)
	}

	l := NewMob(TypeVillager, 2, 0.5, 64, 0.5, 2)
	l.LoadFrom(tag, newTestWorld(), func() int64 { return 0 })
	if l.vb.home == nil || *l.vb.home != *m.vb.home || l.vb.jobSite == nil || *l.vb.jobSite != *m.vb.jobSite {
		t.Errorf("loaded home %v, job site %v", l.vb.home, l.vb.jobSite)
	}
	if l.vb.lastSlept != 77 || l.vb.lastRestock != 1234 || l.vb.restocksToday != 2 {
		t.Errorf("loaded lastSlept %d lastRestock %d restocks %d", l.vb.lastSlept, l.vb.lastRestock, l.vb.restocksToday)
	}
	again := nbt.Compound{}
	l.SaveTo(again, func() int64 { return 0 })
	kept := again["Brain"].(nbt.Compound)["memories"].(nbt.Compound)["minecraft:golem_detected_recently"]
	if kept == nil || kept.(nbt.Compound)["ttl"] != nbt.Long(5) {
		t.Errorf("the unmodelled memory came back as %v", kept)
	}
}

func TestAWalkNearItsTargetForgetsAnOldCantReach(t *testing.T) {
	m := NewMob(TypeVillager, 1, 0.5, 64, 0.5, 1)
	m.Village = fakeVillage{w: newTestWorld()}
	m.vb.jobSite = &GlobalPos{Dim: "minecraft:overworld", X: 3, Y: 64, Z: 0}
	m.GameTime = 5000
	g := &poiWalkGoal{m: m, slot: slotJob, act: actWork, close: 9, far: 100, giveUp: 1200, cantReach: 100}
	m.vb.activity = actWork
	if g.CanStart(&tickView{w: newTestWorld()}) {
		t.Error("a villager at its job site started a walk")
	}
	if m.vb.jobSite == nil || g.cantReach != 0 {
		t.Errorf("job site %v, cantReach %d; want the site kept and cantReach cleared", m.vb.jobSite, g.cantReach)
	}
}

func TestWorkingAtTheSiteRestocksAndTellsAnOpenWindow(t *testing.T) {
	for _, trading := range []bool{true, false} {
		m := NewMob(TypeVillager, 1, 0.5, 64, 0.5, 1)
		m.OfferUpdater = testOfferUpdater{}
		m.Offers = nbt.Compound{"Recipes": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{recipe(3, 12, 0)}}}
		m.Trading = trading
		m.GameTime = 500
		view := &tickView{}
		(&workAtPoiGoal{m: m}).Start(view)
		if u, _ := recipeOf(m, 0); u != 0 {
			t.Errorf("trading %v: uses %d after the work, want a restock", trading, u)
		}
		told := false
		for _, ev := range view.events {
			told = told || ev.Kind == MobOffersChanged
		}
		if told != trading {
			t.Errorf("trading %v: offers change told %v", trading, told)
		}
	}
}

func (v fakeVillage) Claimed(x, y, z int32) bool {
	for _, p := range v.pois {
		if p.X == x && p.Y == y && p.Z == z {
			return true
		}
	}
	return false
}

// SectionsToVillage is the Chebyshev section distance to the nearest listed POI.
func (v fakeVillage) SectionsToVillage(x, y, z int32, limit int) int {
	best := limit + 1
	for _, p := range v.pois {
		best = min(best, int(max(abs32(p.X>>4-x>>4), abs32(p.Y>>4-y>>4), abs32(p.Z>>4-z>>4))))
	}
	return best
}

func TestStructureVillagerAssignsADistantJobOnlyOnItsFirstBrainTick(t *testing.T) {
	w := newTestWorld()
	w.set(10, 64, 0, defaultState(t, "minecraft:lectern"))
	m := NewMob(TypeVillager, 1, 0.5, 64, 0.5, 1)
	m.LoadFrom(nbt.Compound{"AssignProfessionWhenSpawned": nbt.Byte(1)}, w, func() int64 { return 0 })
	m.Village = fakeVillage{w: w}
	m.vb.potentialJobSite = &GlobalPos{Dim: "minecraft:overworld", X: 10, Y: 64, Z: 0}
	saved := nbt.Compound{}
	m.SaveTo(saved, nil)
	if !nbt.BoolField(saved, "AssignProfessionWhenSpawned") {
		t.Fatal("save lost pending structure assignment")
	}
	m.villagerBrainStep(&tickView{w: w})
	if m.Villager.Profession != "minecraft:librarian" || m.vb.jobSite == nil {
		t.Fatal("structure villager did not assign distant workstation")
	}
	if m.vb.assignProfessionWhenSpawned {
		t.Fatal("structure assignment lasted beyond the first brain tick")
	}
	saved = nbt.Compound{}
	m.SaveTo(saved, nil)
	if _, ok := saved["AssignProfessionWhenSpawned"]; ok {
		t.Fatal("completed assignment was saved")
	}
	m.vb.jobSite = nil
	m.vb.potentialJobSite = &GlobalPos{Dim: "minecraft:overworld", X: 10, Y: 64, Z: 0}
	m.assignProfession(&tickView{w: w})
	if m.vb.jobSite != nil {
		t.Fatal("later assignment skipped walking to the workstation")
	}
}

func TestStructureAssignmentWaitsForAIAndStillRequiresThisDimension(t *testing.T) {
	w := newTestWorld()
	m := NewMob(TypeVillager, 1, 0.5, 64, 0.5, 1)
	m.LoadFrom(nbt.Compound{"AssignProfessionWhenSpawned": nbt.Byte(1), "NoAI": nbt.Byte(1)}, w, nil)
	m.Village = fakeVillage{w: w}
	m.Tick(w, nil)
	if !m.vb.assignProfessionWhenSpawned {
		t.Fatal("NoAI cleared pending assignment")
	}
	m.vb.potentialJobSite = &GlobalPos{Dim: "minecraft:the_nether", X: 10, Y: 64, Z: 0}
	m.assignProfession(&tickView{w: w})
	if m.vb.jobSite != nil {
		t.Fatal("structure assignment used a different dimension")
	}
	m.NoAI = false
	m.Tick(w, nil)
	if m.vb.assignProfessionWhenSpawned {
		t.Fatal("first AI tick retained assignment flag")
	}
}

func TestVillagerWorkSoundProfessionOrder(t *testing.T) {
	if len(villagerWorkSounds) != len(villagerProfessions) {
		t.Fatal("profession sound table length")
	}
	for i, name := range villagerProfessions {
		sound := villagerWorkSounds[i]
		if name == "minecraft:none" || name == "minecraft:nitwit" {
			if sound != registry.SoundNone {
				t.Fatalf("%s has a work sound", name)
			}
		} else if sound.Name() != "minecraft:entity.villager.work_"+name[len("minecraft:"):] {
			t.Fatalf("%s work sound = %s", name, sound.Name())
		}
	}
}
