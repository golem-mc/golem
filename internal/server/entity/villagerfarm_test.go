package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	_ "github.com/golem-mc/golem/internal/server/datapack"
	"github.com/golem-mc/golem/registry"
)

func farmStack(name string, n int32) protocol.ItemStack {
	return protocol.ItemStack{ItemID: registry.MustItem("minecraft:" + name), Count: n}
}

func farmer(t *testing.T) (*Mob, carryWorld, Waypoint) {
	t.Helper()
	w := carryWorld{testWorld: newTestWorld()}
	floor(w.testWorld, -6, -6, 6, 6, 63, defaultState(t, "minecraft:stone"))
	m := NewMob(TypeVillager, 1, 0.5, 64, 0.5, 2162)
	m.Villager.Profession, m.vb.activity = "minecraft:farmer", actWork
	m.Village = fakeVillage{w: w.testWorld}
	return m, w, Waypoint{0, 64, 0}
}

func TestVillagerFarmHarvestAndPlantAllSeeds(t *testing.T) {
	for _, tc := range []struct{ seed, crop string }{
		{"wheat_seeds", "wheat"}, {"beetroot_seeds", "beetroots"}, {"carrot", "carrots"},
		{"potato", "potatoes"}, {"torchflower_seeds", "torchflower_crop"}, {"pitcher_pod", "pitcher_crop"},
	} {
		t.Run(tc.seed, func(t *testing.T) {
			m, w, p := farmer(t)
			w.set(0, 63, 0, defaultState(t, "minecraft:farmland"))
			m.barter.Inventory[0] = farmStack(tc.seed, 2)
			r := m.Farm(w, FarmHarvest, p)
			if !r.Changed || r.State != defaultState(t, "minecraft:"+tc.crop) || m.barter.Inventory[0].Count != 1 {
				t.Fatalf("plant = %+v, inventory %v", r, m.barter.Inventory)
			}
			w.set(0, 64, 0, r.State)
			if again := m.Farm(w, FarmHarvest, p); again.Changed || m.barter.Inventory[0].Count != 1 {
				t.Fatal("spent a second seed on the occupied cell")
			}
		})
	}
	m, w, p := farmer(t)
	ripe := registry.MustState("minecraft:wheat", map[string]string{"age": "7"})
	w.set(0, 64, 0, ripe)
	if !m.Farm(w, FarmHarvest, p).Break {
		t.Fatal("ripe wheat was not harvested")
	}
	if m.Farm(w.testWorld, FarmHarvest, p).Break {
		t.Fatal("harvest ignored mob_griefing=false")
	}
	m.Villager.Profession = "minecraft:librarian"
	if m.Farm(w, FarmHarvest, p).Break {
		t.Fatal("a librarian harvested wheat")
	}
}

func TestVillagerFarmSensorAndWorkGoals(t *testing.T) {
	m, w, p := farmer(t)
	w.set(4, 66, 4, defaultState(t, "minecraft:farmland"))
	w.set(5, 64, 0, defaultState(t, "minecraft:farmland"))
	v := &tickView{w: w}
	m.farmlandStep(v)
	if len(m.vb.farmland) != 1 || m.vb.farmland[0] != (Waypoint{4, 66, 4}) {
		t.Fatalf("secondary POIs %v", m.vb.farmland)
	}
	w.set(4, 66, 4, 0)
	for range 39 {
		m.farmlandStep(v)
	}
	if len(m.vb.farmland) != 1 {
		t.Fatal("sensor ran before 40 ticks")
	}
	m.farmlandStep(v)
	if len(m.vb.farmland) != 0 {
		t.Fatal("sensor retained a removed farmland")
	}
	w.set(0, 63, 0, defaultState(t, "minecraft:farmland"))
	m.vb.farmScan = 0
	m.farmlandStep(v)
	g := &farmGoal{m: m, action: FarmHarvest}
	m.GameTime = 100
	for tries := 0; !g.CanStart(v); tries++ {
		if tries > 1000 {
			t.Fatal("harvest never selected")
		}
	}
	g.Start(v)
	g.Tick(v)
	if len(v.events) != 1 || v.events[0].Kind != MobFarm || v.events[0].Farm != FarmHarvest {
		t.Fatalf("harvest events %v", v.events)
	}
	g.Stop(v)
	if g.next != 140 {
		t.Fatal("missing harvest stop cooldown")
	}
	m.vb.activity = actRest
	if g.CanStart(v) || g.ShouldContinue(v) {
		t.Fatal("harvest ran outside WORK")
	}
	m.vb.activity, m.Villager.Profession = actWork, "minecraft:librarian"
	m.farmlandStep(v)
	if len(m.vb.farmland) != 0 {
		t.Fatal("a non-farmer kept secondary POIs")
	}
	// UseBonemeal is in every working profession's package, even with griefing off.
	w.set(p.X, p.Y, p.Z, defaultState(t, "minecraft:beetroots"))
	m.barter.Inventory[0] = farmStack("bone_meal", 2)
	g = &farmGoal{m: m, action: FarmBonemeal}
	v.w, m.Age = w.testWorld, 200
	for tries := 0; !g.CanStart(v); tries++ {
		if tries > 1000 {
			t.Fatal("bone meal never selected")
		}
	}
	g.Start(v)
	if m.MainHand.ItemID != registry.MustItem("minecraft:bone_meal") {
		t.Fatal("bone meal was not displayed")
	}
	g.Tick(v)
	if g.next != 140 || !m.Farm(v.w, FarmBonemeal, p).Bonemeal || m.barter.Inventory[0].Count != 1 {
		t.Fatal("bone meal use/cycle failed")
	}
	g.Stop(v)
	if !m.MainHand.Empty() || g.CanStart(v) {
		t.Fatal("bone meal hand/session cooldown was not cleared/set")
	}
	m.Age = 361 // the other selector parity can start after the session cooldown
	for tries := 0; !g.CanStart(v); tries++ {
		if tries > 1000 {
			t.Fatal("odd selection parity never offered bone meal after cooldown")
		}
	}
	w.set(0, 64, 0, registry.MustState("minecraft:beetroots", map[string]string{"age": "3"}))
	if m.Farm(w, FarmBonemeal, p).Bonemeal || m.barter.Inventory[0].Count != 1 {
		t.Fatal("bone meal was spent on a ripe crop")
	}
}
