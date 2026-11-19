package rules

import (
	"testing"

	"github.com/golem-mc/golem/nbt"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// A can_place_on or can_break component's raw wire bytes, as the client
// writes them: the list of predicates the caller spells, wrapped as the one
// component the stack carries. The gates decode the same bytes again, so a
// rule that matches here is the same bytes that match in play.
func adventureComponentStack(component int32, predicates func(*protocol.Buffer)) protocol.ItemStack {
	var value protocol.Buffer
	predicates(&value)
	return protocol.ItemStack{
		ItemID: 1,
		Count:  1,
		Raw:    []protocol.RawComponent{{Type: component, Data: value.Bytes()}},
	}
}

// predicate writes one block predicate's own fields, the body of the list
// entry the caller counted: the block set the caller spells (or none), one
// property matcher (or none), and whether the block-entity NBT guard is
// carried.
func predicate(b *protocol.Buffer, blocks func(*protocol.Buffer), prop func(*protocol.Buffer), guarded bool) {
	if blocks == nil {
		b.PutBool(false) // no block set: every kind passes that half
	} else {
		b.PutBool(true)
		blocks(b)
	}
	if prop == nil {
		b.PutBool(false) // no property matchers
	} else {
		b.PutBool(true)
		b.PutVarInt(1)
		prop(b)
	}
	b.PutBool(guarded)
	b.PutVarInt(0) // no exact component matchers
	b.PutVarInt(0) // no partial ones
}

// onePredicate is a predicate list of one: the list's count ahead of the
// single body.
func onePredicate(blocks func(*protocol.Buffer), prop func(*protocol.Buffer), guarded bool) func(*protocol.Buffer) {
	return func(b *protocol.Buffer) {
		b.PutVarInt(1)
		predicate(b, blocks, prop, guarded)
	}
}

// inlineBlocks writes a block set of inline block registry IDs.
func inlineBlocks(b *protocol.Buffer, names ...string) {
	b.PutVarInt(int32(len(names)) + 1) // one past the count
	for _, name := range names {
		id, ok := registry.BlockIDs[name]
		if !ok {
			panic("no block registry entry: " + name)
		}
		b.PutVarInt(id)
	}
}

// tagBlocks writes a block set naming a tag.
func tagBlocks(b *protocol.Buffer, tag string) {
	b.PutVarInt(0)
	b.PutString(tag)
}

// exactProp writes one property matcher of the exact form.
func exactProp(prop, value string) func(*protocol.Buffer) {
	return func(b *protocol.Buffer) {
		b.PutString(prop)
		b.PutBool(true)
		b.PutString(value)
	}
}

// rangedProp writes one property matcher of the ranged form, each bound
// present when its string is not empty.
func rangedProp(prop, min, max string) func(*protocol.Buffer) {
	return func(b *protocol.Buffer) {
		b.PutString(prop)
		b.PutBool(false)
		b.PutBool(min != "")
		if min != "" {
			b.PutString(min)
		}
		b.PutBool(max != "")
		if max != "" {
			b.PutString(max)
		}
	}
}

// TestAdventureComponentGatesMatch checks the two components' predicate
// matching: the block set inline or by tag, the exact and ranged property
// matchers, and the shapes that fit nothing.
// vanilla: BlockPredicate.matches, StatePropertiesPredicate.matches
func TestAdventureComponentGatesMatch(t *testing.T) {
	dirt := registry.BlockStates["minecraft:dirt"]
	stone := registry.BlockStates["minecraft:stone"]
	oakLog := registry.BlockStates["minecraft:oak_log"]
	grass := registry.BlockStates["minecraft:grass_block"]
	snowyGrass := mustState(t, "minecraft:grass_block", map[string]string{"snowy": "true"})
	wheat := func(age string) uint16 {
		return mustState(t, "minecraft:wheat", map[string]string{"age": age})
	}
	for _, tc := range []struct {
		name  string
		stack protocol.ItemStack
		state uint16
		want  bool
	}{
		{
			name: "the inline set names the block",
			stack: adventureComponentStack(protocol.DataComponentCanPlaceOn,
				onePredicate(func(b *protocol.Buffer) { inlineBlocks(b, "minecraft:dirt") }, nil, false)),
			state: dirt,
			want:  true,
		},
		{
			name: "the inline set names another block",
			stack: adventureComponentStack(protocol.DataComponentCanPlaceOn,
				onePredicate(func(b *protocol.Buffer) { inlineBlocks(b, "minecraft:dirt") }, nil, false)),
			state: stone,
			want:  false,
		},
		{
			name: "a set present with no block matches nothing",
			stack: adventureComponentStack(protocol.DataComponentCanPlaceOn,
				onePredicate(func(b *protocol.Buffer) { inlineBlocks(b) }, nil, false)),
			state: dirt,
			want:  false,
		},
		{
			name: "a set of several blocks",
			stack: adventureComponentStack(protocol.DataComponentCanPlaceOn,
				onePredicate(func(b *protocol.Buffer) { inlineBlocks(b, "minecraft:stone", "minecraft:dirt") }, nil, false)),
			state: dirt,
			want:  true,
		},
		{
			name:  "no block set passes every kind",
			stack: adventureComponentStack(protocol.DataComponentCanPlaceOn, onePredicate(nil, nil, false)),
			state: stone,
			want:  true,
		},
		{
			name: "the set's tag names a member",
			stack: adventureComponentStack(protocol.DataComponentCanPlaceOn,
				onePredicate(func(b *protocol.Buffer) { tagBlocks(b, "minecraft:logs") }, nil, false)),
			state: oakLog,
			want:  true,
		},
		{
			name: "the set's tag names no member",
			stack: adventureComponentStack(protocol.DataComponentCanPlaceOn,
				onePredicate(func(b *protocol.Buffer) { tagBlocks(b, "minecraft:logs") }, nil, false)),
			state: stone,
			want:  false,
		},
		{
			name: "an exact property the state carries",
			stack: adventureComponentStack(protocol.DataComponentCanPlaceOn,
				onePredicate(nil, exactProp("snowy", "true"), false)),
			state: snowyGrass,
			want:  true,
		},
		{
			name: "an exact property the state misses",
			stack: adventureComponentStack(protocol.DataComponentCanPlaceOn,
				onePredicate(nil, exactProp("snowy", "true"), false)),
			state: grass,
			want:  false,
		},
		{
			name: "a property the state lacks altogether",
			stack: adventureComponentStack(protocol.DataComponentCanPlaceOn,
				onePredicate(nil, exactProp("lit", "true"), false)),
			state: stone,
			want:  false,
		},
		{
			name: "a range the state's value sits inside",
			stack: adventureComponentStack(protocol.DataComponentCanPlaceOn,
				onePredicate(nil, rangedProp("age", "3", "7"), false)),
			state: wheat("5"),
			want:  true,
		},
		{
			name: "a range the state's value sits below",
			stack: adventureComponentStack(protocol.DataComponentCanPlaceOn,
				onePredicate(nil, rangedProp("age", "3", "7"), false)),
			state: wheat("1"),
			want:  false,
		},
		{
			name: "a range bounding only one end",
			stack: adventureComponentStack(protocol.DataComponentCanPlaceOn,
				onePredicate(nil, rangedProp("age", "", "7"), false)),
			state: wheat("7"),
			want:  true,
		},
		{
			name: "a bound no integer spells fits nothing",
			stack: adventureComponentStack(protocol.DataComponentCanPlaceOn,
				onePredicate(nil, rangedProp("age", "north", ""), false)),
			state: wheat("7"),
			want:  false,
		},
		{
			name:  "the block-entity NBT guard fits nothing",
			stack: adventureComponentStack(protocol.DataComponentCanPlaceOn, onePredicate(nil, nil, true)),
			state: dirt,
			want:  false,
		},
		{
			name: "one predicate not fitting and another fitting",
			stack: adventureComponentStack(protocol.DataComponentCanPlaceOn, func(b *protocol.Buffer) {
				b.PutVarInt(2) // the list's count
				predicate(b, func(b *protocol.Buffer) { inlineBlocks(b, "minecraft:stone") }, nil, false)
				predicate(b, func(b *protocol.Buffer) { inlineBlocks(b, "minecraft:dirt") }, nil, false)
			}),
			state: dirt,
			want:  true,
		},
		{
			name: "a predicate list with nothing in it fits nothing",
			stack: adventureComponentStack(protocol.DataComponentCanPlaceOn, func(b *protocol.Buffer) {
				b.PutVarInt(0)
			}),
			state: dirt,
			want:  false,
		},
		{
			name:  "a stack with no component at all",
			stack: protocol.ItemStack{ItemID: 1, Count: 1},
			state: dirt,
			want:  false,
		},
	} {
		if got := CanPlaceOnInAdventureMode(tc.stack, AdventureTarget{State: tc.state}); got != tc.want {
			t.Errorf("%s: CanPlaceOnInAdventureMode = %t, want %t", tc.name, got, tc.want)
		}
	}
}

// TestAdventurePredicateVoicesMatchVanilla pins the three matcher quirks the
// component decoder leaves to the match: a tag named without a namespace is
// qualified, an exact number is read as its number so "07" is 7, and a range
// bound the property does not accept fits nothing. It also pins the boolean
// and enum ranges, which compare by the property's own order.
// vanilla: StatePropertiesPredicate.matches, IntegerProperty.getValue
func TestAdventurePredicateVoicesMatchVanilla(t *testing.T) {
	oakLog := registry.BlockStates["minecraft:oak_log"]
	wheat7 := mustState(t, "minecraft:wheat", map[string]string{"age": "7"})
	wheat5 := mustState(t, "minecraft:wheat", map[string]string{"age": "5"})
	grass := registry.BlockStates["minecraft:grass_block"]
	doorLower := mustState(t, "minecraft:oak_door", map[string]string{"half": "lower"})
	doorUpper := mustState(t, "minecraft:oak_door", map[string]string{"half": "upper"})
	anvilNorth := mustState(t, "minecraft:anvil", map[string]string{"facing": "north"})
	anvilWest := mustState(t, "minecraft:anvil", map[string]string{"facing": "west"})
	for _, tc := range []struct {
		name  string
		stack protocol.ItemStack
		state uint16
		want  bool
	}{
		{
			name: "a tag without a namespace names its members",
			stack: adventureComponentStack(protocol.DataComponentCanPlaceOn,
				onePredicate(func(b *protocol.Buffer) { tagBlocks(b, "logs") }, nil, false)),
			state: oakLog,
			want:  true,
		},
		{
			name: "an exact number is read as its number",
			stack: adventureComponentStack(protocol.DataComponentCanPlaceOn,
				onePredicate(nil, exactProp("age", "07"), false)),
			state: wheat7,
			want:  true,
		},
		{
			name: "a bound past the property's range fits nothing",
			stack: adventureComponentStack(protocol.DataComponentCanPlaceOn,
				onePredicate(nil, rangedProp("age", "", "10"), false)),
			state: wheat5,
			want:  false,
		},
		{
			name: "a boolean range the value sits inside",
			stack: adventureComponentStack(protocol.DataComponentCanPlaceOn,
				onePredicate(nil, rangedProp("snowy", "false", "false"), false)),
			state: grass,
			want:  true,
		},
		{
			name: "a boolean range the value sits above",
			stack: adventureComponentStack(protocol.DataComponentCanPlaceOn,
				onePredicate(nil, rangedProp("snowy", "true", ""), false)),
			state: grass,
			want:  false,
		},
		{
			name: "an enum range naming the value",
			stack: adventureComponentStack(protocol.DataComponentCanPlaceOn,
				onePredicate(nil, rangedProp("half", "lower", "lower"), false)),
			state: doorLower,
			want:  true,
		},
		{
			name: "an enum range missing the value",
			stack: adventureComponentStack(protocol.DataComponentCanPlaceOn,
				onePredicate(nil, rangedProp("half", "lower", "lower"), false)),
			state: doorUpper,
			want:  false,
		},
		{
			name: "an enum range spanning its values",
			stack: adventureComponentStack(protocol.DataComponentCanPlaceOn,
				onePredicate(nil, rangedProp("facing", "north", "south"), false)),
			state: anvilNorth,
			want:  true,
		},
		{
			name: "an enum range past its values",
			stack: adventureComponentStack(protocol.DataComponentCanPlaceOn,
				onePredicate(nil, rangedProp("facing", "north", "south"), false)),
			state: anvilWest,
			want:  false,
		},
	} {
		if got := CanPlaceOnInAdventureMode(tc.stack, AdventureTarget{State: tc.state}); got != tc.want {
			t.Errorf("%s: CanPlaceOnInAdventureMode = %t, want %t", tc.name, got, tc.want)
		}
	}
}

// TestBlockActionRestricted checks the dig gate over every mode: the modes
// that build dig, spectator touches nothing, and adventure reaches as far
// as the main hand's can_break.
// vanilla: Player.blockActionRestricted
func TestBlockActionRestricted(t *testing.T) {
	leaves := mustState(t, "minecraft:oak_leaves", map[string]string{"persistent": "true"})
	stone := registry.BlockStates["minecraft:stone"]
	breaksLeaves := adventureComponentStack(protocol.DataComponentCanBreak,
		onePredicate(func(b *protocol.Buffer) { inlineBlocks(b, "minecraft:oak_leaves") }, nil, false))
	plainStack := protocol.ItemStack{ItemID: 1, Count: 1}
	for _, tc := range []struct {
		mode  int32
		hand  protocol.ItemStack
		state uint16
		want  bool
	}{
		{protocol.GameModeSurvival, protocol.ItemStack{}, stone, false},
		{protocol.GameModeCreative, protocol.ItemStack{}, stone, false},
		{protocol.GameModeSpectator, breaksLeaves, leaves, true},
		{protocol.GameModeAdventure, protocol.ItemStack{}, leaves, true},
		{protocol.GameModeAdventure, plainStack, leaves, true},
		{protocol.GameModeAdventure, breaksLeaves, stone, true},
		{protocol.GameModeAdventure, breaksLeaves, leaves, false},
	} {
		if got := BlockActionRestricted(tc.mode, tc.hand, AdventureTarget{State: tc.state}); got != tc.want {
			t.Errorf("BlockActionRestricted(mode %d) = %t, want %t", tc.mode, got, tc.want)
		}
	}
	// The gates answer their own component alone: a can_break set names what
	// a dig may reach, not what a build may stand on.
	if CanPlaceOnInAdventureMode(breaksLeaves, AdventureTarget{State: stone}) {
		t.Error("CanPlaceOnInAdventureMode read a can_break set's blocks")
	}
	if CanBreakInAdventureMode(plainStack, AdventureTarget{State: stone}) {
		t.Error("CanBreakInAdventureMode gave a plain stack a block")
	}
}

// vanilla: BlockPredicate.matches, NbtPredicate.matches
func TestAdventurePredicateReadsNBTLazily(t *testing.T) {
	t.Parallel()
	state := registry.BlockBeehive.DefaultState()
	wrongID := protocol.BlockPredicate{NBT: nbt.Compound{"id": nbt.String("minecraft:chest")}}
	rightID := protocol.BlockPredicate{NBT: nbt.Compound{"id": nbt.String("minecraft:beehive")}}
	wrongBlock := rightID
	wrongBlock.Blocks = protocol.BlockPredicateSet{Present: true, Inline: []int32{int32(registry.BlockStone)}}
	wrongProp := rightID
	wrongProp.Props = []protocol.BlockPredicateProp{{Name: "honey_level", Value: "5"}}
	for _, tc := range []struct {
		name       string
		predicates []protocol.BlockPredicate
		tag        nbt.Compound
		want       bool
		reads      int
	}{
		{"state only", []protocol.BlockPredicate{{}}, nil, true, 0},
		{"wrong block", []protocol.BlockPredicate{wrongBlock}, nil, false, 0},
		{"wrong property", []protocol.BlockPredicate{wrongProp}, nil, false, 0},
		{"one snapshot", []protocol.BlockPredicate{wrongID, rightID}, nbt.Compound{"id": nbt.String("minecraft:beehive")}, true, 1},
		{"absent entity cached", []protocol.BlockPredicate{wrongID, rightID}, nil, false, 1},
		{"empty guard requires entity", []protocol.BlockPredicate{{NBT: nbt.Compound{}}}, nil, false, 1},
		{"later state-only match", []protocol.BlockPredicate{wrongID, {}}, nil, true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reads := 0
			target := AdventureTarget{State: state, ReadNBT: func() nbt.Compound {
				reads++
				return tc.tag
			}}
			if got := predicatesMatch(tc.predicates, target); got != tc.want {
				t.Errorf("match = %t, want %t", got, tc.want)
			}
			if reads != tc.reads {
				t.Errorf("NBT reads = %d, want %d", reads, tc.reads)
			}
		})
	}
	if predicatesMatch([]protocol.BlockPredicate{rightID}, AdventureTarget{State: state}) {
		t.Error("NBT guard matched without a reader")
	}
}
