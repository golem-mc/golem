// Package scoreboard is the scoreboard as plain state: objectives, scores,
// display slots and the data/minecraft/scoreboard.dat form. Client-visible
// changes go to the board's listener as Events; who hears them, in what
// packet, is package server's.
package scoreboard

import (
	"errors"
	"reflect"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/golem-mc/golem/nbt"
)

// RenderType is how a client draws an objective's scores, numbered as the
// Update Objectives packet carries it.
type RenderType int32

const (
	RenderInteger RenderType = 0
	RenderHearts  RenderType = 1
)

// String is the render type's name, as the command and the saved file spell it.
func (r RenderType) String() string {
	if r == RenderHearts {
		return "hearts"
	}
	return "integer"
}

// ParseRenderType reads a render type's name.
func ParseRenderType(name string) (RenderType, bool) {
	switch name {
	case "integer":
		return RenderInteger, true
	case "hearts":
		return RenderHearts, true
	}
	return 0, false
}

// FormatKind is a number format's kind, numbered by the
// minecraft:number_format_type registry the wire carries it as.
type FormatKind int32

const (
	FormatBlank  FormatKind = 0
	FormatStyled FormatKind = 1
	FormatFixed  FormatKind = 2
)

// NumberFormat is how a score's number shows: hidden, styled (Style, the
// style's fields as a compound) or replaced (Fixed, a component tag).
type NumberFormat struct {
	Kind  FormatKind
	Style nbt.Compound
	Fixed nbt.Tag
}

// Criteria is what an objective counts; ReadOnly ones only the game may write.
// vanilla: ObjectiveCriteria
type Criteria struct {
	Name     string
	ReadOnly bool
	Render   RenderType
	// StatType and StatKey name the statistic a statistic criteria counts,
	// as resource locations; both are empty for every other criteria.
	StatType, StatKey string
}

// teamColors are the sixteen chat colours in ordinal order, which names the
// team display slots and team criteria.
// vanilla: ChatFormatting (colours), TeamColor
var teamColors = []string{
	"black", "dark_blue", "dark_green", "dark_aqua", "dark_red", "dark_purple",
	"gold", "gray", "dark_gray", "blue", "green", "aqua", "red", "light_purple",
	"yellow", "white",
}

// customCriteria are the criteria that are not statistics, in registration order.
var customCriteria = func() []Criteria {
	out := []Criteria{
		{Name: "dummy"}, {Name: "trigger"}, {Name: "deathCount"},
		{Name: "playerKillCount"}, {Name: "totalKillCount"},
		{Name: "health", ReadOnly: true, Render: RenderHearts},
		{Name: "food", ReadOnly: true}, {Name: "air", ReadOnly: true},
		{Name: "armor", ReadOnly: true}, {Name: "xp", ReadOnly: true},
		{Name: "level", ReadOnly: true},
	}
	for _, color := range teamColors {
		out = append(out, Criteria{Name: "teamkill." + color})
	}
	for _, color := range teamColors {
		out = append(out, Criteria{Name: "killedByTeam." + color})
	}
	return out
}()

// LookupCriteria finds a criteria by name, statistic criteria included
// (e.g. minecraft.mined:minecraft.stone).
// vanilla: ObjectiveCriteria.byName
func LookupCriteria(name string) (Criteria, bool) {
	for _, c := range customCriteria {
		if c.Name == name {
			return c, true
		}
	}
	return lookupStat(name)
}

// CriteriaNames is every criteria name the board accepts, in vanilla's order.
// The slice is shared; callers must not change it.
func CriteriaNames() []string { return allNames() }

// DisplaySlot is where a client shows an objective, numbered as the Display
// Objective packet does: list, sidebar, below_name, then a sidebar per team
// colour.
// vanilla: DisplaySlot
type DisplaySlot int32

// SlotCount is how many display slots there are.
const SlotCount = 19

// SlotNames is every display slot's name, indexed by the slot.
var SlotNames = func() []string {
	names := []string{"list", "sidebar", "below_name"}
	for _, color := range teamColors {
		names = append(names, "sidebar.team."+color)
	}
	return names
}()

