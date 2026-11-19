package protocol_test

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

func TestAxolotlBucketVariantSurvivesSlotCodec(t *testing.T) {
	for variant := range int32(5) {
		stack := protocol.ItemStack{ItemID: 1, Count: 1}
		stack.SetAxolotlVariant(variant)
		var b protocol.Buffer
		b.PutSlot(stack)
		out, ok := protocol.NewReader(b.Bytes()).ReadSlot()
		if !ok {
			t.Fatalf("slot codec rejected variant %d", variant)
		}
		if got, ok := out.AxolotlVariant(); !ok || got != variant {
			t.Fatalf("variant=%d (%v), want %d", got, ok, variant)
		}
	}
}
