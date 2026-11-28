// Package command owns the Brigadier-style command tree (the node type the
// Commands packet is built from, the parse and completion walks) and, one
// file per command, the commands. Handlers run against the Source and Target
// interfaces the server satisfies, so this package sends nothing.
package command

import (
	"github.com/golem-mc/golem/internal/protocol"
)

// Permission levels, as the vanilla server numbers them: "gamemasters" in the
// command report is level 2, "admins" is level 3. A player's own level comes
// from ops.json; a missing file means no operators, and op-permission-level
// is only the level /op grants.
const (
	// PermLevelModerators is level 1, the least an operator holds: what
	// Bukkit's "op" permission default means for the non-vanilla commands.
	PermLevelModerators  = 1
	PermLevelGamemasters = 2
	PermLevelAdmins      = 3
	// PermLevelOwners is level 4, the level the commands that end the server
	// run at and the level the console runs everything at.
	PermLevelOwners = 4
)

// NodeType is the type of a command tree node, as the Commands packet encodes
// it in the flags byte's low two bits.
type NodeType uint8

const (
	NodeRoot     NodeType = 0
	NodeLiteral  NodeType = 1
	NodeArgument NodeType = 2
)

// Flags for a command tree node, packed into the byte the Commands packet
// sends before a node's children and name.
const (
	flagExecutable  = 0x04
	flagHasRedirect = 0x08
	flagHasSuggests = 0x10
	flagRestricted  = 0x20
)

