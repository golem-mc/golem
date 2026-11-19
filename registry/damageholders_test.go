package registry

import (
	"bytes"
	"reflect"
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

// Tag resolution belongs to component ingestion, including a wire round trip,
// rather than each equipment-wear or blocking query.
func TestDamageComponentsBindTagsBeforeHurt(t *testing.T) {
	resolve := protocol.DamageTypeTagIndex
	calls := 0
	protocol.DamageTypeTagIndex = func(name string) uint16 { calls++; return resolve(name) }
	t.Cleanup(func() { protocol.DamageTypeTagIndex = resolve })
	stack := protocol.ItemStack{ItemID: int32(ItemIronHelmet), Count: 1}
	stack.SetDamageResistant(protocol.DamageSetComponent{Present: true, Tag: "minecraft:is_fire"})
	stack.SetBlocksAttacks(protocol.BlocksAttacksComponent{BypassedBy: protocol.DamageSetComponent{Present: true, Tag: "minecraft:is_fire"}})
	var wire protocol.Buffer
	wire.PutSlot(stack)
	decoded, ok := protocol.NewReader(wire.Bytes()).ReadSlot()
	if !ok || calls == 0 {
		t.Fatal("component ingestion did not bind tags")
	}
	before := calls
	for range 5 {
		for _, s := range []protocol.ItemStack{stack, decoded} {
			if !StackResistsDamage(s, DamageTypeLava) {
				t.Fatal("lost fire resistance")
			}
			b, ok := StackBlocksAttacks(s)
			if !ok || !b.BypassedBy.Contains(DamageTypeLava) {
				t.Fatal("lost shield bypass")
			}
		}
	}
	if calls != before {
		t.Fatalf("hurt queries resolved %d tag names", calls-before)
	}
}

// Replacing a component must replace its bound snapshot without changing a
// copied stack's component, including explicit lists and removals.
func TestDamageComponentReplacementKeepsCopiesIndependent(t *testing.T) {
	stack := protocol.ItemStack{ItemID: int32(ItemIronHelmet), Count: 1}
	stack.SetDamageResistant(protocol.DamageSetComponent{Present: true, Tag: "minecraft:is_fire"})
	copy := stack
	stack.SetDamageResistant(protocol.DamageSetComponent{Present: true, IDs: []int32{int32(DamageTypeMagic) - 1}})
	if !StackResistsDamage(copy, DamageTypeLava) || StackResistsDamage(copy, DamageTypeMagic) || StackResistsDamage(stack, DamageTypeLava) || !StackResistsDamage(stack, DamageTypeMagic) {
		t.Fatal("replacement changed a shared snapshot or kept stale membership")
	}
	stack.SetDamageResistant(protocol.DamageSetComponent{Present: true})
	if StackResistsDamage(stack, DamageTypeMagic) {
		t.Fatal("empty set retained membership")
	}
	stack.Remove(protocol.DataComponentDamageResistant)
	if StackResistsDamage(stack, DamageTypeMagic) {
		t.Fatal("removed component retained membership")
	}
	for _, set := range []DamageTypeSet{{}, {Types: []DamageType{}}, {Tag: noDamageTypeTag, IsTag: true}} {
		if set.Contains(0) || set.Contains(DamageTypeLava) {
			t.Fatalf("empty holder set matched: %+v", set)
		}
	}
}

func TestDamageComponentGettersKeepCopiesIndependent(t *testing.T) {
	for _, wireRoundTrip := range []bool{false, true} {
		name := "setter"
		if wireRoundTrip {
			name = "wire"
		}
		t.Run(name, func(t *testing.T) {
			prepare := func(stack protocol.ItemStack) protocol.ItemStack {
				t.Helper()
				if !wireRoundTrip {
					return stack
				}
				var wire protocol.Buffer
				wire.PutSlot(stack)
				decoded, ok := protocol.NewReader(wire.Bytes()).ReadSlot()
				if !ok {
					t.Fatal("wire round trip refused")
				}
				return decoded
			}
			t.Run("resistant", func(t *testing.T) {
				want := protocol.DamageSetComponent{Present: true, IDs: []int32{int32(DamageTypeLava) - 1}}
				stack := protocol.ItemStack{ItemID: int32(ItemIronHelmet), Count: 1}
				stack.SetDamageResistant(want)
				stack = prepare(stack)
				copy := stack
				own, ok := copy.DamageResistantOf()
				if !ok {
					t.Fatal("missing resistance")
				}
				own.IDs[0] = int32(DamageTypeMagic) - 1
				checkOriginal := func() {
					t.Helper()
					if got, ok := stack.DamageResistantOf(); !ok || !reflect.DeepEqual(got, want) {
						t.Errorf("original resistance = %+v, %v; want %+v", got, ok, want)
					}
					if !StackResistsDamage(stack, DamageTypeLava) || StackResistsDamage(stack, DamageTypeMagic) {
						t.Error("editing the getter result changed original resistance")
					}
				}
				checkOriginal()
				copy.SetDamageResistant(own)
				checkOriginal()
				if StackResistsDamage(copy, DamageTypeLava) || !StackResistsDamage(copy, DamageTypeMagic) {
					t.Error("edited copy did not acquire magic resistance")
				}
			})
			t.Run("blocks", func(t *testing.T) {
				want := protocol.BlocksAttacksComponent{
					Reductions: []protocol.DamageReductionComponent{{
						Angle: 90, Factor: 1,
						Type: protocol.DamageSetComponent{Present: true, IDs: []int32{int32(DamageTypeLava) - 1}},
					}},
					BypassedBy:   protocol.DamageSetComponent{Present: true, IDs: []int32{int32(DamageTypeMagic) - 1}},
					BlockSound:   &protocol.SoundHolder{Name: "minecraft:item.shield.block", HasRange: true, Range: 16},
					DisableSound: &protocol.SoundHolder{Name: "minecraft:item.shield.break"},
				}
				stack := protocol.ItemStack{ItemID: int32(ItemShield), Count: 1}
				stack.SetBlocksAttacks(want)
				stack = prepare(stack)
				copy := stack
				own, ok := copy.BlocksAttacksOf()
				if !ok {
					t.Fatal("missing blocks_attacks")
				}
				own.Reductions[0].Factor = 0.5
				own.Reductions[0].Type.IDs[0] = int32(DamageTypeMagic) - 1
				own.BypassedBy.IDs[0] = int32(DamageTypeLava) - 1
				own.BlockSound.Range = 32
				own.DisableSound.Name = "minecraft:item.shield.block"
				checkOriginal := func() {
					t.Helper()
					if got, ok := stack.BlocksAttacksOf(); !ok || !reflect.DeepEqual(got, want) {
						t.Error("editing the getter result changed original blocks_attacks")
					}
					blocks, ok := StackBlocksAttacks(stack)
					if !ok || blocks.Reductions[0].Factor != 1 || !blocks.Reductions[0].Type.Contains(DamageTypeLava) ||
						!blocks.BypassedBy.Contains(DamageTypeMagic) {
						t.Error("editing the getter result changed original blocking behavior")
					}
				}
				checkOriginal()
				copy.SetBlocksAttacks(own)
				checkOriginal()
				if got, ok := copy.BlocksAttacksOf(); !ok || !reflect.DeepEqual(got, own) {
					t.Error("blocks_attacks edits were not applied to copy")
				}
			})
		})
	}
}
