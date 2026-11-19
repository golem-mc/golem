package command

import (
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
)

// Tag is the /tag command.
// vanilla: TagCommand
var Tag = Command{
	Name:   "tag",
	Level:  PermLevelGamemasters,
	Build:  tagNode,
	Run:    discardResult(runTag),
	Result: runTag,
}

// The translation keys /tag answers with: the refusals of an add or remove
// that came to nothing, the lines a change leaves on one or several bodies,
// and the prefix of the four forms a list takes.
const (
	tagAddFailedKey     = "commands.tag.add.failed"
	tagAddSuccessKey    = "commands.tag.add.success."
	tagRemoveFailedKey  = "commands.tag.remove.failed"
	tagRemoveSuccessKey = "commands.tag.remove.success."
	tagListKey          = "commands.tag.list."
)

// tagNode builds the /tag tree, matching the vanilla report: a set of
// entities, then add and remove with a word each and list bare. Only the
// remove's name suggests, the tags its targets already hold.
func tagNode() *Node {
	targetsArg := &Node{
		Type:       NodeArgument,
		Name:       "targets",
		Parser:     ParserEntity,
		EntityArg:  entityTargets(),
		Properties: func(b *protocol.Buffer) { b.PutU8(0x00) }, // entities, several
	}
	wordArg := func(suggests bool) *Node {
		node := &Node{
			Type:       NodeArgument,
			Name:       "name",
			Executable: true,
			Parser:     ParserString,
			Properties: func(b *protocol.Buffer) { b.PutVarInt(0) }, // single word
		}
		if suggests {
			node.Suggests = AskServerSuggestion
			node.Completions = suggestHeldTags
		}
		return node
	}
	targetsArg.Children = []*Node{
		{Type: NodeLiteral, Name: "add", Children: []*Node{wordArg(false)}},
		{Type: NodeLiteral, Name: "remove", Children: []*Node{wordArg(true)}},
		{Type: NodeLiteral, Name: "list", Executable: true},
	}
	return &Node{
		Type:     NodeLiteral,
		Name:     "tag",
		Required: PermLevelGamemasters,
		Children: []*Node{targetsArg},
	}
}

// runTag runs /tag <targets> add|remove <name> and /tag <targets> list: the
// scoreboard tags of the entities named. It answers the entities changed, or
// the tags listed.
func runTag(src Source, input string, args []string) (int32, error) {
	word, rest := splitTargets(input, args)
	targets, err := resolveEntities(src, word)
	if err != nil {
		return 0, err
	}
	name := ""
	if len(rest) > 1 {
		name = rest[1]
	}
	switch rest[0] {
	case "add":
		return tagChange(src, targets, name, Entity.AddTag,
			tagAddFailedKey, tagAddSuccessKey)
	case "remove":
		return tagChange(src, targets, name, Entity.RemoveTag,
			tagRemoveFailedKey, tagRemoveSuccessKey)
	default:
		return tagList(src, targets)
	}
}

// tagChange is the shape /tag add and /tag remove share: the change runs on
// every target, a change that came to nothing is the key's own refusal,
// and the targets it changed tell the success line its form.
// vanilla: TagCommand.addTag, removeTag
func tagChange(src Source, targets []Entity, name string, change func(Entity, string) bool,
	failedKey, successKey string) (int32, error) {
	var tracker responseTracker[Entity]
	for _, target := range targets {
		tracker.trackBool(target, change(target, name))
	}
	return tracker.feedback(src, failedKey, successKey, Entity.DisplayName,
		func(who protocol.Text, _ int) []protocol.Text { return []protocol.Text{{Text: name}, who} })
}

// tagList reports the union of the targets' tags, naming the one target that
// holds any or counting those that do. With no tag at all no target counts,
// so even a lone target gets the multiple empty line counting 0. A list is
// not echoed to the other operators.
// vanilla: TagCommand.listTags
func tagList(src Source, targets []Entity) (int32, error) {
	var tracker responseTracker[Entity]
	for _, target := range targets {
		tracker.track(target, len(target.Tags()))
	}
	tags := heldTags(targets)
	form, who := tracker.target(Entity.DisplayName)
	if len(tags) == 0 {
		return 0, src.Feedback(tagListKey+form+".empty", who)
	}
	return resultOf(int32(len(tags)), src.Feedback(tagListKey+form+".success",
		who, entityCount(len(tags)), formatTagList(tags)))
}

// heldTags is the union of the targets' tags, sorted, which is the list
// /tag shows and the remove argument suggests.
func heldTags(targets []Entity) []string {
	union := map[string]bool{}
	for _, target := range targets {
		for _, tag := range target.Tags() {
			union[tag] = true
		}
	}
	tags := make([]string, 0, len(union))
	for tag := range union {
		tags = append(tags, tag)
	}
	slices.Sort(tags)
	return tags
}

// suggestHeldTags suggests the union of the tags the targets the line
// names already hold; a selector that refuses or finds nobody offers
// nothing.
// vanilla: TagCommand.getTags behind the remove argument's suggestions
func suggestHeldTags(ctx any, partial string) (int, []string) {
	state, ok := ctx.(*suggestState)
	if !ok {
		return 0, nil
	}
	word, ok := state.arg("targets")
	if !ok {
		return 0, nil
	}
	sel, _, err := parseSelectorWord(word, 0, state.PermLevel() >= PermLevelGamemasters)
	if err != nil {
		return 0, nil
	}
	return 0, Suggestions(partial, heldTags(sel.findEntities(state.Source))...)
}

// formatTagList spells a /tag list's tags the way ComponentUtils's
// formatAndSortList does: the words in green, comma-separated in gray, the
// order already sorted.
func formatTagList(tags []string) protocol.Text {
	if len(tags) == 1 {
		return protocol.Text{Text: tags[0], Color: protocol.ColorGreen}
	}
	extra := make([]protocol.Text, 0, len(tags)*2-1)
	for i, tag := range tags {
		if i > 0 {
			extra = append(extra, protocol.Text{Text: ", ", Color: protocol.ColorGray})
		}
		extra = append(extra, protocol.Text{Text: tag, Color: protocol.ColorGreen})
	}
	return protocol.Text{Extra: extra}
}
