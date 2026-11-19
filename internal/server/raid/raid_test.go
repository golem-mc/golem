package raid

import (
	"testing"

	"github.com/google/uuid"
)

type fakeRaider struct {
	id     uuid.UUID
	alive  bool
	health float64
	wave   int
	raid   *Raid
	leader bool
	kind   string
}

func (f *fakeRaider) UUID() uuid.UUID             { return f.id }
func (f *fakeRaider) Alive() bool                 { return f.alive }
func (f *fakeRaider) Health() float64             { return f.health }
func (f *fakeRaider) Position() (x, y, z float64) { return 0, 64, 0 }
func (f *fakeRaider) Age() int                    { return 0 }
func (f *fakeRaider) NoActionTicks() int          { return 0 }
func (f *fakeRaider) TicksOutside() int           { return 0 }
func (f *fakeRaider) SetTicksOutside(int)         {}
func (f *fakeRaider) Enlist(r *Raid, wave int)    { f.raid, f.wave = r, wave }
func (f *fakeRaider) Leave()                      { f.raid = nil }
func (f *fakeRaider) die(r *Raid)                 { f.alive = false; r.RemoveFromRaid(f.wave, f, false) }

type fakeLevel struct {
	difficulty uint8
	village    bool
	canSpawn   bool
	spawned    []*fakeRaider
	heroes     map[uuid.UUID]int
	omens      int
	horns      int
	bar        []BarAction
	bigKinds   bool
	rides      [][2]*fakeRaider
}

func newLevel(difficulty uint8) *fakeLevel {
	return &fakeLevel{difficulty: difficulty, village: true, canSpawn: true, heroes: map[uuid.UUID]int{}}
}

func (l *fakeLevel) Difficulty() uint8                          { return l.difficulty }
func (l *fakeLevel) ChunkLoaded(x, z int) bool                  { return true }
func (l *fakeLevel) IsVillage(x, y, z int) bool                 { return l.village }
func (l *fakeLevel) SurfaceY(x, z int) int                      { return 64 }
func (l *fakeLevel) CanSpawnAt(x, y, z int) bool                { return l.canSpawn }
func (l *fakeLevel) Players() []Player                          { return []Player{{UUID: uuid.New(), Alive: true}} }
func (l *fakeLevel) Hero(id uuid.UUID, amp int)                 { l.heroes[id] = amp }
func (l *fakeLevel) Omen(uuid.UUID)                             { l.omens++ }
func (l *fakeLevel) Horn(uuid.UUID, float64, float64, int64)    { l.horns++ }
func (l *fakeLevel) SendBar(_ []Viewer, a BarAction, _ BarView) { l.bar = append(l.bar, a) }
func (l *fakeLevel) Spawn(kind string, x, y, z, wave int, leader bool, _ float32) Raider {
	if (kind == Evoker || kind == Ravager) && !l.bigKinds {
		return nil
	}
	f := &fakeRaider{id: uuid.New(), alive: true, health: 24, kind: kind, leader: leader}
	l.spawned = append(l.spawned, f)
	return f
}

func (l *fakeLevel) Ride(vehicle, rider Raider) {
	l.rides = append(l.rides, [2]*fakeRaider{vehicle.(*fakeRaider), rider.(*fakeRaider)})
}

func (l *fakeLevel) count(kind string) int {
	n := 0
	for _, f := range l.spawned {
		if f.kind == kind {
			n++
		}
	}
	return n
}

// omenRaid makes the raid a player's omen of the amplifier starts at the origin.
func omenRaid(l *fakeLevel, amplifier int) (*Raids, *Raid, Player) {
	rs := New(l)
	p := Player{UUID: uuid.New(), Alive: true}
	return rs, rs.CreateOrExtend(p, amplifier, Pos{0, 64, 0}, nil), p
}

func tick(rs *Raids, n int) {
	for range n {
		rs.Tick(true)
	}
}

