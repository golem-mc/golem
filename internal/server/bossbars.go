package server

import (
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/google/uuid"
)

// bossEventPayload builds a Boss Event of one action: add carries the whole
// bar, others only the changed field. vanilla: ClientboundBossEventPacket
func bossEventPayload(bar world.BossBar, action int32) []byte {
	return bossEventPayloadProgress(bar, action, bar.Fill())
}

func bossEventPayloadProgress(bar world.BossBar, action int32, progress float32) []byte {
	var b protocol.Buffer
	var name []byte
	if action == protocol.BossEventAdd || action == protocol.BossEventName {
		// The tag was checked as a component when the bar was made or read.
		name, _ = nbt.EncodeNetwork(bar.Name)
	}
	b.PutUUID(bar.Wire)
	b.PutVarInt(action)
	switch action {
	case protocol.BossEventAdd:
		b.PutRaw(name)
		b.PutF32(progress)
		b.PutVarInt(bar.Color)
		b.PutVarInt(bar.Overlay)
		b.PutU8(bar.Flags())
	case protocol.BossEventProgress:
		b.PutF32(progress)
	case protocol.BossEventName:
		b.PutRaw(name)
	case protocol.BossEventStyle:
		b.PutVarInt(bar.Color)
		b.PutVarInt(bar.Overlay)
	case protocol.BossEventProperties:
		b.PutU8(bar.Flags())
	}
	return b.Bytes()
}

// sendBossBars sends the join the bars that name the player and are shown.
func (c *client) sendBossBars() error {
	for _, bar := range c.srv.bossBars.All() {
		if bar.Visible && slices.Contains(bar.Players, c.player.UUID) {
			if err := c.writePacket(protocol.IDPlayBossEvent, "boss event", bossEventPayload(bar, protocol.BossEventAdd)); err != nil {
				return err
			}
		}
	}
	return nil
}

// sendBossEvent sends a Boss Event to the named online players.
func (s *Server) sendBossEvent(players []uuid.UUID, bar world.BossBar, action int32) {
	data := bossEventPayload(bar, action)
	for _, id := range players {
		if c := s.clientByUUID(id); c != nil {
			c.send(protocol.IDPlayBossEvent, data)
		}
	}
}

// bossBarResult is what the command is told after the bar's players have
// been sent the change.
func (s *Server) bossBarResult(ch world.BossBarChange, tell func(ch world.BossBarChange)) command.BossBarResult {
	if !ch.Existed {
		return command.BossBarResult{}
	}
	if ch.Changed {
		tell(ch)
	}
	return command.BossBarResult{
		Existed: true, Changed: ch.Changed, Display: ch.After.DisplayName(), Players: len(ch.After.Players),
	}
}

// AddBossBar makes the bar the command names.
func (ws worldScope) AddBossBar(id string, name nbt.Tag) (protocol.Text, bool) {
	bar, created := ws.srv.bossBars.Add(id, name)
	return bar.DisplayName(), created
}

// SetBossBarPlayers sets exactly the targets on the bar: joiners get its
// add, leavers its remove, only while shown. vanilla: CustomBossEvent.setPlayers
func (ws worldScope) SetBossBarPlayers(id string, targets []command.Target) command.BossBarResult {
	players := make([]uuid.UUID, len(targets))
	for i, t := range targets {
		players[i] = t.Profile().UUID
	}
	return ws.srv.bossBarResult(ws.srv.bossBars.SetPlayers(id, players), func(ch world.BossBarChange) {
		if !ch.After.Visible {
			return
		}
		joined := slices.DeleteFunc(slices.Clone(ch.After.Players), func(p uuid.UUID) bool { return slices.Contains(ch.Before.Players, p) })
		left := slices.DeleteFunc(slices.Clone(ch.Before.Players), func(p uuid.UUID) bool { return slices.Contains(ch.After.Players, p) })
		ws.srv.sendBossEvent(joined, ch.After, protocol.BossEventAdd)
		ws.srv.sendBossEvent(left, ch.After, protocol.BossEventRemove)
	})
}

// SetBossBarValue sets the value; viewers get the new fill if it changed.
// vanilla: ServerBossEvent.setProgress
func (ws worldScope) SetBossBarValue(id string, value int32) command.BossBarResult {
	return ws.srv.bossBarResult(ws.srv.bossBars.SetValue(id, value), func(ch world.BossBarChange) {
		if ch.After.Visible && ch.After.Fill() != ch.Before.Fill() {
			ws.srv.sendBossEvent(ch.After.Players, ch.After, protocol.BossEventProgress)
		}
	})
}

