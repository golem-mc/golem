package entity

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
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

func TestTadpoleBucketAndSavedAge(t *testing.T) {
	m := NewMob(TypeTadpole, 1, 0, 64, 0, 1)
	m.LoadFrom(nbt.Compound{"Age": nbt.Double(1234.9), "AgeLocked": nbt.Byte(1)}, nil, nil)
	m.NoAI, m.Silent, m.Health = true, true, 4
	m.tadpole.temptationCooldown = 73
	if MobBucketItem(m.Type) != "minecraft:tadpole_bucket" {
		t.Fatal("wrong bucket item")
	}
	if kind, ok := BucketMobKind(registry.ItemTadpoleBucket); !ok || kind != TypeTadpole {
		t.Fatal("bucket has no tadpole")
	}
	saved := nbt.Compound{}
	m.SaveTo(saved, nil)
	for _, bucket := range []bool{false, true} {
		out := NewMob(TypeTadpole, 2, 0, 64, 0, 2)
		if bucket {
			out.LoadBucketTag(m.BucketTag())
		} else {
			out.LoadFrom(saved, nil, nil)
		}
		if !bucket && out.tadpole.temptationCooldown != 73 {
			t.Fatal("save lost temptation cooldown")
		}
		if bucket && out.tadpole.temptationCooldown != 0 {
			t.Fatal("bucket carried brain memory")
		}
		if out.tadpole.age != 1234 || !out.AgeLocked || !out.NoAI || !out.Silent || out.Health != 4 || !out.FromBucket {
			t.Fatalf("bucket=%v lost saved fields: %+v", bucket, out.tadpole)
		}
		out.FromBucket = false // Tadpole.fromBucket is always true, even after a /data write.
		if out.CountsForSpawnCap() || out.despawnStep(newTestWorld(), []Player{survivor(7, 200)}) {
			t.Fatal("tadpole despawned or counted toward cap")
		}
		if out.RollExperience(rules.LootKiller{}) != 0 {
			t.Fatal("tadpole drops experience")
		}
	}
}

func TestTadpoleGoldenDandelionResetsAge(t *testing.T) {
	m := NewMob(TypeTadpole, 1, 0, 64, 0, 1)
	m.tadpole.age = 1234
	if !m.ToggleAgeLock() || !m.AgeLocked || !m.Persistence || m.tadpole.age != 0 {
		t.Fatal("golden dandelion did not lock and reset age")
	}
	if m.ToggleAgeLock() {
		t.Fatal("dandelion ignored cooldown")
	}
	w := newTestWorld()
	m.NoAI = true
	for range 40 {
		m.Tick(w, nil)
	}
	if m.tadpole.age != 0 || !m.ToggleAgeLock() || m.AgeLocked {
		t.Fatal("dandelion did not unlock after 40 ticks")
	}
	m.Tick(w, nil)
	if m.tadpole.age != 1 {
		t.Fatal("unlocked tadpole did not resume growth")
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

func TestTadpoleFrogVariantAndGrowthFit(t *testing.T) {
	for biome, want := range map[string]string{"minecraft:plains": "minecraft:temperate", "minecraft:desert": "minecraft:warm", "minecraft:snowy_plains": "minecraft:cold"} {
		old := NewMob(TypeTadpole, 1, 0.5, 64, 0.5, 1)
		old.MainHand = protocol.ItemStack{ItemID: 1, Count: 1}
		frog := NewMob(TypeFrog, 2, 0.5, 64, 0.5, 2)
		frog.ConvertedFrom(old)
		frog.FinishTadpoleGrowth(newTestWorld(), old, testBiome(biome))
		if !frog.MainHand.Empty() {
			t.Fatal("tadpole conversion carried equipment")
		}
		if frog.Variant != want || !frog.Persistence || frog.Health != 10 || frog.IsBaby() {
			t.Fatalf("%s frog: variant=%s persistence=%v health=%v", biome, frog.Variant, frog.Persistence, frog.Health)
		}
		tag := nbt.Compound{}
		frog.SaveTo(tag, nil)
		back := NewMob(TypeFrog, 3, 0, 64, 0, 3)
		back.LoadFrom(tag, nil, nil)
		if back.Variant != want {
			t.Fatal("save lost frog variant")
		}
	}
	frog := NewMob(TypeFrog, 1, 0, 64, 0, 1)
	frog.SetBaby(true)
	if frog.IsBaby() || frog.TakesBabyEgg() {
		t.Fatal("frog can become a baby")
	}
	if frog.fallDamageImmune() {
		t.Fatal("frog should reduce fall damage rather than refuse it")
	}
	if d, _, _, _ := frog.fallBlow(newTestWorld(), 9); d != 1 {
		t.Fatalf("frog fall damage=%v, want 1", d)
	}
}

func TestTadpolePanicHoldsForBrainDuration(t *testing.T) {
	m := NewMob(TypeTadpole, 1, 0, 64, 0, 1)
	m.noteLastDamage(registry.DamageTypePlayerAttack)
	g := &panicGoal{m: m, pace: 2, brain: true}
	v := &tickView{w: newTestWorld()}
	if !g.CanStart(v) {
		t.Fatal("hurt tadpole did not panic")
	}
	g.Start(v)
	duration := g.remaining
	if duration < 100 || duration > 120 {
		t.Fatalf("panic duration=%d", duration)
	}
	for range duration {
		if !g.ShouldContinue(v) {
			t.Fatal("panic ended before its duration")
		}
		g.Tick(v)
	}
	if g.ShouldContinue(v) {
		t.Fatal("panic exceeded its duration")
	}
}
