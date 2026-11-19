package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func TestMobEquipmentRevisionTracksItemChanges(t *testing.T) {
	t.Parallel()
	m := NewMob(TypeWolf, 7, 0.5, 64, 0.5, 11)
	check := func(name string, change func()) {
		t.Helper()
		before := m.EquipmentRevision()
		change()
		if m.EquipmentRevision() == before {
			t.Errorf("%s did not invalidate equipment", name)
		}
	}
	for slot := range 7 {
		check("replacement", func() { m.SetEquipment(slot, protocol.ItemStack{ItemID: int32(registry.ItemDiamondBoots), Count: 1}) })
		before := m.EquipmentRevision()
		_ = m.EquipmentSlot(slot)
		if m.EquipmentRevision() != before {
			t.Fatal("read invalidated equipment")
		}
	}
	for piece := range 6 {
		check("wear", func() { m.WearEnchPiece(piece, 1) })
	}
	stack := protocol.ItemStack{ItemID: int32(registry.ItemWolfArmor), Count: 1, Damage: 10}
	m.SetEquipment(6, stack)
	m.Tamable.SittingPose = true
	check("repair", func() {
		if !m.RepairWolfArmor(int32(registry.ItemArmadilloScute), true) {
			t.Fatal("armor repair refused")
		}
	})
	check("load", func() { m.LoadFrom(nbt.Compound{"equipment": nbt.Compound{}}, nil, nil) })
	m.SetEquipment(protocol.EquipMainHand, protocol.ItemStack{ItemID: int32(registry.ItemCrossbow), Count: 1})
	check("charged projectiles", func() { m.loadCrossbow() })
	if !m.CrossbowLoaded() {
		t.Fatal("crossbow did not load")
	}
}

func TestMobEquipmentAttributeSnapshotResetsWithItsMap(t *testing.T) {
	m := NewMob(TypeZombie, 7, .5, 64, .5, 11)
	m.SetEquipment(protocol.EquipHead, protocol.ItemStack{ItemID: int32(registry.ItemDiamondHelmet), Count: 1})
	for range 2 {
		m.SetAttributes(m.BuildAttributes(nbt.List{}))
		if m.attrWornValid {
			t.Fatal("new attribute map kept the old equipment revision")
		}
		m.SyncEquipmentModifiers(m.attrs)
		if !m.attrWornValid || m.attrWornRead != m.EquipmentRevision() || m.attrs.Value(registry.AttributeArmor) != 5 {
			t.Fatal("new attribute map did not apply equipment modifiers")
		}
	}
	m.SetEquipment(protocol.EquipHead, protocol.ItemStack{})
	m.SyncEquipmentModifiers(m.attrs)
	if m.attrs.Value(registry.AttributeArmor) != 2 {
		t.Fatal("equipment removal left the helmet modifier")
	}
}
