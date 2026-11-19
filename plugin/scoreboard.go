package plugin

// Scoreboard is a scoreboard: objectives, scores and teams. Bukkit:
// Scoreboard
type Scoreboard interface {
	Objective(name string) (Objective, bool)
	Objectives() []Objective
	// AddObjective adds an objective counting criterion (dummy, a
	// statistic, health), as /scoreboard objectives add does.
	AddObjective(name, criterion string, display Text, render RenderType) (Objective, error)
	// Display answers the objective shown in a slot; SetDisplay shows one
	// (nil clears the slot).
	Display(slot DisplaySlot) (Objective, bool)
	SetDisplay(slot DisplaySlot, o Objective)

	Team(name string) (Team, bool)
	Teams() []Team
	AddTeam(name string) (Team, error)
	// TeamOf answers the team an entry (a player name or entity UUID) is on.
	TeamOf(entry string) (Team, bool)
	// ResetScores removes the entry's scores in every objective.
	ResetScores(entry string)
}

// Objective is a scoreboard objective.
type Objective interface {
	Name() string
	Criterion() string
	Display() Text
	SetDisplay(Text)
	Render() RenderType
	SetRender(RenderType)
	// Score answers an entry's score, false when it has none.
	Score(entry string) (int, bool)
	SetScore(entry string, v int)
	// SetScoreDisplay shows text in place of the entry's number; nil
	// restores the number.
	SetScoreDisplay(entry string, text *Text)
	Remove()
}

// RenderType is how an objective's scores show in the player list.
type RenderType uint8

const (
	RenderInteger RenderType = iota
	RenderHearts
)

// DisplaySlot is where an objective shows: the list, the sidebar, below
// names, or a team colour's sidebar.
type DisplaySlot uint8

const (
	SlotList DisplaySlot = iota
	SlotSidebar
	SlotBelowName
	// SlotSidebarTeam plus a colour index 0 to 15 is that team colour's
	// sidebar.
	SlotSidebarTeam
)

// Team is a scoreboard team.
type Team interface {
	Name() string
	Display() Text
	SetDisplay(Text)
	Prefix() Text
	SetPrefix(Text)
	Suffix() Text
	SetSuffix(Text)
	// Color is a named colour, "reset" for none.
	Color() string
	SetColor(string)
	FriendlyFire() bool
	SetFriendlyFire(bool)
	SeeInvisible() bool
	SetSeeInvisible(bool)
	// NameTags and Collision are "always", "never", "hideForOtherTeams",
	// "hideForOwnTeam" (or "pushOtherTeams", "pushOwnTeam").
	NameTags() string
	SetNameTags(string)
	Collision() string
	SetCollision(string)
	Entries() []string
	AddEntry(entry string)
	RemoveEntry(entry string)
	Remove()
}

// BossBar is a boss bar shown to players. Bukkit: BossBar
type BossBar interface {
	Title() Text
	SetTitle(Text)
	// Progress is 0 to 1.
	Progress() float32
	SetProgress(float32)
	Color() BossBarColor
	SetColor(BossBarColor)
	Style() BossBarStyle
	SetStyle(BossBarStyle)
	// Flags darken the sky, play boss music and create fog.
	Flags() (darkenSky, music, fog bool)
	SetFlags(darkenSky, music, fog bool)
	Visible() bool
	SetVisible(bool)
	Players() []Player
	AddPlayer(Player)
	RemovePlayer(Player)
	// Remove hides the bar from everyone and deletes it.
	Remove()
}

// BossBarColor is a boss bar's colour.
type BossBarColor uint8

const (
	BarPink BossBarColor = iota
	BarBlue
	BarRed
	BarGreen
	BarYellow
	BarPurple
	BarWhite
)

// BossBarStyle is a boss bar's notches.
type BossBarStyle uint8

const (
	BarProgress BossBarStyle = iota
	BarNotched6
	BarNotched10
	BarNotched12
	BarNotched20
)
