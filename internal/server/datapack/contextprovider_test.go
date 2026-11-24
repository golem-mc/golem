package datapack

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func weightedProvider(w1, w2 int32) nbt.Tag {
	entry := func(v, w int32) nbt.Tag {
		return nbt.Compound{"data": nbt.Int(v), "weight": nbt.Int(w)}
	}
	return nbt.Compound{
		"type":         nbt.String("minecraft:weighted_list"),
		"distribution": nbt.NewList([]nbt.Tag{entry(1, w1), entry(2, w2)}),
	}
}

func TestWeightedProviderTotalWeight(t *testing.T) {
	if _, msg := DecodeIntProvider(weightedProvider(2147483647, 1)); msg == "" {
		t.Error("total 2147483648 accepted")
	}
	p, msg := DecodeIntProvider(weightedProvider(2147483646, 1))
	if msg != "" {
		t.Fatalf("total 2147483647 refused: %s", msg)
	}
	if _, ok := p.(registry.LootWeightedList); !ok {
		t.Errorf("got %T", p)
	}
}

func TestProviderListRefusesLiteralEmptyKeyCompound(t *testing.T) {
	decode := func(raw string) string {
		tag, err := JSONTag([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		_, msg := DecodeIntProvider(tag)
		return msg
	}
	if msg := decode(`{"type":"minecraft:add","inputs":[{"":7}]}`); msg == "" {
		t.Error("lone {\"\":7} input decoded as a provider")
	}
	if msg := decode(`{"type":"minecraft:add","inputs":[{"":7},{"type":"minecraft:constant","value":1}]}`); msg == "" {
		t.Error("{\"\":7} input beside a provider decoded as a provider")
	}
	if msg := decode(`{"type":"minecraft:add","inputs":[7,{"type":"minecraft:constant","value":1}]}`); msg != "" {
		t.Errorf("mixed list refused: %s", msg)

	}
}
