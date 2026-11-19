package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

// rabbitVariantEgg is a rabbit spawn egg carrying minecraft:rabbit/variant.
func rabbitVariantEgg(t *testing.T, variant int32) protocol.ItemStack {
	t.Helper()
	var b protocol.Buffer
	b.PutVarInt(variant)
	return protocol.ItemStack{
		ItemID: servertest.MustItemID(t, "minecraft:rabbit_spawn_egg"), Count: 1,
		Raw: []protocol.RawComponent{{Type: protocol.DataComponentRabbitVariant, Data: b.Bytes()}},
	}
}

// TestARabbitEggTakesItsVariantComponent: a spawn egg's rabbit/variant
// component is applied after the finalize's own biome roll
// (Rabbit.applyImplicitComponent), so the stack decides the variant; an id
// Rabbit.Variant does not know reads as brown.
func TestARabbitEggTakesItsVariantComponent(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name         string
		variant      int32
		want         int32
		present, bun bool
	}{
		{"black", 2, 2, true, false},
		{"splotched", 3, 3, true, false},
		{"unknown", 42, 0, false, false},
		{"killer bunny", 99, 99, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.EggPlayer(t, "survival", "Breeder", "minecraft:rabbit_spawn_egg", 1)
			srv.GiveSlot("Breeder", player.HotbarSlot, rabbitVariantEgg(t, tc.variant))
			c.PlaceBlock(2, 63, 3, protocol.FaceTop)
			rabbit := c.ExpectMobSpawn(protocol.EntityTypeRabbit)
			if rabbit.RabbitType != tc.want || rabbit.HasRabbitType != tc.present {
				t.Errorf("variant %d (sent %v), want %d (sent %v)", rabbit.RabbitType, rabbit.HasRabbitType, tc.want, tc.present)
			}
			name, ok := rabbit.Name.(nbt.Compound)
			if named := ok && name["translate"] == nbt.String("entity.minecraft.killer_bunny"); named != tc.bun {
				t.Errorf("named killer bunny %v, want %v", named, tc.bun)
			}
		})
	}
}
