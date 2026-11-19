package registry

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// rocketWith is a rocket of the given flight and explosions.
func rocketWith(flight int32, explosions ...protocol.FireworkExplosion) protocol.ItemStack {
	stack := protocol.ItemStack{ItemID: MustItem("minecraft:firework_rocket"), Count: 1}
	stack.SetRaw(protocol.FireworksRawComponent(flight, explosions))
	return stack
}

// fireworksEntry is a stored rocket whose fireworks component is value.
func fireworksEntry(value nbt.Tag) nbt.Compound {
	return nbt.Compound{
		"id":         nbt.String("minecraft:firework_rocket"),
		"count":      nbt.Int(1),
		"components": nbt.Compound{"minecraft:fireworks": value},
	}
}

// TestFireworksSurviveTheStackCodec writes a rocket and a star and reads
// them back whole.
// vanilla: Fireworks.CODEC, FireworkExplosion.CODEC
func TestFireworksSurviveTheStackCodec(t *testing.T) {
	burst := protocol.FireworkExplosion{
		Shape: 4, Colors: []int32{1, 2}, FadeColors: []int32{3}, Twinkle: true,
	}
	rocket := rocketWith(3, burst, protocol.FireworkExplosion{Shape: 3})
	star := protocol.ItemStack{ItemID: MustItem("minecraft:firework_star"), Count: 1}
	star.SetRaw(burst.RawComponent())
	for _, stack := range []protocol.ItemStack{rocket, star} {
		got, ok := StoredStack(StackTag(stack))
		if !ok {
			t.Fatalf("the stored reading refused %v", StackTag(stack))
		}
		if !got.Equal(stack) {
			t.Errorf("came back as %+v, want %+v", got, stack)
		}
	}
}
