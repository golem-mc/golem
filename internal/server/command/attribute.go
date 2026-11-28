package command

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// Attribute is the /attribute command.
var Attribute = Command{
	Name:   "attribute",
	Level:  PermLevelGamemasters,
	Build:  attributeNode,
	Run:    discardResult(runAttribute),
	Result: runAttribute,
}

const attributeRegistry = "minecraft:attribute"

// AttributeTarget is an entity whose attribute map a command edits.
type AttributeTarget interface {
	Entity

	// HasAttributes reports whether the entity is a living one with an
	// attribute map at all.
	HasAttributes() bool

	// WithAttributes runs fn over the entity's attributes under its lock.
	// The changes reach the clients on the next tick.
	WithAttributes(fn func(a *player.Attributes))
}

// attributeNode builds the /attribute tree, matching the vanilla report.
func attributeNode() *Node {
	lit := func(name string, executable bool, children ...*Node) *Node {
		return &Node{Type: NodeLiteral, Name: name, Executable: executable, Children: children}
	}
	double := func(name string, executable bool, children ...*Node) *Node {
		return &Node{
			Type: NodeArgument, Name: name, Executable: executable, Parser: ParserDouble,
			Properties: func(b *protocol.Buffer) { b.PutU8(0) },
			Check:      checkDoubleArg, Children: children,
		}
	}
	id := func(executable bool, children ...*Node) *Node {
		return &Node{Type: NodeArgument, Name: "id", Executable: executable, Parser: ParserResourceLoc, Children: children}
	}
	scaled := func() *Node { return double("scale", true) }
	op := func(name string) *Node { return lit(name, true) }
	attr := &Node{
		Type:       NodeArgument,
		Name:       "attribute",
		Parser:     ParserResource,
		Properties: func(b *protocol.Buffer) { b.PutString(attributeRegistry) },
		Resource:   &ResourceArg{Registry: attributeRegistry},
		Suggests:   AskServerSuggestion,
		Completions: func(_ any, partial string) (int, []string) {
			names := make([]string, len(registry.Attributes))
			for i, a := range registry.Attributes {
				names[i] = a.Name
			}
			return 0, ResourceSuggestions(partial, names...)
		},
		Children: []*Node{
			lit("get", true, scaled()),
			lit("base", false,
				lit("set", false, double("value", true)),
				lit("get", true, scaled()),
				lit("reset", true),
			),
			lit("modifier", false,
				lit("add", false, id(false, double("value", false,
					op("add_value"), op("add_multiplied_base"), op("add_multiplied_total")))),
				lit("remove", false, id(true)),
				lit("value", false, lit("get", false, id(true, scaled()))),
			),
		},
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
		Children: []*Node{attr},
	}
	return &Node{Type: NodeLiteral, Name: "attribute", Required: PermLevelGamemasters, Children: []*Node{target}}
}

// runAttribute runs /attribute <target> <attribute> get|base|modifier ...
// and answers the value a get returns, scaled and truncated to an int.
// vanilla: AttributeCommand
func runAttribute(src Source, input string, args []string) (int32, error) {
	word, rest := splitTargets(input, args)
	if word == "" || len(rest) < 2 {
		return 0, nil
	}
	found, err := resolveEntities(src, word)
	if err != nil {
		return 0, err
	}
	target := found[0]
	name, ok := parseResourceLocation(rest[0])
	if !ok {
		return 0, nil
	}
	attrName := protocol.Text{Translate: "attribute.name." + name[len("minecraft:"):]}
	holder, ok := target.(AttributeTarget)
	if !ok || !holder.HasAttributes() {
		return 0, refuse(src, "commands.attribute.failed.entity", target.DisplayName())
	}
	who := target.DisplayName
	noAttribute := func() error {
		return refuse(src, "commands.attribute.failed.no_attribute", who(), attrName)
	}
	attr, known := registry.AttributeByName(name)
	id := registry.AttributeID(attr.ID)
	has := false
	if known {
		holder.WithAttributes(func(a *player.Attributes) { has = a.Has(id) })
	}
	if !has {
		return 0, noAttribute()
	}
	number := func(v float64) protocol.Text { return protocol.Text{Text: formatJavaDouble(v)} }
	scaleAt := func(i int) (float64, bool) {
		if i >= len(rest) {
			return 1, true
		}
		scale, perr := readDoubleWord(rest[i])
		return scale, perr == nil
	}

	switch rest[1] {
	case "get":
		scale, ok := scaleAt(2)
		if !ok {
			return 0, nil
		}
		var value float64
		holder.WithAttributes(func(a *player.Attributes) { value = a.Value(id) })
		return javaDoubleToInt(value * scale), lazyFeedback(src, "commands.attribute.value.get.success", func() []protocol.Text { return []protocol.Text{attrName, who(), number(value)} })
	case "base":
		return runAttributeBase(src, holder, id, rest[2:], attrName, who, scaleAt)
	case "modifier":
		return runAttributeModifier(src, holder, id, rest[2:], attrName, who)
	}
	return 0, nil
}

