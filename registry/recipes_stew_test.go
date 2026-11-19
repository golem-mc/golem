package registry

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

func stack(name string, count int32) protocol.ItemStack {
	return protocol.ItemStack{ItemID: mustItemID(name), Count: count}
}

func TestPoppyStewCarriesNightVision(t *testing.T) {
	cells := []protocol.ItemStack{
		stack("minecraft:bowl", 1), stack("minecraft:brown_mushroom", 1),
		stack("minecraft:red_mushroom", 1), stack("minecraft:poppy", 1),
	}
	got := CraftResult(cells, 2)
	if got.ItemID != mustItemID("minecraft:suspicious_stew") || got.Count != 1 {
		t.Fatalf("result = %+v, want one suspicious_stew", got)
	}
	var want protocol.Buffer
	want.PutVarInt(1)
	want.PutVarInt(MustMobEffect("minecraft:night_vision"))
	want.PutVarInt(100)
	if len(got.Raw) != 1 || got.Raw[0].Type != protocol.DataComponentSuspiciousStew ||
		string(got.Raw[0].Data) != string(want.Bytes()) {
		t.Fatalf("raw = %+v, want night_vision for 100 ticks", got.Raw)
	}
}
