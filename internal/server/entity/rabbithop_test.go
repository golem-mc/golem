package entity

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// griefingWorld is a test world whose mob_griefing rule is on.
type griefingWorld struct{ testWorld }

func (griefingWorld) MobGriefing() bool { return true }

func hopWorld(t *testing.T) testWorld {
	w := newTestWorld()
	floor(w, -40, -40, 40, 40, 63, defaultState(t, "minecraft:stone"))
	return w
}

// hopRun walks a rabbit toward (x, 64, z) at the given pace and returns the
// ticks it hopped on, the ticks it stood on the ground with the path still
// ahead of it, and its highest point.
func hopRun(t *testing.T, w World, pace, x, z float64, ticks int) (hops []int, idle int, top float64, m *Mob) {
	m = NewMob(TypeRabbit, 1, 0.5, 64, 0.5, 5)
	m.ai.goals, m.ai.running = nil, nil
	m.OnGround = true
	m.seekTo(w, x, 64, z, pace, navReach)
	for tick := range ticks {
		x0, z0 := m.X, m.Z
		var hopped bool
		for _, ev := range m.Tick(w, nil) {
			if ev.Kind == MobJumped {
				hops = append(hops, tick)
			}
			hopped = hopped || ev.Kind == MobHopped
		}
		_ = hopped
		if m.OnGround && len(m.nav.path) > 0 && x0 == m.X && z0 == m.Z {
			idle++
		}
		top = max(top, m.Y)
	}
	return hops, idle, top, m
}

// TestARabbitHopsInsteadOfWalking checks RabbitMoveControl: on the ground
// the rabbit stands still between hops although a path lies ahead, and
// every hop that leaves the ground is told as entity event 1.
func TestARabbitHopsInsteadOfWalking(t *testing.T) {
	w := hopWorld(t)
	hops, idle, _, m := hopRun(t, w, 1.0, 12.5, 0.5, 120)
	if len(hops) < 3 {
		t.Fatalf("the rabbit hopped %d times in 120 ticks, want at least 3", len(hops))
	}
	if idle < 20 {
		t.Errorf("the rabbit stood still on the ground for %d path ticks, want it resting between hops", idle)
	}
	if m.X < 3 {
		t.Errorf("the rabbit reached x=%.2f, want it hopping toward x=12.5", m.X)
	}
}

// TestARabbitHopsFasterWhenFleeing checks Rabbit.setLandingDelay: ten ticks
// between hops at a stroll, three when the speed modifier reaches 2.2, and
// the higher hop of the flight.
func TestARabbitHopsFasterWhenFleeing(t *testing.T) {
	w := hopWorld(t)
	stroll, _, strollTop, _ := hopRun(t, w, 0.6, 14.5, 0.5, 60)
	flee, _, fleeTop, _ := hopRun(t, w, 2.2, 14.5, 0.5, 60)
	for i := 1; i < len(stroll); i++ {
		if gap := stroll[i] - stroll[i-1]; gap < 10 {
			t.Errorf("strolling hops %d ticks apart, want at least the landing delay of 10", gap)
		}
	}
	if len(flee) < 3 {
		t.Fatalf("fleeing hopped %d times, want at least 3", len(flee))
	}
	// A hop in the air takes about 11 ticks at the flight's height, and the
	// landing delay is 3 instead of 10.
	for i := 1; i < len(flee); i++ {
		if gap := flee[i] - flee[i-1]; gap >= 17 {
			t.Errorf("fleeing hops %d ticks apart, want the landing delay of 3 (under 17)", gap)
		}
	}
	if !(fleeTop > strollTop+0.02) {
		t.Errorf("flight top %.3f, stroll top %.3f, want the flight to hop higher (0.3 against 0.2)", fleeTop, strollTop)
	}
}

