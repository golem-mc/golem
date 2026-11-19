package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// vanilla: YieldJobSite
func TestAJoblessVillagerYieldsItsSiteToAFittingNeighbour(t *testing.T) {
	w := newTestWorld()
	floor(w, -6, -6, 6, 6, 63, defaultState(t, "minecraft:stone"))
	w.set(1, 64, 0, defaultState(t, "minecraft:lectern"))
	view := fakeVillage{w: w, pois: []Waypoint{{1, 64, 0}}}
	a := NewMob(TypeVillager, 1, 0.5, 64, 0.5, 1)
	a.Villager.Profession = "minecraft:none"
	b := NewMob(TypeVillager, 2, 0.5, 64, 2.5, 2)
	b.Villager.Profession = "minecraft:librarian"
	a.Village, b.Village = view, view
	a.vb.potentialJobSite = &GlobalPos{Dim: view.Dimension(), X: 1, Y: 64, Z: 0}
	a.yieldJobSite(&tickView{w: w, others: []*Mob{b}})
	if a.vb.potentialJobSite != nil || b.vb.potentialJobSite == nil || b.vb.potentialJobSite.at() != (Waypoint{1, 64, 0}) {
		t.Errorf("a %v, b %v", a.vb.potentialJobSite, b.vb.potentialJobSite)
	}
}

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

