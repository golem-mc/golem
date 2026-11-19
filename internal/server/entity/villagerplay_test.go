package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// vanilla: ShowTradesToPlayer
func TestAVillagerShowsWhatItSellsForTheHeldItem(t *testing.T) {
	w := flatWorld(t)
	m := NewMob(TypeVillager, 1, 0, 64, 0, 1)
	m.Villager.Profession = "minecraft:librarian"
	m.Offers = nbt.Compound{"Recipes": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
		nbt.Compound{"buy": nbt.Compound{"id": nbt.String("minecraft:emerald"), "count": nbt.Int(1)},
			"sell": nbt.Compound{"id": nbt.String("minecraft:paper"), "count": nbt.Int(24)}, "maxUses": nbt.Int(16)},
		nbt.Compound{"buy": nbt.Compound{"id": nbt.String("minecraft:emerald"), "count": nbt.Int(1)},
			"sell": nbt.Compound{"id": nbt.String("minecraft:book"), "count": nbt.Int(1)}, "maxUses": nbt.Int(1), "uses": nbt.Int(1)},
	}}}
	emerald, _ := registry.ItemID("minecraft:emerald")
	paper, _ := registry.ItemID("minecraft:paper")
	players := []Player{{EntityID: 7, X: 2, Y: 64, Z: 0, HeldItem: protocol.ItemStack{ItemID: emerald, Count: 1}}}
	m.vb.activity = actIdle
	shown := false
	for tick := range 20 {
		m.GameTime = int64(tick)
		m.tick(w, players, nil)
		shown = shown || m.MainHand.ItemID == paper && m.MainHand.Count == 24
	}
	if !shown {
		t.Errorf("never held the paper, hand %v", m.MainHand)
	}
	if m.MainHand.ItemID == 0 && m.Unhappy != 0 {
		t.Error("unhappy without a refusal")
	}
}

// vanilla: Villager.spawnGolemIfNeeded, GolemSensor
func TestVillagersThatSleptAskForAGolemUnlessOneIsNear(t *testing.T) {
	asked := func(m *Mob, others []*Mob, need int) bool {
		tick := &tickView{others: others}
		m.spawnGolemIfNeeded(tick, need)
		for _, e := range tick.events {
			if e.Kind == MobWantsGolem {
				return true
			}
		}
		return false
	}
	var herd []*Mob
	for i := range 5 {
		v := NewMob(TypeVillager, int32(i+1), float64(i), 64, 0, int64(i+1))
		v.GameTime = 5000
		v.vb.lastSlept = 4000
		herd = append(herd, v)
	}
	if !asked(herd[0], herd[1:], 5) {
		t.Error("five villagers that slept did not ask")
	}
	herd[4].vb.lastSlept = 5000 - golemSleepWindow
	if asked(herd[0], herd[1:], 5) || !asked(herd[0], herd[1:], 3) {
		t.Error("four wanting villagers: five must not ask, three must")
	}
	herd[1].GolemDetected(5000)
	if asked(herd[1], herd, 1) {
		t.Error("a villager that saw a golem asked")
	}
	golem := NewMob(TypeIronGolem, 9, 1, 64, 1, 9)
	herd[2].golemStep(&tickView{others: []*Mob{golem}})
	if herd[2].WantsGolem() {
		t.Error("a golem in the box did not set the memory")
	}
}

// vanilla: SetClosestHomeAsWalkTarget
func TestAHomelessVillagerAtRestWalksToTheClosestReachableBed(t *testing.T) {
	w := newTestWorld()
	floor(w, -12, -12, 12, 12, 63, defaultState(t, "minecraft:stone"))
	w.set(8, 64, 0, bedHead(t))
	m := NewMob(TypeVillager, 1, 0.5, 64, 0.5, 1)
	m.Village = fakeVillage{w: w, pois: []Waypoint{{8, 64, 0}}}
	m.vb.activity = actRest
	g := &homeSearchGoal{m: m}
	m.GameTime = 100
	if !g.CanStart(&tickView{w: w}) {
		t.Fatal("did not start toward a bed 8 blocks off")
	}
	g.Start(&tickView{w: w})
	if len(m.navOf().nav.path) == 0 {
		t.Error("no path set")
	}
	m.vb.home = &GlobalPos{}
	if g.ShouldContinue(&tickView{w: w}) {
		t.Error("kept walking after claiming a home")
	}
}