// vanilla: Raid.addWaveMob
func TestRestoringARaiderReplacesWaveEntry(t *testing.T) {
	l := newLevel(difficultyNormal)
	_, r, _ := omenRaid(l, 0)
	original := &fakeRaider{id: uuid.New(), alive: true, health: 24}
	other := &fakeRaider{id: uuid.New(), alive: true, health: 24}
	r.Join(1, original)
	r.Join(1, other)
	for range 3 {
		restored := &fakeRaider{id: original.id, alive: true, health: 12}
		r.Restore(1, restored)
		if len(r.groups[1]) != 2 || r.groups[1][0] != restored || r.groups[1][1] != other {
			t.Fatalf("wave = %v, want the restored reference and the other raider", r.groups[1])
		}
		if restored.raid != r || restored.wave != 1 {
			t.Fatal("restored raider was not enlisted in wave one")
		}
		if r.totalHealth != 48 || r.Progress() != 0.75 {
			t.Fatalf("total health = %v, progress = %v; want 48 and 0.75", r.totalHealth, r.Progress())
		}
	}
	// A saved raid has health but no entity references until its chunks load.
	r.groups = map[int][]Raider{}
	r.Restore(1, original)
	if r.totalHealth != 48 || r.Alive() != 1 || r.Progress() != 0.5 {
		t.Fatalf("restoring an empty wave: health %v, alive %d, progress %v", r.totalHealth, r.Alive(), r.Progress())
	}
}

// TestWavesPerDifficulty: easy runs three waves, normal five, hard seven.
// vanilla: Raid.getNumGroups
func TestWavesPerDifficulty(t *testing.T) {
	for difficulty, want := range map[uint8]int{0: 0, 1: 3, 2: 5, 3: 7} {
		if got := NumGroups(difficulty); got != want {
			t.Errorf("NumGroups(%d) = %d, want %d", difficulty, got, want)
		}
	}
}

// TestFirstWaveCompositionPerDifficulty: wave one is four pillagers, plus the
// difficulty's bonus: up to one vindicator on easy, exactly zero or one more
// on normal and up to two on hard.
// vanilla: Raid.getPotentialBonusSpawns, Raid.RaiderType
func TestFirstWaveCompositionPerDifficulty(t *testing.T) {
	for _, tc := range []struct {
		difficulty uint8
		maxBonus   int
	}{{1, 1}, {2, 1}, {3, 2}} {
		seen := map[int]bool{}
		for range 60 {
			l := newLevel(tc.difficulty)
			rs, _, _ := omenRaid(l, 0)
			tick(rs, 310)
			pillagers := l.count(Pillager)
			if pillagers < 4 || pillagers > 4+tc.maxBonus || l.count(Vindicator) > tc.maxBonus || l.count(Witch) != 0 {
				t.Fatalf("difficulty %d: %d pillagers, %d vindicators", tc.difficulty, pillagers, l.count(Vindicator))
			}
			seen[pillagers-4] = true
		}
		if len(seen) != tc.maxBonus+1 {
			t.Errorf("difficulty %d: bonus pillagers seen %v, want 0..%d", tc.difficulty, seen, tc.maxBonus)
		}
	}
}

// TestOneCaptainPerWave: the first raider able to lead gets the banner.
// vanilla: Raid.spawnGroup
func TestOneCaptainPerWave(t *testing.T) {
	l := newLevel(2)
	rs, r, _ := omenRaid(l, 0)
	tick(rs, 310)
	captains := 0
	for _, f := range l.spawned {
		if f.leader {
			captains++
		}
	}
	if captains != 1 || r.Leader(1) == nil {
		t.Errorf("captains = %d, leader = %v", captains, r.Leader(1))
	}
	if l.horns == 0 {
		t.Errorf("no horn at the wave")
	}
}

// TestTheKillerOfARaiderIsAHero: the players who killed raiders get Hero of
// the Village at the omen level less one when the raid is won.
// vanilla: Raid.tick, Raid.addHeroOfTheVillage
func TestTheKillerOfARaiderIsAHero(t *testing.T) {
	l := newLevel(1)
	rs, r, _ := omenRaid(l, 1)
	hero := uuid.New()
	r.AddHero(hero)
	for wave := 1; wave <= 4; wave++ {
		tick(rs, 310)
		for _, f := range l.spawned {
			if f.alive {
				f.die(r)
			}
		}
	}
	tick(rs, 50)
	if r.Status() != Victory {
		t.Fatalf("status = %v after %d waves, want victory", r.Status(), r.GroupsSpawned())
	}
	if amp, ok := l.heroes[hero]; !ok || amp != 1 {
		t.Errorf("heroes = %v, want %v at amplifier 1", l.heroes, hero)
	}
	if r.GroupsSpawned() != 4 {
		t.Errorf("waves = %d, want the three of easy and a bonus wave", r.GroupsSpawned())
	}
}

