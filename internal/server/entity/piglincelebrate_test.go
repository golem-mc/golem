package entity

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
)

func TestAPiglinCelebratesWhereItsTargetDied(t *testing.T) {
	w, ps, hs := huntWorld(t, 1, 1)
	ps[0].TargetID = hs[0].ID
	hs[0].Health = 0
	events := runHunt(w, []*Mob{ps[0], hs[0]}, 1)
	b := &ps[0].barter
	if b.celebrate == 0 || ps[0].TargetID != 0 {
		t.Fatalf("celebrate %d target %d, want a celebration and no target", b.celebrate, ps[0].TargetID)
	}
	if b.x != -4 || b.z != 2 {
		t.Errorf("celebrates at %d,%d, want the hoglin's block -4,2", b.x, b.z)
	}
	var sound bool
	for _, ev := range events {
		sound = sound || ev.Kind == MobSounded && ev.SoundID.Name() == "minecraft:entity.piglin.celebrate"
	}
	if !sound {
		t.Error("no celebrate sound")
	}
	runHunt(w, []*Mob{ps[0]}, 60)
	if d := Dist2(ps[0].X, ps[0].Y, ps[0].Z, -3.5, 64, 2.5); d > 9 {
		t.Errorf("piglin is %v from the kill after 60 ticks", d)
	}
}

func TestPiglinDanceUsesGameTimeWithoutConsumingMobRandom(t *testing.T) {
	// java.util.Random(gameTime).nextFloat(), matching SingleThreadedRandomSource.
	for _, tc := range []struct {
		time  int64
		dance bool
	}{
		{0, false}, {1, false}, {4096, true}, {5000, true},
		{12345, false}, {-1, false}, {1<<32 + 4096, false},
		// nextFloat is 0.099999964; nextDouble would be 0.100000011.
		{13860562717892, true},
	} {
		t.Run(fmt.Sprint(tc.time), func(t *testing.T) {
			for _, hoglin := range []bool{true, false} {
				for _, seed := range []int64{1, 9, 42} {
					m := NewMob(TypePiglin, 1, 0, 64, 0, seed)
					m.SetMetadataGameTime(tc.time)
					m.Rand = rand.New(rand.NewSource(seed))
					wantRandom := rand.New(rand.NewSource(seed))
					for range 3 {
						m.endCelebration()
						m.celebrateAt(-.5, 64, 2.5, hoglin)
						if got, want := m.Dancing(), hoglin && tc.dance; got != want {
							t.Errorf("hoglin=%v seed=%d: dancing=%v, want %v", hoglin, seed, got, want)
						}
						if m.barter.celebrate != piglinCelebrateTicks {
							t.Error("kill did not start a celebration")
						}
						if got, want := m.Rand.Int63(), wantRandom.Int63(); got != want {
							t.Fatalf("celebration consumed the mob random: got %d, want %d", got, want)
						}
					}
				}
			}
		})
	}
}

func TestADanceAndItsCelebrationExpireAfterThreeHundredTicks(t *testing.T) {
	w, ps, _ := huntWorld(t, 1, 0)
	m := ps[0]
	m.barter.celebrate = piglinCelebrateTicks
	m.SetMetadataBarterDance(piglinCelebrateTicks)
	runHunt(w, ps, piglinCelebrateTicks-1)
	if !m.Dancing() || !m.celebrating() {
		t.Fatal("the celebration ended before its 300 ticks")
	}
	runHunt(w, ps, 1)
	if m.Dancing() || m.celebrating() {
		t.Error("the celebration outlived its 300 ticks")
	}
}

func TestADeadPlayerTargetEndsOnlyUnderForgiveDeadPlayers(t *testing.T) {
	for _, forgive := range []bool{true, false} {
		tw, ps, _ := huntWorld(t, 1, 0)
		w := carryWorld{testWorld: tw, off: map[world.GameRuleIndex]bool{world.RuleForgiveDeadPlayers: !forgive}}
		m := ps[0]
		m.TargetID = 50
		dead := []Player{{EntityID: 50, X: 0.5, Y: 64, Z: 0.5, Dead: true}}
		var sounded bool
		for range 2 {
			for _, ev := range m.tick(w, dead, ps) {
				sounded = sounded || ev.Kind == MobSounded && ev.SoundID.Name() == "minecraft:entity.piglin.celebrate"
			}
		}
		if got := m.TargetID == 0; got != forgive {
			t.Errorf("forgive_dead_players=%v: target cleared = %v", forgive, got)
		}
		if sounded != forgive {
			t.Errorf("forgive_dead_players=%v: celebrate sound = %v", forgive, sounded)
		}
		if m.barter.celebrate == 0 {
			t.Errorf("forgive_dead_players=%v: no celebration remembered", forgive)
		}
	}
}

func TestABlowEndsACelebrationUnlessItsFromAPiglin(t *testing.T) {
	cases := []struct {
		name      string
		attacker  func(ps, hs []*Mob) (int32, []Player)
		wantEnded bool
	}{
		{"hoglin", func(_, hs []*Mob) (int32, []Player) { return hs[0].ID, nil }, true},
		{"piglin", func(ps, _ []*Mob) (int32, []Player) { return ps[1].ID, nil }, false},
		{"creative player", func(_, _ []*Mob) (int32, []Player) {
			return 60, []Player{{EntityID: 60, X: -3.5, Y: 64, Z: -1.5}}
		}, true},
	}
	for _, c := range cases {
		w, ps, hs := huntWorld(t, 2, 1)
		m := ps[0]
		m.celebrateAt(0, 64, 0, false)
		id, players := c.attacker(ps, hs)
		m.retaliate = id
		m.tick(w, players, all(ps, hs))
		if ended := m.barter.celebrate == 0; ended != c.wantEnded {
			t.Errorf("blow from a %s: celebration ended = %v, want %v", c.name, ended, c.wantEnded)
		}
	}
}

func TestAnAlertAloneLeavesACelebrationRunning(t *testing.T) {
	w, ps, hs := huntWorld(t, 1, 1)
	m := ps[0]
	m.celebrateAt(0, 64, 0, false)
	m.alerted = hs[0].ID
	m.tick(w, nil, all(ps, hs))
	if m.barter.celebrate == 0 {
		t.Error("a neighbour's alert ended the celebration")
	}
}

func TestPiglinCelebrationInvalidatesOnlyDanceBoundaries(t *testing.T) {
	m := NewMob(TypePiglin, 7, .5, 64, .5, 11)
	m.SetMetadataGameTime(4096)
	m.MetadataRevision()
	m.ClearMetadataDirty()
	m.celebrateAt(0, 64, 0, true)
	if !m.Dancing() || !m.MetadataDirty().Has(MobMetadataDancing) {
		t.Fatal("celebration did not invalidate dancing")
	}
	revision := m.MetadataRevision()
	m.ClearMetadataDirty()
	m.celebrateStep(&tickView{})
	if m.MetadataRevision() != revision || m.MetadataDirty() != (MobMetadataMask{}) {
		t.Fatal("duration-only countdown invalidated dancing")
	}
	m.SetMetadataBarterDance(1)
	revision = m.MetadataRevision()
	m.ClearMetadataDirty()
	m.celebrateStep(&tickView{})
	if m.Dancing() || m.MetadataRevision() != revision+1 || !m.MetadataDirty().Has(MobMetadataDancing) {
		t.Fatal("dance expiry did not invalidate dancing")
	}
}
