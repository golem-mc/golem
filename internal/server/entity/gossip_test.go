package entity

import (
	"testing"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

var (
	gossipA = uuid.MustParse("00000000-0000-0000-0000-00000000000a")
	gossipB = uuid.MustParse("00000000-0000-0000-0000-00000000000b")
)

func TestGossipAddClampsAndDropsSmallValues(t *testing.T) {
	g := Gossips{}
	g.add(gossipA, GossipMajorPositive, 15)
	g.add(gossipA, GossipMajorPositive, 15)
	if v := g[gossipA][GossipMajorPositive]; v != 20 {
		t.Errorf("clamped value = %d, want 20", v)
	}
	g.add(gossipB, GossipTrading, 1)
	if _, ok := g[gossipB]; ok {
		t.Error("a value below 2 left its target behind")
	}
	g.add(gossipB, GossipTrading, 2)
	if g[gossipB][GossipTrading] != 2 {
		t.Errorf("trading = %d, want 2", g[gossipB][GossipTrading])
	}
}

func TestGossipReputationWeights(t *testing.T) {
	g := Gossips{gossipA: {10, 20, 3, 4, 5}}
	if got, want := g.Reputation(gossipA), int32(-50-20+3+20+5); got != want {
		t.Errorf("reputation = %d, want %d", got, want)
	}
	if g.Reputation(gossipB) != 0 {
		t.Error("an unknown target has a reputation")
	}
}

func TestGossipDecaysByTheDay(t *testing.T) {
	g := Gossips{gossipA: {25, 0, 25, 20, 2}}
	g.decay()
	if got := g[gossipA]; got != [5]int32{15, 0, 24, 20, 0} {
		t.Errorf("after a day = %v", got)
	}
}

func TestGossipTransferDecaysAndKeepsTheMax(t *testing.T) {
	src := Gossips{gossipA: {0, 0, 0, 0, 21}, gossipB: {11, 0, 0, 0, 0}}
	dst := Gossips{gossipA: {0, 0, 0, 0, 30}}
	dst.transferFrom(src, func(n int) int { return n - 1 }, 3)
	if got := dst[gossipA][GossipTrading]; got != 30 {
		t.Errorf("trading = %d, want the larger stored 30", got)
	}
	if _, ok := dst[gossipB]; ok {
		t.Errorf("a value decayed below 2 was stored: %v", dst[gossipB])
	}
	boundary := Gossips{}
	boundary.transferFrom(Gossips{gossipA: {0, 0, 0, 0, 25}, gossipB: {0, 0, 0, 25, 0}}, func(int) int { return 25 }, 1)
	if _, ok := boundary[gossipA]; ok {
		t.Errorf("boundary choice 25 drew the entry before it: %v", boundary[gossipA])
	}
	if got := boundary[gossipB][GossipMajorPositive]; got != 5 {
		t.Errorf("boundary major_positive = %d, want 5", got)
	}
	fresh := Gossips{}
	fresh.transferFrom(Gossips{gossipA: {0, 0, 0, 0, 22}}, func(int) int { return 1 }, 10)
	if got := fresh[gossipA][GossipTrading]; got != 2 {
		t.Errorf("transferred trading = %d, want 2", got)
	}
}

func TestVillagersGossipOncePerCooldownOnBothSides(t *testing.T) {
	a := NewMob(TypeVillager, 1, 0, 64, 0, 1)
	b := NewMob(TypeVillager, 2, 1, 64, 0, 2)
	a.GameTime, b.GameTime = 1200, 1200
	b.OnReputationEvent(ReputationVillagerKilled, gossipA)
	a.gossipWith(b)
	if a.PlayerReputation(gossipA) == 0 {
		t.Fatal("no gossip passed")
	}
	if a.vg.lastGossip != 1200 || b.vg.lastGossip != 1200 {
		t.Errorf("cooldowns %d, %d", a.vg.lastGossip, b.vg.lastGossip)
	}
	b.OnReputationEvent(ReputationVillagerHurt, gossipB)
	a.GameTime, b.GameTime = 2399, 2399
	b.gossipWith(a)
	if a.PlayerReputation(gossipB) != 0 || b.PlayerReputation(gossipB) == 0 {
		t.Error("gossip passed inside the cooldown")
	}
	a.GameTime, b.GameTime = 2400, 2400
	a.gossipWith(b)
	if a.PlayerReputation(gossipB) == 0 {
		t.Error("no gossip after the cooldown")
	}
}

func TestVillagerGossipRoundTripsAndSkipsUnknownTypes(t *testing.T) {
	m := NewMob(TypeVillager, 1, 0.5, 64, 0.5, 1)
	m.OnReputationEvent(ReputationVillagerKilled, gossipA)
	m.OnReputationEvent(ReputationTrade, gossipB)
	m.vg.lastDecay = 555
	tag := nbt.Compound{}
	m.SaveTo(tag, func() int64 { return 0 })
	list := tag["Gossips"].(nbt.List)
	list.Items = append(list.Items,
		nbt.Compound{"Target": nbt.UUIDToIntArray(gossipB), "Type": nbt.String("bogus"), "Value": nbt.Int(9)},
		nbt.Compound{"Target": nbt.UUIDToIntArray(gossipB), "Type": nbt.String("trading"), "Value": nbt.Int(0)})
	tag["Gossips"] = list

	l := NewMob(TypeVillager, 2, 0.5, 64, 0.5, 2)
	l.LoadFrom(tag, newTestWorld(), func() int64 { return 0 })
	if l.vg.lastDecay != 555 || len(l.vg.gossips) != 2 ||
		l.vg.gossips[gossipA][GossipMajorNegative] != 25 || l.vg.gossips[gossipB][GossipTrading] != 2 {
		t.Errorf("loaded %v, lastDecay %d", l.vg.gossips, l.vg.lastDecay)
	}
	empty := NewMob(TypeVillager, 3, 0, 64, 0, 3)
	out := nbt.Compound{}
	empty.SaveTo(out, func() int64 { return 0 })
	if l, ok := out["Gossips"].(nbt.List); !ok || len(l.Items) != 0 {
		t.Errorf("no gossips saved as %v", out["Gossips"])
	}
}

func TestAVillagerReportsAnAttackerOnceAPlayerMatches(t *testing.T) {
	w := flatWorld(t)
	players := []Player{{EntityID: 7, UUID: gossipA, X: 1, Y: 64, Z: 0}}
	m := NewMob(TypeVillager, 1, 0, 64, 0, 1)
	m.NoAI = true
	m.vg.hurtBy = 7
	events := m.tick(w, players, nil)
	if m.PlayerReputation(gossipA) != -25 || m.vg.hurtBy != 0 {
		t.Errorf("reputation %d, hurtBy %d", m.PlayerReputation(gossipA), m.vg.hurtBy)
	}
	angry := false
	for _, e := range events {
		angry = angry || e.Kind == MobStatus && e.Status == protocol.EntityStatusVillagerAngry
	}
	if !angry {
		t.Error("no angry status")
	}
	n := NewMob(TypeVillager, 2, 0, 64, 0, 2)
	n.NoAI = true
	n.vg.hurtBy = 99
	n.tick(w, players, nil)
	if len(n.vg.gossips) != 0 || n.vg.hurtBy != 0 {
		t.Errorf("an unknown attacker gave %v, hurtBy %d", n.vg.gossips, n.vg.hurtBy)
	}
}

func TestAMurderedVillagerTellsOnlyWitnessesInView(t *testing.T) {
	w := flatWorld(t)
	players := []Player{{EntityID: 7, UUID: gossipA}}
	victim := NewMob(TypeVillager, 1, 0, 64, 0, 1)
	victim.blowAttacker = 7
	near := NewMob(TypeVillager, 2, 10, 64, 0, 2)
	far := NewMob(TypeVillager, 3, 20, 64, 0, 3)
	victim.tellWitnesses(w, players, []*Mob{victim, near, far})
	if near.PlayerReputation(gossipA) != -125 {
		t.Errorf("witness reputation %d, want -125", near.PlayerReputation(gossipA))
	}
	if len(far.vg.gossips) != 0 {
		t.Errorf("a far villager heard: %v", far.vg.gossips)
	}
}

// vanilla: Villager.maybeDecayGossip
func TestAVillagerDecaysGossipOncePerDayAfterItsFirstTick(t *testing.T) {
	m := NewMob(TypeVillager, 1, 0, 64, 0, 1)
	m.vg.gossips = Gossips{}
	m.vg.gossips.add(gossipA, GossipTrading, 25)
	for _, c := range []struct {
		now  int64
		want int32
	}{{100, 25}, {100 + gossipDayTicks - 1, 25}, {100 + gossipDayTicks, 23}} {
		m.GameTime = c.now
		m.villagerGossipStep(&tickView{})
		if got := m.vg.gossips[gossipA][GossipTrading]; got != c.want {
			t.Fatalf("trading = %d at tick %d, want %d", got, c.now, c.want)
		}
	}
}
