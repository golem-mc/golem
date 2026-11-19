package command

import (
	"math"
	"strconv"
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
)

// Gamerule is the /gamerule command.
var Gamerule = Command{
	Name:   "gamerule",
	Level:  PermLevelGamemasters,
	Build:  gameruleNode,
	Run:    discardResult(runGamerule),
	Result: runGamerule,
}

// The translation keys the /gamerule command answers with, one for the
// query a bare rule name carries out and one for the set a value follows
// with. The vanilla server sends the set one to everyone who would hear a
// command's success and the query one to the runner alone, and this
// server's feedback goes to the runner either way.
const (
	gameruleQueryKey  = "commands.gamerule.query"
	gameruleSetKey    = "commands.gamerule.set"
	gameruleNotSetKey = "commands.gamerule.not_set"
)

// gameruleNode builds /gamerule as the vanilla report has it: a literal per rule,
// plain and minecraft:-prefixed, each a query on its own and a set with the
// value argument its kind asks for, in vanilla's Commands packet order; the
// one feature-gated rule is left out.
func gameruleNode() *Node {
	node := &Node{
		Type:     NodeLiteral,
		Name:     "gamerule",
		Required: PermLevelGamemasters,
	}
	for _, id := range world.GameRuleTreeOrder {
		rule, ok := world.RuleByID(id)
		if !ok {
			continue
		}
		for _, name := range []string{rule.ID, "minecraft:" + rule.ID} {
			ruleNode := &Node{
				Type:       NodeLiteral,
				Name:       name,
				Executable: true,
			}
			if value := gameruleValueArg(&rule); value != nil {
				ruleNode.Children = []*Node{value}
			}
			node.Children = append(node.Children, ruleNode)
		}
	}
	return node
}

// gameruleValueArg builds the value argument one rule's literal takes, or
// nil for a rule whose kind the tree has no argument for.
func gameruleValueArg(rule *world.GameRule) *Node {
	switch rule.Kind {
	case world.RuleBool:
		return &Node{
			Type:       NodeArgument,
			Name:       "value",
			Executable: true,
			Parser:     ParserBool,
		}
	case world.RuleInt:
		flags := 0
		if rule.Min > math.MinInt32 {
			flags |= 0x01
		}
		if rule.Max < math.MaxInt32 {
			flags |= 0x02
		}
		return &Node{
			Type:       NodeArgument,
			Name:       "value",
			Executable: true,
			Parser:     ParserInteger,
			Int:        &IntArg{Min: rule.Min, Max: rule.Max, HasMin: flags&0x01 != 0, HasMax: flags&0x02 != 0},
		}
	}
	return nil
}

// runGamerule runs /gamerule <rule> [value]: a bare rule name reads it out
// and a value follows with puts it there, the value what the rule's own
// argument parser would have read and the change what every reader of the
// rule sees on the next tick.
func runGamerule(src Source, input string, args []string) (int32, error) {
	if len(args) == 0 {
		src.Debug("incomplete gamerule command")
		return 0, nil
	}

	// The rule literal may be spelled with the minecraft: prefix the tree
	// carries alongside the plain name; both name the same rule.
	id := strings.TrimPrefix(args[0], "minecraft:")
	kind, min, max, ok := world.RuleBounds(id)
	if !ok {
		src.Debug("unknown gamerule", "rule", args[0])
		return 0, nil
	}

	if len(args) == 1 {
		return resultOf(gameruleResult(src, id, kind), src.Feedback(gameruleQueryKey,
			protocol.Text{Text: id}, GameRuleValueText(src, id, kind)))
	}

	value := wordsIn(input, args, 1, len(args)-1)
	// Setting a rule to the value it holds is refused; the Set Game Rule
	// packet path has no such check.
	// vanilla: GameRuleCommand.setRule
	if v, valid := gameruleValue(kind, min, max, value); valid && v == gameruleResult(src, id, kind) {
		return 0, refuse(src, gameruleNotSetKey, protocol.Text{Text: id}, GameRuleValueText(src, id, kind))
	}
	if !SetGamerule(src, id, value) {
		src.Debug("gamerule value did not parse", "rule", id, "value", strings.Join(args[1:], " "))
		return 0, nil
	}
	if err := feedbackToAdmins(src, gameruleSetKey,
		protocol.Text{Text: id}, GameRuleValueText(src, id, kind)); err != nil {
		return 0, err
	}
	src.Info("gamerule set", "rule", id, "value", GameRuleValueText(src, id, kind).Text, "by", src.Name())
	return gameruleResult(src, id, kind), nil
}