// SetBossBarMax sets the maximum; viewers get the new fill if it changed.
// vanilla: CustomBossEvent.setMax
func (ws worldScope) SetBossBarMax(id string, maxValue int32) command.BossBarResult {
	return ws.srv.bossBarResult(ws.srv.bossBars.SetMax(id, maxValue), func(ch world.BossBarChange) {
		if ch.After.Visible && ch.After.Fill() != ch.Before.Fill() {
			ws.srv.sendBossEvent(ch.After.Players, ch.After, protocol.BossEventProgress)
		}
	})
}

// SetBossBarVisible shows or hides the bar for its players.
// vanilla: ServerBossEvent.setVisible
func (ws worldScope) SetBossBarVisible(id string, visible bool) command.BossBarResult {
	return ws.srv.bossBarResult(ws.srv.bossBars.SetVisible(id, visible), func(ch world.BossBarChange) {
		action := int32(protocol.BossEventRemove)
		if visible {
			action = protocol.BossEventAdd
		}
		ws.srv.sendBossEvent(ch.After.Players, ch.After, action)
	})
}

// BossBarIDs are the bars that exist, which the id arguments complete from.
func (ws worldScope) BossBarIDs() []string { return ws.srv.bossBars.IDs() }

// RemoveBossBar deletes the bar, removing it from its players.
// vanilla: BossBarCommands.removeBar
func (ws worldScope) RemoveBossBar(id string) (protocol.Text, int, bool) {
	ch := ws.srv.bossBars.Remove(id)
	if !ch.Existed {
		return protocol.Text{}, 0, false
	}
	if ch.Before.Visible {
		ws.srv.sendBossEvent(ch.Before.Players, ch.Before, protocol.BossEventRemove)
	}
	return ch.Before.DisplayName(), len(ws.srv.bossBars.IDs()), true
}

// SetBossBarName renames the bar, sending viewers the name.
// vanilla: ServerBossEvent.setName
func (ws worldScope) SetBossBarName(id string, name nbt.Tag) command.BossBarResult {
	return ws.srv.bossBarResult(ws.srv.bossBars.SetName(id, name), func(ch world.BossBarChange) {
		if ch.After.Visible {
			ws.srv.sendBossEvent(ch.After.Players, ch.After, protocol.BossEventName)
		}
	})
}

// SetBossBarColor sets the colour, sending viewers the style.
// vanilla: ServerBossEvent.setColor
func (ws worldScope) SetBossBarColor(id string, color int32) command.BossBarResult {
	return ws.srv.bossBarResult(ws.srv.bossBars.SetColor(id, color), func(ch world.BossBarChange) {
		if ch.After.Visible {
			ws.srv.sendBossEvent(ch.After.Players, ch.After, protocol.BossEventStyle)
		}
	})
}

// SetBossBarStyle sets the style, sending viewers the style.
// vanilla: ServerBossEvent.setOverlay
func (ws worldScope) SetBossBarStyle(id string, style int32) command.BossBarResult {
	return ws.srv.bossBarResult(ws.srv.bossBars.SetOverlay(id, style), func(ch world.BossBarChange) {
		if ch.After.Visible {
			ws.srv.sendBossEvent(ch.After.Players, ch.After, protocol.BossEventStyle)
		}
	})
}

// BossBars lists the bars sorted by id.
func (ws worldScope) BossBars() []command.BossBarInfo {
	bars := ws.srv.bossBars.All()
	out := make([]command.BossBarInfo, len(bars))
	for i, bar := range bars {
		out[i] = bossBarInfo(bar)
	}
	return out
}

// BossBar describes the bar the id names.
func (ws worldScope) BossBar(id string) (command.BossBarInfo, bool) {
	bar, ok := ws.srv.bossBars.Get(id)
	return bossBarInfo(bar), ok
}

func bossBarInfo(bar world.BossBar) command.BossBarInfo {
	return command.BossBarInfo{
		Display: bar.DisplayName(), Value: bar.Value, Max: bar.Max, Visible: bar.Visible, Players: bar.Players,
	}
}
