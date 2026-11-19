package commands

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"slices"
	"sort"
	"strings"
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

// TestFillFilterSuggestsTagsOnlyWithHash checks that the replace filter's tag
// pool is reached only through the # a tag word carries: a plain partial gets
// blocks and no tags, a # partial gets the tags it matches, and the empty
// partial offers both pools.
func TestFillFilterSuggestsTagsOnlyWithHash(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	c.CommandSuggestion(1, "/fill ~ ~ ~ ~ ~ ~ stone replace st")
	_, _, _, matches := c.ExpectCommandSuggestions()
	for _, m := range matches {
		if strings.HasPrefix(m, "#") {
			t.Errorf("plain partial st offered the tag %q", m)
		}
	}
	if !slices.Contains(matches, "minecraft:stone") {
		t.Errorf("plain partial st did not offer minecraft:stone: %v", matches)
	}

	// The # is stripped before the match, so #wool_ reaches the tags.
	c.CommandSuggestion(2, "/fill ~ ~ ~ ~ ~ ~ stone replace #wool_")
	_, _, _, matches = c.ExpectCommandSuggestions()
	want := []string{"#minecraft:wool_carpets", "#minecraft:wool_slabs", "#minecraft:wool_stairs"}
	if !slices.Equal(matches, want) {
		t.Errorf("#wool_ = %v, want %v", matches, want)
	}

	c.CommandSuggestion(3, "/fill ~ ~ ~ ~ ~ ~ stone replace ")
	_, _, _, matches = c.ExpectCommandSuggestions()
	if !slices.Contains(matches, "minecraft:stone") || !slices.Contains(matches, "#minecraft:wool") {
		t.Errorf("empty partial missing a block or a tag: %d matches", len(matches))
	}

	c.Disconnect()
}

// TestFillSuggestsEveryBlock checks the same for /fill's block argument and
// for the replace filter, whose predicate may name any block or, with its #
// in front, any block tag.
func TestFillSuggestsEveryBlock(t *testing.T) {
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
		{"the block argument",
			"/fill ~ ~ ~ ~ ~ ~ red_w", 18, 5,
			[]string{"minecraft:red_wall_banner", "minecraft:red_wool", "minecraft:red_wool_slab", "minecraft:red_wool_stairs"}},
		{"the replace filter",
			"/fill ~ ~ ~ ~ ~ ~ stone replace red_w", 32, 5,
			[]string{"minecraft:red_wall_banner", "minecraft:red_wool", "minecraft:red_wool_slab", "minecraft:red_wool_stairs"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c.CommandSuggestion(1, tc.text)
			_, start, length, matches := c.ExpectCommandSuggestions()
			if start != tc.start || length != tc.length {
				t.Errorf("range = (%d, %d), want (%d, %d)", start, length, tc.start, tc.length)
			}
			if !slices.Equal(matches, tc.matches) {
				t.Errorf("matches = %v, want %v", matches, tc.matches)
			}
		})
	}

	c.Disconnect()
}

// TestBlockSuggestionsStopAtACompleteID checks that a whole block id stops
// the name list: `[` for a block with properties, `{` for one with block
// entity data, both where it has both, nothing otherwise, and an unmatched
// partial answers the empty 0/0 range.
func TestBlockSuggestionsStopAtACompleteID(t *testing.T) {
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
		{"glass has no properties", "/setblock ~ ~ ~ glass", 0, 0, nil},
		{"namespaced glass", "/setblock ~ ~ ~ minecraft:glass", 0, 0, nil},
		{"stone", "/setblock ~ ~ ~ stone", 0, 0, nil},
		{"stairs have properties", "/setblock ~ ~ ~ minecraft:oak_stairs", 36, 0, []string{"["}},
		{"a chest has both", "/setblock ~ ~ ~ chest", 21, 0, []string{"[", "{"}},
		{"a predicate stops too", "/fill ~ ~ ~ ~ ~ ~ stone replace glass", 0, 0, nil},
		{"a predicate id with properties", "/fill ~ ~ ~ ~ ~ ~ stone replace oak_stairs", 42, 0, []string{"["}},
		{"a complete tag stops at its openings", "/fill ~ ~ ~ ~ ~ ~ stone replace #wool", 0, 0, nil},
		{"a tag with properties", "/fill ~ ~ ~ ~ ~ ~ stone replace #minecraft:stairs", 49, 0, []string{"["}},
		{"nothing matches", "/setblock ~ ~ ~ zzz", 0, 0, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c.CommandSuggestion(1, tc.text)
			_, start, length, matches := c.ExpectCommandSuggestions()
			if start != tc.start || length != tc.length {
				t.Errorf("range = (%d, %d), want (%d, %d)", start, length, tc.start, tc.length)
			}
			if !slices.Equal(matches, tc.matches) {
				t.Errorf("matches = %v, want %v", matches, tc.matches)
			}
		})
	}
	c.Disconnect()
}
