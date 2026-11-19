package servertest

import (
	"encoding/hex"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
)

// MetadataRecorder keeps the Set Entity Metadata payloads a client is sent
// for the bodies a test drives, by case name, each payload
// hex with its entity ID cut off. The loop must be held.
type MetadataRecorder struct {
	T   *testing.T
	Srv *server.Server
	C   *Client
	// Ticks is how many ticks Run reads after a command has been handled.
	Ticks int
	// Rewrite, where set, turns a payload's hex into what the golden file
	// keeps, for a value (an entity ID) that differs from run to run.
	Rewrite func(id int32, payload []byte) string
	Got     map[string][]string
}

// NewMetadataRecorder is a recorder reading two ticks after each command.
func NewMetadataRecorder(t *testing.T, srv *server.Server, c *Client) *MetadataRecorder {
	return &MetadataRecorder{T: t, Srv: srv, C: c, Ticks: 2, Got: map[string][]string{}}
}

// Run sends command and hands back every packet the client is sent up to
// r.Ticks ticks after it has been handled.
func (r *MetadataRecorder) Run(command string) []PushedPacket {
	r.T.Helper()
	r.C.ChatCommand(command)
	return r.Read(r.Ticks)
}

// Read hands back every packet the client is sent from now until n ticks
// have run.
func (r *MetadataRecorder) Read(n int) []PushedPacket {
	r.T.Helper()
	var packets []PushedPacket
	keep := func(id int32, data []byte) {
		packets = append(packets, PushedPacket{Id: id, Data: slices.Clone(data)})
	}
	r.C.SettleAndCollect(keep)
	r.C.ReadTicks(r.Srv, n, func(id int32, data []byte) bool {
		keep(id, data)
		return false
	})
	return packets
}

// Record keeps the metadata payloads for entity id among packets under the
// case's name, after any already kept under it.
func (r *MetadataRecorder) Record(name string, id int32, packets []PushedPacket) {
	for _, p := range packets {
		if p.Id != protocol.IDPlayEntityMetadata {
			continue
		}
		rd := protocol.NewReader(p.Data)
		if rd.ReadVarInt() != id {
			continue
		}
		payload := p.Data[len(p.Data)-rd.Remaining():]
		text := hex.EncodeToString(payload)
		if r.Rewrite != nil {
			text = r.Rewrite(id, payload)
		}
		r.Got[name] = append(r.Got[name], text)
	}
}

// SpawnedID is the entity ID of the first body of entityType among packets'
// spawns, or 0 for none.
func SpawnedID(packets []PushedPacket, entityType int32) int32 {
	for _, p := range packets {
		if p.Id != protocol.IDPlaySpawnEntity {
			continue
		}
		rd := protocol.NewReader(p.Data)
		id := rd.ReadVarInt()
		rd.ReadUUID()
		if rd.ReadVarInt() == entityType {
			return id
		}
	}
	return 0
}

// MetadataIndices reads the field indices of a payload a MetadataRecorder
// kept without a rewrite, in order.
func MetadataIndices(t *testing.T, payload string) []uint8 {
	t.Helper()
	raw, err := hex.DecodeString(payload)
	if err != nil {
		t.Fatalf("payload %q: %v", payload, err)
	}
	rd := protocol.NewReader(raw)
	var indices []uint8
	for rd.Err() == nil {
		index := rd.ReadU8()
		if index == protocol.MetadataEnd {
			break
		}
		indices = append(indices, index)
		StepOverMetadataValue(rd, rd.ReadVarInt())
	}
	if err := rd.Err(); err != nil {
		t.Fatalf("payload %q: %v", payload, err)
	}
	return indices
}
