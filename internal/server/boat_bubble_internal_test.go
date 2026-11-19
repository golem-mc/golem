package server

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// The boat's splash timer rides metadata index 13 as a VarInt, sent when it
// changes and held back when it does not.
func TestBoatDeltaMetadataCarriesTheBubbleTimer(t *testing.T) {
	t.Parallel()
	if boatDeltaMetadata(7, boatShown{}, boatShown{}) != nil {
		t.Fatal("an unchanged boat produced metadata")
	}
	data := boatDeltaMetadata(7, boatShown{}, boatShown{bubbleTime: 59})
	var want protocol.Buffer
	want.PutVarInt(7)
	want.PutMetadata(protocol.MetadataIndexBoatBubbleTime, protocol.MetadataVarInt)
	want.PutVarInt(59)
	want.EndMetadata()
	if !bytes.Equal(data, want.Bytes()) {
		t.Errorf("metadata = %x, want %x", data, want.Bytes())
	}
}
