package rules_test

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server/datapack"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/nbt"
)

func weightedProvider(weights ...int32) nbt.Tag {
	var list []nbt.Tag
	for i, w := range weights {
		list = append(list, nbt.Compound{
			"data":   nbt.Int(i + 1),
			"weight": nbt.Int(w),
		})
	}
	return nbt.Compound{"type": nbt.String("minecraft:weighted_list"), "distribution": nbt.ListOf(list)}
}

func TestWeightedProviderTotalWeight(t *testing.T) {
	const max = 1<<31 - 1
	if _, msg := datapack.DecodeIntProvider(weightedProvider(max, 1)); !strings.Contains(msg, "Sum of weights must be <= 2147483647") {
		t.Errorf("sum above max: msg = %q", msg)
	}
	p, msg := datapack.DecodeIntProvider(weightedProvider(max-1, 1))
	if msg != "" {
		t.Fatalf("sum at max: %s", msg)
	}
	v, err := rules.EvalIntProvider(p, &rules.LootContext{}, rand.New(rand.NewSource(1)))
	if err != nil || (v != 1 && v != 2) {
		t.Errorf("eval = %d, %v", v, err)
	}
}
