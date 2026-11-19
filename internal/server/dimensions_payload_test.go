package server

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// The dimension list of Login (play) is the three names with no plugin world.
func TestDimensionPayloadsVanillaThree(t *testing.T) {
	s := withDims(&Server{}, []*dimension{{name: "minecraft:overworld"}, {name: "minecraft:the_nether"}, {name: "minecraft:the_end"}})
	var got, want protocol.Buffer
	s.dimensionPayloads(&got)
	want.PutVarInt(3)
	for _, n := range []string{"minecraft:overworld", "minecraft:the_nether", "minecraft:the_end"} {
		want.PutString(n)
	}
	if !bytes.Equal(got.Bytes(), want.Bytes()) {
		t.Errorf("payload = %x, want %x", got.Bytes(), want.Bytes())
	}
}
