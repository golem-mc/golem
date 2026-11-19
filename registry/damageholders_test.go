package registry

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// Bound custom tags survive reloads that add earlier-sorting tag names or
// remove the original tag; existing stack copies must see the new membership.
// vanilla: HolderSet.Named.contains, Holder.Reference.bindTags
func TestDamageHoldersKeepTheirTagsAcrossReloads(t *testing.T) {
	t.Cleanup(func() { ApplyPackTags(nil) })
	tag := DamageTypeTagOf("pack:zz_resistant")
	set := DamageTypeSet{Tag: tag, IsTag: true}
	stack := protocol.ItemStack{ItemID: int32(ItemIronHelmet), Count: 1}
	stack.SetDamageResistant(protocol.DamageSetComponent{Present: true, Tag: "pack:zz_resistant"})
	stack.SetBlocksAttacks(protocol.BlocksAttacksComponent{BypassedBy: protocol.DamageSetComponent{Present: true, Tag: "pack:zz_resistant"}})
	copy := stack
	var wire protocol.Buffer
	wire.PutSlot(stack)
	decoded, ok := protocol.NewReader(wire.Bytes()).ReadSlot()
	if !ok {
		t.Fatal("wire round trip refused")
	}
	check := func(wantLava, wantMagic bool) {
		t.Helper()
		if DamageTypeTagOf("pack:zz_resistant") != tag {
			t.Fatal("tag ID changed on reload")
		}
		for _, d := range []struct {
			typ  DamageType
			want bool
		}{{DamageTypeLava, wantLava}, {DamageTypeMagic, wantMagic}} {
			if set.Contains(d.typ) != d.want {
				t.Fatalf("bound set: %s membership != %v", d.typ, d.want)
			}
			for _, s := range []protocol.ItemStack{stack, copy, decoded} {
				if StackResistsDamage(s, d.typ) != d.want {
					t.Fatalf("stack: %s membership != %v", d.typ, d.want)
				}
				blocks, ok := StackBlocksAttacks(s)
				if !ok || blocks.BypassedBy == nil || blocks.BypassedBy.Contains(d.typ) != d.want {
					t.Fatalf("shield: %s membership != %v", d.typ, d.want)
				}
			}
		}
		var after protocol.Buffer
		after.PutSlot(stack)
		if !bytes.Equal(wire.Bytes(), after.Bytes()) {
			t.Fatal("reload changed component wire bytes")
		}
	}
	check(false, false)
	for _, member := range []string{"minecraft:lava", "minecraft:magic"} {
		problems := ApplyPackTags(PackTags{"minecraft:damage_type": {
			"pack:aa_earlier":   {Entries: []PackTagEntry{{ID: "minecraft:arrow", Required: true}}},
			"pack:zz_resistant": {Replace: true, Entries: []PackTagEntry{{ID: member, Required: true}}},
		}})
		if len(problems) > 0 {
			t.Fatal(problems)
		}
		check(member == "minecraft:lava", member == "minecraft:magic")
	}
	ApplyPackTags(nil)
	check(false, false)
}
