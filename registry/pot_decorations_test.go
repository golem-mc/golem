package registry

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// vanilla: PotDecorations.CODEC, ItemStackTemplate.MAP_CODEC
func TestPotDecorationTemplatesOmitDefaultCount(t *testing.T) {
	sides := PotAllBrick()
	sides[1].Count = 3
	sides[2].SetCustomData(nbt.Compound{"decoration": nbt.Int(7)})
	sides[3] = protocol.ItemStack{}
	tag := PotDecorationsTag(sides).Entries()
	if len(tag) != 3 {
		t.Fatalf("saved sides = %v, want three present sides", tag)
	}
	for _, key := range []string{"back", "right"} {
		entry, _ := nbt.CompoundView(tag[key])
		if _, ok := entry["count"]; ok {
			t.Errorf("%s writes a default count: %v", key, entry)
		}
		if stack, ok := StoredStack(entry); !ok || stack.Count != 1 {
			t.Errorf("%s reads as %+v, %v; want count 1", key, stack, ok)
		}
	}
	if count, _ := nbt.Get[nbt.Int](tag["left"].(nbt.CompoundTag).Entries(), "count"); count != 3 {
		t.Errorf("non-default count = %d, want 3", count)
	}
	if _, ok := tag["right"].(nbt.CompoundTag).Entries()["components"]; !ok {
		t.Error("decorated template lost its components")
	}
	got, ok := PotDecorationsOf(tag)
	if !ok {
		t.Fatal("saved templates do not read back")
	}
	for i := range sides {
		if !got[i].Equal(sides[i]) {
			t.Errorf("side %d = %+v, want %+v", i, got[i], sides[i])
		}
	}
}