// TestNoSpawnPositionStopsTheRaid: when no wave position is found after six
// tries the raid stops.
// vanilla: Raid.tick
func TestNoSpawnPositionStopsTheRaid(t *testing.T) {
	l := newLevel(2)
	l.canSpawn = false
	rs, r, _ := omenRaid(l, 0)
	tick(rs, 310)
	if r.Status() != Stopped || len(rs.All()) != 0 {
		t.Errorf("status = %v raids = %d, want stopped and gone", r.Status(), len(rs.All()))
	}
}

// TestTheOmenLevelCaps: a raid absorbs omens up to level five.
// vanilla: Raid.absorbRaidOmen
func TestTheOmenLevelCaps(t *testing.T) {
	l := newLevel(2)
	rs, r, p := omenRaid(l, 3)
	rs.CreateOrExtend(p, 3, Pos{0, 64, 0}, nil)
	if r.OmenLevel() != MaxOmenLevel || len(rs.All()) != 1 {
		t.Errorf("omen level = %d raids = %d, want 5 in one raid", r.OmenLevel(), len(rs.All()))
	}
}

// TestRaidsSurviveAReload: the saved compound reads back as the same raid.
// vanilla: Raids.CODEC
func TestRaidsSurviveAReload(t *testing.T) {
	l := newLevel(2)
	rs, r, _ := omenRaid(l, 2)
	hero := uuid.New()
	r.AddHero(hero)
	tick(rs, 40)
	again := New(l)
	again.Decode(rs.Encode())
	if len(again.All()) != 1 {
		t.Fatalf("reloaded %d raids", len(again.All()))
	}
	for _, got := range again.All() {
		if got.Center() != r.Center() || got.OmenLevel() != 3 || got.cooldown != r.cooldown || got.ticksActive != r.ticksActive ||
			len(got.heroes) != 1 || got.heroes[0] != hero || got.numGroups != 5 {
			t.Errorf("reloaded raid = %+v, saved %+v", got, r)
		}
	}
}

// TestTheCountdownMovesTheBar: the bar sends a progress for each countdown tick.
// vanilla: ServerBossEvent.setProgress
func TestTheCountdownMovesTheBar(t *testing.T) {
	l := newLevel(2)
	rs, _, _ := omenRaid(l, 0)
	tick(rs, 25)
	progress := 0
	for _, a := range l.bar {
		if a == BarProgress {
			progress++
		}
	}
	if progress != 25 {
		t.Errorf("progress packets in 25 countdown ticks = %d, want one per tick", progress)
	}
}

// TestRavagerRiders: the normal raid's last wave seats a pillager on each
// ravager; the hard raid's seventh wave an evoker on the first and
// vindicators on the rest.
// vanilla: Raid.spawnGroup
func TestRavagerRiders(t *testing.T) {
	for _, tc := range []struct {
		difficulty uint8
		wave       int
		first      string
		rest       string
	}{{2, 5, Pillager, Pillager}, {3, 7, Evoker, Vindicator}, {3, 3, "", ""}} {
		l := newLevel(tc.difficulty)
		l.bigKinds = true
		_, r, _ := omenRaid(l, 0)
		for r.GroupsSpawned() < tc.wave-1 {
			r.spawnGroup(Pos{0, 64, 0})
		}
		l.rides = nil
		r.spawnGroup(Pos{0, 64, 0})
		if tc.first == "" {
			if len(l.rides) != 0 {
				t.Fatalf("wave %d seated %d riders", tc.wave, len(l.rides))
			}
			continue
		}
		if len(l.rides) == 0 {
			t.Fatalf("difficulty %d wave %d seated no rider", tc.difficulty, tc.wave)
		}
		for i, ride := range l.rides {
			want := tc.rest
			if i == 0 {
				want = tc.first
			}
			if ride[0].kind != Ravager || ride[1].kind != want {
				t.Errorf("ride %d: %s on %s, want %s on a ravager", i, ride[1].kind, ride[0].kind, want)
			}
		}
	}
}
