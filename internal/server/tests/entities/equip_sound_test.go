package entities

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// equipSounds reads n ticks and returns the category of each Sound Effect
// named want, in order.
func equipSounds(t *testing.T, c *servertest.Client, srv *server.Server, n int, want string) []int32 {
	t.Helper()
	id := soundID(t, want)
	var cats []int32
	c.ReadTicks(srv, n, func(pid int32, data []byte) bool {
		if pid == protocol.IDPlaySoundEffect {
			if s := servertest.DecodePlaySoundEffect(t, data); s.RegistryID == id {
				cats = append(cats, s.Category)
			}
		}
		return false
	})
	return cats
}
