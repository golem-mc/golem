package entity

import (
	"math"
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

func TestPhantomScanUsesTargetVisibility(t *testing.T) {
	armour := [4]protocol.ItemStack{
		headPiece(t, "minecraft:iron_boots"), headPiece(t, "minecraft:iron_leggings"),
		headPiece(t, "minecraft:iron_chestplate"), headPiece(t, "minecraft:iron_helmet"),
	}
	for _, tc := range []struct {
		name     string
		player   Player
		distance float64
		want     bool
	}{
		{"visible inside", Player{}, 63.9, true},
		{"visible outside", Player{}, 64.1, false},
		{"sneaking inside", Player{Sneaking: true}, 51.1, true},
		{"sneaking outside", Player{Sneaking: true}, 51.3, false},
		{"invisible bare inside", Player{Invisible: true}, 4.4, true},
		{"invisible bare outside", Player{Invisible: true}, 4.6, false},
		{"invisible sneaking inside", Player{Invisible: true, Sneaking: true}, 3.5, true},
		{"invisible sneaking outside", Player{Invisible: true, Sneaking: true}, 3.7, false},
		{"invisible armoured inside", Player{Invisible: true, Worn: armour}, 44.7, true},
		{"invisible armoured outside", Player{Invisible: true, Worn: armour}, 44.9, false},
		{"allied", Player{Team: "friends"}, 4, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewMob(TypePhantom, 1, 0.5, 100, 0.5, 1)
			m.Team = "friends"
			p := tc.player
			p.EntityID, p.Huntable = 2, true
			p.X, p.Y, p.Z = m.X, m.Y-tc.distance, m.Z
			tick := &tickView{w: newTestWorld(), players: []Player{p}}
			if got := m.phantomScan(tick) != nil; got != tc.want {
				t.Errorf("scan found player = %v, want %v at %v blocks", got, tc.want, tc.distance)
			}
			// DEFAULT has no range: invisibility does not end an existing hunt.
			if got, want := m.phantomCanAttack(tick, &p), p.Team != m.Team; got != want {
				t.Errorf("can continue attacking = %v, want %v", got, want)
			}
		})
	}
}

// phantomSpawnWorld is a spawnTestWorld that can be a dimension without sky
// light.
type phantomSpawnWorld struct {
	spawnTestWorld
	noSky bool
}

func (w phantomSpawnWorld) HasSkyLight() bool { return !w.noSky }

// motionWorld is a testWorld whose MOTION_BLOCKING height is set apart from
// the rest.
type motionWorld struct {
	testWorld
	motion int32
}

func (w motionWorld) MotionBlockingHeight(x, z int32) int32 { return w.motion }

// darkOverworld is the night the spawner works under: a sky darkened past
// five and open sky light at the player.
func darkOverworld() phantomSpawnWorld {
	w := phantomSpawnWorld{spawnTestWorld: newSpawnTestWorld()}
	w.darken = 11
	w.sky = 15
	return w
}

// sleepless is a player over the sea level whose rest roll always lands and
// whose local difficulty always beats the roll against it.
func sleepless() PhantomPlayer {
	return PhantomPlayer{X: 0.5, Y: 64, Z: 0.5, TimeSinceRest: math.MaxInt32, Effective: 3}
}

// phantomsSpawned runs the spawner for an hour of ticks and counts the
// phantoms it asks for.
func phantomsSpawned(w PhantomWorld, p PhantomPlayer) int {
	var s PhantomSpawner
	rng := rand.New(rand.NewSource(1))
	n := 0
	for range 72000 {
		s.Tick(w, rng, []PhantomPlayer{p}, func(SpawnRequest) { n++ })
	}
	return n
}

