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
