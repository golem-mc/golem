package command

import (
	"strconv"

	"github.com/golem-mc/golem/internal/protocol"
)

// Trigger is the /trigger command, open to every player.
// vanilla: TriggerCommand
var Trigger = Command{
	Name:   "trigger",
	Level:  0,
	Build:  triggerNode,
	Run:    discardResult(runTrigger),
	Result: runTrigger,
}

const triggerKey = "commands.trigger."

func triggerNode() *Node {
	value := func(name string) *Node {
		return &Node{Type: NodeLiteral, Name: name, Children: []*Node{{
			Type: NodeArgument, Name: "value", Parser: ParserInteger, Executable: true}}}
	}
	return &Node{Type: NodeLiteral, Name: "trigger", Children: []*Node{{
		Type: NodeArgument, Name: "objective", Parser: ParserObjective, Executable: true,
		Suggests: AskServerSuggestion, Completions: primedTriggerCompletions,
		Children: []*Node{value("add"), value("set")},
	}}}
}

// primedTriggerCompletions offers the trigger objectives the runner's score
// is unlocked in.
// vanilla: TriggerCommand.suggestObjectives
func primedTriggerCompletions(ctx any, partial string) (int, []string) {
	src, ok := ctx.(Source)
	if !ok || src.SelfEntity() == nil {
		return 0, nil
	}
	board := src.Scoreboard()
	name := entityHolder(src.SelfEntity()).Name
	var names []string
	for _, o := range board.Objectives() {
		if o.Criteria.Name != "trigger" {
			continue
		}
		if score, ok := board.Score(name, o.Name); ok && !score.Locked {
			names = append(names, o.Name)
		}
	}
	return 0, Suggestions(partial, names...)
}

// runTrigger runs /trigger <objective> [add|set <value>], answering the
// score's new value (set's value).
func runTrigger(src Source, _ string, args []string) (int32, error) {
	if len(args) == 0 {
		src.Debug("incomplete trigger command")
		return 0, nil
	}
	self, ok := src.SelfEntity().(Target)
	if !ok {
		return 0, refuse(src, "permissions.requires.player")
	}
	board := src.Scoreboard()
	o, ok := board.Objective(args[0])
	if !ok {
		return 0, refuse(src, objectiveNotFoundKey, protocol.Text{Text: args[0]})
	}
	key := "simple.success"
	change := func(v int32) int32 { return v + 1 }
	var amount int32
	if len(args) >= 3 {
		n, err := strconv.ParseInt(args[2], 10, 32)
		if err != nil {
			src.Debug("bad trigger value", "value", args[2])
			return 0, nil
		}
		amount = int32(n)
		switch args[1] {
		case "add":
			key = "add.success"
			change = func(v int32) int32 { return v + amount }
		case "set":
			key = "set.success"
			change = func(int32) int32 { return amount }
		default:
			src.Debug("unknown trigger mode", "args", args)
			return 0, nil
		}
	}
	if o.Criteria.Name != "trigger" {
		return 0, refuse(src, triggerKey+"failed.invalid")
	}
	value, ok := board.Trigger(entityHolder(self).Holder, o.Name, change)
	if !ok {
		return 0, refuse(src, triggerKey+"failed.unprimed")
	}
	with := []protocol.Text{formatted(o)}
	if key != "simple.success" {
		with = append(with, intText(amount))
	}
	if err := feedbackToAdmins(src, triggerKey+key, with...); err != nil {
		return 0, err
	}
	return value, nil
}
