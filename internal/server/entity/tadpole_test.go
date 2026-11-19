package entity

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func TestTadpoleAgeFeedingAndConversion(t *testing.T) {
	w := newTestWorld()
	m := NewMob(TypeTadpole, 1, 0.5, 64, 0.5, 1)
	m.NoAI = true
	m.LoadFrom(nbt.Compound{"Age": nbt.Int(23998)}, w, nil)
	m.Tick(w, nil)
	if m.tadpole.age != 23999 || m.gone {
		t.Fatalf("age=%d discarded=%v", m.tadpole.age, m.gone)
	}
	events := m.Tick(w, nil)
	found := false
	for _, e := range events {
		if e.Kind == MobConverted && e.Into == TypeFrog {
			found = true
		}
	}
	if !found || !m.gone {
		t.Fatal("tadpole did not convert at 24000 ticks, including with NoAI")
	}

	m = NewMob(TypeTadpole, 2, 0, 64, 0, 2)
	food, _ := registry.ItemID("minecraft:slime_ball")
	if !m.FedBy(protocol.ItemStack{ItemID: food, Count: 1}) {
		t.Fatal("slimeball is not tadpole food")
	}
	if loved, grew := m.Feed(); loved || !grew || m.tadpole.age != 2400 {
		t.Fatalf("feeding: loved=%v grew=%v age=%d", loved, grew, m.tadpole.age)
	}
	m.tadpole.age = 23999
	if _, grew := m.Feed(); !grew || m.tadpole.age != 23999 {
		t.Fatal("feeding must consume food even when rounded growth is zero")
	}
	m.AgeLocked = true
	if _, grew := m.Feed(); grew {
		t.Fatal("age-locked tadpole took food")
	}
	m.Tick(w, nil)
	if m.tadpole.age != 23999 || m.gone {
		t.Fatal("age-locked tadpole grew")
	}
}

func TestTadpoleSwimmingTemptationAndFlopping(t *testing.T) {
	w := newTestWorld()
	stone, water := defaultState(t, "minecraft:stone"), defaultState(t, "minecraft:water")
	floor(w, -9, -9, 9, 9, 0, stone)
	for y := int32(1); y <= 9; y++ {
		floor(w, -9, -9, 9, 9, y, stone)
		floor(w, -8, -8, 8, 8, y, water)
	}
	m := NewMob(TypeTadpole, 1, 0.5, 4, 0.5, 1)
	travelled := 0.0
	for range 200 {
		x, y, z := m.X, m.Y, m.Z
		m.Tick(w, nil)
		travelled += math.Sqrt(Dist2(x, y, z, m.X, m.Y, m.Z))
	}
	if travelled < 1 || !m.InWater() || m.Health != 6 {
		t.Fatalf("swam %.2f blocks, water=%v health=%v", travelled, m.InWater(), m.Health)
	}
	// FollowTemptation holds a sensed lure and installs its 100-tick cooldown on loss.
	m = NewMob(TypeTadpole, 2, 0.5, 4, 0.5, 2)
	p := survivor(7, 5.5)
	p.Y, p.Z = 4, 0.5
	food, _ := registry.ItemID("minecraft:slime_ball")
	p.HeldItem = protocol.ItemStack{ItemID: food, Count: 1}
	for range 25 {
		m.Tick(w, []Player{p})
	}
	if m.tadpole.tempting != p.EntityID || m.nav.speedFactor != 1.25 {
		t.Fatalf("tadpole did not follow lure: %+v pace=%v", m.tadpole, m.nav.speedFactor)
	}
	for range 25 {
		m.Tick(w, nil)
	}
	if m.tadpole.temptationCooldown <= 0 {
		t.Fatal("lost lure did not start cooldown")
	}
	// FollowTemptation stops within 2.5 blocks, including a player outside two blocks.
	m = NewMob(TypeTadpole, 4, 0.5, 4, 0.5, 4)
	p.X = 2.75
	m.Tick(w, []Player{p})
	if m.tadpole.tempting != p.EntityID || len(m.nav.path) != 0 {
		t.Fatal("nearby lure should stop the tadpole's walk")
	}
	dry := newTestWorld()
	floor(dry, -3, -3, 3, 3, 63, stone)
	m = NewMob(TypeTadpole, 3, 0.5, 64, 0.5, 3)
	flopped := false
	for range 330 {
		for _, e := range m.Tick(dry, nil) {
			if e.Kind == MobFlopped {
				flopped = true
			}
		}
	}
	if !flopped || m.Health >= 6 {
		t.Fatalf("dry tadpole flopped=%v health=%v", flopped, m.Health)
	}
}
