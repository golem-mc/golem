package registry

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

func TestBlockStateWireHistory(t *testing.T) {
	data, err := os.ReadFile("testdata/stack_state_wire_history.tsv")
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		t.Run(f[0], func(t *testing.T) {
			tag, err := nbt.ParseOrderedSNBT(f[1])
			if err != nil {
				t.Fatal(err)
			}
			stack, ok := StoredStack(tag)
			if !ok {
				t.Fatal("invalid fixture")
			}
			for _, untrusted := range []bool{false, true} {
				var wire protocol.Buffer
				if untrusted {
					wire.PutUntrustedSlot(stack)
				} else {
					wire.PutSlot(stack)
				}
				r := protocol.NewReader(wire.Bytes())
				var got protocol.ItemStack
				if untrusted {
					got, ok = r.ReadUntrustedSlot()
				} else {
					got, ok = r.ReadSlot()
				}
				if !ok || r.Err() != nil || r.Remaining() != 0 {
					t.Fatalf("wire read: %v, %v", ok, r.Err())
				}
				want, err := nbt.ParseOrderedSNBT(f[3])
				if err != nil {
					t.Fatal(err)
				}
				for range 2 {
					child, ok := got.BlockStatePropertiesTag()
					if !ok || !nbt.Equal(child, want) || !slices.Equal(nbt.HashOrderedKeys(child), nbt.HashOrderedKeys(want)) {
						t.Fatalf("untrusted=%v: %v, want %s", untrusted, child, f[3])
					}
					got, ok = StoredStack(StackTag(got))
					if !ok {
						t.Fatal("save round trip refused")
					}
				}
			}
		})
	}
}

func TestBlockStateHistoryCopiesOwnTheirChanges(t *testing.T) {
	input, _ := nbt.ParseOrderedSNBT(`{BB:"1",Aa:"2",v:"3",f:"4"}`)
	var stack protocol.ItemStack
	if !stack.SetBlockStatePropertiesTag(input) {
		t.Fatal("valid properties refused")
	}
	reversed, _ := nbt.ParseOrderedSNBT(`{Aa:"2",BB:"1",f:"4",v:"3"}`)
	var equivalent protocol.ItemStack
	equivalent.SetBlockStatePropertiesTag(reversed)
	if !stack.Same(equivalent) {
		t.Fatal("equal property maps do not stack across different insertion histories")
	}
	input.Set("BB", nbt.String("changed"))
	copy := stack
	copy.SetBlockStateProperties(map[string]string{"level": "5"})
	got, _ := stack.BlockStatePropertiesTag()
	if got.Entries()["BB"] != nbt.String("1") || !slices.Equal(nbt.HashOrderedKeys(got), []string{"BB", "Aa", "v", "f"}) {
		t.Fatalf("mutation reached original: %v", got)
	}
	got.Set("BB", nbt.String("getter mutation"))
	got, _ = stack.BlockStatePropertiesTag()
	if got.Entries()["BB"] != nbt.String("1") {
		t.Fatal("getter exposed mutable state")
	}
	if stack.SetBlockStatePropertiesTag(nbt.Compound{"bad": nbt.Int(1)}) {
		t.Fatal("non-string property accepted")
	}
	got, _ = stack.BlockStatePropertiesTag()
	if got.Entries()["BB"] != nbt.String("1") {
		t.Fatal("refused update changed stack")
	}
}

func TestDebugStickHistorySurvivesSelectionChanges(t *testing.T) {
	input, _ := nbt.ParseOrderedSNBT(`{"minecraft:oak_stairs":"facing","minecraft:bamboo_stairs":"half","minecraft:stone_stairs":"shape","minecraft:spruce_stairs":"waterlogged"}`)
	stack := protocol.ItemStack{ItemID: int32(ItemDebugStick), Count: 1}
	if !ReadComponent(&stack, "minecraft:debug_stick_state", input) {
		t.Fatal("valid selections refused")
	}
	copy := stack
	copy.SetDebugStickProperty("minecraft:oak_stairs", "half")
	copy.SetDebugStickProperty("minecraft:stone_brick_stairs", "facing")
	if stack.DebugStick["minecraft:oak_stairs"] != "facing" || len(stack.DebugStick) != 4 {
		t.Fatal("selection change reached original stack")
	}
	before := copy.DebugStickTag()
	got, ok := StoredStack(StackTag(copy))
	if !ok || !nbt.Equal(before, got.DebugStickTag()) || !slices.Equal(nbt.HashOrderedKeys(before), nbt.HashOrderedKeys(got.DebugStickTag())) {
		t.Fatal("selection history changed during save round trip")
	}
}
