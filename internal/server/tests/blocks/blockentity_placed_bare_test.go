package blocks

import (
	"encoding/binary"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

// bareChunkEntries reads (packed position, y, type ID) of each block entity in
// a Chunk Data packet, skipping the update tags.
func bareChunkEntries(t *testing.T, packet []byte) map[[3]int32]int32 {
	t.Helper()
	_, r := servertest.ParseHeightmaps(t, packet)
	r.ReadBytes(1 << 24) // the chunk's sections
	count := int(r.ReadVarInt())
	if err := r.Err(); err != nil {
		t.Fatalf("chunk data head: %v", err)
	}
	data := r.Rest()
	off := 0
	out := map[[3]int32]int32{}
	for range count {
		packed := data[off]
		y := int32(int16(binary.BigEndian.Uint16(data[off+1:])))
		off += 3
		var id uint32
		for shift := uint(0); ; shift += 7 {
			b := data[off]
			off++
			id |= uint32(b&0x7F) << shift
			if b&0x80 == 0 {
				break
			}
		}
		out[[3]int32{int32(packed >> 4), y, int32(packed & 0xF)}] = int32(id)
		if data[off] == 0 {
			off++
			continue
		}
		_, n, err := nbt.DecodeNetwork(data[off:])
		if err != nil {
			t.Fatalf("update tag: %v", err)
		}
		off += n
	}
	return out
}

// A block set with no data still owns a block entity, so a chunk sent after
// the write lists it and the block entity commands find it.
// vanilla: LevelChunk.setBlockState, EntityBlock.newBlockEntity
func TestBlocksSetWithoutDataGetABlockEntity(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Builder"))
	c := connect().Join("Builder")
	servertest.HoldTicks(t, srv)
	blocks := []struct {
		block, typ string
	}{
		{"white_banner", "minecraft:banner"},
		{"player_head", "minecraft:skull"},
		{"skeleton_skull", "minecraft:skull"},
		{"enchanting_table", "minecraft:enchanting_table"},
		{"spawner", "minecraft:mob_spawner"},
		{"end_gateway", "minecraft:end_gateway"},
	}
	for i, b := range blocks {
		srv.RunLine("setblock " + string(rune('1'+i)) + " 64 3 " + b.block)
	}
	c.ChatCommand("data merge block 5 64 3 {Delay:5s}")
	servertest.AwaitCommandFeedback(t, c, "commands.data.block.modified", "5", "64", "3")

	reader := connect()
	reader.Login("Reader")
	reader.Configure()
	entries := bareChunkEntries(t, servertest.ChunkAt(t, reader, 0, 0))
	for i, b := range blocks {
		got, ok := entries[[3]int32{int32(1 + i), 64, 3}]
		if !ok {
			t.Errorf("%s: no block entity in the chunk packet", b.block)
			continue
		}
		if want := world.BlockEntityTypeID(b.typ); got != want {
			t.Errorf("%s: block entity type %d, want %d (%s)", b.block, got, want, b.typ)
		}
	}
}
