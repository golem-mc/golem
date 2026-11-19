package servertest

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"testing"
)

// StuckArrows reads the victim's arrow-count metadata off the wire while the
// next ticks run (none on a stopped loop: what is already sent), returning
// the counts in the order they were told.
func StuckArrows(t *testing.T, c *Client, srv *server.Server, entityID int32, ticks int) []int32 {
	t.Helper()
	var counts []int32
	c.ReadTicks(srv, ticks, func(id int32, data []byte) bool {
		if id != protocol.IDPlayEntityMetadata {
			return false
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != entityID {
			return false
		}
		entries, ok := ReadMetadata(r)
		if !ok {
			t.Fatal("unreadable entity metadata")
		}
		if e := MetaOf(entries, protocol.MetadataIndexArrowCount); e != nil {
			counts = append(counts, e.VarintVal)
		}
		return false
	})
	return counts
}
