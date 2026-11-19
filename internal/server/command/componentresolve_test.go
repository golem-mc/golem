package command

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

type storedComponentSource struct {
	forkSource
	storage nbt.Compound
}

func (*storedComponentSource) SelfEntity() Entity { return nil }
func (s *storedComponentSource) ReadCommandStorage(_ string, fn func(nbt.CompoundTag)) {
	fn(s.storage)
}

// TestResolveComponentFoldsNestedLists keeps the first component's style and
// siblings while adding the remaining elements of each list.
func TestResolveComponentFoldsNestedLists(t *testing.T) {
	tag := nbt.NewList([]nbt.Tag{
		nbt.NewList([]nbt.Tag{
			nbt.Compound{"text": nbt.String("A"), "color": nbt.String("red"), "extra": nbt.NewList([]nbt.Tag{nbt.String("B")})},
			nbt.String("C"),
		}),
		nbt.String("D"),
	})
	resolved, resolvedOK := resolveComponent(nil, nil, tag)
	compound, ok := resolved.(nbt.Compound)
	if !resolvedOK || !ok || compound["color"] != nbt.String("red") || protocol.PlainTextOf(resolved) != "ABCD" {
		t.Errorf("resolved = %v, want red ABCD as a compound", resolved)
	}
	var wire protocol.Buffer
	wire.PutText(protocol.Text{Raw: resolved})
	text, ok := protocol.ReadText(protocol.NewReader(wire.Bytes()))
	if !ok || text.Plain() != "ABCD" {
		t.Errorf("wire component = %+v (ok %v), want ABCD", text, ok)
	}
}
