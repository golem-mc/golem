package registry

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// TestPotionTableIsTheWireRegistry pins the generated potion table to the
// registry the client keeps built into itself, whose size
// protocol.RegistrySizePotion bounds the potion a raw potion_contents
// component names.
func TestPotionTableIsTheWireRegistry(t *testing.T) {
	if len(potions) != protocol.RegistrySizePotion {
		t.Fatalf("the table holds %d potions, protocol.RegistrySizePotion says %d",
			len(potions), protocol.RegistrySizePotion)
	}
	if got, want := MustPotion("minecraft:swiftness"), int32(13); got != want {
		t.Errorf("swiftness = %d, want %d", got, want)
	}
	specs := PotionEffects(MustPotion("minecraft:swiftness"))
	if len(specs) != 1 || specs[0].Effect != "minecraft:speed" ||
		specs[0].Duration != 3600 || specs[0].Amplifier != 0 ||
		!specs[0].ShowParticles || !specs[0].ShowIcon || specs[0].Ambient {
		t.Errorf("swiftness = %+v, want speed for 3600 ticks, particles and icon", specs)
	}
	if specs := PotionEffects(MustPotion("minecraft:water")); specs != nil {
		t.Errorf("water = %+v, want no effects", specs)
	}
	if specs := PotionEffects(-1); specs != nil {
		t.Errorf("PotionEffects(-1) = %v, want none for an ID out of the registry", specs)
	}
}
