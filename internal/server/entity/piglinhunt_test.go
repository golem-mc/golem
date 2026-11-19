package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// huntWorld is a nether floor with the given piglins and hoglins on it, the
// mobs at x = 0.5 + 2i.
func huntWorld(t *testing.T, piglins, hoglins int) (testWorld, []*Mob, []*Mob) {
	t.Helper()
	w := newTestWorld()
	w.nether = true
	floor64(w, t)
	var ps, hs []*Mob
	id := int32(1)
	for i := range piglins {
		ps = append(ps, NewMob(TypePiglin, id, -3.5+float64(i), 64, -2.5, int64(id)))
		id++
	}
	for i := range hoglins {
		hs = append(hs, NewMob(TypeHoglin, id, -3.5+float64(i), 64, 2.5, int64(id)))
		id++
	}
	return w, ps, hs
}

func runHunt(w testWorld, mobs []*Mob, ticks int) (events []MobEvent) {
	for range ticks {
		for _, m := range mobs {
			events = append(events, m.tick(w, nil, mobs)...)
		}
	}
	return events
}

func all(ps, hs []*Mob) []*Mob { return append(append([]*Mob{}, ps...), hs...) }

func TestAPiglinTakesAnAdultHoglinAsItsTargetAndRests(t *testing.T) {
	w, ps, hs := huntWorld(t, 1, 1)
	runHunt(w, all(ps, hs), 40)
	if ps[0].TargetID != hs[0].ID {
		t.Fatalf("target %d, want the hoglin %d", ps[0].TargetID, hs[0].ID)
	}
	if ps[0].barter.huntRest < 560 {
		t.Errorf("hunt rest %d, want 30 seconds less the ticks run", ps[0].barter.huntRest)
	}
}

func TestTheHuntCallsTheAdultPiglinsAround(t *testing.T) {
	w, ps, hs := huntWorld(t, 3, 1)
	runHunt(w, all(ps, hs), 40)
	for i, p := range ps {
		if p.TargetID != hs[0].ID {
			t.Errorf("piglin %d target %d, want the hoglin", i, p.TargetID)
		}
		if p.barter.huntRest == 0 {
			t.Errorf("piglin %d did not rest from hunting", i)
		}
	}
}

func TestAPiglinRetreatsFromHoglinsThatOutnumberIt(t *testing.T) {
	w, ps, hs := huntWorld(t, 1, 3)
	ps[0].barter.huntRest = 5000
	ps[0].retaliate = hs[0].ID
	events := runHunt(w, all(ps, hs), 4)
	if ps[0].barter.retreat == 0 || ps[0].barter.from != hs[0].ID {
		t.Fatalf("retreat %d from %d, want a retreat from the hoglin", ps[0].barter.retreat, ps[0].barter.from)
	}
	if ps[0].TargetID != 0 {
		t.Errorf("the retreating piglin targets %d", ps[0].TargetID)
	}
	if ps[0].barter.retreat < piglinRetreatMin-5 {
		t.Errorf("retreat %d under the 5 second minimum", ps[0].barter.retreat)
	}
	var retreatSound bool
	for _, ev := range events {
		retreatSound = retreatSound || ev.Kind == MobSounded && ev.SoundID.Name() == "minecraft:entity.piglin.retreat"
	}
	if !retreatSound {
		t.Error("no retreat sound")
	}
}

func TestAStruckPiglinCallsTheOthersAfterItsAttacker(t *testing.T) {
	w, ps, _ := huntWorld(t, 2, 0)
	attacker := Player{X: -3.5, Y: 64, Z: 0.5, EntityID: 77, Huntable: true}
	ps[0].retaliate = attacker.EntityID
	for range 4 {
		for _, m := range ps {
			m.tick(w, []Player{attacker}, ps)
		}
	}
	if ps[1].TargetID != attacker.EntityID {
		t.Errorf("the second piglin targets %d, want the attacker", ps[1].TargetID)
	}
}

func holdGold(m *Mob) {
	m.OffHand = protocol.ItemStack{ItemID: int32(registry.ItemGoldIngot), Count: 1}
	m.barter.admiring = piglinAdmireTicks
}

func TestAStruckBruteCallsPiglinsAndAStruckPiglinCallsBrutes(t *testing.T) {
	attacker := Player{X: -3.5, Y: 64, Z: 0.5, EntityID: 77, Huntable: true}
	for _, striker := range []MobType{TypePiglin, TypePiglinBrute} {
		w, _, _ := huntWorld(t, 0, 0)
		other := TypePiglinBrute
		if striker == TypePiglinBrute {
			other = TypePiglin
		}
		a := NewMob(striker, 1, -3.5, 64, -2.5, 1)
		b := NewMob(other, 2, -2.5, 64, -2.5, 2)
		a.retaliate = attacker.EntityID
		mobs := []*Mob{a, b}
		for range 4 {
			for _, m := range mobs {
				m.tick(w, []Player{attacker}, mobs)
			}
		}
		if b.TargetID != attacker.EntityID {
			t.Errorf("a struck %s did not call the %s: target %d", striker, other, b.TargetID)
		}
	}
}
