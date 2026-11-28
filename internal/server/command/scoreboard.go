package command

import (
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/scoreboard"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// Scoreboard is the server's scoreboard as the /scoreboard command turns it.
// Each change reaches the players who see the objective as it is made.
type Scoreboard interface {
	Objective(name string, instance ...scoreboard.InstanceID) (scoreboard.Objective, bool)
	Objectives() []scoreboard.Objective
	AddObjective(name string, criteria scoreboard.Criteria, displayName nbt.Tag) bool
	RemoveObjective(name string, instance ...scoreboard.InstanceID) (int, bool)
	SetDisplayName(name string, displayName nbt.Tag, instance ...scoreboard.InstanceID)
	SetRenderType(name string, render scoreboard.RenderType, instance ...scoreboard.InstanceID)
	SetAutoUpdate(name string, autoUpdate bool)
	SetObjectiveFormat(name string, format *scoreboard.NumberFormat)
	Displayed(slot scoreboard.DisplaySlot) (string, bool)
	SetDisplay(slot scoreboard.DisplaySlot, name string, instance ...scoreboard.InstanceID)

	Score(holder, objective string, instance ...scoreboard.InstanceID) (scoreboard.Score, bool)
	Update(holders []scoreboard.Holder, objective string, change func(int32) int32) int64
	Store(holders []scoreboard.Holder, objective string, value int32, instance ...scoreboard.InstanceID) error
	Operation(targets []scoreboard.Holder, targetObjective string, sources []scoreboard.Holder, sourceObjective string,
		swap bool, op func(a, b int32) (int32, int32, error)) (int64, error)
	Enable(holders []scoreboard.Holder, objective string) int
	Trigger(holder scoreboard.Holder, objective string, change func(int32) int32) (int32, bool)
	SetScoreDisplay(holders []scoreboard.Holder, objective string, display nbt.Tag)
	SetScoreFormat(holders []scoreboard.Holder, objective string, format *scoreboard.NumberFormat, instance ...scoreboard.InstanceID)
	ResetAll(holders []string)
	Reset(holders []string, objective string)
	Holders() []string
	HolderScores(holder string) []scoreboard.HolderScore

	Team(name string, instance ...scoreboard.InstanceID) (scoreboard.Team, bool)
	Teams() []scoreboard.Team
	PlayerTeam(player string) (string, bool)
	MemberTeam(holder string) (scoreboard.Team, bool)
	AddTeam(name string, displayName nbt.Tag) bool
	RemoveTeam(name string, instance ...scoreboard.InstanceID) bool
	UpdateTeam(name string, change func(*scoreboard.Team), instance ...scoreboard.InstanceID)
	JoinTeam(team string, players []string, instance ...scoreboard.InstanceID)
	LeaveTeam(players []string, instance ...scoreboard.InstanceID) []bool
	EmptyTeam(team string) int
}

// ScoreboardCommand is the /scoreboard command.
// vanilla: ScoreboardCommand
var ScoreboardCommand = Command{
	Name:   "scoreboard",
	Level:  PermLevelGamemasters,
	Build:  scoreboardNode,
	Run:    discardResult(runScoreboard),
	Result: runScoreboard,
}

const (
	objectiveReadOnlyKey      = "arguments.objective.readonly"
	scoreHolderEmptyKey       = "argument.scoreHolder.empty"
	criteriaInvalidKey        = "argument.criteria.invalid"
	slotInvalidKey            = "argument.scoreboardDisplaySlot.invalid"
	operationInvalidKey       = "arguments.operation.invalid"
	operationDivZeroKey       = "arguments.operation.div0"
	componentInvalidKey       = "argument.component.invalid"
	styleInvalidKey           = "argument.style.invalid"
	scoreboardKey             = "commands.scoreboard."
	squareBracketsKey         = "chat.square_brackets"
	scoreboardOperationsUsage = "= += -= *= /= %= < > ><"
)

// scoreboardOperations are the operators /scoreboard players operation reads.
// vanilla: OperationArgument
var scoreboardOperations = strings.Fields(scoreboardOperationsUsage)

func scoreboardNode() *Node {
	objective := func(name string, children ...*Node) *Node {
		return &Node{Type: NodeArgument, Name: name, Parser: ParserObjective,
			Suggests: AskServerSuggestion, Completions: objectiveCompletions, Children: children}
	}
	holders := func(name string, children ...*Node) *Node {
		n := scoreHolderArg(name, true)
		n.Suggests, n.Completions, n.Children = AskServerSuggestion, holderCompletions, children
		return n
	}
	holder := func(name string, children ...*Node) *Node {
		n := scoreHolderArg(name, false)
		n.Suggests, n.Completions, n.Children = AskServerSuggestion, holderCompletions, children
		return n
	}
	exec := func(n *Node) *Node { n.Executable = true; return n }
	lit := func(name string, children ...*Node) *Node {
		return &Node{Type: NodeLiteral, Name: name, Children: children}
	}
	score := func(min bool) *Node {
		n := &Node{Type: NodeArgument, Name: "score", Parser: ParserInteger, Executable: true, Int: &IntArg{}}
		if min {
			n.Int = &IntArg{HasMin: true}
		}
		return n
	}

	renderTypes := lit("rendertype", exec(lit("integer")), exec(lit("hearts")))
	objectives := lit("objectives",
		exec(lit("list")),
		lit("add", &Node{Type: NodeArgument, Name: "objective", Parser: ParserString,
			Properties: func(b *protocol.Buffer) { b.PutVarInt(0) }, Children: []*Node{
				exec(&Node{Type: NodeArgument, Name: "criteria", Parser: ParserObjectiveCriteria, Check: checkCriteria,
					Suggests: AskServerSuggestion, Completions: criteriaCompletions,
					Children: []*Node{exec(componentArg("displayName"))}}),
			}}),
		lit("modify", objective("objective",
			lit("displayname", exec(componentArg("displayName"))),
			renderTypes,
			lit("displayautoupdate", exec(&Node{Type: NodeArgument, Name: "value", Parser: ParserBool})),
			numberFormats(exec(lit("numberformat"))),
		)),
		lit("remove", exec(objective("objective"))),
		lit("setdisplay", exec(&Node{Type: NodeArgument, Name: "slot", Parser: ParserScoreboardSlot, Check: checkSlot,
			Suggests: AskServerSuggestion, Completions: slotCompletions,
			Children: []*Node{exec(objective("objective"))}})),
	)
	operation := &Node{Type: NodeArgument, Name: "operation", Parser: ParserOperation, Check: checkOperation,
		Suggests: AskServerSuggestion, Completions: operationCompletions,
		Children: []*Node{holders("source", exec(objective("sourceObjective")))}}
	players := lit("players",
		exec(lit("list", exec(holder("target")))),
		lit("set", holders("targets", objective("objective", score(false)))),
		lit("get", holder("target", exec(objective("objective")))),
		lit("add", holders("targets", objective("objective", score(true)))),
		lit("remove", holders("targets", objective("objective", score(true)))),
		lit("reset", exec(holders("targets", exec(objective("objective"))))),
		lit("enable", holders("targets", exec(&Node{Type: NodeArgument, Name: "objective", Parser: ParserObjective,
			Suggests: AskServerSuggestion, Completions: triggerCompletions}))),
		lit("display",
			lit("name", holders("targets", exec(objective("objective", exec(componentArg("name")))))),
			lit("numberformat", holders("targets", numberFormats(exec(objective("objective"))))),
		),
		lit("operation", holders("targets", objective("targetObjective", operation))),
	)
	return &Node{Type: NodeLiteral, Name: "scoreboard", Required: PermLevelGamemasters,
		Children: []*Node{objectives, players}}
}

// componentArg is a text component argument, read as one SNBT value. It has
// no custom suggestion provider, so the client is not told to ask the server.
func componentArg(name string) *Node {
	return &Node{Type: NodeArgument, Name: name, Parser: ParserComponent, Greedy: true, Check: checkComponentArg}
}

// numberFormats hangs the blank, fixed and styled branches off top.
// vanilla: ScoreboardCommand.addNumberFormats
func numberFormats(top *Node) *Node {
	top.Children = append(top.Children,
		&Node{Type: NodeLiteral, Name: "blank", Executable: true},
		&Node{Type: NodeLiteral, Name: "fixed", Children: []*Node{
			componentArgExec("contents")}},
		&Node{Type: NodeLiteral, Name: "styled", Children: []*Node{
			{Type: NodeArgument, Name: "style", Parser: ParserStyle, Greedy: true, NbtValue: true, Executable: true}}},
	)
	return top
}

// checkCriteria refuses a criteria no objective counts, at the word's start.
// vanilla: ObjectiveCriteriaArgument.parse
func checkCriteria(word string) *ParseError {
	if _, ok := scoreboard.LookupCriteria(word); ok {
		return nil
	}
	return &ParseError{Key: criteriaInvalidKey, With: []string{word}}
}

// checkSlot refuses a display slot that is none, at the word's end.
// vanilla: ScoreboardSlotArgument.parse
func checkSlot(word string) *ParseError {
	if _, ok := scoreboard.LookupSlot(word); ok {
		return nil
	}
	return &ParseError{Key: slotInvalidKey, With: []string{word}, Cursor: len(word)}
}

// checkOperation refuses an operator that is none; vanilla's create() carries
// no input, so neither does the refusal.
// vanilla: OperationArgument.parse
func checkOperation(word string) *ParseError {
	if slices.Contains(scoreboardOperations, word) {
		return nil
	}
	return &ParseError{Key: operationInvalidKey, NoContext: true}
}

func objectiveCompletions(ctx any, partial string) (int, []string) {
	src, ok := ctx.(Source)
	if !ok {
		return 0, nil
	}
	var names []string
	for _, o := range src.Scoreboard().Objectives() {
		names = append(names, o.Name)
	}
	return 0, Suggestions(partial, names...)
}

// triggerCompletions offers the trigger objectives. Vanilla leaves out the
// ones every target already has unlocked; this offers them all.
func triggerCompletions(ctx any, partial string) (int, []string) {
	src, ok := ctx.(Source)
	if !ok {
		return 0, nil
	}
	var names []string
	for _, o := range src.Scoreboard().Objectives() {
		if o.Criteria.Name == "trigger" {
			names = append(names, o.Name)
		}
	}
	return 0, Suggestions(partial, names...)
}

func criteriaCompletions(_ any, partial string) (int, []string) {
	return 0, Suggestions(partial, scoreboard.CriteriaNames()...)
}

func slotCompletions(_ any, partial string) (int, []string) {
	return 0, Suggestions(partial, scoreboard.SlotNames...)
}

func operationCompletions(_ any, partial string) (int, []string) {
	return 0, Suggestions(partial, scoreboardOperations...)
}

// holderCompletions offers the online players' names.
// vanilla: ScoreHolderArgument.SUGGEST_SCORE_HOLDERS
func holderCompletions(ctx any, partial string) (int, []string) {
	src, ok := ctx.(Source)
	if !ok {
		return 0, nil
	}
	var names []string
	for _, p := range src.OnlinePlayers() {
		names = append(names, p.Name)
	}
	return 0, Suggestions(partial, names...)
}

// scoreboardRun is one /scoreboard run: the source, the input the
// component arguments are read back out of, and the words after
// "scoreboard".
type scoreboardRun struct {
	src    Source
	board  Scoreboard
	input  string
	tokens []Token
	args   []string
}

// arg is the word at i, "" past the end.
func (r *scoreboardRun) arg(i int) string {
	if i < len(r.args) {
		return r.args[i]
	}
	return ""
}

func runScoreboard(src Source, input string, args []string) (int32, error) {
	if len(args) < 2 {
		src.Debug("incomplete scoreboard command")
		return 0, nil
	}
	r := &scoreboardRun{src: src, board: src.Scoreboard(), input: input, tokens: Tokenize(input), args: args}
	switch args[0] + " " + args[1] {
	case "objectives list":
		return r.listObjectives()
	case "objectives add":
		return r.addObjective()
	case "objectives modify":
		return r.modifyObjective()
	case "objectives remove":
		return r.removeObjective()
	case "objectives setdisplay":
		return r.setDisplay()
	case "players list":
		return r.listPlayers()
	case "players set", "players add", "players remove":
		return r.changeScores(args[1])
	case "players get":
		return r.getScore()
	case "players reset":
		return r.resetScores()
	case "players enable":
		return r.enable()
	case "players display":
		return r.display()
	case "players operation":
		return r.operation()
	}
	src.Debug("unknown scoreboard subcommand", "args", args)
	return 0, nil
}

// feedback is a sendSuccess under commands.scoreboard.; admins hear it when
// broadcast is set.
func (r *scoreboardRun) feedback(broadcast bool, key string, with ...protocol.Text) error {
	if broadcast {
		return feedbackToAdmins(r.src, scoreboardKey+key, with...)
	}
	return r.src.Feedback(scoreboardKey+key, with...)
}

// objective looks the named objective up, refusing one that does not exist
// and, when writable is set, one only the game may write.
// vanilla: ObjectiveArgument.getObjective, getWritableObjective
func (r *scoreboardRun) objective(name string, writable bool) (scoreboard.Objective, error) {
	o, ok := r.board.Objective(name)
	if !ok {
		return o, refuse(r.src, objectiveNotFoundKey, protocol.Text{Text: name})
	}
	if writable && o.Criteria.ReadOnly {
		return o, refuse(r.src, objectiveReadOnlyKey, protocol.Text{Text: name})
	}
	return o, nil
}

// formatted is an objective's name as feedback shows it, its display name in
// square brackets. Vanilla also hovers the objective's name over it, which
// protocol.Text cannot carry.
// vanilla: Objective.createFormattedDisplayName
func formatted(o scoreboard.Objective) protocol.Text {
	return protocol.Text{Translate: squareBracketsKey, With: []protocol.Text{displayText(o.DisplayName)}}
}

// displayText is a stored component as feedback shows it.
func displayText(tag nbt.Tag) protocol.Text {
	if tag == nil {
		return protocol.Text{Text: ""}
	}
	text, ok := protocol.StyledTextOf(tag)
	if !ok {
		return protocol.Text{Text: ""}
	}
	return text
}

// formatList joins texts with a gray ", ".
// vanilla: ComponentUtils.formatList
func formatList(texts []protocol.Text) protocol.Text {
	if len(texts) == 1 {
		return texts[0]
	}
	extra := make([]protocol.Text, 0, len(texts)*2)
	for i, text := range texts {
		if i > 0 {
			extra = append(extra, protocol.Text{Text: ", ", Color: protocol.ColorGray})
		}
		extra = append(extra, text)
	}
	return protocol.Text{Text: "", Extra: extra}
}

// rest is the input from the word at args index i on, the text a component
// argument read.
func (r *scoreboardRun) rest(i int) (string, int) {
	// tokens[0] is "scoreboard", so args[i] is tokens[i+1].
	if i+1 >= len(r.tokens) {
		return "", len(r.input)
	}
	start := r.tokens[i+1].Start
	return r.input[start:], start
}

// componentInvalid is the refusal of a component argument whose text starts
// at cursor, with the shared component codec's diagnostic.
// The cursor is back at the argument's start, where the reader was reset.
// vanilla: CommandArgumentParser.withCodec
func componentInvalid(tag nbt.Tag, text string, cursor int) *ParseError {
	return &ParseError{Key: componentInvalidKey, With: []string{componentFailure(tag, text)}, Cursor: cursor}
}

// component reads the text component that starts at args index i, keeping
// it as the tag the board stores and sends.
// vanilla: ComponentArgument
func (r *scoreboardRun) component(i int) (nbt.Tag, protocol.Text, error) {
	text, start := r.rest(i)
	tag, ok, perr := parseComponentArgument(text)
	if perr != nil && perr.Key != "" {
		return nil, protocol.Text{}, nbtParseError(perr, start)
	}
	if !ok {
		return nil, protocol.Text{}, componentInvalid(tag, text, start)
	}
	component, _ := protocol.RawTextOf(tag)
	return protocol.TextTag(component), component, nil
}

// numberFormat reads the number format whose literal is at args index i: nil
// when there is none.
// vanilla: ScoreboardCommand.addNumberFormats
func (r *scoreboardRun) numberFormat(i int) (*scoreboard.NumberFormat, error) {
	switch r.arg(i) {
	case "blank":
		return &scoreboard.NumberFormat{Kind: scoreboard.FormatBlank}, nil
	case "fixed":
		tag, _, err := r.component(i + 1)
		if err != nil {
			return nil, err
		}
		return &scoreboard.NumberFormat{Kind: scoreboard.FormatFixed, Fixed: tag}, nil
	case "styled":
		text, start := r.rest(i + 1)
		_, tag := readNBTValue(text)
		style, message := registry.ParseTextStyle(tag)
		if message != "" {
			return nil, &ParseError{Key: styleInvalidKey, With: []string{message}, Cursor: start}
		}

		return &scoreboard.NumberFormat{Kind: scoreboard.FormatStyled, Style: style}, nil
	}
	return nil, nil
}

func (r *scoreboardRun) listObjectives() (int32, error) {
	objectives := r.board.Objectives()
	if len(objectives) == 0 {
		return int32(len(objectives)), r.feedback(false, "objectives.list.empty")
	}
	names := make([]protocol.Text, len(objectives))
	for i, o := range objectives {
		names[i] = formatted(o)
	}
	return int32(len(objectives)), r.feedback(false, "objectives.list.success", entityCount(len(objectives)), formatList(names))
}

func (r *scoreboardRun) addObjective() (int32, error) {
	name := r.arg(2)
	criteria, ok := scoreboard.LookupCriteria(r.arg(3))
	if name == "" || !ok {
		r.src.Debug("incomplete scoreboard objectives add")
		return 0, nil
	}
	var display nbt.Tag = nbt.String(name)
	if len(r.args) > 4 {
		tag, _, err := r.component(4)
		if err != nil {
			return 0, err
		}
		display = tag
	}
	if !r.board.AddObjective(name, criteria, display) {
		return 0, refuse(r.src, scoreboardKey+"objectives.add.duplicate")
	}
	o, _ := r.board.Objective(name)
	return int32(len(r.board.Objectives())), r.feedback(true, "objectives.add.success", formatted(o))
}

func (r *scoreboardRun) modifyObjective() (int32, error) {
	o, err := r.objective(r.arg(2), false)
	if err != nil {
		return 0, err
	}
	name := protocol.Text{Text: o.Name}
	switch r.arg(3) {
	case "displayname":
		tag, _, err := r.component(4)
		if err != nil {
			return 0, err
		}
		if nbt.Equal(tag, o.DisplayName) {
			return 0, nil
		}
		r.board.SetDisplayName(o.Name, tag)
		o, _ = r.board.Objective(o.Name)
		return 0, r.feedback(true, "objectives.modify.displayname", name, formatted(o))
	case "rendertype":
		render, ok := scoreboard.ParseRenderType(r.arg(4))
		if !ok || render == o.Render {
			return 0, nil
		}
		r.board.SetRenderType(o.Name, render)
		return 0, r.feedback(true, "objectives.modify.rendertype", formatted(o))
	case "displayautoupdate":
		value := r.arg(4) == "true"
		if value == o.AutoUpdate {
			return 0, nil
		}
		r.board.SetAutoUpdate(o.Name, value)
		key := "objectives.modify.displayAutoUpdate.disable"
		if value {
			key = "objectives.modify.displayAutoUpdate.enable"
		}
		return 0, r.feedback(true, key, name, formatted(o))
	case "numberformat":
		format, err := r.numberFormat(4)
		if err != nil {
			return 0, err
		}
		r.board.SetObjectiveFormat(o.Name, format)
		if format == nil {
			return 0, r.feedback(true, "objectives.modify.objectiveFormat.clear", name)
		}
		return 0, r.feedback(true, "objectives.modify.objectiveFormat.set", name)
	}
	r.src.Debug("unknown scoreboard objectives modify", "args", r.args)
	return 0, nil
}

func (r *scoreboardRun) removeObjective() (int32, error) {
	o, err := r.objective(r.arg(2), false)
	if err != nil {
		return 0, err
	}
	r.board.RemoveObjective(o.Name)
	return int32(len(r.board.Objectives())), r.feedback(true, "objectives.remove.success", formatted(o))
}

func (r *scoreboardRun) setDisplay() (int32, error) {
	slotName := r.arg(2)
	slot, ok := scoreboard.LookupSlot(slotName)
	if !ok {
		r.src.Debug("unknown display slot", "slot", slotName)
		return 0, nil
	}
	shown, isShown := r.board.Displayed(slot)
	if len(r.args) < 4 {
		if !isShown {
			return 0, refuse(r.src, scoreboardKey+"objectives.display.alreadyEmpty")
		}
		r.board.SetDisplay(slot, "")
		return 0, r.feedback(true, "objectives.display.cleared", protocol.Text{Text: slotName})
	}
	o, err := r.objective(r.arg(3), false)
	if err != nil {
		return 0, err
	}
	if isShown && shown == o.Name {
		return 0, refuse(r.src, scoreboardKey+"objectives.display.alreadySet")
	}
	r.board.SetDisplay(slot, o.Name)
	return 0, r.feedback(true, "objectives.display.set", protocol.Text{Text: slotName}, displayText(o.DisplayName))
}

// holder is one resolved score holder: the board's side and, for an entity,
// the entity whose display name feedback shows.
type holder struct {
	scoreboard.Holder
	entity Entity
}

// feedback is the name feedback shows, read when the feedback is built so it
// carries a team the command has just joined or left. An entity's name hovers
// its scoreboard name in place of the entity.
// vanilla: ScoreHolder.getFeedbackDisplayName
func (h holder) feedback() protocol.Text {
	if h.entity != nil {
		name := h.entity.DisplayName()
		name.HoverEntity, name.HoverItem = nil, nil
		name.HoverText = &protocol.Text{Text: h.Name}
		return name
	}
	return protocol.Text{Text: h.Name}
}

// holders resolves the score-holder argument whose first word is at args
// index i, answering how many words it spanned. multiple is a holders
// argument, whose * names every holder the board knows.
// vanilla: ScoreHolderArgument.parse, getNamesWithDefaultWildcard
func (r *scoreboardRun) holders(i int, multiple bool) ([]holder, int, error) {
	word, span := selectorWordIn(r.input, r.args, i)
	if strings.HasPrefix(word, "@") {
		entities, err := resolveEntities(r.src, word)
		if err != nil {
			return nil, span, err
		}
		out := make([]holder, len(entities))
		for j, e := range entities {
			out[j] = entityHolder(e)
		}
		return out, span, nil
	}
	out, key := scoreHolders(r.src, r.board, word, multiple)
	if key != "" {
		return nil, span, refuse(r.src, key)
	}
	return out, span, nil
}

// scoreHolders resolves a score-holder word, answering the key of the
// refusal it throws instead of sending it, for an execute step to gate.
func scoreHolders(src Source, board Scoreboard, word string, multiple bool) ([]holder, string) {
	if word == "*" {
		var out []holder
		if multiple {
			for _, name := range board.Holders() {
				out = append(out, nameOnly(name))
			}
		}
		if len(out) == 0 {
			return nil, scoreHolderEmptyKey
		}
		return out, ""
	}
	if strings.HasPrefix(word, "@") {
		entities, key := resolveRequiredEntities(src, word)
		out := make([]holder, len(entities))
		for j, e := range entities {
			out[j] = entityHolder(e)
		}
		return out, key
	}
	if strings.HasPrefix(word, "#") {
		return []holder{nameOnly(word)}, ""
	}
	if id, err := uuid.Parse(word); err == nil && strings.Count(word, "-") == 4 {
		var out []holder
		for _, e := range src.Entities(EntityQuery{}) {
			if e.UUID() == id {
				out = append(out, entityHolder(e))
			}
		}
		if len(out) > 0 {
			return out, ""
		}
		return []holder{nameOnly(word)}, ""
	}
	for _, t := range src.OnlineTargets() {
		if strings.EqualFold(t.Name(), word) {
			return []holder{entityHolder(t)}, ""
		}
	}
	return []holder{nameOnly(word)}, ""
}

func nameOnly(name string) holder {
	return holder{Name: name}
}

// entityHolder is an entity as a score holder: a player by name, anything
// else by its UUID.
// vanilla: Entity.getScoreboardName
func entityHolder(e Entity) holder {
	return holder{Name: scoreHolderName(e), entity: e,
		Display: func() nbt.Tag { return protocol.TextTag(e.DisplayName()) }}
}

// scoreHolderName is an entity's scoreboard name: a player's name, anything
// else's UUID.
// vanilla: Entity.getScoreboardName
func scoreHolderName(e Entity) string {
	if t, ok := e.(Target); ok {
		return t.Name()
	}
	return e.UUID().String()
}

func boardHolders(hs []holder) []scoreboard.Holder {
	out := make([]scoreboard.Holder, len(hs))
	for i, h := range hs {
		out[i] = h.Holder
	}
	return out
}

func holderNames(hs []holder) []string {
	out := make([]string, len(hs))
	for i, h := range hs {
		out[i] = h.Name
	}
	return out
}

// countOrName is what a .single/.multiple feedback pair names the holders
// by: the one holder's name, or how many there are.
func countOrName(hs []holder) (protocol.Text, string) {
	if len(hs) == 1 {
		return hs[0].feedback(), "single"
	}
	return entityCount(len(hs)), "multiple"
}

func (r *scoreboardRun) listPlayers() (int32, error) {
	if len(r.args) < 3 {
		names := r.board.Holders()
		if len(names) == 0 {
			return 0, r.feedback(false, "players.list.empty")
		}
		texts := make([]protocol.Text, len(names))
		for i, name := range names {
			texts[i] = protocol.Text{Text: name}
		}
		return int32(len(names)), r.feedback(false, "players.list.success", entityCount(len(names)), formatList(texts))
	}
	hs, _, err := r.holders(2, false)
	if err != nil {
		return 0, err
	}
	h := hs[0]
	scores := r.board.HolderScores(h.Name)
	if len(scores) == 0 {
		return 0, r.feedback(false, "players.list.entity.empty", h.feedback())
	}
	if err := r.feedback(false, "players.list.entity.success", h.feedback(), entityCount(len(scores))); err != nil {
		return 0, err
	}
	for _, s := range scores {
		if err := r.feedback(false, "players.list.entity.entry", formatted(s.Objective), entityCount(int(s.Value))); err != nil {
			return 0, err
		}
	}
	return int32(len(scores)), nil
}

// changeScores runs players set, add and remove.
func (r *scoreboardRun) changeScores(verb string) (int32, error) {
	if !wantsFeedback(r.src) {
		return r.changeScoresQuietly(verb)
	}
	hs, span, err := r.holders(2, true)
	if err != nil {
		return 0, err
	}
	o, err := r.objective(r.arg(2+span), true)
	if err != nil {
		return 0, err
	}
	value, err := strconv.ParseInt(r.arg(3+span), 10, 32)
	if err != nil {
		r.src.Debug("scoreboard score is no integer", "word", r.arg(3+span))
		return 0, nil
	}
	v := int32(value)
	var sum int64
	total := int32(0)
	switch verb {
	case "set":
		sum = r.board.Update(boardHolders(hs), o.Name, func(int32) int32 { return v })
		total = v * int32(len(hs))
	case "add":
		sum = r.board.Update(boardHolders(hs), o.Name, func(old int32) int32 { return old + v })
	case "remove":
		sum = r.board.Update(boardHolders(hs), o.Name, func(old int32) int32 { return old - v })
	}
	if verb != "set" {
		total = int32(sum) // Java's int sum wraps
	}
	if !wantsFeedback(r.src) {
		return total, nil
	}
	number := entityCount(int(v))
	if verb == "set" {
		// vanilla: ScoreboardCommand.setScore (NON_ZERO feedback)
		var response responseTracker[holder]
		for _, h := range hs {
			response.track(h, int(v))
		}
		form, who := response.target(holder.feedback)
		return total, r.feedback(true, "players.set.success."+form, formatted(o), who, number)
	}
	who, form := countOrName(hs)
	if form == "single" {
		return total, r.feedback(true, "players."+verb+".success.single", number, formatted(o), who, entityCount(int(int32(sum))))
	}
	return total, r.feedback(true, "players."+verb+".success.multiple", number, formatted(o), who)
}

// changeScoresQuietly is changeScores for a run nobody reads the feedback
// of: the board's holders are built straight from the selector's entities.
func (r *scoreboardRun) changeScoresQuietly(verb string) (int32, error) {
	word, span := selectorWordIn(r.input, r.args, 2)
	var hs []scoreboard.Holder
	if strings.HasPrefix(word, "@") {
		entities, err := resolveEntities(r.src, word)
		if err != nil {
			return 0, err
		}
		hs = make([]scoreboard.Holder, len(entities))
		for j, e := range entities {
			hs[j] = entityHolder(e).Holder
		}
	} else {
		named, key := scoreHolders(r.src, r.board, word, true)
		if key != "" {
			return 0, refuse(r.src, key)
		}
		hs = boardHolders(named)
	}
	o, err := r.objective(r.arg(2+span), true)
	if err != nil {
		return 0, err
	}
	value, err := strconv.ParseInt(r.arg(3+span), 10, 32)
	if err != nil {
		r.src.Debug("scoreboard score is no integer", "word", r.arg(3+span))
		return 0, nil
	}
	v := int32(value)
	switch verb {
	case "set":
		r.board.Update(hs, o.Name, func(int32) int32 { return v })
		return v * int32(len(hs)), nil
	case "add":
		return int32(r.board.Update(hs, o.Name, func(old int32) int32 { return old + v })), nil
	}
	return int32(r.board.Update(hs, o.Name, func(old int32) int32 { return old - v })), nil
}

func (r *scoreboardRun) getScore() (int32, error) {
	hs, span, err := r.holders(2, false)
	if err != nil {
		return 0, err
	}
	o, err := r.objective(r.arg(2+span), false)
	if err != nil {
		return 0, err
	}
	h := hs[0]
	score, ok := r.board.Score(h.Name, o.Name)
	if !ok {
		return 0, refuse(r.src, scoreboardKey+"players.get.null", protocol.Text{Text: o.Name}, h.feedback())
	}
	if !wantsFeedback(r.src) {
		return score.Value, nil
	}
	return score.Value, r.feedback(false, "players.get.success", h.feedback(), entityCount(int(score.Value)), formatted(o))
}

func (r *scoreboardRun) resetScores() (int32, error) {
	hs, span, err := r.holders(2, true)
	if err != nil {
		return 0, err
	}
	if len(r.args) <= 2+span {
		r.board.ResetAll(holderNames(hs))
		if !wantsFeedback(r.src) {
			return int32(len(hs)), nil
		}
		who, form := countOrName(hs)
		return int32(len(hs)), r.feedback(true, "players.reset.all."+form, who)
	}
	o, err := r.objective(r.arg(2+span), false)
	if err != nil {
		return 0, err
	}
	r.board.Reset(holderNames(hs), o.Name)
	if !wantsFeedback(r.src) {
		return int32(len(hs)), nil
	}
	who, form := countOrName(hs)
	return int32(len(hs)), r.feedback(true, "players.reset.specific."+form, formatted(o), who)
}

func (r *scoreboardRun) enable() (int32, error) {
	hs, span, err := r.holders(2, true)
	if err != nil {
		return 0, err
	}
	o, err := r.objective(r.arg(2+span), false)
	if err != nil {
		return 0, err
	}
	if o.Criteria.Name != "trigger" {
		return 0, refuse(r.src, scoreboardKey+"players.enable.invalid")
	}
	count := int32(r.board.Enable(boardHolders(hs), o.Name))
	if count == 0 {
		return 0, refuse(r.src, scoreboardKey+"players.enable.failed")
	}
	if !wantsFeedback(r.src) {
		return count, nil
	}
	who, form := countOrName(hs)
	return count, r.feedback(true, "players.enable.success."+form, formatted(o), who)
}

// display runs players display name and players display numberformat.
func (r *scoreboardRun) display() (int32, error) {
	kind := r.arg(2)
	hs, span, err := r.holders(3, true)
	if err != nil {
		return 0, err
	}
	o, err := r.objective(r.arg(3+span), false)
	if err != nil {
		return 0, err
	}
	at := 4 + span
	who, form := countOrName(hs)
	switch kind {
	case "name":
		if len(r.args) <= at {
			r.board.SetScoreDisplay(boardHolders(hs), o.Name, nil)
			return int32(len(hs)), r.feedback(true, "players.display.name.clear.success."+form, who, formatted(o))
		}
		tag, text, err := r.component(at)
		if err != nil {
			return 0, err
		}
		r.board.SetScoreDisplay(boardHolders(hs), o.Name, tag)
		return int32(len(hs)), r.feedback(true, "players.display.name.set.success."+form, text, who, formatted(o))
	case "numberformat":
		format, err := r.numberFormat(at)
		if err != nil {
			return 0, err
		}
		r.board.SetScoreFormat(boardHolders(hs), o.Name, format)
		if format == nil {
			return int32(len(hs)), r.feedback(true, "players.display.numberFormat.clear.success."+form, who, formatted(o))
		}
		return int32(len(hs)), r.feedback(true, "players.display.numberFormat.set.success."+form, who, formatted(o))
	}
	r.src.Debug("unknown scoreboard players display", "kind", kind)
	return 0, nil
}

// operation runs players operation.
// vanilla: ScoreboardCommand.performOperation, OperationArgument
func (r *scoreboardRun) operation() (int32, error) {
	targets, span, err := r.holders(2, true)
	if err != nil {
		return 0, err
	}
	at := 2 + span
	target, err := r.objective(r.arg(at), true)
	if err != nil {
		return 0, err
	}
	op := r.arg(at + 1)
	sources, sourceSpan, err := r.holders(at+2, true)
	if err != nil {
		return 0, err
	}
	source, err := r.objective(r.arg(at+2+sourceSpan), false)
	if err != nil {
		return 0, err
	}
	sum, err := r.board.Operation(boardHolders(targets), target.Name, boardHolders(sources), source.Name,
		op == "><", func(a, b int32) (int32, int32, error) { return applyOperation(op, a, b) })
	if errors.Is(err, scoreboard.ErrReadOnlyScore) {
		// vanilla: Commands.performCommand answers an exception with command.failed
		return 0, refuse(r.src, "command.failed")
	}
	if err != nil {
		return 0, refuse(r.src, operationDivZeroKey)
	}
	total := int32(sum)
	if !wantsFeedback(r.src) {
		return total, nil
	}
	who, form := countOrName(targets)
	if form == "single" {
		return total, r.feedback(true, "players.operation.success.single", formatted(target), who, entityCount(int(int32(sum))))
	}
	return total, r.feedback(true, "players.operation.success.multiple", formatted(target), who)
}

// errDivZero is a /= or %= by a zero source.
var errDivZero = &ParseError{Key: operationDivZeroKey, NoContext: true}

// applyOperation is one operator on a target score a and a source score b,
// answering both scores after it. Overflow wraps, as Java's int does.
// vanilla: OperationArgument.getSimpleOperation
func applyOperation(op string, a, b int32) (int32, int32, error) {
	switch op {
	case "=":
		return b, b, nil
	case "+=":
		return a + b, b, nil
	case "-=":
		return a - b, b, nil
	case "*=":
		return a * b, b, nil
	case "/=":
		if b == 0 {
			return a, b, errDivZero
		}
		q := a / b
		if (a%b != 0) && ((a < 0) != (b < 0)) {
			q--
		}
		return q, b, nil
	case "%=":
		if b == 0 {
			return a, b, errDivZero
		}
		m := a % b
		if m != 0 && ((m < 0) != (b < 0)) {
			m += b
		}
		return m, b, nil
	case "<":
		return min(a, b), b, nil
	case ">":
		return max(a, b), b, nil
	case "><":
		return b, a, nil
	}
	return a, b, nil
}
