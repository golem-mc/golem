package command

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// Damage is the /damage command.
var Damage = Command{
	Name:  "damage",
	Level: PermLevelGamemasters,
	Build: damageNode,
	Run:   runDamage,
}

// DamageTarget is an entity the /damage command can hurt.
type DamageTarget interface {
	// Damage hurts the entity through its normal hurt pipeline and reports
	// whether the hurt took hold; false means the entity was invulnerable to
	// the type or inside its post-hit window.
	// vanilla: Entity.hurtServer
	Damage(amount float32, damageType registry.DamageType, origin DamageOrigin) bool
}

// DamageOrigin is what the at, by and from clauses of /damage add to the
// damage source; the zero value is a source with no position and no entity.
// vanilla: DamageSource
type DamageOrigin struct {
	// At is the explicit source position of the at clause.
	At *player.Position
	// Direct is what carried the blow, the entity of the by clause.
	Direct Entity
	// Cause is who is responsible, the entity of the from clause; Direct
	// answers for it when nil.
	Cause Entity
}

const damageTypeRegistry = "minecraft:damage_type"

// damageNode builds the /damage tree.
func damageNode() *Node {
	entityNode := func(name string) *Node {
		return &Node{
			Type:       NodeArgument,
			Name:       name,
			Executable: true,
			Parser:     ParserEntity,
			Properties: func(b *protocol.Buffer) { b.PutU8(0x01) }, // one entity
			EntityArg:  &EntityArg{Single: true},
			Suggests:   AskServerSuggestion,
			Completions: func(ctx any, partial string) (int, []string) {
				return EntityCompletions(ctx, partial)
			},
		}
	}
	cause := entityNode("cause")
	by := entityNode("entity")
	by.Children = []*Node{{Type: NodeLiteral, Name: "from", Children: []*Node{cause}}}
	damageType := &Node{
		Type:       NodeArgument,
		Name:       "damageType",
		Executable: true,
		Parser:     ParserResource,
		Properties: func(b *protocol.Buffer) { b.PutString(damageTypeRegistry) },
		Resource:   &ResourceArg{Registry: damageTypeRegistry},
		Suggests:   AskServerSuggestion,
		Completions: func(_ any, partial string) (int, []string) {
			reg, _ := registry.Get(damageTypeRegistry)
			return 0, ResourceSuggestions(partial, reg.Entries...)
		},
		Children: []*Node{
			{Type: NodeLiteral, Name: "at", Children: []*Node{
				{Type: NodeArgument, Name: "location", Executable: true, Parser: ParserVec3},
			}},
			{Type: NodeLiteral, Name: "by", Children: []*Node{by}},
		},
	}
	amount := &Node{
		Type:       NodeArgument,
		Name:       "amount",
		Executable: true,
		Parser:     ParserFloat,
		Float:      &FloatArg{Min: 0, HasMin: true},
		Children:   []*Node{damageType},
	}
	target := &Node{
		Type:       NodeArgument,
		Name:       "target",
		Parser:     ParserEntity,
		Properties: func(b *protocol.Buffer) { b.PutU8(0x01) }, // one entity
		EntityArg:  &EntityArg{Single: true},
		Suggests:   AskServerSuggestion,
		Completions: func(ctx any, partial string) (int, []string) {
			return EntityCompletions(ctx, partial)
		},
		Children: []*Node{amount},
	}
	return &Node{Type: NodeLiteral, Name: "damage", Required: PermLevelGamemasters, Children: []*Node{target}}
}

// runDamage runs /damage <target> <amount> [<damageType> [at <location> |
// by <entity> [from <cause>]]]; the type is minecraft:generic when left out.
// vanilla: DamageCommand.damage
func runDamage(src Source, input string, args []string) error {
	word, rest := splitTargets(input, args)
	if word == "" || len(rest) < 1 {
		return nil
	}
	found, err := resolveEntities(src, word)
	if err != nil {
		return err
	}
	amount, perr := readDoubleWord(rest[0])
	if perr != nil {
		return nil
	}
	damageType := registry.DamageTypeGeneric
	if len(rest) > 1 {
		name, ok := parseResourceLocation(rest[1])
		if !ok {
			return nil
		}
		damageType, _ = registry.DamageTypeByName(name)
	}
	var origin DamageOrigin
	if len(rest) > 2 {
		var ok bool
		var err error
		if origin, ok, err = damageOrigin(src, input, rest[2:]); err != nil || !ok {
			return err
		}
	}
	target := found[0]
	if t, ok := target.(DamageTarget); !ok || !t.Damage(float32(amount), damageType, origin) {
		return refuse(src, "commands.damage.invulnerable")
	}
	return feedbackToAdmins(src, "commands.damage.success",
		protocol.Text{Text: formatJavaFloat(float32(amount))}, target.DisplayName())
}

// damageOrigin reads the words after the damage type: `at <location>` or
// `by <entity> [from <cause>]`. ok is false for words the parser would have
// refused.
// vanilla: DamageCommand.register
func damageOrigin(src Source, input string, words []string) (origin DamageOrigin, ok bool, err error) {
	switch words[0] {
	case "at":
		if len(words) != 4 {
			return origin, false, nil
		}
		x, y, z, valid := resolveVec3(src, words[1:])
		if !valid {
			return origin, false, nil
		}
		origin.At = &player.Position{X: x, Y: y, Z: z}
	case "by":
		word, rest := splitTargets(input, words[1:])
		if word == "" {
			return origin, false, nil
		}
		by, err := resolveEntities(src, word)
		if err != nil {
			return origin, false, err
		}
		origin.Direct = by[0]
		if len(rest) == 0 {
			return origin, true, nil
		}
		if rest[0] != "from" {
			return origin, false, nil
		}
		word, _ = splitTargets(input, rest[1:])
		if word == "" {
			return origin, false, nil
		}
		cause, err := resolveEntities(src, word)
		if err != nil {
			return origin, false, err
		}
		origin.Cause = cause[0]
	default:
		return origin, false, nil
	}
	return origin, true, nil
}
