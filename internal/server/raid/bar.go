package raid

import (
	"slices"

	"github.com/google/uuid"
)

// TitleKind picks the bar's text.
type TitleKind int

// The texts a raid bar shows.
const (
	TitleRaid TitleKind = iota
	TitleVictory
	TitleDefeat
)

// Title is the bar's text. A raid title with Remaining set also names the
// raiders left. vanilla: Raid.RAID_NAME_COMPONENT, RAIDERS_REMAINING
type Title struct {
	Kind      TitleKind
	Remaining int
}

// BarAction is a Boss Event packet's action.
type BarAction int

// The Boss Event actions a raid bar sends.
const (
	BarAdd BarAction = iota
	BarRemove
	BarProgress
	BarName
)

// BarView is the bar as a Boss Event packet carries it.
type BarView struct {
	Wire     uuid.UUID
	Progress float32
	Title    Title
}

// Viewer identifies one player connection; EntityID changes on reconnect.
type Viewer struct {
	UUID     uuid.UUID
	EntityID int32
}

// Bar delivers the bar's changes to the named connections.
type Bar interface {
	SendBar(players []Viewer, action BarAction, bar BarView)
}

// bossBar is a raid's red, ten-notch bar; it sends only changes, only to the
// players it shows.
// vanilla: ServerBossEvent
type bossBar struct {
	out      Bar
	wire     uuid.UUID
	progress float32
	title    Title
	visible  bool
	players  []Viewer
}

func newBossBar(out Bar, wire uuid.UUID) *bossBar {
	return &bossBar{out: out, wire: wire, visible: true}
}

func (b *bossBar) view() BarView { return BarView{b.wire, b.progress, b.title} }

func (b *bossBar) send(players []Viewer, a BarAction) {
	if len(players) > 0 {
		b.out.SendBar(slices.Clone(players), a, b.view())
	}
}

func (b *bossBar) setProgress(p float32) {
	if p != b.progress {
		b.progress = p
		if b.visible {
			b.send(b.players, BarProgress)
		}
	}
}

func (b *bossBar) setTitle(t Title) {
	if t != b.title {
		b.title = t
		if b.visible {
			b.send(b.players, BarName)
		}
	}
}

func (b *bossBar) setVisible(v bool) {
	if v == b.visible {
		return
	}
	b.visible = v
	if v {
		b.send(b.players, BarAdd)
	} else {
		b.send(b.players, BarRemove)
	}
}

func (b *bossBar) addPlayer(id Viewer) {
	if slices.Contains(b.players, id) {
		return
	}
	b.players = append(b.players, id)
	if b.visible {
		b.send([]Viewer{id}, BarAdd)
	}
}

func (b *bossBar) removePlayer(id Viewer) {
	i := slices.Index(b.players, id)
	if i < 0 {
		return
	}
	b.players = slices.Delete(b.players, i, i+1)
	if b.visible {
		b.send([]Viewer{id}, BarRemove)
	}
}

func (b *bossBar) removeAll() {
	if b.visible {
		b.send(b.players, BarRemove)
	}
	b.players = nil
}
