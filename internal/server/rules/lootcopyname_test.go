package rules

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// TestCopyNameSources checks copy_name copies the custom name of the block
// entity and of each entity source the context holds.
// vanilla: CopyNameFunction.run
func TestCopyNameSources(t *testing.T) {
	t.Parallel()
	name := func(s string) *protocol.Text { return &protocol.Text{Text: s} }
	ctx := &LootContext{
		HasBlock:       true,
		CustomName:     name("chest"),
		This:           LootEntity{Type: "minecraft:cow", Name: name("bessie")},
		Attacker:       LootEntity{Type: "minecraft:player", Name: name("alex")},
		DirectAttacker: LootEntity{Type: "minecraft:arrow", Name: name("shot")},
		Target:         LootEntity{Type: "minecraft:allay", Name: name("tapped")},
		Interacting:    LootEntity{Type: "minecraft:player", Name: name("steve")},
	}
	for _, c := range []struct{ source, want string }{
		{"block_entity", "chest"},
		{"this", "bessie"},
		{"attacker", "alex"},
		{"attacking_player", "alex"},
		{"direct_attacker", "shot"},
		{"target_entity", "tapped"},
		{"interacting_entity", "steve"},
	} {
		got := ApplyItemModifierIn(registry.LootCopyName{Source: c.source}, protocol.ItemStack{Count: 1}, rand.New(rand.NewSource(1)), nil, ctx)
		switch {
		case c.want == "" && got.CustomName != nil:
			t.Errorf("%s: copied %q from a source the context lacks", c.source, got.CustomName.Text)
		case c.want != "" && (got.CustomName == nil || got.CustomName.Text != c.want):
			t.Errorf("%s: custom name = %v, want %q", c.source, got.CustomName, c.want)
		}
	}
}
