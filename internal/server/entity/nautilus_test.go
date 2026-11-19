package entity

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

func TestNautilusTamingFoodEquipmentAndSave(t *testing.T) {
	m := NewMob(TypeNautilus, 1, 0, 64, 0, 42)
	fish := protocol.ItemStack{ItemID: int32(registry.ItemCod), Count: 1}
	puffer := protocol.ItemStack{ItemID: int32(registry.ItemPufferfish), Count: 1}
	saddle := protocol.ItemStack{ItemID: int32(registry.ItemSaddle), Count: 1}
	armor := protocol.ItemStack{ItemID: int32(registry.ItemIronNautilusArmor), Count: 1}
	if m.MaxHealth != 15 || m.NautilusFood(fish) || !m.NautilusFood(puffer) || m.NautilusCanEquip() || m.CanBeRidden() {
		t.Fatal("wild adult defaults")
	}
	owner := uuid.New()
	successes := 0
	for range 3000 {
		if m.TameNautilus(owner) {
			successes++
		}
	}
	if successes < 900 || successes > 1100 || m.Tamable.OrderedToSit || m.Tamable.Owner != owner {
		t.Fatalf("taming: successes=%d state=%+v", successes, m.Tamable)
	}
	if !m.NautilusFood(fish) || !m.Wears("saddle", saddle) || !m.Wears("body", armor) || !m.CanBeRidden() {
		t.Fatal("tame adult capabilities")
	}
	m.Health = 10
	if !m.FeedNautilus(fish) || m.Health != 14 {
		t.Fatal("cod did not heal four")
	}
	m.Horse.Saddle, m.Body = saddle, armor
	m.Board(owner)
	if !m.Steered() || m.CanBeRidden() {
		t.Fatal("rider control/capacity")
	}
	if change, ok := m.AddEffect(effect.New(registry.MustMobEffect("minecraft:poison"), 200, 0, false, true, true)); ok {
		t.Fatalf("poison accepted: %+v", change)
	}
	saved := nbt.Compound{}
	m.SaveTo(saved, nil)
	back := NewMob(TypeNautilus, 2, 0, 64, 0, 2)
	back.LoadFrom(saved, nil, nil)
	if !back.Tamable.Tame || back.Tamable.Owner != owner || back.Horse.Saddle.Empty() || back.Body.Empty() || back.Health != 14 {
		t.Fatalf("save lost nautilus: %v", saved)
	}
	baby := NewMob(TypeNautilus, 3, 0, 64, 0, 3)
	m.BreedInto(baby, back)
	baby.SetBaby(true)
	if !baby.Tamable.Tame || baby.Tamable.Owner != owner || !baby.NautilusFood(fish) || baby.Wears("saddle", saddle) || baby.Wears("body", armor) || baby.CanBeRidden() {
		t.Fatal("baby inheritance/capabilities")
	}
	m.Body = protocol.ItemStack{}
	if !m.DispenserEquip(armor) || m.Body.Empty() {
		t.Fatal("dispenser armor")
	}
}

func TestNautilusDryOutAndDash(t *testing.T) {
	m := NewMob(TypeNautilus, 1, 0, 64, 0, 1)
	for range 319 {
		if len(m.nautilusAirStep(nil)) != 0 {
			t.Fatal("dried before -20 air")
		}
	}
	ev := m.nautilusAirStep(nil)
	if len(ev) != 1 || ev[0].DamageType != registry.DamageTypeDryOut || m.Health != 13 || m.AirSupply != 0 {
		t.Fatalf("dry out: %v hp=%v air=%d", ev, m.Health, m.AirSupply)
	}
	m.waterFluid.height = 1
	m.nautilusAirStep(nil)
	if m.AirSupply != 300 {
		t.Fatal("water did not reset air")
	}
	m.NoAI = true
	m.waterFluid.height = 0
	m.nautilusAirStep(nil)
	if m.AirSupply != 300 {
		t.Fatal("NoAI lost air")
	}
	m.NoAI = false
	m.Tamable.Tame = true
	m.Horse.Saddle = protocol.ItemStack{ItemID: int32(registry.ItemSaddle), Count: 1}
	m.Board(uuid.New())
	if m.NautilusJump(0) || m.NautilusJump(-1) {
		t.Fatal("nonpositive jump power started dash")
	}
	if !m.NautilusJump(100) || !m.NautilusDashing() || m.NautilusJump(100) {
		t.Fatal("dash start/cooldown")
	}
	tick := &tickView{w: newTestWorld()}
	for range 7 {
		m.nautilusStep(tick)
	}
	if m.NautilusDashing() {
		t.Fatal("dash pose outlasted minimum duration")
	}
	for range 33 {
		m.nautilusStep(tick)
	}
	if !m.NautilusJump(100) || len(tick.events) != 1 {
		t.Fatal("dash did not recharge with ready sound")
	}
}

func TestNautilusSwimsAndChargesOnlyWildInWater(t *testing.T) {
	w := newTestWorld()
	water := defaultState(t, "minecraft:water")
	for x := int32(-3); x <= 16; x++ {
		for y := int32(60); y <= 67; y++ {
			for z := int32(-3); z <= 3; z++ {
				w.set(x, y, z, water)
			}
		}
	}
	m := NewMob(TypeNautilus, 1, 0.5, 62, 0.5, 1)
	m.waterFluid.height = 1
	if !m.swimTo(w, 8.5, 64, 0.5, 1) {
		t.Fatal("no water path")
	}
	for range 80 {
		m.nautilusMoveControl(w)
		m.nautilusTravel(w)
	}
	if m.X < 2 || m.Y < 62.25 {
		t.Fatalf("did not swim in three dimensions: %.2f %.2f", m.X, m.Y)
	}
	m.X, m.Y, m.Z = 0.5, 62, 0.5
	target := NewMob(TypeCod, 2, 3.5, 62, 0.5, 2)
	target.waterFluid.height = 1
	tick := &tickView{w: w, others: []*Mob{m, target}}
	m.nautilus.angryAt, m.nautilus.angerUntil = target.ID, 400
	g := &nautilusChargeGoal{m: m}
	if !g.CanStart(tick) {
		t.Fatal("wild nautilus did not retaliate")
	}
	g.Start(tick)
	g.Tick(tick)
	if math.Abs(m.VX-0.6) > 1e-9 || m.TargetID != target.ID {
		t.Fatal("charge direction/speed")
	}
	target.X, target.Z = m.X, m.Z+0.1
	g.Tick(tick)
	if m.nautilus.chargeCooldown != 80 || len(tick.events) != 2 || tick.events[1].Damage != 3 || !tick.events[1].Ram || m.Yaw != 0 || m.HeadYaw != m.Yaw || tick.events[1].RamYaw != float64(m.Yaw) || math.Abs(m.VX-0.36) > 1e-9 {
		t.Fatalf("charge impact: %+v", tick.events)
	}
	g.Stop(tick)
	m.nautilus.chargeCooldown = 0
	m.Tamable.Tame = true
	if g.CanStart(tick) {
		t.Fatal("tamed nautilus attacks")
	}
	m.Tamable.Tame = false
	m.SetBaby(true)
	if g.CanStart(tick) {
		t.Fatal("baby attacks")
	}
	m.SetBaby(false)
	m.waterFluid.height = 0
	if g.CanStart(tick) {
		t.Fatal("dry nautilus starts attack")
	}
}
