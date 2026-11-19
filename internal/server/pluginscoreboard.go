package server

import (
	"errors"
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/scoreboard"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
)

// pluginScoreboard shares the board used by commands, without exposing server types.
type pluginScoreboard struct {
	srv   *Server
	board *scoreboard.Board
}

func (h *pluginHost) Scoreboard() plugin.Scoreboard { return pluginScoreboard{h.srv, h.srv.scoreboard} }
func (h *pluginHost) NewScoreboard() plugin.Scoreboard {
	var board *scoreboard.Board
	board = scoreboard.New(func(e scoreboard.Event) {
		id, payload := scoreboardPacket(e)
		h.srv.sendScoreboardEvent(board, id, payload)
	})
	boards := append(slices.Clone(h.srv.privateScoreboards()), board)
	h.srv.pluginScoreboards.Store(&boards)
	h.host.Own(func() {
		boards := slices.DeleteFunc(slices.Clone(h.srv.privateScoreboards()), func(b *scoreboard.Board) bool { return b == board })
		h.srv.pluginScoreboards.Store(&boards)
		for _, c := range h.srv.playerClients() {
			delete(c.privateScoreSamplers, board)
			if c.viewedScoreboard() == board {
				pluginPlayer{host: h, c: c}.SetScoreboard(h.Scoreboard())
			}
		}
	})
	return pluginScoreboard{h.srv, board}
}

// privateScoreboards returns an immutable snapshot; writers must clone it.
func (s *Server) privateScoreboards() []*scoreboard.Board {
	if boards := s.pluginScoreboards.Load(); boards != nil {
		return *boards
	}
	return nil
}

func (c *client) viewedScoreboard() *scoreboard.Board {
	if board := c.scoreboardView.Load(); board != nil {
		return board
	}
	return c.srv.scoreboard
}

func (s *Server) sendScoreboardEvent(board *scoreboard.Board, id int32, payload []byte) {
	s.mu.RLock()
	recipients := make([]*client, 0, len(s.clients))
	for _, c := range s.clients {
		if c.viewedScoreboard() == board {
			recipients = append(recipients, c)
		}
	}
	s.mu.RUnlock()
	sendToAll(nil, recipients, outbound{id: id, data: payload})
}

func (p pluginPlayer) Scoreboard() plugin.Scoreboard {
	if !p.IsValid() {
		return nil
	}
	p.c.scoreboardSwitchMu.Lock()
	defer p.c.scoreboardSwitchMu.Unlock()
	return pluginScoreboard{p.c.srv, p.c.viewedScoreboard()}
}

// detachedScoreboard receives no live events between removing the old view
// and installing the new one. Each snapshot is sent under its board's lock.
var detachedScoreboard = scoreboard.New(nil)

func (p pluginPlayer) SetScoreboard(value plugin.Scoreboard) {
	if !p.IsValid() {
		return
	}
	next, ok := value.(pluginScoreboard)
	if !ok || next.srv != p.c.srv {
		return
	}
	c := p.c
	c.scoreboardSwitchMu.Lock()
	defer c.scoreboardSwitchMu.Unlock()
	old := c.viewedScoreboard()
	if old == next.board {
		return
	}
	old.WithView(func(events []scoreboard.Event) {
		c.scoreboardView.Store(detachedScoreboard)
		for _, e := range events {
			switch e := e.(type) {
			case *scoreboard.TeamEvent:
				e.Mode = scoreboard.TeamRemove
			case *scoreboard.ObjectiveEvent:
				e.Mode = scoreboard.ObjectiveRemove
			default:
				continue
			}
			id, payload := scoreboardPacket(e)
			c.send(id, payload)
		}
	})
	// vanilla: PlayerList.updateEntireScoreboard
	next.board.WithView(func(events []scoreboard.Event) {
		c.scoreboardView.Store(next.board)
		for _, e := range events {
			id, payload := scoreboardPacket(e)
			c.send(id, payload)
		}
	})
}