// LookupSlot finds a display slot by name.
func LookupSlot(name string) (DisplaySlot, bool) {
	i := slices.Index(SlotNames, name)
	return DisplaySlot(i), i >= 0
}

// InstanceID identifies one objective or team lifetime within a board; it is not saved.
type InstanceID uint64

// Objective is a copy of one objective as the board holds it. DisplayName is
// the component's tag.
type Objective struct {
	ID          InstanceID
	Name        string
	Criteria    Criteria
	DisplayName nbt.Tag
	Render      RenderType
	AutoUpdate  bool
	Format      *NumberFormat
}

// Score is a copy of one holder's score in one objective. A new score is
// locked: only /scoreboard players enable opens a trigger to its holder.
type Score struct {
	Value   int32
	Locked  bool
	Display nbt.Tag
	Format  *NumberFormat
}

// Holder is a score's owner: the scoreboard name (player name, entity UUID or
// any text) and, or nil, its display name.
type Holder struct {
	Name string
	// Display builds the holder's display name; the board calls it only for
	// an objective whose display auto-updates, and never under its lock.
	// vanilla: ScoreAccess.set
	Display func() nbt.Tag
}

// ObjectiveMode is the Update Objectives packet's mode byte.
type ObjectiveMode int8

const (
	ObjectiveAdd    ObjectiveMode = 0
	ObjectiveRemove ObjectiveMode = 1
	ObjectiveChange ObjectiveMode = 2
)

// Event is one change a client would see: an *ObjectiveEvent, a
// *DisplayEvent, a *ScoreEvent, a *ResetEvent, a *TeamEvent or a
// *TeamMemberEvent.
type Event interface{ scoreboardEvent() }

// ObjectiveEvent is an objective sent, taken away or changed.
type ObjectiveEvent struct {
	Mode      ObjectiveMode
	Objective Objective
}

// DisplayEvent puts an objective in a slot; an empty Objective clears it.
type DisplayEvent struct {
	Slot      DisplaySlot
	Objective string
}

// ScoreEvent is one holder's score in one objective, sent whole.
type ScoreEvent struct {
	Holder    string
	Objective string
	Score     Score
}

// ResetEvent takes a holder's score away: in one objective, or in every one
// when Objective is empty.
type ResetEvent struct {
	Holder    string
	Objective string
}

func (*ObjectiveEvent) scoreboardEvent() {}
func (*DisplayEvent) scoreboardEvent()   {}
func (*ScoreEvent) scoreboardEvent()     {}
func (*ResetEvent) scoreboardEvent()     {}

// objective is the board's own record of an objective.
type objective struct {
	Objective
}

// holderScores is one holder's scores, keyed by objective.
type holderScores map[*objective]*Score

// Board is the server's scoreboard; every method is goroutine-safe. The
// listener hears events under the board's lock, so in order, and must not
// call back into the board.
// An optional instance argument restricts a read or write to that lifetime;
// a stale instance returns zero values or does nothing, under the same lock.
// vanilla: ServerScoreboard
type Board struct {
	mu         sync.Mutex
	nextID     InstanceID
	notify     func(Event)
	objectives []*objective
	// holders is every holder with a score, in the order they got their
	// first one, which is the order /scoreboard players list names them.
	holders []string
	scores  map[string]holderScores
	display [SlotCount]*objective
	// tracked are the objectives shown in some slot, whose changes reach
	// the clients.
	tracked map[*objective]bool
	// teams are the teams in the order they were added; teamOf is the team
	// each member player is on.
	teams  []*Team
	teamOf map[string]*Team
	// members and collisions are the read-only MemberTeams and
	// TeamCollisions snapshots, nil once stale (see membersChanged).
	members    map[string]string
	collisions map[string]CollisionRule
	// version counts membersChanged calls.
	version uint64
	dirty   bool
	// statObjectives is how many objectives count a statistic, so that an
	// award with none to write takes no lock.
	statObjectives atomic.Int32
	// byStat lists the objectives counting each statistic, in objective
	// order; rebuilt by countStatObjectives.
	byStat map[statRef][]*objective
}

// statRef is one statistic: its type and key as resource locations.
type statRef struct{ statType, key string }

