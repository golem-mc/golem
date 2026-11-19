package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
)

// TestMobTookOverBoatAppendsSeat checks the converted body is appended rather
// than put back at the old body's index: vanilla's startRiding adds it to the
// end of the passenger list.
func TestMobTookOverBoatAppendsSeat(t *testing.T) {
	t.Parallel()
	w, err := world.NewWorld(t.TempDir(), world.NewFlatGenerator())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	old := &mobEntity{mob: &entity.Mob{ID: 1}, w: w}
	pig := &mobEntity{mob: &entity.Mob{ID: 2}, w: w}
	next := &mobEntity{mob: &entity.Mob{ID: 3}, w: w}
	it := &boatEntity{
		boat:       &entity.Boat{ID: 9},
		w:          w,
		passengers: []*boatPassenger{{ent: old}, {ent: pig}},
	}
	s := withDims(&Server{boats: []*boatEntity{it}}, []*dimension{{
		w:       w,
		mobs:    &mobHerd{w: w},
		physics: world.NewPhysics(w),
	}})

	got, _ := s.mobTookOverBoat(old, next)
	if got != it {
		t.Fatalf("mobTookOverBoat returned %v, want the hull holding the old body", got)
	}
	s.boatsMu.Lock()
	ids := it.passengerIDs()
	s.boatsMu.Unlock()
	if len(ids) != 2 || ids[0] != 2 || ids[1] != 3 {
		t.Errorf("the boat's passengers = %v, want the pig %d then the appended body %d", ids, pig.mob.ID, next.mob.ID)
	}
	if next.vehicle != it || !next.mob.RidesVehicle {
		t.Error("the converted body did not take the boat seat")
	}
}

// TestConvertedBodyCarriesInvulnerability checks a full conversion carries
// the old body's invulnerability flag onto the new one — vanilla's
// ConversionType.convertCommon copying it across.
func TestConvertedBodyCarriesInvulnerability(t *testing.T) {
	t.Parallel()
	w := &world.World{}
	old := &mobEntity{mob: &entity.Mob{ID: 1, Invulnerable: true}, w: w}
	drowned := entity.NewMob(entity.TypeDrowned, 2, 0, 0, 0, 0)

	got := convertedEntity(old, drowned, w, true)
	if !got.mob.Invulnerable {
		t.Error("the converted body dropped the old body's invulnerability")
	}
}

// vanilla: ConversionType.convertCommon, Entity.setNoGravity
func TestConvertedBodyInvalidatesGravityMetadata(t *testing.T) {
	for _, full := range []bool{false, true} {
		old := &mobEntity{mob: entity.NewMob(entity.TypeZombie, 1, 0, 0, 0, 1)}
		old.mob.SetMetadataNoGravity(true)
		drowned := entity.NewMob(entity.TypeDrowned, 2, 0, 0, 0, 2)
		drowned.ClearMetadataDirty()
		got := convertedEntity(old, drowned, nil, full)
		if !got.mob.NoGravity || !got.mob.MetadataDirty().Has(entity.MobMetadataNoGravity) {
			t.Fatalf("full=%v: conversion lost gravity metadata invalidation", full)
		}
	}
}
