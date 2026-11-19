package combat

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// tridentSpawnMetadata reads the next n ticks until a trident is spawned
// and returns the metadata its pairing told beside the spawn: the packet
// right after it where that is metadata about it, none otherwise.
func tridentSpawnMetadata(t *testing.T, c *servertest.Client, srv *server.Server, n int) []tridentMetadata {
	t.Helper()
	var id int32
	for gotID, data := range tridentPackets(c, srv, n) {
		if id != 0 {
			if gotID == protocol.IDPlayEntityMetadata {
				if m := readTridentMetadata(c, data); m.entityID == id {
					return []tridentMetadata{m}
				}
			}
			return nil
		}
		if gotID != protocol.IDPlaySpawnEntity {
			continue
		}
		r := protocol.NewReader(data)
		spawned := r.ReadVarInt()
		r.ReadUUID()
		if r.ReadVarInt() == protocol.EntityTypeTrident {
			id = spawned
		}
	}
	t.Fatalf("no trident came into the world within %d ticks", n)
	return nil
}
