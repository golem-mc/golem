package entity

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
)

func TestArmadilloSaveKeepsStateAndScuteTimer(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"idle", "rolling", "scared", "unrolling", "unknown"} {
		m := NewMob(TypeArmadillo, 1, 0, 64, 0, 1)
		if m.ScuteTimer < 6000 || m.ScuteTimer >= 12000 {
			t.Fatalf("initial timer = %d", m.ScuteTimer)
		}
		m.LoadFrom(nbt.Compound{"state": nbt.String(state), "scute_time": nbt.Int(-3)}, nil, nil)
		saved := nbt.Compound{}
		m.SaveTo(saved, nil)
		want := state
		if state == "unknown" {
			want = "idle"
		}
		if saved["state"] != nbt.String(want) || saved["scute_time"] != nbt.Int(-3) {
			t.Fatalf("saved state/timer = %v/%v", saved["state"], saved["scute_time"])
		}
		back := NewMob(TypeArmadillo, 2, 0, 64, 0, 2)
		back.LoadFrom(saved, nil, nil)
		if back.ArmadilloState() != m.ArmadilloState() || back.ScuteTimer != -3 {
			t.Fatal("state/timer lost on reload")
		}
	}
}

func TestArmadilloRollsAndUnrolls(t *testing.T) {
	w := newTestWorld()
	floor(w, -20, -20, 20, 20, 63, defaultState(t, "minecraft:stone"))
	m := NewMob(TypeArmadillo, 1, 0.5, 64, 0.5, 1)
	m.OnGround = true
	zombie := NewMob(TypeZombie, 2, 4.5, 64, 0.5, 2)
	zombie.NoAI = true
	herd := Mobs{m, zombie}
	herd.Tick(w, nil, nil)
	if m.ArmadilloState() != 1 {
		t.Fatalf("state = %d, want rolling", m.ArmadilloState())
	}
	x, z := m.X, m.Z
	for range 12 {
		herd.Tick(w, nil, nil)
	}
	if m.ArmadilloState() != 2 {
		t.Fatalf("state = %d, want scared", m.ArmadilloState())
	}
	if m.X != x || m.Z != z {
		t.Fatal("rolled armadillo walked")
	}
	if m.Sounds().Hurt.Name() != "minecraft:entity.armadillo.hurt_reduced" || m.Sounds().Ambient.Name() != "" {
		t.Fatalf("rolled sounds: %+v", m.Sounds())
	}
	zombie.X = 40
	unrolling := false
	for range 81 {
		herd.Tick(w, nil, nil)
		unrolling = unrolling || m.ArmadilloState() == 3
	}
	if !unrolling || m.ArmadilloState() != 0 {
		t.Fatalf("unrolling=%v, final state=%d", unrolling, m.ArmadilloState())
	}
}

func TestArmadilloScareSources(t *testing.T) {
	for _, tc := range []struct {
		name string
		p    Player
		want bool
	}{
		{"walking", Player{EntityID: 2, X: 2}, false},
		{"sprinting", Player{EntityID: 2, X: 2, Sprinting: true}, true},
		{"riding", Player{EntityID: 2, X: 2, Passenger: true}, true},
		{"spectator", Player{EntityID: 2, X: 2, Sprinting: true, Spectator: true}, false},
		{"above", Player{EntityID: 2, X: 2, Y: 4, Sprinting: true}, false},
		{"far", Player{EntityID: 2, X: 9, Sprinting: true}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewMob(TypeArmadillo, 1, 0, 0, 0, 1)
			m.OnGround = true
			m.armadilloScareStep(&tickView{w: newTestWorld(), players: []Player{tc.p}})
			if m.ArmadilloScared() != tc.want {
				t.Fatalf("scared=%v, want %v", m.ArmadilloScared(), tc.want)
			}
		})
	}
}

func TestSpidersAvoidIdleArmadillos(t *testing.T) {
	for _, typ := range []MobType{TypeSpider, TypeCaveSpider} {
		w := newTestWorld()
		floor(w, -25, -25, 25, 25, 63, defaultState(t, "minecraft:stone"))
		spider := NewMob(typ, 1, 0.5, 64, 0.5, 1)
		spider.OnGround = true
		armadillo := NewMob(TypeArmadillo, 2, 3.5, 64, 0.5, 2)
		armadillo.NoAI = true
		herd := Mobs{spider, armadillo}
		before := Dist2(spider.X, spider.Y, spider.Z, armadillo.X, armadillo.Y, armadillo.Z)
		for range 40 {
			herd.Tick(w, nil, nil)
		}
		after := Dist2(spider.X, spider.Y, spider.Z, armadillo.X, armadillo.Y, armadillo.Z)
		if after <= before {
			t.Fatalf("%s: distance squared %v -> %v", typ, before, after)
		}
		for _, goal := range hostileGoals(spider) {
			if g, ok := goal.(*avoidGoal); ok {
				armadillo.armadilloSwitch(2)
				if g.threatens(armadillo) {
					t.Fatal("spider fears a rolled armadillo")
				}
			}
		}
	}
}
