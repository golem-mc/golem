package mobs

import (
	"github.com/golem-mc/golem/internal/server"
)

func countKind(srv *server.Server, kind string) int {
	n := 0
	for _, m := range srv.Mobs() {
		if m.Kind == kind {
			n++
		}
	}
	return n
}