// vanilla: Villager.tick
func TestAnUnhappyVillagerCountsDown(t *testing.T) {
	m := NewMob(TypeVillager, 1, 0, 64, 0, 1)
	m.NoAI = true
	m.Unhappy = 2
	m.villagerGossipStep(&tickView{})
	m.villagerGossipStep(&tickView{})
	m.villagerGossipStep(&tickView{})
	if m.Unhappy != 0 {
		t.Errorf("unhappy %d", m.Unhappy)
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

// vanilla: VillagerPanicTrigger.tick
func TestTwoFleeingVillagersRequestAGolemOnTheSameTick(t *testing.T) {
	var herd []*Mob
	for i := range 3 {
		m := NewMob(TypeVillager, int32(i+1), float64(i), 64, 0, int64(i+1))
		m.GameTime, m.vb.lastSlept, m.vb.golemScan = 5000, 4000, 100
		herd = append(herd, m)
	}
	view := &tickView{others: herd}
	for _, m := range herd[:2] {
		m.vb.danger.hurt = true
		m.golemStep(view)
	}
	if len(view.events) != 2 {
		t.Fatalf("requests %d, want 2", len(view.events))
	}
	for _, m := range herd {
		m.GolemDetected(5000)
	}
	for _, ev := range view.events {
		if ev.Mob.WantsGolem() {
			t.Fatal("queued asker still wants a golem after another request succeeds")
		}
	}
}

// vanilla: NearestBedSensor
func TestAChildRemembersAReachableBedBeyondSixteenBlocks(t *testing.T) {
	w := newTestWorld()
	floor(w, -4, -4, 30, 4, 63, defaultState(t, "minecraft:stone"))
	blocked, reachable := Waypoint{4, 64, 0}, Waypoint{17, 64, 0}
	w.set(blocked.X, blocked.Y, blocked.Z, bedHead(t))
	w.set(reachable.X, reachable.Y, reachable.Z, bedHead(t))
	for x := int32(2); x <= 6; x++ {
		for z := int32(-2); z <= 2; z++ {
			if x == 2 || x == 6 || z == -2 || z == 2 {
				for y := int32(64); y <= 67; y++ {
					w.set(x, y, z, defaultState(t, "minecraft:stone"))
				}
			}
		}
	}
	m := NewMob(TypeVillager, 1, 0.5, 64, 0.5, 1)
	m.BabyAge = -24000
	m.Village = fakeVillage{w: w, pois: []Waypoint{blocked, reachable}}
	m.GameTime, m.DayTime = 100, 100
	view := &tickView{w: w}
	m.villagerBrainStep(view)
	if p := m.vb.nearestBed; p == nil || *p != reachable {
		t.Fatalf("nearest bed %v, want %v", p, reachable)
	}
	g := &jumpOnBedGoal{m: m}
	started := false
	for range 100 {
		if g.CanStart(view) {
			started = true
			break
		}
	}
	if !started || g.bed != reachable {
		t.Fatalf("jump target %v, started %v", g.bed, started)
	}
	// The sensor does not replace the memory between its 20-tick scans.
	m.GameTime++
	m.villagerBrainStep(view)
	if p := m.vb.nearestBed; p == nil || *p != reachable {
		t.Fatalf("lost nearest-bed memory: %v", p)
	}
}

func TestVillagerPlayerGoalsLoseTargetsBetweenSelections(t *testing.T) {
	for _, kind := range []string{"gift disconnect", "gift effect lost", "gift out of range", "gift hidden", "trade disconnect"} {
		t.Run(kind, func(t *testing.T) {
			w := flatWorld(t)
			m := NewMob(TypeVillager, 1, 0, 64, 0, 1)
			m.vb.activity = actIdle
			view := &tickView{w: w, players: []Player{{EntityID: 7, X: 2, Y: 64, Hero: true}}}
			var g Goal = &giveGiftGoal{m: m}
			if kind == "trade disconnect" {
				g = &showTradesGoal{m: m}
			}
			sel := goalSelector{goals: []Goal{g}}
			m.Age = 1
			sel.run(m, view)
			if !sel.isRunning(g) {
				t.Fatal("goal did not start")
			}
			switch kind {
			case "gift effect lost":
				view.players[0].Hero = false
			case "gift out of range":
				view.players[0].X = 30
			case "gift hidden":
				for y := int32(64); y < 67; y++ {
					w.set(1, y, 0, defaultState(t, "minecraft:stone"))
				}
			default:
				view.players = nil
			}
			if g.ShouldContinue(view) {
				t.Fatal("target was not invalidated")
			}
			m.Age = 2 // the intervening every-tick update does not call ShouldContinue
			sel.run(m, view)
			if !sel.isRunning(g) {
				t.Fatal("did not exercise the intervening tick")
			}
		})
	}
}

// vanilla: GolemSensor, Sensor.tick
func TestVillagerGolemSensorScansEveryTwoHundredTicks(t *testing.T) {
	m := NewMob(TypeVillager, 1, 0, 64, 0, 1)
	golem := NewMob(TypeIronGolem, 2, 1, 64, 0, 2)
	m.GameTime = 1000
	m.golemStep(&tickView{}) // start a sensor interval without a golem
	view := &tickView{others: []*Mob{golem}}
	for tick := 1; tick <= golemScanRate; tick++ {
		m.GameTime++
		m.golemStep(view)
		if tick < golemScanRate && m.vb.golemUntil != 0 {
			t.Fatalf("scanned early at tick %d", tick)
		}
	}
	if m.vb.golemUntil != m.GameTime+golemMemory {
		t.Fatal("did not detect golem at tick 200")
	}
}

func TestAVillagerRetriesDeferredOffersWithTheSameHeldItem(t *testing.T) {
	m := NewMob(TypeVillager, 1, 0, 64, 0, 1)
	emerald, _ := registry.ItemID("minecraft:emerald")
	paper, _ := registry.ItemID("minecraft:paper")
	view := &tickView{players: []Player{{EntityID: 7, X: 2, Y: 64, Z: 0,
		HeldItem: protocol.ItemStack{ItemID: emerald, Count: 1}}}}
	requested := 0
	m.DrawTrades = func(*Mob) { requested++ }
	g := &showTradesGoal{m: m, player: 7, left: 40, lookTime: 40}
	g.Tick(view)
	if requested != 1 || g.haveHeld {
		t.Fatal("missing deferred draw or same-item retry")
	}
	// The server's apply phase publishes the result between entity ticks.
	m.Offers = nbt.Compound{"Recipes": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
		nbt.Compound{"buy": nbt.Compound{"id": nbt.String("minecraft:emerald"), "count": nbt.Int(1)},
			"sell": registry.StackTag(protocol.ItemStack{ItemID: paper, Count: 24}), "maxUses": nbt.Int(16)},
	}}}
	g.Tick(view)
	if requested != 1 || m.MainHand.ItemID != paper || m.MainHand.Count != 24 {
		t.Fatalf("deferred offer not shown: requests %d, hand %+v", requested, m.MainHand)
	}
}

func TestAJoblessVillagerStopsRetryingAfterEmptyOffersArePublished(t *testing.T) {
	m := NewMob(TypeVillager, 1, 0, 64, 0, 1)
	m.Villager.Profession = "minecraft:none"
	emerald, _ := registry.ItemID("minecraft:emerald")
	view := &tickView{players: []Player{{EntityID: 7, X: 2, Y: 64, Z: 0,
		HeldItem: protocol.ItemStack{ItemID: emerald, Count: 1}}}}
	requested := 0
	m.DrawTrades = func(*Mob) { requested++ }
	g := &showTradesGoal{m: m, player: 7, left: 40, lookTime: 40}
	g.Tick(view)
	if requested != 1 {
		t.Fatal("missing initial deferred draw")
	}
	// The apply phase publishes an empty list when the profession has no trade set.
	m.Offers = nbt.Compound{"Recipes": nbt.List{Elem: nbt.TypeCompound}}
	g.Tick(view)
	g.Tick(view)
	if requested != 1 || !g.haveHeld {
		t.Fatalf("empty offers retried: requests %d, held %v", requested, g.haveHeld)
	}
}
