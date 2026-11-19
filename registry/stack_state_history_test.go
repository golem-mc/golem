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
