package command

import (
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/advancement"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// Advancement is the /advancement command.
var Advancement = Command{
	Name:   "advancement",
	Level:  PermLevelGamemasters,
	Build:  advancementNode,
	Run:    discardResult(runAdvancement),
	Result: runAdvancement,
}

const (
	// advancementRegistry is the registry the advancement argument names.
	advancementRegistry = "minecraft:advancement"
	// advancementNotFoundKey and advancementCriterionNotFoundKey are the
	// execution-time refusals for an unknown advancement and criterion.
	advancementNotFoundKey          = "advancement.advancementNotFound"
	advancementCriterionNotFoundKey = "commands.advancement.criterionNotFound"
)

// advancementMode is which advancements around the named one an
// /advancement applies to. vanilla: AdvancementCommands.Mode
type advancementMode struct{ parents, children bool }

var advancementModes = map[string]advancementMode{
	"only":    {},
	"through": {parents: true, children: true},
	"from":    {children: true},
	"until":   {parents: true},
}

// advancementNode builds the /advancement tree.
// vanilla: AdvancementCommands.register
func advancementNode() *Node {
	action := func(name string) *Node {
		targets := &Node{
			Type:       NodeArgument,
			Name:       "targets",
			Parser:     ParserEntity,
			Properties: func(b *protocol.Buffer) { b.PutU8(0x02) }, // players, several
			EntityArg:  &EntityArg{PlayersOnly: true},
			Suggests:   AskServerSuggestion,
			Completions: func(ctx any, partial string) (int, []string) {
				return EntityCompletions(ctx, partial)
			},
		}
		advancementArg := func() *Node {
			return &Node{
				Type:        NodeArgument,
				Name:        "advancement",
				Executable:  true,
				Parser:      ParserResourceKey,
				Properties:  func(b *protocol.Buffer) { b.PutString(advancementRegistry) },
				Suggests:    AskServerSuggestion,
				Check:       checkAdvancementID,
				Completions: advancementCompletions,
			}
		}
		only := advancementArg()
		only.Children = []*Node{{
			Type:       NodeArgument,
			Name:       "criterion",
			Executable: true,
			Parser:     ParserString,
			Properties: func(b *protocol.Buffer) { b.PutVarInt(2) }, // greedy phrase
			Greedy:     true,
		}}
		targets.Children = []*Node{
			{Type: NodeLiteral, Name: "only", Children: []*Node{only}},
			{Type: NodeLiteral, Name: "from", Children: []*Node{advancementArg()}},
			{Type: NodeLiteral, Name: "until", Children: []*Node{advancementArg()}},
			{Type: NodeLiteral, Name: "through", Children: []*Node{advancementArg()}},
			{Type: NodeLiteral, Name: "everything", Executable: true},
		}
		return &Node{Type: NodeLiteral, Name: name, Children: []*Node{targets}}
	}
	return &Node{
		Type:     NodeLiteral,
		Name:     "advancement",
		Required: PermLevelGamemasters,
		Children: []*Node{action("grant"), action("revoke")},
	}
}

// checkAdvancementID refuses a word that is not a resource location; whether
// the advancement exists is only known when the command runs.
// vanilla: ResourceKeyArgument.parse
func checkAdvancementID(word string) *ParseError {
	_, n, ok := readIdentifier(word)
	if !ok {
		return &ParseError{Key: keyIDInvalid}
	}
	if n < len(word) {
		return &ParseError{Key: keyExpectedSeparator, Cursor: n}
	}
	return nil
}

func advancementCompletions(_ any, partial string) (int, []string) {
	tree := advancement.Current()
	ids := make([]string, 0, tree.Len())
	for _, n := range tree.All() {
		ids = append(ids, n.ID)
	}
	return 0, ResourceSuggestions(partial, ids...)
}

// AdvancementName is how feedback and chat name an advancement: its title in
// square brackets (chat.square_brackets), coloured by its frame, with the
// description on hover; an advancement with no display is its bare ID.
// vanilla: Advancement.name, Advancement.decorateName
func AdvancementName(n *advancement.Node) protocol.Text {
	d := n.Display
	if d == nil {
		return protocol.Text{Text: n.ID}
	}
	color := [...]string{"green", "dark_purple", "green"}[d.Frame]
	title := DisplayTitle(d)
	desc := DisplayDescription(d)
	if title.Raw != nil {
		base := rawTextCompound(title.Raw)
		tooltip := maps.Clone(base)
		if _, hasColor := tooltip["color"]; !hasColor {
			tooltip["color"] = nbt.String(color)
		}
		tooltip["extra"] = appendRawExtra(tooltip["extra"], nbt.String("\n"), protocol.TextTag(desc))
		hovered := maps.Clone(base)
		hovered["hover_event"] = nbt.Compound{
			"action": nbt.String("show_text"),
			"value":  tooltip,
		}
		return protocol.Text{
			Translate: "chat.square_brackets",
			Color:     color,
			With:      []protocol.Text{{Raw: hovered}},
		}
	}
	tooltip := title
	if tooltip.Color == "" {
		tooltip.Color = color
	}
	tooltip.Extra = append(slices.Clone(tooltip.Extra), protocol.Text{Text: "\n"}, desc)
	title.HoverText = &tooltip
	return protocol.Text{Translate: "chat.square_brackets", Color: color, With: []protocol.Text{title}}
}

// DisplayTitle is a display's title component.
func DisplayTitle(d *registry.AdvancementDisplay) protocol.Text {
	return advancementDisplayText(d.Title, d.TitleLiteral, d.TitleText)
}

// DisplayDescription is a display's description component.
func DisplayDescription(d *registry.AdvancementDisplay) protocol.Text {
	return advancementDisplayText(d.Description, d.DescriptionLiteral, d.DescriptionText)
}

func advancementDisplayText(s string, literal bool, full *protocol.Text) protocol.Text {
	if full != nil {
		return *full
	}
	if literal {
		return protocol.Text{Text: s}
	}
	return protocol.Text{Translate: s}
}

// rawTextCompound copies a raw component tag into a mutable compound, unfolding
// a list component into its head with the remaining elements appended to extra.
// vanilla: ComponentSerialization.createFromList
func rawTextCompound(tag nbt.Tag) nbt.Compound {
	if fields, ok := nbt.CompoundView(tag); ok {
		return maps.Clone(fields)
	}
	if s, ok := tag.(nbt.String); ok {
		return nbt.Compound{"text": s}
	}
	if list, ok := tag.(nbt.List); ok && len(list.Items) > 0 {
		head := rawTextCompound(nbt.ListElement(list.Items[0]))
		if len(list.Items) > 1 {
			rest := make([]nbt.Tag, len(list.Items)-1)
			for i, item := range list.Items[1:] {
				rest[i] = nbt.ListElement(item)
			}
			head["extra"] = appendRawExtra(head["extra"], rest...)
		}
		return head
	}
	return nbt.Compound{"text": nbt.String("")}
}

func appendRawExtra(existing nbt.Tag, added ...nbt.Tag) nbt.List {
	var items []nbt.Tag
	if list, ok := existing.(nbt.List); ok {
		items = make([]nbt.Tag, 0, len(list.Items)+len(added))
		for _, item := range list.Items {
			items = append(items, nbt.ListElement(item))
		}
	}
	items = append(items, added...)
	return nbt.NewList(items)
}

// advancementAction is the grant or revoke half of an /advancement.
// vanilla: AdvancementCommands.Action
type advancementAction struct {
	key string
	// perform changes one advancement of one player, reporting whether it
	// did anything.
	perform func(p *advancement.Progress, n *advancement.Node) bool
	// performCriterion does the same for one criterion.
	performCriterion func(p *advancement.Progress, n *advancement.Node, criterion string) bool
}

var (
	advancementGrant = advancementAction{
		key: "commands.advancement.grant.",
		perform: func(p *advancement.Progress, n *advancement.Node) bool {
			if p.Done(n) {
				return false
			}
			now := time.Now()
			for _, c := range p.Remaining(n) {
				p.Award(n, c, now)
			}
			return true
		},
		performCriterion: func(p *advancement.Progress, n *advancement.Node, c string) bool {
			return p.Award(n, c, time.Now())
		},
	}
	advancementRevoke = advancementAction{
		key: "commands.advancement.revoke.",
		perform: func(p *advancement.Progress, n *advancement.Node) bool {
			if !p.HasProgress(n) {
				return false
			}
			for _, c := range p.Completed(n) {
				p.Revoke(n, c)
			}
			return true
		},
		performCriterion: func(p *advancement.Progress, n *advancement.Node, c string) bool {
			return p.Revoke(n, c)
		},
	}
)

// runAdvancement runs /advancement grant|revoke <targets> only|from|until|
// through|everything. It answers how many advancements changed, or for the
// criterion form how many players.
// vanilla: AdvancementCommands.perform, performCriterion
func runAdvancement(src Source, input string, args []string) (int32, error) {
	if len(args) < 2 {
		src.Debug("incomplete advancement command")
		return 0, nil
	}
	action := advancementGrant
	if args[0] == "revoke" {
		action = advancementRevoke
	}
	word, rest := splitTargets(input, args[1:])
	players, err := resolvePlayers(src, word)
	if err != nil {
		return 0, err
	}
	if len(rest) == 0 {
		src.Debug("incomplete advancement command")
		return 0, nil
	}
	tree := advancement.Current()
	if rest[0] == "everything" {
		// A grant of everything sends its final update without toasts.
		return advancementPerform(src, players, action, tree.All(), args[0] != "grant")
	}
	mode, ok := advancementModes[rest[0]]
	if !ok || len(rest) < 2 {
		src.Debug("advancement command does not parse", "mode", rest[0])
		return 0, nil
	}
	id, _, _ := readIdentifier(rest[1])
	n := tree.Get(id)
	if n == nil {
		return 0, refuse(src, advancementNotFoundKey, protocol.Text{Text: id})
	}
	if len(rest) > 2 && rest[0] == "only" {
		return advancementCriterion(src, players, action, n, strings.Join(rest[2:], " "))
	}
	var nodes []*advancement.Node
	if mode.parents {
		for p := n.ParentNode; p != nil; p = p.ParentNode {
			nodes = append(nodes, p)
		}
	}
	nodes = append(nodes, n)
	if mode.children {
		nodes = appendDescendants(nodes, n)
	}
	return advancementPerform(src, players, action, nodes, true)
}

func appendDescendants(out []*advancement.Node, n *advancement.Node) []*advancement.Node {
	for _, child := range n.Children {
		out = append(out, child)
		out = appendDescendants(out, child)
	}
	return out
}

func advancementPerform(src Source, players []Target, action advancementAction, nodes []*advancement.Node, show bool) (int32, error) {
	advancementCount, playerCount := 0, 0
	// Held until the feedback is sent: a tick's flush in between would send
	// the changes with show advancements set, or ahead of the feedback.
	var releases []func()
	defer func() {
		for _, release := range releases {
			release()
		}
	}()
	for _, player := range players {
		progress := player.Advancements()
		if progress == nil {
			continue
		}
		releases = append(releases, progress.Hold())
		if !show {
			player.FlushAdvancements(true)
		}
		changed := 0
		for _, n := range nodes {
			if action.perform(progress, n) {
				changed++
			}
		}
		player.AnnounceAdvancements()
		if !show {
			player.FlushAdvancements(false)
		}
		if changed > 0 {
			playerCount++
		}
		advancementCount += changed
	}
	oneAdvancement := len(nodes) == 1
	onePlayer := len(players) == 1
	if advancementCount == 0 {
		var key string
		var with []protocol.Text
		switch {
		case oneAdvancement && onePlayer:
			key, with = "one.to.one", []protocol.Text{AdvancementName(nodes[0]), players[0].DisplayName()}
		case oneAdvancement:
			key, with = "one.to.many", []protocol.Text{AdvancementName(nodes[0]), entityCount(len(players))}
		case onePlayer:
			key, with = "many.to.one", []protocol.Text{entityCount(len(nodes)), players[0].DisplayName()}
		default:
			key, with = "many.to.many", []protocol.Text{entityCount(len(nodes)), entityCount(len(players))}
		}
		return 0, refuse(src, action.key+key+".failure", with...)
	}
	var key string
	var with []protocol.Text
	switch {
	case oneAdvancement && onePlayer:
		key, with = "one.to.one", []protocol.Text{AdvancementName(nodes[0]), players[0].DisplayName()}
	case oneAdvancement:
		key, with = "one.to.many", []protocol.Text{AdvancementName(nodes[0]), entityCount(playerCount)}
	case onePlayer:
		key, with = "many.to.one", []protocol.Text{entityCount(len(nodes)), players[0].DisplayName()}
	default:
		key, with = "many.to.many", []protocol.Text{entityCount(len(nodes)), entityCount(playerCount)}
	}
	if err := feedbackToAdmins(src, action.key+key+".success", with...); err != nil {
		return 0, err
	}
	return int32(advancementCount), nil
}

func advancementCriterion(src Source, players []Target, action advancementAction, n *advancement.Node, criterion string) (int32, error) {
	if !n.HasCriterion(criterion) {
		return 0, refuse(src, advancementCriterionNotFoundKey, AdvancementName(n), protocol.Text{Text: criterion})
	}
	playerCount := 0
	for _, player := range players {
		if progress := player.Advancements(); progress != nil && action.performCriterion(progress, n, criterion) {
			playerCount++
			player.AnnounceAdvancements()
		}
	}
	name, crit := AdvancementName(n), protocol.Text{Text: criterion}
	if playerCount == 0 {
		if len(players) == 1 {
			return 0, refuse(src, action.key+"criterion.to.one.failure", crit, name, players[0].DisplayName())
		}
		return 0, refuse(src, action.key+"criterion.to.many.failure", crit, name, entityCount(len(players)))
	}
	var err error
	if len(players) == 1 {
		err = feedbackToAdmins(src, action.key+"criterion.to.one.success", crit, name, players[0].DisplayName())
	} else {
		err = feedbackToAdmins(src, action.key+"criterion.to.many.success", crit, name, entityCount(playerCount))
	}
	return int32(playerCount), err
}
