package scoreboard

import (
	"slices"

	"github.com/golem-mc/golem/nbt"
)

// Visibility is who a team's name tags or death messages show to, numbered
// as the Set Player Team packet carries it.
// vanilla: Team.Visibility
type Visibility int32

const (
	VisibleAlways        Visibility = 0
	VisibleNever         Visibility = 1
	HideForOtherTeams    Visibility = 2
	HideForOwnTeam       Visibility = 3
	visibilityValueCount            = 4
)

var visibilityNames = [visibilityValueCount]string{"always", "never", "hideForOtherTeams", "hideForOwnTeam"}

// String is the visibility's name, as the command and the saved file spell it.
func (v Visibility) String() string { return visibilityNames[v] }

// CollisionRule is whom a team's members push, numbered as the Set Player
// Team packet carries it.
// vanilla: Team.CollisionRule
type CollisionRule int32

const (
	CollideAlways    CollisionRule = 0
	CollideNever     CollisionRule = 1
	PushOtherTeams   CollisionRule = 2
	PushOwnTeam      CollisionRule = 3
	collisionRuleNum               = 4
)

var collisionNames = [collisionRuleNum]string{"always", "never", "pushOtherTeams", "pushOwnTeam"}

// String is the rule's name, as the command and the saved file spell it.
func (c CollisionRule) String() string { return collisionNames[c] }

// NoColor is a team's Color when it has none.
const NoColor = -1

// Team is a copy of one team as the board holds it. The component fields
// are component tags; Color is an index into TeamColors, or NoColor.
// vanilla: PlayerTeam
type Team struct {
	ID                   InstanceID
	Name                 string
	DisplayName          nbt.Tag
	Prefix, Suffix       nbt.Tag
	Color                int
	FriendlyFire         bool
	SeeFriendlyInvisible bool
	NameTag              Visibility
	DeathMessage         Visibility
	Collision            CollisionRule
	// Players is the members' scoreboard names, in the order they joined.
	Players []string
}

// TeamMode is the Set Player Team packet's method byte for a team itself.
type TeamMode int8

const (
	TeamAdd    TeamMode = 0
	TeamRemove TeamMode = 1
	TeamChange TeamMode = 2
)

// TeamEvent is a team sent (with its members), taken away or changed.
type TeamEvent struct {
	Mode TeamMode
	Team Team
}

// TeamMemberEvent is one player joining or leaving a team.
type TeamMemberEvent struct {
	Team   string
	Player string
	Joined bool
}

func (*TeamEvent) scoreboardEvent()       {}
func (*TeamMemberEvent) scoreboardEvent() {}

// TeamColors are the sixteen team colours' names, indexed by Team.Color.
// vanilla: TeamColor
func TeamColors() []string { return slices.Clone(teamColors) }

// teamRGB is each team colour's RGB, indexed like teamColors.
// vanilla: TeamColor
var teamRGB = [...]int32{0x000000, 0x0000AA, 0x00AA00, 0x00AAAA, 0xAA0000, 0xAA00AA, 0xFFAA00, 0xAAAAAA,
	0x555555, 0x5555FF, 0x55FF55, 0x55FFFF, 0xFF5555, 0xFF55FF, 0xFFFF55, 0xFFFFFF}

// TeamColorRGB is the RGB of team colour index i.
func TeamColorRGB(i int) int32 { return teamRGB[i] }

// TeamIconRGB is the RGB a locator icon wears for team colour index i: black
// is lifted to 0x303030 (-13619152) so it shows on the bar.
// vanilla: Waypoint.Icon.cloneAndAssignStyle
func TeamIconRGB(i int) int32 {
	if i == 0 {
		return 0x303030
	}
	return teamRGB[i]
}

// MemberTeamColor is the colour index of the team a holder is on, NoColor
// for no team or a team without a colour. It copies nothing.
func (b *Board) MemberTeamColor(holder string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	if t := b.teamOf[holder]; t != nil {
		return t.Color
	}
	return NoColor
}

// LookupTeamColor finds a team colour's index by name.
func LookupTeamColor(name string) (int, bool) {
	i := slices.Index(teamColors, name)
	return i, i >= 0
}

// ParseVisibility reads a visibility's name.
func ParseVisibility(name string) (Visibility, bool) {
	i := slices.Index(visibilityNames[:], name)
	return Visibility(i), i >= 0
}

// ParseCollisionRule reads a collision rule's name.
func ParseCollisionRule(name string) (CollisionRule, bool) {
	i := slices.Index(collisionNames[:], name)
	return CollisionRule(i), i >= 0
}

func (b *Board) newTeam(name string, displayName nbt.Tag) *Team {
	return &Team{ID: b.newID(), Name: name, DisplayName: displayName, Color: NoColor, FriendlyFire: true, SeeFriendlyInvisible: true}
}

func (b *Board) findTeam(name string, instance ...InstanceID) *Team {
	for _, t := range b.teams {
		if t.Name == name && (len(instance) == 0 || t.ID == instance[0]) {
			return t
		}
	}
	return nil
}

func copyTeam(t *Team) Team {
	out := *t
	out.Players = slices.Clone(t.Players)
	return out
}

