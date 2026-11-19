package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

func TestMobMetadataBulkLoadInvalidatesSnapshots(t *testing.T) {
	m := NewMob(TypeWolf, 7, .5, 64, .5, 11)
	before, equipment := m.MetadataRevision(), m.EquipmentRevision()
	m.ClearMetadataDirty()
	m.LoadFrom(nbt.Compound{"NoAI": nbt.Byte(1), "Health": nbt.Float(7), "NoGravity": nbt.Byte(1), "Sitting": nbt.Byte(1)}, nil, nil)
	if !m.NoAI || !m.NoGravity || !m.Tamable.SittingPose || m.Health != 7 || m.MetadataRevision() <= before || m.EquipmentRevision() <= equipment {
		t.Fatal("bulk load did not invalidate both snapshots")
	}
	for group := MobMetadataHealth; group <= MobMetadataNoGravity; group++ {
		if !m.MetadataDirty().Has(group) {
			t.Fatalf("bulk load omitted capture %d", group)
		}
	}
	before = m.MetadataRevision()
	saved := nbt.Compound{}
	m.SaveTo(saved, nil)
	if !nbt.BoolField(saved, "NoGravity") || !nbt.BoolField(saved, "NoAI") || !nbt.BoolField(saved, "Sitting") || m.MetadataRevision() != before {
		t.Fatal("saving codec fields must preserve their values without invalidating metadata")
	}
	m.ClearMetadataDirty()
	before = m.MetadataRevision()
	m.LoadFrom(nbt.Compound{}, nil, nil)
	if m.Health != 7 || m.NoAI || m.NoGravity || m.Tamable.SittingPose || m.MetadataRevision() <= before || !m.MetadataDirty().Has(MobMetadataHealth) {
		t.Fatal("bulk invalidation must preserve the codec's absent-tag defaults")
	}
}

// vanilla: Bucketable.loadDefaultDataFromBucketTag, Entity.setNoGravity
func TestMobMetadataBucketGravityInvalidatesSnapshots(t *testing.T) {
	for _, tc := range []struct {
		name         string
		before, want bool
		tag          nbt.Compound
	}{
		{"set", false, true, nbt.Compound{"NoGravity": nbt.Byte(1)}},
		{"clear", true, false, nbt.Compound{"NoGravity": nbt.Byte(0)}},
		{"equal", true, true, nbt.Compound{"NoGravity": nbt.Byte(1)}},
		{"absent", true, true, nbt.Compound{}},
		{"unreadable", true, true, nbt.Compound{"NoGravity": nbt.String("invalid")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewMob(TypeCod, 7, .5, 64, .5, 11)
			m.SetMetadataNoGravity(tc.before)
			m.SetMetadataFromBucket(true)
			m.ClearMetadataDirty()
			before := m.MetadataRevision()
			m.LoadBucketTag(tc.tag)
			changed := tc.before != tc.want
			if m.NoGravity != tc.want || m.MetadataDirty().Has(MobMetadataNoGravity) != changed || (m.MetadataRevision() != before) != changed {
				t.Fatal("bucket gravity did not preserve optional reads and invalidate changed metadata")
			}
		})
	}
}

func TestMobMetadataNestedSimulationInvalidation(t *testing.T) {
	for _, kind := range []MobType{TypeGoat, TypeFrog} {
		m := NewMob(kind, 7, .5, 64, .5, 11)
		m.ClearMetadataDirty()
		m.setLongJumpPose(true)
		if !m.LongJumping() || !m.MetadataDirty().Has(MobMetadataPose) {
			t.Fatalf("kind %d: jump pose bypassed invalidation", kind)
		}
		m.ClearMetadataDirty()
		before := m.MetadataRevision()
		m.replaceLongJump(goatJump{cooldown: 19})
		if m.LongJumping() || m.LongJumpCooldown() != 19 || m.MetadataRevision() == before || !m.MetadataDirty().Has(MobMetadataPose) {
			t.Fatalf("kind %d: jump replacement lost its capture union", kind)
		}
	}
	fox := NewMob(TypeFox, 7, .5, 64, .5, 11)
	fox.SetMetadataFoxCrouching(true)
	fox.SetMetadataFoxSleeping(true)
	fox.SetMetadataFoxDefending(true)
	fox.SetMetadataFoxPouncing(true)
	fox.ClearMetadataDirty()
	fox.clearFoxStates()
	if !fox.MetadataDirty().Has(MobMetadataFox) || fox.FoxFlags() != protocol.FoxPouncing {
		t.Fatal("fox clearing must invalidate its flags and retain pouncing")
	}
	fox.ClearMetadataDirty()
	before := fox.MetadataRevision()
	fox.clearFoxStates()
	if fox.MetadataRevision() != before {
		t.Fatal("equal fox clearing dirtied metadata")
	}
}

func TestMobMetadataAngerSimulationKeepsEndTime(t *testing.T) {
	m := NewMob(TypeWolf, 7, .5, 64, .5, 11)
	m.SetMetadataGameTime(100)
	m.SetAnger(20, [16]byte{}, false)
	end := m.AngerEndTime()
	m.ClearMetadataDirty()
	m.SetMetadataGameTime(101)
	m.angerStep(&tickView{})
	if m.AngerEndTime() != end || !m.MetadataDirty().Has(MobMetadataAngerEnd) {
		t.Fatal("clock and countdown must invalidate without moving the final end time")
	}
	m.ClearMetadataDirty()
	m.SetAnger(0, [16]byte{}, false)
	if m.AngerEndTime() != -1 || !m.MetadataDirty().Has(MobMetadataAngerEnd) {
		t.Fatal("anger cancellation bypassed invalidation")
	}
}
