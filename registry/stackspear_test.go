package registry

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// TestStackSpearComponentsReadOwnRemovedAndDefault checks that a stack's own
// kinetic_weapon and piercing_weapon win over the item's, a removal hides the
// item's, and a bare stack falls back to the item.
// vanilla: ItemStack.get(DataComponents.KINETIC_WEAPON, PIERCING_WEAPON)
func TestStackSpearComponentsReadOwnRemovedAndDefault(t *testing.T) {
	stick := protocol.ItemStack{ItemID: int32(ItemStick), Count: 1}
	if StackKineticWeapon(stick) != nil {
		t.Error("a plain stick has a kinetic_weapon")
	}
	if _, ok := StackPiercingWeapon(stick); ok {
		t.Error("a plain stick has a piercing_weapon")
	}
	if !readComponent(&stick, "minecraft:kinetic_weapon", nbt.Compound{
		"delay_ticks":            nbt.Int(7),
		"contact_cooldown_ticks": nbt.Int(9),
		"damage_multiplier":      nbt.Float(2),
		"damage_conditions":      nbt.Compound{"max_duration_ticks": nbt.Int(30), "min_speed": nbt.Float(1.5)},
	}) {
		t.Fatal("kinetic_weapon was refused")
	}
	if !readComponent(&stick, "minecraft:piercing_weapon", nbt.Compound{"deals_knockback": nbt.Byte(0)}) {
		t.Fatal("piercing_weapon was refused")
	}
	k := StackKineticWeapon(stick)
	if k == nil || k.Delay != 7 || k.ContactCooldown != 9 || k.DamageMultiplier != 2 ||
		k.Damage == nil || k.Damage.MaxDuration != 30 || k.Damage.MinSpeed != 1.5 || k.Knockback != nil {
		t.Errorf("StackKineticWeapon = %+v", k)
	}
	if p, ok := StackPiercingWeapon(stick); !ok || p.DealsKnockback {
		t.Errorf("StackPiercingWeapon = %+v, %v", p, ok)
	}

	spear := protocol.ItemStack{ItemID: int32(ItemIronSpear), Count: 1}
	if StackKineticWeapon(spear) == nil {
		t.Error("an iron spear lost its own kinetic_weapon")
	}
	if _, ok := StackPiercingWeapon(spear); !ok {
		t.Error("an iron spear lost its own piercing_weapon")
	}
	spear.Remove(protocol.DataComponentKineticWeapon)
	spear.Remove(protocol.DataComponentPiercingWeapon)
	if StackKineticWeapon(spear) != nil {
		t.Error("a removed kinetic_weapon still reads")
	}
	if _, ok := StackPiercingWeapon(spear); ok {
		t.Error("a removed piercing_weapon still reads")
	}
}
