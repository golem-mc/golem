package entity

import (
	"math"
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func TestStriderLooksAtANearbyStrider(t *testing.T) {
	a := NewMob(TypeStrider, 1, 0, 64, 0, 1)
	b := NewMob(TypeStrider, 2, 0, 64, 5, 2)
	var g *lookAtStrider
	for _, goal := range striderGoals(a) {
		if l, ok := goal.(*lookAtStrider); ok {
			g = l
		}
	}
	if g == nil {
		t.Fatal("striderGoals has no lookAtStrider")
	}
	a.Rand = rand.New(rand.NewSource(1))
	view := &tickView{others: []*Mob{a, b}}
	started := false
	for range 500 {
		if g.CanStart(view) {
			started = true
			break
		}
	}
	if !started || g.target != b {
		t.Fatalf("started=%v target=%v, want a glance at the other strider", started, g.target)
	}
	far := &tickView{others: []*Mob{a, NewMob(TypeStrider, 3, 0, 64, 20, 3)}}
	for range 500 {
		if g.CanStart(far) {
			t.Fatal("glanced at a strider 20 blocks away")
		}
	}
}

func TestLavaCarriesAStriderOnlyAsASource(t *testing.T) {
	w := spawnTestWorld{}
	source, flowing := stateOf(t, "minecraft:lava", "level=0"), stateOf(t, "minecraft:lava", "level=3")
	if got := lavaStandBoxes(w, source, 0, 63, 0, 64); len(got) == 0 {
		t.Error("a source gives no footing")
	}
	if got := lavaStandBoxes(w, flowing, 0, 63, 0, 64); len(got) != 0 {
		t.Errorf("flowing lava gives footing %v", got)
	}
}

func stateOf(t *testing.T, name, props string) uint16 {
	t.Helper()
	for _, s := range registry.AllStates[name] {
		if s.Props == props {
			return s.ID
		}
	}
	t.Fatalf("no %s[%s]", name, props)
	return 0
}

func TestStridersSpawnOnlyInLava(t *testing.T) {
	w := newSpawnTestWorld()
	w.biomeName = "minecraft:nether_wastes"
	w.dimension = "minecraft:the_nether"
	const y = 31
	bx, bz := atChunk(0, 0)
	netherrack := defaultState(t, "minecraft:netherrack")
	lava := defaultState(t, "minecraft:lava")
	for dx := int32(-12); dx <= 12; dx++ {
		for dz := int32(-12); dz <= 12; dz++ {
			w.set(bx+dx, y-1, bz+dz, netherrack)
			w.set(bx+dx, y, bz+dz, lava)
		}
	}
	players := []SpawnPlayer{{EntityID: 1, X: float64(bx) + 40.5, Y: float64(y), Z: float64(bz) + 0.5}}
	striders := 0
	for seed := int64(1); seed <= 300; seed++ {
		var bodies []SpawnBody
		spawnCategoryForPosition(w, readSpawnPoint(w), rand.New(rand.NewSource(seed)), ChunkPos{}, y, bx, bz,
			players, &bodies, &SpawnPotential{}, &SpawnCounts{PerPlayer: make(map[int32]int)}, CategoryCreature, func(r SpawnRequest) {
				if r.Type != TypeStrider {
					return
				}
				striders++
				if !rules.IsLava(w.BlockAt(int32(math.Floor(r.X)), int32(r.Y), int32(math.Floor(r.Z)))) {
					t.Errorf("strider requested at (%v, %v, %v), outside lava", r.X, r.Y, r.Z)
				}
			})
	}
	if striders == 0 {
		t.Error("no strider requested over a lava lake in the nether wastes")
	}
	if striderSpawnOk(w, bx, y-1, bz) || striderSpawnOk(w, bx, y+1, bz) {
		t.Error("a strider may spawn in netherrack or in the air over the lake")
	}
	w.set(bx, y+1, bz, netherrack)
	if striderSpawnOk(w, bx, y, bz) {
		t.Error("a strider may spawn in lava roofed by a solid block")
	}
}

// The jockey rolls come from the level's dice, the rider's whole finalize
// draws next, and only then the strider's follow-range triangle and
// left-handedness.
// vanilla: Strider.finalizeSpawn, Strider.spawnJockey, Mob.finalizeSpawn
func TestAStriderRollsItsJockeyOnTheLevelDiceBeforeItsFollowRange(t *testing.T) {
	biome := testBiome("minecraft:nether_wastes")
	local := rules.LocalDifficulty{Special: 0.5}
	counts := map[MobType]int{}
	for seed := int64(1); seed <= 600; seed++ {
		m := NewMob(TypeStrider, 1, 0, 64, 0, 7)
		m.FinalizeSpawn(GoDice{rand.New(rand.NewSource(seed))}, biome, nil, local)
		replay := GoDice{rand.New(rand.NewSource(seed))}
		piglin := replay.NextIntN(30) == 0
		var want *Mob
		switch {
		case piglin:
			want = NewMob(TypeZombifiedPiglin, 0, 0, 64, 0, m.lootSeed^striderRiderSalt)
			group := &SpawnGroup{rng: replay, claimed: true, zombie: true, noChicken: true}
			group.baby = replay.NextFloat() < 0.05
			want.finalizeSpawn(replay, biome, group, local, false, false)
		case replay.NextIntN(10) == 0:
			want = NewMob(TypeStrider, 0, 0, 64, 0, m.lootSeed^striderRiderSalt)
			want.SetBaby(true)
			want.followSpawnBonus = followSpawnSpread * (replay.NextDouble() - replay.NextDouble())
			want.LeftHanded = replay.NextFloat() < 0.05
		}
		bonus := followSpawnSpread * (replay.NextDouble() - replay.NextDouble())
		if m.followSpawnBonus != bonus {
			t.Fatalf("seed %d: follow-range bonus %v, want %v", seed, m.followSpawnBonus, bonus)
		}
		if want == nil {
			if m.Jockey != JockeyNone || m.Rider != nil {
				t.Fatalf("seed %d: jockey %v without a roll", seed, m.Jockey)
			}
			continue
		}
		r := m.Rider
		if m.Jockey != JockeyBuiltRider || r == nil || r.Type != want.Type {
			t.Fatalf("seed %d: jockey %v rider %v, want a %s", seed, m.Jockey, r, want.Type)
		}
		counts[r.Type]++
		if r.followSpawnBonus != want.followSpawnBonus || r.LeftHanded != want.LeftHanded ||
			r.IsBaby() != want.IsBaby() || r.CanPickUpLoot != want.CanPickUpLoot {
			t.Fatalf("seed %d: %s rider bonus %v left %v baby %v loot %v, want %v %v %v %v", seed, r.Type,
				r.followSpawnBonus, r.LeftHanded, r.IsBaby(), r.CanPickUpLoot,
				want.followSpawnBonus, want.LeftHanded, want.IsBaby(), want.CanPickUpLoot)
		}
		if piglin {
			if registry.Item(r.MainHand.ItemID) != registry.ItemWarpedFungusOnAStick || m.Horse.Saddle.Empty() || r.Jockey != JockeyNone {
				t.Fatalf("seed %d: piglin holds %v, saddle %v, jockey %v", seed, r.MainHand, m.Horse.Saddle, r.Jockey)
			}
		}
		b := NewMob(TypeStrider, 2, 0, 64, 0, seed)
		b.SetBaby(true)
		b.FinalizeSpawn(GoDice{rand.New(rand.NewSource(seed))}, biome, nil, local)
		if b.Jockey != JockeyNone {
			t.Fatalf("baby strider rolled jockey %v", b.Jockey)
		}
	}
	if counts[TypeZombifiedPiglin] == 0 || counts[TypeStrider] == 0 {
		t.Errorf("riders over 600 striders: %v", counts)
	}
}

func TestItemSteeringBoostRunsLongAndTakesNoSecond(t *testing.T) {
	for _, kind := range []MobType{TypeStrider, TypePig} {
		t.Run(kind.Name(), func(t *testing.T) {
			m := NewMob(kind, 1, 0, 64, 0, 1)
			if !m.ItemSteeringBoost() {
				t.Fatal("first boost refused")
			}
			total := int(m.ItemSteeringBoostTime())
			if total < 140 || total > 980 {
				t.Fatalf("boost length %d, want 140 to 980", total)
			}
			if m.ItemSteeringBoost() {
				t.Error("a second boost took while one runs")
			}
			for range total + 2 {
				m.itemSteeringBoostStep()
			}
			if !m.ItemSteeringBoost() {
				t.Error("boost still running after its length")
			}
		})
	}
}

func TestAStriderPathsOverALavaLake(t *testing.T) {
	w := newTestWorld()
	stone := defaultState(t, "minecraft:stone")
	lava := defaultState(t, "minecraft:lava")
	floor(w, -10, -10, 20, 10, 63, stone)
	for x := int32(4); x <= 12; x++ {
		for z := int32(-4); z <= 4; z++ {
			w.set(x, 63, z, lava)
			w.set(x, 62, z, lava)
		}
	}
	m := NewMob(TypeStrider, 1, 0.5, 64, 0.5, 1)
	if !m.seekTo(w, 8.5, 63, 0.5, 1, navReach) {
		t.Fatal("no path to the middle of the lava lake")
	}
	path := m.nav.path
	if last := path[len(path)-1]; last.X < 7 || last.Y != 63 {
		t.Errorf("the path ends at %v, want a lava cell from x=7 at y=63", last)
	}
	// A grown mob that cannot stand on lava finds no way across.
	h := NewMob(TypeHoglin, 2, 0.5, 64, 0.5, 1)
	if h.seekTo(w, 8.5, 63, 0.5, 1, navReach) && len(h.nav.path) > 0 && h.nav.path[len(h.nav.path)-1].X >= 8 {
		t.Error("a hoglin found a way onto the lava")
	}
}

func TestStriderWalkTargetValue(t *testing.T) {
	w := newTestWorld()
	w.set(0, 63, 0, defaultState(t, "minecraft:lava"))
	m := NewMob(TypeStrider, 1, 5.5, 64, 5.5, 1)
	if got := m.walkTargetValue(w, Waypoint{0, 63, 0}); got != 10 {
		t.Errorf("lava is worth %v, want 10", got)
	}
	if got := m.walkTargetValue(w, Waypoint{3, 63, 3}); got != 0 {
		t.Errorf("dry ground is worth %v, want 0", got)
	}
	w.set(5, 64, 5, defaultState(t, "minecraft:lava"))
	m.updateMobFluids(w)
	if got := m.walkTargetValue(w, Waypoint{3, 63, 3}); !math.IsInf(got, -1) {
		t.Errorf("dry ground is worth %v to a strider in lava, want -Inf", got)
	}
}

func TestStriderVoiceFollowsItsMind(t *testing.T) {
	count := func(running Goal, ticks int) (sounds map[string]int) {
		m := NewMob(TypeStrider, 1, 0, 64, 0, 7)
		m.ai.running = []Goal{running}
		sounds = map[string]int{}
		for range ticks {
			for _, e := range m.striderVoiceStep(nil) {
				sounds[e.SoundID.Name()]++
			}
		}
		return sounds
	}
	probe := NewMob(TypeStrider, 1, 0, 64, 0, 7)
	if probe.Sounds().Ambient.Name() == "" {
		t.Fatal("an idle strider has no ambient sound")
	}
	if got := count(&randomStroll{m: probe}, 3000); len(got) != 0 {
		t.Errorf("a strolling strider sounded %v", got)
	}
	panicker := NewMob(TypeStrider, 1, 0, 64, 0, 7)
	panicker.ai.running = []Goal{&panicGoal{m: panicker}}
	if panicker.Sounds().Ambient.Name() != "" {
		t.Error("a panicking strider keeps its ambient sound")
	}
	if got := count(&panicGoal{m: panicker}, 3000); got["minecraft:entity.strider.retreat"] == 0 || len(got) != 1 {
		t.Errorf("a panicking strider sounded %v, want retreat calls", got)
	}
	lured := NewMob(TypeStrider, 1, 0, 64, 0, 7)
	lured.ai.running = []Goal{&temptGoal{m: lured}}
	if lured.Sounds().Ambient.Name() != "" {
		t.Error("a tempted strider keeps its ambient sound")
	}
	if got := count(&temptGoal{m: lured}, 6000); got["minecraft:entity.strider.happy"] == 0 || len(got) != 1 {
		t.Errorf("a tempted strider sounded %v, want happy chirps", got)
	}
}

func TestAStriderInLavaSeeksNoLava(t *testing.T) {
	w := newTestWorld()
	stone := defaultState(t, "minecraft:stone")
	lava := defaultState(t, "minecraft:lava")
	floor(w, -10, -10, 20, 10, 63, stone)
	for x := int32(3); x <= 9; x++ {
		for z := int32(-4); z <= 4; z++ {
			w.set(x, 63, z, lava)
		}
	}
	dry := NewMob(TypeStrider, 1, 0.5, 64, 0.5, 1)
	if !newStriderGoToLavaGoal(dry).CanStart(&tickView{w: w}) {
		t.Error("a strider on dry ground beside lava does not seek it")
	}
	wet := NewMob(TypeStrider, 2, 5.5, 63.5, 0.5, 1)
	wet.updateMobFluids(w)
	if newStriderGoToLavaGoal(wet).CanStart(&tickView{w: w}) {
		t.Error("a strider in lava seeks lava")
	}
}

func TestAStriderPackRollsNoBabies(t *testing.T) {
	group := NewSpawnGroup(GoDice{rand.New(rand.NewSource(7))})
	for i := range 2000 {
		m := NewMob(TypeStrider, int32(i+1), 0, 64, 0, int64(i))
		m.FinalizeSpawn(GoDice{m.Rand}, testBiome("minecraft:nether_wastes"), group, rules.LocalDifficulty{})
		if m.IsBaby() {
			t.Fatalf("pack member %d is a baby; a strider replaces the pack data", i)
		}
	}
}

func TestStriderLavaTargetFollowsLandPathfinding(t *testing.T) {
	lava := defaultState(t, "minecraft:lava")
	state := func(name string, props map[string]string) uint16 {
		s, ok := registry.FindState(name, props)
		if !ok {
			t.Fatalf("no state %s %v", name, props)
		}
		return s
	}
	cases := []struct {
		name  string
		above uint16
		want  bool
	}{
		{"air", 0, true},
		{"lava", lava, false},
		{"stone", defaultState(t, "minecraft:stone"), false},
		{"slab", state("minecraft:stone_slab", map[string]string{"type": "bottom", "waterlogged": "false"}), false},
		{"fence", defaultState(t, "minecraft:oak_fence"), false},
		{"closed door", defaultState(t, "minecraft:oak_door"), false},
		{"open trapdoor", state("minecraft:oak_trapdoor", map[string]string{"facing": "north", "half": "bottom", "open": "true", "powered": "false", "waterlogged": "false"}), true},
		{"shelf", defaultState(t, "minecraft:oak_shelf"), false},
		{"copper golem statue", defaultState(t, "minecraft:copper_golem_statue"), false},
		{"iron bars", defaultState(t, "minecraft:iron_bars"), false},
		{"copper bars", defaultState(t, "minecraft:exposed_copper_bars"), false},
		{"glass pane", defaultState(t, "minecraft:glass_pane"), false},
		{"stained pane", defaultState(t, "minecraft:red_stained_glass_pane"), false},
		{"pointed dripstone", defaultState(t, "minecraft:pointed_dripstone"), false},
		{"carpet", defaultState(t, "minecraft:white_carpet"), true},
	}
	for _, c := range cases {
		w := newTestWorld()
		w.set(0, 64, 0, lava)
		w.set(0, 65, 0, c.above)
		if got := striderLavaTarget(w, 0, 64, 0); got != c.want {
			t.Errorf("%s over lava: valid target %v, want %v", c.name, got, c.want)
		}
	}
}

func TestAStriderAutonomouslyStrollsAcrossLava(t *testing.T) {
	w := newTestWorld()
	floor(w, -24, -24, 24, 24, 62, defaultState(t, "minecraft:stone"))
	floor(w, -24, -24, 24, 24, 63, defaultState(t, "minecraft:lava"))
	m := NewMob(TypeStrider, 1, 0.5, 63.5, 0.5, 1)
	stepped := false
	for range 800 {
		m.NoActionTicks = 0
		for _, ev := range m.Tick(w, nil) {
			if ev.Kind == MobSounded && ev.SoundID == registry.SoundEntityStriderStepLava && ev.Volume == 1 {
				stepped = true
			}
		}
		if math.Hypot(m.X-0.5, m.Z-0.5) > 3 && stepped {
			return
		}
	}
	t.Fatalf("idle strider reached (%g, %g, %g), lava steps=%v", m.X, m.Y, m.Z, stepped)
}

func TestABabyStridersPassengerAttachment(t *testing.T) {
	m := NewMob(TypeStrider, 1, 0, 64, 0, 1)
	for _, tc := range []struct {
		baby bool
		y    float64
	}{{false, 1.7}, {true, 0.65625}} {
		m.SetBaby(tc.baby)
		x, y, z := vehiclePassengerPoint(m, 0)
		if x != 0 || y != tc.y || z != 0 {
			t.Errorf("baby=%v: passenger point (%g, %g, %g), want (0, %g, 0)", tc.baby, x, y, z, tc.y)
		}
	}
}

func TestAStridersColdAttributeControlsItsPace(t *testing.T) {
	w := newTestWorld()
	m := NewMob(TypeStrider, 1, 0.5, 64, 0.5, 1)
	a := m.BuildAttributes(nbt.List{})
	m.SetAttributes(a)
	a.SetBase(registry.AttributeMovementSpeed, 0.5)
	m.striderColdStep(w)
	want := 0.5 * (1 + striderColdSpeed)
	if got := m.pace(); math.Abs(got-want) > 1e-9 {
		t.Errorf("cold pace=%g, want %g with the edited base", got, want)
	}
	w.set(0, 64, 0, defaultState(t, "minecraft:lava"))
	m.striderColdStep(w)
	if got := m.pace(); got != 0.5 {
		t.Errorf("warm pace=%g, want 0.5", got)
	}
}

func TestAStriderTakesOneDamageFromWaterOrRain(t *testing.T) {
	for _, tc := range []struct {
		name              string
		water, rain, roof bool
		damage            float64
	}{
		{"water", true, false, false, 1}, {"rain", false, true, false, 1}, {"roof", false, true, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newTestWorld()
			w.rain = tc.rain
			if tc.water {
				w.set(0, 64, 0, defaultState(t, "minecraft:water"))
			}
			if tc.roof {
				w.set(0, 67, 0, defaultState(t, "minecraft:stone"))
			}
			m := NewMob(TypeStrider, 1, 0.5, 64, 0.5, 1)
			m.NoAI = true
			m.Tick(w, nil)
			if got := m.MaxHealth - m.Health; got != tc.damage {
				t.Errorf("damage=%g, want %g", got, tc.damage)
			}
		})
	}
}
