package servertest

import "github.com/golem-mc/golem/internal/server"

// ReadTicksSkipping is ReadTicks over the packets skip does not name: see is
// handed the rest while the next n ticks run, and a nil skip steps over
// nothing.
func (c *Client) ReadTicksSkipping(srv *server.Server, n int, skip func(int32) bool, see func(id int32, data []byte) bool) bool {
	c.T.Helper()
	return c.ReadTicks(srv, n, func(id int32, data []byte) bool {
		if skip != nil && skip(id) {
			return false
		}
		return see(id, data)
	})
}
