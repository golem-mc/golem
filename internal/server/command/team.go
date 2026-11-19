package command

import (
	"reflect"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/scoreboard"
	"github.com/golem-mc/golem/nbt"
)

// TeamCommand is the /team command.
// vanilla: TeamCommand
var TeamCommand = Command{
	Name:   "team",
	Level:  PermLevelGamemasters,
	Build:  teamNode,
	Run:    discardResult(runTeam),
	Result: runTeam,
}

const (
	teamKey         = "commands.team."
	teamOptionKey   = teamKey + "option."
	teamNotFoundKey = "team.notFound"
)

func teamNode() *Node {
	lit := func(name string, children ...*Node) *Node {
		return &Node{Type: NodeLiteral, Name: name, Children: children}
	}
	exec := func(n *Node) *Node { n.Executable = true; return n }
	team := func(children ...*Node) *Node {
		return &Node{Type: NodeArgument, Name: "team", Parser: ParserTeam,
			Suggests: AskServerSuggestion, Completions: teamCompletions, Children: children}
	}
	members := func() *Node {
		n := scoreHolderArg("members", true)
		n.Suggests, n.Completions, n.Executable = AskServerSuggestion, holderCompletions, true
		return n
	}
	choice := func(name string, values ...string) *Node {
		n := lit(name)
		for _, v := range values {
			n.Children = append(n.Children, exec(lit(v)))
		}
		return n
	}
	toggle := func(name string) *Node {
		return lit(name, exec(&Node{Type: NodeArgument, Name: "allowed", Parser: ParserBool}))
	}
	visibilities := []string{"never", "hideForOtherTeams", "hideForOwnTeam", "always"}
	color := lit("color", exec(lit("reset")), exec(&Node{Type: NodeArgument, Name: "value", Parser: ParserTeamColor,
		Check: checkTeamColorArg, Suggests: AskServerSuggestion, Completions: teamColorCompletions}))
	modify := lit("modify", team(
		lit("displayName", exec(componentArg("displayName"))),
		color,
		toggle("friendlyFire"),
		toggle("seeFriendlyInvisibles"),
		choice("nametagVisibility", visibilities...),
		choice("deathMessageVisibility", visibilities...),
		choice("collisionRule", "never", "pushOwnTeam", "pushOtherTeams", "always"),
		lit("prefix", exec(componentArg("prefix"))),
		lit("suffix", exec(componentArg("suffix"))),
	))
	return &Node{Type: NodeLiteral, Name: "team", Required: PermLevelGamemasters, Children: []*Node{
		exec(lit("list", exec(team()))),
		lit("add", exec(&Node{Type: NodeArgument, Name: "team", Parser: ParserString,
			Properties: func(b *protocol.Buffer) { b.PutVarInt(0) },
			Children:   []*Node{exec(componentArg("displayName"))}})),
		lit("remove", exec(team())),
		lit("empty", exec(team())),
		lit("join", exec(team(members()))),
		lit("leave", members()),
		modify,
	}}
}

func teamCompletions(ctx any, partial string) (int, []string) {
	src, ok := ctx.(Source)
	if !ok {
		return 0, nil
	}
	var names []string
	for _, t := range src.Scoreboard().Teams() {
		names = append(names, t.Name)
	}
	return 0, Suggestions(partial, names...)
}

func teamColorCompletions(_ any, partial string) (int, []string) {
	return 0, Suggestions(partial, scoreboard.TeamColors()...)
}

// teamRun is one /team run, on the scoreboard run's argument reading.
type teamRun struct {
	*scoreboardRun
}

// runTeam answers what vanilla's TeamCommand handlers return: a count for
// list, add, remove, empty, join and leave, 1 for prefix and suffix, and 0
// for every other modify option.
func runTeam(src Source, input string, args []string) (int32, error) {
	if len(args) == 0 {
		src.Debug("incomplete team command")
		return 0, nil
	}
	r := &teamRun{&scoreboardRun{src: src, board: src.Scoreboard(), input: input, tokens: Tokenize(input), args: args}}
	switch args[0] {
	case "list":
		return r.list()
	case "add":
		return r.add()
	case "remove":
		return r.remove()
	case "empty":
		return r.empty()
	case "join":
		return r.join()
	case "leave":
		return r.leave()
	case "modify":
		return r.modify()
	}
	src.Debug("unknown team subcommand", "args", args)
	return 0, nil
}

// say is a sendSuccess under commands.team.; admins hear it when broadcast
// is set.
func (r *teamRun) say(broadcast bool, key string, with ...protocol.Text) error {
	if broadcast {
		return feedbackToAdmins(r.src, teamKey+key, with...)
	}
	return r.src.Feedback(teamKey+key, with...)
}

