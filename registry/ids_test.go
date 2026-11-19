package registry

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

func TestBlockOfMatchesStateNames(t *testing.T) {
	for s := range uint16(StateCount) {
		name, _, ok := BlockStateName(s)
		if !ok {
			t.Fatalf("state %d has no name", s)
		}
		if got := BlockOf(s).Name(); got != name {
			t.Fatalf("BlockOf(%d) = %s, want %s", s, got, name)
		}
	}
	if _, _, ok := BlockStateName(StateCount); ok {
		t.Fatalf("state %d past StateCount has a name", StateCount)
	}
	if got := BlockOf(StateCount); got != BlockAir {
		t.Fatalf("BlockOf(StateCount) = %s, want air", got.Name())
	}
}

func TestBlockIDsAndDefaultStates(t *testing.T) {
	if len(BlockIDs) != BlockCount {
		t.Fatalf("BlockIDs has %d blocks, BlockCount is %d", len(BlockIDs), BlockCount)
	}
	for b := range Block(BlockCount) {
		name := b.Name()
		if id, ok := BlockIDs[name]; !ok || id != int32(b) {
			t.Fatalf("%s is Block %d, BlockIDs says %d (%v)", name, b, id, ok)
		}
		if got, ok := BlockByName(name); !ok || got != b {
			t.Fatalf("BlockByName(%s) = %d, %v", name, got, ok)
		}
		want, ok := FindState(name, nil)
		if !ok || b.DefaultState() != want {
			t.Fatalf("%s default state %d, FindState says %d (%v)", name, b.DefaultState(), want, ok)
		}
		if BlockOf(b.DefaultState()) != b {
			t.Fatalf("%s default state %d belongs to %s", name, b.DefaultState(), BlockOf(b.DefaultState()).Name())
		}
	}
	if _, ok := BlockByName("minecraft:diamond"); ok {
		t.Fatal("minecraft:diamond is a block")
	}
}

func TestItemsMatchItemIDs(t *testing.T) {
	if len(Items) != ItemCount {
		t.Fatalf("Items has %d entries, ItemCount is %d", len(Items), ItemCount)
	}
	for i := range Item(ItemCount) {
		id, ok := ItemID(i.Name())
		if !ok || id != int32(i) {
			t.Fatalf("%s is Item %d, ItemID says %d (%v)", i.Name(), i, id, ok)
		}
		if got, ok := ItemByName(i.Name()); !ok || got != i {
			t.Fatalf("ItemByName(%s) = %d, %v", i.Name(), got, ok)
		}
	}
	if _, ok := ItemByName("minecraft:water"); ok {
		t.Fatal("minecraft:water is an item")
	}
}

func TestProtocolEntityTypesMatchTheRegistry(t *testing.T) {
	for name, want := range map[string]int{
		"minecraft:mannequin":           protocol.EntityTypeMannequin,
		"minecraft:mangrove_chest_boat": protocol.EntityTypeMangroveChestBoat,
		"minecraft:item":                protocol.EntityTypeItem,
		"minecraft:player":              protocol.EntityTypePlayer,
		"minecraft:tnt":                 protocol.EntityTypeTnt,
	} {
		if got, ok := EntityTypeID(name); !ok || int(got) != want {
			t.Errorf("EntityTypeID(%q) = %d, %v; protocol constant %d", name, got, ok, want)
		}
	}
}
