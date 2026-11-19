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

// TestARabbitEggOnARabbitCarriesTheVariantToTheBaby: the egg used on a
// rabbit makes a baby that takes the parent's variant
// (Mob.getBreedOffspring) and then the stack's component over it.
func TestARabbitEggOnARabbitCarriesTheVariantToTheBaby(t *testing.T) {
	t.Parallel()
	srv, c := servertest.EggPlayer(t, "survival", "Breeder", "minecraft:rabbit_spawn_egg", 1)
	srv.GiveSlot("Breeder", player.HotbarSlot, rabbitVariantEgg(t, 4))
	id := srv.SpawnMob("minecraft:rabbit", 1.5, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeRabbit)
	c.UseOn(id)
	baby := c.ExpectMobSpawn(protocol.EntityTypeRabbit)
	if !baby.HasBaby || baby.RabbitType != 4 {
		t.Errorf("baby %v variant %d, want a baby of variant 4", baby.HasBaby, baby.RabbitType)
	}
}

// TestARabbitClimbsOutOfPowderSnow: a rabbit sunk in a field of powder snow
// keeps jumping (ClimbOnTopOfPowderSnowGoal) until it stands on top of it.
func TestARabbitClimbsOutOfPowderSnow(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "creative", "Watcher", "", 0)
	snow := servertest.MustState(t, "minecraft:powder_snow", nil)
	for x := int32(8); x <= 18; x++ {
		for z := int32(8); z <= 18; z++ {
			srv.SetBlock(x, 64, z, snow)
		}
	}
	id := srv.SpawnMob("minecraft:rabbit", 13.5, 64, 13.5)
	c.ExpectMobSpawn(protocol.EntityTypeRabbit)
	servertest.AwaitTicks(t, srv, 120)
	m, _ := servertest.MobByID(srv, id)
	if m.Y < 64.99 {
		t.Errorf("the rabbit is at y=%.2f, want it on top of the snow at y=65", m.Y)
	}
}
