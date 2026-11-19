package entities

import (
	"slices"

	"github.com/golem-mc/golem/internal/server"
)

func hit(srv *server.Server) bool {
	for _, e := range srv.SelectedEntities("minecraft:pig") {
		if slices.Contains(e.Tags(), "hit") {
			return true
		}
	}
	return false
}
