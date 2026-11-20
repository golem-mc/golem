package registry

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

func TestComponentSoundDefaultsAreResolved(t *testing.T) {
	t.Parallel()
	check := func(sound protocol.SoundHolder) {
		t.Helper()
		if sound.Name != "" || sound.ID < 0 || int(sound.ID) >= len(SoundEvents) {
			t.Fatalf("unresolved default sound %+v", sound)
		}
	}
	for item := range ItemCount {
		if use, ok := ConsumableOf(int32(item)); ok {
			check(use.Sound)
			for _, e := range use.Effects {
				if e.Type == PlaySound {
					check(e.Sound)
				}
			}
		}
		if eq, ok := Item(item).Equippable(); ok {
			check(eq.EquipSound)
			check(eq.ShearingSound)
		}
	}
}

func TestSoundByNameKeepsAPackSoundInline(t *testing.T) {
	built := SoundByName("minecraft:entity.cat.ambient")
	if built.IsPack() || built.Holder().Name != "" {
		t.Errorf("a built-in sound travels by ID, got %+v", built.Holder())
	}
	pack := SoundByName("test:horn.loud")
	if !pack.IsPack() || pack != SoundByName("test:horn.loud") {
		t.Fatalf("a pack sound is interned once, got %d", pack)
	}
	if h := pack.Holder(); h.ID != -1 || h.Name != "test:horn.loud" {
		t.Errorf("a pack sound's holder = %+v, want the inline name", h)
	}
}
