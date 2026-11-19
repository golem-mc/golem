package command

import (
	"github.com/golem-mc/golem/internal/server/player"
)

type sortPositionEntity struct {
	Entity
	pos   player.Position
	reads int
}

func (e *sortPositionEntity) Position() player.Position {
	e.reads++
	return e.pos
}