func (b pluginScoreboard) Objective(name string) (plugin.Objective, bool) {
	o, ok := b.board.Objective(name)
	if !ok {
		return nil, false
	}
	return pluginObjective{b, name, o.ID}, true
}
func (b pluginScoreboard) Objectives() []plugin.Objective {
	out := make([]plugin.Objective, 0)
	for _, o := range b.board.Objectives() {
		out = append(out, pluginObjective{b, o.Name, o.ID})
	}
	return out
}
func (b pluginScoreboard) AddObjective(name, criterion string, display plugin.Text, render plugin.RenderType) (plugin.Objective, error) {
	criteria, ok := scoreboard.LookupCriteria(criterion)
	if !ok {
		return nil, errors.New("unknown scoreboard criterion")
	}
	if render > plugin.RenderHearts {
		return nil, errors.New("unknown scoreboard render type")
	}
	if !b.board.AddObjective(name, criteria, protocol.TextTag(pluginProtocolText(display))) {
		return nil, errors.New("objective already exists")
	}
	b.board.SetRenderType(name, scoreboard.RenderType(render))
	o, _ := b.Objective(name)
	return o, nil
}
func (b pluginScoreboard) Display(slot plugin.DisplaySlot) (plugin.Objective, bool) {
	if int(slot) >= scoreboard.SlotCount {
		return nil, false
	}
	name, ok := b.board.Displayed(scoreboard.DisplaySlot(slot))
	if !ok {
		return nil, false
	}
	return b.Objective(name)
}
func (b pluginScoreboard) SetDisplay(slot plugin.DisplaySlot, objective plugin.Objective) {
	if int(slot) >= scoreboard.SlotCount {
		return
	}
	if objective != nil {
		o, ok := objective.(pluginObjective)
		if !ok || o.board != b {
			return
		}
		b.board.SetDisplay(scoreboard.DisplaySlot(slot), o.name, o.id)
		return
	}
	b.board.SetDisplay(scoreboard.DisplaySlot(slot), "")
}
func (b pluginScoreboard) ResetScores(entry string) { b.board.ResetAll([]string{entry}) }
func (b pluginScoreboard) Team(name string) (plugin.Team, bool) {
	team, ok := b.board.Team(name)
	if !ok {
		return nil, false
	}
	return pluginTeam{b, name, team.ID}, true
}
func (b pluginScoreboard) Teams() []plugin.Team {
	out := make([]plugin.Team, 0)
	for _, team := range b.board.Teams() {
		out = append(out, pluginTeam{b, team.Name, team.ID})
	}
	return out
}
func (b pluginScoreboard) AddTeam(name string) (plugin.Team, error) {
	if !b.board.AddTeam(name, nbt.String(name)) {
		return nil, errors.New("team already exists")
	}
	team, _ := b.Team(name)
	return team, nil
}
func (b pluginScoreboard) TeamOf(entry string) (plugin.Team, bool) {
	team, ok := b.board.MemberTeam(entry)
	if !ok {
		return nil, false
	}
	return pluginTeam{b, team.Name, team.ID}, true
}

type pluginObjective struct {
	board pluginScoreboard
	name  string
	id    scoreboard.InstanceID
}

func (o pluginObjective) snapshot() scoreboard.Objective {
	v, _ := o.board.board.Objective(o.name, o.id)
	return v
}
func (o pluginObjective) Name() string         { return o.snapshot().Name }
func (o pluginObjective) Criterion() string    { return o.snapshot().Criteria.Name }
func (o pluginObjective) Display() plugin.Text { return pluginScoreText(o.snapshot().DisplayName) }
func (o pluginObjective) SetDisplay(text plugin.Text) {
	o.board.board.SetDisplayName(o.name, protocol.TextTag(pluginProtocolText(text)), o.id)
}
func (o pluginObjective) Render() plugin.RenderType { return plugin.RenderType(o.snapshot().Render) }
func (o pluginObjective) SetRender(render plugin.RenderType) {
	if render <= plugin.RenderHearts {
		o.board.board.SetRenderType(o.name, scoreboard.RenderType(render), o.id)
	}
}
func (o pluginObjective) Score(entry string) (int, bool) {
	v, ok := o.board.board.Score(entry, o.name, o.id)
	return int(v.Value), ok
}
func (o pluginObjective) SetScore(entry string, v int) {
	_ = o.board.board.Store([]scoreboard.Holder{{Name: entry}}, o.name, int32(v), o.id)
}
func (o pluginObjective) SetScoreDisplay(entry string, text *plugin.Text) {
	var format *scoreboard.NumberFormat
	if text != nil {
		format = &scoreboard.NumberFormat{Kind: scoreboard.FormatFixed, Fixed: protocol.TextTag(pluginProtocolText(*text))}
	}
	o.board.board.SetScoreFormat([]scoreboard.Holder{{Name: entry}}, o.name, format, o.id)
}
func (o pluginObjective) Remove() { o.board.board.RemoveObjective(o.name, o.id) }

