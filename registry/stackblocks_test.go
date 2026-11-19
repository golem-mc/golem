package registry

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// TestBlocksAttacksAndWeaponRoundTripThroughAStack checks that a stored
// stack's blocks_attacks and weapon read into the stack, come back out as the
// same tags and win over the item's defaults.
// vanilla: BlocksAttacks.CODEC, Weapon.CODEC
func TestBlocksAttacksAndWeaponRoundTripThroughAStack(t *testing.T) {
	stick, _ := ItemID("minecraft:stick")
	stack := protocol.ItemStack{ItemID: stick, Count: 1}
	reductions := nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
		nbt.Compound{"base": nbt.Float(0), "factor": nbt.Float(0.5), "type": nbt.String("#minecraft:is_fire")},
	}}
	if !readComponent(&stack, "minecraft:blocks_attacks", nbt.Compound{
		"damage_reductions": reductions, "bypassed_by": nbt.String("minecraft:magic"), "block_sound": nbt.String("minecraft:item.shield.block"),
	}) {
		t.Fatal("blocks_attacks was refused")
	}
	if !readComponent(&stack, "minecraft:weapon", nbt.Compound{"disable_blocking_for_seconds": nbt.Float(3)}) {
		t.Fatal("weapon was refused")
	}
	b, ok := StackBlocksAttacks(stack)
	if !ok || len(b.Reductions) != 1 || b.Reductions[0].Factor != 0.5 || b.Reductions[0].Type.Tag != DamageTypeTagIDIsFire ||
		b.BypassedBy == nil || len(b.BypassedBy.Types) != 1 || b.BypassedBy.Types[0] != DamageTypeMagic ||
		(b.BlockSound == nil || *b.BlockSound != SoundItemShieldBlock.Holder()) || b.DisableSound != nil || b.DisableCooldownScale != 1 ||
		b.ItemDamage != (ItemDamageFunction{Threshold: 1, Factor: 1}) {
		t.Errorf("StackBlocksAttacks = %+v, %v", b, ok)
	}
	if w, ok := StackWeapon(stack); !ok || w.DamagePerAttack != 1 || w.DisableBlockingSeconds != 3 {
		t.Errorf("StackWeapon = %+v, %v", w, ok)
	}
	if _, ok := StackBlocksAttacks(protocol.ItemStack{ItemID: stick, Count: 1}); ok {
		t.Error("a plain stick blocks attacks")
	}
	own, _ := stack.BlocksAttacksOf()
	var again protocol.ItemStack
	if !readComponent(&again, "minecraft:blocks_attacks", blocksAttacksTag(own)) {
		t.Fatal("the written tag was refused on reading back")
	}
	if got, _ := again.BlocksAttacksOf(); len(got.Reductions) != 1 || got.BypassedBy.IDs[0] != own.BypassedBy.IDs[0] {
		t.Errorf("the tag round trip = %+v, want %+v", got, own)
	}
	if readComponent(&again, "minecraft:weapon", nbt.Compound{"disable_blocking_for_seconds": nbt.Float(-1)}) {
		t.Error("a negative disable time was accepted")
	}
}
