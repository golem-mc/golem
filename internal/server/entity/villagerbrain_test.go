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