// team looks the team named by the word at args index i up.
// vanilla: TeamArgument.getTeam
func (r *teamRun) team(i int) (scoreboard.Team, error) {
	name := r.arg(i)
	t, ok := r.board.Team(name)
	if !ok {
		return t, refuse(r.src, teamNotFoundKey, protocol.Text{Text: name})
	}
	return t, nil
}

// teamName is a team's name as feedback shows it: the display name in square
// brackets, in the team's colour. Vanilla also puts the team's name on it as
// an insertion and a hover, which protocol.Text cannot carry.
// vanilla: PlayerTeam.getFormattedDisplayName
func teamName(t scoreboard.Team) protocol.Text {
	text := protocol.Text{Translate: squareBracketsKey, With: []protocol.Text{displayText(t.DisplayName)}}
	if t.Color != scoreboard.NoColor {
		text.Color = scoreboard.TeamColors()[t.Color]
	}
	return text
}

// named is the team's name as feedback shows it after a change.
func (r *teamRun) named(name string) protocol.Text {
	t, _ := r.board.Team(name)
	return teamName(t)
}

func (r *teamRun) list() (int32, error) {
	if len(r.args) > 1 {
		t, err := r.team(1)
		if err != nil {
			return 0, err
		}
		if len(t.Players) == 0 {
			return 0, r.say(false, "list.members.empty", teamName(t))
		}
		names := make([]protocol.Text, len(t.Players))
		for i, p := range t.Players {
			names[i] = protocol.Text{Text: p}
		}
		return resultOf(int32(len(names)), r.say(false, "list.members.success", teamName(t), entityCount(len(names)), formatList(names)))
	}
	teams := r.board.Teams()
	if len(teams) == 0 {
		return 0, r.say(false, "list.teams.empty")
	}
	names := make([]protocol.Text, len(teams))
	for i, t := range teams {
		names[i] = teamName(t)
	}
	return resultOf(int32(len(teams)), r.say(false, "list.teams.success", entityCount(len(teams)), formatList(names)))
}

// add answers the number of teams on the board after the new one.
func (r *teamRun) add() (int32, error) {
	name := r.arg(1)
	if name == "" {
		r.src.Debug("incomplete team add")
		return 0, nil
	}
	var display nbt.Tag = nbt.String(name)
	if len(r.args) > 2 {
		tag, _, err := r.component(2)
		if err != nil {
			return 0, err
		}
		display = tag
	}
	if !r.board.AddTeam(name, display) {
		return 0, refuse(r.src, teamKey+"add.duplicate")
	}
	return resultOf(int32(len(r.board.Teams())), r.say(true, "add.success", r.named(name)))
}

// remove answers the number of teams left on the board.
func (r *teamRun) remove() (int32, error) {
	t, err := r.team(1)
	if err != nil {
		return 0, err
	}
	r.board.RemoveTeam(t.Name)
	return resultOf(int32(len(r.board.Teams())), r.say(true, "remove.success", teamName(t)))
}

func (r *teamRun) empty() (int32, error) {
	t, err := r.team(1)
	if err != nil {
		return 0, err
	}
	if len(t.Players) == 0 {
		return 0, refuse(r.src, teamKey+"empty.unchanged")
	}
	n := r.board.EmptyTeam(t.Name)
	return resultOf(int32(n), r.say(true, "empty.success", entityCount(n), teamName(t)))
}

// join answers the number of members joined: vanilla tracks each one as
// added, since Scoreboard.addPlayerToTeam takes a member off its old team
// first.
func (r *teamRun) join() (int32, error) {
	t, err := r.team(1)
	if err != nil {
		return 0, err
	}
	var hs []holder
	if len(r.args) > 2 {
		hs, _, err = r.holders(2, true)
		if err != nil {
			return 0, err
		}
	} else {
		self := r.src.SelfEntity()
		if self == nil {
			return 0, refuse(r.src, "permissions.requires.entity")
		}
		hs = []holder{entityHolder(self)}
	}
	r.board.JoinTeam(t.Name, holderNames(hs))
	who, kind := countOrName(hs)
	return resultOf(int32(len(hs)), r.say(true, "join.success."+kind, who, teamName(t)))
}

// leave answers the number of members that were on a team.
func (r *teamRun) leave() (int32, error) {
	hs, _, err := r.holders(1, true)
	if err != nil {
		return 0, err
	}
	var tracker responseTracker[holder]
	for i, left := range r.board.LeaveTeam(holderNames(hs)) {
		tracker.trackBool(hs[i], left)
	}
	return tracker.feedback(r.src, "", teamKey+"leave.success.", holder.feedback,
		func(who protocol.Text, _ int) []protocol.Text { return []protocol.Text{who} })
}

// modify runs team modify, refusing a change that changes nothing. It
// answers 1 for prefix and suffix and 0 for every other option.
// vanilla: TeamCommand.setPrefix, setSuffix, setColor and the other setters
func (r *teamRun) modify() (int32, error) {
	t, err := r.team(1)
	if err != nil {
		return 0, err
	}
	if option := r.arg(2); option == "prefix" || option == "suffix" {
		return r.setAffix(t, option)
	}
	return 0, r.modifyOption(t)
}

