package entity

import (
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
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

type sulfurRuleReadWorld struct {
	testWorld
	reads int
}

func (w *sulfurRuleReadWorld) BoolRule(id world.GameRuleIndex) bool {
	w.reads++
	return w.testWorld.BoolRule(id)
}