// TestThePhantomSpawnersChecks runs the spawner's checks one at a time
// against the sleepless player under a dark open sky.
// vanilla: PhantomSpawner.tick
func TestThePhantomSpawnersChecks(t *testing.T) {
	if n := phantomsSpawned(darkOverworld(), sleepless()); n == 0 {
		t.Fatal("the control: no phantom over a sleepless player under a dark open sky")
	}

	spectator := sleepless()
	spectator.Spectator = true
	if n := phantomsSpawned(darkOverworld(), spectator); n != 0 {
		t.Errorf("%d phantoms over a spectator, want none", n)
	}

	below := sleepless()
	below.Y = 62
	if n := phantomsSpawned(darkOverworld(), below); n != 0 {
		t.Errorf("%d phantoms over a player under the sea level, want none", n)
	}

	covered := darkOverworld()
	covered.sky = 14
	if n := phantomsSpawned(covered, sleepless()); n != 0 {
		t.Errorf("%d phantoms over a player who cannot see the sky, want none", n)
	}

	day := darkOverworld()
	day.darken = 4
	if n := phantomsSpawned(day, sleepless()); n != 0 {
		t.Errorf("%d phantoms under a sky darkened by 4, want none", n)
	}

	rested := sleepless()
	rested.TimeSinceRest = phantomRestThreshold
	if n := phantomsSpawned(darkOverworld(), rested); n != 0 {
		t.Errorf("%d phantoms over a player rested %d ticks ago, want none", n, phantomRestThreshold)
	}

	// Without sky light none of the darkness, sea-level or sky checks is
	// asked.
	skyless := darkOverworld()
	skyless.noSky = true
	skyless.darken = 0
	skyless.sky = 0
	if n := phantomsSpawned(skyless, below); n == 0 {
		t.Error("no phantom in a dimension without sky light, where the sky checks are not asked")
	}
}

// TestThePhantomsAnchorWithABoatInTheWorld moves the anchor through a world
// wrapped with body boxes, as the herd's tick hands it over while a boat
// exists: the attack's end lifts it 10 to 29 over the MOTION_BLOCKING
// height, and the swoop's never puts it under the sea.
func TestThePhantomsAnchorWithABoatInTheWorld(t *testing.T) {
	base := motionWorld{testWorld: newTestWorld(), motion: 70}
	w := WithBodies(base, []AABB{{minX: 5, minY: 64, minZ: 5, maxX: 6, maxY: 65, maxZ: 6}})
	if _, ok := w.(Bodies); !ok {
		t.Fatal("the world was not wrapped")
	}

	m := NewMob(TypePhantom, 1, 0.5, 90, 0.5, 1)
	for range 50 {
		m.SetAnchorPos(0, 21, 0)
		m.phantomStrategyStop(w)
		if _, y, _, _ := m.AnchorPos(); y < 70+1+10 || y > 70+1+29 {
			t.Fatalf("the anchor after the attack is at y=%d, want 81 to 100 over the MOTION_BLOCKING height 70", y)
		}
	}

	low := survivor(0, 0)
	low.Y = 0
	for range 50 {
		m.SetAnchorPos(0, 21, 0)
		m.phantomAnchorAboveTarget(w, &low)
		if _, y, _, _ := m.AnchorPos(); y != 64 {
			t.Fatalf("the anchor over a target at y=0 is at y=%d, want the sea level's 63 plus one", y)
		}
	}
}

// TestAPhantomIdlesAtTheMobsPace checks the phantom, no Monster, takes no
// bright-light idleness share.
// vanilla: Monster.updateNoActionTime
func TestAPhantomIdlesAtTheMobsPace(t *testing.T) {
	w := newTestWorld()
	w.block = 15
	m := NewMob(TypePhantom, 1, 0, 64, 0, 1)
	m.idleStep(w)
	if m.NoActionTicks != 1 {
		t.Errorf("a phantom idled %d in bright light, want 1", m.NoActionTicks)
	}
}

// TestAPhantomFliesThroughLavaAtHalfPace checks the flight's lava drag.
// vanilla: LivingEntity.travelFlying
func TestAPhantomFliesThroughLavaAtHalfPace(t *testing.T) {
	w := newTestWorld()
	m := NewMob(TypePhantom, 1, 0.5, 90, 0.5, 1)
	m.lavaFluid.height = 0.5
	m.VX = 0.1
	m.phantomTravel(&tickView{w: w})
	if math.Abs(m.VX-0.05) > 1e-9 {
		t.Errorf("the speed after a tick in lava is %v, want 0.05", m.VX)
	}
}