// setAffix sets a team's prefix or suffix.
func (r *teamRun) setAffix(t scoreboard.Team, option string) (int32, error) {
	tag, text, err := r.component(3)
	if err != nil {
		return 0, err
	}
	r.board.UpdateTeam(t.Name, func(t *scoreboard.Team) {
		if option == "prefix" {
			t.Prefix = tag
		} else {
			t.Suffix = tag
		}
	})
	return resultOf(1, r.say(false, "option."+option+".success", text))
}

// modifyOption runs every team modify option but prefix and suffix.
func (r *teamRun) modifyOption(t scoreboard.Team) error {
	option, value := r.arg(2), r.arg(3)
	switch option {
	case "displayName":
		tag, _, err := r.component(3)
		if err != nil {
			return err
		}
		if reflect.DeepEqual(t.DisplayName, tag) {
			return refuse(r.src, teamOptionKey+"name.unchanged")
		}
		r.board.UpdateTeam(t.Name, func(t *scoreboard.Team) { t.DisplayName = tag })
		return r.say(true, "option.name.success", r.named(t.Name))
	case "color":
		return r.setColor(t, value)
	case "friendlyFire":
		return r.setToggle(t, value, "friendlyfire", t.FriendlyFire,
			func(t *scoreboard.Team, v bool) { t.FriendlyFire = v })
	case "seeFriendlyInvisibles":
		return r.setToggle(t, value, "seeFriendlyInvisibles", t.SeeFriendlyInvisible,
			func(t *scoreboard.Team, v bool) { t.SeeFriendlyInvisible = v })
	case "nametagVisibility", "deathMessageVisibility":
		return r.setVisibility(t, option, value)
	case "collisionRule":
		rule, ok := scoreboard.ParseCollisionRule(value)
		if !ok {
			break
		}
		if t.Collision == rule {
			return refuse(r.src, teamOptionKey+"collisionRule.unchanged")
		}
		r.board.UpdateTeam(t.Name, func(t *scoreboard.Team) { t.Collision = rule })
		return r.say(true, "option.collisionRule.success", r.named(t.Name),
			protocol.Text{Translate: "team.collision." + rule.String()})
	}
	r.src.Debug("unknown team modify option", "args", r.args)
	return nil
}

func (r *teamRun) setColor(t scoreboard.Team, value string) error {
	if value == "reset" {
		if t.Color == scoreboard.NoColor {
			return refuse(r.src, teamOptionKey+"color.unchanged")
		}
		r.board.UpdateTeam(t.Name, func(t *scoreboard.Team) { t.Color = scoreboard.NoColor })
		return r.say(true, "option.color.clear.success", r.named(t.Name))
	}
	color, ok := scoreboard.LookupTeamColor(value)
	if !ok {
		r.src.Debug("incomplete team modify color")
		return nil
	}
	if t.Color == color {
		return refuse(r.src, teamOptionKey+"color.unchanged")
	}
	r.board.UpdateTeam(t.Name, func(t *scoreboard.Team) { t.Color = color })
	return r.say(true, "option.color.success", r.named(t.Name), protocol.Text{Text: value})
}

// setToggle sets a team's boolean option, refusing when it already has the
// value; key is the option's name in the feedback keys.
func (r *teamRun) setToggle(t scoreboard.Team, value, key string, current bool, set func(*scoreboard.Team, bool)) error {
	if value != "true" && value != "false" {
		r.src.Debug("incomplete team modify", "option", key)
		return nil
	}
	allowed := value == "true"
	state, already := "disabled", "alreadyDisabled"
	if allowed {
		state, already = "enabled", "alreadyEnabled"
	}
	if current == allowed {
		return refuse(r.src, teamOptionKey+key+"."+already)
	}
	r.board.UpdateTeam(t.Name, func(t *scoreboard.Team) { set(t, allowed) })
	return r.say(true, "option."+key+"."+state, r.named(t.Name))
}

func (r *teamRun) setVisibility(t scoreboard.Team, option, value string) error {
	v, ok := scoreboard.ParseVisibility(value)
	if !ok {
		r.src.Debug("incomplete team modify", "option", option)
		return nil
	}
	current := t.NameTag
	if option == "deathMessageVisibility" {
		current = t.DeathMessage
	}
	if current == v {
		return refuse(r.src, teamOptionKey+option+".unchanged")
	}
	r.board.UpdateTeam(t.Name, func(t *scoreboard.Team) {
		if option == "deathMessageVisibility" {
			t.DeathMessage = v
		} else {
			t.NameTag = v
		}
	})
	return r.say(true, "option."+option+".success", r.named(t.Name),
		protocol.Text{Translate: "team.visibility." + v.String()})
}
