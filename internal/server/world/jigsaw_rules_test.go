package world

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/golem-mc/golem/registry"
)

func rulesFor(t *testing.T, rules ...string) ruleProcessor {
	t.Helper()
	raw := `{"processors":[{"processor_type":"minecraft:rule","rules":[` + strings.Join(rules, ",") + `]}]}`
	ps, _, err := loadStructureProcessors(json.RawMessage(raw), nil)
	if err != nil || len(ps) != 1 {
		t.Fatalf("processors: %v, %d", err, len(ps))
	}
	return ps[0].(ruleProcessor)
}

func blockState(t *testing.T, name string) uint16 {
	t.Helper()
	s, ok := registry.FindState(name, nil)
	if !ok {
		t.Fatalf("no block %s", name)
	}
	return s
}

// TestRuleProcessorProbability checks the random_block_match input predicate:
// probability 1 always replaces the block and 0 never does, and another block
// is never touched.
// vanilla: RandomBlockMatchTest.test
func TestRuleProcessorProbability(t *testing.T) {
	t.Parallel()
	const rule = `{"input_predicate":{"block":"minecraft:gold_block","predicate_type":"minecraft:random_block_match","probability":%s},` +
		`"location_predicate":{"predicate_type":"minecraft:always_true"},"output_state":{"Name":"minecraft:cracked_polished_blackstone_bricks"}}`
	gold, cracked, stone := blockState(t, "minecraft:gold_block"), blockState(t, "minecraft:cracked_polished_blackstone_bricks"), blockState(t, "minecraft:stone")
	always, never := rulesFor(t, fmt.Sprintf(rule, "1.0")), rulesFor(t, fmt.Sprintf(rule, "0.0"))
	for x := range int32(40) {
		if got := always.process(gold, 0, x, 50, -x, 0); got != cracked {
			t.Fatalf("probability 1 at %d left %d", x, got)
		}
		if got := never.process(gold, 0, x, 50, -x, 0); got != gold {
			t.Fatalf("probability 0 at %d gave %d", x, got)
		}
		if got := always.process(stone, 0, x, 50, -x, 0); got != stone {
			t.Fatalf("a block the rule does not name changed at %d", x)
		}
	}
}

// TestRuleProcessorPositionChance checks the axis_aligned_linear_pos predicate
// of the high rampart: from max_dist above the reference height up, the chance
// is max_chance.
// vanilla: AxisAlignedLinearPosTest.test
func TestRuleProcessorPositionChance(t *testing.T) {
	t.Parallel()
	const rule = `{"input_predicate":{"predicate_type":"minecraft:always_true"},"location_predicate":{"predicate_type":"minecraft:always_true"},` +
		`"output_state":{"Name":"minecraft:air"},"position_predicate":{"max_chance":%s,"max_dist":100,"predicate_type":"minecraft:axis_aligned_linear_pos"}}`
	stone, air := blockState(t, "minecraft:stone"), blockState(t, "minecraft:air")
	sure, none := rulesFor(t, fmt.Sprintf(rule, "1.0")), rulesFor(t, fmt.Sprintf(rule, "0.0"))
	for x := range int32(30) {
		if got := sure.process(stone, 0, x, 150, 7, 50); got != air {
			t.Fatalf("100 above the reference with chance 1 kept the block at %d", x)
		}
		if got := sure.process(stone, 0, x, 250, 7, 50); got != air {
			t.Fatalf("200 above the reference with chance 1 kept the block at %d", x)
		}
		if got := none.process(stone, 0, x, 150, 7, 50); got != stone {
			t.Fatalf("chance 0 replaced the block at %d", x)
		}
	}
}

func TestCappedProcessorLimits(t *testing.T) {
	for _, raw := range []string{`3`, `{"type":"minecraft:constant","value":3}`, `{"type":"minecraft:uniform","min_inclusive":1,"max_inclusive":3}`} {
		if _, _, _, err := cappedLimit(json.RawMessage(raw)); err != nil {
			t.Fatal(err)
		}
	}
	for _, raw := range []string{`0`, `-1`, `{"type":"minecraft:uniform","min_inclusive":4,"max_inclusive":3}`, `{"type":"minecraft:biased_to_bottom","min_inclusive":1,"max_inclusive":3}`} {
		if _, _, _, err := cappedLimit(json.RawMessage(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}