// Team looks a team up by name.
func (b *Board) Team(name string, instance ...InstanceID) (Team, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	t := b.findTeam(name, instance...)
	if t == nil {
		return Team{}, false
	}
	return copyTeam(t), true
}

// Teams is every team, in the order they were added.
func (b *Board) Teams() []Team {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]Team, len(b.teams))
	for i, t := range b.teams {
		out[i] = copyTeam(t)
	}
	return out
}

// MemberTeam is the team a scoreboard holder is on, false for none.
func (b *Board) MemberTeam(holder string) (Team, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	t := b.teamOf[holder]
	if t == nil {
		return Team{}, false
	}
	return copyTeam(t), true
}

// PlayerTeam is the name of the team a player is on, false for none.
func (b *Board) PlayerTeam(player string) (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	t := b.teamOf[player]
	if t == nil {
		return "", false
	}
	return t.Name, true
}

// CanHarm reports whether attacker may hurt victim: always unless both are on
// the same team, then that team's friendly-fire option. A player is allied to
// themselves.
// vanilla: Player.canHarmPlayer
func (b *Board) CanHarm(victim, attacker string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	t := b.teamOf[victim]
	return t == nil || b.teamOf[attacker] != t || t.FriendlyFire
}

// MemberTeams maps each member's name to the name of its team, nil while no
// one is on a team.
func (b *Board) MemberTeams() map[string]string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.teamOf) == 0 {
		return nil
	}
	out := make(map[string]string, len(b.teamOf))
	for name, t := range b.teamOf {
		out[name] = t.Name
	}
	return out
}

// TeamCollisions maps each team's name to its collision rule, nil while there
// are no teams.
func (b *Board) TeamCollisions() map[string]CollisionRule {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.teams) == 0 {
		return nil
	}
	out := make(map[string]CollisionRule, len(b.teams))
	for _, t := range b.teams {
		out[t.Name] = t.Collision
	}
	return out
}

// AddTeam adds a team with a display name, answering false when the name is
// taken. Clients are told the team under its own name first, then the
// display name.
// vanilla: Scoreboard.addPlayerTeam, TeamCommand.createTeam
func (b *Board) AddTeam(name string, displayName nbt.Tag) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.findTeam(name) != nil {
		return false
	}
	t := b.newTeam(name, nbt.String(name))
	b.teams = append(b.teams, t)
	b.emit(&TeamEvent{Mode: TeamAdd, Team: copyTeam(t)})
	t.DisplayName = displayName
	b.teamChanged(t)
	return true
}

// teamChanged tells the clients a team's parameters changed.
// vanilla: ServerScoreboard.onTeamChanged
func (b *Board) teamChanged(t *Team) {
	b.emit(&TeamEvent{Mode: TeamChange, Team: copyTeam(t)})
	b.dirty = true
}

// UpdateTeam lets change edit a team's own fields (not ID, Name or Players),
// then tells the clients once.
func (b *Board) UpdateTeam(name string, change func(*Team), instance ...InstanceID) {
	b.mu.Lock()
	defer b.mu.Unlock()
	t := b.findTeam(name, instance...)
	if t == nil {
		return
	}
	edited := copyTeam(t)
	change(&edited)
	edited.ID = t.ID
	edited.Name, edited.Players = t.Name, t.Players
	*t = edited
	b.teamChanged(t)
}

// RemoveTeam removes a team and frees its members, false when there was none.
// vanilla: Scoreboard.removePlayerTeam
func (b *Board) RemoveTeam(name string, instance ...InstanceID) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	t := b.findTeam(name, instance...)
	if t == nil {
		return false
	}
	b.teams = slices.DeleteFunc(b.teams, func(x *Team) bool { return x == t })
	for _, player := range t.Players {
		delete(b.teamOf, player)
	}
	b.emit(&TeamEvent{Mode: TeamRemove, Team: copyTeam(t)})
	b.dirty = true
	return true
}

// JoinTeam puts players on a team, leaving the one they were on. It does
// nothing for a team that does not exist.
// vanilla: ServerScoreboard.addPlayerToTeam
func (b *Board) JoinTeam(team string, players []string, instance ...InstanceID) {
	b.mu.Lock()
	defer b.mu.Unlock()
	t := b.findTeam(team, instance...)
	if t == nil {
		return
	}
	for _, player := range players {
		b.leave(player)
		b.teamOf[player] = t
		t.Players = append(t.Players, player)
		b.emit(&TeamMemberEvent{Team: team, Player: player, Joined: true})
		b.dirty = true
	}
}

// LeaveTeam takes players off whichever team they are on, reporting for each
// whether they were on one.
// vanilla: Scoreboard.removePlayerFromTeam
func (b *Board) LeaveTeam(players []string, instance ...InstanceID) []bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	left := make([]bool, len(players))
	for i, player := range players {
		if len(instance) != 0 && (b.teamOf[player] == nil || b.teamOf[player].ID != instance[0]) {
			continue
		}
		left[i] = b.leave(player)
	}
	return left
}