// TestARabbitFleesPlayersWolvesAndMonsters checks the three
// RabbitAvoidEntityGoals and their distances: 8 blocks for a player, 10 for
// a wolf and 4 for a monster.
func TestARabbitFleesPlayersWolvesAndMonsters(t *testing.T) {
	cases := []struct {
		name  string
		dist  float64
		kind  MobType
		flees bool
	}{
		{"player near", 6, NoMobType, true},
		{"player far", 9.5, NoMobType, false},
		{"wolf near", 9, TypeWolf, true},
		{"wolf far", 11.5, TypeWolf, false},
		{"zombie near", 3, TypeZombie, true},
		{"zombie far", 5.5, TypeZombie, false},
		{"cow", 2, TypeCow, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := hopWorld(t)
			m := NewMob(TypeRabbit, 1, 0.5, 64, 0.5, 3)
			m.OnGround = true
			var players []Player
			var others []*Mob
			if c.kind == NoMobType {
				players = []Player{{X: 0.5 + c.dist, Y: 64, Z: 0.5, EntityID: 9, Huntable: true}}
			} else {
				o := NewMob(c.kind, 2, 0.5+c.dist, 64, 0.5, 4)
				o.NoAI = true
				others = []*Mob{m, o}
			}
			fled := false
			for range 200 {
				m.tick(w, players, others)
				if m.rabbit.speedMod >= 2.2 {
					fled = true
					break
				}
			}
			if fled != c.flees {
				t.Errorf("fled = %v, want %v", fled, c.flees)
			}
		})
	}
}

// TestTheKillerBunnyDoesNotFleeAndBites checks Rabbit.setVariant for EVIL:
// armour 8, attack 3+5, the player hunted and bitten, and no avoiding.
func TestTheKillerBunnyDoesNotFleeAndBites(t *testing.T) {
	w := hopWorld(t)
	players := []Player{{X: 3.5, Y: 64, Z: 0.5, EntityID: 9, Huntable: true}}
	killer := NewMob(TypeRabbit, 1, 0.5, 64, 0.5, 3)
	killer.RabbitType = rabbitEvil
	killer.OnGround = true
	plain := NewMob(TypeRabbit, 2, 0.5, 64, 2.5, 3)
	plain.OnGround = true

	if got := killer.AttributeSupplier().Bases["minecraft:armor"]; got != 8 {
		t.Errorf("killer bunny armour %v, want 8", got)
	}
	if got := killer.BuildAttributes(nbt.List{}).Value(registry.AttributeAttackDamage); got != 8 {
		t.Errorf("killer bunny attack %v, want 8", got)
	}
	if got := plain.AttributeSupplier().Bases["minecraft:attack_damage"]; got != 3 {
		t.Errorf("rabbit attack %v, want 3", got)
	}
	if got := killer.armoured(10); got >= plain.armoured(10) {
		t.Errorf("killer bunny takes %v of 10, plain rabbit %v, want the armour to cut it", got, plain.armoured(10))
	}

	var bitten float64
	for range 400 {
		for _, ev := range killer.Tick(w, players) {
			if ev.Kind == MobAttackedPlayer {
				bitten = ev.Damage
			}
		}
		for _, ev := range plain.Tick(w, players) {
			if ev.Kind == MobAttackedPlayer {
				t.Fatal("a plain rabbit bit the player")
			}
		}
		if plain.TargetID != 0 {
			t.Fatal("a plain rabbit took the player as its target")
		}
		if killer.rabbit.speedMod >= 2.2 {
			t.Fatal("the killer bunny fled")
		}
	}
	if bitten != 8 {
		t.Errorf("killer bunny bite %v, want 8", bitten)
	}
	if killer.TargetID != 9 {
		t.Errorf("killer bunny target %d, want 9", killer.TargetID)
	}
}

