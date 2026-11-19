package registry

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// TestAUseCooldownIsReadAndSavedBack reads use_cooldown values, refusing a
// non-positive seconds or a bad group, and writes a kept one back as read.
// vanilla: UseCooldown.CODEC
func TestAUseCooldownIsReadAndSavedBack(t *testing.T) {
	snowball := int32(ItemSnowball)
	for _, refused := range []nbt.Compound{
		{"seconds": nbt.Float(0)},
		{"seconds": nbt.Float(-1)},
		{"cooldown_group": nbt.String("a:b")},
		{"seconds": nbt.Float(1), "cooldown_group": nbt.String("Bad Group")},
	} {
		stack := protocol.ItemStack{ItemID: snowball, Count: 1}
		if ReadComponent(&stack, "minecraft:use_cooldown", refused) {
			t.Errorf("use_cooldown %v was read, want it refused", refused)
		}
	}

	stack := protocol.ItemStack{ItemID: snowball, Count: 1}
	if !ReadComponent(&stack, "minecraft:use_cooldown", nbt.Compound{"seconds": nbt.Float(2), "cooldown_group": nbt.String("g")}) {
		t.Fatal("a valid use_cooldown was refused")
	}
	if ticks, group, ok := StackUseCooldown(stack); !ok || ticks != 40 || group != "minecraft:g" {
		t.Errorf("StackUseCooldown = %d, %q, %v, want 40, minecraft:g, true", ticks, group, ok)
	}
	components := StackComponents(stack)
	saved, ok := nbt.CompoundView(components["minecraft:use_cooldown"])
	if !ok {
		t.Fatalf("saved use_cooldown = %v, want a compound", components["minecraft:use_cooldown"])
	}
	if saved["seconds"] != nbt.Float(2) || saved["cooldown_group"] != nbt.String("minecraft:g") {
		t.Errorf("saved use_cooldown = %v", saved)
	}

	if ticks, group, ok := StackUseCooldown(protocol.ItemStack{ItemID: int32(ItemEnderPearl), Count: 1}); !ok || ticks != 20 || group != "minecraft:ender_pearl" {
		t.Errorf("the pearl's default = %d, %q, %v, want 20, minecraft:ender_pearl, true", ticks, group, ok)
	}
	if _, _, ok := StackUseCooldown(protocol.ItemStack{ItemID: snowball, Count: 1}); ok {
		t.Error("a plain snowball has a use_cooldown")
	}
}
