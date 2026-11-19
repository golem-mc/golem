package entity

import (
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func TestSulfurCubeSizeGrowthAndSplits(t *testing.T) {
	t.Parallel()
	m := NewMob(TypeSulfurCube, 1, 0.5, 64, 0.5, 1)
	if m.Size != 2 || m.IsBaby() || m.Health != 8 || m.BbWidth() != 0.98 || m.eyeHeight() != 0.35 {
		t.Fatalf("adult size/health/box/eyes = %d/%g/%g/%g", m.Size, m.Health, m.BbWidth(), m.eyeHeight())
	}
	if GoneInPeaceful(m.Type) || Enemy(m.Type) || MonsterClassKind(m.Type) || !MonsterKind(m.Type) || m.cubeDealsDamage() {
		t.Fatal("sulfur cube must be a non-attacking monster-category AgeableMob")
	}
	s := m.AttributeSupplier()
	if s.Names["minecraft:attack_damage"] || s.Bases["minecraft:tempt_range"] != 8 {
		t.Fatalf("attributes = %+v", s)
	}
	for range 20 {
		if xp := m.RollExperience(rules.LootKiller{}); xp < 1 || xp > 2 {
			t.Fatalf("adult XP = %d", xp)
		}
		splits := m.Splits()
		if len(splits) != 2 || splits[0].Size != 1 || splits[1].Size != 1 {
			t.Fatalf("splits = %+v", splits)
		}
	}
	m.SetSize(1, true)
	if m.Size != 1 || m.Health != 4 || m.BbWidth() != 0.49 || m.RollExperience(rules.LootKiller{}) != 0 || len(m.Splits()) != 0 {
		t.Fatal("incorrect baby dimensions, health, XP or splits")
	}
	w := newTestWorld()
	m.SetAge(w, -1)
	m.NoAI, m.Persistence = true, true
	herd := Mobs{m}
	herd.Tick(w, nil, nil)
	if m.IsBaby() || m.Size != 2 || m.Health != 8 {
		t.Fatal("baby did not grow to a healthy adult")
	}
	m.SetSize(1, true)
	m.AgeLocked = true
	herd.Tick(w, nil, nil)
	if m.BabyAge != BabyAgeStart {
		t.Fatal("locked baby aged")
	}
}

func TestSulfurCubeSaveAndBucketAge(t *testing.T) {
	t.Parallel()
	m := NewMob(TypeSulfurCube, 1, 0, 64, 0, 1)
	m.SetSize(1, true)
	m.BabyAge, m.AgeLocked, m.FromBucket, m.Health = -1200, true, true, 3
	m.NoAI, m.Silent = true, true
	saved := nbt.Compound{}
	m.SaveTo(saved, nil)
	out := NewMob(TypeSulfurCube, 2, 0, 64, 0, 2)
	out.LoadFrom(saved, newTestWorld(), nil)
	for _, v := range []*Mob{out, NewMob(TypeSulfurCube, 3, 0, 64, 0, 3)} {
		if v != out {
			v.LoadBucketTag(m.BucketTag())
		}
		if v.BabyAge != -1200 || !v.AgeLocked || !v.FromBucket || v.Size != 1 || v.Health != 3 || !v.NoAI || !v.Silent || v.CountsForSpawnCap() {
			t.Fatalf("lost bucket/saved age or flags: %+v", v)
		}
	}
}

func TestSulfurCubeTemptationAndFeeding(t *testing.T) {
	t.Parallel()
	m := NewMob(TypeSulfurCube, 1, 0, 64, 0, 1)
	food, _ := registry.ItemTagMembers("minecraft:sulfur_cube_food")
	if len(food) == 0 {
		t.Fatal("missing food tag")
	}
	id, _ := registry.ItemID(food[0])
	stack := protocol.ItemStack{ItemID: id, Count: 1}
	if m.FedBy(stack) {
		t.Fatal("adult eats baby food")
	}
	m.SetSize(1, true)
	if !m.FedBy(stack) || !m.tempts(stack) {
		t.Fatal("baby ignores food")
	}
	if loved, grew := m.Feed(); loved || !grew || m.BabyAge != -21600 {
		t.Fatalf("feeding = %v/%v age %d", loved, grew, m.BabyAge)
	}
	m.AgeLocked = true
	if loved, grew := m.Feed(); loved || grew {
		t.Fatal("fed a locked baby")
	}
}

func TestSulfurCubeHopsWithoutAttacking(t *testing.T) {
	t.Parallel()
	w := newTestWorld()
	floor(w, -16, -16, 16, 16, 63, defaultState(t, "minecraft:stone"))
	m := NewMob(TypeSulfurCube, 1, 0.5, 64, 0.5, 1)
	m.Persistence = true
	herd := Mobs{m}
	players := []Player{{EntityID: 2, X: 1, Y: 64, Z: 1, Huntable: true}}
	hops := 0
	for range 100 {
		for _, ev := range herd.Tick(w, players, nil) {
			if ev.Kind == MobJumped {
				hops++
			}
			if ev.Kind == MobAttackedPlayer {
				t.Fatal("sulfur cube attacked a player")
			}
		}
	}
	if hops == 0 {
		t.Fatal("sulfur cube never hopped")
	}
	if m.TargetID != 0 {
		t.Fatal("sulfur cube acquired a combat target")
	}
}

func TestSulfurCubeSpawnsInBrightSulfurCaves(t *testing.T) {
	t.Parallel()
	w := newSpawnTestWorld()
	w.sky = 15
	w.biomeName = "minecraft:sulfur_caves"
	w.groundAt(t, 0, 63, 0)
	w.blockLights[[3]int32{0, 64, 0}] = 15
	for seed := range int64(20) {
		if !validSpawnPositionAt(w, GoDice{rand.New(rand.NewSource(seed))}, TypeSulfurCube, 0.5, 64, 0.5, 100, CategoryMonster) {
			t.Fatalf("bright spawn rejected for seed %d", seed)
		}
	}
	list := spawnListAt(w, 0, 64, 0, CategoryMonster, testBiome(w.biomeName))
	if len(list) == 0 || list[0].mob != TypeSulfurCube || list[0].weight != 100 {
		t.Fatalf("sulfur caves list = %+v", list)
	}
}

func TestSulfurCubeSavedSizeDoesNotImplyBaby(t *testing.T) {
	t.Parallel()
	m := NewMob(TypeSulfurCube, 1, 0, 64, 0, 1)
	m.LoadFrom(nbt.Compound{"Size": nbt.Int(0), "from_bucket": nbt.Byte(1)}, newTestWorld(), nil)
	if m.IsBaby() || m.Size != 1 || m.MaxHealth != 4 || !m.FromBucket {
		t.Fatalf("loaded size-one adult: baby=%v size=%d health=%g", m.IsBaby(), m.Size, m.MaxHealth)
	}
}

func TestSulfurCubeAbsorbsShearsAndPicksUpBlocks(t *testing.T) {
	t.Parallel()
	m := NewMob(TypeSulfurCube, 1, 0.5, 64, 0.5, 1)
	m.Persistence = true
	w := newTestWorld()
	floor(w, -16, -16, 16, 16, 63, defaultState(t, "minecraft:stone"))
	stone, _ := registry.ItemID("minecraft:stone")
	dirt, _ := registry.ItemID("minecraft:dirt")
	stack := protocol.ItemStack{ItemID: stone, Count: 3}
	if old, ok := m.AbsorbSulfurBlock(stack); !ok || !old.Empty() || m.Body.Count != 1 {
		t.Fatal("block not absorbed")
	}
	if _, ok := m.AbsorbSulfurBlock(stack); ok {
		t.Fatal("replaced block with same item")
	}
	if m.canFreeze() || !m.canBreatheUnderwater() || m.CountsForSpawnCap() {
		t.Fatal("filled cube has wrong environmental rules")
	}
	saved := nbt.Compound{}
	m.SaveTo(saved, nil)
	copy := NewMob(TypeSulfurCube, 2, 0, 64, 0, 2)
	copy.LoadFrom(saved, w, nil)
	if copy.Body.ItemID != stone {
		t.Fatal("saved cube lost its block")
	}
	herd := Mobs{m}
	for range 30 {
		for _, ev := range herd.Tick(w, nil, nil) {
			if ev.Kind == MobJumped {
				t.Fatal("filled cube hopped")
			}
		}
	}
	if old, ok := m.AbsorbSulfurBlock(protocol.ItemStack{ItemID: dirt, Count: 1}); !ok || old.ItemID != stone {
		t.Fatal("replacement did not return previous block")
	}
	drops := m.RollShear(protocol.ItemStack{}, nil)
	if len(drops) != 1 || drops[0].ItemID != dirt || !m.Body.Empty() {
		t.Fatal("shearing lost block")
	}
	items := []LootItem{{X: m.X, Y: m.Y, Z: m.Z, Stack: stack}}
	if len(herd.TakeLoot(items)) != 0 {
		t.Fatal("ignored shear pickup cooldown")
	}
	for range 100 {
		herd.Tick(w, nil, nil)
	}
	items[0].X, items[0].Y, items[0].Z = m.X, m.Y, m.Z
	takes := herd.TakeLoot(items)
	if len(takes) != 1 || takes[0].Count != 1 {
		t.Fatal("did not take one block after cooldown")
	}
	m.TookLoot(takes[0], stack)
	if m.Body.ItemID != stone || m.Body.Count != 1 {
		t.Fatal("pickup lost block")
	}
}

func TestSulfurCubeSearchPrefersNearestSwallowableItem(t *testing.T) {
	m := NewMob(TypeSulfurCube, 1, 0, 64, 0, 1)
	stone, _ := registry.ItemID("minecraft:stone")
	sword, _ := registry.ItemID("minecraft:diamond_sword")
	items := []LootItem{{X: 4, Y: 64, Stack: protocol.ItemStack{ItemID: stone, Count: 1}}, {X: 2, Y: 64, Stack: protocol.ItemStack{ItemID: stone, Count: 1}}, {X: 1, Y: 64, Stack: protocol.ItemStack{ItemID: sword, Count: 1}}}
	m.senseSulfurItems(items)
	if !m.sulfur.hasWanted || m.sulfur.wanted.X != 2 {
		t.Fatal("searched for the wrong item")
	}
	g := sulfurSearchGoal{m: m}
	if !g.CanStart(nil) {
		t.Fatal("did not search")
	}
	m.Age = 3
	if g.CanStart(nil) {
		t.Fatal("followed stale ground snapshot")
	}
	m.SetSize(1, true)
	m.senseSulfurItems(items)
	if m.sulfur.hasWanted {
		t.Fatal("baby searched for blocks")
	}
}

func TestSulfurCubeTemptationStopsComposedMovement(t *testing.T) {
	t.Parallel()
	w := newTestWorld()
	floor(w, -16, -16, 16, 16, 63, defaultState(t, "minecraft:stone"))
	m := NewMob(TypeSulfurCube, 1, 0.5, 64, 0.5, 1)
	m.Persistence = true
	stone, _ := registry.ItemID("minecraft:stone")
	players := []Player{{EntityID: 2, X: 1, Y: 64, Z: 0.5, HeldItem: protocol.ItemStack{ItemID: stone, Count: 1}}}
	herd := Mobs{m}
	for range 60 {
		players[0].Y = m.Y
		herd.Tick(w, players, nil)
		if m.walk.speed != 0 || m.X != 0.5 || m.Z != 0.5 {
			t.Fatalf("cube moved beside lure: speed=%g position=(%g,%g)", m.walk.speed, m.X, m.Z)
		}
	}
	players[0].X = 5
	moving := false
	for range 60 {
		herd.Tick(w, players, nil)
		moving = moving || m.walk.speed > 0
	}
	if !moving {
		t.Fatal("cube did not resume hopping toward distant lure")
	}
}

type sulfurRuleReadWorld struct {
	testWorld
	reads int
}

func (w *sulfurRuleReadWorld) BoolRule(id world.GameRuleIndex) bool {
	w.reads++
	return w.testWorld.BoolRule(id)
}

func TestSulfurFuseReadsRuleOnlyForExplosiveCubes(t *testing.T) {
	t.Parallel()
	for _, item := range []string{"", "minecraft:oak_planks", "minecraft:tnt"} {
		m := NewMob(TypeSulfurCube, 1, 0, 64, 0, 1)
		if item != "" {
			id, _ := registry.ItemID(item)
			m.Body = protocol.ItemStack{ItemID: id, Count: 1}
		}
		w := &sulfurRuleReadWorld{}
		m.sulfurFuseStep(w, nil)
		want := 0
		if item == "minecraft:tnt" {
			want = 1
		}
		if w.reads != want {
			t.Fatalf("item %q: rule reads = %d, want %d", item, w.reads, want)
		}
	}
}

func TestSulfurCubeHitSoundKeepsDefaultPitch(t *testing.T) {
	t.Parallel()
	m := NewMob(TypeSulfurCube, 1, 0, 64, 0, 1)
	m.AbsorbSulfurBlock(protocol.ItemStack{ItemID: int32(registry.ItemStone), Count: 1})
	m.sulfurKnockback(HitSource{X: 1, Y: 64, Eye: 65.62}, 0.4, -1, 0, 1, false)
	events, _ := m.sulfurFuseStep(newTestWorld(), nil)
	if len(events) != 1 || events[0].Kind != MobSounded || events[0].Volume != 1 || events[0].Pitch != 1 {
		t.Fatalf("hit events=%+v, want one sound at volume/pitch 1", events)
	}
}