// New makes an empty board whose events go to notify, which may be nil.
func New(notify func(Event)) *Board {
	return &Board{notify: notify, scores: map[string]holderScores{}, teamOf: map[string]*Team{}, tracked: map[*objective]bool{}}
}

func (b *Board) emit(e Event) {
	if b.notify != nil {
		b.notify(e)
	}
}

func (b *Board) newID() InstanceID {
	b.nextID++
	return b.nextID
}

func (b *Board) find(name string, instance ...InstanceID) *objective {
	for _, o := range b.objectives {
		if o.Name == name && (len(instance) == 0 || o.ID == instance[0]) {
			return o
		}
	}
	return nil
}

// copyObjective is the snapshot of o a caller may keep.
func copyObjective(o *objective) Objective {
	out := o.Objective
	if o.Format != nil {
		format := *o.Format
		out.Format = &format
	}
	return out
}

// Objective looks an objective up by name.
func (b *Board) Objective(name string, instance ...InstanceID) (Objective, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	o := b.find(name, instance...)
	if o == nil {
		return Objective{}, false
	}
	return copyObjective(o), true
}

// Objectives is every objective, in the order they were added.
func (b *Board) Objectives() []Objective {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]Objective, len(b.objectives))
	for i, o := range b.objectives {
		out[i] = copyObjective(o)
	}
	return out
}

// AddObjective adds an objective drawn the way its criteria draws, answering
// false when the name is taken.
func (b *Board) AddObjective(name string, criteria Criteria, displayName nbt.Tag) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.find(name) != nil {
		return false
	}
	b.objectives = append(b.objectives, &objective{Objective{
		ID:   b.newID(),
		Name: name, Criteria: criteria, DisplayName: displayName, Render: criteria.Render,
	}})
	b.countStatObjectives()
	b.dirty = true
	return true
}

// countStatObjectives refreshes statObjectives and byStat; the caller holds
// the lock.
func (b *Board) countStatObjectives() {
	n := 0
	b.byStat = nil
	for _, o := range b.objectives {
		if o.Criteria.StatType == "" {
			continue
		}
		n++
		if b.byStat == nil {
			b.byStat = map[statRef][]*objective{}
		}
		ref := statRef{o.Criteria.StatType, o.Criteria.StatKey}
		b.byStat[ref] = append(b.byStat[ref], o)
	}
	b.statObjectives.Store(int32(n))
}

// RemoveObjective removes an objective with its slots and scores, returning
// how many are left; false when there was none.
// vanilla: Scoreboard.removeObjective
func (b *Board) RemoveObjective(name string, instance ...InstanceID) (int, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	o := b.find(name, instance...)
	if o == nil {
		return len(b.objectives), false
	}
	b.objectives = slices.DeleteFunc(b.objectives, func(x *objective) bool { return x == o })
	for slot := range b.display {
		if b.display[slot] == o {
			b.setDisplay(DisplaySlot(slot), nil)
		}
	}
	for _, scores := range b.scores {
		delete(scores, o)
	}
	if b.tracked[o] {
		b.stopTracking(o)
	}
	b.countStatObjectives()
	b.dirty = true
	return len(b.objectives), true
}

// changed tells the clients an objective's display changed, when they track it.
// vanilla: ServerScoreboard.onObjectiveChanged
func (b *Board) changed(o *objective) {
	if b.tracked[o] {
		b.emit(&ObjectiveEvent{Mode: ObjectiveChange, Objective: copyObjective(o)})
	}
	b.dirty = true
}

// SetDisplayName gives an objective a new display name.
func (b *Board) SetDisplayName(name string, displayName nbt.Tag, instance ...InstanceID) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if o := b.find(name, instance...); o != nil {
		o.DisplayName = displayName
		b.changed(o)
	}
}

// SetRenderType sets how an objective is drawn.
func (b *Board) SetRenderType(name string, render RenderType, instance ...InstanceID) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if o := b.find(name, instance...); o != nil {
		o.Render = render
		b.changed(o)
	}
}

// SetAutoUpdate sets whether a score set in the objective takes its holder's
// display name.
func (b *Board) SetAutoUpdate(name string, autoUpdate bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if o := b.find(name); o != nil {
		o.AutoUpdate = autoUpdate
		b.changed(o)
	}
}

