package commands

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

// blockEntityDataAt runs a command and returns the Block Entity Data
// compound the server broadcasts for it.
func blockEntityDataAt(t *testing.T, command string) nbt.Compound {
	t.Helper()
	_, c := servertest.Join(t, servertest.BlockTestConfig(), "Tagger", servertest.OpsListing(t, "Tagger"))
	c.ChatCommand(command)
	_, _, _, _, data := c.AwaitBlockEntityData()
	c.Disconnect()
	return data
}

// TestBlockEntityDataKeepsNoIdOrPosition checks the packet a command's tag
// lands as carries the update tag: no id, x, y or z.
// vanilla: ClientboundBlockEntityDataPacket.create, BlockEntity.getUpdateTag
func TestBlockEntityDataKeepsNoIdOrPosition(t *testing.T) {
	t.Parallel()
	data := blockEntityDataAt(t, `setblock 3 64 0 minecraft:spawner{SpawnData:{entity:{id:"minecraft:skeleton"}}}`)
	for _, key := range []string{"id", "x", "y", "z"} {
		if _, has := data[key]; has {
			t.Errorf("spawner packet carries %q: %#v", key, data)
		}
	}
	if _, has := data["SpawnData"]; !has {
		t.Errorf("spawner packet lost its SpawnData: %#v", data)
	}
}

// TestBlockEntityDataOfATypeWithoutAnUpdateTagIsEmpty checks a copper golem
// statue, which overrides getUpdatePacket but not getUpdateTag, is told with
// an empty compound whatever tag the command stored.
// vanilla: BlockEntity.getUpdateTag
func TestBlockEntityDataOfATypeWithoutAnUpdateTagIsEmpty(t *testing.T) {
	t.Parallel()
	data := blockEntityDataAt(t, `setblock 3 64 0 minecraft:copper_golem_statue{Marker:1b}`)
	if len(data) != 0 {
		t.Errorf("statue packet = %#v, want an empty compound", data)
	}
}