// runAttributeBase runs base set|get|reset.
func runAttributeBase(src Source, holder AttributeTarget, id registry.AttributeID, rest []string, attrName protocol.Text, who func() protocol.Text,
	scaleAt func(int) (float64, bool)) (int32, error) {
	if len(rest) == 0 {
		return 0, nil
	}
	number := func(v float64) protocol.Text { return protocol.Text{Text: formatJavaDouble(v)} }
	switch rest[0] {
	case "set":
		if len(rest) < 2 {
			return 0, nil
		}
		value, perr := readDoubleWord(rest[1])
		if perr != nil {
			return 0, nil
		}
		holder.WithAttributes(func(a *player.Attributes) { a.SetBase(id, value) })
		return 1, lazyFeedback(src, "commands.attribute.base_value.set.success", func() []protocol.Text { return []protocol.Text{attrName, who(), number(value)} })
	case "get":
		scale, ok := scaleAt(3)
		if !ok {
			return 0, nil
		}
		var value float64
		holder.WithAttributes(func(a *player.Attributes) { value = a.Base(id) })
		return javaDoubleToInt(value * scale),
			lazyFeedback(src, "commands.attribute.base_value.get.success", func() []protocol.Text { return []protocol.Text{attrName, who(), number(value)} })
	case "reset":
		var value float64
		holder.WithAttributes(func(a *player.Attributes) {
			a.ResetBase(id)
			value = a.Base(id)
		})
		return 1, lazyFeedback(src, "commands.attribute.base_value.reset.success", func() []protocol.Text { return []protocol.Text{attrName, who(), number(value)} })
	}
	return 0, nil
}

// runAttributeModifier runs modifier add|remove|value get.
func runAttributeModifier(src Source, holder AttributeTarget, id registry.AttributeID, rest []string, attrName protocol.Text, who func() protocol.Text) (int32, error) {
	if len(rest) < 2 {
		return 0, nil
	}
	sub := rest[0]
	if sub == "value" {
		sub, rest = "value get", rest[1:]
		if len(rest) < 2 || rest[0] != "get" {
			return 0, nil
		}
	}
	modID, ok := parseResourceLocation(rest[1])
	if !ok {
		return 0, nil
	}
	idText := protocol.Text{Text: modID}
	noModifier := func() error {
		return refuse(src, "commands.attribute.failed.no_modifier", attrName, who(), idText)
	}
	var existing player.Modifier
	var present bool
	holder.WithAttributes(func(a *player.Attributes) { existing, present = a.Modifier(id, modID) })
	switch sub {
	case "add":
		if len(rest) < 4 {
			return 0, nil
		}
		amount, perr := readDoubleWord(rest[2])
		operation, known := registry.AttributeOperationByName(rest[3])
		if perr != nil || !known {
			return 0, nil
		}
		if present {
			return 0, refuse(src, "commands.attribute.failed.modifier_already_present", idText, attrName, who())
		}
		holder.WithAttributes(func(a *player.Attributes) {
			a.AddModifier(id, player.Modifier{ID: modID, Amount: amount, Operation: operation, Persistent: true})
		})
		return 1, lazyFeedback(src, "commands.attribute.modifier.add.success", func() []protocol.Text { return []protocol.Text{idText, attrName, who()} })
	case "remove":
		if !present {
			return 0, noModifier()
		}
		holder.WithAttributes(func(a *player.Attributes) { a.RemoveModifier(id, modID) })
		return 1, lazyFeedback(src, "commands.attribute.modifier.remove.success", func() []protocol.Text { return []protocol.Text{idText, attrName, who()} })
	case "value get":
		scale := 1.0
		if len(rest) > 2 {
			var perr *ParseError
			if scale, perr = readDoubleWord(rest[2]); perr != nil {
				return 0, nil
			}
		}
		if !present {
			return 0, noModifier()
		}
		return javaDoubleToInt(existing.Amount * scale), lazyFeedback(src,
			"commands.attribute.modifier.value.get.success", func() []protocol.Text {
				return []protocol.Text{idText, attrName, who(), {Text: formatJavaDouble(existing.Amount)}}
			})
	}
	return 0, nil
}

// lazyFeedback sends the runner's feedback, building its arguments only when
// someone will see it.
func lazyFeedback(src Source, key string, with func() []protocol.Text) error {
	if !wantsFeedback(src) {
		return nil
	}
	return src.Feedback(key, with()...)
}