func pluginScoreText(tag nbt.Tag) plugin.Text {
	text, _ := pluginText(protocol.Text{Raw: tag})
	return text
}

type pluginTeam struct {
	board pluginScoreboard
	name  string
	id    scoreboard.InstanceID
}

func (t pluginTeam) snapshot() scoreboard.Team { v, _ := t.board.board.Team(t.name, t.id); return v }
func (t pluginTeam) Name() string              { return t.snapshot().Name }
func (t pluginTeam) Display() plugin.Text      { return pluginScoreText(t.snapshot().DisplayName) }
func (t pluginTeam) SetDisplay(v plugin.Text) {
	t.board.board.UpdateTeam(t.name, func(team *scoreboard.Team) { team.DisplayName = protocol.TextTag(pluginProtocolText(v)) }, t.id)
}
func (t pluginTeam) Prefix() plugin.Text { return pluginScoreText(t.snapshot().Prefix) }
func (t pluginTeam) SetPrefix(v plugin.Text) {
	t.board.board.UpdateTeam(t.name, func(team *scoreboard.Team) { team.Prefix = protocol.TextTag(pluginProtocolText(v)) }, t.id)
}
func (t pluginTeam) Suffix() plugin.Text { return pluginScoreText(t.snapshot().Suffix) }
func (t pluginTeam) SetSuffix(v plugin.Text) {
	t.board.board.UpdateTeam(t.name, func(team *scoreboard.Team) { team.Suffix = protocol.TextTag(pluginProtocolText(v)) }, t.id)
}
func (t pluginTeam) Color() string {
	team := t.snapshot()
	if team.ID == 0 {
		return ""
	}
	color := team.Color
	if color < 0 {
		return "reset"
	}
	return scoreboard.TeamColors()[color]
}
func (t pluginTeam) SetColor(v string) {
	color, ok := scoreboard.LookupTeamColor(v)
	if v == "reset" {
		color, ok = scoreboard.NoColor, true
	}
	if ok {
		t.board.board.UpdateTeam(t.name, func(team *scoreboard.Team) { team.Color = color }, t.id)
	}
}
func (t pluginTeam) FriendlyFire() bool { return t.snapshot().FriendlyFire }
func (t pluginTeam) SetFriendlyFire(v bool) {
	t.board.board.UpdateTeam(t.name, func(team *scoreboard.Team) { team.FriendlyFire = v }, t.id)
}
func (t pluginTeam) SeeInvisible() bool { return t.snapshot().SeeFriendlyInvisible }
func (t pluginTeam) SetSeeInvisible(v bool) {
	t.board.board.UpdateTeam(t.name, func(team *scoreboard.Team) { team.SeeFriendlyInvisible = v }, t.id)
}
func (t pluginTeam) NameTags() string {
	team := t.snapshot()
	if team.ID == 0 {
		return ""
	}
	return team.NameTag.String()
}
func (t pluginTeam) SetNameTags(v string) {
	if visibility, ok := scoreboard.ParseVisibility(v); ok {
		t.board.board.UpdateTeam(t.name, func(team *scoreboard.Team) { team.NameTag = visibility }, t.id)
	}
}
func (t pluginTeam) Collision() string {
	team := t.snapshot()
	if team.ID == 0 {
		return ""
	}
	return team.Collision.String()
}
func (t pluginTeam) SetCollision(v string) {
	if collision, ok := scoreboard.ParseCollisionRule(v); ok {
		t.board.board.UpdateTeam(t.name, func(team *scoreboard.Team) { team.Collision = collision }, t.id)
	}
}
func (t pluginTeam) Entries() []string     { return t.snapshot().Players }
func (t pluginTeam) AddEntry(entry string) { t.board.board.JoinTeam(t.name, []string{entry}, t.id) }
func (t pluginTeam) RemoveEntry(entry string) {
	t.board.board.LeaveTeam([]string{entry}, t.id)
}
func (t pluginTeam) Remove() { t.board.board.RemoveTeam(t.name, t.id) }