// SetObjectiveFormat sets the number format of an objective's scores; nil
// clears it.
func (b *Board) SetObjectiveFormat(name string, format *NumberFormat) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if o := b.find(name); o != nil {
		o.Format = format
		b.changed(o)
	}
}

// Displayed is the name of the objective in a slot, false for an empty slot.
func (b *Board) Displayed(slot DisplaySlot) (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if o := b.display[slot]; o != nil {
		return o.Name, true
	}
	return "", false
}

// SetDisplay shows the named objective in a slot; an empty or unknown name
// clears it.
func (b *Board) SetDisplay(slot DisplaySlot, name string, instance ...InstanceID) {
	b.mu.Lock()
	defer b.mu.Unlock()
	o := b.find(name, instance...)
	if len(instance) != 0 && o == nil {
		return
	}
	b.setDisplay(slot, o)
	b.dirty = true
}

// setDisplay untracks an objective leaving its last slot and tracks one
// entering its first.
// vanilla: ServerScoreboard.setDisplayObjective
func (b *Board) setDisplay(slot DisplaySlot, o *objective) {
	old := b.display[slot]
	b.display[slot] = o
	if old != o && old != nil {
		if b.slotCount(old) > 0 {
			b.emit(&DisplayEvent{Slot: slot, Objective: objectiveName(o)})
		} else {
			b.stopTracking(old)
		}
	}
	if o != nil {
		if b.tracked[o] {
			b.emit(&DisplayEvent{Slot: slot, Objective: o.Name})
		} else {
			b.startTracking(o)
		}
	}
}

func objectiveName(o *objective) string {
	if o == nil {
		return ""
	}
	return o.Name
}

func (b *Board) slotCount(o *objective) int {
	count := 0
	for _, shown := range b.display {
		if shown == o {
			count++
		}
	}
	return count
}

// startEvents is all a client needs to show o: objective, slots, scores.
// vanilla: ServerScoreboard.getStartTrackingPackets
func (b *Board) startEvents(o *objective) []Event {
	events := []Event{&ObjectiveEvent{Mode: ObjectiveAdd, Objective: copyObjective(o)}}
	for slot, shown := range b.display {
		if shown == o {
			events = append(events, &DisplayEvent{Slot: DisplaySlot(slot), Objective: o.Name})
		}
	}
	for _, holder := range b.holders {
		if score, ok := b.scores[holder][o]; ok {
			events = append(events, &ScoreEvent{Holder: holder, Objective: o.Name, Score: copyScore(score)})
		}
	}
	return events
}

func (b *Board) startTracking(o *objective) {
	for _, e := range b.startEvents(o) {
		b.emit(e)
	}
	b.tracked[o] = true
}

// stopTracking takes o away from the clients.
// vanilla: ServerScoreboard.getStopTrackingPackets
func (b *Board) stopTracking(o *objective) {
	b.emit(&ObjectiveEvent{Mode: ObjectiveRemove, Objective: copyObjective(o)})
	for slot, shown := range b.display {
		if shown == o {
			b.emit(&DisplayEvent{Slot: DisplaySlot(slot), Objective: o.Name})
		}
	}
	delete(b.tracked, o)
}

// JoinEvents is a joining player's view: every team, then each displayed
// objective once, in slot order, with its slots and scores.
// vanilla: PlayerList.updateEntireScoreboard
func (b *Board) JoinEvents() []Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.joinEvents()
}

// WithView applies a client view change atomically with respect to live
// events. send must not call back into this board.
func (b *Board) WithView(send func([]Event)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	send(b.joinEvents())
}

func (b *Board) joinEvents() []Event {
	events := b.teamJoinEvents()
	var seen []*objective
	for _, o := range b.display {
		if o == nil || slices.Contains(seen, o) {
			continue
		}
		seen = append(seen, o)
		events = append(events, b.startEvents(o)...)
	}
	return events
}

func copyScore(s *Score) Score {
	out := *s
	if s.Format != nil {
		format := *s.Format
		out.Format = &format
	}
	return out
}