// TestARabbitEatsARipeCarrot checks RaidGardenGoal: on the farmland under a
// full-grown carrot the rabbit eats it, rests for MoreCarrotTicks and does
// not raid under mob_griefing false.
func TestARabbitEatsARipeCarrot(t *testing.T) {
	carrots := func(age string) uint16 {
		return registry.MustState("minecraft:carrots", map[string]string{"age": age})
	}
	cases := []struct {
		name    string
		crop    uint16
		grief   bool
		resting int
		want    bool
	}{
		{"ripe", carrots("7"), true, 0, true},
		{"unripe", carrots("6"), true, 0, false},
		{"no griefing", carrots("7"), false, 0, false},
		{"just ate", carrots("7"), true, 5000, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			base := newTestWorld()
			floor(base, -8, -8, 8, 8, 63, defaultState(t, "minecraft:farmland"))
			base.set(0, 64, 0, c.crop)
			var w World = base
			if c.grief {
				w = griefingWorld{base}
			}
			m := NewMob(TypeRabbit, 1, 0.5, 64, 0.5, 3)
			m.OnGround = true
			m.MoreCarrotTicks = c.resting
			var raided *MobEvent
			for range 700 {
				for _, ev := range m.Tick(w, nil) {
					if ev.Kind == MobRaidedCrop {
						raided = &ev
					}
				}
				if raided != nil {
					break
				}
			}
			if (raided != nil) != c.want {
				t.Fatalf("raided = %v, want %v", raided != nil, c.want)
			}
			if raided != nil && (raided.X != 0 || raided.Y != 64 || raided.Z != 0 || m.MoreCarrotTicks < 38) {
				t.Errorf("raid at %v,%v,%v with %d carrot ticks, want the crop cell and 40", raided.X, raided.Y, raided.Z, m.MoreCarrotTicks)
			}
		})
	}
}

// TestARabbitIgnoresMobsThatAreNotMonsters checks the Monster class bound of
// the avoid goal: phantoms and slimes are hostile-category but not Monster.
func TestARabbitIgnoresMobsThatAreNotMonsters(t *testing.T) {
	for _, kind := range []MobType{TypePhantom, TypeSlime, TypeMagmaCube} {
		w := hopWorld(t)
		m := NewMob(TypeRabbit, 1, 0.5, 64, 0.5, 3)
		m.OnGround = true
		o := NewMob(kind, 2, 2.5, 64, 0.5, 4)
		o.NoAI = true
		for range 100 {
			m.tick(w, nil, []*Mob{m, o})
			if m.rabbit.speedMod >= 2.2 {
				t.Fatalf("the rabbit fled a %s", kind)
			}
		}
	}
}

// TestARabbitWatchesPlayersWithinTen checks Rabbit.registerGoals'
// LookAtPlayerGoal distance of 10.
func TestARabbitWatchesPlayersWithinTen(t *testing.T) {
	for _, c := range []struct {
		dist float64
		look bool
	}{{9.5, true}, {10.5, false}} {
		w := hopWorld(t)
		m := NewMob(TypeRabbit, 1, 0.5, 64, 0.5, 3)
		m.OnGround = true
		players := []Player{{X: 0.5 + c.dist, Y: 64, Z: 0.5, EntityID: 9}}
		looked := false
		for range 400 {
			m.tick(w, players, []*Mob{m})
			// Facing +x is yaw -90.
			if m.HeadYaw < -89 && m.HeadYaw > -91 {
				looked = true
				break
			}
		}
		if looked != c.look {
			t.Errorf("player at %.1f: looked = %v, want %v", c.dist, looked, c.look)
		}
	}
}

// TestTheKillerBunnyTargetsAWolf checks Rabbit.setVariant's
// NearestAttackableTargetGoal<Wolf>.
func TestTheKillerBunnyTargetsAWolf(t *testing.T) {
	w := hopWorld(t)
	killer := NewMob(TypeRabbit, 1, 0.5, 64, 0.5, 3)
	killer.RabbitType = rabbitEvil
	killer.OnGround = true
	plain := NewMob(TypeRabbit, 2, 0.5, 64, 4.5, 3)
	plain.OnGround = true
	wolf := NewMob(TypeWolf, 3, 4.5, 64, 0.5, 4)
	wolf.NoAI = true
	for range 200 {
		killer.tick(w, nil, []*Mob{killer, plain, wolf})
		plain.tick(w, nil, []*Mob{killer, plain, wolf})
	}
	if killer.TargetID != 3 {
		t.Errorf("killer bunny target %d, want the wolf (3)", killer.TargetID)
	}
	if plain.TargetID != 0 {
		t.Errorf("plain rabbit target %d, want none", plain.TargetID)
	}
}
