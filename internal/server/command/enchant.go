package command

import (
	"strconv"
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// Enchant is the /enchant command.
var Enchant = Command{
	Name:   "enchant",
	Level:  PermLevelGamemasters,
	Build:  enchantNode,
	Run:    discardResult(runEnchant),
	Result: runEnchant,
}

// enchantmentRegistry is the registry the enchantment argument is parsed
// against, the one ResourceArgument.resource names.
const enchantmentRegistry = "minecraft:enchantment"

// The translation keys /enchant answers with.
const (
	enchantFailedKey             = "commands.enchant.failed"
	enchantFailedEntityKey       = "commands.enchant.failed.entity"
	enchantFailedItemlessKey     = "commands.enchant.failed.itemless"
	enchantFailedIncompatibleKey = "commands.enchant.failed.incompatible"
	enchantFailedLevelKey        = "commands.enchant.failed.level"
	enchantSuccessKey            = "commands.enchant.success."
)

// enchantNode builds the /enchant tree, matching the vanilla wire: any
// entities as targets, then the enchantment out of its registry — executable
// on its own, one being the level then — and the level behind it, at least
// zero.
// vanilla: EnchantCommand.register
func enchantNode() *Node {
	targetsArg := &Node{
		Type:       NodeArgument,
		Name:       "targets",
		Parser:     ParserEntity,
		Properties: func(b *protocol.Buffer) { b.PutU8(0x00) }, // entities, several
		EntityArg:  entityTargets(),
		Suggests:   AskServerSuggestion,
		Completions: func(ctx any, partial string) (int, []string) {
			return EntityCompletions(ctx, partial)
		},
	}
	enchantmentArg := &Node{
		Type:        NodeArgument,
		Name:        "enchantment",
		Executable:  true,
		Parser:      ParserResource,
		Properties:  func(b *protocol.Buffer) { b.PutString(enchantmentRegistry) },
		Resource:    &ResourceArg{Registry: enchantmentRegistry},
		Suggests:    AskServerSuggestion,
		Completions: ResourceCompletions(enchantmentRegistry),
	}
	enchantmentArg.Children = []*Node{{
		Type:       NodeArgument,
		Name:       "level",
		Executable: true,
		Parser:     ParserInteger,
		Int:        &IntArg{Min: 0, HasMin: true},
	}}
	targetsArg.Children = []*Node{enchantmentArg}
	return &Node{
		Type:     NodeLiteral,
		Name:     "enchant",
		Required: PermLevelGamemasters,
		Children: []*Node{targetsArg},
	}
}

// runEnchant runs /enchant <targets> <enchantment> [level] on each living
// target's main-hand item that may take it. The level is checked against the
// enchantment first; a single target that cannot take it refuses the
// command, while several such targets are skipped.
// It answers how many items took the enchantment.
// vanilla: EnchantCommand.enchant
func runEnchant(src Source, input string, args []string) (int32, error) {
	word, rest := splitTargets(input, args)
	if len(rest) < 1 {
		src.Debug("incomplete enchant command")
		return 0, nil
	}
	targets, err := resolveEntities(src, word)
	if err != nil {
		return 0, err
	}
	name, ok := parseResourceLocation(rest[0])
	if !ok {
		src.Debug("enchant enchantment did not parse", "word", rest[0])
		return 0, nil
	}
	id, ok := registry.EnchantmentID(name)
	if !ok {
		src.Debug("enchant names an unknown enchantment", "name", name)
		return 0, nil
	}
	def, ok := registry.EnchantmentEffectsByID(id)
	if !ok {
		src.Debug("enchant names an enchantment this server keeps no rules for", "name", name)
		return 0, nil
	}
	level := int64(1)
	if len(rest) > 1 {
		if len(rest) > 2 {
			src.Debug("enchant carries trailing words", "words", rest[2:])
			return 0, nil
		}
		if level, ok = parseInteger(rest[1]); !ok {
			src.Debug("enchant level did not parse", "level", rest[1])
			return 0, nil
		}
	}
	if level > int64(def.MaxLevel) {
		return 0, refuse(src, enchantFailedLevelKey, entityCount(int(level)), entityCount(int(def.MaxLevel)))
	}

	// The main-hand slot answers only for a living body here, which is
	// the one shape of target the command may enchant. The read, the check
	// and the write are one hold of the target's lock, so a sword dropped or
	// a slot re-selected between them cannot take the enchanted copy.
	var tracker responseTracker[Entity]
	for _, target := range targets {
		var (
			failKey  string
			failWith []protocol.Text
		)
		ok := target.UpdateItemIn(ItemSlotMainHand, func(stack protocol.ItemStack) (protocol.ItemStack, bool) {
			switch {
			case stack.Empty():
				failKey, failWith = enchantFailedItemlessKey, []protocol.Text{{Text: target.DisplayName().Plain()}}
			case !rules.CanEnchant(stack, id):
				failKey, failWith = enchantFailedIncompatibleKey, []protocol.Text{{Text: rules.ItemName(stack).Plain()}}
			default:
				// A zero level writes nothing: vanilla's upgrade merge
				// of a zero leaves the stack carrying what it did.
				if level <= 0 {
					return stack, false
				}
				stack.SetEnchantment(false, id, int32(level))
				return stack, true
			}
			return stack, false
		})
		switch {
		case !ok:
			if len(targets) == 1 {
				return 0, refuse(src, enchantFailedEntityKey, protocol.Text{Text: target.DisplayName().Plain()})
			}
		case failKey != "":
			if len(targets) == 1 {
				return 0, refuse(src, failKey, failWith...)
			}
		default:
			tracker.track(target, 1)
		}
	}
	return tracker.feedback(src, enchantFailedKey, enchantSuccessKey, Entity.DisplayName,
		func(who protocol.Text, _ int) []protocol.Text {
			return []protocol.Text{enchantNameText(id, int32(level), def), who}
		})
}

// enchantNameText is an enchantment's name with its level the way the
// feedback carries it: the description, gray or the red the curses wear,
// with the level's numerals behind it — which a single-level enchantment at
// level one does not carry.
// vanilla: Enchantment.getFullname
func enchantNameText(id, level int32, def *registry.EnchantmentDef) protocol.Text {
	name := protocol.Text{
		Translate: "enchantment." + strings.Replace(def.Name, ":", ".", 1),
		Color:     protocol.ColorGray,
	}
	if registry.EnchantmentInTag(id, "minecraft:curse") {
		name.Color = protocol.ColorRed
	}
	if level != 1 || def.MaxLevel != 1 {
		name.Extra = []protocol.Text{
			{Text: " "},
			{Translate: "enchantment.level." + strconv.Itoa(int(level))},
		}
	}
	return name
}