// EmptyTeam takes every member off a team, returning how many there were.
func (b *Board) EmptyTeam(team string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	t := b.findTeam(team)
	if t == nil {
		return 0
	}
	members := slices.Clone(t.Players)
	for _, player := range members {
		b.leave(player)
	}
	return len(members)
}

// leave takes a player off their team, reporting whether they were on one;
// the caller holds the lock.
func (b *Board) leave(player string) bool {
	t := b.unlink(player)
	if t != nil {
		b.emit(&TeamMemberEvent{Team: t.Name, Player: player})
		b.dirty = true
	}
	return t != nil
}

// unlink takes a player off their team without telling anyone, returning
// the team they were on.
func (b *Board) unlink(player string) *Team {
	t := b.teamOf[player]
	if t != nil {
		delete(b.teamOf, player)
		t.Players = slices.DeleteFunc(t.Players, func(p string) bool { return p == player })
	}
	return t
}

// teamJoinEvents are the teams a joining client is sent, each with its
// members; the caller holds the lock.
// vanilla: PlayerList.updateEntireScoreboard
func (b *Board) teamJoinEvents() []Event {
	events := make([]Event, len(b.teams))
	for i, t := range b.teams {
		events[i] = &TeamEvent{Mode: TeamAdd, Team: copyTeam(t)}
	}
	return events
}

// packTeams is the saved Teams list.
// vanilla: PlayerTeam.Packed.CODEC (a default value is left out)
func (b *Board) packTeams() nbt.List {
	out := nbt.List{Elem: nbt.TypeCompound}
	for _, t := range b.teams {
		entry := nbt.Compound{"Name": nbt.String(t.Name), "DisplayName": t.DisplayName}
		if t.Color != NoColor {
			entry["TeamColor"] = nbt.String(teamColors[t.Color])
		}
		if !t.FriendlyFire {
			entry["AllowFriendlyFire"] = boolByte(false)
		}
		if !t.SeeFriendlyInvisible {
			entry["SeeFriendlyInvisibles"] = boolByte(false)
		}
		if !emptyComponent(t.Prefix) {
			entry["MemberNamePrefix"] = t.Prefix
		}
		if !emptyComponent(t.Suffix) {
			entry["MemberNameSuffix"] = t.Suffix
		}
		if t.NameTag != VisibleAlways {
			entry["NameTagVisibility"] = nbt.String(t.NameTag.String())
		}
		if t.DeathMessage != VisibleAlways {
			entry["DeathMessageVisibility"] = nbt.String(t.DeathMessage.String())
		}
		if t.Collision != CollideAlways {
			entry["CollisionRule"] = nbt.String(t.Collision.String())
		}
		if len(t.Players) > 0 {
			players := nbt.List{Elem: nbt.TypeString}
			for _, p := range t.Players {
				players.Items = append(players.Items, nbt.String(p))
			}
			entry["Players"] = players
		}
		out.Items = append(out.Items, entry)
	}
	return out
}

// emptyComponent says a component tag is vanilla's empty one: absent or "".
func emptyComponent(tag nbt.Tag) bool {
	s, ok := tag.(nbt.String)
	return tag == nil || ok && s == ""
}

// loadTeams reads the saved Teams list, skipping a nameless or repeated
// team and keeping a default for a field that does not read.
// vanilla: Scoreboard.loadPlayerTeam
func (b *Board) loadTeams(list nbt.List) {
	for _, tag := range list.Items {
		entry, ok := tag.(nbt.Compound)
		if !ok {
			continue
		}
		name, _ := nbt.Get[nbt.String](entry, "Name")
		if name == "" || b.findTeam(string(name)) != nil {
			continue
		}
		t := b.newTeam(string(name), nbt.String(name))
		if v := entry["DisplayName"]; v != nil {
			t.DisplayName = v
		}
		t.Prefix, t.Suffix = entry["MemberNamePrefix"], entry["MemberNameSuffix"]
		if color, ok := nbt.Get[nbt.String](entry, "TeamColor"); ok {
			if i, ok := LookupTeamColor(string(color)); ok {
				t.Color = i
			}
		}
		if v, ok := nbt.ByteValue(entry["AllowFriendlyFire"]); ok {
			t.FriendlyFire = v != 0
		}
		if v, ok := nbt.ByteValue(entry["SeeFriendlyInvisibles"]); ok {
			t.SeeFriendlyInvisible = v != 0
		}
		if v, ok := nbt.Get[nbt.String](entry, "NameTagVisibility"); ok {
			t.NameTag, _ = ParseVisibility(string(v))
		}
		if v, ok := nbt.Get[nbt.String](entry, "DeathMessageVisibility"); ok {
			t.DeathMessage, _ = ParseVisibility(string(v))
		}
		if v, ok := nbt.Get[nbt.String](entry, "CollisionRule"); ok {
			t.Collision, _ = ParseCollisionRule(string(v))
		}
		b.teams = append(b.teams, t)
		players, _ := nbt.Get[nbt.List](entry, "Players")
		for _, p := range players.Items {
			if player, ok := p.(nbt.String); ok {
				b.unlink(string(player))
				b.teamOf[string(player)] = t
				t.Players = append(t.Players, string(player))
			}
		}
	}
}
