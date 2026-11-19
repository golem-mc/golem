package entity

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestEntityTypeTableFallbacks(t *testing.T) {
	for _, typ := range []MobType{-1, NoMobType, registry.EntityTypeItem, registry.EntityTypeCount, 1<<31 - 1} {
		if KnownMobKind(typ) || SpawnableKind(typ) || CreatureKind(typ) {
			t.Errorf("unmodelled type %d is a spawnable mob", typ)
		}
		if _, ok := SpawnCategoryOf(typ); ok {
			t.Errorf("unmodelled type %d counts toward a cap", typ)
		}
		if _, ok := MobName(typ); ok {
			t.Errorf("unmodelled type %d has a mob name", typ)
		}
		if MobTrackingRange(typ) != 5 || MobUpdateInterval(typ) != 3 {
			t.Errorf("unlisted type %d lost default tracking", typ)
		}
		if _, ok := MinecartKindForType(typ); ok {
			t.Errorf("unlisted type %d is a cart", typ)
		}
		if sound, ok := ParrotImitationSound(typ); ok || sound != registry.SoundNone {
			t.Errorf("unlisted imitation %d = %q, %v", typ, sound, ok)
		}
		if IsChestBoat(typ) || IsRaft(typ) || BoatCapacity(typ) != 2 {
			t.Errorf("unlisted type %d lost default seats", typ)
		}
	}
	// An intentional empty sound still participates in the parrot's draw.
	if sound, ok := ParrotImitationSound(registry.EntityTypeHappyGhast); !ok || sound != registry.SoundIntentionallyEmpty {
		t.Errorf("happy ghast imitation = %q, %v; want empty, true", sound, ok)
	}
	if sound, ok := ParrotImitationSound(registry.EntityTypeBlaze); !ok || sound != registry.SoundEntityParrotImitateBlaze {
		t.Errorf("blaze imitation = %q, %v", sound, ok)
	}
	// A mount without an explicit passenger point uses its body height.
	cow, rabbit := NewMob(TypeCow, 1, 0, 0, 0, 1), NewMob(TypeRabbit, 2, 0, 0, 0, 1)
	if _, y, _ := vehiclePassengerPoint(cow, 0); y != 1.36875 {
		t.Errorf("cow seat = %v", y)
	}
	if _, y, _ := vehiclePassengerPoint(rabbit, 0); y != rabbit.height() {
		t.Errorf("rabbit seat = %v, want %v", y, rabbit.height())
	}
}
