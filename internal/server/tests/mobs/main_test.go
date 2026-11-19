package mobs

import (
	"github.com/golem-mc/golem/internal/server"
	"os"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

func TestMain(m *testing.M) {
	os.Exit(servertest.RunMain(m))
}

// pumpTicks hands consume every packet skip does not drop while the next n
// ticks run, until consume says the wait is over; it reports whether it did.
func pumpTicks(c *servertest.Client, srv *server.Server, n int, skip func(int32) bool, consume func(id int32, data []byte) bool) bool {
	c.T.Helper()
	return c.ReadTicks(srv, n, func(id int32, data []byte) bool {
		if skip != nil && skip(id) {
			return false
		}
		return consume(id, data)
	})
}