// gameruleResult is a rule's value as the command answers it: 1 or 0 for a
// boolean rule, the value for an integer one.
// vanilla: GameRules.registerBoolean, GameRules.registerInteger
func gameruleResult(src Source, id string, kind world.RuleKind) int32 {
	switch kind {
	case world.RuleBool:
		if src.BoolRule(world.RuleIndex(id)) {
			return 1
		}
	case world.RuleInt:
		return int32(src.IntRule(world.RuleIndex(id)))
	}
	return 0
}

// SetGamerule sets one rule, shared by /gamerule and Set Game Rule: the value
// is read by the rule's kind, refused where its bounded argument would be,
// and the rule's packets go out. It reports whether the rule exists and the
// value was accepted; feedback is the caller's, since the command answers
// the runner and operators while the packet path messages every operator.
// vanilla: GameRuleCommand.setRule
func SetGamerule(src Source, id, value string) bool {
	kind, min, max, ok := world.RuleBounds(id)
	if !ok {
		return false
	}
	v, valid := gameruleValue(kind, min, max, value)
	if !valid {
		return false
	}
	switch kind {
	case world.RuleBool:
		src.SetBoolRule(world.RuleIndex(id), v == 1)
	case world.RuleInt:
		src.SetIntRule(world.RuleIndex(id), v)
	}
	// The rule's own packets precede the caller's feedback.
	// vanilla: GameRuleCommand.setRule
	src.RuleChanged(id)
	return true
}

// gameruleValue reads a value by the rule's kind, answered as gameruleResult
// would answer it: 1 or 0 for a boolean, the value for an integer.
func gameruleValue(kind world.RuleKind, min, max int32, value string) (int32, bool) {
	switch kind {
	case world.RuleBool:
		v, valid := gameruleBoolValue(value)
		if v {
			return 1, valid
		}
		return 0, valid
	case world.RuleInt:
		return gameruleIntValue(value, min, max)
	}
	return 0, false
}

// gameruleBoolValue reads a value the way the rule's own boolean argument
// reads: exactly "true" or "false", refused with a tail.
// vanilla: BoolArgumentType.parse
func gameruleBoolValue(value string) (bool, bool) {
	v, n, err := readStringValue(value)
	if err != nil || n != len(value) {
		return false, false
	}
	switch v {
	case "true":
		return true, true
	case "false":
		return false, true
	}
	return false, false
}

// gameruleIntValue reads a value the way the rule's own integer argument
// reads: a base-10 int with no tail, refused outside the rule's range.
// vanilla: IntegerArgumentType.parse
func gameruleIntValue(value string, min, max int32) (int32, bool) {
	r := &coordReader{s: value}
	v, err := r.readInt()
	if err != nil || r.canRead() {
		return 0, false
	}
	if v < min || v > max {
		return 0, false
	}
	return v, true
}

// GameRuleValueText reads one rule's current value as the text component the
// /gamerule feedback and the Game Rule Values packet spell it with: true or
// false for a boolean, the decimal digits for an integer.
func GameRuleValueText(src Source, id string, kind world.RuleKind) protocol.Text {
	switch kind {
	case world.RuleBool:
		if src.BoolRule(world.RuleIndex(id)) {
			return protocol.Text{Text: "true"}
		}
		return protocol.Text{Text: "false"}
	case world.RuleInt:
		return protocol.Text{Text: strconv.FormatInt(int64(src.IntRule(world.RuleIndex(id))), 10)}
	}
	return protocol.Text{}
}