// TestAPhantomSpawnerSpawnsInLight reads the spawner's rule at full block
// light, where a Monster's darkness check refuses and a plain Mob's does not,
// and the walk-target check a PathfinderMob alone makes.
// vanilla: SpawnPlacements (PHANTOM: Mob.checkMobSpawnRules)
func TestAPhantomSpawnerSpawnsInLight(t *testing.T) {
	w := newSpawnTestWorld()
	w.blockLights[[3]int32{0, 64, 0}] = 15
	rng := rand.New(rand.NewSource(1))
	for range 100 {
		if !SpawnerAllows(w, rng, TypePhantom, 0.5, 64, 0.5) {
			t.Fatal("a phantom spawner refused a lit cell")
		}
	}
	if !SpawnerMobFits(w, TypePhantom, 0.5, 64, 0.5, nil) {
		t.Error("a phantom spawner's phantom was refused by the walk-target check")
	}
	if !MonsterClassKind(TypeZombie) || MonsterClassKind(TypePhantom) ||
		MonsterClassKind(TypeSlime) || MonsterClassKind(TypeMagmaCube) || MonsterClassKind(TypeCow) ||
		MonsterClassKind(TypeGhast) || MonsterClassKind(TypeShulker) || MonsterClassKind(TypeHoglin) {
		t.Error("MonsterClassKind is not the enemies minus slimes, phantom, ghast, shulker and hoglin")
	}
}

// TestAPhantomsSweepStopsForACatNearby checks a live cat within 16 blocks
// ends the swoop at the next look, and a distant one does not.
// vanilla: Phantom.PhantomSweepAttackGoal.canContinueToUse
func TestAPhantomsSweepStopsForACatNearby(t *testing.T) {
	target := survivor(0, 0)
	cases := []struct {
		name string
		dist float64
		want bool
	}{{"near", 10, false}, {"far", 40, true}}
	for _, c := range cases {
		m := NewMob(TypePhantom, 1, 0.5, 90, 0.5, 1)
		m.Age = 5
		m.phantom.phase = phantomSwoop
		cat := NewMob(TypeCat, 2, 0.5+c.dist, 90, 0.5, 1)
		view := &tickView{w: newTestWorld(), others: []*Mob{cat}}
		if got := m.phantomSweepContinues(view, &target); got != c.want {
			t.Errorf("%s cat: sweep continues = %v, want %v", c.name, got, c.want)
		}
	}
}

// vanilla: Phantom.onSyncedDataUpdated, LivingEntity.addAdditionalSaveData
func TestPhantomDamageBeforeAttributeInstallation(t *testing.T) {
	t.Parallel()
	m := NewMob(TypePhantom, 1, 0, 84, 0, 1)
	for _, tc := range []struct {
		size int
		want float64
	}{
		{0, 2}, {5, 11}, {0, 6}, {0, 6}, {64, 70},
	} {
		m.SetPhantomSize(tc.size)
		if got := m.attackDamage(); got != tc.want {
			t.Errorf("size %d: bite = %v, want %v", tc.size, got, tc.want)
		}
	}
	// No attribute command has run; returning to zero must still save damage 6.
	m.SetPhantomSize(0)
	if m.Attributes() == nil {
		t.Fatal("resize left no persistent attribute map")
	}
	tag := nbt.Compound{}
	m.SaveTo(tag, func() int64 { return 0 })
	tag["attributes"] = *m.Attributes().Save(nil)
	reloaded := NewMob(TypePhantom, 2, 0, 84, 0, 1)
	reloaded.LoadFrom(tag, nil, func() int64 { return 0 })
	if got := reloaded.attackDamage(); got != 6 {
		t.Errorf("reloaded resized-zero phantom bite = %v, want 6", got)
	}
	fresh := NewMob(TypePhantom, 3, 0, 84, 0, 1)
	if got := fresh.AttributeSupplier().Bases["minecraft:attack_damage"]; got != 2 {
		t.Errorf("fresh phantom supplier base = %v, want 2", got)
	}
}
