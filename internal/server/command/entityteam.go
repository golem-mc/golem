package command

import (
	"encoding/binary"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/scoreboard"
	"github.com/golem-mc/golem/nbt"
	"github.com/google/uuid"
)

// TeamEntity supplies a body's effective team, including owner inheritance.
type TeamEntity interface {
	Entity
	Team() string
}

// TeamFormattedName is name between the prefix and suffix of holder's team,
// in its colour; name itself for a holder on no team.
// vanilla: PlayerTeam.formatNameForTeam
func TeamFormattedName(board Scoreboard, holder string, name protocol.Text) protocol.Text {
	t, ok := board.MemberTeam(holder)
	if !ok {
		return name
	}
	text := protocol.Text{Extra: []protocol.Text{teamComponent(t.Prefix), name, teamComponent(t.Suffix)}}
	if t.Color != scoreboard.NoColor {
		text.Color = scoreboard.TeamColors()[t.Color]
	}
	return text
}

// teamComponent is a team's stored prefix or suffix, empty text when unset.
func teamComponent(tag nbt.Tag) protocol.Text {
	if tag == nil {
		return protocol.Text{}
	}
	text, _ := protocol.StyledTextOf(tag)
	return text
}

// EntityDisplayName is Entity.getDisplayName for a non-player body: name
// formatted for the team its UUID is on, with UUID insertion and a
// show-entity hover carrying name undecorated.
// vanilla: Entity.getDisplayName, createHoverEvent
func EntityDisplayName(board Scoreboard, entityType string, identity uuid.UUID, name protocol.Text) protocol.Text {
	text := TeamFormattedName(board, identity.String(), name)
	if text.Raw != nil {
		// A raw tag carries no insertion or hover of its own.
		text = protocol.Text{Extra: []protocol.Text{text}}
	}
	text.Insertion = identity.String()
	var words [4]uint32
	for i := range words {
		words[i] = binary.BigEndian.Uint32(identity[i*4 : (i+1)*4])
	}
	text.HoverEntity = &protocol.HoverEntity{Name: name, ID: entityType, UUID: words}
	return text
}

// JoinTagTeam puts a body loaded from tag on the team its Team string names.
// It answers the team's name and whether the body joined: false with a name
// for a team that does not exist, false with none for a tag with no Team.
// vanilla: Entity.load
func JoinTagTeam(board Scoreboard, identity uuid.UUID, tag nbt.Compound) (string, bool) {
	team, ok := nbt.Get[nbt.String](tag, "Team")
	if !ok {
		return "", false
	}
	if _, exists := board.Team(string(team)); !exists {
		return string(team), false
	}
	board.JoinTeam(string(team), []string{identity.String()})
	return string(team), true
}
