package commands

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"slices"
	"sort"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/registry"
)

// TestSetblockSuggestsEveryBlock checks that a block argument offers every
// block in the registry, sorted and matched the way a resource is: blocks
// without properties - air, stone_bricks, red_wool - are among them, and a
// partial that spans both kinds keeps the vanilla order.
func TestSetblockSuggestsEveryBlock(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	for _, tc := range []struct {
		name    string
		text    string
		start   int32
		length  int32
		matches []string
	}{
		{"a block with and without properties, in order",
			"/setblock ~ ~ ~ red_w", 16, 5,
			[]string{"minecraft:red_wall_banner", "minecraft:red_wool", "minecraft:red_wool_slab", "minecraft:red_wool_stairs"}},
		{"a namespaced partial across a propertyless block",
			"/setblock ~ ~ ~ minecraft:stone_b", 16, 17,
			[]string{"minecraft:stone_brick_slab", "minecraft:stone_brick_stairs",
				"minecraft:stone_brick_wall", "minecraft:stone_bricks", "minecraft:stone_button"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c.CommandSuggestion(1, tc.text)
			id, start, length, matches := c.ExpectCommandSuggestions()
			if id != 1 {
				t.Errorf("transaction ID = %d, want 1", id)
			}
			if start != tc.start || length != tc.length {
				t.Errorf("range = (%d, %d), want (%d, %d)", start, length, tc.start, tc.length)
			}
			if !slices.Equal(matches, tc.matches) {
				t.Errorf("matches = %v, want %v", matches, tc.matches)
			}
		})
	}

	// The empty prefix offers the whole block registry, sorted.
	c.CommandSuggestion(2, "/setblock ~ ~ ~ ")
	_, _, _, matches := c.ExpectCommandSuggestions()
	want := make([]string, 0, len(registry.BlockTypes))
	for name := range registry.BlockTypes {
		want = append(want, name)
	}
	sort.Strings(want)
	if !slices.Equal(matches, want) {
		t.Errorf("bare /setblock offered %d blocks, want all %d", len(matches), len(want))
	}

	c.Disconnect()
}
