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
