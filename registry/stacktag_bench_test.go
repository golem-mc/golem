package registry

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// BenchmarkStackTagPlain writes the NBT of a stack with no component patch.
func BenchmarkStackTagPlain(b *testing.B) {
	stack := protocol.ItemStack{ItemID: int32(ItemDiamond), Count: 1}
	b.ReportAllocs()
	for b.Loop() {
		StackTag(stack)
	}
}