// scoreFor is the holder's score in o, made (locked, at zero) when there is
// none; created says it was.
func (b *Board) scoreFor(holder string, o *objective) (score *Score, created bool) {
	scores, ok := b.scores[holder]
	if !ok {
		scores = holderScores{}
		b.scores[holder] = scores
		b.holders = append(b.holders, holder)
	}
	if score, ok := scores[o]; ok {
		return score, false
	}
	score = &Score{Locked: true}
	scores[o] = score
	b.dirty = true
	return score, true
}

// sendScore tells the clients a score, when they track its objective.
// vanilla: ServerScoreboard.onScoreChanged
func (b *Board) sendScore(holder string, o *objective, score *Score) {
	if b.tracked[o] {
		b.emit(&ScoreEvent{Holder: holder, Objective: o.Name, Score: copyScore(score)})
	}
	b.dirty = true
}

// set stores value and an auto-updating objective's display name, sending
// when either changed or the score is new (created, which vanilla's
// ScoreAccess keeps for every write it makes).
// vanilla: ScoreAccess.set, Scoreboard.getOrCreatePlayerScore (requiresSync)
// display is the holder's built display name, from displays, or nil.
func (b *Board) set(holder string, display nbt.Tag, o *objective, score *Score, created bool, value int32) {
	changed := created
	if o.AutoUpdate && display != nil && !tagsEqual(display, score.Display) {
		score.Display = display
		changed = true
	}
	if score.Value != value {
		score.Value = value
		changed = true
	}
	if changed {
		b.sendScore(holder, o, score)
	}
}

// displays builds each holder's display name, indexed like holders, when
// some objective matching match auto-updates; nil otherwise. It takes the
// lock only to look, since a display name reads the board's teams.
func (b *Board) displays(holders []Holder, match func(*objective) bool) []nbt.Tag {
	b.mu.Lock()
	wanted := false
	for _, o := range b.objectives {
		if o.AutoUpdate && match(o) {
			wanted = true
			break
		}
	}
	b.mu.Unlock()
	if !wanted {
		return nil
	}
	out := make([]nbt.Tag, len(holders))
	for i, h := range holders {
		if h.Display != nil {
			out[i] = h.Display()
		}
	}
	return out
}

// named matches the objectives with one of names.
func named(names ...string) func(*objective) bool {
	return func(o *objective) bool { return slices.Contains(names, o.Name) }
}

// displayAt is displays[i], nil when displays is.
func displayAt(displays []nbt.Tag, i int) nbt.Tag {
	if displays == nil {
		return nil
	}
	return displays[i]
}

// Score is a holder's score in an objective, false when there is none.
func (b *Board) Score(holder, objective string, instance ...InstanceID) (Score, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	o := b.find(objective, instance...)
	if o == nil {
		return Score{}, false
	}
	score, ok := b.scores[holder][o]
	if !ok {
		return Score{}, false
	}
	return copyScore(score), true
}

// Update sets each holder's score to change(current) (zero if new) and
// returns the sum. The objective must exist.
func (b *Board) Update(holders []Holder, objective string, change func(int32) int32) int64 {
	displays := b.displays(holders, named(objective))
	b.mu.Lock()
	defer b.mu.Unlock()
	o := b.find(objective)
	if o == nil {
		return 0
	}
	var sum int64
	for i, holder := range holders {
		score, created := b.scoreFor(holder.Name, o)
		value := change(score.Value)
		b.set(holder.Name, displayAt(displays, i), o, score, created, value)
		sum += int64(value)
	}
	return sum
}

// Store sets each holder's score to value, creating it. On a read-only
// objective it creates the first holder's score and returns ErrReadOnlyScore
// unwritten, where vanilla's ScoreAccess.set throws.
// vanilla: ExecuteCommand.storeValue
func (b *Board) Store(holders []Holder, objective string, value int32, instance ...InstanceID) error {
	displays := b.displays(holders, named(objective))
	b.mu.Lock()
	defer b.mu.Unlock()
	o := b.find(objective, instance...)
	if o == nil {
		return nil
	}
	for i, holder := range holders {
		score, created := b.scoreFor(holder.Name, o)
		if o.Criteria.ReadOnly {
			return ErrReadOnlyScore
		}
		b.set(holder.Name, displayAt(displays, i), o, score, created, value)
	}
	return nil
}