// Parser IDs from the minecraft:command_argument_type registry, which is
// built into the client. The IDs move between versions; see tools/README.md.
const (
	// ParserBool is brigadier:bool, which reads its word case-sensitively.
	ParserBool  = 0
	ParserFloat = 1
	// ParserDouble is brigadier:double.
	ParserDouble      = 2
	ParserInteger     = 3
	ParserString      = 5
	ParserEntity      = 6
	ParserGameProfile = 7
	ParserBlockPos    = 8
	ParserVec3        = 10
	// ParserVec2 is minecraft:vec2, the x and z of /spreadplayers' centre.
	ParserVec2 = 11
	// ParserColumnPos is minecraft:column_pos, the x and z of a block column.
	ParserColumnPos  = 9
	ParserBlockState = 12
	// ParserBlockPredicate is minecraft:block_predicate, the parser a
	// /fill's filter reads.
	ParserBlockPredicate = 13
	ParserItemStack      = 14
	ParserItemPred       = 15
	ParserMessage        = 20
	// ParserComponent is minecraft:component, the parser a text component
	// argument reads.
	ParserComponent = 18
	// ParserStyle is minecraft:style, a text component's style fields in SNBT.
	ParserStyle = 19
	// ParserNbtPath is minecraft:nbt_path, the parser an /execute store or
	// data conditional reads.
	ParserNbtPath   = 23
	ParserObjective = 24
	// ParserObjectiveCriteria is minecraft:objective_criteria.
	ParserObjectiveCriteria = 25
	// ParserOperation is minecraft:operation, a scoreboard operator.
	ParserOperation = 26
	// ParserParticle is minecraft:particle, a particle type and its data.
	ParserParticle = 27
	ParserRotation = 29
	// ParserScoreboardSlot is minecraft:scoreboard_slot.
	ParserScoreboardSlot = 30
	// ParserScoreHolder is minecraft:score_holder, the parser of a
	// scoreboard score's holder.
	ParserScoreHolder = 31
	// ParserSwizzle is minecraft:swizzle, the parser of the axes string an
	// /execute align reads.
	ParserSwizzle = 32
	// ParserTeam is minecraft:team.
	ParserTeam = 33
	// ParserItemSlot is minecraft:item_slot.
	ParserItemSlot    = 34
	ParserItemSlots   = 35
	ParserResourceLoc = 36
	// ParserFunction is minecraft:function, whose registry this server keeps none of.
	ParserFunction   = 37
	ParserEntityAnch = 38
	// ParserIntRange is minecraft:int_range, the parser of the range a
	// scoreboard conditional matches a score against.
	ParserIntRange = 39
	// ParserFloatRange is minecraft:float_range, the parser of the range a
	// stopwatch conditional matches a reading against.
	ParserFloatRange = 40
	// ParserDimension is minecraft:dimension.
	ParserDimension = 41
	ParserGamemode  = 42
	ParserTime      = 43
	// ParserResourceOrTag is minecraft:resource_or_tag, the parser the biome
	// conditional reads.
	ParserResourceOrTag = 44
	// ParserResourceOrTagKey is minecraft:resource_or_tag_key, the parser of
	// the registry key or tag a /locate structure names.
	ParserResourceOrTagKey = 45
	ParserResource         = 46
	// ParserResourceKey is minecraft:resource_key, an identifier whose
	// registry the client does not hold a copy of, such as the recipes.
	ParserResourceKey = 47
	// ParserResourceSelector is minecraft:resource_selector, a wildcard
	// pattern over a registry's ids, with the registry as its property.
	ParserResourceSelector = 48
	// ParserTemplateMirror is minecraft:template_mirror.
	ParserTemplateMirror = 49
	// ParserTemplateRotation is minecraft:template_rotation.
	ParserTemplateRotation = 50
	// ParserHeightmap is minecraft:heightmap, the parser a positioned over reads.
	ParserHeightmap = 51
	// ParserLootTable is minecraft:loot_table, the parser of the table a /loot rolls.
	ParserLootTable = 52
	// ParserLootPredicate is minecraft:loot_predicate, whose registry this
	// server keeps none of.
	ParserLootPredicate = 53
	// ParserLootModifier is minecraft:loot_modifier, the parser of the item
	// modifier a /item names, a data pack's or an inline one.
	ParserLootModifier = 54
	// ParserContextFloatProvider and ParserContextIntProvider are the
	// parsers of the number provider a /compute evaluates.
	ParserContextFloatProvider = 55
	ParserContextIntProvider   = 56
	// ParserSlotSource is minecraft:slot_source, the parser of the slots an
	// /item names.
	ParserSlotSource = 57
	// ParserSwingAnimation is minecraft:swing_animation, a swing animation
	// type's name.
	ParserSwingAnimation = 60
	// ParserUUID is minecraft:uuid.
	ParserUUID = 61
	// ParserDialog is minecraft:dialog, a dialog registry entry or an inline
	// dialog compound.
	ParserDialog = 58
	// ParserFeature is minecraft:feature, a worldgen feature's name or an
	// inline feature.
	ParserFeature = 59
	// ParserNbtCompound is minecraft:nbt_compound_tag, the parser of the
	// compound tag an argument may carry whole.
	ParserNbtCompound = 21
	// ParserNbtTag is minecraft:nbt_tag, the parser of the one tag — of
	// any shape — a data modify's value argument carries.
	ParserNbtTag = 22
)

// AskServerSuggestion is the suggestion type that tells the client to send a
// Command Suggestions Request packet rather than completing locally.
const AskServerSuggestion = "minecraft:ask_server"

