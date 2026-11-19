package server

import (
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/plugin"
	"log/slog"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/scoreboard"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

// entityDestroyed queues a killed or discarded non-mob body's scores and team
// for forgetting. It is safe under any entity lock; flushDestroyed applies
// the queue, since the board's listener broadcasts.
// vanilla: ServerLevel.EntityCallbacks.onDestroyed
func (s *Server) entityDestroyed(id uuid.UUID) { s.entityDestroyedBy(id, plugin.RemoveDiscard) }

// entityDestroyedBy is entityDestroyed with the reason EntityRemoveEvent
// reports.
func (s *Server) entityDestroyedBy(id uuid.UUID, cause plugin.RemoveCause) {
	s.destroyedMu.Lock()
	s.destroyed = append(s.destroyed, goneMob{id, cause})
	s.destroyedMu.Unlock()
}

// goneMob is an entity that left the world for good and why.
type goneMob struct {
	id    uuid.UUID
	cause plugin.RemoveCause
}

// fireEntityRemove tells the listeners an entity is gone; the handle no
// longer resolves, so a listener has its UUID and the cause. Runs with no
// lock held.
// vanilla: ServerLevel.EntityCallbacks.onDestroyed
func (s *Server) fireEntityRemove(id uuid.UUID, cause plugin.RemoveCause) {
	if r := s.plugins; r != nil && r.Has(pluginruntime.EntityRemove) {
		r.Fire(&plugin.EntityRemoveEvent{Entity: pluginEntity{host: s.pluginAdapter, id: id}, Cause: cause})
	}
}

// flushDestroyed forgets the queued bodies' scores and teams with no entity
// lock held.
// vanilla: Scoreboard.entityRemoved
func (s *Server) flushDestroyed() {
	s.destroyedMu.Lock()
	ids := s.destroyed
	s.destroyed = nil
	s.destroyedMu.Unlock()
	for _, gone := range ids {
		id := gone.id
		s.fireEntityRemove(id, gone.cause)
		s.scoreboard.EntityRemoved(id.String())
		s.dropEntityData(id.String())
		for _, board := range s.privateScoreboards() {
			board.EntityRemoved(id.String())
		}
	}
}

// newScoreboard is the world's scoreboard from scoreboard.dat, broadcasting
// changes to every player.
func (s *Server) newScoreboard(root string) *scoreboard.Board {
	board := scoreboard.New(func(e scoreboard.Event) {
		id, payload := scoreboardPacket(e)
		if s.plugins == nil {
			s.broadcast(id, payload)
		} else {
			s.sendScoreboardEvent(s.scoreboard, id, payload)
		}
	})
	data, ok, err := world.ReadSavedData(root, scoreboard.File, world.MaxSavedData)
	if err != nil {
		slog.Warn("could not read scoreboard.dat, starting with an empty scoreboard", "error", err)
	} else if ok {
		board.Load(data)
	}
	return board
}

// saveScoreboard writes scoreboard.dat if the board changed since the last
// save.
func (s *Server) saveScoreboard() {
	if !s.scoreboard.Dirty() {
		return
	}
	if err := world.WriteSavedData(s.levelDir, scoreboard.File, s.scoreboard.Pack()); err != nil {
		slog.Error("could not save the scoreboard", "error", err)
		s.scoreboard.MarkDirty()
	}
}

// sendScoreboard tells a joining player every displayed objective.
// vanilla: PlayerList.updateEntireScoreboard
func (c *client) sendScoreboard() error {
	for _, e := range c.srv.scoreboard.JoinEvents() {
		id, payload := scoreboardPacket(e)
		if err := c.writePacket(id, "scoreboard", payload); err != nil {
			return err
		}
	}
	return nil
}

// scoreboardPacket is the packet that carries a scoreboard event.
func scoreboardPacket(e scoreboard.Event) (int32, []byte) {
	var b protocol.Buffer
	switch e := e.(type) {
	case *scoreboard.ObjectiveEvent:
		// vanilla: ClientboundSetObjectivePacket
		b.PutString(e.Objective.Name)
		b.PutU8(uint8(e.Mode))
		if e.Mode != scoreboard.ObjectiveRemove {
			putComponent(&b, e.Objective.DisplayName)
			b.PutVarInt(int32(e.Objective.Render))
			putOptionalFormat(&b, e.Objective.Format)
		}
		return protocol.IDPlayUpdateObjectives, b.Bytes()
	case *scoreboard.DisplayEvent:
		// vanilla: ClientboundSetDisplayObjectivePacket
		b.PutVarInt(int32(e.Slot))
		b.PutString(e.Objective)
		return protocol.IDPlayDisplayObjective, b.Bytes()
	case *scoreboard.ScoreEvent:
		// vanilla: ClientboundSetScorePacket
		b.PutString(e.Holder)
		b.PutString(e.Objective)
		b.PutVarInt(e.Score.Value)
		b.PutBool(e.Score.Display != nil)
		if e.Score.Display != nil {
			putComponent(&b, e.Score.Display)
		}
		putOptionalFormat(&b, e.Score.Format)
		return protocol.IDPlayUpdateScore, b.Bytes()
	case *scoreboard.ResetEvent:
		// vanilla: ClientboundResetScorePacket; no objective resets the holder in
		// all of them.
		b.PutString(e.Holder)
		b.PutBool(e.Objective != "")
		if e.Objective != "" {
			b.PutString(e.Objective)
		}
		return protocol.IDPlayResetScore, b.Bytes()
	case *scoreboard.TeamEvent:
		// vanilla: ClientboundSetPlayerTeamPacket
		b.PutString(e.Team.Name)
		b.PutU8(uint8(e.Mode))
		if e.Mode != scoreboard.TeamRemove {
			putComponent(&b, e.Team.DisplayName)
			putComponent(&b, e.Team.Prefix)
			putComponent(&b, e.Team.Suffix)
			// The options byte: bit 0 friendly fire, bit 1 see friendly invisibles.
			var options uint8
			if e.Team.FriendlyFire {
				options |= 1
			}
			if e.Team.SeeFriendlyInvisible {
				options |= 2
			}
			b.PutVarInt(int32(e.Team.NameTag))
			b.PutVarInt(int32(e.Team.Collision))
			b.PutBool(e.Team.Color != scoreboard.NoColor)
			if e.Team.Color != scoreboard.NoColor {
				b.PutVarInt(int32(e.Team.Color))
			}
			b.PutU8(options)
		}
		if e.Mode == scoreboard.TeamAdd {
			putStrings(&b, e.Team.Players)
		}
		return protocol.IDPlaySetPlayerTeam, b.Bytes()
	case *scoreboard.TeamMemberEvent:
		b.PutString(e.Team)
		if e.Joined {
			b.PutU8(3)
		} else {
			b.PutU8(4)
		}
		putStrings(&b, []string{e.Player})
		return protocol.IDPlaySetPlayerTeam, b.Bytes()
	}
	panic("unknown scoreboard event")
}

// putComponent writes a stored component tag as network NBT.
func putComponent(b *protocol.Buffer, tag nbt.Tag) {
	if tag == nil {
		tag = nbt.String("")
	}
	encoded, err := nbt.EncodeNetwork(tag)
	if err != nil {
		// The command only stores tags TextOf read, which encode.
		encoded, _ = nbt.EncodeNetwork(nbt.String(""))
	}
	b.PutRaw(encoded)
}

// putOptionalFormat writes an optional number format: its
// minecraft:number_format_type id, then a styled format's style or a fixed
// one's component. vanilla: NumberFormatTypes.OPTIONAL_STREAM_CODEC
func putOptionalFormat(b *protocol.Buffer, format *scoreboard.NumberFormat) {
	b.PutBool(format != nil)
	if format == nil {
		return
	}
	b.PutVarInt(int32(format.Kind))
	switch format.Kind {
	case scoreboard.FormatStyled:
		style := format.Style
		if style == nil {
			style = nbt.Compound{}
		}
		putComponent(b, style)
	case scoreboard.FormatFixed:
		putComponent(b, format.Fixed)
	}
}

func putStrings(b *protocol.Buffer, list []string) {
	b.PutVarInt(int32(len(list)))
	for _, s := range list {
		b.PutString(s)
	}
}