// ErrReadOnlyScore is a write to a read-only criteria's score, which vanilla
// throws on rather than refuses.
// vanilla: Scoreboard.getOrCreatePlayerScore
var ErrReadOnlyScore = errors.New("cannot modify read-only score")

// Operation applies op to each target against each source, returning the sum
// of the targets' new values. op returns both values so a swap can write the
// source back; a swap always writes it, other operators only on change. An
// error from op stops the run.
// vanilla: ScoreboardCommand.performOperation
func (b *Board) Operation(targets []Holder, targetObjective string, sources []Holder, sourceObjective string,
	swap bool, op func(a, b int32) (int32, int32, error)) (int64, error) {
	targetDisplays := b.displays(targets, named(targetObjective))
	sourceDisplays := b.displays(sources, named(sourceObjective))
	b.mu.Lock()
	defer b.mu.Unlock()
	to, so := b.find(targetObjective), b.find(sourceObjective)
	if to == nil || so == nil {
		return 0, nil
	}
	var sum int64
	for ti, target := range targets {
		score, created := b.scoreFor(target.Name, to)
		for si, source := range sources {
			sourceScore, sourceCreated := b.scoreFor(source.Name, so)
			// The source's value before the target is written: the two
			// are one score when a holder operates on itself.
			before := sourceScore.Value
			a, s, err := op(score.Value, before)
			if err != nil {
				return sum, err
			}
			b.set(target.Name, displayAt(targetDisplays, ti), to, score, created, a)
			if swap || s != before {
				if so.Criteria.ReadOnly {
					return sum, ErrReadOnlyScore
				}
				b.set(source.Name, displayAt(sourceDisplays, si), so, sourceScore, sourceCreated, s)
			}
		}
		sum += int64(score.Value)
	}
	return sum, nil
}

// Enable unlocks each holder's trigger score, returning how many were locked.
// vanilla: ScoreboardCommand.enableTrigger
func (b *Board) Enable(holders []Holder, objective string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	o := b.find(objective)
	if o == nil {
		return 0
	}
	count := 0
	for _, holder := range holders {
		score, created := b.scoreFor(holder.Name, o)
		if !score.Locked {
			continue
		}
		score.Locked = false
		if created {
			b.sendScore(holder.Name, o, score)
		}
		b.dirty = true
		count++
	}
	return count
}

// Trigger sets the holder's unlocked score to change(current) and locks it
// again, returning the new value; false, unchanged, when the score is missing
// or locked.
// vanilla: TriggerCommand.getScore
func (b *Board) Trigger(holder Holder, objective string, change func(int32) int32) (int32, bool) {
	displays := b.displays([]Holder{holder}, named(objective))
	b.mu.Lock()
	defer b.mu.Unlock()
	o := b.find(objective)
	if o == nil {
		return 0, false
	}
	score, ok := b.scores[holder.Name][o]
	if !ok || score.Locked {
		return 0, false
	}
	score.Locked = true
	b.dirty = true
	value := change(score.Value)
	b.set(holder.Name, displayAt(displays, 0), o, score, false, value)
	return value, true
}

// SetScoreDisplay sets each holder's display name for its score in an
// objective; nil clears it.
func (b *Board) SetScoreDisplay(holders []Holder, objective string, display nbt.Tag) {
	b.mu.Lock()
	defer b.mu.Unlock()
	o := b.find(objective)
	if o == nil {
		return
	}
	for _, holder := range holders {
		score, created := b.scoreFor(holder.Name, o)
		if created || !tagsEqual(display, score.Display) {
			score.Display = display
			b.sendScore(holder.Name, o, score)
		}
	}
}

// SetScoreFormat sets each holder's number format for its score in an
// objective; nil clears it.
func (b *Board) SetScoreFormat(holders []Holder, objective string, format *NumberFormat, instance ...InstanceID) {
	b.mu.Lock()
	defer b.mu.Unlock()
	o := b.find(objective, instance...)
	if o == nil {
		return
	}
	for _, holder := range holders {
		score, _ := b.scoreFor(holder.Name, o)
		score.Format = format
		b.sendScore(holder.Name, o, score)
	}
}