// Node is a node in the command tree: the root has no name, literal nodes
// carry the word they match, and argument nodes carry the argument name and
// a parser. The tree is built per player, filtered down to the commands their
// permission level allows.
type Node struct {
	// Handler and Permission belong to server-owned commands.
	Handler    func(Source, string, map[string]ArgumentValue) (int32, error)
	Permission string
	// Plugin marks a plugin command's root: it runs from its own Handler,
	// never from the registry command of the same word. Override says it
	// replaces the vanilla root of that name, which Extend then moves to
	// minecraft:<name>; Dispatch is the registry command such a moved root
	// runs.
	Plugin, Override bool
	Dispatch         string
	Type             NodeType
	Name             string
	Executable       bool
	Children         []*Node
	// literals indexes the literal children by name for FindLiteralChild;
	// nil on a node whose children are still being built.
	literals map[string]*Node

	// Required is the permission level a player needs for this node, and
	// flagRestricted announces it on the wire. A command at one level gates
	// its root literal, as vanilla's .requires does; one whose paths differ
	// (bare /random for everyone, sequences and reset for operators) gates
	// the higher nodes. ForLevel drops an unmet node and everything under it.
	Required int

	// Redirect is the node whose children and executability stand in for
	// this one's: an alias (/tp for /teleport, /w and /tell for /msg).
	// RedirectName is its build-time form, resolved once the tree is built;
	// RedirectRoot hands the rest of the input back to the root, as
	// /execute's run does.
	Redirect     *Node
	RedirectName string
	RedirectRoot bool

	// Argument-node fields. Parser is the minecraft:command_argument_type
	// ID and Properties writes the parser properties after it; Suggests is
	// the suggestion type sent with flagHasSuggests, and Completions fills
	// the server's answer (matches, and the offset they replace from, zero
	// for the whole word). Greedy parsers read every remaining word;
	// Quotable marks brigadier:string phrases quotes may span. EntityArg,
	// Resource, Int (its bounds doubling as wire properties) and Nbt are the
	// parse-time checks vanilla runs, so a misfit stops the command in the
	// walk. Check refuses a word that must name something the argument knows
	// (a dimension, an anchor) with vanilla's error; Nbt and NbtValue check
	// the SNBT arguments' words.
	Check       func(word string) *ParseError
	Parser      int32
	Properties  func(*protocol.Buffer)
	Greedy      bool
	Quotable    bool
	EntityArg   *EntityArg
	Selector    *SelectorArg
	Resource    *ResourceArg
	Int         *IntArg
	Float       *FloatArg
	Nbt         bool
	NbtValue    bool
	Suggests    string
	Completions func(ctx any, partial string) (int, []string)
	// InputCompletions receives the input, partial and parsed preceding arguments.
	InputCompletions func(Source, string, string, map[string]ArgumentValue) (int, []string)
	// IntegerMatches names which of the matches Completions gives for a
	// partial are integer suggestions, which sort by value instead of text.
	// vanilla: IntegerSuggestion.compareToIgnoreCase
	IntegerMatches func(partial string) []string
}

// SelectorArg is the shape of the score-holder argument's selector reading:
// the word spans like an entity argument's and a single-holder one refuses a
// selector that names more than one, but the argument is no entity argument
// on the wire, which is why the shape lives beside EntityArg rather than in it.
type SelectorArg struct {
	Single bool
}

// IntArg is the range an integer argument accepts: the walk holds the
// number it reads to it, and the Commands packet's parser properties are
// written from it, so the bounds are declared here and nowhere else.
type IntArg struct {
	Min, Max       int32
	HasMin, HasMax bool
}

// FloatArg is the range a float argument accepts, declared and written out
// the way an IntArg's bounds are.
type FloatArg struct {
	Min, Max       float32
	HasMin, HasMax bool
}

// ResourceArg says which registry a resource argument reads from.
type ResourceArg struct {
	Registry string
	// Taggable marks the resource-or-tag shape of the same argument, whose
	// word may carry the # of a tag in front of it.
	Taggable bool
}

// FindLiteralChild returns the child of node with the given literal name, or
// nil if there is none.
func FindLiteralChild(node *Node, name string) *Node {
	if node.literals != nil {
		return node.literals[name]
	}
	for _, child := range node.Children {
		if child.Type == NodeLiteral && child.Name == name {
			return child
		}
	}
	return nil
}

// indexLiterals fills root's literal index once its children are final and
// the tree is shared read-only. The first of two same-named children wins,
// as in FindLiteralChild's loop.
func indexLiterals(root *Node) {
	root.literals = make(map[string]*Node, len(root.Children))
	for _, child := range root.Children {
		if child.Type != NodeLiteral {
			continue
		}
		if _, dup := root.literals[child.Name]; !dup {
			root.literals[child.Name] = child
		}
	}
}