// ResetAll takes every score of each holder away.
// vanilla: Scoreboard.resetAllPlayerScores
func (b *Board) ResetAll(holders []string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, holder := range holders {
		b.removeHolder(holder)
	}
}

// EntityRemoved forgets a destroyed non-player entity's scores and takes it
// off its team.
// vanilla: Scoreboard.entityRemoved
func (b *Board) EntityRemoved(holder string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.removeHolder(holder)
	b.leave(holder)
}

func (b *Board) removeHolder(holder string) {
	if _, ok := b.scores[holder]; !ok {
		return
	}
	delete(b.scores, holder)
	b.holders = slices.DeleteFunc(b.holders, func(h string) bool { return h == holder })
	// vanilla: ServerScoreboard.onPlayerRemoved, sent whether or not any
	// objective is tracked
	b.emit(&ResetEvent{Holder: holder})
	b.dirty = true
}

// Reset removes each holder's score in one objective, forgetting a holder
// left with none.
// vanilla: Scoreboard.resetSinglePlayerScore
func (b *Board) Reset(holders []string, objective string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	o := b.find(objective)
	if o == nil {
		return
	}
	for _, holder := range holders {
		b.resetOne(holder, o)
	}
}

// resetOne takes one holder's score in o away.
func (b *Board) resetOne(holder string, o *objective) {
	scores, ok := b.scores[holder]
	if !ok {
		return
	}
	_, had := scores[o]
	delete(scores, o)
	if len(scores) == 0 {
		b.removeHolder(holder)
	} else if had {
		if b.tracked[o] {
			b.emit(&ResetEvent{Holder: holder, Objective: o.Name})
		}
		b.dirty = true
	}
}

// Holders is every holder with a score, in the order they got their first.
func (b *Board) Holders() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.holders)
}

// HolderScore is one of a holder's scores, beside its objective.
type HolderScore struct {
	Objective Objective
	Value     int32
}

// HolderScores is a holder's scores, in objective order.
func (b *Board) HolderScores(holder string) []HolderScore {
	b.mu.Lock()
	defer b.mu.Unlock()
	scores := b.scores[holder]
	var out []HolderScore
	for _, o := range b.objectives {
		if score, ok := scores[o]; ok {
			out = append(out, HolderScore{Objective: copyObjective(o), Value: score.Value})
		}
	}
	return out
}

// tagsEqual compares two component tags, nil standing for no component.
func tagsEqual(a, b nbt.Tag) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return reflect.DeepEqual(a, b)
}

// UpdateCriteria sets the holder's score in each objective of criteria to
// change(current) (zero if new). Read-only criteria are written too; only
// commands are refused them.
// vanilla: Scoreboard.forAllObjectives
func (b *Board) UpdateCriteria(criteria string, holder Holder, change func(int32) int32) {
	displays := b.displays([]Holder{holder}, func(o *objective) bool { return o.Criteria.Name == criteria })
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, o := range b.objectives {
		if o.Criteria.Name != criteria {
			continue
		}
		score, created := b.scoreFor(holder.Name, o)
		b.set(holder.Name, displayAt(displays, 0), o, score, created, change(score.Value))
	}
}

// CountsStats is whether any objective counts a statistic, a check without
// the lock that lets the game skip naming one.
func (b *Board) CountsStats() bool { return b.statObjectives.Load() > 0 }

// CountsStat is whether some objective counts the statistic. The game asks
// before building a holder, which it must not do under the board's lock.
func (b *Board) CountsStat(statType, key string) bool {
	if b.statObjectives.Load() == 0 {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.byStat[statRef{statType, key}]) > 0
}

// UpdateStat sets the holder's score in each objective counting the
// statistic to change(current) (zero if new).
// vanilla: Scoreboard.forAllObjectives
func (b *Board) UpdateStat(statType, key string, holder Holder, change func(int32) int32) {
	if b.statObjectives.Load() == 0 {
		return
	}
	ref := statRef{statType, key}
	displays := b.displays([]Holder{holder}, func(o *objective) bool { return slices.Contains(b.byStat[ref], o) })
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, o := range b.byStat[ref] {
		score, created := b.scoreFor(holder.Name, o)
		b.set(holder.Name, displayAt(displays, 0), o, score, created, change(score.Value))
	}
}
